package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/sessions"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// These tests feed hand-written HTTP faults to the real Token Plan adapter
// through the same agent setup and run code as ask -p.

// runFault runs prompt in print mode with rt as the provider transport and
// returns the exit code, stdout and stderr.
func runFault(t *testing.T, rt http.RoundTripper, sigs <-chan os.Signal) (int, string, string) {
	return runFaultWait(t, rt, sigs, nil)
}

func runFaultWait(t *testing.T, rt http.RoundTripper, sigs <-chan os.Signal, delays *[]time.Duration) (int, string, string) {
	t.Helper()
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt, wait: func(ctx context.Context, d time.Duration) error {
		if delays != nil {
			*delays = append(*delays, d)
		}
		return ctx.Err()
	}}
	ag, err := newHeadlessAgent(o, func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "test-key"
		}
		return ""
	})
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"ping"}, modePrint, &out, &errb, sigs)
	return code, out.String(), errb.String()
}

// counted counts the requests that reach rt.
func counted(rt http.RoundTripper, n *atomic.Int32) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		n.Add(1)
		return rt.RoundTrip(r)
	}
}

func TestFaultHTTPStatusRetriesFiveTimesThenExits1(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"rate limit", http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, "HTTP 429"},
		{"server error", http.StatusInternalServerError, `{"type":"error","error":{"type":"api_error","message":"boom"}}`, "HTTP 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			rt := counted(rtFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status,
					Status:     http.StatusText(tc.status),
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(tc.body)),
					Request:    r,
				}, nil
			}), &n)
			var delays []time.Duration
			code, out, errb := runFaultWait(t, rt, nil, &delays)
			assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}, delays)
			assert.Equal(t, 1, code)
			assert.Empty(t, out)
			assert.Contains(t, errb, tc.want)
			assert.EqualValues(t, 6, n.Load(), "the agent retries five times")
		})
	}
}

func TestFaultStreamEndsWithoutMessageStop(t *testing.T) {
	full := sseText("pong")
	cut := full[:strings.Index(full, "event:content_block_stop")]
	var n atomic.Int32
	code, out, errb := runFault(t, counted(sseResponse(http.StatusOK, cut), &n), nil)
	assert.EqualValues(t, 1, n.Load(), "a clean closed stream is not retried")
	assert.Equal(t, 1, code, "out=%q err=%q", out, errb)
	assert.Contains(t, errb, "stream ended without a terminal event")
}

func TestFaultBadJSONEvent(t *testing.T) {
	body := strings.Replace(sseText("pong"), `"text_delta","text":"pong"}}`, `"text_delta","text":`, 1)
	code, out, errb := runFault(t, sseResponse(http.StatusOK, body), nil)
	assert.Equal(t, 1, code, "out=%q err=%q", out, errb)
	assert.Contains(t, errb, "unexpected end of JSON input")
}

// errAfter is a body that gives its data, then fails like a reset connection.
type errAfter struct{ r io.Reader }

func (e *errAfter) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, syscall.ECONNRESET
	}
	return n, err
}
func (e *errAfter) Close() error { return nil }

func TestFaultConnectionResetMidStream(t *testing.T) {
	full := sseText("pong")
	half := full[:strings.Index(full, "event:content_block_delta")]
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "OK",
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &errAfter{r: strings.NewReader(half)},
			Request:    r,
		}, nil
	})
	var n atomic.Int32
	var delays []time.Duration
	code, out, errb := runFaultWait(t, counted(rt, &n), nil, &delays)
	assert.EqualValues(t, 6, n.Load())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}, delays)
	assert.Equal(t, 1, code, "out=%q err=%q", out, errb)
	assert.Contains(t, errb, "connection reset")
}

// blockingBody gives head, then blocks until the request is canceled.
type blockingBody struct {
	head io.Reader
	done <-chan struct{}
	sent chan<- struct{}
	once atomic.Bool
}

func (b *blockingBody) Read(p []byte) (int, error) {
	if n, err := b.head.Read(p); n > 0 || !errors.Is(err, io.EOF) {
		return n, err
	}
	if b.once.CompareAndSwap(false, true) {
		close(b.sent)
	}
	<-b.done
	return 0, errors.New("request canceled")
}
func (b *blockingBody) Close() error { return nil }

func TestFaultSIGINTMidStreamExits130(t *testing.T) {
	full := sseText("pong")
	head := full[:strings.Index(full, "event:content_block_stop")]
	sent := make(chan struct{})
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "OK",
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &blockingBody{head: strings.NewReader(head), done: r.Context().Done(), sent: sent},
			Request:    r,
		}, nil
	})
	sigs := make(chan os.Signal, 1)
	go func() {
		select {
		case <-sent:
		case <-time.After(5 * time.Second):
		}
		sigs <- os.Interrupt
	}()
	code, out, _ := runFault(t, rt, sigs)
	assert.Equal(t, 130, code)
	assert.Empty(t, out, "an interrupted run prints no reply")
}

func TestFaultHTTP429ThenSuccessRetriesAndExits0(t *testing.T) {
	var requests atomic.Int32
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)), Request: r}, nil
		}
		return sseResponse(http.StatusOK, sseText("pong")).RoundTrip(r)
	})
	var delays []time.Duration
	code, out, errb := runFaultWait(t, rt, nil, &delays)
	assert.Equal(t, 0, code, "stderr=%s", errb)
	assert.Equal(t, "pong\n", out)
	assert.Empty(t, errb)
	assert.EqualValues(t, 2, requests.Load())
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
}

// TestRefusedConnectionRecoversAfterBackoff uses a real closed loopback port.
// The wait seam opens that port after the adapter reports the dial failure.
func TestRefusedConnectionRecoversAfterBackoff(t *testing.T) {
	t.Run("headless setup", func(t *testing.T) { testRefusedConnectionRecovery(t, false) })
	t.Run("durable session", func(t *testing.T) { testRefusedConnectionRecovery(t, true) })
}

// The first run checks the CLI composition. The second exposes its log through
// the existing Agent constructor, with the same real adapter and auth runner.
func testRefusedConnectionRecovery(t *testing.T, durable bool) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	received := make(chan []byte, 1)
	var requests [][]byte
	var dialErrors []error
	var delays []time.Duration
	var server *httptest.Server
	var events []protocol.Event
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			return nil, readErr
		}
		requests = append(requests, body)
		req := r.Clone(r.Context())
		req.URL = &url.URL{Scheme: "http", Host: address, Path: r.URL.Path}
		req.Host = address
		req.Body = io.NopCloser(bytes.NewReader(body))
		response, roundErr := transport.RoundTrip(req)
		dialErrors = append(dialErrors, roundErr)
		return response, roundErr
	})
	opts := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt, wait: func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		if err := ctx.Err(); err != nil {
			return err
		}
		require.Nil(t, server, "there is only one backoff")
		open, listenErr := net.Listen("tcp", address)
		require.NoError(t, listenErr)
		server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusBadRequest)
				return
			}
			received <- body
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, sseText("pong"))
		}))
		require.NoError(t, server.Listener.Close())
		server.Listener = open
		server.Start()
		t.Cleanup(server.Close)
		return nil
	}}
	var ag *agent.Agent
	var log *sessions.MemoryLog
	if durable {
		log = &sessions.MemoryLog{}
		registry := providers.NewRegistry()
		registry.RegisterProvider(anthropic.New(anthropic.WithHTTPClient(&http.Client{Transport: rt})))
		store, storeErr := settings.NewAuthStore(t.TempDir())
		require.NoError(t, storeErr)
		service := &auth.Service{Store: store}
		cfg := app.BindAuth(agent.Config{
			Registry: registry, NewContext: func() sessions.Writer { return log },
			LoopConfig: agent.LoopConfig{Model: anthropic.Model(), Options: providers.StreamOptions{APIKey: "test-key", Reasoning: protocol.ThinkingMedium}, Wait: opts.wait},
		}, service, registry)
		ag, err = agent.New(cfg)
	} else {
		ag, err = newHeadlessAgent(opts, func(key string) string {
			if key == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
				return "test-key"
			}
			return ""
		})
	}
	require.NoError(t, err)
	unsubscribe := ag.Subscribe(func(event protocol.Event) error { events = append(events, event); return nil })
	defer unsubscribe()
	var stdout, stderr bytes.Buffer
	code := runHeadless(ag, []string{"ping"}, modePrint, &stdout, &stderr, nil)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Equal(t, "pong\n", stdout.String())
	require.Empty(t, stderr.String())
	require.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
	require.Len(t, requests, 2)
	require.Len(t, dialErrors, 2)
	require.ErrorIs(t, dialErrors[0], syscall.ECONNREFUSED, "the operating system refused the first connection")
	require.NoError(t, dialErrors[1])
	require.Equal(t, requests[0], requests[1], "a retry prepares the same committed request")
	require.Equal(t, requests[1], <-received, "the real HTTP server got the prepared bytes")
	var cycles, retries, successes int
	for _, event := range events {
		switch event := event.(type) {
		case *protocol.CycleStart:
			cycles++
		case *protocol.AutoRetryStart:
			retries++
			require.Equal(t, 1, event.Attempt)
			require.Equal(t, 5, event.MaxAttempts)
			require.EqualValues(t, 500, event.DelayMs)
			require.Contains(t, event.ErrorMessage, "connection refused")
		case *protocol.AutoRetryEnd:
			successes++
			require.True(t, event.Success)
			require.Equal(t, 1, event.Attempt)
		}
	}
	require.Equal(t, 1, cycles, "recovery stays in one input cycle")
	require.Equal(t, 1, retries)
	require.Equal(t, 1, successes)
	if log != nil {
		var scheduled []sessions.RetryScheduled
		var started []sessions.RetryStarted
		var turns []sessions.TurnOpened
		var attempts []sessions.AttemptSettled
		for _, entry := range log.Entries() {
			switch entry := entry.(type) {
			case sessions.RetryScheduled:
				scheduled = append(scheduled, entry)
			case sessions.RetryStarted:
				started = append(started, entry)
			case sessions.TurnOpened:
				turns = append(turns, entry)
			case sessions.AttemptSettled:
				attempts = append(attempts, entry)
			}
		}
		require.Len(t, scheduled, 1, "one durable transport retry")
		require.Equal(t, providers.CodeTransport, scheduled[0].Failure.Code)
		require.Equal(t, 1, scheduled[0].Retry)
		require.EqualValues(t, 500, scheduled[0].DelayMs)
		require.Len(t, turns, 1, "retry stays inside the same Ask turn")
		require.Equal(t, turns[0].CycleID, scheduled[0].CycleID)
		require.Equal(t, 1, scheduled[0].Turn)
		require.Len(t, started, 1)
		require.Equal(t, scheduled[0].RetryID, started[0].RetryID)
		require.Len(t, attempts, 2)
		require.Equal(t, "failed", attempts[0].Outcome)
		require.NotNil(t, attempts[0].Failure)
		require.Equal(t, providers.CodeTransport, attempts[0].Failure.Code)
		require.Equal(t, "completed", attempts[1].Outcome)
	}

}
