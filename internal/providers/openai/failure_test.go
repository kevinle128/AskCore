package openai

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
)

// These tests send hand-written HTTP responses through the real adapters and
// read the typed failure from the stream result.

func answer(status int, header http.Header, body string) http.RoundTripper {
	return rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
}

func completionsFailure(t *testing.T, rt http.RoundTripper, idle time.Duration) *providers.Failure {
	t.Helper()
	p := NewCompletions(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithEnv(func(k string) (string, bool) { return "tp-key", k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" }),
		WithIdle(idle),
	)
	_, err := resultOf(t, p.Stream(context.Background(), providers.TokenPlanCompletions(), helloReq(), providers.StreamOptions{}))
	require.Error(t, err)
	f, ok := providers.AsFailure(err)
	require.Truef(t, ok, "no *providers.Failure in %v", err)
	return f
}

func responsesFailure(t *testing.T, rt http.RoundTripper) *providers.Failure {
	t.Helper()
	p := NewResponses(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithEnv(func(k string) (string, bool) { return "sk-openai", k == "OPENAI_API_KEY" }),
	)
	_, err := resultOf(t, p.Stream(context.Background(), providers.OpenAIGPT55(), helloReq(), providers.StreamOptions{}))
	require.Error(t, err)
	f, ok := providers.AsFailure(err)
	require.Truef(t, ok, "no *providers.Failure in %v", err)
	return f
}

func jsonHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

func TestCompletions429IsRateLimit(t *testing.T) {
	h := jsonHeader()
	h.Set("Retry-After", "3")
	f := completionsFailure(t, answer(http.StatusTooManyRequests, h,
		`{"error":{"message":"Rate limit reached for requests","type":"requests","code":"rate_limit_exceeded"}}`), 0)
	assert.Equal(t, providers.CodeRateLimit, f.Code)
	assert.Equal(t, http.StatusTooManyRequests, f.Status)
	assert.Equal(t, 3*time.Second, f.RetryAfter)
}

func TestCompletions503IsServer(t *testing.T) {
	f := completionsFailure(t, answer(http.StatusServiceUnavailable, jsonHeader(),
		`{"error":{"message":"The engine is currently overloaded","type":"server_error"}}`), 0)
	assert.Equal(t, providers.CodeServer, f.Code)
	assert.Equal(t, http.StatusServiceUnavailable, f.Status)
}

func TestCompletionsInsufficientQuotaIsQuota(t *testing.T) {
	f := completionsFailure(t, answer(http.StatusTooManyRequests, jsonHeader(),
		`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`), 0)
	assert.Equal(t, providers.CodeQuota, f.Code)
}

func TestCompletionsUnmappedStatusKeepsStatusInCode(t *testing.T) {
	f := completionsFailure(t, answer(http.StatusConflict, jsonHeader(), `{"error":{"message":"conflict"}}`), 0)
	assert.Equal(t, "HTTP_409", f.Code)
}

type resetBody struct{ r io.Reader }

func (b *resetBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, syscall.ECONNRESET
	}
	return n, err
}
func (b *resetBody) Close() error { return nil }

func TestCompletionsResetMidBodyIsTransport(t *testing.T) {
	half := completionsSSE()[:60]
	f := completionsFailure(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &resetBody{r: strings.NewReader(half)},
			Request:    r,
		}, nil
	}), 0)
	assert.Equal(t, providers.CodeTransport, f.Code)
	assert.Contains(t, f.Error(), "connection reset")
}

func TestCompletionsCleanEOFIsStreamClosed(t *testing.T) {
	// A body that ends on its own before the finish chunk is a closed stream.
	body := chatChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"finish_reason":null}]}`)
	f := completionsFailure(t, answer(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body), 0)
	assert.Equal(t, providers.CodeStreamClosed, f.Code)
	assert.Contains(t, f.Error(), "stream ended without a terminal event")
}

// stalledBody sends its data and then blocks until the request ends.
type stalledBody struct {
	r   io.Reader
	ctx context.Context
}

func (b *stalledBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 || !errors.Is(err, io.EOF) {
		return n, err
	}
	<-b.ctx.Done()
	return 0, io.ErrClosedPipe
}
func (b *stalledBody) Close() error { return nil }

func TestCompletionsStalledBodyIsTimeout(t *testing.T) {
	half := completionsSSE()[:60]
	f := completionsFailure(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &stalledBody{r: strings.NewReader(half), ctx: r.Context()},
			Request:    r,
		}, nil
	}), 50*time.Millisecond)
	assert.Equal(t, providers.CodeTimeout, f.Code)
}

func TestResponsesAllowanceExhaustedIsQuota(t *testing.T) {
	f := responsesFailure(t, answer(http.StatusTooManyRequests, jsonHeader(),
		`{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"allowance exhausted","type":"request_error"}}`))
	assert.Equal(t, providers.CodeQuota, f.Code)
	assert.ErrorIs(t, f, providers.ErrAllowanceExhausted)
}

func TestResponsesRateLimitKeepsRetryAfter(t *testing.T) {
	h := jsonHeader()
	h.Set("Retry-After", "12")
	f := responsesFailure(t, answer(http.StatusTooManyRequests, h,
		`{"error":{"code":"rate_limit_exceeded","message":"slow down","type":"requests"}}`))
	assert.Equal(t, providers.CodeRateLimit, f.Code)
	assert.Equal(t, 12*time.Second, f.RetryAfter)
	assert.ErrorIs(t, f, providers.ErrRateLimited)
}

func TestResponsesFailedEventWithServerErrorIsServer(t *testing.T) {
	body := responsesEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"server_error","message":"The server had an error"}}}`)
	f := responsesFailure(t, answer(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body))
	assert.Equal(t, providers.CodeServer, f.Code)
}

func TestResponsesContentlessStopIsEmptyResponse(t *testing.T) {
	body := responsesEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":0,"total_tokens":10}}}`)
	f := responsesFailure(t, answer(http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}}, body))
	assert.Equal(t, providers.CodeEmptyResponse, f.Code)
}
