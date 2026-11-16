package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// firstBody keeps the body of the first request and answers every request
// with HTTP 400, so the run ends after one request.
type firstBody struct {
	mu   sync.Mutex
	body []byte
}

func (f *firstBody) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.body == nil {
		f.body = b
	}
	f.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusBadRequest, Request: r,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"error":{"message":"fixture","type":"invalid_request_error"}}`)),
	}, nil
}

// TestPrintStartLevelClampsToTheModel runs ask -p on models that cannot turn
// thinking off. A requested start level goes through ClampThinkingLevel like a
// level that the TUI sets, so off sends the lowest effort of the model and
// never the word "none". With no --thinking flag the request has no effort and
// the vendor default applies.
func TestPrintStartLevelClampsToTheModel(t *testing.T) {
	var gpt6 providers.Model
	for _, m := range providers.GeneratedCatalog() {
		if m.Provider == providers.ProviderOpenAI && m.ID == "gpt-6-astra" && m.API == providers.APIOpenAIResponses {
			gpt6 = m
		}
	}
	require.NotEmpty(t, gpt6.ID, "the catalog has gpt-6-astra")
	cases := []struct {
		name, provider, model, keyEnv string
		args                          []string
		want                          string
	}{
		{"grok-4.7 off", providers.ProviderXAI, "grok-4.7", "XAI_API_KEY", []string{"--thinking", "off"}, "low"},
		{"grok-4.7 no flag", providers.ProviderXAI, "grok-4.7", "XAI_API_KEY", nil, ""},
		{"grok-4.7 high", providers.ProviderXAI, "grok-4.7", "XAI_API_KEY", []string{"--thinking", "high"}, "high"},
		{"gpt-6-astra off", providers.ProviderOpenAI, "gpt-6-astra", "OPENAI_API_KEY", []string{"--thinking", "off"},
			*gpt6.ThinkingLevelMap[providers.ClampThinkingLevel(gpt6, protocol.ThinkingOff)]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := privateHome(t)
			rt := &firstBody{}
			getenv := func(k string) string {
				switch k {
				case "ASK_HOME":
					return home
				case "ASK_DISABLE_MODEL_REFRESH":
					return "1"
				case tc.keyEnv:
					return "fixture-key"
				}
				return ""
			}
			argv := append([]string{"-p", "--provider", tc.provider, "--model", tc.model}, tc.args...)
			argv = append(argv, "Reply OK")
			var out, errb bytes.Buffer
			code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, runDependencies{getenv: getenv, inferenceHTTP: &http.Client{Transport: rt}})
			require.Equal(t, 1, code, "the fixture answers 400\n%s", errb.String())
			require.NotNil(t, rt.body, "the request reached the wire\n%s", errb.String())
			var req struct {
				Model     string `json:"model"`
				Reasoning *struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
			}
			require.NoError(t, json.Unmarshal(rt.body, &req))
			assert.Equal(t, tc.model, req.Model)
			if tc.want == "" {
				assert.Nil(t, req.Reasoning, "the request has no reasoning object")
				return
			}
			require.NotNil(t, req.Reasoning, "the request has a reasoning object")
			assert.Equal(t, tc.want, req.Reasoning.Effort)
			assert.NotEqual(t, "none", req.Reasoning.Effort)
		})
	}
}
