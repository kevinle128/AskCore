package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/pipeline"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// TestTurnStagesOrder verifies that turnStages is ordered as expected.
// A reordering is a visible test change.
func TestTurnStagesOrder(t *testing.T) {
	expected := []string{"steer", "prepare", "reason", "act", "observe", "decide"}
	require.Equal(t, len(expected), len(turnStages), "turnStages has wrong length")
	for i, stage := range turnStages {
		require.Equal(t, expected[i], stage.name(), "stage %d name mismatch", i)
	}
}

// TestSteerStageFirstTurn verifies that the steer stage behaves correctly on
// the first turn (no poll, no turn_start) and subsequent turns.
func TestSteerStageFirstTurn(t *testing.T) {
	cases := []struct {
		name            string
		turns           int
		pendingSet      bool
		expectPoll      bool
		expectTurnStart bool
	}{
		{
			name:            "first turn: no poll, no turn_start",
			turns:           0,
			pendingSet:      false,
			expectPoll:      false,
			expectTurnStart: false,
		},
		{
			name:            "first turn with pending: no poll, no turn_start",
			turns:           0,
			pendingSet:      true,
			expectPoll:      false,
			expectTurnStart: false,
		},
		{
			name:            "second turn, empty pending: polls steering, emits turn_start",
			turns:           1,
			pendingSet:      false,
			expectPoll:      true,
			expectTurnStart: true,
		},
		{
			name:            "second turn, pending already set: no poll, emits turn_start",
			turns:           1,
			pendingSet:      true,
			expectPoll:      false,
			expectTurnStart: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &eventRecorder{}
			pollCalled := false
			steerMsg := protocol.UserMessage{
				Content:   []protocol.UserBlock{protocol.Text{Text: "steering"}},
				Timestamp: 1,
			}

			l := &loop{
				ctx:   context.Background(),
				cfg:   LoopConfig{Hooks: pipeline.Hooks{}},
				ac:    pipeline.AgentContext{},
				emit:  rec.emit,
				turns: tc.turns,
				ts:    turnState{results: []protocol.ToolResultMessage{}},
			}
			if tc.pendingSet {
				l.pending = append(l.pending, steerMsg)
			}

			// Set up the hook to track if it's called
			l.cfg.Hooks.GetSteeringMessages = func(ctx context.Context) ([]protocol.Message, error) {
				pollCalled = true
				return []protocol.Message{steerMsg}, nil
			}

			f, err := steerStage{}.run(l)
			require.NoError(t, err)
			require.Equal(t, flowNext, f)
			require.Equal(t, tc.expectPoll, pollCalled, "poll called mismatch")

			labels := rec.eventLabels()
			if tc.expectTurnStart {
				require.Contains(t, labels, "turn_start", "turn_start expected")
			} else {
				require.NotContains(t, labels, "turn_start", "turn_start not expected")
			}
		})
	}
}

// TestToolExecutorSelection verifies that toolExecutor picks the correct
// strategy: truncated for StopLength, sequential for Sequential tools, else parallel.
func TestToolExecutorSelection(t *testing.T) {
	cases := []struct {
		name             string
		stopReason       protocol.StopReason
		toolIsSequential bool
		expectTruncated  bool
		expectSequential bool
	}{
		{
			name:             "StopLength yields truncated executor",
			stopReason:       protocol.StopLength,
			toolIsSequential: false,
			expectTruncated:  true,
			expectSequential: false,
		},
		{
			name:             "StopLength yields truncated even with sequential tool",
			stopReason:       protocol.StopLength,
			toolIsSequential: true,
			expectTruncated:  true,
			expectSequential: false,
		},
		{
			name:             "Sequential tool yields sequential executor",
			stopReason:       protocol.StopToolUse,
			toolIsSequential: true,
			expectTruncated:  false,
			expectSequential: true,
		},
		{
			name:             "No sequential tool yields parallel executor",
			stopReason:       protocol.StopToolUse,
			toolIsSequential: false,
			expectTruncated:  false,
			expectSequential: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tool tools.Tool
			if tc.toolIsSequential {
				tool = &sequentialTestTool{
					funcTestTool: &funcTestTool{name: "seq_tool"},
				}
			} else {
				tool = &funcTestTool{name: "tool"}
			}

			reg := &tools.Registry{}
			require.NoError(t, reg.Register(tool, tools.SourceInfo{Kind: "builtin", Name: tool.Decl().Name}))

			l := &loop{
				ctx: context.Background(),
				ac:  pipeline.AgentContext{Tools: reg},
				cfg: LoopConfig{},
				// The executor reads tools from the snapshot of the turn.
				ts: turnState{tools: reg.Snapshot()},
			}

			msg := protocol.AssistantMessage{
				StopReason: tc.stopReason,
				Content: []protocol.AssistantBlock{
					protocol.ToolCall{
						ID:        "call1",
						Name:      tool.Decl().Name,
						Arguments: json.RawMessage("{}"),
					},
				},
			}

			executor := l.toolExecutor(msg, []protocol.ToolCall{
				{
					ID:        "call1",
					Name:      tool.Decl().Name,
					Arguments: json.RawMessage("{}"),
				},
			})

			_, isTruncated := executor.(truncatedExecutor)
			_, isSequential := executor.(sequentialExecutor)
			_, isParallel := executor.(parallelExecutor)

			require.Equal(t, tc.expectTruncated, isTruncated, "truncated executor mismatch")
			require.Equal(t, tc.expectSequential, isSequential, "sequential executor mismatch")
			if !tc.expectTruncated && !tc.expectSequential {
				require.True(t, isParallel, "should be parallel executor")
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

			l := &loop{
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
				cfg: LoopConfig{
					Hooks: pipeline.Hooks{
						FinishTurn: func(ctx context.Context, t pipeline.Turn) (pipeline.TurnDecision, error) {
							return tc.finishDecision, nil
						},
						GetSteeringMessages: func(ctx context.Context) ([]protocol.Message, error) {
							if tc.steeringMsg {
								return []protocol.Message{
									protocol.UserMessage{
										Content:   []protocol.UserBlock{protocol.Text{Text: "steering"}},
										Timestamp: 1,
									},
								}, nil
							}
							return nil, nil
						},
					},
				},
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

// funcTestTool is a minimal test tool.
type funcTestTool struct {
	name string
}

func (f *funcTestTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name:        f.name,
		Description: f.name,
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (f *funcTestTool) Execute(ctx context.Context, tc tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: "ok"}},
	}, nil
}

// sequentialTestTool adds the Sequential marker.
type sequentialTestTool struct {
	*funcTestTool
}

func (sequentialTestTool) Sequential() bool { return true }
