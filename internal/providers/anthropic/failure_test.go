package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// These tests send hand-written HTTP responses through the real adapter and
// read the typed failure from the stream result.

// respond returns a transport that answers every request with status, headers
// and body.
func respond(status int, header http.Header, body string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
}

// failureOf runs one request on rt and returns the failure of the result.
func failureOf(t *testing.T, rt http.RoundTripper) *providers.Failure {
	t.Helper()
	p := New(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithEnv(func(k string) (string, bool) { return "test-key", k == askKeyEnv }),
		WithNow(func() int64 { return 1_700_000_000_000 }),
	)
	msg, err := resultOf(t, p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{}))
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	f, ok := providers.AsFailure(err)
	require.Truef(t, ok, "no *providers.Failure in %v", err)
	return f
}

func jsonHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

func TestAnthropic429WithRetryAfterIsRateLimit(t *testing.T) {
	h := jsonHeader()
	h.Set("Retry-After", "7")
	f := failureOf(t, respond(http.StatusTooManyRequests, h,
		`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	assert.Equal(t, providers.CodeRateLimit, f.Code)
	assert.Equal(t, http.StatusTooManyRequests, f.Status)
	assert.Equal(t, 7*time.Second, f.RetryAfter)
	assert.Contains(t, f.Error(), "HTTP 429")
}

func TestAnthropic429WithoutRetryAfterHasNoDelay(t *testing.T) {
	f := failureOf(t, respond(http.StatusTooManyRequests, jsonHeader(),
		`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	assert.Equal(t, providers.CodeRateLimit, f.Code)
	assert.Zero(t, f.RetryAfter)
}

func TestAnthropic529OverloadedIsServer(t *testing.T) {
	f := failureOf(t, respond(529, jsonHeader(),
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	assert.Equal(t, providers.CodeServer, f.Code)
	assert.Equal(t, 529, f.Status)
}

func TestAnthropic500IsServer(t *testing.T) {
	f := failureOf(t, respond(http.StatusInternalServerError, jsonHeader(),
		`{"type":"error","error":{"type":"api_error","message":"boom"}}`))
	assert.Equal(t, providers.CodeServer, f.Code)
	assert.Equal(t, http.StatusInternalServerError, f.Status)
}

func TestAnthropicUsageLimitIsQuota(t *testing.T) {
	f := failureOf(t, respond(http.StatusTooManyRequests, jsonHeader(),
		`{"type":"error","error":{"type":"rate_limit_error","message":"Usage limit reached for your plan. Try again after the reset."}}`))
	assert.Equal(t, providers.CodeQuota, f.Code, "quota wording wins over the 429 status")
	assert.ErrorIs(t, f, providers.ErrAllowanceExhausted)
}

func TestAnthropicAuthStatusIsAuth(t *testing.T) {
	f := failureOf(t, respond(http.StatusUnauthorized, jsonHeader(),
		`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	assert.Equal(t, providers.CodeAuth, f.Code)
	assert.ErrorIs(t, f, providers.ErrAuthentication)
}

func TestAnthropicContextOverflowIsContextWindow(t *testing.T) {
	f := failureOf(t, respond(http.StatusBadRequest, jsonHeader(),
		`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens > 200000 maximum"}}`))
	assert.Equal(t, providers.CodeContextWindow, f.Code)
}

func TestAnthropicBadRequestIsInvalidRequest(t *testing.T) {
	f := failureOf(t, respond(http.StatusBadRequest, jsonHeader(),
		`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: Field required"}}`))
	assert.Equal(t, providers.CodeInvalidRequest, f.Code)
}

func TestUnmappedHTTPStatusIsHTTPStatusCode(t *testing.T) {
	f := failureOf(t, respond(http.StatusTeapot, jsonHeader(), `{"error":{"message":"short and stout"}}`))
	assert.Equal(t, "HTTP_418", f.Code)
	assert.NotEqual(t, providers.CodeUnknown, f.Code)
	assert.Equal(t, http.StatusTeapot, f.Status)
}

func TestUnknownInBandErrorIsServer(t *testing.T) {
	body := sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`) +
		sse("error", `{"type":"error","error":{"type":"mystery_error","message":"something odd"}}`)
	f := failureOf(t, respond(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body))
	assert.Equal(t, providers.CodeServer, f.Code)
	assert.Zero(t, f.Status, "an in-band error has no HTTP status")
}

func TestAnthropicOverloadedInBandErrorIsServer(t *testing.T) {
	body := sse("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	f := failureOf(t, respond(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body))
	assert.Equal(t, providers.CodeServer, f.Code)
}

// resetAfter is a body that gives its data and then fails like a reset
// connection.
type resetAfter struct{ r io.Reader }

func (b *resetAfter) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, syscall.ECONNRESET
	}
	return n, err
}
func (b *resetAfter) Close() error { return nil }

func TestAnthropicResetMidBodyIsTransport(t *testing.T) {
	full := textSSE("end_turn")
	half := full[:strings.Index(full, "event: content_block_delta")]
	f := failureOf(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "OK",
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &resetAfter{r: strings.NewReader(half)},
			Request:    r,
		}, nil
	}))
	assert.Equal(t, providers.CodeTransport, f.Code)
	assert.NotEqual(t, providers.CodeStreamClosed, f.Code)
	assert.Contains(t, f.Error(), "connection reset")
}

func TestAnthropicCleanEOFIsStreamClosed(t *testing.T) {
	full := textSSE("end_turn")
	cut := full[:strings.Index(full, "event: content_block_stop")]
	f := failureOf(t, respond(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, cut))
	assert.Equal(t, providers.CodeStreamClosed, f.Code)
	assert.Contains(t, f.Error(), "stream ended without a terminal event")
	assert.ErrorIs(t, f, providers.ErrStreamIncomplete)
}

func TestAnthropicContentlessStopIsEmptyResponse(t *testing.T) {
	body := sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`) +
		sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":0}}`) +
		sse("message_stop", `{"type":"message_stop"}`)
	f := failureOf(t, respond(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body))
	assert.Equal(t, providers.CodeEmptyResponse, f.Code)
}

func TestAnthropicContentlessLengthStaysSuccess(t *testing.T) {
	body := sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`) +
		sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":0}}`) +
		sse("message_stop", `{"type":"message_stop"}`)
	p := New(
		WithHTTPClient(&http.Client{Transport: respond(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body)}),
		WithEnv(func(k string) (string, bool) { return "test-key", k == askKeyEnv }),
	)
	msg, err := resultOf(t, p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{}))
	require.NoError(t, err)
	assert.Equal(t, protocol.StopLength, msg.StopReason)
}

// stalledBody sends its data and then blocks until the request ends, as a
// connection that stops sending does.
type stalledBody struct {
	r    io.Reader
	done chan struct{}
	ctx  context.Context
}

func (b *stalledBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		select {
		case <-b.done:
		case <-b.ctx.Done():
		}
		return 0, io.ErrClosedPipe
	}
	return n, err
}

func (b *stalledBody) Close() error {
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	return nil
}

func TestAnthropicStalledBodyIsTimeout(t *testing.T) {
	full := textSSE("end_turn")
	half := full[:strings.Index(full, "event: content_block_delta")]
	p := New(
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "OK",
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       &stalledBody{r: strings.NewReader(half), done: make(chan struct{}), ctx: r.Context()},
				Request:    r,
			}, nil
		})}),
		WithEnv(func(k string) (string, bool) { return "test-key", k == askKeyEnv }),
		WithIdle(50*time.Millisecond),
	)
	_, err := resultOf(t, p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{}))
	f, ok := providers.AsFailure(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, providers.CodeTimeout, f.Code)
}

func TestAnthropicMissingKeyIsAuthFailureAndSendsNothing(t *testing.T) {
	var calls int
	p := New(
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("must not be called")
		})}),
		WithEnv(func(string) (string, bool) { return "", false }),
	)
	_, err := resultOf(t, p.Stream(context.Background(), Model(), helloReq(), providers.StreamOptions{}))
	f, ok := providers.AsFailure(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, providers.CodeAuth, f.Code)
	assert.Zero(t, calls)
}
