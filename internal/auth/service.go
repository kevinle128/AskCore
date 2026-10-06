// Package auth resolves saved credentials into request-local provider bindings.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

var (
	ErrNoCredential      = errors.New("auth: no credential")
	ErrMethod            = errors.New("auth: unsupported credential method")
	ErrRecovery          = errors.New("auth: refresh result uncertain; sign in again")
	ErrAccess            = errors.New("auth: account access denied")
	ErrCompetingOverride = errors.New("auth: competing credential overrides")
	ErrShuttingDown      = errors.New("auth: shutting down")
	ErrBinding           = errors.New("auth: credential destination mismatch")
)

// Method describes one supported provider credential and its request endpoint.
type Method struct {
	Provider    string
	ID          string
	API         providers.API
	Profile     string
	Endpoint    string
	Lead        time.Duration
	Login       func(context.Context, LoginRequest, settings.Credential) (settings.Credential, error)
	Refresh     func(context.Context, settings.Credential) (settings.Credential, error)
	CheckAccess func(context.Context, providers.Model, settings.Credential) error
}

type Service struct {
	Store    *settings.AuthStore
	Methods  []Method
	Env      func(string) (string, bool)
	Now      func() time.Time
	mu       sync.Mutex
	active   int
	idle     chan struct{}
	stopping bool
}

// WaitRefresh waits for all registered rotating operations to finish.
func (s *Service) WaitRefresh(ctx context.Context) error {
	s.mu.Lock()
	ch := s.idle
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StopRefresh prevents new rotating operations from starting.
func (s *Service) StopRefresh() {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
}

// DrainRefresh stops new rotations and waits for registered work.
func (s *Service) DrainRefresh(ctx context.Context) error {
	s.StopRefresh()
	return s.WaitRefresh(ctx)
}

func (s *Service) beginRefresh() (func(), error) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil, ErrShuttingDown
	}
	if s.active == 0 {
		s.idle = make(chan struct{})
	}
	s.active++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.active--
		if s.active == 0 {
			close(s.idle)
		}
		s.mu.Unlock()
	}, nil
}

func (s *Service) method(m providers.Model, id string) (Method, bool) {
	for _, method := range s.Methods {
		if method.Provider == m.Provider && method.ID == id && method.API == m.API && method.Endpoint == m.BaseURL {
			return method, true
		}
	}
	return Method{}, false
}

// Resolve selects the credential for one final model request.
func (s *Service) Resolve(ctx context.Context, m providers.Model, key string) (providers.AuthSnapshot, error) {
	if err := providers.Validate(m); err != nil {
		return providers.AuthSnapshot{}, err
	}
	if s == nil || s.Store == nil {
		return providers.AuthSnapshot{}, ErrNoCredential
	}
	if key != "" {
		return s.keySnapshot(m, key, "override")
	}
	c, _, err := s.Store.Read(ctx, m.Provider)
	if err != nil {
		return providers.AuthSnapshot{}, err
	}
	if c.Method == "" {
		env := s.Env
		if env == nil {
			env = os.LookupEnv
		}
		key = providers.LookupKey(m.Provider, env)
		if key == "" {
			return providers.AuthSnapshot{}, ErrNoCredential
		}
		return s.keySnapshot(m, key, "env")
	}
	if c.RefreshState != nil {
		return providers.AuthSnapshot{}, ErrRecovery
	}
	if c.Method == "api-key" {
		return s.keySnapshot(m, c.APIKey, "saved")
	}
	method, ok := s.method(m, c.Method)
	if !ok {
		return providers.AuthSnapshot{}, fmt.Errorf("%w: %s/%s", ErrMethod, m.Provider, c.Method)
	}
	if c.OAuth == nil || strings.TrimSpace(c.OAuth.AccessToken) == "" {
		return providers.AuthSnapshot{}, ErrRecovery
	}
	now := s.now()
	if !c.OAuth.ExpiresAt.After(now.Add(method.Lead)) {
		if now.Before(c.OAuth.RefreshNotBefore) {
			if !c.OAuth.ExpiresAt.After(now) {
				return providers.AuthSnapshot{}, ErrNoCredential
			}
		} else {
			if method.Refresh == nil {
				return providers.AuthSnapshot{}, ErrRecovery
			}
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return providers.AuthSnapshot{}, err
			}
			finish, err := s.beginRefresh()
			if err != nil {
				return providers.AuthSnapshot{}, err
			}
			defer finish()
			original := c
			c, err = s.Store.Refresh(ctx, m.Provider, c.Generation, hex.EncodeToString(id[:]), func(exchangeCtx context.Context, current settings.Credential) (settings.Credential, error) {
				if current.Method != original.Method || current.OAuth == nil || current.OAuth.Subject != original.OAuth.Subject || current.OAuth.ClientID != original.OAuth.ClientID || current.OAuth.Issuer != original.OAuth.Issuer {
					return settings.Credential{}, settings.ErrConflict
				}
				replacement, err := method.Refresh(exchangeCtx, current)
				if err != nil {
					return settings.Credential{}, err
				}
				if err := s.validateReplacement(method, original, replacement); err != nil {
					return settings.Credential{}, err
				}
				return replacement, nil
			})
			if err != nil {
				return providers.AuthSnapshot{}, errors.Join(ErrRecovery, err)
			}
			if err := ctx.Err(); err != nil {
				return providers.AuthSnapshot{}, err
			}
			if err := s.validateReplacement(method, original, c); err != nil {
				return providers.AuthSnapshot{}, ErrRecovery
			}
		}
	}
	if method.CheckAccess != nil {
		if err := method.CheckAccess(ctx, m, c); err != nil {
			return providers.AuthSnapshot{}, fmt.Errorf("%w: %v", ErrAccess, err)
		}
		current, _, err := s.Store.Read(ctx, m.Provider)
		if err != nil {
			return providers.AuthSnapshot{}, err
		}
		if current.Method != c.Method || current.Generation != c.Generation || current.RefreshState != nil || current.OAuth == nil || current.OAuth.Subject != c.OAuth.Subject || current.OAuth.ClientID != c.OAuth.ClientID {
			return providers.AuthSnapshot{}, settings.ErrConflict
		}
	}
	return providers.AuthSnapshot{Provider: m.Provider, Method: c.Method, Profile: method.Profile, Source: "saved", AccountID: c.OAuth.Subject, Generation: c.Generation, Endpoint: m.BaseURL, BillingHint: "subscription", AccessToken: c.OAuth.AccessToken}, nil
}

func (s *Service) keySnapshot(m providers.Model, key, source string) (providers.AuthSnapshot, error) {
	if strings.TrimSpace(key) == "" {
		return providers.AuthSnapshot{}, ErrNoCredential
	}
	_, registered := s.method(m, "api-key")
	known := m.Provider == providers.ProviderOpenAI && m.BaseURL == providers.OpenAIURL && m.API == providers.APIOpenAIResponses ||
		m.Provider == providers.ProviderTokenPlan && m.BaseURL == providers.TokenPlanMessagesURL && m.API == providers.APIAnthropicMessages ||
		m.Provider == providers.ProviderTokenPlan && m.BaseURL == providers.TokenPlanCompletionsURL && m.API == providers.APIOpenAICompletions
	if !registered && !known {
		return providers.AuthSnapshot{}, fmt.Errorf("%w: %s/api-key", ErrMethod, m.Provider)
	}
	return providers.AuthSnapshot{Provider: m.Provider, Method: "api-key", Profile: "api-key", Source: source, Endpoint: m.BaseURL, BillingHint: "api-key", AccessToken: key}, nil
}

func (s *Service) validateReplacement(method Method, original, replacement settings.Credential) error {
	now := s.now()
	if replacement.Method != method.ID || replacement.OAuth == nil || replacement.APIKey != "" || strings.TrimSpace(replacement.OAuth.AccessToken) == "" || strings.TrimSpace(replacement.OAuth.RefreshToken) == "" || replacement.OAuth.Subject != original.OAuth.Subject || replacement.OAuth.ClientID != original.OAuth.ClientID || replacement.OAuth.Issuer != original.OAuth.Issuer || !replacement.OAuth.ExpiresAt.After(now) {
		return ErrRecovery
	}
	return nil
}

// ValidateOverride checks a typed credential against the registered request contract.
func (s *Service) ValidateOverride(m providers.Model, binding providers.AuthSnapshot) error {
	if err := providers.Validate(m); err != nil {
		return err
	}
	if !binding.ValidFor(m.Provider, m.BaseURL) || strings.TrimSpace(binding.AccessToken) == "" {
		return ErrBinding
	}
	if s == nil {
		return ErrMethod
	}
	if binding.Method == "api-key" {
		if binding.Profile != "api-key" {
			return ErrMethod
		}
		_, err := s.keySnapshot(m, binding.AccessToken, "override")
		return err
	}
	method, ok := s.method(m, binding.Method)
	if !ok || method.Profile != binding.Profile {
		return ErrMethod
	}
	return nil
}
