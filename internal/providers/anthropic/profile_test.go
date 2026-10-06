package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestOAuthProfileUsesBearerAndWireToolName(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ambient-sdk-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-sdk-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://wrong.example")
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Equal(t, "Read", fields["tools"].([]any)[0].(map[string]any)["name"])
		require.Equal(t, "Read", fields["tool_choice"].(map[string]any)["name"])
		require.Equal(t, "Bearer secret", req.Header.Get("Authorization"))
		require.Empty(t, req.Header.Get("X-Api-Key"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(textSSE("end_turn"))), Request: req}, nil
	})}
	m := Model()
	m.BaseURL = oauthOrigin
	m.Provider = "anthropic"
	m.Headers = map[string]string{"Authorization": "Bearer wrong", "X-Api-Key": "wrong", "X-App": "wrong", "Anthropic-Beta": "wrong", "User-Agent": "wrong"}
	req := providers.NormalizeRequest(providers.Request{Tools: []protocol.ToolDecl{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}, Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}}})
	p := New(WithHTTPClient(client), WithEnv(func(string) (string, bool) { return "ambient", true }))
	s := p.Stream(context.Background(), m, req, providers.StreamOptions{ToolChoice: "read", Auth: providers.AuthSnapshot{Provider: "anthropic", Method: "anthropic-oauth", Profile: oauthProfile, Endpoint: oauthOrigin, AccessToken: "secret"}})
	_, err := resultOf(t, s)
	require.NoError(t, err)
	require.True(t, called)
}

func TestOAuthRejectsWrongProviderAndUnknownMethodBeforeHTTP(t *testing.T) {
	for _, method := range []string{"anthropic-oauth", "unknown-oauth"} {
		t.Run(method, func(t *testing.T) {
			called := false
			p := New(WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, fmt.Errorf("unexpected HTTP") })}))
			m := Model()
			m.BaseURL = oauthOrigin
			_, err := resultOf(t, p.Stream(context.Background(), m, providers.NormalizeRequest(providers.Request{Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hello"}}}}}), providers.StreamOptions{APIKey: "ambient-key", Auth: providers.AuthSnapshot{Provider: "anthropic", Method: method, Profile: oauthProfile, Endpoint: oauthOrigin, AccessToken: "secret"}}))
			require.Error(t, err)
			require.False(t, called)
		})
	}
}

func TestOAuthRejectsHistoricalCollisionBeforeHTTP(t *testing.T) {
	msgs := []protocol.Message{
		protocol.SystemMessage{ToolsAdded: []protocol.ToolDecl{{Name: "read"}, {Name: "Read"}}},
		protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "old", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		protocol.SystemMessage{ToolsRemoved: []protocol.ToolRef{{Name: "read"}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hello"}}},
	}
	called := false
	p := New(WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, fmt.Errorf("unexpected HTTP") })}))
	m := Model()
	m.Provider = "anthropic"
	m.BaseURL = oauthOrigin
	_, err := resultOf(t, p.Stream(context.Background(), m, providers.TranscriptRequest{Messages: msgs}, providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "anthropic", Method: "anthropic-oauth", Profile: oauthProfile, Endpoint: oauthOrigin, AccessToken: "secret"}}))
	require.ErrorContains(t, err, "collide")
	require.False(t, called)
}

func TestTypedAPIKeyOverridesAmbientSDKBearer(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-bearer")
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		require.Empty(t, req.Header.Get("Authorization"))
		require.Equal(t, "saved-key", req.Header.Get("X-Api-Key"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(textSSE("end_turn"))), Request: req}, nil
	})}
	m := Model()
	m.Provider = "anthropic"
	m.BaseURL = oauthOrigin
	_, err := resultOf(t, New(WithHTTPClient(client)).Stream(context.Background(), m, providers.NormalizeRequest(providers.Request{Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}}}), providers.StreamOptions{APIKey: "saved-key", Auth: providers.AuthSnapshot{Provider: "anthropic", Method: "api-key", Profile: "api-key", Endpoint: oauthOrigin, AccessToken: "saved-key"}}))
	require.NoError(t, err)
	require.True(t, called)
}

func TestOAuthResponseNameIsCanonicalBeforeFirstPublicEvent(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(toolSSE(), `"name":"echo"`, `"name":"Read"`))), Request: req}, nil
	})}
	m := providers.AnthropicSonnet46()
	req := providers.NormalizeRequest(providers.Request{Tools: []protocol.ToolDecl{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}, Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "read file"}}}}})
	stream := New(WithHTTPClient(client)).Stream(context.Background(), m, req, providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "anthropic", Method: "anthropic-oauth", Profile: oauthProfile, Endpoint: oauthOrigin, AccessToken: "token"}})
	found := false
	for item := range stream.Events() {
		if ev, ok := item.Event.(protocol.ToolCallStartEvent); ok {
			found = true
			require.Equal(t, "read", ev.ToolName)
		}
	}
	msg, err := stream.Result(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "read", msg.Content[0].(protocol.ToolCall).Name)
	require.Equal(t, "read", providers.CurrentTools(req.Messages)[0].Name)
}

func TestBoundAnthropicDoesNotFollowRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": []string{"https://wrong.example/credential-sink"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: req}, nil
	})}
	m := providers.AnthropicSonnet46()
	req := providers.NormalizeRequest(providers.Request{Messages: []protocol.Message{protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}}})
	_, err := resultOf(t, New(WithHTTPClient(client)).Stream(context.Background(), m, req, providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "anthropic", Method: "anthropic-oauth", Profile: oauthProfile, Endpoint: oauthOrigin, AccessToken: "token"}}))
	require.Error(t, err)
	require.Equal(t, 1, calls)
}
