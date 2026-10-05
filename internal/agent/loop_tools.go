package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"AskCore/internal/pipeline"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const (
	textAborted = "Operation aborted"
	textBlocked = "Tool execution was blocked"
)

// toolBatch is the outcome of all tool calls of one assistant message.
type toolBatch struct {
	messages  []protocol.ToolResultMessage
	terminate bool
}

// toolOutcome is the final result of one call after the hooks ran.
type toolOutcome struct {
	call    protocol.ToolCall
	result  protocol.ToolExecutionResult
	isError bool
}

// preparedCall passed preflight and is ready to execute with args.
type preparedCall struct {
	call protocol.ToolCall
	tool tools.Tool
	args json.RawMessage
}

// job is a prepared call at its source position in the batch.
type job struct {
	index int
	call  preparedCall
}

// toolSignal is what a tool goroutine sends to the loop goroutine, which
// emits every event. partial is set for an update of call, outcome otherwise.
type toolSignal struct {
	index   int
	call    protocol.ToolCall
	partial *protocol.ToolExecutionResult
	outcome toolOutcome
}

// toolExecutor runs all tool calls of one assistant message.
type toolExecutor interface {
	run(l *loop, assistant protocol.AssistantMessage, calls []protocol.ToolCall) (toolBatch, error)
}

// truncatedExecutor answers every call of a message that hit the output
// token limit with an error, because its arguments may be cut off. No hook runs.
type truncatedExecutor struct{}

func (truncatedExecutor) run(l *loop, _ protocol.AssistantMessage, calls []protocol.ToolCall) (toolBatch, error) {
	var batch toolBatch
	for _, c := range calls {
		if err := l.emitToolStart(c); err != nil {
			return toolBatch{}, err
		}
		o := errorOutcome(c, fmt.Sprintf(`Tool call "%s" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.`, c.Name))
		msg, err := l.emitToolEndAndResult(o)
		if err != nil {
			return toolBatch{}, err
		}
		batch.messages = append(batch.messages, msg)
	}
	return batch, nil
}

// sequentialExecutor runs the calls one after the other, for a batch with a
// tool that must not run beside another.
type sequentialExecutor struct{}

func (sequentialExecutor) run(l *loop, assistant protocol.AssistantMessage, calls []protocol.ToolCall) (toolBatch, error) {
	return newBatchRun(l, assistant).sequential(calls)
}

// parallelExecutor runs the prepared calls at the same time.
type parallelExecutor struct{}

func (parallelExecutor) run(l *loop, assistant protocol.AssistantMessage, calls []protocol.ToolCall) (toolBatch, error) {
	return newBatchRun(l, assistant).parallel(calls)
}

// toolExecutor picks the runner of a batch: a truncated message gets error
// results, a batch with a sequential tool runs in order, else all at once.
func (l *loop) toolExecutor(assistant protocol.AssistantMessage, calls []protocol.ToolCall) toolExecutor {
	if assistant.StopReason == protocol.StopLength {
		return truncatedExecutor{}
	}
	b := &batchRun{loop: l}
	for _, c := range calls {
		if t, ok := b.lookup(c.Name); ok {
			if s, ok := t.(tools.Sequential); ok && s.Sequential() {
				return sequentialExecutor{}
			}
		}
	}
	return parallelExecutor{}
}

// newBatchRun takes the context view when the batch starts, after the results
// of the earlier turn are in the context.
func newBatchRun(l *loop, assistant protocol.AssistantMessage) *batchRun {
	return &batchRun{loop: l, assistant: assistant, ctxView: l.context(), seen: map[string]bool{}}
}

// batchRun is one tool batch. The loop goroutine owns it; tool goroutines
// only read assistant and ctxView.
type batchRun struct {
	*loop
	assistant protocol.AssistantMessage
	ctxView   pipeline.AgentContext
	seen      map[string]bool
}

func (b *batchRun) sequential(calls []protocol.ToolCall) (toolBatch, error) {
	var outcomes []toolOutcome
	var batch toolBatch
	for _, c := range calls {
		if err := b.emitToolStart(c); err != nil {
			return toolBatch{}, err
		}
		p, o := b.prepare(c)
		if o == nil {
			done := make([]toolOutcome, 1)
			if err := b.runJobs([]job{{0, p}}, done, false); err != nil {
				return toolBatch{}, err
			}
			o = &done[0]
		} else if err := b.emitToolEnd(*o); err != nil {
			return toolBatch{}, err
		}
		msg := toolResultMessage(*o)
		if err := b.emitMessage(msg); err != nil {
			return toolBatch{}, err
		}
		outcomes = append(outcomes, *o)
		batch.messages = append(batch.messages, msg)
		if b.ctx.Err() != nil {
			break
		}
	}
	batch.terminate = shouldTerminate(outcomes)
	return batch, nil
}

// parallel preflights the calls in source order, then runs the prepared ones
// at once. tool_execution_end follows completion order; the result messages
// follow source order after the whole batch.
func (b *batchRun) parallel(calls []protocol.ToolCall) (toolBatch, error) {
	outcomes := make([]toolOutcome, 0, len(calls))
	var jobs []job
	for _, c := range calls {
		if err := b.emitToolStart(c); err != nil {
			return toolBatch{}, err
		}
		p, o := b.prepare(c)
		if o != nil {
			if err := b.emitToolEnd(*o); err != nil {
				return toolBatch{}, err
			}
			outcomes = append(outcomes, *o)
		} else {
			jobs = append(jobs, job{len(outcomes), p})
			outcomes = append(outcomes, toolOutcome{})
		}
		// Calls after an abort get no events and no result (D19).
		if b.ctx.Err() != nil {
			break
		}
	}
	if err := b.runJobs(jobs, outcomes, true); err != nil {
		return toolBatch{}, err
	}
	batch := toolBatch{terminate: shouldTerminate(outcomes)}
	for _, o := range outcomes {
		msg := toolResultMessage(o)
		if err := b.emitMessage(msg); err != nil {
			return toolBatch{}, err
		}
		batch.messages = append(batch.messages, msg)
	}
	return batch, nil
}

// runJobs runs each job in its own goroutine and stores its outcome in
// outcomes[job.index]. The loop goroutine emits the updates and the end
// events as they arrive. With abortedAtStart, a job that starts after the
// abort gets "Operation aborted" without executing. After an emit error the
// tools are cancelled and drained, so no goroutine outlives the call.
func (b *batchRun) runJobs(jobs []job, outcomes []toolOutcome, abortedAtStart bool) error {
	if len(jobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	signals := make(chan toolSignal)
	for _, j := range jobs {
		go func() {
			var o toolOutcome
			if abortedAtStart && b.ctx.Err() != nil {
				o = errorOutcome(j.call.call, textAborted)
			} else {
				o = b.execute(ctx, j, signals)
			}
			signals <- toolSignal{index: j.index, outcome: o}
		}()
	}
	var emitErr error
	for left := len(jobs); left > 0; {
		s := <-signals
		if emitErr != nil {
			if s.partial == nil {
				left--
			}
			continue
		}
		if s.partial != nil {
			emitErr = b.emit(&protocol.ToolExecutionUpdate{ToolCallID: s.call.ID, ToolName: s.call.Name, Args: s.call.Arguments, PartialResult: *s.partial})
		} else {
			left--
			outcomes[s.index] = s.outcome
			emitErr = b.emitToolEnd(s.outcome)
		}
		if emitErr != nil {
			cancel()
		}
	}
	return emitErr
}

// prepare is the preflight of one call: lookup, PrepareArguments,
// validation, BeforeToolCall. A non-nil outcome means the call does not
// execute.
func (b *batchRun) prepare(c protocol.ToolCall) (p preparedCall, immediate *toolOutcome) {
	if b.seen[c.ID] {
		return preparedCall{}, ptr(errorOutcome(c, fmt.Sprintf("Tool call id %q is used more than once in this message", c.ID)))
	}
	b.seen[c.ID] = true
	tool, ok := b.lookup(c.Name)
	if !ok {
		return preparedCall{}, ptr(errorOutcome(c, fmt.Sprintf("Tool %s not found", c.Name)))
	}
	defer func() {
		if v := recover(); v != nil {
			p, immediate = preparedCall{}, ptr(errorOutcome(c, panicText(v)))
		}
	}()

	raw := c.Arguments
	if ap, ok := tool.(tools.ArgumentPreparer); ok {
		var err error
		if raw, err = ap.PrepareArguments(raw); err != nil {
			return preparedCall{}, ptr(errorOutcome(c, err.Error()))
		}
	}
	args, err := b.ts.tools.Prepare(c.Name, raw)
	if err != nil {
		return preparedCall{}, ptr(errorOutcome(c, err.Error()))
	}
	args, blocked := b.beforeToolCall(c, args)
	if blocked != nil {
		return preparedCall{}, blocked
	}
	if b.ctx.Err() != nil {
		return preparedCall{}, ptr(errorOutcome(c, textAborted))
	}
	return preparedCall{call: c, tool: tool, args: args}, nil
}

// beforeToolCall is the only reader of the BeforeToolCall hook. A non-nil
// outcome means the call does not execute; otherwise the returned arguments
// are the ones to execute with. It runs inside prepare, so a panic of the
// hook becomes an error result.
func (b *batchRun) beforeToolCall(c protocol.ToolCall, args json.RawMessage) (json.RawMessage, *toolOutcome) {
	hook := b.cfg.Hooks.BeforeToolCall
	if hook == nil {
		return args, nil
	}
	r, err := hook(b.ctx, pipeline.ToolCallInfo{AssistantMessage: b.assistant, Call: c, Args: args, Context: b.ctxView})
	if err != nil {
		return nil, ptr(errorOutcome(c, err.Error()))
	}
	if b.ctx.Err() != nil {
		return nil, ptr(errorOutcome(c, textAborted))
	}
	if r != nil && r.Block {
		reason := r.Reason
		if reason == "" {
			reason = textBlocked
		}
		o := errorOutcome(c, reason)
		if r.Terminate {
			o.result.Terminate = ptr(true)
		}
		return nil, &o
	}
	if r != nil && r.Args != nil {
		args = r.Args
	}
	return args, nil
}

// execute runs one prepared call and its AfterToolCall hook on a tool
// goroutine. A panic in either becomes an error result.
func (b *batchRun) execute(ctx context.Context, j job, signals chan<- toolSignal) toolOutcome {
	p := j.call
	u := &updater{call: p.call, signals: signals, open: true}
	o := func() (o toolOutcome) {
		defer func() {
			if v := recover(); v != nil {
				o = errorOutcome(p.call, panicText(v))
			}
		}()
		res, err := p.tool.Execute(ctx, tools.Context{CallID: p.call.ID, Cwd: b.cfg.Cwd, Update: u.send}, p.args)
		if err != nil {
			return errorOutcome(p.call, err.Error())
		}
		return toolOutcome{call: p.call, result: res, isError: res.IsError != nil && *res.IsError}
	}()
	u.close()
	return b.afterToolCall(ctx, p, o)
}

func (b *batchRun) afterToolCall(ctx context.Context, p preparedCall, o toolOutcome) (out toolOutcome) {
	hook := b.cfg.Hooks.AfterToolCall
	if hook == nil {
		return o
	}
	defer func() {
		if v := recover(); v != nil {
			out = errorOutcome(p.call, panicText(v))
		}
	}()
	r, err := hook(ctx, pipeline.ToolResultInfo{
		ToolCallInfo: pipeline.ToolCallInfo{AssistantMessage: b.assistant, Call: p.call, Args: p.args, Context: b.ctxView},
		Result:       o.result,
		IsError:      o.isError,
	})
	if err != nil {
		return errorOutcome(p.call, err.Error())
	}
	o.result, o.isError = r.Apply(o.result, o.isError)
	return o
}

// updater is the Update callback of one Execute call. An update is either
// emitted before the call's tool_execution_end or dropped: close waits for an
// update in flight, and later updates are no-ops.
type updater struct {
	mu      sync.Mutex
	call    protocol.ToolCall
	signals chan<- toolSignal
	open    bool
}

func (u *updater) send(partial protocol.ToolExecutionResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.open {
		u.signals <- toolSignal{call: u.call, partial: &partial}
	}
}

func (u *updater) close() {
	u.mu.Lock()
	u.open = false
	u.mu.Unlock()
}

// lookup finds a tool in the snapshot of the turn, never in the live
// registry, so a call can only run a tool the model was told about.
func (b *batchRun) lookup(name string) (tools.Tool, bool) {
	t, _, ok := b.ts.tools.Lookup(name)
	return t, ok
}

func (l *loop) emitToolStart(c protocol.ToolCall) error {
	return l.emit(&protocol.ToolExecutionStart{ToolCallID: c.ID, ToolName: c.Name, Args: c.Arguments})
}

func (l *loop) emitToolEnd(o toolOutcome) error {
	return l.emit(&protocol.ToolExecutionEnd{ToolCallID: o.call.ID, ToolName: o.call.Name, Result: o.result, IsError: o.isError})
}

func (l *loop) emitToolEndAndResult(o toolOutcome) (protocol.ToolResultMessage, error) {
	if err := l.emitToolEnd(o); err != nil {
		return protocol.ToolResultMessage{}, err
	}
	msg := toolResultMessage(o)
	return msg, l.emitMessage(msg)
}

func toolResultMessage(o toolOutcome) protocol.ToolResultMessage {
	content := o.result.Content
	if content == nil {
		content = []protocol.UserBlock{}
	}
	return protocol.ToolResultMessage{
		ToolCallID: o.call.ID,
		ToolName:   o.call.Name,
		Content:    content,
		Details:    o.result.Details,
		Usage:      o.result.Usage,
		IsError:    o.isError,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func errorOutcome(c protocol.ToolCall, text string) toolOutcome {
	return toolOutcome{
		call: c,
		result: protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: text}},
			Details: json.RawMessage(`{}`),
		},
		isError: true,
	}
}

func shouldTerminate(outcomes []toolOutcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, o := range outcomes {
		if o.result.Terminate == nil || !*o.result.Terminate {
			return false
		}
	}
	return true
}

func panicText(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(v)
}

func ptr[T any](v T) *T { return &v }
