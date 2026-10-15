package providers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestNormalizeCompletionsToolCallID(t *testing.T) {
	t.Parallel()
	longPlain := strings.Repeat("a", 41)
	joined := "call_" + strings.Repeat("x", 40)
	piped := "call|" + strings.Repeat("x", 40)
	cases := []struct {
		name  string
		id    string
		model Model
		want  string
	}{
		{
			name:  "openai plain over 40 uses hash form",
			id:    longPlain,
			model: Model{Provider: ProviderOpenAI},
			want:  string([]rune(longPlain)[:31]) + "_" + ShortHash(longPlain),
		},
		{
			name:  "joined over 40 uses hash form",
			id:    piped,
			model: Model{Provider: ProviderTokenPlan},
			want:  string([]rune(joined)[:31]) + "_" + ShortHash(joined),
		},
		{
			name:  "non-openai plain id is not capped",
			id:    longPlain,
			model: Model{Provider: ProviderTokenPlan},
			want:  longPlain,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeCompletionsToolCallID(tc.id, tc.model, protocol.AssistantMessage{})
			assert.Equal(t, tc.want, got)
			if tc.model.Provider == ProviderOpenAI || strings.Contains(tc.id, "|") {
				require.LessOrEqual(t, len([]rune(got)), 40)
			}
		})
	}
}

func TestNormalizeAnthropicToolCallID(t *testing.T) {
	t.Parallel()
	id := "ab c\u00e9-" + strings.Repeat("x", 70)
	got := NormalizeAnthropicToolCallID(id, Model{}, protocol.AssistantMessage{})
	wantRunes := []rune("ab_c_-" + strings.Repeat("x", 70))
	if len(wantRunes) > 64 {
		wantRunes = wantRunes[:64]
	}
	assert.Equal(t, string(wantRunes), got)
	assert.LessOrEqual(t, len([]rune(got)), 64)
}

func TestNormalizeResponsesToolCallID(t *testing.T) {
	t.Parallel()
	target := OpenAIGPT55()
	item := "fc_original"
	cases := []struct {
		name   string
		id     string
		source protocol.AssistantMessage
		want   string
	}{
		{
			name:   "foreign native item becomes fc_hash",
			id:     "call1|" + item,
			source: protocol.AssistantMessage{Provider: ProviderOpenAI, Model: "other"},
			want:   "call1|fc_" + ShortHash(item),
		},
		{
			name:   "same model keeps item",
			id:     "call1|" + item,
			source: protocol.AssistantMessage{Provider: ProviderOpenAI, Model: ModelGPT55},
			want:   "call1|" + item,
		},
		{
			name:   "other provider turns pipe into underscore",
			id:     "call1|" + item,
			source: protocol.AssistantMessage{Provider: ProviderTokenPlan, Model: ModelDeepSeekFlash},
			want:   "call1_" + item,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, NormalizeResponsesToolCallID(tc.id, target, tc.source))
		})
	}
}

func TestDropResponsesItemID(t *testing.T) {
	t.Parallel()
	gpt := OpenAIGPT55()
	other := gpt
	other.ID = "other"
	cases := []struct {
		name     string
		itemID   string
		itemType string
		source   Model
		target   Model
		want     bool
	}{
		{name: "models differ", itemID: "fc_ok", itemType: "function_call", source: other, target: gpt, want: true},
		{name: "prefix mismatch", itemID: "msg_1", itemType: "function_call", source: gpt, target: gpt, want: true},
		{name: "keeps fc_ on same model", itemID: "fc_ok", itemType: "function_call", source: gpt, target: gpt, want: false},
		{name: "keeps ctc_ on custom", itemID: "ctc_ok", itemType: "custom_tool_call", source: gpt, target: gpt, want: false},
		{name: "custom rejects fc_", itemID: "fc_ok", itemType: "custom_tool_call", source: gpt, target: gpt, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, DropResponsesItemID(tc.itemID, tc.itemType, tc.source, tc.target))
		})
	}
}
