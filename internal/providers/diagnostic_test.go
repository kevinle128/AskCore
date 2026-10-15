package providers_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"AskCore/internal/providers"
)

func TestCleanDiagnosticRemovesQueryAndKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		gone []string
		kept []string
	}{
		{
			name: "url query and fragment",
			in:   `Post "https://api.example.com/v1/messages?key=SECRETQ&alt=sse#frag": EOF`,
			gone: []string{"SECRETQ", "alt=sse", "frag"},
			kept: []string{"https://api.example.com/v1/messages", "EOF"},
		},
		{
			name: "url userinfo",
			in:   "dial https://user:SECRETP@host.example:8443/path failed",
			gone: []string{"SECRETP", "user:"},
			kept: []string{"host.example:8443/path"},
		},
		{
			name: "bearer token",
			in:   "401: Authorization: Bearer SECRETB.abc-def~ex rejected",
			gone: []string{"SECRETB"},
			kept: []string{"401", "rejected"},
		},
		{
			name: "sk key",
			in:   "invalid key sk-proj-SECRETK123456 for project",
			gone: []string{"SECRETK"},
			kept: []string{"invalid key", "for project"},
		},
		{
			name: "x-api-key header",
			in:   "request header x-api-key: SECRETH was sent",
			gone: []string{"SECRETH"},
			kept: []string{"x-api-key", "was sent"},
		},
		{
			name: "json quoted api_key",
			in:   `provider said {"error":"bad","api_key":"SECRETJ1"} and {"x-api-key": "SECRETJ2"}`,
			gone: []string{"SECRETJ1", "SECRETJ2"},
			kept: []string{"provider said", `"error":"bad"`},
		},
		{
			name: "quoted x-api-key header",
			in:   `x-api-key: "SECRETJ3" rejected`,
			gone: []string{"SECRETJ3"},
			kept: []string{"rejected"},
		},
		{
			name: "websocket url",
			in:   "dial wss://user:SECRETW1@stream.example.test/v1/ws?token=SECRETW2: refused; ws://h.example.test/p?key=SECRETW3",
			gone: []string{"SECRETW1", "SECRETW2", "SECRETW3"},
			kept: []string{"wss://stream.example.test/v1/ws", "ws://h.example.test/p", "refused"},
		},
		{
			name: "google key",
			in:   "API key AIzaSECRETG0123456789abcdefghij is not valid",
			gone: []string{"SECRETG"},
			kept: []string{"API key", "is not valid"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providers.CleanDiagnostic(errors.New(tt.in))
			for _, g := range tt.gone {
				assert.NotContains(t, got, g)
			}
			for _, k := range tt.kept {
				assert.Contains(t, got, k)
			}
		})
	}
}

func TestCleanDiagnosticCapsTextAtARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", 400) // 800 bytes
	got := providers.CleanDiagnostic(fmt.Errorf("wrapped: %w", errors.New(long)))
	assert.LessOrEqual(t, len(got), 512)
	assert.True(t, utf8.ValidString(got))
	assert.True(t, strings.HasPrefix(got, "wrapped: éé"))
}

func TestCleanDiagnosticOfNilIsEmpty(t *testing.T) {
	assert.Empty(t, providers.CleanDiagnostic(nil))
}

func TestCleanDiagnosticKeepsPlainText(t *testing.T) {
	assert.Equal(t, "context canceled", providers.CleanDiagnostic(errors.New("context canceled")))
}
