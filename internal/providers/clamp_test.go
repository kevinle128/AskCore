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
	word := func(s string) *string { return &s }
	alwaysOn := ThinkingLevelMap{
		protocol.ThinkingOff:     nil,
		protocol.ThinkingMinimal: nil,
		protocol.ThinkingLow:     word("low"),
		protocol.ThinkingMedium:  word("medium"),
		protocol.ThinkingHigh:    word("high"),
		protocol.ThinkingXHigh:   nil,
		protocol.ThinkingMax:     nil,
	}
	onlyHigh := ThinkingLevelMap{
		protocol.ThinkingOff:     nil,
		protocol.ThinkingMinimal: nil,
		protocol.ThinkingLow:     nil,
		protocol.ThinkingMedium:  nil,
		protocol.ThinkingHigh:    word("high"),
		protocol.ThinkingXHigh:   nil,
		protocol.ThinkingMax:     nil,
	}
	nothing := ThinkingLevelMap{}
	for _, l := range thinkingRank {
		nothing[l] = nil
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
		{
			name:  "off walks up to the lowest effort when the model refuses off",
			model: Model{Reasoning: true, ThinkingLevelMap: alwaysOn},
			in:    protocol.ThinkingOff,
			want:  protocol.ThinkingLow,
		},
		{
			name:  "minimal walks up past an unsupported off",
			model: Model{Reasoning: true, ThinkingLevelMap: alwaysOn},
			in:    protocol.ThinkingMinimal,
			want:  protocol.ThinkingLow,
		},
		{
			name:  "empty level is off and walks up when the model refuses off",
			model: Model{Reasoning: true, ThinkingLevelMap: alwaysOn},
			in:    "",
			want:  protocol.ThinkingLow,
		},
		{
			name:  "a level at or below the request still wins over the walk up",
			model: Model{Reasoning: true, ThinkingLevelMap: alwaysOn},
			in:    protocol.ThinkingMax,
			want:  protocol.ThinkingHigh,
		},
		{
			name:  "one word only: every level goes to it",
			model: Model{Reasoning: true, ThinkingLevelMap: onlyHigh},
			in:    protocol.ThinkingOff,
			want:  protocol.ThinkingHigh,
		},
		{
			name:  "no supported level gives off",
			model: Model{Reasoning: true, ThinkingLevelMap: nothing},
			in:    protocol.ThinkingMedium,
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
