package faux

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const fixedNow int64 = 1_700_000_000_000

// fakeClock is a manual clock. After registers a waiter and signals armed;
// Advance fires the waiters that are due.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	armed   chan struct{}
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.UnixMilli(fixedNow), armed: make(chan struct{}, 1024)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at: c.now.Add(d), ch: ch})
	c.armed <- struct{}{}
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			kept = append(kept, w)
		}
	}
	c.waiters = kept
}

// waitArmed blocks until the producer waits on the clock.
func (c *fakeClock) waitArmed(t *testing.T) {
	t.Helper()
	select {
	case <-c.armed:
	case <-time.After(5 * time.Second):
		t.Fatal("producer did not wait on the clock")
	}
}

func newProvider(t *testing.T, opts ...Option) *Provider {
	t.Helper()
	p, err := New(opts...)
	require.NoError(t, err)
	return p
}

func userMsg(text string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}

func req(msgs ...protocol.Message) providers.TranscriptRequest {
	if len(msgs) == 0 {
		msgs = []protocol.Message{userMsg("hi")}
	}
	return providers.TranscriptRequest{Messages: msgs}
}

func defaultModel(t *testing.T, p *Provider) providers.Model {
	t.Helper()
	m, ok := p.Model(defaultModelID)
	require.True(t, ok)
	return m
}

// start begins one call with the default model and a one-message request.
func start(t *testing.T, p *Provider, ctx context.Context, opts ...providers.StreamOptions) *providers.Stream {
	t.Helper()
	var o providers.StreamOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	return p.Stream(ctx, defaultModel(t, p), req(), o)
}

func drain(s *providers.Stream) []providers.StreamItem {
	var out []providers.StreamItem
	for it := range s.Events() {
		out = append(out, it)
	}
	return out
}

func result(t *testing.T, s *providers.Stream) (protocol.AssistantMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, err := s.Result(ctx)
	require.NoError(t, ctx.Err(), "result did not settle")
	return msg, err
}

// play runs one call to the end and returns the items and the result.
func play(t *testing.T, p *Provider, opts ...providers.StreamOptions) ([]providers.StreamItem, protocol.AssistantMessage, error) {
	t.Helper()
	s := start(t, p, context.Background(), opts...)
	items := drain(s)
	msg, err := result(t, s)
	return items, msg, err
}

func kinds(items []providers.StreamItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Event.EventType()
	}
	return out
}

func sp(s string) *string { return &s }
