package agent

import (
	"context"
	"slices"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// flow tells the driver what to do after a stage.
type flow uint8

const (
	// flowNext runs the next stage of this turn.
	flowNext flow = iota
	// flowNextTurn starts a new turn: tool results or steering messages are
	// pending.
	flowNextTurn
	// flowIdle means nothing is pending: check the queues, else end the run.
	flowIdle
	// flowIdleContinue is flowIdle, but FinishTurn asked for one more request
	// when nothing else selects one.
	flowIdleContinue
	// flowEndRun emits agent_end and stops without claiming from any queue.
	flowEndRun
)

// stage is one step of a turn. A stage reads and writes the loop and its
// turnState only; it holds no state of its own.
type stage interface {
	name() string
	run(l *loop) (flow, error)
}

// turnState is the data of the turn in progress. The steer stage resets it.
type turnState struct {
	msg protocol.AssistantMessage
	// results starts as a non-nil empty slice, because turn_end carries it
	// as an empty list and not as null.
	results []protocol.ToolResultMessage
	// moreTools is set when a tool batch ran and did not ask to terminate.
	moreTools bool
	// ranTool is set when at least one tool body started in this turn.
	ranTool bool
	// tools is the tool set of this turn. The model is told about it before
	// the request, and the act stage runs calls against it, so the declared
	// and the executable tools are the same even if the registry changes.
	tools *tools.Snapshot
}

// failed reports a message that ended in an error or an abort. Such a turn
// runs no tools and claims nothing from a queue.
func (t *turnState) failed() bool {
	return t.msg.StopReason == protocol.StopError || t.msg.StopReason == protocol.StopAborted
}

// turnStages is one ReAct turn in order. decide must stay last: it is the
// only stage that does not return flowNext.
var turnStages = []stage{steerStage{}, prepareStage{}, reasonStage{}, actStage{}, observeStage{}, decideStage{}}

// runTurn runs the stages until one returns a flow other than flowNext.
func (l *loop) runTurn() (flow, error) {
	for _, s := range turnStages {
		f, err := s.run(l)
		if err != nil {
			return flowEndRun, err
		}
		if f != flowNext {
			return f, nil
		}
	}
	panic("agent: last turn stage returned flowNext: " + turnStages[len(turnStages)-1].name())
}

// steerStage opens every turn. It dispatches AdmitStep with the input that the
// loop claimed for the turn, none after a tool batch. A rejection ends the
// cycle with reason blocked and no turn. When the first turn of a cycle is left
// with no input at all, the cycle ends completed with no turn. Otherwise it
// publishes turn_start, takes the tool snapshot of the turn, declares tool
// changes, and adds the admitted messages to the context.
type steerStage struct{}

func (steerStage) name() string { return "steer" }

func (steerStage) run(l *loop) (flow, error) {
	dec, err := l.admit()
	if err != nil {
		return flowEndRun, err
	}
	allowEmpty := l.allowEmpty
	l.allowEmpty = false
	if dec.Reject {
		if err := l.rejectPending(); err != nil {
			return flowEndRun, err
		}
		l.cycle.settle(ReasonBlocked)
		return flowEndRun, nil
	}
	if len(dec.Messages) == 0 && l.cycle.turns == 0 && !allowEmpty {
		l.pending = nil
		return flowIdle, nil
	}
	if err := l.openTurn(); err != nil {
		return flowEndRun, err
	}
	l.ts = turnState{results: []protocol.ToolResultMessage{}, tools: l.ac.Tools.Snapshot()}
	claimed := l.pending
	l.pending = nil
	l.claimed = claimed
	l.stagedInputCount = len(dec.Messages)
	l.stagedIDs = nil
	declared := declareToolChanges(l.ac.Messages, l.ts.tools, dec.Messages)
	// A tool declaration that the loop added has no input. It goes before the
	// first message that is not a system message.
	synthetic := -1
	if len(declared) == len(dec.Messages)+1 {
		synthetic = slices.IndexFunc(dec.Messages, func(m protocol.Message) bool {
			_, ok := m.(protocol.SystemMessage)
			return !ok
		})
		if synthetic < 0 {
			synthetic = len(dec.Messages)
		}
	}
	l.staged = declared
	next := 0
	for i, m := range declared {
		id := ""
		if i != synthetic {
			if next < len(claimed) {
				id = claimed[next].id
			}
			next++
		}
		l.stagedIDs = append(l.stagedIDs, id)
		l.ac.Messages = append(l.ac.Messages, m)
	}
	return flowNext, nil
}

// dropUnentered acknowledges the claimed inputs that no admitted message
// carries, which happens when a handler rewrote the messages to fewer. The
// admitted messages take the claimed inputs in order.
func (l *loop) dropUnentered(claimed []input, entered int) error {
	if entered >= len(claimed) {
		return nil
	}
	var entries []sessions.Entry
	for _, in := range claimed[entered:] {
		entries = append(entries, sessions.InputOutcome{InputID: in.id, Accepted: false, Reason: "dropped"})
	}
	return l.commit(entries...)
}

// admit is the only caller of the AdmitStep point. The handlers get copies of
// the claimed messages.
func (l *loop) admit() (pipeline.AdmitDecision, error) {
	return l.cfg.Pipeline.AdmitStep(l.ctx, pipeline.AdmitInput{
		CycleID:  l.cycleID(),
		Turn:     l.cycle.turns + 1,
		Messages: cloneMessages(messagesOf(l.pending)),
	})
}

// rejectPending acknowledges every claimed message as rejected, once, and
// drops them: a rejected claim is consumed.
func (l *loop) rejectPending() error {
	entries := make([]sessions.Entry, len(l.pending))
	for i, in := range l.pending {
		entries[i] = sessions.InputOutcome{InputID: in.id, Accepted: false, Reason: "rejected"}
	}
	l.pending = nil
	if len(entries) == 0 {
		return nil
	}
	return l.commit(entries...)
}

// prepareStage lets the request hook change the run state before the request.
type prepareStage struct{}

func (prepareStage) name() string { return "prepare" }

func (prepareStage) run(l *loop) (flow, error) {
	err := runGuarded(l.prepareRequest)
	if err != nil {
		return flowEndRun, l.preparationFailed(err)
	}
	return flowNext, nil
}

// reasonStage sends one model request and stores the assistant message.
type reasonStage struct{}

func (reasonStage) name() string { return "reason" }

func (reasonStage) run(l *loop) (flow, error) {
	msg, err := l.streamAssistantResponse()
	if err != nil {
		return flowEndRun, err
	}
	l.newMessages = append(l.newMessages, msg)
	l.ts.msg = msg
	return flowNext, nil
}

// actStage runs the tool calls of the assistant message, if it has any.
type actStage struct{}

func (actStage) name() string { return "act" }

func (actStage) run(l *loop) (flow, error) {
	if l.ts.failed() {
		return flowNext, nil
	}
	calls := toolCalls(l.ts.msg)
	if len(calls) == 0 {
		return flowNext, nil
	}
	batch, err := l.runToolBatch(l.ts.msg, calls)
	if err != nil {
		return flowEndRun, err
	}
	l.ts.results = batch.messages
	l.ts.moreTools = !batch.terminate
	l.ts.ranTool = batch.ran
	return flowNext, nil
}

// observeStage adds the tool results to the context.
type observeStage struct{}

func (observeStage) name() string { return "observe" }

func (observeStage) run(l *loop) (flow, error) {
	if l.ts.failed() {
		return flowNext, nil
	}
	for _, r := range l.ts.results {
		l.ac.Messages = append(l.ac.Messages, r)
		l.newMessages = append(l.newMessages, r)
	}
	if len(l.ts.results) > 0 && l.ctx.Err() != nil {
		msg := abortedMessage(l.cfg.Model, l.clock, context.Cause(l.ctx))
		if err := l.emitMessage(msg); err != nil {
			return flowEndRun, err
		}
		l.ac.Messages = append(l.ac.Messages, msg)
		l.newMessages = append(l.newMessages, msg)
		l.ts.msg = msg
	}
	return flowNext, nil
}

// decideStage closes the turn: FinishTurn, turn_end, then the choice between
// another turn, an idle check and the end of the run.
type decideStage struct{}

func (decideStage) name() string { return "decide" }

func (decideStage) run(l *loop) (flow, error) {
	ts := &l.ts
	l.turns++
	l.cycle.settleTurn(ts.msg.StopReason, l.failureCode)
	decision, err := l.finishTurn(*l.turn(ts.msg, ts.results))
	if err != nil {
		return flowEndRun, err
	}
	if err := l.closeTurn(ts.msg, ts.results); err != nil {
		return flowEndRun, err
	}
	// The decision of CompleteStep does not count for a failed message: the
	// run ends without a queue claim. An explicit End also ends the run before
	// StopTurn, so StopTurn can never override it.
	if ts.failed() || decision == pipeline.End {
		return flowEndRun, nil
	}
	// The turn boundary takes all steering messages, after the whole tool
	// batch. A cancelled run takes none.
	l.pending = append(l.pending, l.takeSteering()...)
	if ts.moreTools || len(l.pending) > 0 {
		return flowNextTurn, nil
	}
	// The cycle has no required work left. StopTurn runs after a reply that
	// was cut off at the token limit as well: a handler can steer the cycle on.
	stop, err := l.stopTurn()
	if err != nil {
		return flowEndRun, err
	}
	if l.ctx.Err() != nil {
		// A handler cancelled the run: the turn that a Continue or a steering
		// message would start does not run.
		l.cycle.settle(ReasonAborted)
		return flowEndRun, nil
	}
	if stop == pipeline.End {
		return flowEndRun, nil
	}
	l.pending = append(l.pending, l.takeSteering()...)
	if len(l.pending) > 0 {
		return flowNextTurn, nil
	}
	// A reply cut off at the token limit ends the cycle unless steering is
	// queued: a Continue decision does not ask for another request.
	if (decision == pipeline.Continue || stop == pipeline.Continue) && ts.msg.StopReason != protocol.StopLength {
		return flowIdleContinue, nil
	}
	return flowIdle, nil
}

// endRun closes the open cycle and emits agent_end with the messages that
// this run added.
func (l *loop) endRun() error {
	if err := l.closeCycle(); err != nil {
		return err
	}
	return l.emit(&protocol.AgentEnd{Messages: slices.Clip(l.newMessages)})
}
