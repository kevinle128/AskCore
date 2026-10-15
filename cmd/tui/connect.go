package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"AskCore/internal/leader"
	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

const connectUsage = `ask connect - a line-oriented client of the local leader

Usage:
  ask connect [--cwd DIR | --attach SESSION]
  ask connect --help

Reaches the leader of this Ask home and starts it when none runs. It never falls
back to an agent of its own: when the leader cannot be reached it says why and
exits with 1. It then creates a session in DIR (default: the current directory),
or attaches to SESSION, and reads one line of stdin at a time:

  text                 send text as a prompt to the current session
  //text               send text that starts with a slash
  /new [DIR]           create a session and make it current
  /sessions            list the live sessions of the leader
  /attach ID           watch a session; the current session changes only if this works
  /detach              leave the current session; its run goes on
  /take                become the driver of the current session
  /cancel              cancel the run of the current session (driver only)
  /follow [EPOCH SEQ]  follow the events of the current session, from a cursor
  /unfollow            stop following
  /answer ID OPTION    answer an open question
  /quit                leave at once

The prompt does not block the input: /cancel, /take and /quit work while a run
is pending. At the end of stdin the client waits for the prompts in flight and
leaves. Leaving only detaches: the leader and its runs go on.
Output is one line for each event. The first word names the kind: connected,
session, live, update, result, error, follow, event, resync, question, detached, bye.
Exit codes: 0 left, 1 error or lost leader, 130 SIGINT, 143 SIGTERM, 129 SIGHUP.
`

type connectOptions struct {
	cwd    string
	attach string
	help   bool
}

func parseConnectArgs(argv []string) (connectOptions, error) {
	var o connectOptions
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; a {
		case "--help", "-h":
			o.help = true
		case "--cwd", "--attach":
			if i+1 >= len(argv) {
				return o, fmt.Errorf("%s needs a value; use ask connect --help", a)
			}
			i++
			if a == "--cwd" {
				o.cwd = argv[i]
			} else {
				o.attach = argv[i]
			}
		default:
			return o, fmt.Errorf("unknown argument %q; use ask connect --help", a)
		}
	}
	if o.cwd != "" && o.attach != "" {
		return o, errors.New("--cwd and --attach exclude each other")
	}
	return o, nil
}

// ---- input lines ----

type inputKind int

const (
	inputPrompt inputKind = iota + 1
	inputNew
	inputSessions
	inputAttach
	inputDetach
	inputTake
	inputCancel
	inputFollow
	inputUnfollow
	inputAnswer
	inputQuit
)

type inputCommand struct {
	kind inputKind
	text string
	args []string
}

var errBlankLine = errors.New("blank line")

// commandShapes gives each command and the number of arguments it takes.
var commandShapes = map[string]struct {
	kind inputKind
	min  int
	max  int
}{
	"new": {inputNew, 0, 1}, "sessions": {inputSessions, 0, 0}, "attach": {inputAttach, 1, 1},
	"detach": {inputDetach, 0, 0}, "take": {inputTake, 0, 0}, "cancel": {inputCancel, 0, 0},
	"follow": {inputFollow, 0, 2}, "unfollow": {inputUnfollow, 0, 0}, "answer": {inputAnswer, 2, 2},
	"quit": {inputQuit, 0, 0},
}

func parseInputLine(line string) (inputCommand, error) {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return inputCommand{}, errBlankLine
	case strings.HasPrefix(line, "//"):
		return inputCommand{kind: inputPrompt, text: line[1:]}, nil
	case !strings.HasPrefix(line, "/"):
		return inputCommand{kind: inputPrompt, text: line}, nil
	}
	fields := strings.Fields(line[1:])
	if len(fields) == 0 {
		return inputCommand{}, errors.New("empty command")
	}
	shape, ok := commandShapes[fields[0]]
	if !ok {
		return inputCommand{}, fmt.Errorf("unknown command /%s", fields[0])
	}
	args := fields[1:]
	if len(args) < shape.min || len(args) > shape.max {
		return inputCommand{}, fmt.Errorf("/%s takes %d to %d arguments", fields[0], shape.min, shape.max)
	}
	if shape.kind == inputFollow && len(args) == 1 {
		return inputCommand{}, errors.New("/follow takes an epoch and a sequence number, or nothing")
	}
	if shape.kind == inputFollow && len(args) == 2 {
		if _, err := strconv.ParseUint(args[1], 10, 64); err != nil {
			return inputCommand{}, errors.New("/follow: the sequence number is not a number")
		}
	}
	cmd := inputCommand{kind: shape.kind}
	if len(args) > 0 {
		cmd.args = args
	}
	return cmd, nil
}

// ---- the client ----

type fault struct {
	Code    int
	Kind    string
	Message string
}

type lineClient struct {
	conn *rpc.Conn
	out  *lockedWriter

	mu        sync.Mutex
	view      string
	subs      map[string]string // subscription id -> session
	questions map[string]string // raw id of a question -> session
	inflight  sync.WaitGroup
	done      <-chan struct{} // closed when the reader ended
	once      sync.Once
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) printf(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, format+"\n", a...)
}

func newLineClient(cl *leader.Client, out io.Writer) *lineClient {
	c := &lineClient{
		out:  &lockedWriter{w: out},
		subs: map[string]string{}, questions: map[string]string{},
	}
	c.conn = rpc.NewConn(rpc.NewLeaderTransport(cl), c)
	c.done = c.conn.Done()
	return c
}

// call sends a request and waits for its answer.
func (c *lineClient) call(method string, params any) (json.RawMessage, *fault) {
	return c.callForSession(method, params, "")
}

// callForSession is call for a follow request: the new subscription is known
// on the reader before it reads the next event or resync.
func (c *lineClient) callForSession(method string, params any, followSession string) (json.RawMessage, *fault) {
	var commit func(json.RawMessage)
	if followSession != "" {
		commit = func(result json.RawMessage) {
			var out protocol.ACPFollowResult
			if json.Unmarshal(result, &out) == nil && out.SubscriptionID != "" {
				c.mu.Lock()
				c.subs[out.SubscriptionID] = followSession
				c.mu.Unlock()
			}
		}
	}
	res, err := c.conn.CallCommit(context.Background(), method, params, commit)
	if err == nil {
		return res, nil
	}
	var f *rpc.Fault
	if errors.As(err, &f) {
		return nil, &fault{Code: f.Code, Kind: f.Kind, Message: f.Message}
	}
	return nil, &fault{Kind: "connection_lost", Message: "the leader closed the connection"}
}

func (c *lineClient) close() {
	c.once.Do(func() { _ = c.conn.Close() })
}

// readLoop is the only reader of the socket. It splits answers from the
// notifications and requests of the leader.
func (c *lineClient) readLoop() {
	c.conn.Run()
	var le *rpc.LeaderError
	if errors.As(c.conn.Err(), &le) {
		c.out.printf("error leader=%s", le.Kind)
	}
}

// Request handles a call of the agent. The client has no file or terminal
// capability, so the only request it takes is a question.
func (c *lineClient) Request(id json.RawMessage, method string, params json.RawMessage) {
	if method != "session/request_permission" {
		_ = c.conn.RespondError(id, -32601, "not supported")
		return
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(params, &p)
	c.mu.Lock()
	c.questions[string(id)] = p.SessionID
	c.mu.Unlock()
	c.out.printf("question id=%s session=%s", string(id), p.SessionID)
}

func (c *lineClient) Notification(method string, params json.RawMessage) {
	switch method {
	case "session/update":
		var p struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				Kind    string `json:"sessionUpdate"`
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(params, &p) == nil {
			c.out.printf("update session=%s kind=%s text=%s", p.SessionID, p.Update.Kind, strconv.Quote(p.Update.Content.Text))
		}
	case protocol.ACPEvent:
		var p protocol.ACPEventNotification
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(params, &p) != nil || !c.following(p.SubscriptionID) {
			return // an ended subscription has no reader
		}
		_ = json.Unmarshal(p.Event, &ev)
		c.out.printf("event session=%s seq=%d type=%s", p.SessionID, p.Seq, ev.Type)
	case protocol.ACPResync:
		var p protocol.ACPResyncNotification
		if json.Unmarshal(params, &p) != nil || !c.following(p.SubscriptionID) {
			return
		}
		c.out.printf("resync session=%s reason=%s epoch=%s seq=%d", p.SessionID, p.Reason, p.Cursor.Epoch, p.Cursor.Seq)
	case "$/cancel_request":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		c.mu.Lock()
		_, open := c.questions[string(p.RequestID)]
		delete(c.questions, string(p.RequestID))
		c.mu.Unlock()
		if open {
			c.out.printf("question-closed id=%s", string(p.RequestID))
		}
	}
}

func (c *lineClient) following(sub string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.subs[sub]
	return ok
}

func (c *lineClient) currentView() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view
}

func (c *lineClient) setView(sid string) {
	c.mu.Lock()
	c.view = sid
	c.mu.Unlock()
}

func (c *lineClient) printFault(op string, f *fault) {
	c.out.printf("error op=%s code=%d kind=%s", op, f.Code, f.Kind)
}

// ---- commands ----

func (c *lineClient) newSession(cwd string) bool {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		c.out.printf("error op=session/new kind=invalid_cwd")
		return false
	}
	res, f := c.call("session/new", map[string]any{"cwd": abs, "mcpServers": []any{}})
	if f != nil {
		c.printFault("session/new", f)
		return false
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(res, &out) != nil || out.SessionID == "" {
		c.out.printf("error op=session/new kind=bad_answer")
		return false
	}
	c.setView(out.SessionID)
	c.out.printf("session id=%s role=%s cwd=%s", out.SessionID, protocol.ACPRoleDriver, strconv.Quote(abs))
	return true
}

func (c *lineClient) attach(sid string) bool {
	res, f := c.call(protocol.ACPAttach, map[string]any{"sessionId": sid})
	if f != nil {
		c.printFault("attach", f) // the current session stays
		return false
	}
	var out protocol.ACPAttachResult
	if json.Unmarshal(res, &out) != nil || out.SessionID == "" {
		c.out.printf("error op=attach kind=bad_answer")
		return false
	}
	c.setView(out.SessionID)
	c.out.printf("session id=%s role=%s", out.SessionID, out.Role)
	return true
}

func (c *lineClient) sessions() {
	res, f := c.call(protocol.ACPListLive, map[string]any{})
	if f != nil {
		c.printFault("sessions", f)
		return
	}
	var out protocol.ACPListLiveResult
	if json.Unmarshal(res, &out) != nil {
		c.out.printf("error op=sessions kind=bad_answer")
		return
	}
	c.out.printf("sessions count=%d", len(out.Sessions))
	for _, s := range out.Sessions {
		role := s.Role
		if role == "" {
			role = "-"
		}
		c.out.printf("live id=%s role=%s driver=%s subscribers=%d cwd=%s", s.SessionID, role, yesNo(s.HasDriver), s.Subscribers, strconv.Quote(s.Cwd))
	}
}

func (c *lineClient) detach() {
	sid := c.currentView()
	if sid == "" {
		c.out.printf("error op=detach kind=no_current_session")
		return
	}
	if _, f := c.call(protocol.ACPDetach, map[string]any{"sessionId": sid}); f != nil {
		c.printFault("detach", f)
		return
	}
	c.mu.Lock()
	for sub, s := range c.subs {
		if s == sid {
			delete(c.subs, sub)
		}
	}
	c.view = ""
	c.mu.Unlock()
	c.out.printf("detached session=%s", sid)
}

func (c *lineClient) take() {
	sid := c.currentView()
	if sid == "" {
		c.out.printf("error op=take kind=no_current_session")
		return
	}
	res, f := c.call(protocol.ACPTake, map[string]any{"sessionId": sid})
	if f != nil {
		c.printFault("take", f)
		return
	}
	var out protocol.ACPTakeResult
	_ = json.Unmarshal(res, &out)
	c.out.printf("session id=%s role=%s generation=%d", sid, protocol.ACPRoleDriver, out.DriverGen)
}

func (c *lineClient) cancel() {
	sid := c.currentView()
	if sid == "" {
		c.out.printf("error op=cancel kind=no_current_session")
		return
	}
	if err := c.conn.Notify("session/cancel", map[string]any{"sessionId": sid}); err != nil {
		c.out.printf("error op=cancel kind=connection_lost")
		return
	}
	c.out.printf("cancel session=%s sent", sid)
}

func (c *lineClient) follow(args []string) {
	sid := c.currentView()
	if sid == "" {
		c.out.printf("error op=follow kind=no_current_session")
		return
	}
	params := map[string]any{"sessionId": sid}
	if len(args) == 2 {
		seq, _ := strconv.ParseUint(args[1], 10, 64)
		params["cursor"] = map[string]any{"epoch": args[0], "seq": strconv.FormatUint(seq, 10)}
	}
	res, f := c.callForSession(protocol.ACPFollow, params, sid)
	if f != nil {
		c.printFault("follow", f)
		return
	}
	var out protocol.ACPFollowResult
	if json.Unmarshal(res, &out) != nil {
		c.out.printf("error op=follow kind=bad_answer")
		return
	}
	c.out.printf("follow session=%s sub=%s epoch=%s seq=%d entries=%d resumed=%t resync=%t", sid, out.SubscriptionID, out.Cursor.Epoch, out.Cursor.Seq, len(out.Entries), out.Resumed, out.Resync)
}

func (c *lineClient) unfollow() {
	sid := c.currentView()
	c.mu.Lock()
	var sub string
	for s, v := range c.subs {
		if v == sid {
			sub = s
		}
	}
	c.mu.Unlock()
	if sub == "" {
		c.out.printf("error op=unfollow kind=not_following")
		return
	}
	if _, f := c.call(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": sub}); f != nil {
		c.printFault("unfollow", f)
		return
	}
	c.mu.Lock()
	delete(c.subs, sub)
	c.mu.Unlock()
	c.out.printf("unfollowed session=%s sub=%s", sid, sub)
}

func (c *lineClient) answerQuestion(id, option string) {
	c.mu.Lock()
	_, open := c.questions[id]
	delete(c.questions, id)
	c.mu.Unlock()
	if !open {
		c.out.printf("error op=answer kind=no_such_question")
		return
	}
	result := map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": option}}
	if err := c.conn.Respond(json.RawMessage(id), result); err != nil {
		c.out.printf("error op=answer kind=connection_lost")
		return
	}
	c.out.printf("answered id=%s option=%s", id, option)
}

// prompt runs in its own goroutine, so the input stays usable while it is pending.
// Its answer prints under the session that it was sent to, whatever the view is now.
func (c *lineClient) prompt(text string) {
	sid := c.currentView()
	if sid == "" {
		c.out.printf("error op=prompt kind=no_current_session")
		return
	}
	c.inflight.Add(1)
	go func() {
		defer c.inflight.Done()
		res, f := c.call("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}})
		if f != nil {
			c.out.printf("error session=%s op=prompt code=%d kind=%s", sid, f.Code, f.Kind)
			return
		}
		var out struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(res, &out)
		c.out.printf("result session=%s stop=%s", sid, out.StopReason)
	}()
}

// ---- the command ----

// runConnect implements "ask connect". callerExe is this ask binary; a leader
// that it has to start runs from it.
func runConnect(argv []string, stdin io.Reader, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal, callerExe string) int {
	o, err := parseConnectArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.help {
		_, _ = io.WriteString(stdout, connectUsage)
		return 0
	}
	paths, err := leader.ResolvePaths(deps.getenv("ASK_HOME"))
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	cl, err := leader.ConnectOrSpawn(context.Background(), leader.ConnectConfig{
		Paths:       paths,
		Hello:       leaderHello("connect"),
		CallerExe:   callerExe,
		CallerIsAsk: true,
		Warn:        func(s string) { report(stderr, s) },
	})
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	c := newLineClient(cl, stdout)
	go c.readLoop()
	defer c.close()

	if _, f := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]any{"name": "ask-connect", "version": buildIdentity()}}); f != nil {
		report(stderr, "Error: initialize:", f.Kind, f.Message)
		return 1
	}
	c.out.printf("connected instance=%s client=%s build=%s protocol=%d", cl.Info.InstanceID, cl.Info.ClientID, cl.Info.Build, cl.Info.ProtocolVersion)
	ok := false
	if o.attach != "" {
		ok = c.attach(o.attach)
	} else {
		cwd := o.cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		ok = c.newSession(cwd)
	}
	if !ok {
		return 1
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 0, 64<<10), protocol.LeaderMaxFrame)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-c.done:
				return
			}
		}
	}()
	for {
		select {
		case <-c.done:
			report(stderr, "ask connect: the leader closed the connection")
			return 1
		case sig := <-sigs:
			c.out.printf("bye")
			return signalExitCodes[sig]
		case line, open := <-lines:
			if !open {
				return c.finish(sigs)
			}
			cmd, perr := parseInputLine(line)
			if errors.Is(perr, errBlankLine) {
				continue
			}
			if perr != nil {
				c.out.printf("error input=%s msg=%s", strconv.Quote(line), strconv.Quote(perr.Error()))
				continue
			}
			if cmd.kind == inputQuit {
				c.out.printf("bye")
				return 0
			}
			c.run(cmd, o)
		}
	}
}

// finish waits for the prompts in flight after the end of stdin, then leaves.
func (c *lineClient) finish(sigs <-chan os.Signal) int {
	settled := make(chan struct{})
	go func() { c.inflight.Wait(); close(settled) }()
	select {
	case <-settled:
	case <-c.done:
	case sig := <-sigs:
		c.out.printf("bye")
		return signalExitCodes[sig]
	}
	c.out.printf("bye")
	return 0
}

func (c *lineClient) run(cmd inputCommand, o connectOptions) {
	switch cmd.kind {
	case inputPrompt:
		c.prompt(cmd.text)
	case inputNew:
		dir := o.cwd
		if len(cmd.args) > 0 {
			dir = cmd.args[0]
		}
		if dir == "" {
			dir, _ = os.Getwd()
		}
		c.newSession(dir)
	case inputSessions:
		c.sessions()
	case inputAttach:
		c.attach(cmd.args[0])
	case inputDetach:
		c.detach()
	case inputTake:
		c.take()
	case inputCancel:
		c.cancel()
	case inputFollow:
		c.follow(cmd.args)
	case inputUnfollow:
		c.unfollow()
	case inputAnswer:
		c.answerQuestion(cmd.args[0], cmd.args[1])
	}
}
