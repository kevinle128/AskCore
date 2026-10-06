package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"AskCore/internal/providers"
)

const (
	oauthOrigin    = providers.AnthropicURL
	oauthProfile   = "claude-code"
	oauthBeta      = "claude-code-20250219,oauth-2025-04-20"
	oauthUserAgent = "claude-cli/2.1.280"
)

func oauthToken(opts providers.StreamOptions, model providers.Model) (string, error) {
	a := opts.Auth
	if a.Method == "" {
		if a != (providers.AuthSnapshot{}) {
			return "", fmt.Errorf("%w: incomplete credential binding", providers.ErrAuthentication)
		}
		return "", nil
	}
	if !a.ValidFor(model.Provider, model.BaseURL) || (opts.APIKey != "" && (a.Method != "api-key" || opts.APIKey != a.AccessToken)) {
		return "", fmt.Errorf("%w: Anthropic credential is not bound to this endpoint", providers.ErrAuthentication)
	}
	if a.Method == "api-key" {
		return "", nil
	}
	if a.Method != "anthropic-oauth" || a.Provider != providers.ProviderAnthropic || a.Profile != oauthProfile || model.BaseURL != oauthOrigin {
		return "", fmt.Errorf("%w: Anthropic subscription credential is not bound to this endpoint", providers.ErrAuthentication)
	}
	return a.AccessToken, nil
}

type oauthTransport struct {
	base  http.RoundTripper
	token string
}

func (t oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(oauthOrigin)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.URL.RawQuery != "" || req.URL.Fragment != "" || req.Header.Get("Cookie") != "" || len(req.Header.Values("Authorization")) != 1 || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != "/v1/messages" || req.Header.Get("Authorization") != "Bearer "+t.token || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Anthropic-Version") != "2023-06-01" || req.Header.Get("Anthropic-Dangerous-Direct-Browser-Access") != "true" || req.Header.Get("X-App") != "cli" || req.Header.Get("User-Agent") != oauthUserAgent {
		return nil, fmt.Errorf("anthropic subscription request profile is invalid")
	}
	beta := req.Header.Get("Anthropic-Beta")
	for _, flag := range strings.Split(oauthBeta, ",") {
		if !containsBeta(beta, flag) {
			return nil, errors.New("anthropic subscription beta profile is invalid")
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	var fields struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
	}
	if json.Unmarshal(body, &fields) != nil || len(fields.System) == 0 || fields.System[0].Text != "You are Claude Code, Anthropic's official CLI for Claude." {
		return nil, errors.New("anthropic subscription system profile is invalid")
	}
	return t.base.RoundTrip(req)
}

func containsBeta(value, flag string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(part) == flag {
			return true
		}
	}
	return false
}

type keyTransport struct {
	base          http.RoundTripper
	endpoint, key string
}

func (t keyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.endpoint)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != strings.TrimRight(u.Path, "/")+"/v1/messages" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.Header.Get("Authorization") != "" || req.Header.Get("X-Api-Key") != t.key || len(req.Header.Values("X-Api-Key")) != 1 || req.Header.Get("Cookie") != "" {
		return nil, errors.New("messages API-key destination or headers are invalid")
	}
	return t.base.RoundTrip(req)
}
