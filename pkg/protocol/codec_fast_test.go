package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeltaUpdateFastPathMatchesReflection(t *testing.T) {
	one := int64(1)
	usages := []Usage{
		{},
		{Input: 57, Output: 4, TotalTokens: 61, Cost: Cost{Input: 3, Output: -2, Total: 1}},
		{Input: 1 << 62, CacheWrite1h: &one, Reasoning: &one},
	}
	deltas := []string{
		"", "hello", "a<b>&c", "xin chào 😀", "a\nb", "q\"", "back\\slash", "\t", "\x00",
		"a\u2028b", "a\u2029b", "\xe2\x80", "bad\xff", "\u2027\u202a",
	}
	envs := []Envelope{
		{Seq: 7, TS: 1790935177402, SessionID: "s", RunID: "7dd7"},
		{},
		{SessionID: "s\"x", RunID: "r\n"},
	}
	for _, env := range envs {
		for _, u := range usages {
			for _, d := range deltas {
				for _, ev := range []BlockEvent{
					TextDeltaEvent{ContentIndex: 2, Delta: d},
					ThinkingDeltaEvent{ContentIndex: 0, Delta: d},
				} {
					e := &MessageUpdate{Envelope: env, AssistantMessageEvent: ev, Usage: u}
					want, err := encodeMessageUpdate(e)
					require.NoError(t, err)
					got, err := EncodeEvent(e)
					require.NoError(t, err)
					assert.Equal(t, string(want), string(got), "delta %q env %+v", d, env)
				}
			}
		}
	}
}

func TestDeltaUpdateFastPathTakesPlainStrings(t *testing.T) {
	e := &MessageUpdate{AssistantMessageEvent: TextDeltaEvent{Delta: "xin chào <b>"}}
	_, ok := appendDeltaUpdate(e)
	assert.True(t, ok)
	e.AssistantMessageEvent = TextDeltaEvent{Delta: "a\nb"}
	_, ok = appendDeltaUpdate(e)
	assert.False(t, ok)
	e.AssistantMessageEvent = TextStartEvent{}
	_, ok = appendDeltaUpdate(e)
	assert.False(t, ok)
}

func BenchmarkEncodeDeltaUpdate(b *testing.B) {
	e := &MessageUpdate{
		Envelope:              Envelope{Seq: 7, TS: 1790935177402, RunID: "7dd73213a6ba499cf93bd9c648cd1234"},
		AssistantMessageEvent: TextDeltaEvent{Delta: "hello world foo!"},
		Usage:                 Usage{Input: 57, Output: 4, TotalTokens: 61},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := EncodeEvent(e); err != nil {
			b.Fatal(err)
		}
	}
}
