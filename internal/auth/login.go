package auth

import (
	"AskCore/internal/settings"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// LoginRequest supplies private, cancellation-aware user interaction.
type LoginRequest struct {
	Interaction string
	NewAccount  bool
	HostID      string
	Notify      func(context.Context, LoginNotice) error
	Input       func(context.Context) (string, error)
}

// LoginNotice contains private sign-in instructions, never issued tokens.
type LoginNotice struct{ URL, UserCode, Message string }

// NativeOptions supplies external HTTP and time boundaries.
type NativeOptions struct {
	HTTPClient *http.Client
	Now        func() time.Time
	Wait       func(context.Context, time.Duration) error
}

const privateInputLimit = 16 * 1024

func privateInput(ctx context.Context, r LoginRequest) (string, error) {
	if r.Input == nil {
		return "", errors.New("auth: private input required")
	}
	v, e := r.Input(ctx)
	if e != nil {
		return "", e
	}
	if len(v) > privateInputLimit {
		return "", errors.New("auth: private input exceeds 16 KiB")
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("auth: empty private input")
	}
	return v, nil
}

func notify(ctx context.Context, r LoginRequest, n LoginNotice) error {
	if r.Notify == nil {
		return errors.New("auth: sign-in interaction required")
	}
	return r.Notify(ctx, n)
}

// Login commits one validated credential after the interaction completes.
func (s *Service) Login(ctx context.Context, provider, method string, r LoginRequest) error {
	if s == nil || s.Store == nil {
		return ErrNoCredential
	}
	if r.NewAccount && (provider != "openai" || method != "openai-chatgpt") {
		return ErrMethod
	}
	var selected Method
	found := false
	for _, m := range s.Methods {
		if m.Provider == provider && m.ID == method {
			selected = m
			found = true
			break
		}
	}
	if !found {
		return ErrMethod
	}
	current, revision, err := s.Store.Read(ctx, provider)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	finish, err := s.beginRefresh()
	if err != nil {
		return err
	}
	defer finish()
	var next settings.Credential
	if method == "api-key" {
		key, e := privateInput(ctx, r)
		if e != nil {
			return e
		}
		next = settings.Credential{Method: method, APIKey: key}
	} else {
		if selected.Login == nil {
			return ErrMethod
		}
		next, err = selected.Login(ctx, r, current)
		if err != nil {
			return err
		}
		if next.Method != method || next.APIKey != "" || next.OAuth == nil || strings.TrimSpace(next.OAuth.AccessToken) == "" || strings.TrimSpace(next.OAuth.RefreshToken) == "" || !next.OAuth.ExpiresAt.After(s.now()) {
			return ErrRecovery
		}
	}
	_, err = s.Store.Replace(ctx, provider, revision, next)
	return err
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Logout deletes the local provider credential without a remote revocation.
func (s *Service) Logout(ctx context.Context, provider, method string) error {
	if s == nil || s.Store == nil {
		return ErrNoCredential
	}
	c, rev, err := s.Store.Read(ctx, provider)
	if err != nil {
		return err
	}
	if method != "" && c.Method != "" && c.Method != method {
		return ErrMethod
	}
	_, err = s.Store.Logout(ctx, provider, rev)
	return err
}
