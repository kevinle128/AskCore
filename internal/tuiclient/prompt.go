package tuiclient

import (
	"context"
	"encoding/json"
	"errors"

	"AskCore/internal/tui"
	"AskCore/internal/tuiclient/rpc"
)

// rpcCaller is the part of rpc.Conn that follow needs.
type rpcCaller interface {
	CallCommit(ctx context.Context, method string, params any, commit func(json.RawMessage)) (json.RawMessage, error)
}

var _ rpcCaller = (*rpc.Conn)(nil)

// Prompt sends a prompt and returns at once. The outcome arrives as messages:
// PromptAcceptedMsg when the run starts, then PromptDoneMsg when it settles, or
// PromptSendFailedMsg or PromptRunErrorMsg when the call fails. The prompt is
// never sent again, also not after a reconnect.
func (c *Client) Prompt(text string) (tui.PromptID, error) {
	conn, sid, err := c.withSession()
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	if c.prompt != nil {
		c.mu.Unlock()
		return 0, tui.ErrPromptPending
	}
	c.nextPrompt++
	id := c.nextPrompt
	c.prompt = &promptState{id: id}
	c.mu.Unlock()
	c.goSafe("prompt", false, func() { c.runPrompt(conn, sid, id, text) })
	return id, nil
}

// runPrompt waits for the answer of the prompt call. The answer is the same
// error for a prompt that the agent refused and for a prompt whose run failed, so
// the acceptance event tells them apart: an error before acceptance is a failure
// to send, an error after it is a failure of the run.
func (c *Client) runPrompt(conn *rpc.Conn, sid string, id tui.PromptID, text string) {
	res, err := conn.Call(context.Background(), "session/prompt", map[string]any{
		"sessionId": sid,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	accepted := c.prompt != nil && c.prompt.id == id && c.prompt.accepted
	if c.prompt != nil && c.prompt.id == id {
		c.prompt = nil
	}
	if err != nil {
		unknown := rpcLost(err)
		switch {
		case accepted:
			c.events.push(tui.PromptRunErrorMsg{ID: id, Err: translate(err), OutcomeUnknown: unknown})
		default:
			c.events.push(tui.PromptSendFailedMsg{ID: id, Err: translate(err), OutcomeUnknown: unknown})
		}
		return
	}
	if !accepted {
		// An answer without an error means the run started and settled; the
		// follow stream may not be on this link.
		c.events.push(tui.PromptAcceptedMsg{ID: id})
	}
	var out struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(res, &out)
	c.events.push(tui.PromptDoneMsg{ID: id, StopReason: out.StopReason})
}

func rpcLost(err error) bool { return errors.Is(err, rpc.ErrConnectionLost) }
