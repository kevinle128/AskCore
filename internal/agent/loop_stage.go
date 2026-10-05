package agent

import (
	"slices"

	"AskCore/internal/pipeline"
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
	// flowIdle means nothing is pending: check follow-ups, else end the run.
	flowIdle
	// flowIdleContinue is flowIdle, but FinishTurn asked for one more request
	// when nothing else selects one.
	flowIdleContinue
	// flowEndRun emits agent_end and stops without polling any queue.
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
	// tools is the tool set of this turn. The model is told about it before
	// the request, and the act stage runs calls against it, so the declared
	// and the executable tools are the same even if the registry changes.
	tools *tools.Snapshot
}

// failed reports a message that ended in an error or an abort. Such a turn
// runs no tools and polls no queue.
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

// steerStage opens a turn after the first: it polls steering messages when
// none are pending and emits turn_start. Then it takes the tool snapshot of
// the turn, declares tool changes, and adds the pending messages to the
// context.
type steerStage struct{}

func (steerStage) name() string { return "steer" }

func (steerStage) run(l *loop) (flow, error) {
	if l.turns > 0 {
		// Poll again only if the earlier poll was empty, so a one-at-a-time
		// queue never delivers two messages in a turn.
		if len(l.pending) == 0 {
			var err error
			if l.pending, err = l.pollSteering(); err != nil {
				return flowEndRun, err
			}
		}
		if err := l.emit(&protocol.TurnStart{}); err != nil {
			return flowEndRun, err
		}
	}
	l.ts = turnState{results: []protocol.ToolResultMessage{}, tools: l.ac.Tools.Snapshot()}
	for _, m := range declareToolChanges(l.ac.Messages, l.ts.tools, l.pending) {
		if err := l.emitMessage(m); err != nil {
			return flowEndRun, err
		}
		l.ac.Messages = append(l.ac.Messages, m)
		l.newMessages = append(l.newMessages, m)
	}
	l.pending = nil
	return flowNext, nil
}

// prepareStage lets the request hook change the run state before the request.
type prepareStage struct{}

func (prepareStage) name() string { return "prepare" }

func (prepareStage) run(l *loop) (flow, error) {
	return flowNext, l.prepareRequest()
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
	batch, err := l.toolExecutor(l.ts.msg, calls).run(l, l.ts.msg, calls)
	if err != nil {
		return flowEndRun, err
	}
	l.ts.results = batch.messages
	l.ts.moreTools = !batch.terminate
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
	return flowNext, nil
}

// decideStage closes the turn: FinishTurn, turn_end, then the choice between
// another turn, an idle check and the end of the run.
type decideStage struct{}

func (decideStage) name() string { return "decide" }

func (decideStage) run(l *loop) (flow, error) {
	ts := &l.ts
	l.turns++
	decision, err := l.finishTurn(*l.turn(ts.msg, ts.results))
	if err != nil {
		return flowEndRun, err
	}
	if err := l.emit(&protocol.TurnEnd{Message: ts.msg, ToolResults: ts.results}); err != nil {
		return flowEndRun, err
	}
	// The decision of FinishTurn does not count for a failed message: the
	// run ends without a queue poll.
	if ts.failed() || decision == pipeline.End {
		return flowEndRun, nil
	}
	if l.pending, err = l.pollSteering(); err != nil {
		return flowEndRun, err
	}
	if ts.moreTools || len(l.pending) > 0 {
		return flowNextTurn, nil
	}
	if decision == pipeline.Continue {
		return flowIdleContinue, nil
	}
	return flowIdle, nil
}

// endRun emits agent_end with the messages that this run added.
func (l *loop) endRun() error {
	return l.emit(&protocol.AgentEnd{Messages: slices.Clip(l.newMessages)})
}
