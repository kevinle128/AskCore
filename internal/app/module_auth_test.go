package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/settings"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestAuthRunnerBindsRequestAndSettlesConflict(t *testing.T) {
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	s := &auth.Service{Store: store, Env: func(string) (string, bool) { return "ambient", true }}
	r := providers.NewRegistry()
	var received providers.StreamOptions
	r.Register(m.API, func(ctx context.Context, _ providers.Model, _ providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
		received = opts
		return providers.NewStream(ctx, 0, protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID}, func(a *providers.Assembler) { a.Start(); a.Done(protocol.StopStop) })
	})
	stream, ready := AuthRunner(s, r)
	if err := ready(context.Background(), m, ""); err != nil {
		t.Fatal(err)
	}
	result := stream(context.Background(), m, providers.TranscriptRequest{}, providers.StreamOptions{})
	for range result.Events() {
	}
	if _, err := result.Result(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received.Auth.AccessToken != "ambient" || received.Auth.Source != "env" || received.APIKey != "ambient" {
		t.Fatalf("binding: %v", received.Auth)
	}
	result = stream(context.Background(), m, providers.TranscriptRequest{}, providers.StreamOptions{APIKey: "legacy", Auth: providers.AuthSnapshot{Provider: m.Provider, Endpoint: m.BaseURL, Method: "api-key", AccessToken: "typed"}})
	for range result.Events() {
	}
	if _, err := result.Result(context.Background()); !errors.Is(err, auth.ErrCompetingOverride) {
		t.Fatalf("conflict: %v", err)
	}
}

func TestAuthRunnerResolvesAgainAfterTool(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := faux.New()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := p.Model("faux-1")
	if !ok {
		t.Fatal("missing faux model")
	}
	_, rev, err := store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, m.Provider, rev, settings.Credential{Method: "api-key", APIKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	r := providers.NewRegistry()
	var count int
	r.Register(m.API, func(ctx context.Context, m providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
		count++
		if count == 1 {
			_, current, err := store.Read(ctx, m.Provider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.Replace(ctx, m.Provider, current, settings.Credential{Method: "api-key", APIKey: "second"}); err != nil {
				t.Fatal(err)
			}
		}
		return p.Stream(ctx, m, req, opts)
	})
	s := &auth.Service{Store: store, Methods: []auth.Method{{Provider: m.Provider, ID: "api-key", API: m.API, Endpoint: m.BaseURL}}}
	reg := &tools.Registry{}
	if err := reg.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}); err != nil {
		t.Fatal(err)
	}
	a, err := agent.New(BindAuth(agent.Config{LoopConfig: agent.LoopConfig{Model: m}, Tools: reg}, s, r))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(ctx, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "go"}}}); err != nil {
		t.Fatal(err)
	}
	reqs := p.Requests()
	if len(reqs) != 2 {
		t.Fatalf("request count %d", len(reqs))
	}
	if reqs[0].Options.Auth.AccessToken != "first" || reqs[1].Options.Auth.AccessToken != "second" {
		t.Fatal("tool request reused stale credential")
	}
}

func TestBoundAgentPromptResolvesSavedCredential(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := faux.New()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := p.Model("faux-1")
	if !ok {
		t.Fatal("missing faux model")
	}
	_, rev, err := store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, m.Provider, rev, settings.Credential{Method: "api-key", APIKey: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	p.Set(faux.Say("hello"))
	r := providers.NewRegistry()
	r.Register(m.API, p.Stream)
	s := &auth.Service{Store: store, Methods: []auth.Method{{Provider: m.Provider, ID: "api-key", API: m.API, Endpoint: m.BaseURL}}}
	cfg := BindAuth(agent.Config{LoopConfig: agent.LoopConfig{Model: m}}, s, r)
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(ctx, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "go"}}}); err != nil {
		t.Fatal(err)
	}
	reqs := p.Requests()
	if len(reqs) != 1 || reqs[0].Options.Auth.AccessToken != "saved" {
		t.Fatalf("binding requests: %v", len(reqs))
	}
}

func TestAuthRunnerRejectsUnsupportedTypedOverride(t *testing.T) {
	m := providers.OpenAIGPT55()
	for _, invalid := range []string{"method", "profile", "api", "endpoint"} {
		t.Run(invalid, func(t *testing.T) {
			store, err := settings.NewAuthStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			method := auth.Method{Provider: m.Provider, ID: "oauth", API: m.API, Profile: "subscription", Endpoint: m.BaseURL}
			s := &auth.Service{Store: store, Methods: []auth.Method{method}}
			r := providers.NewRegistry()
			calls := 0
			r.Register(m.API, func(ctx context.Context, model providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) *providers.Stream {
				calls++
				return providers.NewStream(ctx, 0, protocol.AssistantMessage{API: string(model.API), Provider: model.Provider, Model: model.ID}, func(a *providers.Assembler) { a.Start(); a.Done(protocol.StopStop) })
			})
			binding := providers.AuthSnapshot{Provider: m.Provider, Method: "oauth", Profile: "subscription", Endpoint: m.BaseURL, AccessToken: "typed"}
			model := m
			switch invalid {
			case "method":
				binding.Method = "unknown"
			case "profile":
				binding.Profile = "api-key"
			case "api":
				s.Methods[0].API = providers.APIOpenAICompletions
			case "endpoint":
				s.Methods[0].Endpoint = "https://other.invalid"
			}
			stream, _ := AuthRunner(s, r)
			result := stream(context.Background(), model, providers.TranscriptRequest{}, providers.StreamOptions{Auth: binding})
			for range result.Events() {
			}
			if _, err = result.Result(context.Background()); err == nil || calls != 0 {
				t.Fatalf("unsupported override dispatched: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestBoundAgentRejectsInvalidRotationAcrossRestart(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	store, err := settings.NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	p, err := faux.New()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := p.Model("faux-1")
	if !ok {
		t.Fatal("missing faux model")
	}
	_, err = store.Replace(ctx, m.Provider, 0, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "refresh", Subject: "account", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	exchanges := 0
	s := &auth.Service{Store: store, Env: func(string) (string, bool) { return "billed-key", true }, Methods: []auth.Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Refresh: func(_ context.Context, c settings.Credential) (settings.Credential, error) {
		exchanges++
		c.OAuth.Subject = "other"
		c.OAuth.AccessToken = "new"
		c.OAuth.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}}}}
	r := providers.NewRegistry()
	r.Register(m.API, p.Stream)
	for attempt := 0; attempt < 2; attempt++ {
		a, err := agent.New(BindAuth(agent.Config{LoopConfig: agent.LoopConfig{Model: m}}, s, r))
		if err != nil {
			t.Fatal(err)
		}
		ends := 0
		a.Subscribe(func(event protocol.Event) error {
			if _, ok := event.(*protocol.AgentEnd); ok {
				ends++
			}
			return nil
		})
		if err = a.Prompt(ctx, protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "go"}}}); err != nil {
			t.Fatal(err)
		}
		messages := a.State().Messages
		final, ok := messages[len(messages)-1].(protocol.AssistantMessage)
		if !ok || final.StopReason != protocol.StopError {
			t.Fatal("invalid rotation did not settle as assistant error")
		}
		if ends != 1 || len(p.Requests()) != 0 || exchanges != 1 {
			t.Fatalf("unsafe settlement: ends=%d requests=%d exchanges=%d", ends, len(p.Requests()), exchanges)
		}
		s.Store, err = settings.NewAuthStore(home)
		if err != nil {
			t.Fatal(err)
		}
	}
}
