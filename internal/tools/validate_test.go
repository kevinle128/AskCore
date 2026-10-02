package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func echoRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	r := &tools.Registry{}
	require.NoError(t, r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	return r
}

func TestValidateMissingRequired(t *testing.T) {
	_, err := echoRegistry(t).Prepare("echo", json.RawMessage(`{}`))
	require.EqualError(t, err, "Validation failed for tool \"echo\":\n"+
		"  - text: must have required properties text\n"+
		"\n"+
		"Received arguments:\n"+
		"{}")
}

func TestValidateWrongType(t *testing.T) {
	_, err := echoRegistry(t).Prepare("echo", json.RawMessage(`{"text":[1]}`))
	require.EqualError(t, err, "Validation failed for tool \"echo\":\n"+
		"  - text: must be string\n"+
		"\n"+
		"Received arguments:\n"+
		"{\n  \"text\": [\n    1\n  ]\n}")
}

func TestValidateIssuesSortedByPath(t *testing.T) {
	params := `{"type":"object","required":["a"],"additionalProperties":false,"properties":{
		"a":{"type":"string"},"b":{"type":"integer","minimum":3}}}`
	_, err := prepareWith(t, params, `{"b":1,"c":true}`)
	require.EqualError(t, err, "Validation failed for tool \"probe\":\n"+
		"  - a: must have required properties a\n"+
		"  - b: must be >= 3\n"+
		"  - root: must not have additional properties\n"+
		"\n"+
		"Received arguments:\n"+
		"{\n  \"b\": 1,\n  \"c\": true\n}")
}

func TestValidateEchoIsCappedAt2KiB(t *testing.T) {
	big := strings.Repeat("a", 10*1024)
	_, err := echoRegistry(t).Prepare("echo", json.RawMessage(`{"text":["`+big+`"]}`))
	require.Error(t, err)

	head := "{\n  \"text\": [\n    \""
	want := "Validation failed for tool \"echo\":\n" +
		"  - text: must be string\n" +
		"\n" +
		"Received arguments:\n" +
		head + strings.Repeat("a", 2048-len(head)) +
		"\n... (truncated)"
	assert.Equal(t, want, err.Error())
}

func TestValidateInvalidJSON(t *testing.T) {
	_, err := echoRegistry(t).Prepare("echo", json.RawMessage(`{"text":`))
	require.EqualError(t, err, "Validation failed for tool \"echo\":\n"+
		"  - root: must be valid JSON\n"+
		"\n"+
		"Received arguments:\n"+
		`{"text":`)
}
