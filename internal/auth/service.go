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
	ErrBindingChanged    = errors.New("auth: credential billing binding changed")
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
	// Forget drops per-provider cached answers after a credential change.
	Forget func()
	// ListModels returns the models that a credential of this method can use.
	// Nil means that the method has no list, so its access state stays unknown.
	ListModels func(context.Context, settings.Credential) (AccountModels, error)
}

type Service struct {
	Store   *settings.AuthStore
	Methods []Method
	Env     func(string) (string, bool)
	Now     func() time.Time
	// RunCommand runs the "!command" values of models.json. Nil makes every
	// such value fail.
	RunCommand RunCommand
	mu         sync.Mutex
	active     int
	idle       chan struct{}
	stopping   bool
	valuesOnce sync.Once
	values     *Values
}

// SourceKind names the place a request key comes from.
type SourceKind string

const (
	SourceOverride SourceKind = "override" // the --api-key flag
	SourceSaved    SourceKind = "saved"    // the saved credential
	SourceConfig   SourceKind = "config"   // the models.json apiKey
	SourceEnv      SourceKind = "env"      // a provider environment variable
)

// Source is the credential source that resolution selects. Name is safe to
// show: "override", "saved:<method>", "config:$NAME", "config:command",
// "config:literal" or "env:<NAME>". It never holds a secret.
type Source struct {
	Kind SourceKind
	Name string
}

// ResolveRequest is the input of one credential resolution. Override is the
// --api-key value pinned to Model.Provider. Config is the models.json apiKey
// of that provider, nil when the user file sets none.
type ResolveRequest struct {
	Model    providers.Model
	Override string
	Config   *ConfigKey
}

// Values returns the value resolver of the service. It is made once and
// reads Env, Now and RunCommand of the service.
func (s *Service) Values() *Values {
	s.valuesOnce.Do(func() {
		s.values = NewValues(s.RunCommand, s.env, s.now)
	})
	return s.values
}

func (s *Service) env(name string) (string, bool) {
	if s.Env != nil {
		return s.Env(name)
	}
	return os.LookupEnv(name)
}

// Forget drops the cached answers of the methods of one provider only.
func (s *Service) Forget(provider string) {
	if s == nil {
		return
	}
	for _, m := range s.Methods {
		if m.Provider == provider && m.Forget != nil {
			m.Forget()
		}
	}
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

// Resolve selects the credential for one final model request. It is
// ResolveWith with no models.json key.
func (s *Service) Resolve(ctx context.Context, m providers.Model, key string) (providers.AuthSnapshot, error) {
	return s.ResolveWith(ctx, ResolveRequest{Model: m, Override: key})
}

// ResolveWith selects the credential for one final model request in this
// order: the override, the saved credential, the models.json key, the
// environment key.
func (s *Service) ResolveWith(ctx context.Context, req ResolveRequest) (providers.AuthSnapshot, error) {
	_, snap, err := s.selectSource(ctx, req, false)
	return snap, err
}

// KeySource names the source that ResolveWith would use for req. It never
// runs a command, never refreshes a token and never returns a secret. With
// ErrRecovery, the source names the saved record that waits for recovery.
func (s *Service) KeySource(ctx context.Context, req ResolveRequest) (Source, error) {
	src, _, err := s.selectSource(ctx, req, true)
	return src, err
}

// selectSource holds the one source order for ResolveWith and KeySource.
//
//   - The override binds through a registered api-key method, or through the
//     models.json key endpoint when that endpoint is the row BaseURL.
//   - A saved credential is used when it binds to the row. A saved credential
//     that does not bind is skipped only when the models.json key binds;
//     otherwise the result is ErrMethod, so a saved key never goes to a
//     changed base URL.
//   - The models.json key binds only when its endpoint is the row BaseURL.
//   - The environment key is read only when no saved record exists, and it
//     binds only through a registered api-key method.
//
// With dryRun, the result names the source and the snapshot is empty; no
// command runs, no token refresh starts and no access check runs.
func (s *Service) selectSource(ctx context.Context, req ResolveRequest, dryRun bool) (Source, providers.AuthSnapshot, error) {
	m := req.Model
	if err := providers.Validate(m); err != nil {
		return Source{}, providers.AuthSnapshot{}, err
	}
	if s == nil || s.Store == nil {
		return Source{}, providers.AuthSnapshot{}, ErrNoCredential
	}
	configBinds := req.Config != nil && req.Config.Endpoint == m.BaseURL
	if req.Override != "" {
		snap, err := s.keySnapshot(m, req.Override, string(SourceOverride))
		if errors.Is(err, ErrMethod) && configBinds {
			snap, err = configSnapshot(m, req.Config, req.Override, string(SourceOverride))
		}
		return finishSource(Source{Kind: SourceOverride, Name: string(SourceOverride)}, snap, err, dryRun)
	}
	c, _, err := s.Store.Read(ctx, m.Provider)
	if err != nil {
		return Source{}, providers.AuthSnapshot{}, err
	}
	if c.Method != "" && (!configBinds || s.savedBinds(m, c)) {
		return s.savedSource(ctx, m, c, dryRun)
	}
	if configBinds {
		return s.configSource(ctx, m, req.Config, dryRun)
	}
	name := providers.KeyEnvName(m.Provider, s.env)
	key := providers.LookupKey(m.Provider, s.env)
	if key == "" {
		return Source{}, providers.AuthSnapshot{}, ErrNoCredential
	}
	snap, err := s.keySnapshot(m, key, string(SourceEnv))
	return finishSource(Source{Kind: SourceEnv, Name: "env:" + name}, snap, err, dryRun)
}

func finishSource(src Source, snap providers.AuthSnapshot, err error, dryRun bool) (Source, providers.AuthSnapshot, error) {
	if err != nil {
		return Source{}, providers.AuthSnapshot{}, err
	}
	if dryRun {
		return src, providers.AuthSnapshot{}, nil
	}
	return src, snap, nil
}

// savedBinds reports whether the saved credential c has a request contract
// for the row m. It has no side effects.
func (s *Service) savedBinds(m providers.Model, c settings.Credential) bool {
	if c.Method == "api-key" {
		return s.apiKeyBinds(m)
	}
	_, ok := s.method(m, c.Method)
	return ok
}

func (s *Service) configSource(ctx context.Context, m providers.Model, ck *ConfigKey, dryRun bool) (Source, providers.AuthSnapshot, error) {
	label := m.Provider + " apiKey"
	if dryRun {
		name, err := s.Values().describe(label, ck.Value)
		if err != nil {
			return Source{}, providers.AuthSnapshot{}, err
		}
		return Source{Kind: SourceConfig, Name: "config:" + name}, providers.AuthSnapshot{}, nil
	}
	key, err := s.Values().Expand(ctx, label, ck.Value)
	if err != nil {
		return Source{}, providers.AuthSnapshot{}, err
	}
	snap, err := configSnapshot(m, ck, key, string(SourceConfig))
	return finishSource(Source{Kind: SourceConfig, Name: string(SourceConfig)}, snap, err, false)
}

func (s *Service) savedSource(ctx context.Context, m providers.Model, c settings.Credential, dryRun bool) (Source, providers.AuthSnapshot, error) {
	src := Source{Kind: SourceSaved, Name: "saved:" + c.Method}
	if c.RefreshState != nil {
		// The source is named so that a dry run can tell which record blocks
		// the request. The snapshot stays empty.
		return src, providers.AuthSnapshot{}, ErrRecovery
	}
	if c.Method == "api-key" {
		snap, err := s.keySnapshot(m, c.APIKey, string(SourceSaved))
		return finishSource(src, snap, err, dryRun)
	}
	method, ok := s.method(m, c.Method)
	if !ok {
		return Source{}, providers.AuthSnapshot{}, fmt.Errorf("%w: %s/%s", ErrMethod, m.Provider, c.Method)
	}
	if dryRun {
		return src, providers.AuthSnapshot{}, nil
	}
	c, finish, err := s.freshOAuth(ctx, m.Provider, method, c)
	if err != nil {
		return Source{}, providers.AuthSnapshot{}, err
	}
	// The refresh slot stays held through the access check, so a shutdown
	// drain also waits for the check that uses the new token.
	defer finish()
	if method.CheckAccess != nil {
		if err := method.CheckAccess(ctx, m, c); err != nil {
			return Source{}, providers.AuthSnapshot{}, fmt.Errorf("%w: %v", ErrAccess, err)
		}
		current, _, err := s.Store.Read(ctx, m.Provider)
		if err != nil {
			return Source{}, providers.AuthSnapshot{}, err
		}
		if current.Method != c.Method || current.Generation != c.Generation || current.RefreshState != nil || current.OAuth == nil || current.OAuth.Subject != c.OAuth.Subject || current.OAuth.ClientID != c.OAuth.ClientID {
			return Source{}, providers.AuthSnapshot{}, settings.ErrConflict
		}
	}
	return src, providers.AuthSnapshot{Provider: m.Provider, Method: c.Method, Profile: method.Profile, Source: "saved", AccountID: c.OAuth.Subject, Generation: c.Generation, Endpoint: m.BaseURL, BillingHint: "subscription", AccessToken: c.OAuth.AccessToken}, nil
}

// freshOAuth returns the saved OAuth credential c, refreshed first when it
// expires within the method lead time. When a refresh ran, finish releases its
// shutdown slot and the caller must call it; otherwise finish does nothing.
// On error the slot is already released.
func (s *Service) freshOAuth(ctx context.Context, provider string, method Method, c settings.Credential) (settings.Credential, func(), error) {
	none := func() {}
	if c.OAuth == nil || strings.TrimSpace(c.OAuth.AccessToken) == "" {
		return settings.Credential{}, none, ErrRecovery
	}
	now := s.now()
	if c.OAuth.ExpiresAt.After(now.Add(method.Lead)) {
		return c, none, nil
	}
	if now.Before(c.OAuth.RefreshNotBefore) {
		if !c.OAuth.ExpiresAt.After(now) {
			return settings.Credential{}, none, ErrNoCredential
		}
		return c, none, nil
	}
	if method.Refresh == nil {
		return settings.Credential{}, none, ErrRecovery
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return settings.Credential{}, none, err
	}
	finish, err := s.beginRefresh()
	if err != nil {
		return settings.Credential{}, none, err
	}
	original := c
	c, err = s.Store.Refresh(ctx, provider, c.Generation, hex.EncodeToString(id[:]), func(exchangeCtx context.Context, current settings.Credential) (settings.Credential, error) {
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
		finish()
		return settings.Credential{}, none, errors.Join(ErrRecovery, err)
	}
	if err := ctx.Err(); err != nil {
		finish()
		return settings.Credential{}, none, err
	}
	if err := s.validateReplacement(method, original, c); err != nil {
		finish()
		return settings.Credential{}, none, ErrRecovery
	}
	return c, finish, nil
}

func (s *Service) keySnapshot(m providers.Model, key, source string) (providers.AuthSnapshot, error) {
	if strings.TrimSpace(key) == "" {
		return providers.AuthSnapshot{}, ErrNoCredential
	}
	if !s.apiKeyBinds(m) {
		return providers.AuthSnapshot{}, fmt.Errorf("%w: %s/api-key", ErrMethod, m.Provider)
	}
	return apiKeySnapshot(m, key, source), nil
}

// apiKeyBinds reports whether an API key may go to the row m: a registered
// api-key method binds the row, or the row is a fixed OpenAI or Token Plan
// endpoint.
func (s *Service) apiKeyBinds(m providers.Model) bool {
	_, registered := s.method(m, "api-key")
	known := m.Provider == providers.ProviderOpenAI && m.BaseURL == providers.OpenAIURL && m.API == providers.APIOpenAIResponses ||
		m.Provider == providers.ProviderTokenPlan && m.BaseURL == providers.TokenPlanMessagesURL && m.API == providers.APIAnthropicMessages ||
		m.Provider == providers.ProviderTokenPlan && m.BaseURL == providers.TokenPlanCompletionsURL && m.API == providers.APIOpenAICompletions
	return registered || known
}

// configSnapshot binds a key to the row m through the models.json key ck. It
// checks only that the ck endpoint is the row BaseURL and never reads the
// method table, so a provider that only the user file defines can bind.
func configSnapshot(m providers.Model, ck *ConfigKey, key, source string) (providers.AuthSnapshot, error) {
	if ck == nil || ck.Endpoint != m.BaseURL {
		return providers.AuthSnapshot{}, fmt.Errorf("%w: %s/api-key", ErrMethod, m.Provider)
	}
	if strings.TrimSpace(key) == "" {
		return providers.AuthSnapshot{}, ErrNoCredential
	}
	return apiKeySnapshot(m, key, source), nil
}

func apiKeySnapshot(m providers.Model, key, source string) providers.AuthSnapshot {
	return providers.AuthSnapshot{Provider: m.Provider, Method: "api-key", Profile: "api-key", Source: source, Endpoint: m.BaseURL, BillingHint: "api-key", AccessToken: key}
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
