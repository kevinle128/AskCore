package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestEchoReturnsOneTextBlock(t *testing.T) {
	r := echoRegistry(t)
	args, err := r.Prepare("echo", json.RawMessage(`{"text":5}`))
	require.NoError(t, err)

	res, err := tools.Echo{}.Execute(context.Background(), tools.Context{CallID: "c1"}, args)
	require.NoError(t, err)
	assert.Equal(t, protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "5"}}}, res)
}

func TestEchoDeclRequiresText(t *testing.T) {
	b, err := json.Marshal(tools.Echo{}.Decl())
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"echo","description":"Return the given text unchanged.","parameters":
		{"type":"object","properties":{"text":{"type":"string","description":"The text to return."}},"required":["text"]}}`, string(b))
}

func TestEchoStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := tools.Echo{}.Execute(ctx, tools.Context{}, json.RawMessage(`{"text":"hi"}`))
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, protocol.ToolExecutionResult{}, res)
}
