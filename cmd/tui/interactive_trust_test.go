package main

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// trustClient records the trust calls of the program. Any call it does not
// list would panic, which the test reports as a failure.
type trustClient struct {
	tui.Client
	events chan tui.Message

	mu        sync.Mutex
	overrides []protocol.ACPTrustOverride
	asked     int
}

func (c *trustClient) Events() <-chan tui.Message { return c.events }
func (c *trustClient) Info() tui.ConnInfo {
	return tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "inst_0123456789abcdef"}
}
func (c *trustClient) Close() error { return nil }
func (c *trustClient) NewSession(_ context.Context, cwd string, trust protocol.ACPTrustOverride) (tui.SessionInfo, error) {
	c.mu.Lock()
	c.overrides = append(c.overrides, trust)
	c.mu.Unlock()
	return tui.SessionInfo{ID: "sess_5e1f0c2a9b7d4468", Role: protocol.ACPRoleDriver, Cwd: cwd}, nil
}
func (c *trustClient) TrustState(context.Context, string) (protocol.ACPTrustStateResult, error) {
	c.mu.Lock()
	c.asked++
	c.mu.Unlock()
	return protocol.ACPTrustStateResult{}, tui.ErrTrustNotAvailable
}
func (c *trustClient) State(context.Context) (protocol.ACPStateResult, error) {
	return protocol.ACPStateResult{}, nil
}
func (c *trustClient) Models(context.Context) (protocol.ACPModelsResult, error) {
	return protocol.ACPModelsResult{}, nil
}

func (c *trustClient) sent() ([]protocol.ACPTrustOverride, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]protocol.ACPTrustOverride(nil), c.overrides...), c.asked
}

// The trust override of the command line reaches every session/new of the
// program, and the program then asks no trust question.
func TestInteractiveTrustFlagReachesNewSession(t *testing.T) {
	for _, c := range []struct {
		name  string
		trust protocol.ACPTrustOverride
		asked int
	}{
		{"no_flag", "", 1},
		{"no_approve", protocol.ACPTrustDeny, 0},
		{"approve", protocol.ACPTrustApprove, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			require.NoError(t, err)
			t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
			require.NoError(t, pty.Setsize(master, &pty.Winsize{Rows: termRows, Cols: termCols}))
			var raw syncBuffer
			go func() { _, _ = io.Copy(&raw, master) }()

			sigs := make(chan os.Signal, 1)
			client := &trustClient{events: make(chan tui.Message)}
			t.Cleanup(func() { close(client.events) })
			var stderr syncBuffer
			code := make(chan int, 1)
			go func() {
				code <- runTerminal(terminalConfig{
					in: slave, out: slave, stderr: &stderr, sigs: sigs, cwd: t.TempDir(), home: t.TempDir(), version: "test",
					trust: c.trust,
					open:  func(context.Context) (tui.Client, error) { return client, nil },
				})
			}()
			require.Eventually(t, func() bool {
				overrides, _ := client.sent()
				return len(overrides) == 1
			}, exitWait, 20*time.Millisecond, "the program starts one session")
			require.Eventually(t, func() bool { return strings.Contains(raw.String(), "Ask anything") },
				exitWait, 20*time.Millisecond, "the editor shows")
			sigs <- syscall.SIGTERM
			select {
			case <-code:
			case <-time.After(exitWait):
				t.Fatal("the program did not end on SIGTERM")
			}
			overrides, asked := client.sent()
			require.Equal(t, []protocol.ACPTrustOverride{c.trust}, overrides)
			require.Equal(t, c.asked, asked)
		})
	}
}
