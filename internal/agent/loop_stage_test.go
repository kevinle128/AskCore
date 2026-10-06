package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// stageLog opens a log the way the Agent does, for a loop built by hand.
func stageLog(t *testing.T) sessions.Writer {
	t.Helper()
	d, err := openLog(&sessions.MemoryLog{}, snapshotOf("", nil))
	require.NoError(t, err)
	return d.log
}

// TestTurnStagesOrder verifies that turnStages is ordered as expected.
// A reordering is a visible test change.
func TestTurnStagesOrder(t *testing.T) {
	expected := []string{"steer", "prepare", "reason", "act", "observe", "decide"}
	require.Equal(t, len(expected), len(turnStages), "turnStages has wrong length")
	for i, stage := range turnStages {
		require.Equal(t, expected[i], stage.name(), "stage %d name mismatch", i)
	}
}

// stubInputs is a fixed source of claims for the stage tests, which build the
// loop by hand.
type stubInputs struct{ steering []input }

func (s *stubInputs) claimSteering() []input {
	got := s.steering
	s.steering = nil
	return got
}
func (s *stubInputs) claimCycle(bool) []input { return s.claimSteering() }
func (s *stubInputs) nextID() string          { return "stub" }

// TestSteerStageAdmission verifies the steer stage: it opens every turn after
// admission, and a turn with nothing to run at the start of a cycle ends the
// cycle instead.
func TestSteerStageAdmission(t *testing.T) {
	steerMsg := protocol.UserMessage{
		Content:   []protocol.UserBlock{protocol.Text{Text: "steering"}},
		Timestamp: 1,
	}
	cases := []struct {
		name            string
		turns           int
		pending         bool
		allowEmpty      bool
		reject          bool
		expectFlow      flow
		expectTurnStart bool
		expectReason    CycleReason
	}{
		{name: "first turn with input", pending: true, expectFlow: flowNext, expectTurnStart: true},
		{name: "first turn with no input ends the cycle", expectFlow: flowIdle},
		{name: "first turn with no input after Continue", allowEmpty: true, expectFlow: flowNext, expectTurnStart: true},
		{name: "later turn with no input is a tool continuation", turns: 1, expectFlow: flowNext, expectTurnStart: true},
		{name: "rejected input ends the cycle blocked", pending: true, reject: true, expectFlow: flowEndRun, expectReason: ReasonBlocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &eventRecorder{}
			reg := pipeline.NewRegistry()
			if tc.reject {
				reg.OnAdmitStep(func(context.Context, pipeline.AdmitInput, pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
					return pipeline.AdmitDecision{Reject: true}, nil
				})
			}
			l := &loop{
				log:        stageLog(t),
				ctx:        context.Background(),
				cfg:        LoopConfig{Pipeline: reg},
				ac:         pipeline.AgentContext{},
				emit:       rec.emit,
				cycle:      &cycleState{id: "c1", turns: tc.turns},
				allowEmpty: tc.allowEmpty,
				ts:         turnState{results: []protocol.ToolResultMessage{}},
			}
			if tc.pending {
				l.pending = []input{{id: "i1", msg: steerMsg}}
			}

			f, err := steerStage{}.run(l)
			require.NoError(t, err)
			require.Equal(t, tc.expectFlow, f)
			require.Equal(t, tc.expectReason, l.cycle.reason)
			require.Empty(t, l.pending, "the claimed input is consumed")
			require.False(t, l.allowEmpty, "only the first admission may be empty")

			labels := rec.eventLabels()
			if tc.expectTurnStart {
				require.Contains(t, labels, "turn_start", "turn_start expected")
			} else {
				require.NotContains(t, labels, "turn_start", "turn_start not expected")
			}
		})
	}
}

// TestDecideFlow is a table-driven test of the decide stage flow logic.
// It tests all combinations of failed message, moreTools, steering messages,
// and FinishTurn decision, verifying the returned flow and turn_end event.
func TestDecideFlow(t *testing.T) {
	cases := []struct {
		name               string
		msgFailed          bool
		moreTools          bool
		steeringMsg        bool
		finishDecision     pipeline.TurnDecision
		expectFlow         flow
		expectTurnEndEvent bool
	}{
		// Failed message cases: always flowEndRun, no queue poll
		{
			name:               "failed message returns flowEndRun regardless of decision",
			msgFailed:          true,
			moreTools:          false,
			steeringMsg:        false,
			finishDecision:     pipeline.Proceed,
			expectFlow:         flowEndRun,
			expectTurnEndEvent: true,
		},
		{
			name:               "failed message with moreTools still returns flowEndRun",
			msgFailed:          true,
			moreTools:          true,
			steeringMsg:        false,
			finishDecision:     pipeline.Proceed,
			expectFlow:         flowEndRun,
			expectTurnEndEvent: true,
		},
		// End decision: flowEndRun
		{
			name:               "End decision returns flowEndRun",
			msgFailed:          false,
			moreTools:          false,
			steeringMsg:        false,
			finishDecision:     pipeline.End,
			expectFlow:         flowEndRun,
			expectTurnEndEvent: true,
		},
		// Continue with moreTools: flowNextTurn
		{
			name:               "Continue with moreTools returns flowNextTurn",
			msgFailed:          false,
			moreTools:          true,
			steeringMsg:        false,
			finishDecision:     pipeline.Continue,
			expectFlow:         flowNextTurn,
			expectTurnEndEvent: true,
		},
		// Continue with steering pending: flowNextTurn
		{
			name:               "Continue with steering pending returns flowNextTurn",
			msgFailed:          false,
			moreTools:          false,
			steeringMsg:        true,
			finishDecision:     pipeline.Continue,
			expectFlow:         flowNextTurn,
			expectTurnEndEvent: true,
		},
		// Continue without moreTools and no steering: flowIdleContinue
		{
			name:               "Continue without moreTools and no steering returns flowIdleContinue",
			msgFailed:          false,
			moreTools:          false,
			steeringMsg:        false,
			finishDecision:     pipeline.Continue,
			expectFlow:         flowIdleContinue,
			expectTurnEndEvent: true,
		},
		// Proceed with moreTools: flowNextTurn
		{
			name:               "Proceed with moreTools returns flowNextTurn",
			msgFailed:          false,
			moreTools:          true,
			steeringMsg:        false,
			finishDecision:     pipeline.Proceed,
			expectFlow:         flowNextTurn,
			expectTurnEndEvent: true,
		},
		// Proceed with steering pending: flowNextTurn
		{
			name:               "Proceed with steering pending returns flowNextTurn",
			msgFailed:          false,
			moreTools:          false,
			steeringMsg:        true,
			finishDecision:     pipeline.Proceed,
			expectFlow:         flowNextTurn,
			expectTurnEndEvent: true,
		},
		// Proceed without moreTools and no steering: flowIdle
		{
			name:               "Proceed without moreTools and no steering returns flowIdle",
			msgFailed:          false,
			moreTools:          false,
			steeringMsg:        false,
			finishDecision:     pipeline.Proceed,
			expectFlow:         flowIdle,
			expectTurnEndEvent: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &eventRecorder{}
			stopReason := protocol.StopStop
			if tc.msgFailed {
				stopReason = protocol.StopError
			}

			msg := protocol.AssistantMessage{
				StopReason: stopReason,
				Content:    []protocol.AssistantBlock{protocol.Text{Text: "response"}},
			}

			reg := pipeline.NewRegistry()
			reg.OnCompleteStep(func(ctx context.Context, t pipeline.Turn) (pipeline.TurnDecision, error) {
				return tc.finishDecision, nil
			})
			l := &loop{
				log:         &sessions.MemoryLog{},
				ctx:         context.Background(),
				newMessages: []protocol.Message{},
				ac:          pipeline.AgentContext{},
				emit:        rec.emit,
				turns:       0,
				ts: turnState{
					msg:       msg,
					results:   []protocol.ToolResultMessage{},
					moreTools: tc.moreTools,
				},
				cfg: LoopConfig{Pipeline: reg},
			}
			if tc.steeringMsg {
				l.in = &stubInputs{steering: []input{{id: "i1", msg: protocol.UserMessage{
					Content:   []protocol.UserBlock{protocol.Text{Text: "steering"}},
					Timestamp: 1,
				}}}}
			}

			f, err := decideStage{}.run(l)
			require.NoError(t, err)
			require.Equal(t, tc.expectFlow, f)

			labels := rec.eventLabels()
			if tc.expectTurnEndEvent {
				require.Contains(t, labels, "turn_end", "turn_end event expected")
			}

			// Verify turn_end appears exactly once
			count := 0
			for _, label := range labels {
				if label == "turn_end" {
					count++
				}
			}
			require.Equal(t, 1, count, "turn_end should appear exactly once")
		})
	}
}

// Test helper types and functions below

// eventRecorder records events emitted by the loop.
type eventRecorder struct {
	events []protocol.Event
}

func (r *eventRecorder) emit(ev protocol.Event) error {
	r.events = append(r.events, ev)
	return nil
}

func (r *eventRecorder) eventLabels() []string {
	labels := make([]string, 0, len(r.events))
	for _, ev := range r.events {
		labels = append(labels, eventLabel(ev))
	}
	return labels
}

func eventLabel(ev protocol.Event) string {
	switch ev.(type) {
	case *protocol.TurnStart:
		return "turn_start"
	case *protocol.TurnEnd:
		return "turn_end"
	case *protocol.AgentStart:
		return "agent_start"
	case *protocol.AgentEnd:
		return "agent_end"
	case *protocol.MessageStart:
		return "message_start"
	case *protocol.MessageEnd:
		return "message_end"
	default:
		return "unknown"
	}
}
