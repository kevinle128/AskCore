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
