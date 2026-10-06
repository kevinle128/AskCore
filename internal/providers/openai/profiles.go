package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"AskCore/internal/providers"
)

const chatGPTProfile = "chatgpt"

func responsesCredential(m providers.Model, opts providers.StreamOptions) (string, bool, error) {
	a := opts.Auth
	if a.Method == "" {
		if a != (providers.AuthSnapshot{}) {
			return "", false, fmt.Errorf("%w: incomplete credential binding", providers.ErrAuthentication)
		}
		return "", false, nil
	}
	if !a.ValidFor(m.Provider, m.BaseURL) || (opts.APIKey != "" && (a.Method != "api-key" || opts.APIKey != a.AccessToken)) {
		return "", false, fmt.Errorf("%w: Responses credential is not bound to this endpoint", providers.ErrAuthentication)
	}
	if a.Method == "api-key" {
		return a.AccessToken, false, nil
	}
	if a.Method == "openai-chatgpt" && a.Provider == providers.ProviderOpenAI && a.Profile == chatGPTProfile && m.BaseURL == providers.OpenAIURL {
		return a.AccessToken, true, nil
	}
	if a.Method == "xai-oauth" && a.Provider == providers.ProviderXAI && a.Profile == "xai" && m.BaseURL == providers.XAIURL {
		return a.AccessToken, true, nil
	}
	return "", false, fmt.Errorf("%w: Responses OAuth credential is not bound to a supported profile", providers.ErrAuthentication)
}

type responsesProfileRT struct {
	base     http.RoundTripper
	token    string
	endpoint string
	chatGPT  bool
}

var forbiddenResponsesFields = []string{"background", "conversation", "max_output_tokens", "max_tool_calls", "metadata", "moderation", "multi_agent", "prompt", "prompt_cache_retention", "safety_identifier", "temperature", "top_logprobs", "top_p", "truncation", "user", "previous_response_id"}

func (t responsesProfileRT) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint := t.endpoint
	chatGPT := t.chatGPT || endpoint == ""
	if endpoint == "" {
		endpoint = providers.OpenAIURL
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != u.Path+"/responses" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.URL.Fragment != "" || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("OpenAI-Organization") != "" || req.Header.Get("OpenAI-Project") != "" || req.Header.Get("ChatGPT-Account-ID") != "" || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {
		return nil, fmt.Errorf("%w: Responses destination or headers are invalid", providers.ErrAuthentication)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = req.Body.Close() }()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if chatGPT {
		for _, name := range forbiddenResponsesFields {
			if _, ok := fields[name]; ok {
				return nil, fmt.Errorf("%w: ChatGPT Responses field %q is unsupported", providers.ErrUnsupportedRequest, name)
			}
		}
	}
	var stream, store bool
	if json.Unmarshal(fields["stream"], &stream) != nil || !stream || json.Unmarshal(fields["store"], &store) != nil || store || len(fields["input"]) == 0 {
		return nil, fmt.Errorf("%w: responses body is invalid", providers.ErrUnsupportedRequest)
	}
	var input []map[string]json.RawMessage
	if json.Unmarshal(fields["input"], &input) != nil {
		return nil, fmt.Errorf("%w: responses input is invalid", providers.ErrUnsupportedRequest)
	}
	if chatGPT {
		for _, item := range input {
			var role string
			_ = json.Unmarshal(item["role"], &role)
			if role == "system" {
				return nil, fmt.Errorf("%w: ChatGPT system input is unsupported", providers.ErrUnsupportedRequest)
			}
		}
	}
	if raw := fields["tools"]; len(raw) > 0 {
		var tools []map[string]json.RawMessage
		if json.Unmarshal(raw, &tools) != nil {
			return nil, fmt.Errorf("%w: responses tools are invalid", providers.ErrUnsupportedRequest)
		}
		for _, tool := range tools {
			var kind string
			_ = json.Unmarshal(tool["type"], &kind)
			if kind != "function" && kind != "namespace" && kind != "custom" {
				return nil, fmt.Errorf("%w: responses tool type is invalid", providers.ErrUnsupportedRequest)
			}
		}
	}
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	clone.ContentLength = int64(len(body))
	return t.base.RoundTrip(clone)
}

// The guard is last, after SDK serialization and model header isolation.
func profileClient(base *http.Client, m providers.Model, token string, chatGPT bool) *http.Client {
	c := &http.Client{}
	if base != nil {
		*c = *base
	}
	transport := c.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if m.API == providers.APIOpenAICompletions {
		c.Transport = completionKeyRT{base: transport, endpoint: m.BaseURL, token: token}
	} else {
		c.Transport = responsesProfileRT{base: transport, token: token, endpoint: m.BaseURL, chatGPT: chatGPT}
	}
	headers := make(map[string]string, len(m.Headers)+1)
	for k, v := range m.Headers {
		switch http.CanonicalHeaderKey(k) {
		case "Authorization", "X-Api-Key", "Openai-Organization", "Openai-Project", "Chatgpt-Account-Id", "Cookie":
		default:
			headers[k] = v
		}
	}
	headers["Authorization"] = "Bearer " + token
	c.Transport = Isolate(c.Transport, headers)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar = nil
	return c
}

type completionKeyRT struct {
	base            http.RoundTripper
	endpoint, token string
}

func (t completionKeyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.endpoint)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || req.URL.Path != u.Path+"/chat/completions" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.RawPath != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != u.Host) || req.Header.Get("Authorization") != "Bearer "+t.token || len(req.Header.Values("Authorization")) != 1 || req.Header.Get("X-Api-Key") != "" || req.Header.Get("Cookie") != "" {
		return nil, fmt.Errorf("%w: Completions destination or headers are invalid", providers.ErrAuthentication)
	}
	return t.base.RoundTrip(req)
}
