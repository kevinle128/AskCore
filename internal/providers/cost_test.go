package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/pkg/protocol"
)

func TestCalculateCost(t *testing.T) {
	t.Parallel()
	gpt := OpenAIGPT55()
	zeroPlan := TokenPlanCompletions()
	cases := []struct {
		name string
		m    Model
		u    protocol.Usage
		tier ServiceTier
		want protocol.Cost
	}{
		{
			name: "zeros stay zero",
			m:    zeroPlan,
			u:    protocol.Usage{},
			want: protocol.Cost{},
		},
		{
			name: "cache tokens count toward the long-context threshold",
			m:    gpt,
			u: protocol.Usage{
				Input:      271_900,
				CacheRead:  400,
				CacheWrite: 200,
			},
			want: protocol.Cost{
				Input:     271_900 * 10_000_000 / 1_000_000,
				CacheRead: 400 * 1_000_000 / 1_000_000,
				Total:     271_900*10_000_000/1_000_000 + 400*1_000_000/1_000_000,
			},
		},
		{
			name: "sum equal to Above stays on the base tier",
			m:    gpt,
			u:    protocol.Usage{Input: LongContextAbove},
			want: protocol.Cost{
				Input: LongContextAbove * 5_000_000 / 1_000_000,
				Total: LongContextAbove * 5_000_000 / 1_000_000,
			},
		},
		{
			name: "gpt-5.5 priority is 5/2 toward zero",
			m:    gpt,
			u:    protocol.Usage{Input: 3},
			tier: TierPriority,
			want: protocol.Cost{
				Input: (3 * 5_000_000 / 1_000_000) * 5 / 2,
				Total: (3 * 5_000_000 / 1_000_000) * 5 / 2,
			},
		},
		{
			name: "flex is half toward zero",
			m:    gpt,
			u:    protocol.Usage{Input: 3},
			tier: TierFlex,
			want: protocol.Cost{
				Input: (3 * 5_000_000 / 1_000_000) / 2,
				Total: (3 * 5_000_000 / 1_000_000) / 2,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, CalculateCost(tc.m, tc.u, tc.tier))
		})
	}
}

func TestNormalizeUsage(t *testing.T) {
	t.Parallel()
	reason := int64(3)
	write1h := int64(4)
	got := NormalizeUsage(RawUsage{
		InputTokens:      20,
		OutputTokens:     8,
		CachedTokens:     5,
		CacheWriteTokens: 6,
		CacheWrite1h:     &write1h,
		ReasoningTokens:  &reason,
		TotalTokens:      30,
	})
	assert.Equal(t, int64(9), got.Input)
	assert.Equal(t, int64(8), got.Output)
	assert.Equal(t, int64(5), got.CacheRead)
	assert.Equal(t, int64(6), got.CacheWrite)
	assert.Equal(t, int64(4), *got.CacheWrite1h)
	assert.Equal(t, int64(3), *got.Reasoning)
	assert.Equal(t, int64(30), got.TotalTokens)
}
