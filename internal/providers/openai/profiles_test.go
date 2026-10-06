package openai

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

func TestChatGPTProfileRejectsUnboundBeforeHTTP(t *testing.T) {
	srv := newCaptureServer(t, responsesSSE())
	p := responsesProvider(srv.srv, 0)
	m := responsesModel(srv.srv.URL)
	opts := providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: srv.srv.URL, AccessToken: "test-token"}}
	_, err := resultOf(t, p.Stream(context.Background(), m, helloReq(), opts))
	require.ErrorContains(t, err, "not bound")
	require.Empty(t, srv.ch)
}

func TestChatGPTProfileRejectsEachForbiddenField(t *testing.T) {
	for _, name := range forbiddenResponsesFields {
		t.Run(name, func(t *testing.T) {
			payload := `{"stream":true,"store":false,"input":[],"` + name + `":null}`
			req, err := http.NewRequest(http.MethodPost, providers.OpenAIURL+"/responses", strings.NewReader(payload))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-token")
			_, err = (responsesProfileRT{base: http.DefaultTransport, token: "test-token"}).RoundTrip(req)
			require.ErrorContains(t, err, name)
		})
	}
}

func TestResponsesNamedToolChoice(t *testing.T) {
	m := responsesModel(providers.OpenAIURL)
	_, err := buildResponsesCall(helloReq().Messages, m, &providers.Prepared{ToolChoice: "missing"}, "")
	require.ErrorContains(t, err, "unknown Responses tool choice")
	call, err := buildResponsesCall(helloReq().Messages, m, &providers.Prepared{ToolChoice: "echo"}, "")
	require.NoError(t, err)
	require.NotNil(t, call.ToolChoice)
	require.Equal(t, "echo", string(*call.ToolChoice))
}

func TestChatGPTFinalSerializedRequest(t *testing.T) {
	t.Setenv("OPENAI_ORG_ID", "ambient-org")
	t.Setenv("OPENAI_PROJECT_ID", "ambient-project")
	called := false
	client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, "/v1/responses", req.URL.Path)
		require.Equal(t, "Bearer subscription-token", req.Header.Get("Authorization"))
		require.Equal(t, "system instruction", body["instructions"])
		for _, item := range body["input"].([]any) {
			require.NotEqual(t, "system", item.(map[string]any)["role"])
		}
		require.Equal(t, "function", body["tools"].([]any)[0].(map[string]any)["type"])
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responsesSSE())), Request: req}, nil
	})}
	req := helloReq()
	req.Messages[0] = protocol.SystemMessage{Content: []protocol.Text{{Text: "system instruction"}}, ToolsAdded: providers.CurrentTools(req.Messages)}
	m := providers.OpenAIGPT55()
	p := NewResponses(WithHTTPClient(client))
	_, err := resultOf(t, p.Stream(context.Background(), m, req, providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "subscription-token"}}))
	require.NoError(t, err)
	require.True(t, called)
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestXAIProfilesUseNativeRouteAndConditionalReasoning(t *testing.T) {
	for _, method := range []string{"api-key", "xai-oauth"} {
		for _, reasoning := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", method, reasoning), func(t *testing.T) {
				called := false
				client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
					called = true
					require.Equal(t, "api.x.ai", req.URL.Host)
					require.Equal(t, "/v1/responses", req.URL.Path)
					require.Equal(t, "Bearer xai-token", req.Header.Get("Authorization"))
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					if reasoning {
						require.Contains(t, body["include"], "reasoning.encrypted_content")
					} else {
						require.NotContains(t, body, "include")
					}
					require.Equal(t, "echo", body["tools"].([]any)[0].(map[string]any)["name"])
					require.Equal(t, "echo", body["tool_choice"].(map[string]any)["name"])
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responsesSSE())), Request: req}, nil
				})}
				m := providers.XAIGrok47()
				m.Reasoning = reasoning
				m.Headers = map[string]string{"Authorization": "Bearer wrong", "OpenAI-Project": "wrong", "Cookie": "wrong"}
				_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{ToolChoice: "echo", Auth: providers.AuthSnapshot{Provider: "xai", Method: method, Profile: "xai", Endpoint: providers.XAIURL, AccessToken: "xai-token"}}))
				require.NoError(t, err)
				require.True(t, called)
			})
		}
	}
}

func TestChatGPTGroupedToolsAndNamedChoice(t *testing.T) {
	called := false
	client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		group := body["tools"].([]any)[0].(map[string]any)
		require.Equal(t, "namespace", group["type"])
		require.Equal(t, "functions", group["name"])
		require.Equal(t, "echo", group["tools"].([]any)[0].(map[string]any)["name"])
		choice := body["tool_choice"].(map[string]any)
		require.Equal(t, "echo", choice["name"])
		require.Equal(t, "functions", choice["namespace"])
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responsesSSE())), Request: req}, nil
	})}
	m := providers.OpenAIGPT55()
	m.SamplingParams = map[string]any{"tools": []any{map[string]any{"type": "namespace", "name": "functions", "description": "Local tools", "tools": []any{map[string]any{"type": "function", "name": "echo", "description": "Echo text", "parameters": map[string]any{"type": "object"}}}}}}
	_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{ToolChoice: "echo", Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
	require.NoError(t, err)
	require.True(t, called)
}

func TestChatGPTClassifiesProviderFailuresWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
		class  error
	}{
		{"subscription_sharing_usage_limit_exceeded", 429, providers.ErrAllowanceExhausted},
		{"rate_limit_exceeded", 429, providers.ErrRateLimited},
		{"invalid_api_key", 401, providers.ErrAuthentication},
		{"unsupported_parameter", 400, providers.ErrUnsupportedRequest},
	} {
		t.Run(tc.code, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + tc.code + `","message":"rejected","type":"request_error"}}`)), Request: req}, nil
			})}
			m := providers.OpenAIGPT55()
			_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
			require.ErrorIs(t, err, tc.class)
			require.Equal(t, 1, calls)
		})
	}
}

func TestChatGPTRejectsEachSerializedExtraField(t *testing.T) {
	for _, field := range forbiddenResponsesFields {
		t.Run(field, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, fmt.Errorf("unexpected external HTTP")
			})}
			m := providers.OpenAIGPT55()
			m.SamplingParams = map[string]any{field: nil}
			_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
			require.ErrorContains(t, err, field)
			require.Zero(t, calls)
		})
	}
}

func TestCompletionsNamedChoiceAndTypedCredential(t *testing.T) {
	srv := newCaptureServer(t, completionsSSE())
	m := completionsModel(srv.srv.URL)
	p := NewCompletions(WithHTTPClient(srv.srv.Client()), WithEnv(func(string) (string, bool) { return "", false }))
	_, err := resultOf(t, p.Stream(context.Background(), m, helloReq(), providers.StreamOptions{ToolChoice: "echo", Auth: providers.AuthSnapshot{Provider: m.Provider, Method: "api-key", Profile: "api-key", Endpoint: m.BaseURL, AccessToken: "saved-key"}}))
	require.NoError(t, err)
	got := srv.take(t)
	require.Equal(t, "Bearer saved-key", got.headers.Get("Authorization"))
	require.Equal(t, "echo", got.body(t)["tool_choice"].(map[string]any)["function"].(map[string]any)["name"])
}

func TestChatGPTPartialAllowanceFailureIsNotReplayed(t *testing.T) {
	calls := 0
	payload := responsesEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`) + responsesEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","delta":"partial"}`) + responsesEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","message":"allowance exhausted"}}}`)
	client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: req}, nil
	})}
	m := providers.OpenAIGPT55()
	msg, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
	require.ErrorIs(t, err, providers.ErrAllowanceExhausted)
	require.Equal(t, 1, calls)
	require.Equal(t, "partial", msg.Content[0].(protocol.Text).Text)
}

func TestBoundResponsesDoNotFollowRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": []string{"https://wrong.example/credential-sink"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: req}, nil
	})}
	m := providers.OpenAIGPT55()
	_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, helloReq(), providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestResponsesFinalGuardRejectsWrongDestinations(t *testing.T) {
	for _, destination := range []string{"https://wrong.example/v1/responses", providers.OpenAIURL + "/responses?unexpected=1", providers.OpenAIURL + "/other", "http://api.openai.com/v1/responses"} {
		t.Run(destination, func(t *testing.T) {
			calls := 0
			req, err := http.NewRequest(http.MethodPost, destination, strings.NewReader(`{"stream":true,"store":false,"input":[]}`))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer token")
			_, err = (responsesProfileRT{base: rtFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected HTTP") }), token: "token"}).RoundTrip(req)
			require.Error(t, err)
			require.Zero(t, calls)
		})
	}
}

func TestChatGPTReplaysFullCanonicalLocalHistory(t *testing.T) {
	m := providers.OpenAIGPT55()
	signature := providers.EncodeThinkingSignature(providers.ReasoningReplay{Type: "reasoning", ID: "rs_prior", EncryptedContent: "opaque", Summary: []providers.ReasoningSummary{{Text: "summary"}}})
	namespace := "functions"
	req := providers.NormalizeRequest(providers.Request{SystemPrompt: "instructions", Tools: []protocol.ToolDecl{{Name: "echo"}}, Messages: []protocol.Message{
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "first"}}},
		protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID, StopReason: protocol.StopToolUse, Content: []protocol.AssistantBlock{protocol.Thinking{Thinking: "summary", ThinkingSignature: &signature}, protocol.Text{Text: "before tool"}, protocol.ToolCall{ID: "call_prior|fc_prior", Name: "echo", Namespace: &namespace, Arguments: json.RawMessage(`{"value":"preserved"}`)}}},
		protocol.ToolResultMessage{ToolCallID: "call_prior|fc_prior", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "tool output"}}},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "next"}}},
	}})
	called := false
	client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.NotContains(t, body, "previous_response_id")
		require.Equal(t, false, body["store"])
		input := body["input"].([]any)
		found := map[string]bool{}
		for _, raw := range input {
			item := raw.(map[string]any)
			switch item["type"] {
			case "reasoning":
				require.Equal(t, "opaque", item["encrypted_content"])
				require.Equal(t, "rs_prior", item["id"])
				found["reasoning"] = true
			case "function_call":
				require.Equal(t, "echo", item["name"])
				require.Equal(t, "functions", item["namespace"])
				require.JSONEq(t, `{"value":"preserved"}`, item["arguments"].(string))
				found["call"] = true
			case "function_call_output":
				require.Equal(t, "call_prior", item["call_id"])
				require.Equal(t, "tool output", item["output"])
				found["result"] = true
			}
		}
		require.Equal(t, map[string]bool{"reasoning": true, "call": true, "result": true}, found)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responsesSSE())), Request: r}, nil
	})}
	_, err := resultOf(t, NewResponses(WithHTTPClient(client)).Stream(context.Background(), m, req, providers.StreamOptions{Auth: providers.AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Profile: chatGPTProfile, Endpoint: providers.OpenAIURL, AccessToken: "token"}}))
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "echo", req.Messages[2].(protocol.AssistantMessage).Content[2].(protocol.ToolCall).Name)
}
