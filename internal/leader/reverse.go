package leader

import (
	"encoding/json"
	"strconv"
	"strings"

	"AskCore/pkg/protocol"

	"go.uber.org/zap"
)

// maxFinishedReverse is how many finished reverse requests the router remembers,
// so that a late or repeated answer is ignored. It does not limit new requests.
const maxFinishedReverse = 4096

// reverseReq is a request that the agent sent to clients. The agent id stays
// here. Each client that may answer gets an id of its own, made by the router and
// never used twice, so an answer cannot reach a later question.
type reverseReq struct {
	canon      string          // canonical agent id
	id         json.RawMessage // the agent id, as the agent wrote it
	method     string
	params     json.RawMessage
	session    string
	gen        uint64          // the driver generation when the agent asked
	options    map[string]bool // option ids of a permission request; nil for other requests
	recipients map[*client]string
	rids       []string // every id that this request handed out
	done       bool
}

// reverseRecipient is what a router-made id stands for.
type reverseRecipient struct {
	req *reverseReq
	c   *client
}

// terminalOwner is the client that made a terminal, and the generation it did
// it in. Later calls about that terminal go to this client and nowhere else.
type terminalOwner struct {
	c       *client
	gen     uint64
	session string
}

// onAgentRequest takes a call of the agent to a client.
//
//   - A permission request (a shared question) goes to every client of the
//     session. The first valid answer wins.
//   - A call about a terminal that exists goes to the client that made it.
//   - Every other call (files, a new terminal) goes to the driver, if its
//     capabilities allow the method.
//
// When nobody can answer, the agent gets a safe error at once. There is no
// fallback to another client.
func (r *router) onAgentRequest(m *rpcMessage) {
	canon, err := canonicalID(m.id)
	if err != nil {
		return
	}
	params, perr := m.params()
	s := r.sessions[stringParam(params, "sessionId")]
	refuse := func(kind protocol.ACPErrorKind) { r.agentQ.push(errorResponse(m.id, kind)) }
	if perr != nil || s == nil {
		refuse(protocol.ACPErrUnknownSession)
		return
	}
	if old := r.reverse[canon]; old != nil && !old.done {
		refuse(protocol.ACPErrInvalidParams) // the agent reused an id that is still open
		return
	}
	e := &reverseReq{
		canon: canon, id: m.id, method: m.method, params: m.fields["params"],
		session: s.id, gen: s.gen, recipients: map[*client]string{},
	}
	switch {
	case sharedInteractions[m.method]:
		if len(s.members) == 0 {
			refuse(protocol.ACPErrInvalidState) // nobody is there to answer
			return
		}
		e.options = permissionOptions(params)
		r.reverse[canon] = e
		for c := range s.members {
			r.addRecipient(e, s, c)
		}
		return
	case strings.HasPrefix(m.method, "terminal/") && m.method != "terminal/create":
		t := r.terminals[terminalKey(s.id, stringParam(params, "terminalId"))]
		if t == nil || t.session != s.id || t.gen != s.gen || t.c.closed || !clientAllows(t.c.caps, m.method) {
			refuse(protocol.ACPErrInvalidState)
			return
		}
		r.reverse[canon] = e
		r.addRecipient(e, s, t.c)
		return
	}
	d := s.driver
	if d == nil || !clientAllows(d.caps, m.method) {
		refuse(protocol.ACPErrInvalidState)
		return
	}
	r.reverse[canon] = e
	r.addRecipient(e, s, d)
}

// permissionOptions reads the option ids of a permission request.
func permissionOptions(params map[string]json.RawMessage) map[string]bool {
	var list []struct {
		ID string `json:"optionId"`
	}
	_ = json.Unmarshal(params["options"], &list)
	out := make(map[string]bool, len(list))
	for _, o := range list {
		out[o.ID] = true
	}
	return out
}

// addRecipient asks one more client. The client gets an id that only it can use.
func (r *router) addRecipient(e *reverseReq, s *session, c *client) {
	r.ridSeq++
	rid := "r" + strconv.FormatUint(r.ridSeq, 10)
	e.recipients[c] = rid
	e.rids = append(e.rids, rid)
	r.byRID[rid] = &reverseRecipient{req: e, c: c}
	c.sendACP(requestMessage(rid, e.method, e.params), s.id, c.members[s.id].gen)
}

// onClientResponse takes the answer of a client to a call of the agent. The
// answer counts only when the client got that id for that call, the call is
// still open, and the answer has the shape that the call allows. Anything else
// is ignored: a late, repeated, foreign or invalid answer never resolves a call.
func (r *router) onClientResponse(c *client, m *rpcMessage) {
	var rid string
	if json.Unmarshal(m.id, &rid) != nil {
		return
	}
	rec := r.byRID[rid]
	if rec == nil || rec.c != c || rec.req.done || rec.req.recipients[c] != rid {
		r.log.Debug("late, repeated or foreign answer ignored", zap.String("client", c.id))
		return
	}
	e := rec.req
	if !validAnswer(e, m) {
		r.log.Debug("invalid answer ignored", zap.String("client", c.id), zap.String("method", e.method))
		return
	}
	fields := make(map[string]json.RawMessage, len(m.fields))
	for k, v := range m.fields {
		fields[k] = v
	}
	fields["id"] = e.id
	r.agentQ.push(marshalPlain(fields))
	r.afterAnswer(e, c, m)
	r.finishReverse(e, c)
}

// validAnswer checks the shape of an answer. A permission answer selects one of
// the offered options, or says that it was cancelled. Any other call takes a
// result or an error.
func validAnswer(e *reverseReq, m *rpcMessage) bool {
	hasResult, hasError := m.fields["result"] != nil, m.fields["error"] != nil
	if hasResult == hasError {
		return false
	}
	if e.options == nil {
		return true
	}
	if !hasResult {
		return false
	}
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(m.fields["result"], &res) != nil {
		return false
	}
	switch res.Outcome.Outcome {
	case "cancelled":
		return true
	case "selected":
		return e.options[res.Outcome.OptionID]
	}
	return false
}

// afterAnswer keeps the terminals in step with the answers: a terminal belongs to
// the client that made it until the client releases it.
func (r *router) afterAnswer(e *reverseReq, c *client, m *rpcMessage) {
	switch e.method {
	case "terminal/create":
		var res struct {
			TerminalID string `json:"terminalId"`
		}
		if json.Unmarshal(m.fields["result"], &res) == nil && res.TerminalID != "" {
			key := terminalKey(e.session, res.TerminalID)
			if _, taken := r.terminals[key]; !taken {
				r.terminals[key] = &terminalOwner{c: c, gen: e.gen, session: e.session}
			}
		}
	case "terminal/release":
		if m.fields["error"] == nil {
			var p struct {
				TerminalID string `json:"terminalId"`
			}
			_ = json.Unmarshal(e.params, &p)
			delete(r.terminals, terminalKey(e.session, p.TerminalID))
		}
	}
}

// cancelReverse ends a question without an answer. The agent gets a cancelled error.
func (r *router) cancelReverse(e *reverseReq) {
	r.agentQ.push(errorResponse(e.id, protocol.ACPErrCancelled))
	r.finishReverse(e, nil)
}

// finishReverse marks a call as done and tells the other clients to drop it.
// Replay, cancel and driver loss all end here.
func (r *router) finishReverse(e *reverseReq, answered *client) {
	e.done = true
	for c, rid := range e.recipients {
		if c != answered && !c.closed {
			c.sendACP(notification("$/cancel_request", map[string]any{"requestId": rid}), "", 0)
		}
	}
	e.recipients = nil
	r.finished = append(r.finished, e)
	for len(r.finished) > maxFinishedReverse {
		old := r.finished[0]
		r.finished = r.finished[1:]
		if r.reverse[old.canon] == old {
			delete(r.reverse, old.canon)
		}
		for _, rid := range old.rids {
			delete(r.byRID, rid)
		}
	}
}

// agentCancelled handles the agent's own cancel of a call that it made.
func (r *router) agentCancelled(params map[string]json.RawMessage) {
	canon, err := canonicalID(params["requestId"])
	if err != nil {
		return
	}
	if e := r.reverse[canon]; e != nil && !e.done {
		r.finishReverse(e, nil)
	}
}

// terminalKey names a terminal by its session and the id that the client chose.
// The id alone is not enough: it is the choice of the answering client.
func terminalKey(session, id string) string { return session + "\x00" + id }

// dropRecipient takes a client out of the open calls of one session, as a
// detach does, or of all sessions when sessionID is empty, as a leave does. A
// call that has nobody left to answer it ends, so the agent never waits for a
// client that is not there.
func (r *router) dropRecipient(c *client, sessionID string) {
	var empty []*reverseReq
	for _, e := range r.reverse {
		if e.done || (sessionID != "" && e.session != sessionID) {
			continue
		}
		if _, had := e.recipients[c]; !had {
			continue
		}
		delete(e.recipients, c)
		if len(e.recipients) == 0 {
			empty = append(empty, e)
		}
	}
	for _, e := range empty {
		r.cancelReverse(e)
	}
}

// replayQuestions shows the open shared questions of a session to a client that
// joins late. It adds the client to the call, so it can answer it.
func (r *router) replayQuestions(c *client, s *session) {
	for _, e := range r.reverse {
		if e.done || e.session != s.id || e.options == nil {
			continue
		}
		if _, has := e.recipients[c]; !has {
			r.addRecipient(e, s, c)
		}
	}
}

// cancelQuestions ends the open calls of a driver generation. The run goes on.
// The terminals of that generation end with them.
func (r *router) cancelQuestions(s *session, gen uint64) {
	for _, e := range r.reverse {
		if !e.done && e.session == s.id && e.gen == gen {
			r.cancelReverse(e)
		}
	}
	for key, t := range r.terminals {
		if t.session == s.id && t.gen == gen {
			delete(r.terminals, key)
		}
	}
}

// forgetTerminals ends the terminals of a client that left.
func (r *router) forgetTerminals(c *client) {
	for key, t := range r.terminals {
		if t.c == c {
			delete(r.terminals, key)
		}
	}
}

// openReverse tells whether the agent waits for an answer from a client.
func (r *router) openReverse() bool {
	for _, e := range r.reverse {
		if !e.done {
			return true
		}
	}
	return false
}

// holdsReverse tells whether a client has an open call to answer.
func (r *router) holdsReverse(c *client) bool {
	for _, e := range r.reverse {
		if _, ok := e.recipients[c]; ok && !e.done {
			return true
		}
	}
	return false
}
