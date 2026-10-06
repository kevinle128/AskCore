package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
)

// These tests feed hand-written HTTP faults to the real Token Plan adapter
// through the same agent setup and run code as ask -p.

// runFault runs prompt in print mode with rt as the provider transport and
// returns the exit code, stdout and stderr.
func runFault(t *testing.T, rt http.RoundTripper, sigs <-chan os.Signal) (int, string, string) {
	t.Helper()
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt}
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

func TestFaultHTTPStatusIsOneRequestAndExit1(t *testing.T) {
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
			code, out, errb := runFault(t, rt, nil)
			assert.Equal(t, 1, code)
			assert.Empty(t, out)
			assert.Contains(t, errb, tc.want)
			assert.EqualValues(t, 1, n.Load(), "the adapter does not retry")
		})
	}
}

func TestFaultStreamEndsWithoutMessageStop(t *testing.T) {
	full := sseText("pong")
	cut := full[:strings.Index(full, "event:content_block_stop")]
	code, out, errb := runFault(t, sseResponse(http.StatusOK, cut), nil)
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
	code, out, errb := runFault(t, rt, nil)
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
