package acp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func hostFixture(t *testing.T, edit func(*agent.Config), write WriteEvent) *Host {
	t.Helper()
	h := NewHost(context.Background(), func(_ context.Context, id, cwd string) (*agent.Agent, error) {
		p, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := p.Model("faux-1")
		p.Set(faux.Say("first"), faux.Say("second"), faux.Say("third"))
		cfg := agent.Config{SessionID: id, LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream, Cwd: cwd}}
		if edit != nil {
			edit(&cfg)
		}
		return agent.New(cfg)
	}, write)
	t.Cleanup(func() { require.NoError(t, h.Close()) })
	return h
}
func hostUser(text string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}
func hostDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestHostPromptWaitsForPhysicalSettledWrite(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h := hostFixture(t, nil, func(ctx context.Context, _, _ string, ev protocol.Event) error {
		if ev.EventType() == protocol.TypeAgentSettled {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := s.Prompt(hostDeadline(t), hostUser("hi")); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("settled not observed")
	}
	select {
	case err := <-done:
		t.Fatalf("result overtook physical write: %v", err)
	default:
	}
	close(release)
	require.NoError(t, <-done)
}

type hostBrokenLog struct{ sessions.MemoryLog }

var errHostLog = errors.New("log fixture failure")

func (*hostBrokenLog) Append(...sessions.Entry) (sessions.CommitRef, error) {
	return sessions.CommitRef{}, errHostLog
}

func TestHostNoStartFailureStillWaitsForSettled(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	h := hostFixture(t, func(c *agent.Config) { c.NewContext = func() sessions.Writer { return &hostBrokenLog{} } }, func(_ context.Context, _, _ string, e protocol.Event) error {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, e.EventType())
		return nil
	})
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	result, err := s.Prompt(hostDeadline(t), hostUser("hi"))
	require.ErrorIs(t, err, errHostLog)
	require.NotEmpty(t, result.RunID)
	require.NotZero(t, result.Seq)
	mu.Lock()
	defer mu.Unlock()
	require.NotContains(t, kinds, protocol.TypeAgentStart)
	require.Contains(t, kinds, protocol.TypeAgentSettled)
}

func TestHostContinueWithoutEventsAndResetPrompt(t *testing.T) {
	h := hostFixture(t, nil, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	result, err := s.Continue(hostDeadline(t))
	require.ErrorIs(t, err, agent.ErrContinueEmpty)
	require.Empty(t, result.RunID)
	first, err := s.Prompt(hostDeadline(t), hostUser("first"))
	require.NoError(t, err)
	_, err = s.Continue(hostDeadline(t))
	require.ErrorIs(t, err, agent.ErrContinueFromAssistant)
	cut, err := s.Reset(hostDeadline(t))
	require.NoError(t, err)
	require.NotEqual(t, first.Epoch, cut.Cursor.Epoch)
	second, err := s.Prompt(hostDeadline(t), hostUser("second"))
	require.NoError(t, err)
	require.Equal(t, cut.Cursor.Epoch, second.Epoch)
	require.NotEqual(t, first.RunID, second.RunID)
}

func TestHostConcurrentSessionsAndOutputFailure(t *testing.T) {
	writeErr := errors.New("lost physical write")
	h := hostFixture(t, nil, func(_ context.Context, _, _ string, _ protocol.Event) error { return writeErr })
	a, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	b, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	require.NotEqual(t, a.ID, b.ID)
	require.NotSame(t, a.Agent(), b.Agent())
	_, err = a.Prompt(hostDeadline(t), hostUser("hi"))
	require.ErrorIs(t, err, writeErr)
	_, err = h.NewSession(hostDeadline(t), t.TempDir())
	require.Error(t, err)
}
