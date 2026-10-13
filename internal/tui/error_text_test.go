package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The error block holds one short sentence. A provider text that starts with an
// HTTP status also holds the body of the answer; the body can be long and can
// hold a masked part of the API key, so the block shows the status in words.
func TestProviderErrorTextIsOneShortSentence(t *testing.T) {
	cases := []struct {
		name, provider, msg, want string
	}{
		{
			name:     "invalid key json body",
			provider: "openai",
			msg:      `HTTP 401: { "error": { "message": "Incorrect API key provided: dummy-in**********0000. You can find your API key at https://platform.openai.com/account/api-keys.", "type": "invalid_request_error", "param": null, "code": "invalid_api_key" } }`,
			want:     "openai rejected the credentials (HTTP 401).",
		},
		{name: "forbidden", provider: "anthropic", msg: `HTTP 403: {"error":{"message":"not allowed"}}`, want: "anthropic denied access (HTTP 403)."},
		{name: "server html body", provider: "openai", msg: "HTTP 500: <html>\n<body><h1>Internal Server Error</h1></body>\n</html>", want: "openai had a server error (HTTP 500)."},
		{name: "rate limit", provider: "openai", msg: `HTTP 429: {"error":"slow down"}`, want: "openai is rate limited (HTTP 429)."},
		{name: "other status", provider: "xai", msg: `HTTP 400: {"error":"bad field"}`, want: "xai rejected the request (HTTP 400)."},
		{name: "no provider", provider: "", msg: `HTTP 401: {"error":"x"}`, want: "The provider rejected the credentials (HTTP 401)."},
		{name: "plain text stays", provider: "anthropic", msg: "anthropic token expired and refresh failed.", want: "anthropic token expired and refresh failed."},
		{name: "plain text on many lines", provider: "openai", msg: "dial tcp: lookup api.openai.com:\n no such host", want: "dial tcp: lookup api.openai.com: no such host."},
		{name: "key in plain text", provider: "openai", msg: "bad key sk-proj-abc123DEF456ghi789 in header", want: "bad key [redacted] in header."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sentence(providerErrorText(c.msg, c.provider))
			require.Equal(t, c.want, got)
			require.NotContains(t, got, "{")
			require.NotContains(t, got, "**")
		})
	}
}

func TestErrorBlockForProviderBodyKeepsShape(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	m := protocol.AssistantMessage{Provider: "openai", Model: "m", StopReason: protocol.StopError}
	body := `HTTP 401: { "error": { "message": "Incorrect API key provided: dummy-in**********0000.", "code": "invalid_api_key" } }`
	m.ErrorMessage = &body
	x.feed(&protocol.MessageEnd{Envelope: at(0), Message: m})
	x.feed(&protocol.CycleEnd{Reason: "error", Code: "AUTH"}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
	require.Equal(t, []string{
		"  ✗ Error (auth): openai rejected the credentials (HTTP 401).",
		"    Run in a shell: ask auth login --provider openai. Then send again.",
		"",
	}, x.rows())
}
