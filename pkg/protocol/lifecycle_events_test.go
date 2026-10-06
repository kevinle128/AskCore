package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lifecycleEvents() []Event {
	return []Event{
		&CycleStart{Envelope: env(1), CycleID: "c1"},
		&CycleEnd{Envelope: env(2), CycleID: "c1", Reason: "max-tokens"},
		&CycleEnd{Envelope: env(3), CycleID: "c1", Reason: "aborted", Cause: "user"},
		&CycleEnd{Envelope: env(4), CycleID: "c1", Reason: "error", Code: "RATE_LIMIT"},
		&AttemptStart{Envelope: env(4), AttemptID: "a1", CycleID: "c1", Number: 2},
		&AttemptEnd{Envelope: env(5), AttemptID: "a1", Outcome: "failed"},
		&TurnStart{Envelope: env(6), CycleID: "c1"},
		&TurnEnd{Envelope: env(7), CycleID: "c1", Message: finalAssistant(), ToolResults: []ToolResultMessage{}},
	}
}

func TestLifecycleEventsRoundTripThroughCodec(t *testing.T) {
	for _, ev := range lifecycleEvents() {
		t.Run(ev.EventType(), func(t *testing.T) {
			b, err := EncodeEvent(ev)
			require.NoError(t, err)
			back, err := DecodeEvent(b)
			require.NoError(t, err)
			assert.Equal(t, ev, back)
		})
	}
}

// TestLifecycleEventBytes pins the exact bytes of the
// lifecycle events. They have no fast path, so the bytes are the contract of
// JSON readers: key order, omitted empty fields and the envelope at the top.
func TestLifecycleEventBytes(t *testing.T) {
	prefix := func(seq string, typ string) string {
		return `{"seq":` + seq + `,"ts":170000000000` + seq + `,"sessionId":"s1","runId":"r1","type":"` + typ + `"`
	}
	cases := []struct {
		ev   Event
		want string
	}{
		{&CycleStart{Envelope: env(1), CycleID: "c1"}, prefix("1", "cycle_start") + `,"cycleId":"c1"}`},
		{&CycleEnd{Envelope: env(2), CycleID: "c1", Reason: "completed"}, prefix("2", "cycle_end") + `,"cycleId":"c1","reason":"completed"}`},
		{&CycleEnd{Envelope: env(3), CycleID: "c1", Reason: "aborted", Cause: "user"}, prefix("3", "cycle_end") + `,"cycleId":"c1","reason":"aborted","cause":"user"}`},
		{&CycleEnd{Envelope: env(4), CycleID: "c1", Reason: "error", Code: "RATE_LIMIT"}, prefix("4", "cycle_end") + `,"cycleId":"c1","reason":"error","code":"RATE_LIMIT"}`},
		{&AttemptStart{Envelope: env(4), AttemptID: "a1", CycleID: "c1", Number: 1}, prefix("4", "attempt_start") + `,"attemptId":"a1","cycleId":"c1","number":1}`},
		{&AttemptEnd{Envelope: env(5), AttemptID: "a1", Outcome: "completed"}, prefix("5", "attempt_end") + `,"attemptId":"a1","outcome":"completed"}`},
		{&TurnStart{Envelope: env(6)}, prefix("6", "turn_start") + `}`},
		{&TurnStart{Envelope: env(7), CycleID: "c1"}, prefix("7", "turn_start") + `,"cycleId":"c1"}`},
	}
	for _, tc := range cases {
		b, err := EncodeEvent(tc.ev)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(b))
	}
}

func TestTurnEndOmitsEmptyCycleID(t *testing.T) {
	b, err := EncodeEvent(&TurnEnd{Envelope: env(1), Message: UserMessage{}})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "cycleId")
}
