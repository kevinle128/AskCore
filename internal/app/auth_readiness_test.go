package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

type readinessRT func(*http.Request) (*http.Response, error)

func (f readinessRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeSetModelDiscoveryKeepsPriorSelection(t *testing.T) {
	for _, scenario := range []string{"unknown", "denied", "listed", "replaced", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store, err := settings.NewAuthStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			credential := settings.Credential{Method: "openai-chatgpt", OAuth: &settings.OAuthCredential{AccessToken: "access", RefreshToken: "refresh", ClientID: "issued", Subject: "subject", Issuer: "https://auth.openai.com", ExpiresAt: time.Now().Add(time.Hour)}}
			if _, err := store.Replace(ctx, providers.ProviderOpenAI, 0, credential); err != nil {
				t.Fatal(err)
			}
			count := 0
			external := &http.Client{Transport: readinessRT(func(req *http.Request) (*http.Response, error) {
				count++
				if req.URL.String() != providers.OpenAIURL+"/models" || req.Header.Get("Authorization") != "Bearer access" {
					t.Fatal("wrong discovery binding")
				}
				code, body := 200, `{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`
				if scenario == "unknown" {
					code = 503
					body = `{"error":"unavailable"}`
				}
				if scenario == "denied" {
					code = 403
					body = `{"error":"denied"}`
				}
				if scenario == "replaced" || scenario == "deleted" {
					_, revision, err := store.Read(ctx, providers.ProviderOpenAI)
					if err != nil {
						t.Fatal(err)
					}
					if scenario == "deleted" {
						_, err = store.Logout(ctx, providers.ProviderOpenAI, revision)
					} else {
						credential.OAuth.Subject = "other"
						credential.OAuth.ClientID = "other-client"
						_, err = store.Replace(ctx, providers.ProviderOpenAI, revision, credential)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			service := &auth.Service{Store: store, Methods: auth.NativeMethods(auth.NativeOptions{HTTPClient: external})}
			wires := providers.NewRegistry()
			wires.Register(providers.APIAnthropicMessages, anthropic.New().Stream)
			wires.Register(providers.APIOpenAIResponses, openai.NewResponses().Stream)
			previous := providers.TokenPlanMessages()
			a, err := agent.New(BindAuth(agent.Config{LoopConfig: agent.LoopConfig{Model: previous, Options: providers.StreamOptions{Reasoning: protocol.ThinkingMedium}}, Registry: wires}, service, wires))
			if err != nil {
				t.Fatal(err)
			}
			err = a.SetModel(ctx, providers.OpenAIGPT55())
			allowed := scenario == "unknown" || scenario == "listed"
			if (err == nil) != allowed {
				t.Fatalf("readiness error=%v allowed=%v", err, allowed)
			}
			want := previous.Provider
			if allowed {
				want = providers.ProviderOpenAI
			}
			if count != 1 || a.State().Model.Provider != want || a.State().ThinkingLevel != protocol.ThinkingMedium {
				t.Fatal("discovery changed wrong selection")
			}
		})
	}
}
