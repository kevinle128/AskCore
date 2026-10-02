package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDeclJSON(t *testing.T) {
	got, err := json.Marshal(ToolDecl{Name: "echo", Description: "d"})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo","description":"d","parameters":{}}`, string(got))

	got, err = json.Marshal(ToolDecl{Name: "echo", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo","description":"d","parameters":{"type":"object"}}`, string(got))

	got, err = json.Marshal(ToolRef{Name: "echo"})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo"}`, string(got))
}
