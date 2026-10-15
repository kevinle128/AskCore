package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// maxEmptyContinuations is how many times in a row a Continue decision may
// start a turn that has no new input and no tool work. The next one ends the
// cycle: a handler that always answers Continue would loop for ever.
const maxEmptyContinuations = 3

// Errors of Continue. Continue returns them before any event.
var (
	ErrContinueEmpty         = errors.New("agent: cannot continue: no messages in context")
	ErrContinueFromAssistant = errors.New("agent: cannot continue from message role: assistant")
	ErrNoStream              = errors.New("agent: loop config has no stream function")
)

// LoopConfig is the fixed configuration of one loop run.
type LoopConfig struct {
	// MaxParallelTools bounds concurrent tool bodies. Zero uses 10.
	MaxParallelTools int
	Wait             func(context.Context, time.Duration) error
	Model            providers.Model
	Stream           providers.StreamFn
	// Prepare computes the effective values of a request, such as the output
	// limit after the clamp. The loop calls it for each attempt, before it logs
	// the request, and the stream gets the result in StreamOptions.Prepared. New
	// sets it to Registry.Prepare when it is nil and Registry is set. Nil means
	// no effective values: the request log then holds the options only.
	Prepare func(m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) (*providers.Prepared, error)
	// Ready checks the same binding rules as Stream before an idle model switch.
	Ready func(context.Context, providers.Model, string) error
	// Options go to every request. An empty Reasoning means "off". APIKey
	// is the fallback when BoundKey is empty and GetAPIKey is nil or returns
	// an empty key.
	Options providers.StreamOptions
	// BoundKey is the --api-key pin. ResolveKey applies it only when
	// Provider matches, so the secret never goes to another host.
	BoundKey providers.BoundKey
	// Cwd is passed to every tool call.
	Cwd string
	// Pipeline holds the handlers of the control points. Nil means no handlers.
	Pipeline *pipeline.Registry
	// GetAPIKey resolves the key before each request. An empty key falls back
	// to the configured one. Nil means no resolver.
	GetAPIKey func(ctx context.Context, provider string) (string, error)
	// ConvertToLLM maps the log to model messages. Nil means
	// providers.ConvertToLLM.
	ConvertToLLM func(msgs []protocol.Message) ([]protocol.Message, error)
}

// Emit receives every event of a run, in order, on the goroutine that called
// Run. An error ends the run with that error. The loop leaves the envelope
// empty.
type Emit func(protocol.Event) error

// driver is what the loop needs from the Agent beyond its config: the log
// that it commits to, the leading system messages of every request, and the
// queues. The log is the sole record of history. The loop commits a message
// there first and publishes its event after.
type driver struct {
	onPreparationFailure func()
	clock                func() time.Time
	log                  sessions.Writer
	header               []protocol.Message
	// in is the source of queued input. Nil means a loop with no queues.
	in inputSource
}

// inputSource is the claim side of the queues of an Agent. A claim removes the
// messages from the queue and gives them to the loop. Both claims return
// nothing once the run is cancelled.
type inputSource interface {
	// claimSteering takes all steering messages.
	claimSteering() []input
	// claimCycle takes all steering messages and one follow-up message. When
	// it finds nothing and ending is set, the run is closing: input that
	// arrives later waits for the next run.
	claimCycle(ending bool) []input
	// nextID returns an ID for an input that did not come through a queue.
	nextID() string
}

// runLoop adds prompts to ac and runs the loop until the model stops. The first
// cycle takes the prompts and the queued steering messages. It returns the
// messages the run added, prompts included.
//
// The loop catches no error of the points that shape a request or a turn
// (AdmitStep, PrepareRequest, ExecuteModel, CompleteStep), of GetAPIKey,
// ConvertToLLM or the log, and none of emit: runLoop returns it unchanged and
// emits nothing more, so the caller owns the failure message. Cancel ctx to
// abort.
func runLoop(ctx context.Context, prompts []protocol.Message, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) ([]protocol.Message, error) {
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	l := newLoop(ctx, ac, cfg, d, emit)
	if err := l.start(l.promptInputs(prompts), claimSteering); err != nil {
		return nil, err
	}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.newMessages, nil
}

// continueLoop runs the loop on ac without a new message, for example to
// retry. The last message must not be an assistant message. The first cycle
// takes the queued steering messages, and its first turn runs when there are
// none. The result holds only the messages that this run added.
func continueLoop(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) ([]protocol.Message, error) {
	if len(ac.Messages) == 0 {
		return nil, ErrContinueEmpty
	}
	if ac.Messages[len(ac.Messages)-1].Role() == protocol.RoleAssistant {
		return nil, ErrContinueFromAssistant
	}
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	l := newLoop(ctx, ac, cfg, d, emit)
	l.allowEmpty = true
	if err := l.start(nil, claimSteering); err != nil {
		return nil, err
	}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.newMessages, nil
}

// runQueued runs the loop for input that is already queued: the first cycle
// takes all steering messages and one follow-up message. When the messages are
// gone by then, the cycle ends with no turn.
func runQueued(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) ([]protocol.Message, error) {
	if cfg.Stream == nil {
		return nil, ErrNoStream
	}
	l := newLoop(ctx, ac, cfg, d, emit)
	if err := l.start(nil, claimCycle); err != nil {
		return nil, err
	}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.newMessages, nil
}

// loop is the state of one run. Only the goroutine that called Run touches it.
type loop struct {
	usedToolIDs map[string]bool
	toolContext []input
	clock       func() time.Time
	ctx         context.Context
	cfg         LoopConfig
	log         sessions.Writer
	header      []protocol.Message
	ac          pipeline.AgentContext
	newMessages []protocol.Message
	emit        Emit
	// pending holds the claimed input that the next turn admits and adds
	// before its request.
	pending []input
	// in is the queue source, nil when the loop has none.
	in inputSource
	// allowEmpty lets the first turn of the run start with no input. Only
	// continueLoop sets it; the first admission clears it.
	allowEmpty bool
	// requestMessages replaces the context messages of the next request only.
	// It is set by prepareRequest and used by streamAssistantResponse.
	requestMessages []protocol.Message
	// turns counts completed turns. The first turn has its turn_start from
	// Run or Continue.
	turns int
	// emptyContinues counts the consecutive Continue decisions that started a
	// turn with no new input and no tool work.
	emptyContinues int
	ts             turnState
	// cycle is the open input cycle and attempt the open model request. Each
	// is nil when none is open.
	cycle   *cycleState
	attempt *attemptState
	// failureCode is the failure code of the last model answer, empty when
	// that answer did not end in an error.
	failureCode          string
	staged               []protocol.Message
	stagedIDs            []string
	stagedInputCount     int
	claimed              []input
	onPreparationFailure func()
	retryNumber          int
	retryBudgets         map[string]retryBudget
	lastSettlement       sessions.AttemptSettled
	retryMessage         protocol.AssistantMessage
	retry                *sessions.RetryScheduled
	retryPending         bool
	binding              *providers.AuthBinding
}

func newLoop(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, d driver, emit Emit) *loop {
	// Clip so that appends never write into the caller's backing array.
	ac.Messages = slices.Clip(ac.Messages)
	used := map[string]bool{}
	for _, m := range ac.Messages {
		if a, ok := messageValue(m).(protocol.AssistantMessage); ok {
			for _, c := range toolCalls(a) {
				used[c.ID] = true
			}
		}
	}
	clock := d.clock
	if clock == nil {
		clock = time.Now
	}
	return &loop{usedToolIDs: used, clock: clock, ctx: ctx, cfg: cfg, log: d.log, header: d.header, in: d.in, onPreparationFailure: d.onPreparationFailure, ac: ac, emit: emit}
}

// commit writes entries to the log in one step. The driver calls it before it
// publishes the event of what it wrote. A failed write ends the run: the loop
// starts no later side effect, and the error reaches the caller of the run.
func (l *loop) commit(entries ...sessions.Entry) error {
	if _, err := l.log.Append(entries...); err != nil {
		return fmt.Errorf("agent: write session log: %w", err)
	}
	return nil
}

// claimKind says how the first cycle of a run takes input from the queues.
type claimKind uint8

const (
	// claimSteering takes the steering messages after the input of the call.
	claimSteering claimKind = iota
	// claimCycle takes steering and one follow-up message.
	claimCycle
)

// start publishes agent_start, opens the first cycle and claims its input: first
// the messages of the call, then what the queues give. The first turn opens
// after its admission.
func (l *loop) start(first []input, kind claimKind) error {
	if err := l.emit(&protocol.AgentStart{}); err != nil {
		return err
	}
	if err := l.openCycle(); err != nil {
		return err
	}
	l.pending = first
	if kind == claimCycle {
		l.pending = append(l.pending, l.takeCycle(false)...)
	} else {
		l.pending = append(l.pending, l.takeSteering()...)
	}
	return nil
}

// promptInputs gives each prompt of the call an ID.
func (l *loop) promptInputs(msgs []protocol.Message) []input {
	out := make([]input, len(msgs))
	for i, m := range msgs {
		id := newRunID()
		if l.in != nil {
			id = l.in.nextID()
		}
		out[i] = input{id: id, msg: m}
	}
	return out
}

// takeSteering and takeCycle are the only callers of the queue claims. A loop
// without queues, and a run that was cancelled, claim nothing.
func (l *loop) takeSteering() []input {
	if l.ctx.Err() != nil {
		return nil
	}
	got := l.toolContext
	l.toolContext = nil
	if l.in != nil {
		got = append(got, l.in.claimSteering()...)
	}
	return got
}

// takeCycle claims the input of the next cycle. When ending is set and nothing
// is queued, the run ends: the claim marks it, in the same critical section.
func (l *loop) takeCycle(ending bool) []input {
	if l.in == nil || l.ctx.Err() != nil {
		return nil
	}
	return l.in.claimCycle(ending)
}

// run is Pi's runLoop. A turn is the list of turnStages. The driver reads the
// flow of the last stage: flowNextTurn starts another turn at once (Pi's inner
// loop); flowIdle and flowIdleContinue check the queued follow-up messages after
// the agent would stop (Pi's outer loop).
func (l *loop) run() error {
	for {
		f, err := l.runTurn()
		if err != nil {
			if l.ctx.Err() != nil && errors.Is(err, l.ctx.Err()) {
				return l.preparationAborted()
			}
			return err
		}
		switch f {
		case flowNextTurn:
			// Tool results or steering input start this turn.
			l.emptyContinues = 0
			continue
		case flowEndRun:
			return l.endRun()
		}
		// A Continue decision keeps the run going when no input is queued, so
		// the run is closing only when it would end now.
		continues := f == flowIdleContinue && (l.ts.ranTool || l.emptyContinues < maxEmptyContinuations)
		if claimed := l.takeCycle(!continues); len(claimed) > 0 {
			// Queued input is a new input cycle. The steer stage of its first
			// turn admits it and publishes turn_start.
			if err := l.closeCycle(); err != nil {
				return err
			}
			if err := l.openCycle(); err != nil {
				return err
			}
			l.pending = claimed
			l.emptyContinues = 0
			continue
		}
		// No natural request was selected, so the continuation decision gets
		// one request with the current context, up to the bound.
		if f == flowIdleContinue {
			// A turn that ran a tool body did real work, even when every
			// result asked to stop: it is not an empty continuation.
			if l.ts.ranTool {
				l.emptyContinues = 0
				continue
			}
			if l.emptyContinues >= maxEmptyContinuations {
				l.cycle.settle(ReasonContinuationLimit)
				return l.endRun()
			}
			l.emptyContinues++
			continue
		}
		return l.endRun()
	}
}

// prepareRequest is the only caller of the PrepareRequest point. The update
// changes the run state for this request and the later ones; its request
// messages change this request only.
func (l *loop) prepareRequest() error {
	l.requestMessages = nil
	upd, err := l.cfg.Pipeline.PrepareRequest(l.ctx, pipeline.Request{
		Context:        pipeline.AgentContext{Messages: freezeMessages(l.ac.Messages), Tools: l.ac.Tools},
		StagedMessages: freezeMessages(l.staged),
		Model:          l.cfg.Model,
		ThinkingLevel:  l.thinkingLevel(),
	})
	if err != nil || upd == nil {
		return err
	}
	l.requestMessages = upd.RequestMessages
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
	return l.cfg.Pipeline.CompleteStep(l.ctx, t)
}

// stopTurn is the only caller of the StopTurn point.
func (l *loop) stopTurn() (pipeline.TurnDecision, error) {
	return l.cfg.Pipeline.StopTurn(l.ctx, pipeline.StopInput{Context: l.context(), NewMessages: slices.Clip(l.newMessages)})
}

// context is the view of the run context that handlers get. The clipped slice
// keeps a handler's append away from the loop's spare capacity.
func (l *loop) context() pipeline.AgentContext {
	return pipeline.AgentContext{Messages: slices.Clip(l.ac.Messages), Tools: l.ac.Tools}
}

func (l *loop) thinkingLevel() protocol.ThinkingLevel {
	if l.cfg.Options.Reasoning == "" {
		return protocol.ThinkingOff
	}
	return l.cfg.Options.Reasoning
}

// emitMessage publishes a whole message: message_start, then the commit of the
// message, then message_end. A listener of message_end finds the message in
// the log.
func (l *loop) emitMessage(m protocol.Message) error {
	return l.emitInput(m, "")
}

// emitInput is emitMessage for a message that an input carries. The same commit
// records the input ID on the message and the acceptance of the input.
func (l *loop) emitInput(m protocol.Message, inputID string) error {
	if err := l.emit(&protocol.MessageStart{Message: m}); err != nil {
		return err
	}
	entries := []sessions.Entry{sessions.MessageEntry{Message: m, InputID: inputID}}
	if inputID != "" {
		entries = append(entries, sessions.InputOutcome{InputID: inputID, Accepted: true})
	}
	if err := l.commit(entries...); err != nil {
		return err
	}
	return l.emit(&protocol.MessageEnd{Message: m})
}

// toolCalls returns the tool calls of msg. Their arguments are copies: the
// message is part of the logged transcript, and no later edit of a call may
// reach it.
func toolCalls(msg protocol.AssistantMessage) []protocol.ToolCall {
	var calls []protocol.ToolCall
	for _, b := range msg.Content {
		var c protocol.ToolCall
		switch v := b.(type) {
		case protocol.ToolCall:
			c = v
		case *protocol.ToolCall:
			if v == nil {
				continue
			}
			c = *v
		default:
			continue
		}
		calls = append(calls, protocol.CloneAssistantBlock(c).(protocol.ToolCall))
	}
	return calls
}

// cloneAssistant copies msg so that every ToolCall.Arguments in it is a copy.
func cloneAssistant(msg protocol.AssistantMessage) protocol.AssistantMessage { return msg.Clone() }

// cloneMessages copies msgs so that every ToolCall.Arguments in an assistant
// message is a copy.
func cloneMessages(msgs []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, len(msgs))
	for i, m := range msgs {
		out[i] = protocol.CloneMessage(m)
	}
	return out
}

// messageValue reads a supported pointer or value record without changing its stored form.
func messageValue(m protocol.Message) protocol.Message {
	switch v := m.(type) {
	case *protocol.AssistantMessage:
		if v == nil {
			return nil
		}
		return *v
	case *protocol.ToolResultMessage:
		if v == nil {
			return nil
		}
		return *v
	default:
		return m
	}
}
