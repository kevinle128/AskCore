package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryEventsRoundTripThroughCodec(t *testing.T) {
	for _, event := range []Event{
		&AutoRetryStart{Envelope: env(1), Attempt: 2, MaxAttempts: 5, DelayMs: 1000, ErrorMessage: "slow down"},
		&AutoRetryEnd{Envelope: env(2), Success: true, Attempt: 2},
		&AutoRetryEnd{Envelope: env(3), Success: false, Attempt: 5, FinalError: "failed"},
		&AgentEnd{Envelope: env(4), Messages: []Message{}, WillRetry: true},
	} {
		t.Run(event.EventType(), func(t *testing.T) {
			line, err := EncodeEvent(event)
			require.NoError(t, err)
			decoded, err := DecodeEvent(line)
			require.NoError(t, err)
			require.Equal(t, event, decoded)
			require.NoError(t, NewBuilder().ApplyAgentEvent(decoded))
		})
	}
}

func TestAgentEndAlwaysCarriesWillRetry(t *testing.T) {
	line, err := EncodeEvent(&AgentEnd{})
	require.NoError(t, err)
	require.Contains(t, string(line), `"willRetry":false`)
	event, err := DecodeEvent([]byte(`{"type":"agent_end","messages":[]}`))
	require.NoError(t, err)
	require.False(t, event.(*AgentEnd).WillRetry)
	line, err = EncodeEvent(&AutoRetryEnd{Success: true, Attempt: 1})
	require.NoError(t, err)
	require.NotContains(t, string(line), "finalError")
}
