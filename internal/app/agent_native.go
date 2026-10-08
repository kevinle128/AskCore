package app

import (
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// NativeAgentConfig supplies session ownership and private inference transport.
// Config retains tool, session writer, pipeline, and lifecycle options.
type NativeAgentConfig struct {
	Config        agent.Config
	InitialStream providers.StreamFn
	Auth          *auth.Service
	Env           func(string) (string, bool)
	HTTPClient    *http.Client
}

// NewNativeAgent creates one independent Agent with all supported native wires.
func NewNativeAgent(in NativeAgentConfig) (*agent.Agent, error) {
	cfg := in.Config
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("native agent: session ID is required")
	}
	if !filepath.IsAbs(cfg.Cwd) {
		return nil, fmt.Errorf("native agent: cwd must be absolute")
	}
	info, err := os.Stat(cfg.Cwd)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("native agent: cwd must be a directory")
	}
	if in.InitialStream == nil {
		return nil, fmt.Errorf("native agent: initial stream is required")
	}
	wires := providers.NewRegistry()
	wires.Register(cfg.Model.API, in.InitialStream)
	aopts := []anthropic.Option{anthropic.WithEnv(in.Env)}
	oopts := []openai.Option{openai.WithEnv(in.Env)}
	if in.HTTPClient != nil {
		aopts = append(aopts, anthropic.WithHTTPClient(in.HTTPClient))
		oopts = append(oopts, openai.WithHTTPClient(in.HTTPClient))
	}
	wires.RegisterProvider(anthropic.New(aopts...))
	wires.RegisterProvider(openai.NewCompletions(oopts...))
	wires.RegisterProvider(openai.NewResponses(oopts...))
	cfg.Registry = wires
	cfg.Stream = wires.Stream
	if in.Auth != nil {
		cfg = BindAuth(cfg, in.Auth, wires)
		stream, ready := cfg.Stream, cfg.Ready
		cfg.Stream = func(ctx context.Context, m providers.Model, r providers.TranscriptRequest, o providers.StreamOptions) *providers.Stream {
			if m.Provider == "faux" {
				return wires.Stream(ctx, m, r, o)
			}
			return stream(ctx, m, r, o)
		}
		cfg.Ready = func(ctx context.Context, m providers.Model, key string) error {
			if m.Provider == "faux" {
				return nil
			}
			return ready(ctx, m, key)
		}
	}
	return agent.New(cfg)
}

// NativeAuthReady checks the effective method without changing the selected credential.
// Matching saved OAuth credentials can refresh through the normal auth resolver.
func NativeAuthReady(ctx context.Context, s *auth.Service, m providers.Model, key, requestedMethod string) error {
	if m.Provider == "faux" {
		if requestedMethod != "" {
			return auth.ErrMethod
		}
		return nil
	}
	if s == nil || s.Store == nil {
		return auth.ErrNoCredential
	}
	if requestedMethod != "" {
		effective := "api-key"
		if key == "" {
			c, _, err := s.Store.Read(ctx, m.Provider)
			if err != nil {
				return err
			}
			if c.Method != "" {
				effective = c.Method
			}
		}
		if effective != requestedMethod {
			return fmt.Errorf("%w: requested method is not the configured method", auth.ErrMethod)
		}
	}
	binding, err := s.Resolve(ctx, m, key)
	if err != nil {
		return err
	}
	if requestedMethod != "" && binding.Method != requestedMethod {
		return auth.ErrMethod
	}
	return nil
}
