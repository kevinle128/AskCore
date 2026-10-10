package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/tui"
	"AskCore/internal/tuiclient/rpc"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// scriptedLink is a transport whose peer answers initialize and nothing else.
// onNext runs in place of every read after the answer, so a test can make the
// reader fail or panic.
type scriptedLink struct {
	mu       sync.Mutex
	answered bool
	replies  chan json.RawMessage
	closed   chan struct{}
	once     sync.Once
	onNext   func() (json.RawMessage, error)
}

func newScriptedLink(onNext func() (json.RawMessage, error)) *scriptedLink {
	return &scriptedLink{replies: make(chan json.RawMessage, 8), closed: make(chan struct{}), onNext: onNext}
}

func (l *scriptedLink) Send(raw json.RawMessage) error {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(raw, &m) == nil && m.Method == "initialize" {
		l.replies <- json.RawMessage(`{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":{"protocolVersion":1}}`)
	}
	return nil
}

func (l *scriptedLink) Next() (json.RawMessage, error) {
	l.mu.Lock()
	answered := l.answered
	l.mu.Unlock()
	if !answered || l.onNext == nil {
		select {
		case r := <-l.replies:
			l.mu.Lock()
			l.answered = true
			l.mu.Unlock()
			return r, nil
		case <-l.closed:
			return nil, io.EOF
		}
	}
	return l.onNext()
}

func (l *scriptedLink) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func drain(t *testing.T, c *Client, until func(tui.Message) bool) []tui.Message {
	t.Helper()
	var got []tui.Message
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m, ok := <-c.Events():
			if !ok {
				t.Fatalf("the event stream closed; got %v", got)
			}
			got = append(got, m)
			if until(m) {
				return got
			}
		case <-timeout:
			t.Fatalf("timeout; got %v", got)
		}
	}
}

// A panic in the goroutine that reads the link must reach the UI as a message.
// It must not crash the process, because the UI has to restore the terminal.
func TestClientGoroutinePanicBecomesError(t *testing.T) {
	// The first read gives the initialize answer; the next read panics.
	link := newScriptedLink(func() (json.RawMessage, error) { panic("reader bug") })
	c, err := Open(context.Background(), Config{
		NoReconnect: true,
		Dial: func(context.Context) (Link, error) {
			return Link{Transport: link, Info: tui.ConnInfo{Mode: tui.ModeInProcess}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	msgs := drain(t, c, func(m tui.Message) bool {
		e, ok := m.(tui.ClientErrorMsg)
		return ok && e.Fatal
	})
	var found *rpc.PanicError
	for _, m := range msgs {
		if e, ok := m.(tui.ClientErrorMsg); ok {
			var pe *rpc.PanicError
			if errors.As(e.Err, &pe) {
				found = pe
			}
		}
	}
	require.NotNil(t, found, "the panic of the reader is reported as an error: %v", msgs)
	require.Contains(t, found.Error(), "reader bug")
}

// Every goroutine of the client starts through the same recover wrapper.
func TestClientSafelyReportsPanic(t *testing.T) {
	link := newScriptedLink(nil)
	c, err := Open(context.Background(), Config{
		NoReconnect: true,
		Dial: func(context.Context) (Link, error) {
			return Link{Transport: link, Info: tui.ConnInfo{Mode: tui.ModeInProcess}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	c.goSafe("test goroutine", false, func() { panic("boom") })
	msgs := drain(t, c, func(m tui.Message) bool {
		e, ok := m.(tui.ClientErrorMsg)
		return ok && !e.Fatal
	})
	last := msgs[len(msgs)-1].(tui.ClientErrorMsg)
	var pe *rpc.PanicError
	require.ErrorAs(t, last.Err, &pe)
	require.Equal(t, "test goroutine", pe.Where)
}

func TestReconnectBackoffDoublesToMaximum(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	first := newScriptedLink(func() (json.RawMessage, error) { return nil, io.EOF })
	c, err := Open(context.Background(), Config{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     40 * time.Millisecond,
		Dial: func(context.Context) (Link, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			switch {
			case calls == 1:
				return Link{Transport: first, Info: tui.ConnInfo{Mode: tui.ModeLeader}}, nil
			case calls <= 6:
				return Link{}, errors.New("no leader")
			}
			return Link{Transport: newScriptedLink(nil), Info: tui.ConnInfo{Mode: tui.ModeLeader}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	var waits []time.Duration
	msgs := drain(t, c, func(m tui.Message) bool {
		if r, ok := m.(tui.ReconnectingMsg); ok {
			require.Equal(t, len(waits)+1, r.Attempt, "attempts count from 1")
			waits = append(waits, r.Wait)
		}
		cm, ok := m.(tui.ConnectedMsg)
		return ok && len(waits) > 0 && cm.Info.Mode == tui.ModeLeader
	})
	require.Equal(t, []time.Duration{10, 20, 40, 40, 40}, scale(waits), "1 s doubling to 30 s, in the units of this test")
	require.IsType(t, tui.ConnectedMsg{}, msgs[0])
	require.IsType(t, tui.DisconnectedMsg{}, msgs[1])
}

func scale(d []time.Duration) []time.Duration {
	out := make([]time.Duration, len(d))
	for i, v := range d {
		out[i] = v / time.Millisecond
	}
	return out
}

func TestCloseEndsReconnectLoopAndEventStream(t *testing.T) {
	first := newScriptedLink(func() (json.RawMessage, error) { return nil, io.EOF })
	var mu sync.Mutex
	calls := 0
	c, err := Open(context.Background(), Config{
		InitialBackoff: time.Hour, // the loop would wait for ever
		Dial: func(ctx context.Context) (Link, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return Link{Transport: first}, nil
			}
			return Link{}, errors.New("no leader")
		},
	})
	require.NoError(t, err)
	drain(t, c, func(m tui.Message) bool { _, ok := m.(tui.ReconnectingMsg); return ok })
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool {
		select {
		case _, open := <-c.Events():
			return !open
		default:
			return false
		}
	}, 5*time.Second, time.Millisecond, "Events closes after Close")
	// A call after Close fails and sends nothing.
	_, err = c.Prompt("late")
	require.ErrorIs(t, err, tui.ErrClosed)
}

func TestMailboxNeverBlocksThePusher(t *testing.T) {
	m := newMailbox()
	done := make(chan struct{})
	go func() { defer close(done); m.pump() }()
	const n = 5000
	for i := range n {
		m.push(tui.PromptDoneMsg{ID: tui.PromptID(i)}) // nobody reads yet
	}
	for i := range n {
		msg := <-m.out
		require.Equal(t, tui.PromptID(i), msg.(tui.PromptDoneMsg).ID, "order is kept")
	}
	m.close()
	<-done
	m.push(tui.ConnectedMsg{}) // after close: dropped, no panic
	_, open := <-m.out
	require.False(t, open)
}

func TestTranslateMapsErrors(t *testing.T) {
	require.ErrorIs(t, translate(rpc.ErrConnectionLost), tui.ErrOutcomeUnknown)
	var re *tui.RequestError
	require.ErrorAs(t, translate(&rpc.Fault{Code: -32015, Kind: "not_driver", Message: "no"}), &re)
	require.Equal(t, "not_driver", re.Kind)
	require.Equal(t, -32015, re.Code)
	require.NoError(t, translate(nil))
}
