package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueEventsRoundTripThroughCodec(t *testing.T) {
	for _, ev := range []Event{
		&QueueUpdate{Envelope: env(1), Steering: []string{"a", "b"}, FollowUp: []string{"c"}},
		&QueueUpdate{Envelope: env(2), Steering: []string{}, FollowUp: []string{}},
		&AgentDisposed{Envelope: env(3)},
	} {
		t.Run(ev.EventType(), func(t *testing.T) {
			b, err := EncodeEvent(ev)
			require.NoError(t, err)
			back, err := DecodeEvent(b)
			require.NoError(t, err)
			assert.Equal(t, ev, back)
		})
	}
}

// TestQueueEventBytes pins the bytes: an empty queue is an empty array, never
// null, because Pi readers iterate over both lists.
func TestQueueEventBytes(t *testing.T) {
	prefix := func(seq string, typ string) string {
		return `{"seq":` + seq + `,"ts":170000000000` + seq + `,"sessionId":"s1","runId":"r1","type":"` + typ + `"`
	}
	cases := []struct {
		ev   Event
		want string
	}{
		{&QueueUpdate{Envelope: env(1), Steering: []string{"a"}, FollowUp: []string{"b", "c"}}, prefix("1", "queue_update") + `,"steering":["a"],"followUp":["b","c"]}`},
		{&QueueUpdate{Envelope: env(2)}, prefix("2", "queue_update") + `,"steering":[],"followUp":[]}`},
		{&AgentDisposed{Envelope: env(3)}, prefix("3", "agent_disposed") + `}`},
	}
	for _, tc := range cases {
		b, err := EncodeEvent(tc.ev)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(b))
	}
}

func TestDecodeQueueUpdateWithMissingListsGivesEmptyLists(t *testing.T) {
	ev, err := DecodeEvent([]byte(`{"seq":1,"ts":1,"sessionId":"s","runId":"r","type":"queue_update"}`))
	require.NoError(t, err)
	q := ev.(*QueueUpdate)
	assert.Empty(t, q.Steering)
	assert.Empty(t, q.FollowUp)
}
