package agent

import (
	"context"
	"errors"
	"slices"

	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// Errors of Continue. Continue returns them before any event.
var (
	ErrContinueEmpty         = errors.New("agent: cannot continue: no messages in context")
	ErrContinueFromAssistant = errors.New("agent: cannot continue from message role: assistant")
	ErrNoStream              = errors.New("agent: loop config has no stream function")
)

// LoopConfig is the fixed configuration of one loop run.
type LoopConfig struct {
	Model  providers.Model
	Stream providers.StreamFn
	// Options go to every request. An empty Reasoning means "off". APIKey
	// is the fallback when Hooks.GetAPIKey returns an empty key.
	Options providers.StreamOptions
	// Cwd is passed to every tool call.
	Cwd   string
	Hooks pipeline.Hooks
}

// Emit receives every event of a run, in order, on the goroutine that called
// Run. An error ends the run with that error. The loop leaves the envelope
// empty.
type Emit func(protocol.Event) error

// Run adds prompts to ac and runs the loop until the model stops. It returns
// the messages the run added, prompts included.
//
// The loop catches no error of the hooks that shape a request or a turn (see
// pipeline.Hooks) and none of emit: Run returns it unchanged and emits
// nothing more, so the caller owns the failure message. Cancel ctx to abort.
func Run(ctx context.Context, prompts []protocol.Message, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) ([]protocol.Message, error) {
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	l := newLoop(ctx, ac, cfg, emit)
	l.ac.Messages = append(l.ac.Messages, prompts...)
	l.newMessages = append(l.newMessages, prompts...)
	if err := l.emitAll(&protocol.AgentStart{}, &protocol.TurnStart{}); err != nil {
		return nil, err
	}
	for _, m := range prompts {
		if err := l.emitMessage(m); err != nil {
			return nil, err
		}
	}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.newMessages, nil
}

// Continue runs the loop on ac without a new message, for example to retry.
// The last message must not be an assistant message. The result holds only
// the messages that this run added.
func Continue(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) ([]protocol.Message, error) {
	if len(ac.Messages) == 0 {
		return nil, ErrContinueEmpty
	}
	if ac.Messages[len(ac.Messages)-1].Role() == protocol.RoleAssistant {
		return nil, ErrContinueFromAssistant
	}
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	l := newLoop(ctx, ac, cfg, emit)
	if err := l.emitAll(&protocol.AgentStart{}, &protocol.TurnStart{}); err != nil {
		return nil, err
	}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.newMessages, nil
}

// loop is the state of one run. Only the goroutine that called Run touches it.
type loop struct {
	ctx         context.Context
	cfg         LoopConfig
	ac          pipeline.AgentContext
	newMessages []protocol.Message
	emit        Emit
	// pending holds the messages that the next turn adds before its request.
	pending []protocol.Message
	// turns counts completed turns. The first turn has its turn_start from
	// Run or Continue.
	turns int
	ts    turnState
}

func newLoop(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) *loop {
	// Clip so that appends never write into the caller's backing array.
	ac.Messages = slices.Clip(ac.Messages)
	return &loop{ctx: ctx, cfg: cfg, ac: ac, emit: emit}
}

// run is Pi's runLoop. A turn is the list of turnStages. The driver reads the
// flow of the last stage: flowNextTurn starts another turn at once (Pi's inner
// loop); flowIdle and flowIdleContinue check follow-up messages after the
// agent would stop (Pi's outer loop).
func (l *loop) run() error {
	var err error
	if l.pending, err = l.pollSteering(); err != nil {
		return err
	}
	for {
		f, err := l.runTurn()
		if err != nil {
			return err
		}
		switch f {
		case flowNextTurn:
			continue
		case flowEndRun:
			return l.endRun()
		}
		followUps, err := l.pollFollowUps()
		if err != nil {
			return err
		}
		if len(followUps) > 0 {
			l.pending = followUps
			continue
		}
		// No natural request was selected, so the continuation decision gets
		// one request with the current context.
		if f == flowIdleContinue {
			continue
		}
		return l.endRun()
	}
}

// pollSteering and pollFollowUps are the only readers of the queue hooks.
func (l *loop) pollSteering() ([]protocol.Message, error) {
	return l.poll(l.cfg.Hooks.GetSteeringMessages)
}

func (l *loop) pollFollowUps() ([]protocol.Message, error) {
	return l.poll(l.cfg.Hooks.GetFollowUpMessages)
}

func (l *loop) poll(hook func(context.Context) ([]protocol.Message, error)) ([]protocol.Message, error) {
	if hook == nil {
		return nil, nil
	}
	return hook(l.ctx)
}

func (l *loop) prepareRequest() error {
	hook := l.cfg.Hooks.PrepareRequest
	if hook == nil {
		return nil
	}
	upd, err := hook(l.ctx, pipeline.Request{
		Context:       l.context(),
		Model:         l.cfg.Model,
		ThinkingLevel: l.thinkingLevel(),
	})
	if err != nil || upd == nil {
		return err
	}
	if upd.Context != nil {
		l.ac = *upd.Context
		l.ac.Messages = slices.Clip(l.ac.Messages)
	}
	if upd.Model != nil {
		l.cfg.Model = *upd.Model
	}
	switch upd.ThinkingLevel {
	case "":
	case protocol.ThinkingOff:
		l.cfg.Options.Reasoning = ""
	default:
		l.cfg.Options.Reasoning = upd.ThinkingLevel
	}
	return nil
}

func (l *loop) turn(msg protocol.AssistantMessage, results []protocol.ToolResultMessage) *pipeline.Turn {
	return &pipeline.Turn{
		Message:     msg,
		ToolResults: results,
		Context:     l.context(),
		NewMessages: slices.Clip(l.newMessages),
	}
}

func (l *loop) finishTurn(t pipeline.Turn) (pipeline.TurnDecision, error) {
	hook := l.cfg.Hooks.FinishTurn
	if hook == nil {
		return pipeline.Proceed, nil
	}
	return hook(l.ctx, t)
}

// context is the view of the run context that hooks get. The clipped slice
// keeps a hook's append away from the loop's spare capacity.
func (l *loop) context() pipeline.AgentContext {
	return pipeline.AgentContext{Messages: slices.Clip(l.ac.Messages), Tools: l.ac.Tools}
}

func (l *loop) thinkingLevel() protocol.ThinkingLevel {
	if l.cfg.Options.Reasoning == "" {
		return protocol.ThinkingOff
	}
	return l.cfg.Options.Reasoning
}

func (l *loop) emitMessage(m protocol.Message) error {
	return l.emitAll(&protocol.MessageStart{Message: m}, &protocol.MessageEnd{Message: m})
}

func (l *loop) emitAll(events ...protocol.Event) error {
	for _, ev := range events {
		if err := l.emit(ev); err != nil {
			return err
		}
	}
	return nil
}

func toolCalls(msg protocol.AssistantMessage) []protocol.ToolCall {
	var calls []protocol.ToolCall
	for _, b := range msg.Content {
		if c, ok := b.(protocol.ToolCall); ok {
			calls = append(calls, c)
		}
	}
	return calls
}
