package agent

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// toolSlot belongs to the coordinator. Workers return their facts through done.
type toolSlot struct {
	preparedCall
	outcome   toolOutcome
	prepared  bool
	ready     bool
	after     bool
	exclusive bool
	update    *updater
}
type toolDone struct {
	index          int
	outcome        toolOutcome
	after, invoked bool
}

type toolCoordinator struct {
	*loop
	assistant      protocol.AssistantMessage
	view           pipeline.AgentContext
	assistantEntry int
	seen           map[string]bool
	notify         chan struct{}
}

func (l *loop) runToolBatch(assistant protocol.AssistantMessage, calls []protocol.ToolCall) (batch toolBatch, err error) {
	b := &toolCoordinator{loop: l, assistant: cloneAssistant(assistant), view: l.context(), assistantEntry: -1, seen: map[string]bool{}, notify: make(chan struct{}, 1)}
	entries := l.log.Entries()
	for i := len(entries) - 1; i >= 0; i-- {
		if m, ok := entries[i].(sessions.MessageEntry); ok {
			if _, ok := messageValue(m.Message).(protocol.AssistantMessage); ok {
				b.assistantEntry = i
				break
			}
		}
	}
	if l.usedToolIDs == nil {
		l.usedToolIDs = map[string]bool{}
	}
	limit := l.cfg.MaxParallelTools
	if limit <= 0 {
		limit = 10
	}
	slots := make([]toolSlot, len(calls))
	done := make(chan toolDone, limit)
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	active, next, committed := 0, 0, 0
	exclusive := false
	var added []protocol.UserMessage
	// A driver failure cancels and drains every worker before repair can start.
	defer func() {
		cancel()
		for active > 0 {
			<-done
			active--
		}
	}()
	accept := func(d toolDone) {
		s := &slots[d.index]
		s.outcome, s.after, s.ready = d.outcome, d.after, true
		active--
		batch.ran = batch.ran || d.invoked
		if s.exclusive {
			exclusive = false
		}
	}
	for committed < len(calls) {
		// Collect completions before deciding whether to start or commit a call.
		for {
			select {
			case d := <-done:
				accept(d)
			default:
				goto collected
			}
		}
	collected:
		for committed < next && slots[committed].ready {
			s := &slots[committed]
			if err = b.progress(slots); err != nil {
				return batch, err
			}
			if s.after {
				var contextMessages []protocol.UserMessage
				s.outcome, contextMessages = b.afterTool(s.preparedCall, s.outcome)
				added = append(added, contextMessages...)
			}
			if err = l.emitToolEnd(s.outcome); err != nil {
				return batch, err
			}
			msg := toolResultMessage(s.outcome)
			if err = l.emitMessage(msg); err != nil {
				return batch, err
			}
			batch.messages = append(batch.messages, msg)
			committed++
		}
		if committed == len(calls) {
			break
		}
		// A prepared exclusive call waits for all earlier bodies and their post-control.
		if next < len(calls) && !exclusive && active < limit {
			s := &slots[next]
			if !s.prepared {
				*s, err = b.prepare(calls[next])
				if err != nil {
					return batch, err
				}
			}
			if s.ready {
				next++
				continue
			}
			if l.ctx.Err() != nil {
				s.outcome = errorOutcome(s.call, textBeforeDispatch)
				s.ready, s.after = true, true
				next++
				continue
			}
			s.exclusive = !concurrencySafe(s.preparedCall)
			if !s.exclusive || (active == 0 && committed == next) {
				index := next
				next++
				active++
				exclusive = s.exclusive
				s.update = &updater{partial: make(chan protocol.ToolExecutionResult, 1), notify: b.notify, open: true}
				p, u := s.preparedCall, s.update
				go func() { o, after, invoked := b.execute(ctx, p, u); done <- toolDone{index, o, after, invoked} }()
				continue
			}
		}
		select {
		case d := <-done:
			accept(d)
		case <-b.notify:
			if err = b.progress(slots); err != nil {
				return batch, err
			}
		}
	}
	outcomes := make([]toolOutcome, len(slots))
	for i := range slots {
		outcomes[i] = slots[i].outcome
	}
	batch.terminate = shouldTerminate(outcomes)
	l.addToolContext(added)
	return batch, nil
}

func concurrencySafe(p preparedCall) (safe bool) {
	defer func() {
		if recover() != nil {
			safe = false
		}
	}()
	classifier, ok := p.tool.(tools.ConcurrencySafe)
	return ok && classifier.ConcurrencySafe(bytes.Clone(p.args))
}

func (b *toolCoordinator) prepare(c protocol.ToolCall) (s toolSlot, err error) {
	s.call = c
	s.prepared = true
	if err = b.commit(sessions.ToolCall{AssistantEntry: b.assistantEntry, CallID: c.ID}); err != nil {
		return s, err
	}
	if err = b.emitToolStart(c); err != nil {
		return s, err
	}
	immediate := func(o toolOutcome, after bool) { s.outcome, s.ready, s.after = o, true, after }
	if b.ctx.Err() != nil {
		immediate(errorOutcome(c, textBeforeDispatch), false)
		return s, nil
	}
	if b.seen[c.ID] {
		immediate(errorOutcome(c, fmt.Sprintf("Tool call id %q is used more than once in this message", c.ID)), false)
		return s, nil
	}
	b.seen[c.ID] = true
	if b.usedToolIDs[c.ID] {
		immediate(errorOutcome(c, fmt.Sprintf("Tool call id %q is already used in this session", c.ID)), false)
		return s, nil
	}
	b.usedToolIDs[c.ID] = true
	s.tool, _, _ = b.ts.tools.Lookup(c.Name)
	s.args = bytes.Clone(c.Arguments)
	if s.tool != nil {
		validation := toolGuarded(func() error {
			raw := bytes.Clone(c.Arguments)
			if ap, ok := s.tool.(tools.ArgumentPreparer); ok {
				var e error
				raw, e = ap.PrepareArguments(raw)
				if e != nil {
					return e
				}
			}
			var e error
			s.args, e = b.ts.tools.Prepare(c.Name, raw)
			s.args = bytes.Clone(s.args)
			return e
		})
		if validation != nil {
			immediate(errorOutcome(c, validation.Error()), false)
			return s, nil
		}
	}
	var result *pipeline.BeforeToolCallResult
	gateErr := toolGuarded(func() error {
		var e error
		result, e = b.cfg.Pipeline.BeforeTool(b.ctx, b.info(s.preparedCall))
		return e
	})
	if gateErr != nil {
		immediate(errorOutcome(c, gateErr.Error()), false)
		return s, nil
	}
	switch {
	case result != nil && result.Cancel:
		immediate(errorOutcome(c, textBeforeDispatch), true)
	case result != nil && result.Block:
		reason := result.Reason
		if reason == "" {
			reason = textBlocked
		}
		o := errorOutcome(c, reason)
		if result.Terminate {
			o.result.Terminate = ptr(true)
		}
		immediate(o, true)
	case b.ctx.Err() != nil:
		immediate(errorOutcome(c, textBeforeDispatch), true)
	}
	return s, nil
}

func (b *toolCoordinator) info(p preparedCall) pipeline.ToolCallInfo {
	view := b.view
	view.Messages = freezeMessages(view.Messages)
	return pipeline.ToolCallInfo{AssistantMessage: cloneAssistant(b.assistant), Call: p.handlerCall(), Args: bytes.Clone(p.args), Context: view}
}

// execute isolates body failures from errors of the ExecuteTool middleware.
func (b *toolCoordinator) execute(ctx context.Context, p preparedCall, u *updater) (out toolOutcome, after, invoked bool) {
	defer u.close()
	var res protocol.ToolExecutionResult
	executeErr := toolGuarded(func() error {
		var err error
		var once sync.Once
		var terminal protocol.ToolExecutionResult
		res, err = b.cfg.Pipeline.ExecuteTool(ctx, pipeline.ExecuteToolInput{Call: p.handlerCall(), Args: bytes.Clone(p.args), Context: tools.Context{CallID: p.call.ID, Cwd: b.cfg.Cwd, Update: u.send}}, func(nextCtx context.Context, in pipeline.ExecuteToolInput) (protocol.ToolExecutionResult, error) {
			once.Do(func() {
				if b.ctx.Err() != nil || nextCtx.Err() != nil {
					terminal = errorOutcome(p.call, textBeforeDispatch).result
					terminal.IsError = ptr(true)
					return
				}
				bodyCtx, cancel := context.WithCancelCause(nextCtx)
				stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
				defer stop()
				defer cancel(nil)
				if p.tool == nil {
					terminal = errorOutcome(p.call, fmt.Sprintf("Tool %s not found", p.call.Name)).result
					terminal.IsError = ptr(true)
					return
				}
				var body protocol.ToolExecutionResult
				bodyErr := toolGuarded(func() error {
					if bodyCtx.Err() != nil {
						return bodyCtx.Err()
					}
					tc := in.Context
					tc.CallID, tc.Cwd = p.call.ID, b.cfg.Cwd
					invoked = true
					var e error
					body, e = p.tool.Execute(bodyCtx, tc, bytes.Clone(p.args))
					return e
				})
				if bodyErr != nil {
					body = errorOutcome(p.call, bodyErr.Error()).result
					body.IsError = ptr(true)
				}
				terminal = cloneToolResult(body)
			})
			return cloneToolResult(terminal), nil
		})
		return err
	})
	if executeErr != nil {
		return errorOutcome(p.call, executeErr.Error()), false, invoked
	}
	out = toolOutcome{call: p.call, result: cloneToolResult(res), isError: res.IsError != nil && *res.IsError}
	if b.ctx.Err() != nil && !out.isError {
		out = errorOutcome(p.call, textAborted)
	}
	return out, true, invoked
}

func (b *toolCoordinator) afterTool(p preparedCall, o toolOutcome) (toolOutcome, []protocol.UserMessage) {
	var r *pipeline.AfterToolCallResult
	err := toolGuarded(func() error {
		var e error
		r, e = b.cfg.Pipeline.AfterTool(b.ctx, pipeline.ToolResultInfo{ToolCallInfo: b.info(p), Result: cloneToolResult(o.result), IsError: o.isError})
		return e
	})
	if err != nil {
		return errorOutcome(p.call, err.Error()), nil
	}
	o.result, o.isError = r.Apply(o.result, o.isError)
	if b.ctx.Err() != nil && !o.isError {
		o = errorOutcome(p.call, textAborted)
	}
	if r == nil {
		return o, nil
	}
	return o, r.Clone().AddedContext
}

func (b *toolCoordinator) progress(slots []toolSlot) error {
	for i := range slots {
		s := &slots[i]
		if s.update == nil {
			continue
		}
		select {
		case p := <-s.update.partial:
			if err := b.emit(&protocol.ToolExecutionUpdate{ToolCallID: s.call.ID, ToolName: s.call.Name, Args: bytes.Clone(s.call.Arguments), PartialResult: p}); err != nil {
				return err
			}
		default:
		}
	}
	return nil
}

// updater keeps only the newest progress value. A worker never waits for a listener.
type updater struct {
	mu      sync.Mutex
	partial chan protocol.ToolExecutionResult
	notify  chan struct{}
	open    bool
}

func (u *updater) send(p protocol.ToolExecutionResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.open {
		return
	}
	p = cloneToolResult(p)
	select {
	case <-u.partial:
	default:
	}
	u.partial <- p
	select {
	case u.notify <- struct{}{}:
	default:
	}
}
func (u *updater) close() { u.mu.Lock(); u.open = false; u.mu.Unlock() }

func cloneToolResult(r protocol.ToolExecutionResult) protocol.ToolExecutionResult {
	c := (&pipeline.AfterToolCallResult{Content: r.Content, Details: r.Details, StructuredContent: r.StructuredContent, IsError: r.IsError, Terminate: r.Terminate, Usage: r.Usage}).Clone()
	return protocol.ToolExecutionResult{Content: c.Content, Details: c.Details, StructuredContent: c.StructuredContent, IsError: c.IsError, Terminate: c.Terminate, Usage: c.Usage}
}

func toolGuarded(fn func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%s", panicText(v))
		}
	}()
	return fn()
}
