package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// answerSignal tells the test that the SDK parsed a line and wrote an answer.
type answerSignal struct{ done chan struct{} }

func (w answerSignal) Write(p []byte) (int, error) {
	select {
	case w.done <- struct{}{}:
	default:
	}
	return len(p), nil
}

// sdkTakes sends one line of the given size to a real SDK connection and tells
// whether the SDK parsed it.
func sdkTakes(t *testing.T, size int) bool {
	t.Helper()
	prefix := `{"jsonrpc":"2.0","id":1,"method":"probe","params":{"text":"`
	suffix := `"}}`
	line := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix + "\n"
	r, w := io.Pipe()
	defer func() { _ = r.Close(); _ = w.Close() }()
	out := answerSignal{done: make(chan struct{}, 1)}
	conn := sdk.NewConnection(func(context.Context, string, json.RawMessage) (any, *sdk.RequestError) {
		return struct{}{}, nil
	}, out, r)
	go func() { _, _ = io.WriteString(w, line) }()
	select {
	case <-out.done:
		return true
	case <-conn.Done():
		return false
	case <-time.After(30 * time.Second):
		t.Fatal("the SDK neither answered nor closed")
		return false
	}
}

// TestSDKAcceptsLeaderLineLimit guards the Ask copy of the SDK. The leader
// carries ACP messages of 64 MiB plus 1 MiB of route data. Upstream stops at
// 10 MiB, which would close the shared connection on a large prompt.
func TestSDKAcceptsLeaderLineLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("moves 65 MiB lines")
	}
	const limit = 65 << 20
	require.True(t, sdkTakes(t, 11<<20), "a line above the upstream limit")
	require.True(t, sdkTakes(t, limit), "a line at the limit")
	require.False(t, sdkTakes(t, limit+1), "one byte over the limit closes the connection")
}
