package faux

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestSerializeTranscript(t *testing.T) {
	removed := protocol.ToolRef{Name: "old"}
	added := protocol.ToolDecl{Name: "t", Description: "d", Parameters: []byte(`{"type":"object"}`)}
	msgs := []protocol.Message{
		protocol.SystemMessage{
			Content: []protocol.Text{{Text: "sys"}},
			Sections: protocol.Sections{
				{Name: "a", Value: sp("A")}, {Name: "gone"}, {Name: "b", Value: sp("B")},
			},
			ToolsRemoved: []protocol.ToolRef{removed},
			ToolsAdded:   []protocol.ToolDecl{added},
		},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}, protocol.Image{Data: "abcd", MimeType: "image/png"}}},
		protocol.AssistantMessage{Content: []protocol.AssistantBlock{
			protocol.Thinking{Thinking: "hmm"}, protocol.Text{Text: "ok"},
			protocol.ToolCall{ID: "1", Name: "echo", Arguments: []byte(`{ "x": 1 }`)},
		}},
		protocol.ToolResultMessage{ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "r"}, protocol.Image{Data: "xy", MimeType: "image/gif"}}},
		&protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "ptr"}}},
	}
	want := "system:sys\n\nA\n\nB\n" +
		`tool-:{"name":"old"}` + "\n" +
		`tool+:{"name":"t","description":"d","parameters":{"type":"object"}}` +
		"\n\nuser:hi\n[image:image/png:4]" +
		"\n\nassistant:hmm\nok\necho:{\"x\":1}" +
		"\n\ntoolResult:echo\nr\n[image:image/gif:2]" +
		"\n\nuser:ptr"
	assert.Equal(t, want, serializeTranscript(msgs))
	assert.Equal(t, "", serializeTranscript(nil))
}

func TestEstimateCountsRunesNotBytes(t *testing.T) {
	assert.Equal(t, int64(0), estimateTokens(""))
	assert.Equal(t, int64(1), estimateTokens("abcd"))
	assert.Equal(t, int64(2), estimateTokens("abcde"))
	assert.Equal(t, int64(2), estimateTokens("😀😀😀😀😀"), "five runes, twenty bytes")
}

func usageOf(t *testing.T, p *Provider, text string, opts providers.StreamOptions) protocol.Usage {
	t.Helper()
	s := p.Stream(t.Context(), defaultModel(t, p), req(userMsg(text)), opts)
	items := drain(s)
	msg, err := result(t, s)
	require.NoError(t, err)
	for _, it := range items {
		assert.Equal(t, msg.Usage, it.Usage, "every item carries the usage of the result")
	}
	return msg.Usage
}

func TestUsageEstimateAsciiAndEmoji(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("abcde"), Say("😀😀"))
	// "user:hi" is 7 runes (2 tokens); the output has 5 runes (2 tokens).
	assert.Equal(t, protocol.Usage{Input: 2, Output: 2, TotalTokens: 4}, usageOf(t, p, "hi", providers.StreamOptions{}))
	// "user:" and five emoji are 10 runes (3 tokens, not 7); two emoji output is 1 token.
	assert.Equal(t, protocol.Usage{Input: 3, Output: 1, TotalTokens: 4}, usageOf(t, p, "😀😀😀😀😀", providers.StreamOptions{}))
}

func TestUsageCountsToolCallOutput(t *testing.T) {
	p := newProvider(t)
	p.Set(Reply(Text("ab"), ToolCall("echo", map[string]int{"n": 1}), ToolCallRaw("raw", "", "{")))
	// "ab\necho:{"n":1}\nraw:{" is 2+1+12+1+5 = 21 runes: 6 tokens.
	u := usageOf(t, p, "hi", providers.StreamOptions{})
	assert.Equal(t, int64(6), u.Output)
}

func TestUsageCacheCommonPrefix(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("ok"), Say("ok"), Say("ok"), Say("ok"))
	sess := providers.StreamOptions{SessionID: "s"}
	// First call: "user:aaaaaaaa" is 13 runes, 4 tokens.
	u1 := usageOf(t, p, "aaaaaaaa", sess)
	assert.Equal(t, protocol.Usage{Input: 4, Output: 1, CacheWrite: 4, TotalTokens: 9}, u1)
	// Longer prompt of 21 runes (6 tokens); the common prefix is 13 runes.
	u2 := usageOf(t, p, "aaaaaaaabbbbbbbb", sess)
	assert.Equal(t, protocol.Usage{Input: 2, Output: 1, CacheRead: 4, CacheWrite: 2, TotalTokens: 9}, u2)
	// Shorter prompt "user:aaaa" (9 runes, 3 tokens) is a prefix of the cached one.
	u3 := usageOf(t, p, "aaaa", sess)
	assert.Equal(t, protocol.Usage{Input: 0, Output: 1, CacheRead: 3, CacheWrite: 0, TotalTokens: 4}, u3)
	// The cached prompt is now "user:aaaa"; a diverging prompt shares 6 runes ("user:a").
	u4 := usageOf(t, p, "abbb", sess)
	assert.Equal(t, protocol.Usage{Input: 1, Output: 1, CacheRead: 2, CacheWrite: 1, TotalTokens: 5}, u4)
}

func TestUsageCacheSessionsAreSeparate(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("ok"), Say("ok"))
	a := usageOf(t, p, "aaaaaaaa", providers.StreamOptions{SessionID: "a"})
	b := usageOf(t, p, "aaaaaaaa", providers.StreamOptions{SessionID: "b"})
	assert.Equal(t, a, b)
	assert.Equal(t, int64(0), b.CacheRead)
}

func TestUsageCacheDisabled(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("ok"), Say("ok"), Say("ok"), Say("ok"))
	none := providers.StreamOptions{SessionID: "s", CacheRetention: providers.CacheRetentionNone}
	// No session id: no cache fields.
	u := usageOf(t, p, "aaaaaaaa", providers.StreamOptions{})
	assert.Zero(t, u.CacheRead+u.CacheWrite)
	// Prime the cache, then a "none" call neither reads nor updates it.
	usageOf(t, p, "aaaaaaaa", providers.StreamOptions{SessionID: "s"})
	un := usageOf(t, p, "zzzzzzzz", none)
	assert.Zero(t, un.CacheRead+un.CacheWrite)
	assert.Equal(t, int64(4), un.Input)
	next := usageOf(t, p, "aaaaaaaabbbbbbbb", providers.StreamOptions{SessionID: "s"})
	assert.Equal(t, int64(4), next.CacheRead, "the prefix still comes from the call before the none call")
}

func TestWithUsageIsExactAndKeepsCachePolicy(t *testing.T) {
	cost := protocol.Cost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}
	one, rs := int64(1), int64(5)
	exact := protocol.Usage{Input: 100, Output: 50, CacheRead: 7, CacheWrite: 8, CacheWrite1h: &one, Reasoning: &rs, TotalTokens: 999, Cost: cost}
	p := newProvider(t)
	p.Set(Say("ok").WithUsage(exact), Say("ok"))
	sess := providers.StreamOptions{SessionID: "s"}
	u1 := usageOf(t, p, "aaaaaaaa", sess)
	assert.Equal(t, exact, u1)
	// The first call still primed the cache, so the second reads its prefix.
	u2 := usageOf(t, p, "aaaaaaaabbbbbbbb", sess)
	assert.Equal(t, int64(4), u2.CacheRead)
	assert.Equal(t, protocol.Cost{}, u2.Cost)
	// The supplied value is copied.
	step := Say("ok").WithUsage(exact)
	*exact.Reasoning = 0
	p.Set(step)
	_, msg, _ := play(t, p)
	assert.Equal(t, int64(5), *msg.Usage.Reasoning)
}

func TestSetupErrorCarriesUsageEstimate(t *testing.T) {
	p := newProvider(t)
	p.Set(Func(func(context.Context, Call) (Step, error) { return Step{}, errors.New("no reply") }))
	items, msg, err := play(t, p, providers.StreamOptions{SessionID: "s"})
	require.Error(t, err)
	require.Len(t, items, 1)
	want := protocol.Usage{Input: 2, CacheWrite: 2, TotalTokens: 4}
	assert.Equal(t, want, msg.Usage)
	assert.Equal(t, want, items[0].Usage)
}
