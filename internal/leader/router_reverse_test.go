package leader

import (
	"encoding/json"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// permissionRequest is a question of the agent with two options.
func permissionRequest(id any, sid string) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/request_permission",
		"params": map[string]any{
			"sessionId": sid, "toolCall": map[string]any{"toolCallId": "t1"},
			"options": []any{
				map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
			},
		}})
}

// answer selects an option. id is the id that the client was given.
func answer(id any, option string) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": option}}})
}

// agentAnswers returns what the agent got back for a reverse request, by the
// id that the agent used.
func (a *fakeAgent) answersFor(id string) []json.RawMessage {
	var out []json.RawMessage
	for _, raw := range a.answers() {
		if m, err := parseMessage(raw); err == nil && string(m.id) == id {
			out = append(out, raw)
		}
	}
	return out
}

func waitAnswers(t *testing.T, h *harness, id string, n int) {
	t.Helper()
	eventually(t, "answers for "+id, func() bool { return len(h.agent.answersFor(id)) >= n })
}

// theCall returns the one call of a method that a client got.
func theCall(t *testing.T, c *testClient, method string) *rpcMessage {
	t.Helper()
	return c.waitIncoming(method, 1)[0]
}

func TestRouterSharedQuestionFirstAnswerWins(t *testing.T) {
	h := startHarness(t)
	a, b, c := h.ready(nil), h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)

	h.agent.send(permissionRequest("q1", sid))
	reqA, reqB := theCall(t, a, "session/request_permission"), theCall(t, b, "session/request_permission")
	require.NotEqual(t, string(reqA.id), string(reqB.id), "every client gets an id of its own")
	require.NotEqual(t, `"q1"`, string(reqA.id), "the id of the agent stays inside the router")
	c.barrier()
	require.Empty(t, c.incoming("session/request_permission"), "a client outside the session is not asked")

	// An id works for the client that got it, and for no other client.
	b.sendRaw(answer(reqA.id, "allow"))
	b.barrier()
	require.Empty(t, h.agent.answersFor(`"q1"`), "B cannot use the id of A")

	b.sendRaw(answer(reqB.id, "allow"))
	waitAnswers(t, h, `"q1"`, 1)
	got := h.agent.answersFor(`"q1"`)[0]
	require.Contains(t, string(got), `"allow"`, "the answer reaches the agent under the agent's own id")

	cancel := theCall(t, a, "$/cancel_request")
	require.JSONEq(t, `{"requestId":`+string(reqA.id)+`}`, string(cancel.fields["params"]), "A is told to drop the question, with the id it knows")
	b.barrier()
	require.Empty(t, b.incoming("$/cancel_request"), "the winner is not told")

	a.sendRaw(answer(reqA.id, "reject")) // late
	c.sendRaw(answer(reqA.id, "reject"))
	a.barrier()
	c.barrier()
	require.Len(t, h.agent.answersFor(`"q1"`), 1, "late and duplicate answers are ignored")
}

func TestRouterAnswerWithAGuessedIDIsIgnored(t *testing.T) {
	h := startHarness(t)
	a, c := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest(7, sid))
	req := theCall(t, a, "session/request_permission")

	for _, guess := range []any{7, "7", "r1", "r2", "r999", "q7"} {
		c.sendRaw(answer(guess, "allow"))
	}
	c.barrier()
	require.Empty(t, h.agent.answersFor("7"), "a client that was not asked cannot answer")

	a.sendRaw(answer(req.id, "allow"))
	waitAnswers(t, h, "7", 1)
}

func TestRouterInvalidAnswerDoesNotResolve(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q1", sid))
	req := theCall(t, a, "session/request_permission")

	bad := map[string]json.RawMessage{
		"option that was not offered": answer(req.id, "delete-everything"),
		"unknown outcome":             marshalPlain(map[string]any{"jsonrpc": "2.0", "id": req.id, "result": map[string]any{"outcome": map[string]any{"outcome": "maybe"}}}),
		"no outcome":                  marshalPlain(map[string]any{"jsonrpc": "2.0", "id": req.id, "result": map[string]any{}}),
		"an error":                    json.RawMessage(`{"jsonrpc":"2.0","id":` + string(req.id) + `,"error":{"code":-32000,"message":"no"}}`),
		"result and error":            json.RawMessage(`{"jsonrpc":"2.0","id":` + string(req.id) + `,"result":{},"error":{"code":1,"message":"x"}}`),
		"nothing":                     json.RawMessage(`{"jsonrpc":"2.0","id":` + string(req.id) + `}`),
	}
	for name, raw := range bad {
		a.sendRaw(raw)
		a.barrier()
		require.Empty(t, h.agent.answersFor(`"q1"`), name)
	}

	a.sendRaw(answer(req.id, "reject"))
	waitAnswers(t, h, `"q1"`, 1)
}

func TestRouterCancelledOutcomeResolves(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q1", sid))
	req := theCall(t, a, "session/request_permission")
	a.sendRaw(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": req.id, "result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}}))
	waitAnswers(t, h, `"q1"`, 1)
}

func TestRouterReplaysPendingQuestionOnAttach(t *testing.T) {
	h := startHarness(t)
	a, c := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q9", sid))
	reqA := theCall(t, a, "session/request_permission")

	c.attach(sid)
	reqC := theCall(t, c, "session/request_permission")
	require.NotEqual(t, string(reqA.id), string(reqC.id), "a client that joins late sees the open question, under an id of its own")

	c.sendRaw(answer(reqC.id, "allow"))
	waitAnswers(t, h, `"q9"`, 1)
	theCall(t, a, "$/cancel_request")
}

func TestRouterEvictsResolvedQuestion(t *testing.T) {
	h := startHarness(t)
	a, c := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q2", sid))
	req := theCall(t, a, "session/request_permission")
	a.sendRaw(answer(req.id, "allow"))
	waitAnswers(t, h, `"q2"`, 1)

	c.attach(sid)
	c.barrier()
	require.Empty(t, c.incoming("session/request_permission"), "a resolved question is not replayed")
}

func TestRouterAReusedAgentIDIsAnotherQuestion(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q1", sid))
	first := theCall(t, a, "session/request_permission")
	a.sendRaw(answer(first.id, "allow"))
	waitAnswers(t, h, `"q1"`, 1)

	// The agent uses the same id for a new question. The old id of the client does not reach it.
	h.agent.send(permissionRequest("q1", sid))
	second := a.waitIncoming("session/request_permission", 2)[1]
	require.NotEqual(t, string(first.id), string(second.id))
	a.sendRaw(answer(first.id, "allow"))
	a.barrier()
	require.Len(t, h.agent.answersFor(`"q1"`), 1, "the answer to the first question does not answer the second")
	a.sendRaw(answer(second.id, "reject"))
	waitAnswers(t, h, `"q1"`, 2)
}

func TestRouterDriverLossCancelsQuestion(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q3", sid))
	theCall(t, a, "session/request_permission")
	reqB := theCall(t, b, "session/request_permission")

	a.close()
	a.waitClosed()
	waitAnswers(t, h, `"q3"`, 1)
	var m struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(h.agent.answersFor(`"q3"`)[0], &m))
	require.Equal(t, -32800, m.Error.Code, "the agent gets a cancelled error")
	theCall(t, b, "$/cancel_request")

	b.sendRaw(answer(reqB.id, "allow"))
	b.barrier()
	require.Len(t, h.agent.answersFor(`"q3"`), 1, "no answer is taken after the cancel")
	require.Empty(t, h.agent.requests("session/cancel"), "the run is not aborted")
}

func TestRouterDriverDetachCancelsQuestion(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q4", sid))
	theCall(t, b, "session/request_permission")
	a.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	waitAnswers(t, h, `"q4"`, 1)
}

func TestRouterDetachedClientCannotAnswer(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q5", sid))
	reqB := theCall(t, b, "session/request_permission")
	theCall(t, a, "session/request_permission")
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	b.sendRaw(answer(reqB.id, "allow"))
	b.barrier()
	require.Empty(t, h.agent.answersFor(`"q5"`), "a client that left the session left the question")
}

func TestRouterTakeCancelsQuestionOfFormerDriver(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q5", sid))
	theCall(t, a, "session/request_permission")
	b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid})
	waitAnswers(t, h, `"q5"`, 1)
	h.agent.send(permissionRequest("q6", sid))
	reqB := b.waitIncoming("session/request_permission", 2)[1]
	a.waitIncoming("session/request_permission", 2)
	b.sendRaw(answer(reqB.id, "allow"))
	waitAnswers(t, h, `"q6"`, 1)
}

func TestRouterAgentCancelRequestFannedOutWithEachID(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q7", sid))
	reqA, reqB := theCall(t, a, "session/request_permission"), theCall(t, b, "session/request_permission")

	h.agent.send(notification("$/cancel_request", map[string]any{"requestId": "q7"}))
	cancelA, cancelB := theCall(t, a, "$/cancel_request"), theCall(t, b, "$/cancel_request")
	require.JSONEq(t, `{"requestId":`+string(reqA.id)+`}`, string(cancelA.fields["params"]))
	require.JSONEq(t, `{"requestId":`+string(reqB.id)+`}`, string(cancelB.fields["params"]))
	a.sendRaw(answer(reqA.id, "allow"))
	a.barrier()
	require.Empty(t, h.agent.answersFor(`"q7"`), "an answer after the agent cancelled is ignored")
}

func TestRouterDriverOnlyReverseRequest(t *testing.T) {
	h := startHarness(t)
	a := h.ready(map[string]any{"fs": map[string]any{"readTextFile": true}})
	b := h.ready(map[string]any{"fs": map[string]any{"readTextFile": true, "writeTextFile": true}, "terminal": true})
	sid := a.newSession("/w")
	b.attach(sid)

	read := marshalPlain(map[string]any{"jsonrpc": "2.0", "id": "f1", "method": "fs/read_text_file", "params": map[string]any{"sessionId": sid, "path": "/w/x"}})
	h.agent.send(read)
	reqA := theCall(t, a, "fs/read_text_file")
	b.barrier()
	require.Empty(t, b.incoming("fs/read_text_file"), "only the driver is asked")

	b.sendRaw(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": reqA.id, "result": map[string]any{"content": "stolen"}})) // an observer with the id of the driver
	b.barrier()
	require.Empty(t, h.agent.answersFor(`"f1"`))
	a.sendRaw(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": reqA.id, "result": map[string]any{"content": "data"}}))
	waitAnswers(t, h, `"f1"`, 1)
	require.Contains(t, string(h.agent.answersFor(`"f1"`)[0]), "data")

	// The driver lacks the write capability: the agent gets an error and nobody else is asked.
	write := marshalPlain(map[string]any{"jsonrpc": "2.0", "id": "f2", "method": "fs/write_text_file", "params": map[string]any{"sessionId": sid, "path": "/w/x", "content": "c"}})
	h.agent.send(write)
	waitAnswers(t, h, `"f2"`, 1)
	var m struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(h.agent.answersFor(`"f2"`)[0], &m))
	require.Equal(t, protocol.ACPCodeInvalidState, m.Error.Code)
	b.barrier()
	require.Empty(t, b.incoming("fs/write_text_file"), "there is no fallback to another client")
}

func TestRouterReverseRequestWithoutDriver(t *testing.T) {
	h := startHarness(t)
	a := h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/w")
	a.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	h.agent.send(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": "t1", "method": "terminal/create", "params": map[string]any{"sessionId": sid, "command": "ls"}}))
	waitAnswers(t, h, `"t1"`, 1)
	h.agent.send(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": "u1", "method": "x/unknown", "params": map[string]any{"sessionId": sid}}))
	waitAnswers(t, h, `"u1"`, 1)
	h.agent.send(permissionRequest("p1", "ghost"))
	waitAnswers(t, h, `"p1"`, 1)
}

// ---- terminals ----

func terminalCall(id, method, sid string, extra map[string]any) json.RawMessage {
	params := map[string]any{"sessionId": sid}
	for k, v := range extra {
		params[k] = v
	}
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func okResult(id json.RawMessage, result map[string]any) json.RawMessage {
	return marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func TestRouterTerminalBelongsToItsCreator(t *testing.T) {
	h := startHarness(t)
	caps := map[string]any{"terminal": true}
	a, b := h.ready(caps), h.ready(caps)
	sid := a.newSession("/w")
	b.attach(sid)

	h.agent.send(terminalCall("c1", "terminal/create", sid, map[string]any{"command": "ls"}))
	create := theCall(t, a, "terminal/create")
	a.sendRaw(okResult(create.id, map[string]any{"terminalId": "T1"}))
	waitAnswers(t, h, `"c1"`, 1)

	h.agent.send(terminalCall("o1", "terminal/output", sid, map[string]any{"terminalId": "T1"}))
	output := theCall(t, a, "terminal/output")
	b.barrier()
	require.Empty(t, b.incoming("terminal/output"), "a call about a terminal goes to its creator")
	a.sendRaw(okResult(output.id, map[string]any{"output": "x", "truncated": false}))
	waitAnswers(t, h, `"o1"`, 1)

	// A new driver cannot use or release the terminal of the old one, and the old one does not reach it either.
	b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid})
	h.agent.send(terminalCall("o2", "terminal/output", sid, map[string]any{"terminalId": "T1"}))
	h.agent.send(terminalCall("r1", "terminal/release", sid, map[string]any{"terminalId": "T1"}))
	waitAnswers(t, h, `"o2"`, 1)
	waitAnswers(t, h, `"r1"`, 1)
	require.Contains(t, string(h.agent.answersFor(`"o2"`)[0]), `"error"`, "the agent gets a safe error")
	b.barrier()
	a.barrier()
	require.Len(t, b.incoming("terminal/output"), 0)
	require.Len(t, b.incoming("terminal/release"), 0)
	require.Len(t, a.incoming("terminal/output"), 1, "the former driver was not asked again")
}

func TestRouterUnknownTerminalIsRefused(t *testing.T) {
	h := startHarness(t)
	a := h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/w")
	h.agent.send(terminalCall("o1", "terminal/output", sid, map[string]any{"terminalId": "nope"}))
	waitAnswers(t, h, `"o1"`, 1)
	a.barrier()
	require.Empty(t, a.incoming("terminal/output"))
}

func TestRouterReleasedTerminalIsGone(t *testing.T) {
	h := startHarness(t)
	a := h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/w")
	h.agent.send(terminalCall("c1", "terminal/create", sid, nil))
	create := theCall(t, a, "terminal/create")
	a.sendRaw(okResult(create.id, map[string]any{"terminalId": "T9"}))
	waitAnswers(t, h, `"c1"`, 1)

	h.agent.send(terminalCall("r1", "terminal/release", sid, map[string]any{"terminalId": "T9"}))
	release := theCall(t, a, "terminal/release")
	a.sendRaw(okResult(release.id, map[string]any{}))
	waitAnswers(t, h, `"r1"`, 1)

	h.agent.send(terminalCall("o1", "terminal/output", sid, map[string]any{"terminalId": "T9"}))
	waitAnswers(t, h, `"o1"`, 1)
	require.Contains(t, string(h.agent.answersFor(`"o1"`)[0]), `"error"`)
}

func TestRouterDriverLossEndsTerminalsWithoutAbortingTheRun(t *testing.T) {
	h := startHarness(t)
	caps := map[string]any{"terminal": true}
	a, b := h.ready(caps), h.ready(caps)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(terminalCall("c1", "terminal/create", sid, nil))
	create := theCall(t, a, "terminal/create")
	a.sendRaw(okResult(create.id, map[string]any{"terminalId": "T2"}))
	waitAnswers(t, h, `"c1"`, 1)

	a.close()
	a.waitClosed()
	eventually(t, "driver gone", func() bool { return h.clientCount() == 1 })
	h.agent.send(terminalCall("o1", "terminal/output", sid, map[string]any{"terminalId": "T2"}))
	waitAnswers(t, h, `"o1"`, 1)
	require.Contains(t, string(h.agent.answersFor(`"o1"`)[0]), `"error"`)
	b.barrier()
	require.Empty(t, b.incoming("terminal/output"), "nobody inherits the terminal")
	require.Empty(t, h.agent.requests("session/cancel"), "the run is not aborted")
}

// A detach from one session must not take a client out of the open questions of
// its other sessions.
func TestRouterDetachFromOneSessionKeepsTheQuestionsOfAnother(t *testing.T) {
	h := startHarness(t)
	x := h.ready(map[string]any{"fs": map[string]any{"readTextFile": true}})
	sa, sb := x.newSession("/a"), x.newSession("/b")
	h.agent.send(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": "f1", "method": "fs/read_text_file", "params": map[string]any{"sessionId": sb, "path": "/b/x"}}))
	req := theCall(t, x, "fs/read_text_file")

	x.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sa})
	x.sendRaw(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": req.id, "result": map[string]any{"content": "data"}}))
	waitAnswers(t, h, `"f1"`, 1)
	require.Contains(t, string(h.agent.answersFor(`"f1"`)[0]), "data", "the answer for the other session still counts")
}

// A question that nobody can answer ends at once, and one whose last recipient
// leaves ends then. The agent never waits for ever, and the idle check can pass.
func TestRouterQuestionWithoutRecipientsEnds(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	a.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid}) // B alone, with no driver

	h.agent.send(permissionRequest("q1", sid))
	req := theCall(t, b, "session/request_permission")
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	waitAnswers(t, h, `"q1"`, 1)
	var m struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(h.agent.answersFor(`"q1"`)[0], &m))
	require.Equal(t, -32800, m.Error.Code, "the last recipient left: the question is cancelled")
	_ = req

	// The session has no member now: a new question is refused at once.
	h.agent.send(permissionRequest("q2", sid))
	waitAnswers(t, h, `"q2"`, 1)
	require.NoError(t, json.Unmarshal(h.agent.answersFor(`"q2"`)[0], &m))
	require.Equal(t, protocol.ACPCodeInvalidState, m.Error.Code)
	var open bool
	h.srv.r.call(func() { open = h.srv.r.openReverse() })
	require.False(t, open, "no question stays open for nobody")
}

// A terminal id is the choice of the client that answers, so it must not be able
// to take over the terminal of another session.
func TestRouterTerminalIDOfAnotherSessionIsNotTaken(t *testing.T) {
	h := startHarness(t)
	caps := map[string]any{"terminal": true}
	a, b := h.ready(caps), h.ready(caps)
	sa, sb := a.newSession("/a"), b.newSession("/b")
	h.agent.send(terminalCall("c1", "terminal/create", sa, nil))
	createA := theCall(t, a, "terminal/create")
	a.sendRaw(okResult(createA.id, map[string]any{"terminalId": "T1"}))
	waitAnswers(t, h, `"c1"`, 1)

	h.agent.send(terminalCall("c2", "terminal/create", sb, nil))
	createB := theCall(t, b, "terminal/create")
	b.sendRaw(okResult(createB.id, map[string]any{"terminalId": "T1"})) // the same id, another session
	waitAnswers(t, h, `"c2"`, 1)

	h.agent.send(terminalCall("o1", "terminal/output", sa, map[string]any{"terminalId": "T1"}))
	theCall(t, a, "terminal/output")
	b.barrier()
	require.Empty(t, b.incoming("terminal/output"), "the terminal of A stays with A")
}
