package openai

import (
	"bytes"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"
	openaisdk "github.com/charmbracelet/openai-go"
)

const reasoningFieldKey = "ask_reasoning_field"

// The compatible wire has three names for reasoning text. Keep one block per
// choice and close it at the terminal chunk, even when text arrives between
// reasoning deltas.
func completionsStreamReasoning(chunk openaisdk.ChatCompletionChunk, yield func(fantasy.StreamPart) bool, ctx map[string]any) (map[string]any, bool) {
	for _, choice := range chunk.Choices {
		id := fmt.Sprint(choice.Index)
		stateKey := "ask_reasoning_started:" + id
		fieldKey := "ask_reasoning_field:" + id
		var delta map[string]json.RawMessage
		if err := json.Unmarshal([]byte(choice.Delta.RawJSON()), &delta); err != nil {
			_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return ctx, false
		}
		for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
			raw := bytes.TrimSpace(delta[field])
			if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
				continue
			}
			var text string
			if json.Unmarshal(raw, &text) != nil || text == "" {
				continue
			}
			if ctx[stateKey] != true {
				ctx[stateKey] = true
				ctx[fieldKey] = field
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: id}) {
					return ctx, false
				}
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: id, Delta: text}) {
				return ctx, false
			}
			break
		}
		if choice.FinishReason == "" || ctx[stateKey] != true {
			continue
		}
		ctx[stateKey] = false
		field, _ := ctx[fieldKey].(string)
		raw, _ := json.Marshal(field)
		metadata := fantasy.ProviderMetadata{fopenai.Name: &fopenai.ProviderMetadata{ExtraFields: map[string]json.RawMessage{reasoningFieldKey: raw}}}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: id, ProviderMetadata: metadata}) {
			return ctx, false
		}
	}
	return ctx, true
}
