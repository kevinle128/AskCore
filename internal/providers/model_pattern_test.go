package providers_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestParseModelRef(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in      string
		want    providers.Ref
		wantErr bool
	}{
		{in: "anthropic/claude-opus-4-5", want: providers.Ref{Provider: "anthropic", ID: "claude-opus-4-5"}},
		{in: "openai/gpt-5.5@openai-responses", want: providers.Ref{Provider: "openai", ID: "gpt-5.5", API: providers.APIOpenAIResponses}},
		{in: "a/b@x", want: providers.Ref{Provider: "a", ID: "b", API: "x"}},
		{in: "gpt-5.5", want: providers.Ref{ID: "gpt-5.5"}},
		{in: "gpt-5.5@openai-responses", want: providers.Ref{ID: "gpt-5.5", API: providers.APIOpenAIResponses}},
		{in: "openrouter/qwen/qwen3:free", want: providers.Ref{Provider: "openrouter", ID: "qwen/qwen3:free"}},
		{in: "", wantErr: true},
		{in: "/x", wantErr: true},
		{in: "x/", wantErr: true},
		{in: "a/@api", wantErr: true},
		{in: "a/b@", wantErr: true},
		{in: "@api", wantErr: true},
	} {
		got, err := providers.ParseRef(tc.in)
		if tc.wantErr {
			require.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
}

func customRow(provider, id string, api providers.API) providers.Model {
	m := providers.Model{ID: id, Name: id, Provider: provider, API: api, BaseURL: "http://127.0.0.1:1/v1", Input: []string{"text"}}
	switch api {
	case providers.APIAnthropicMessages:
		m.Compat = providers.AnthropicCompat{}
	case providers.APIOpenAICompletions:
		m.Compat = providers.CompletionsCompat{}
	default:
		m.Compat = providers.ResponsesCompat{}
	}
	return m
}

// patternRows builds the rows of the resolve table through a real ModelSet,
// so the rows are in display order.
func patternRows(t *testing.T, extra ...providers.Model) []providers.Model {
	t.Helper()
	base := []providers.Model{
		providers.TokenPlanMessages(),
		providers.TokenPlanCompletions(),
		anthropicRow("claude-opus-4-5"),
		anthropicRow("claude-opus-4-5-20251101"),
		openAIRow("gpt-5.5"),
		openAIRow("gpt-5.5-mini"),
		customRow("local", "qwen3-coder:30b", providers.APIOpenAICompletions),
		customRow("local", "shared-model", providers.APIOpenAICompletions),
		customRow("lab", "shared-model", providers.APIOpenAICompletions),
		customRow("router", "x:max", providers.APIOpenAICompletions),
		customRow("moon", "kimi", providers.APIOpenAICompletions),
		customRow("moon", "kimi-highspeed", providers.APIOpenAICompletions),
		customRow("openrouter", "qwen/qwen3:free", providers.APIOpenAICompletions),
	}
	return providers.NewModelSet(append(base, extra...), nil, nil).All()
}

var errSyntax = errors.New("malformed pattern")

func TestResolveModelPattern(t *testing.T) {
	t.Parallel()
	rows := patternRows(t)
	tokenPlanID := providers.TokenPlanCompletions().ID
	for _, tc := range []struct {
		pattern string
		allow   bool
		want    string // provider/id@api of the row
		level   protocol.ThinkingLevel
		warning bool
		err     error // a sentinel class, or errSyntax for a malformed pattern
		choices []string
	}{
		{pattern: "local/qwen3-coder:30b", want: "local/qwen3-coder:30b@openai-completions"},
		{pattern: "router/x:max", want: "router/x:max@openai-completions"},
		{pattern: "kimi:high", want: "moon/kimi@openai-completions", level: protocol.ThinkingHigh},
		{pattern: "kimi:default", want: "moon/kimi@openai-completions", level: protocol.ThinkingDefault},
		{pattern: "anthropic/claude-opus-4-5:high", want: "anthropic/claude-opus-4-5@anthropic-messages", level: protocol.ThinkingHigh},
		{pattern: "gpt-5.5:hgih", err: providers.ErrUnknownModel},
		{pattern: "gpt-5.5:hgih", allow: true, want: "openai/gpt-5.5@openai-responses", warning: true},
		{pattern: "openrouter/qwen/qwen3:free", want: "openrouter/qwen/qwen3:free@openai-completions"},
		{pattern: "qwen/qwen3:free", want: "openrouter/qwen/qwen3:free@openai-completions"},
		{
			pattern: "shared-model", err: providers.ErrAmbiguousModel,
			choices: []string{"lab/shared-model@openai-completions", "local/shared-model@openai-completions"},
		},
		{pattern: "openai/GPT-5.5", want: "openai/gpt-5.5@openai-responses"},
		{pattern: "gpt-5.5", want: "openai/gpt-5.5@openai-responses"},
		{pattern: "alibaba-token-plan/" + tokenPlanID + "@openai-completions", want: "alibaba-token-plan/" + tokenPlanID + "@openai-completions"},
		{
			pattern: "alibaba-token-plan/" + tokenPlanID + "@openai-completions:high",
			want:    "alibaba-token-plan/" + tokenPlanID + "@openai-completions", level: protocol.ThinkingHigh,
		},
		{
			pattern: "alibaba-token-plan/" + tokenPlanID, err: providers.ErrAmbiguousModel,
			choices: []string{
				"alibaba-token-plan/" + tokenPlanID + "@anthropic-messages",
				"alibaba-token-plan/" + tokenPlanID + "@openai-completions",
			},
		},
		// An alias names its newest dated snapshot when the set has no alias
		// row; a dated id names the alias row when the set has no snapshot.
		{pattern: "anthropic/claude-opus-4-5-20251101", want: "anthropic/claude-opus-4-5-20251101@anthropic-messages"},
		{pattern: "gpt-5.5-2026-01-01", want: "openai/gpt-5.5@openai-responses"},
		{pattern: "nothing-like-this", err: providers.ErrUnknownModel},
		{pattern: "x/", err: errSyntax},
	} {
		got, err := providers.ResolvePattern(rows, tc.pattern, providers.PatternOptions{AllowInvalidLevel: tc.allow})
		if tc.err != nil {
			require.Error(t, err, tc.pattern)
			if tc.err == providers.ErrUnknownModel || tc.err == providers.ErrAmbiguousModel {
				require.ErrorIs(t, err, tc.err, tc.pattern)
			}
			if tc.choices != nil {
				var me *providers.ModelError
				require.ErrorAs(t, err, &me, tc.pattern)
				assert.ElementsMatch(t, tc.choices, me.Choices, tc.pattern)
			}
			continue
		}
		require.NoError(t, err, tc.pattern)
		m := got.Model
		assert.Equal(t, tc.want, m.Provider+"/"+m.ID+"@"+string(m.API), tc.pattern)
		assert.Equal(t, tc.level, got.Level, tc.pattern)
		assert.Equal(t, tc.warning, got.Warning != "", tc.pattern)
	}
}

func TestResolveModelPatternInvalidLevelErrorListsLevels(t *testing.T) {
	t.Parallel()
	_, err := providers.ResolvePattern(patternRows(t), "gpt-5.5:hgih", providers.PatternOptions{})
	require.ErrorIs(t, err, providers.ErrUnknownModel)
	assert.Contains(t, err.Error(), `"hgih" is not a thinking level`)
	assert.Contains(t, err.Error(), "(default, off, minimal, low, medium, high, xhigh, max)")
}

func TestResolveModelPatternCaseTwinsAreAmbiguous(t *testing.T) {
	t.Parallel()
	rows := patternRows(t,
		customRow("moon", "Kimi-K2", providers.APIOpenAICompletions),
		customRow("moon", "kimi-k2", providers.APIOpenAICompletions))
	_, err := providers.ResolvePattern(rows, "moon/KIMI-K2", providers.PatternOptions{})
	require.ErrorIs(t, err, providers.ErrAmbiguousModel)
	// An exact name still names one row.
	got, err := providers.ResolvePattern(rows, "moon/kimi-k2", providers.PatternOptions{})
	require.NoError(t, err)
	assert.Equal(t, "kimi-k2", got.Model.ID)
}

func TestResolveModelPatternFuzzyInTwoProvidersIsAmbiguous(t *testing.T) {
	t.Parallel()
	_, err := providers.ResolvePattern(patternRows(t), "shared", providers.PatternOptions{})
	require.ErrorIs(t, err, providers.ErrAmbiguousModel)
	got, err := providers.ResolvePattern(patternRows(t), "lab/shared", providers.PatternOptions{})
	require.NoError(t, err)
	assert.Equal(t, "lab", got.Model.Provider)
}

func TestResolveModelPatternReturnsACopy(t *testing.T) {
	t.Parallel()
	rows := patternRows(t)
	got, err := providers.ResolvePattern(rows, "gpt-5.5", providers.PatternOptions{})
	require.NoError(t, err)
	got.Model.Input[0] = "changed"
	for _, m := range rows {
		assert.NotContains(t, m.Input, "changed")
	}
}

func TestExpandModelScope(t *testing.T) {
	t.Parallel()
	rows := patternRows(t, anthropicRow("claude-sonnet-4-6"))
	got, problems := providers.ExpandScope(rows, []string{
		"anthropic/*",
		"*sonnet*:low",
		"gpt-5.5",
		"openai/gpt-5.5",
		"nothing-like-this",
		"[",
		"gpt-5.5-mini:hgih",
		"zzz*",
	})
	var names []string
	levels := map[string]protocol.ThinkingLevel{}
	for _, s := range got {
		names = append(names, s.Model.Provider+"/"+s.Model.ID)
		levels[s.Model.ID] = s.Level
	}
	assert.Equal(t, []string{
		"anthropic/claude-opus-4-5",
		"anthropic/claude-opus-4-5-20251101",
		"anthropic/claude-sonnet-4-6",
		"openai/gpt-5.5",
		"openai/gpt-5.5-mini",
	}, names)
	// The first entry that adds a row gives its level.
	assert.Equal(t, protocol.ThinkingLevel(""), levels["claude-sonnet-4-6"])
	assert.Equal(t, protocol.ThinkingLevel(""), levels["gpt-5.5-mini"])

	var entries []string
	for _, p := range problems {
		entries = append(entries, p.Entry)
		assert.NotEmpty(t, p.Text)
	}
	assert.Equal(t, []string{"nothing-like-this", "[", "gpt-5.5-mini:hgih", "zzz*"}, entries)
}

func TestExpandModelScopeGlobCarriesLevel(t *testing.T) {
	t.Parallel()
	rows := patternRows(t, anthropicRow("claude-sonnet-4-6"))
	// A suffix that is not a level stays part of the glob.
	got, problems := providers.ExpandScope(rows, []string{"*sonnet*:low", "x:*"})
	require.Empty(t, problems)
	require.Len(t, got, 2)
	assert.Equal(t, "claude-sonnet-4-6", got[0].Model.ID)
	assert.Equal(t, protocol.ThinkingLow, got[0].Level)
	assert.Equal(t, "x:max", got[1].Model.ID)
	assert.Equal(t, protocol.ThinkingLevel(""), got[1].Level)
}

func TestExpandModelScopeGlobCarriesDefault(t *testing.T) {
	t.Parallel()
	rows := patternRows(t, anthropicRow("claude-sonnet-4-6"))
	got, problems := providers.ExpandScope(rows, []string{"*sonnet*:default"})
	require.Empty(t, problems)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.ThinkingDefault, got[0].Level)
}
