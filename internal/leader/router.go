package leader

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"

	"AskCore/pkg/protocol"

	"go.uber.org/zap"
)

// Events that reach the router. The router handles them one at a time on one
// goroutine, so its state needs no lock and never waits for I/O.
type (
	addClientEvent struct{ c *client }
	clientMsgEvent struct {
		c   *client
		raw json.RawMessage
	}
	clientGoneEvent struct{ c *client }
	agentLineEvent  struct{ line []byte }
	agentGoneEvent  struct{ err error }
	callEvent       struct {
		fn   func()
		done chan struct{}
	}
)

type tagKind int

const (
	tagNone tagKind = iota
	tagNewSession
	tagFollow
	tagUnfollow
	tagTake
)

// maxEndedFollows is how many ended subscriptions the router remembers. The
// record keeps a repeated unfollow idempotent and keeps a foreign client from
// reaching an ended subscription. The oldest record goes first.
const maxEndedFollows = 65536

// noteEnded records that a subscription ended and who owned it.
func (r *router) noteEnded(sub, owner string) {
	if _, known := r.ended[sub]; !known {
		r.endedOrder = append(r.endedOrder, sub)
		for len(r.endedOrder) > maxEndedFollows {
			delete(r.ended, r.endedOrder[0])
			r.endedOrder = r.endedOrder[1:]
		}
	}
	r.ended[sub] = owner
}

// reqTag remembers what a forwarded request was for, so the router can finish the
// job even when the client has left before the answer arrives.
type reqTag struct {
	kind      tagKind
	session   string
	cwd       string
	memberGen uint64
	sub       string
}

type member struct{ gen uint64 }

type session struct {
	id          string
	cwd         string
	driver      *client
	gen         uint64 // the driver generation that the host committed
	members     map[*client]struct{}
	takePending bool
	takeCaller  *client
}

type follow struct {
	sub     string
	session string
	owner   *client
}

type router struct {
	log  *zap.Logger
	cfg  Config
	ids  *IDTable
	in   chan any
	stop chan struct{}
	done chan struct{}

	agentQ *queue[[]byte]

	initResult json.RawMessage
	linkReady  chan struct{}
	linkErr    error

	clients    map[string]*client
	sessions   map[string]*session
	follows    map[string]*follow
	ended      map[string]string // ended subscription -> owner client id
	endedOrder []string
	internal   map[string]func(*rpcMessage)
	reverse    map[string]*reverseReq // by the canonical agent id
	byRID      map[string]*reverseRecipient
	finished   []*reverseReq
	terminals  map[string]*terminalOwner
	ridSeq     uint64

	memberSeq   uint64
	internalSeq uint64
	closing     bool
	// draining is set after a quiesce. The router then takes no new request.
	draining bool
}

func newRouter(cfg Config, agentQ *queue[[]byte]) *router {
	return &router{
		log:       cfg.Logger,
		cfg:       cfg,
		ids:       NewIDTable(),
		in:        make(chan any, 256),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		agentQ:    agentQ,
		linkReady: make(chan struct{}),
		clients:   map[string]*client{},
		sessions:  map[string]*session{},
		follows:   map[string]*follow{},
		ended:     map[string]string{},
		internal:  map[string]func(*rpcMessage){},
		reverse:   map[string]*reverseReq{},
		byRID:     map[string]*reverseRecipient{},
		terminals: map[string]*terminalOwner{},
	}
}

// post sends an event to the router. It reports false once the router has stopped.
func (r *router) post(ev any) bool {
	// A select picks at random when both cases are ready, so the stop is checked first.
	select {
	case <-r.done:
		return false
	default:
	}
	select {
	case r.in <- ev:
		return true
	case <-r.done:
		return false
	}
}

// call runs fn on the router goroutine and waits for it.
func (r *router) call(fn func()) bool {
	ev := callEvent{fn: fn, done: make(chan struct{})}
	if !r.post(ev) {
		return false
	}
	select {
	case <-ev.done:
		return true
	case <-r.done:
		return false
	}
}

func (r *router) run() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			r.shutdown()
			return
		case ev := <-r.in:
			switch e := ev.(type) {
			case addClientEvent:
				r.clients[e.c.id] = e.c
			case clientMsgEvent:
				r.onClientMsg(e.c, e.raw)
			case clientGoneEvent:
				r.removeClient(e.c)
			case agentLineEvent:
				r.onAgentLine(e.line)
			case agentGoneEvent:
				r.linkErr = e.err
				r.shutdown()
				return
			case callEvent:
				e.fn()
				close(e.done)
			}
		}
	}
}

// shutdown tells every client and closes it. The router handles no more events.
func (r *router) shutdown() {
	r.closing = true
	for _, c := range r.clients {
		c.sendError(protocol.LeaderError{Kind: protocol.LeaderErrShuttingDown, Message: "the leader is stopping"})
		c.finish()
	}
	r.clients = map[string]*client{}
}

// ---- client messages ----

func (r *router) reply(c *client, raw json.RawMessage) { c.sendACP(raw, "", 0) }

func (r *router) fail(c *client, m *rpcMessage, kind protocol.ACPErrorKind) {
	if m.hasID {
		r.reply(c, errorResponse(m.id, kind))
		return
	}
	r.log.Debug("notification refused", zap.String("client", c.id), zap.String("method", m.method), zap.String("kind", string(kind)))
}

func (r *router) onClientMsg(c *client, raw json.RawMessage) {
	if c.closed {
		return
	}
	m, err := parseMessage(raw)
	if err != nil {
		var id json.RawMessage
		if fields, derr := decodeObject(raw); derr == nil {
			if cand, ok := fields["id"]; ok {
				if _, cerr := canonicalID(cand); cerr == nil {
					id = cand
				}
			}
		}
		r.reply(c, invalidRequest(id))
		return
	}
	if !m.hasMethod {
		r.onClientResponse(c, m)
		return
	}
	if r.draining && m.method != "$/cancel_request" {
		r.fail(c, m, protocol.ACPErrDisposed)
		return
	}
	if m.method == "$/cancel_request" {
		r.forwardCancel(c, m)
		return
	}
	pol, known := policies[m.method]
	if !known {
		r.fail(c, m, protocol.ACPErrUnsupported)
		return
	}
	if pol.kind != callInitialize && !c.initialized {
		r.fail(c, m, protocol.ACPErrClientNotInitialized)
		return
	}
	switch pol.kind {
	case callInitialize:
		r.handleInitialize(c, m)
	case callUnsupported:
		r.fail(c, m, protocol.ACPErrUnsupported)
	case callAuthenticate:
		r.forward(c, m, protocol.ACPRouteMeta{ClientID: c.id}, nil)
	case callNewSession:
		r.handleNewSession(c, m)
	case callListLive:
		r.handleListLive(c, m)
	default:
		r.handleSessionCall(c, m, pol)
	}
}

func (r *router) handleInitialize(c *client, m *rpcMessage) {
	if !m.hasID {
		return
	}
	params, err := m.params()
	if err != nil || !validACPVersion(params["protocolVersion"]) {
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return
	}
	if c.initialized && (len(c.members) > 0 || r.ids.Pending(c.id) > 0 || r.holdsReverse(c)) {
		// New capabilities would change what the client may do in sessions it already uses.
		r.fail(c, m, protocol.ACPErrInvalidState)
		return
	}
	if len(params["clientCapabilities"]) > maxCapabilitiesBytes {
		// The capabilities go into the route context of every call.
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return
	}
	c.caps = params["clientCapabilities"]
	c.initialized = true
	r.reply(c, marshalPlain(map[string]any{"jsonrpc": "2.0", "id": m.id, "result": r.initResult}))
}

// maxCapabilitiesBytes bounds the client capabilities that a client may declare.
const maxCapabilitiesBytes = 64 << 10

// validACPVersion accepts a JSON integer of 1 or more. A quoted number is not a number.
func validACPVersion(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var n json.Number
	if d.Decode(&n) != nil {
		return false
	}
	v, err := n.Int64()
	return err == nil && v >= 1
}

func (r *router) handleNewSession(c *client, m *rpcMessage) {
	params, err := m.params()
	if err != nil {
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return
	}
	r.forward(c, m, protocol.ACPRouteMeta{ClientID: c.id, Capabilities: c.caps},
		&reqTag{kind: tagNewSession, cwd: stringParam(params, "cwd")})
}

// handleSessionCall serves every method that names a session.
func (r *router) handleSessionCall(c *client, m *rpcMessage, pol policy) {
	params, err := m.params()
	sid := ""
	if err == nil {
		sid = stringParam(params, "sessionId")
	}
	if sid == "" {
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return
	}
	s := r.sessions[sid]
	if s == nil {
		r.fail(c, m, protocol.ACPErrUnknownSession)
		return
	}
	_, isMember := c.members[sid]
	switch pol.kind {
	case callAttach:
		r.handleAttach(c, m, s)
		return
	case callDetach:
		r.handleDetach(c, m, s)
		return
	case callTake:
		r.handleTake(c, m, s)
		return
	case callUnfollow:
		r.handleUnfollow(c, m, params, s)
		return
	}
	if pol.driverOnly && s.driver != c {
		r.fail(c, m, protocol.ACPErrNotDriver)
		return
	}
	if !isMember {
		if !pol.subscribe {
			r.fail(c, m, protocol.ACPErrNotDriver)
			return
		}
		r.addMember(c, s)
	}
	meta := protocol.ACPRouteMeta{ClientID: c.id, SessionID: sid, DriverGen: s.gen, LiveDriver: s.driver != nil}
	if s.driver == c {
		meta.Capabilities = c.caps
	}
	var tag *reqTag
	if pol.kind == callFollow {
		tag = &reqTag{kind: tagFollow, session: sid, memberGen: c.members[sid].gen}
	}
	r.forward(c, m, meta, tag)
}

// forward sends a client request or notification to the agent with a route context.
func (r *router) forward(c *client, m *rpcMessage, meta protocol.ACPRouteMeta, tag *reqTag) bool {
	params, err := m.params()
	if err != nil {
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return false
	}
	fields := make(map[string]json.RawMessage, len(m.fields))
	for k, v := range m.fields {
		fields[k] = v
	}
	fields["params"] = injectRoute(params, meta)
	msgOut := marshalPlain(fields)
	if len(msgOut) > MaxLine {
		// The line codec of the agent link would drop it and the caller would wait for ever.
		r.fail(c, m, protocol.ACPErrInvalidParams)
		return false
	}
	var anyTag any
	if tag != nil {
		anyTag = tag
	}
	out, err := r.ids.ForwardTagged(c.id, msgOut, anyTag)
	if err != nil {
		r.reply(c, invalidRequest(m.id))
		return false
	}
	if !out.Drop {
		if len(out.Msg) > MaxLine {
			// Rewriting a short client id can make a line exceed the limit.
			forwarded, _ := parseMessage(out.Msg)
			r.ids.Restore(errorResponse(forwarded.id, protocol.ACPErrInvalidParams))
			r.fail(c, m, protocol.ACPErrInvalidParams)
			return false
		}
		r.agentQ.push(out.Msg)
	}
	return true
}

func (r *router) forwardCancel(c *client, m *rpcMessage) {
	out, err := r.ids.Forward(c.id, m.raw)
	if err != nil || out.Drop {
		return
	}
	r.agentQ.push(out.Msg)
}

// ---- membership ----

func (r *router) addMember(c *client, s *session) {
	if _, ok := c.members[s.id]; ok {
		return
	}
	r.memberSeq++
	c.members[s.id] = member{gen: r.memberSeq}
	s.members[c] = struct{}{}
}

// removeMember ends the membership of a client: its driver role, its follows and
// its place in shared questions. The run of the session goes on.
func (r *router) removeMember(c *client, s *session) {
	if s.takeCaller == c {
		s.takeCaller = nil
	}
	mem, ok := c.members[s.id]
	if !ok {
		return
	}
	delete(c.members, s.id)
	delete(s.members, c)
	for _, f := range r.follows {
		if f.owner == c && f.session == s.id {
			r.releaseFollow(f)
		}
	}
	r.dropRecipient(c, s.id)
	if s.driver == c {
		r.driverLost(s)
	}
	c.purge(s.id, mem.gen)
}

// driverLost leaves the session without a driver. The run goes on. The open
// questions of the lost driver generation end.
func (r *router) driverLost(s *session) {
	s.driver = nil
	r.cancelQuestions(s, s.gen)
}

func (r *router) removeClient(c *client) {
	if c.closed {
		return
	}
	c.closed = true
	delete(r.clients, c.id)
	r.ids.DropClient(c.id)
	for sid := range c.members {
		if s := r.sessions[sid]; s != nil {
			r.removeMember(c, s)
		}
	}
	r.dropRecipient(c, "")
	r.forgetTerminals(c)
	c.finish()
}

// ---- attach, detach, take, list ----

func (r *router) handleAttach(c *client, m *rpcMessage, s *session) {
	if !m.hasID {
		return
	}
	r.addMember(c, s)
	role := protocol.ACPRoleObserver
	if s.driver == c {
		role = protocol.ACPRoleDriver
	}
	r.reply(c, resultResponse(m.id, protocol.ACPAttachResult{SessionID: s.id, Role: role}))
	// A client that joins late still sees the questions that are open.
	r.replayQuestions(c, s)
}

func (r *router) handleDetach(c *client, m *rpcMessage, s *session) {
	r.removeMember(c, s)
	if m.hasID {
		// The purge ran inside removeMember, so no event of the old membership
		// can follow the answer.
		r.reply(c, resultResponse(m.id, map[string]any{}))
	}
}

func (r *router) handleListLive(c *client, m *rpcMessage) {
	if !m.hasID {
		return
	}
	rows := make([]protocol.ACPLiveSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		row := protocol.ACPLiveSession{SessionID: s.id, Cwd: s.cwd, HasDriver: s.driver != nil, Subscribers: len(s.members)}
		if _, ok := c.members[s.id]; ok {
			row.Role = protocol.ACPRoleObserver
			if s.driver == c {
				row.Role = protocol.ACPRoleDriver
			}
		}
		rows = append(rows, row)
	}
	sortLive(rows)
	r.reply(c, resultResponse(m.id, protocol.ACPListLiveResult{Sessions: rows}))
}

func (r *router) handleTake(c *client, m *rpcMessage, s *session) {
	if !m.hasID {
		return
	}
	if s.driver == c {
		r.reply(c, resultResponse(m.id, protocol.ACPTakeResult{SessionID: s.id, DriverGen: s.gen}))
		return
	}
	if s.takePending {
		r.fail(c, m, protocol.ACPErrBusy)
		return
	}
	s.takePending = true
	s.takeCaller = c
	gen := uint64(0)
	if mem, ok := c.members[s.id]; ok {
		gen = mem.gen
	}
	meta := protocol.ACPRouteMeta{ClientID: c.id, SessionID: s.id, DriverGen: s.gen, LiveDriver: s.driver != nil, Capabilities: c.caps}
	if !r.forward(c, m, meta, &reqTag{kind: tagTake, session: s.id, memberGen: gen}) {
		s.takePending = false // the request was refused before it left
		s.takeCaller = nil
	}
}

func (r *router) handleUnfollow(c *client, m *rpcMessage, params map[string]json.RawMessage, s *session) {
	sub := stringParam(params, "subscriptionId")
	owner := ""
	if f := r.follows[sub]; f != nil {
		owner = f.owner.id
	} else if id, ok := r.ended[sub]; ok {
		owner = id
	}
	if owner != "" && owner != c.id {
		// Another client's subscription is never visible to this client.
		r.fail(c, m, protocol.ACPErrUnknownSubscription)
		return
	}
	meta := protocol.ACPRouteMeta{ClientID: c.id, SessionID: s.id, DriverGen: s.gen, LiveDriver: s.driver != nil}
	r.forward(c, m, meta, &reqTag{kind: tagUnfollow, session: s.id, sub: sub})
}

// releaseFollow ends a follow in the host with an internal request.
func (r *router) releaseFollow(f *follow) {
	delete(r.follows, f.sub)
	r.noteEnded(f.sub, f.owner.id)
	s := r.sessions[f.session]
	meta := protocol.ACPRouteMeta{ClientID: f.owner.id, SessionID: f.session}
	if s != nil {
		meta.DriverGen, meta.LiveDriver = s.gen, s.driver != nil
	}
	r.internalRequest(protocol.ACPUnfollow, map[string]any{
		"sessionId": f.session, "subscriptionId": f.sub, "_meta": map[string]any{protocol.ACPRouteMetaKey: meta},
	}, nil)
}

// releaseFollowByID ends a follow that no client owns any more.
func (r *router) releaseOrphanFollow(sessionID, sub string) {
	s := r.sessions[sessionID]
	meta := protocol.ACPRouteMeta{ClientID: internalClient, SessionID: sessionID}
	if s != nil {
		meta.DriverGen, meta.LiveDriver = s.gen, s.driver != nil
	}
	r.noteEnded(sub, "")
	r.internalRequest(protocol.ACPUnfollow, map[string]any{
		"sessionId": sessionID, "subscriptionId": sub, "_meta": map[string]any{protocol.ACPRouteMetaKey: meta},
	}, nil)
}

// internalClient is the client id of a request that the router makes itself.
const internalClient = "leader"

func (r *router) internalRequest(method string, params any, done func(*rpcMessage)) {
	r.internalSeq++
	id := "leader:" + strconv.FormatUint(r.internalSeq, 10)
	if done == nil {
		// Nobody waits for the answer, but a refusal must not pass without a word:
		// the follow or the other resource would stay in the host.
		done = func(m *rpcMessage) {
			if m.fields["error"] != nil {
				r.log.Warn("the host refused a request of the router", zap.String("method", method), zap.ByteString("error", m.fields["error"]))
			}
		}
	}
	r.internal[id] = done
	r.agentQ.push(requestMessage(id, method, params))
}

// ---- agent messages ----

func (r *router) onAgentLine(line []byte) {
	m, err := parseMessage(line)
	if err != nil {
		r.log.Warn("agent sent an invalid message", zap.Error(err))
		return
	}
	switch {
	case !m.hasMethod:
		r.onAgentResponse(m)
	case m.hasID:
		r.onAgentRequest(m)
	default:
		r.onAgentNotification(m)
	}
}

func (r *router) onAgentResponse(m *rpcMessage) {
	var idText string
	if json.Unmarshal(m.id, &idText) == nil {
		if done, ok := r.internal[idText]; ok {
			delete(r.internal, idText)
			done(m)
			return
		}
	}
	restored, ok := r.ids.Restore(m.raw)
	if !ok {
		r.log.Debug("agent response has no route")
		return
	}
	c := r.clients[restored.ClientID]
	out, perr := parseMessage(restored.Msg)
	if perr != nil {
		return
	}
	tag, _ := restored.Tag.(*reqTag)
	if tag == nil {
		tag = &reqTag{}
	}
	failed := out.fields["error"] != nil
	switch tag.kind {
	case tagNewSession:
		r.finishNewSession(c, restored, out, m, tag, failed)
	case tagFollow:
		r.finishFollow(c, restored, out, tag, failed)
	case tagUnfollow:
		if !failed && c != nil {
			if f := r.follows[tag.sub]; f != nil && f.owner == c {
				delete(r.follows, tag.sub)
			}
			r.noteEnded(tag.sub, c.id)
		}
		r.deliver(c, restored, restored.Msg)
	case tagTake:
		r.finishTake(c, restored, out, m, tag, failed)
	default:
		r.deliver(c, restored, restored.Msg)
	}
}

func (r *router) deliver(c *client, restored Restored, msg json.RawMessage) {
	if c == nil || restored.Dropped {
		return
	}
	c.sendACP(msg, "", 0)
}

func (r *router) finishNewSession(c *client, restored Restored, out, agentMsg *rpcMessage, tag *reqTag, failed bool) {
	if failed {
		r.deliver(c, restored, restored.Msg)
		return
	}
	result, _ := decodeObject(agentMsg.fields["result"])
	sid := stringParam(result, "sessionId")
	gen, ok := routeGen(agentMsg)
	if sid == "" || !ok {
		r.log.Error("host session result has no session id or driver generation")
		r.deliver(c, restored, errorResponse(out.id, protocol.ACPErrInternal))
		return
	}
	s := &session{id: sid, cwd: tag.cwd, gen: gen, members: map[*client]struct{}{}}
	r.sessions[sid] = s
	if c == nil || restored.Dropped || c.closed {
		return // the session lives; any client can attach later
	}
	s.driver = c
	r.addMember(c, s)
	c.sendACP(stripRouteMeta(out), "", 0)
}

func (r *router) finishFollow(c *client, restored Restored, out *rpcMessage, tag *reqTag, failed bool) {
	if failed {
		r.deliver(c, restored, restored.Msg)
		return
	}
	result, _ := decodeObject(out.fields["result"])
	sub := stringParam(result, "subscriptionId")
	if sub == "" {
		r.deliver(c, restored, restored.Msg)
		return
	}
	stillMember := c != nil && !restored.Dropped && !c.closed && c.members[tag.session].gen == tag.memberGen
	if !stillMember {
		// The caller left before the result: end the follow, tell nobody.
		r.releaseOrphanFollow(tag.session, sub)
		return
	}
	r.follows[sub] = &follow{sub: sub, session: tag.session, owner: c}
	delete(r.ended, sub)
	c.sendACP(restored.Msg, tag.session, tag.memberGen)
}

func (r *router) finishTake(c *client, restored Restored, out, agentMsg *rpcMessage, tag *reqTag, failed bool) {
	s := r.sessions[tag.session]
	if s == nil {
		r.deliver(c, restored, restored.Msg)
		return
	}
	present := c != nil && s.takeCaller == c && !restored.Dropped && !c.closed
	s.takePending = false
	s.takeCaller = nil
	if failed {
		r.deliver(c, restored, restored.Msg)
		return
	}
	var res protocol.ACPTakeResult
	if json.Unmarshal(agentMsg.fields["result"], &res) != nil || res.DriverGen == 0 {
		r.log.Error("host take result has no driver generation")
		r.deliver(c, restored, errorResponse(out.id, protocol.ACPErrInternal))
		return
	}
	old := s.gen
	if s.driver != nil && s.driver != c {
		s.driver = nil
	}
	s.gen = res.DriverGen
	if old != s.gen {
		r.cancelQuestions(s, old)
	}
	if present && tag.memberGen != 0 && c.members[s.id].gen != tag.memberGen {
		present = false // the caller detached while the take ran
	}
	if !present {
		// The host moved on, but nobody is there to drive. The session has no driver.
		s.driver = nil
		if c != nil && !c.closed && !restored.Dropped {
			c.sendACP(errorResponse(out.id, protocol.ACPErrCancelled), "", 0)
		}
		return
	}
	s.driver = c
	r.addMember(c, s)
	c.sendACP(restored.Msg, "", 0)
}

// ---- notifications and reverse requests from the agent ----

func (r *router) onAgentNotification(m *rpcMessage) {
	params, err := m.params()
	if err != nil {
		return
	}
	switch m.method {
	case "session/update":
		s := r.sessions[stringParam(params, "sessionId")]
		if s == nil {
			r.log.Debug("session update for an unknown session")
			return
		}
		for c := range s.members {
			c.sendACP(m.raw, s.id, c.members[s.id].gen)
		}
	case protocol.ACPEvent, protocol.ACPResync:
		f := r.follows[stringParam(params, "subscriptionId")]
		if f == nil {
			return // the owner left; the event has no reader
		}
		c := f.owner
		c.sendACP(m.raw, f.session, c.members[f.session].gen)
	case "$/cancel_request":
		r.agentCancelled(params)
	default:
		r.log.Debug("agent notification dropped", zap.String("method", m.method))
	}
}

// quiesce closes the router to new requests, but only when nothing is open:
// no question waits for an answer and the host reports no work. Call it on the
// router goroutine, so no request is read between the check and the close.
func (r *router) quiesce() bool {
	if r.ids.hasPending() || len(r.internal) != 0 || r.openReverse() || r.cfg.QuiesceIfIdle == nil || !r.cfg.QuiesceIfIdle() {
		return false
	}
	r.draining = true
	return true
}

// status returns counts for the status control. Call it on the router goroutine.
func (r *router) status() protocol.LeaderStatus {
	st := protocol.LeaderStatus{
		InstanceID: r.cfg.Server.InstanceID, Build: r.cfg.Server.Build,
		ProtocolVersion: protocol.LeaderProtocolVersion, PID: r.cfg.PID,
		Clients: len(r.clients), Sessions: len(r.sessions), SpawnedByClient: r.cfg.SpawnedByClient,
	}
	if r.cfg.ActiveRuns != nil {
		st.ActiveRuns = r.cfg.ActiveRuns()
	}
	return st
}

func sortLive(rows []protocol.ACPLiveSession) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].SessionID < rows[j].SessionID })
}
