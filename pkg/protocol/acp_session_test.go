package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionOptionsWireRoundTrip(t *testing.T) {
	assert.Equal(t, "ask.dev/session", ACPSessionOptionsMetaKey)
	assert.Equal(t, 256*1024, ACPSessionPromptMaxBytes)
	assert.Equal(t, "context-file", CustomContextFiles)
	assert.Equal(t, "skill-list", CustomSkillList)

	o := ACPSessionOptions{SystemPrompt: "be brief", AppendSystemPrompt: []string{"a", "b"}, Tools: []string{"read", "grep"}, NoTools: true}
	got, raw := wireRoundTrip(t, o)
	assert.Equal(t, o, got)
	assert.JSONEq(t, `{"systemPrompt":"be brief","appendSystemPrompt":["a","b"],"tools":["read","grep"],"noTools":true}`, raw)

	_, raw = wireRoundTrip(t, ACPSessionOptions{})
	assert.JSONEq(t, `{}`, raw, "every field is omitted when empty")
}

func TestSessionOptionsIsZero(t *testing.T) {
	assert.True(t, ACPSessionOptions{}.IsZero())
	assert.True(t, ACPSessionOptions{AppendSystemPrompt: []string{}, Tools: []string{}}.IsZero())
	assert.False(t, ACPSessionOptions{SystemPrompt: "x"}.IsZero())
	assert.False(t, ACPSessionOptions{AppendSystemPrompt: []string{"x"}}.IsZero())
	assert.False(t, ACPSessionOptions{Tools: []string{"read"}}.IsZero())
	assert.False(t, ACPSessionOptions{NoTools: true}.IsZero())
}

func TestSessionModelOptionsWire(t *testing.T) {
	assert.Equal(t, "ask.dev/appliedOptions", ACPAppliedOptionsMetaKey)
	assert.Equal(t, 256, ACPSessionModelMaxBytes)
	assert.Equal(t, 64, ACPSessionModelsMax)

	o := ACPSessionOptions{Model: "anthropic/claude-x:high", Thinking: ThinkingLow, Models: []string{"anthropic/*", "gpt-5.5:low"}}
	got, raw := wireRoundTrip(t, o)
	assert.Equal(t, o, got)
	assert.JSONEq(t, `{"model":"anthropic/claude-x:high","thinking":"low","models":["anthropic/*","gpt-5.5:low"]}`, raw)
	assert.Equal(t, ACPAppliedOptions{Model: o.Model, Thinking: o.Thinking, Models: o.Models}, o.ModelOptions())

	assert.False(t, ACPSessionOptions{Model: "m"}.IsZero())
	assert.False(t, ACPSessionOptions{Thinking: ThinkingHigh}.IsZero())
	assert.False(t, ACPSessionOptions{Models: []string{"a"}}.IsZero())
	assert.True(t, ACPSessionOptions{Models: []string{}}.IsZero())
	assert.True(t, ACPAppliedOptions{}.IsZero())

	_, raw = wireRoundTrip(t, ACPAppliedOptions{Model: "m"})
	assert.JSONEq(t, `{"model":"m"}`, raw, "an empty echo field is omitted")
}

func TestModelWireFieldsAreOptional(t *testing.T) {
	_, raw := wireRoundTrip(t, ACPModelRequest{SessionID: "s", ModelID: "p/m@a"})
	assert.JSONEq(t, `{"sessionId":"s","modelId":"p/m@a"}`, raw, "an older host sees no new field")
	req, raw := wireRoundTrip(t, ACPModelRequest{SessionID: "s", ModelID: "p/m@a", ThinkingLevel: ThinkingHigh})
	assert.Equal(t, ThinkingHigh, req.ThinkingLevel)
	assert.JSONEq(t, `{"sessionId":"s","modelId":"p/m@a","thinkingLevel":"high"}`, raw)

	res := ACPModelsResult{Current: "p/m@a", Models: []ACPModel{}, Scope: []ACPScopedModel{{ModelID: "p/m@a", ThinkingLevel: ThinkingLow}, {ModelID: "p/n@a"}},
		Diagnostics: []ACPDiagnostic{{Layer: "user", Path: "/h/models.json", Key: "providers", Text: "bad"}}}
	back, raw := wireRoundTrip(t, res)
	assert.Equal(t, res, back)
	assert.Contains(t, raw, `"scope":[{"modelId":"p/m@a","thinkingLevel":"low"},{"modelId":"p/n@a"}]`)
	_, raw = wireRoundTrip(t, ACPModelsResult{Current: "", Models: []ACPModel{}})
	assert.JSONEq(t, `{"current":"","models":[]}`, raw)
}

func TestLevelDroppedText(t *testing.T) {
	d := ACPLevelDropped{ModelID: "local/qwen3-coder:30b@openai-completions", Level: ThinkingHigh}
	assert.Equal(t, "local/qwen3-coder:30b has no thinking; level high is not used.", d.Text())
	assert.Equal(t, "p/m has no thinking; level low is not used.", ACPLevelDropped{ModelID: "p/m", Level: ThinkingLow}.Text())
	st, raw := wireRoundTrip(t, ACPStateResult{SessionID: "s", LevelDropped: &d})
	assert.Equal(t, &d, st.LevelDropped)
	assert.Contains(t, raw, `"levelDropped":{"modelId":"local/qwen3-coder:30b@openai-completions","level":"high"}`)
}
