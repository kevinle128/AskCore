package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

// These tests run the built ask against a scripted Anthropic service and check
// how a context overflow and its look-alikes (rate limit, quota, byte size)
// end a run.

const (
	overflowBody = `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 1000500 tokens > 1000000 maximum"}}`
	overflowText = "prompt is too long: 1000500 tokens > 1000000 maximum"
	overflowHint = "Start a new session or choose a model with a larger context window."
)

// sseWithUsage is an Anthropic stream that says text. The input count is in
// message_start and in message_delta, as a real service sends it.
func sseWithUsage(text string, input, output int) string {
	return fmt.Sprintf(`event:message_start
data:{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%[2]d,"output_tokens":0}}}

event:content_block_start
data:{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%[1]q}}

event:content_block_stop
data:{"type":"content_block_stop","index":0}

event:message_delta
data:{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":%[2]d,"output_tokens":%[3]d}}

event:message_stop
data:{"type":"message_stop"}

`, text, input, output)
}

// runBuilt runs the built ask in dir on the Ask home home and returns what it
// wrote and how it ended.
func runBuilt(t *testing.T, f *providerFixture, dir, home string, args ...string) result {
	t.Helper()
	cmd := builtAsk(t, f, dir, []string{"ASK_HOME=" + home}, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		require.ErrorAs(t, err, &ee, "stderr: %s", stderr.String())
		code = ee.ExitCode()
	}
	return result{stdout.String(), stderr.String(), code}
}

// printSonnet runs one headless prompt on the Sonnet row in a new folder.
func printSonnet(t *testing.T, f *providerFixture, prompt string, extra ...string) result {
	t.Helper()
	args := append([]string{"-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6"}, extra...)
	return runBuilt(t, f, canonicalTempDir(t), privateHome(t), append(args, prompt)...)
}

// answers makes the fixture give each step in turn. A request past the last
// step fails the test.
func answers(t *testing.T, f *providerFixture, steps ...func() (int, string, string)) {
	t.Helper()
	f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
		if n >= len(steps) {
			t.Errorf("request %d came, but the script has %d steps", n+1, len(steps))
			return http.StatusOK, "text/event-stream", sseText("unexpected request")
		}
		return steps[n]()
	}
}

func answer(status int, contentType, body string) func() (int, string, string) {
	return func() (int, string, string) { return status, contentType, body }
}

func TestPrintOverflowErrorExits1WithClearText(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusBadRequest, "application/json", overflowBody))

	got := printSonnet(t, f, "go")

	assert.Equal(t, 1, got.code, got.stderr)
	assert.Equal(t, 1, f.calls(), "an overflow is not retried")
	assert.Empty(t, got.stdout)
	assert.True(t, strings.HasPrefix(got.stderr, "Context window exceeded:"), got.stderr)
	assert.Contains(t, got.stderr, overflowText)
	assert.Contains(t, got.stderr, overflowHint)
}

func TestPrintRateLimitWithOverflowWordingRetries(t *testing.T) {
	f := newProviderFixture(t)
	f.retryAfter = "1"
	answers(t, f,
		answer(http.StatusTooManyRequests, "application/json", overflowBody),
		answer(http.StatusOK, "text/event-stream", sseText("recovered")),
	)

	got := printSonnet(t, f, "go")

	assert.Equal(t, 0, got.code, got.stderr)
	assert.Equal(t, 2, f.calls(), "a 429 is a rate limit, whatever its words say")
	assert.Equal(t, "recovered\n", got.stdout)
	assert.NotContains(t, got.stderr, "Context window exceeded")
}

func TestPrintPayloadTooLargeIsNotOverflow(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusRequestEntityTooLarge, "application/json",
		`{"type":"error","error":{"type":"request_too_large","message":"Request exceeds the maximum allowed number of bytes."}}`))

	got := printSonnet(t, f, "go")

	assert.Equal(t, 1, got.code)
	assert.NotContains(t, got.stderr, "Context window exceeded")
}

func TestPrintQuotaIsNotOverflow(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusPaymentRequired, "application/json",
		`{"type":"error","error":{"type":"billing_error","message":"Your credit balance is too low to access the API."}}`))

	got := printSonnet(t, f, "go")

	assert.Equal(t, 1, got.code)
	assert.Equal(t, 1, f.calls(), "a quota error is not retried")
	assert.NotContains(t, got.stderr, "Context window exceeded")
}

func TestPrintSilentOverflowPrintsReplyAndExits1(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusOK, "text/event-stream", sseWithUsage("a large answer", 1_000_001, 5)))

	got := printSonnet(t, f, "go")

	assert.Equal(t, 1, got.code, got.stderr)
	assert.Equal(t, "a large answer\n", got.stdout)
	assert.Equal(t, "Context window exceeded: the provider reported more input tokens than the context window of the model. "+overflowHint+"\n", got.stderr)
}

// eventsOfType returns the events of one type from a --mode json stream.
func eventsOfType(t *testing.T, stdout, typ string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev["type"] == typ {
			out = append(out, ev)
		}
	}
	return out
}

func TestJSONOverflowCycleEndAndSettledUsage(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f, answer(http.StatusBadRequest, "application/json", overflowBody))

	got := printSonnet(t, f, "go", "--mode", "json")

	assert.Equal(t, 0, got.code, "json mode keeps exit 0 on an assistant error: %s", got.stderr)
	ends := eventsOfType(t, got.stdout, "cycle_end")
	require.Len(t, ends, 1)
	assert.Equal(t, "context_overflow", ends[0]["cause"])
	assert.Equal(t, "CONTEXT_WINDOW_EXCEEDED", ends[0]["code"])
	settled := eventsOfType(t, got.stdout, "agent_settled")
	require.Len(t, settled, 1)
	assert.Contains(t, settled[0], "usage")
}

func TestACPContextOverflowHasItsOwnKind(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusOK, "text/event-stream", sseText("pong")),
		answer(http.StatusBadRequest, "application/json", overflowBody),
	)
	env := f.env()
	env["ANTHROPIC_API_KEY"] = "fixture-anthropic-key"
	p := startE2E(t, e2eOptions{env: env})
	sid := p.start()
	p.ok("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": anthropicModel, "authMethodId": "api-key"}, nil)

	require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "ping")).Error)
	var u protocol.ACPUsageResult
	p.ok("_ask/session/usage", e2eSession(sid), &u)
	assert.False(t, u.CostComplete, "the model has no known price")
	assert.Positive(t, u.ContextTokens)
	assert.Equal(t, 1_000_000, u.ContextWindow)

	r := p.call("session/prompt", e2ePrompt(sid, "again"))
	assert.Equal(t, "context_overflow", r.errKind(t))
	assert.Equal(t, 2, f.calls(), "an overflow is not retried")
}

func TestLeaderContextOverflowHasItsOwnKind(t *testing.T) {
	f := newProviderFixture(t)
	answers(t, f,
		answer(http.StatusOK, "text/event-stream", sseText("pong")),
		answer(http.StatusBadRequest, "application/json", overflowBody),
	)
	p, _, _ := startToolLeader(t, f)
	sid := p.newSession(t.TempDir())

	p.prompt(sid, "ping")
	var u protocol.ACPUsageResult
	require.NoError(t, p.call("_ask/session/usage", map[string]any{"sessionId": sid}, &u))
	assert.False(t, u.CostComplete)
	assert.Positive(t, u.ContextTokens)
	assert.Equal(t, 1_000_000, u.ContextWindow)

	err := p.call("session/prompt", e2ePrompt(sid, "again"), nil)
	var fault *rpc.Fault
	require.ErrorAs(t, err, &fault)
	assert.Equal(t, "context_overflow", fault.Kind)
}
