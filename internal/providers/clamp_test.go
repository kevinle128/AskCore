package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/pkg/protocol"
)

func TestClampThinkingLevel(t *testing.T) {
	t.Parallel()
	off := "none"
	onlyOff := ThinkingLevelMap{
		protocol.ThinkingOff:     &off,
		protocol.ThinkingMinimal: nil,
		protocol.ThinkingLow:     nil,
		protocol.ThinkingMedium:  nil,
		protocol.ThinkingHigh:    nil,
		protocol.ThinkingXHigh:   nil,
		protocol.ThinkingMax:     nil,
	}
	cases := []struct {
		name  string
		model Model
		in    protocol.ThinkingLevel
		want  protocol.ThinkingLevel
	}{
		{
			name:  "reasoning false is off",
			model: Model{Reasoning: false},
			in:    protocol.ThinkingHigh,
			want:  protocol.ThinkingOff,
		},
		{
			name:  "high walks to off when only off is supported",
			model: Model{Reasoning: true, ThinkingLevelMap: onlyOff},
			in:    protocol.ThinkingHigh,
			want:  protocol.ThinkingOff,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClampThinkingLevel(tc.model, tc.in)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, got, ClampThinkingLevel(tc.model, got), "second clamp is stable")
		})
	}
}
