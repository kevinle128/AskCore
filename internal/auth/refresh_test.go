package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

func TestConcurrentRefreshUsesOneGrant(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	_, rev, err := store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, m.Provider, rev, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "old-refresh", Subject: "account", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int32
	s := &Service{Store: store, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Lead: 8 * time.Minute, Refresh: func(ctx context.Context, c settings.Credential) (settings.Credential, error) {
		exchanges.Add(1)
		return settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "new", RefreshToken: "new-refresh", Subject: "account", ExpiresAt: time.Now().Add(time.Hour)}}, nil
	}}}}
	var wg sync.WaitGroup
	results := make([]providers.AuthSnapshot, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = s.Resolve(ctx, m, "") }(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || results[i].AccessToken != "new" {
			t.Fatalf("caller %d: %v %v", i, results[i], err)
		}
	}
	if exchanges.Load() != 1 {
		t.Fatalf("exchanges = %d", exchanges.Load())
	}
	if err := s.WaitRefresh(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFailedRefreshLeavesDurableFence(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	_, rev, err := store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, m.Provider, rev, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s := &Service{Store: store, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Lead: 8 * time.Minute, Refresh: func(context.Context, settings.Credential) (settings.Credential, error) {
		calls.Add(1)
		return settings.Credential{}, errors.New("response lost")
	}}}}
	_, err = s.Resolve(ctx, m, "")
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("first error: %v", err)
	}
	_, err = s.Resolve(ctx, m, "")
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("second error: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unsafe retry count %d", calls.Load())
	}
}

func TestDrainRefreshTracksExchange(t *testing.T) {
	ctx := context.Background()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	_, rev, err := store.Read(ctx, m.Provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Replace(ctx, m.Provider, rev, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "rotate", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	s := &Service{Store: store, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Lead: 8 * time.Minute, Refresh: func(ctx context.Context, c settings.Credential) (settings.Credential, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return settings.Credential{}, ctx.Err()
		}
		c.OAuth.AccessToken = "new"
		c.OAuth.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}}}}
	go func() { _, err := s.Resolve(ctx, m, ""); done <- err }()
	<-started
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := s.DrainRefresh(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait result: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.WaitRefresh(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRefreshResponseKeepsDurableFence(t *testing.T) {
	for _, invalid := range []string{"subject", "client", "issuer", "method", "expiry", "access", "refresh"} {
		t.Run(invalid, func(t *testing.T) {
			ctx := context.Background()
			home := t.TempDir()
			store, err := settings.NewAuthStore(home)
			if err != nil {
				t.Fatal(err)
			}
			m := providers.OpenAIGPT55()
			_, err = store.Replace(ctx, m.Provider, 0, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "old-refresh", Subject: "account", ClientID: "client", Issuer: "issuer", ExpiresAt: time.Now().Add(-time.Minute)}})
			if err != nil {
				t.Fatal(err)
			}
			var calls int
			method := Method{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Refresh: func(_ context.Context, c settings.Credential) (settings.Credential, error) {
				calls++
				c.OAuth.AccessToken = "new"
				c.OAuth.RefreshToken = "new-refresh"
				c.OAuth.ExpiresAt = time.Now().Add(time.Hour)
				switch invalid {
				case "subject":
					c.OAuth.Subject = "other"
				case "client":
					c.OAuth.ClientID = "other"
				case "issuer":
					c.OAuth.Issuer = "other"
				case "method":
					c.Method = "other"
				case "expiry":
					c.OAuth.ExpiresAt = time.Now().Add(-time.Minute)
				case "access":
					c.OAuth.AccessToken = ""
				case "refresh":
					c.OAuth.RefreshToken = ""
				}
				return c, nil
			}}
			s := &Service{Store: store, Methods: []Method{method}}
			if _, err = s.Resolve(ctx, m, ""); !errors.Is(err, ErrRecovery) {
				t.Fatalf("invalid response accepted: %v", err)
			}
			restarted, err := settings.NewAuthStore(home)
			if err != nil {
				t.Fatal(err)
			}
			c, _, err := restarted.Read(ctx, m.Provider)
			if err != nil || c.RefreshState == nil || c.OAuth.AccessToken != "old" || c.Generation != 1 {
				t.Fatalf("invalid replacement activated or fence cleared: generation=%d fence=%v err=%v", c.Generation, c.RefreshState != nil, err)
			}
			s.Store = restarted
			if _, err = s.Resolve(ctx, m, ""); !errors.Is(err, ErrRecovery) || calls != 1 {
				t.Fatalf("grant reused after restart: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestDrainRefreshStopsNewRotations(t *testing.T) {
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	_, err = store.Replace(context.Background(), m.Provider, 0, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s := &Service{Store: store, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Refresh: func(context.Context, settings.Credential) (settings.Credential, error) {
		calls.Add(1)
		return settings.Credential{}, errors.New("must not run")
	}}}}
	if err = s.DrainRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(context.Background(), m, ""); !errors.Is(err, ErrShuttingDown) || calls.Load() != 0 {
		t.Fatalf("rotation began after drain: calls=%d err=%v", calls.Load(), err)
	}
	c, _, err := store.Read(context.Background(), m.Provider)
	if err != nil || c.RefreshState != nil {
		t.Fatalf("rejected rotation fenced credential: %v", err)
	}
}

func TestExpiredCredentialRefreshesBeforeAccessCheck(t *testing.T) {
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	_, err = store.Replace(context.Background(), m.Provider, 0, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "expired", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: store, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Refresh: func(_ context.Context, c settings.Credential) (settings.Credential, error) {
		c.OAuth.AccessToken = "new"
		c.OAuth.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}, CheckAccess: func(_ context.Context, _ providers.Model, c settings.Credential) error {
		if c.OAuth.AccessToken != "new" {
			return errors.New("expired token")
		}
		return nil
	}}}}
	got, err := s.Resolve(context.Background(), m, "")
	if err != nil || got.AccessToken != "new" {
		t.Fatalf("expired access check blocked refresh: %v", err)
	}
}

func TestRefreshExpiryUsesClockAfterExchange(t *testing.T) {
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := providers.OpenAIGPT55()
	now := time.Now()
	_, err = store.Replace(context.Background(), m.Provider, 0, settings.Credential{Method: "oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: now.Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: store, Now: func() time.Time { return now }, Methods: []Method{{Provider: m.Provider, ID: "oauth", API: m.API, Endpoint: m.BaseURL, Refresh: func(_ context.Context, c settings.Credential) (settings.Credential, error) {
		c.OAuth.AccessToken = "new"
		c.OAuth.ExpiresAt = now.Add(time.Second)
		now = now.Add(time.Minute)
		return c, nil
	}}}}
	if _, err = s.Resolve(context.Background(), m, ""); !errors.Is(err, ErrRecovery) {
		t.Fatalf("expiry used stale clock: %v", err)
	}
	c, _, err := store.Read(context.Background(), m.Provider)
	if err != nil || c.RefreshState == nil || c.OAuth.AccessToken != "old" {
		t.Fatalf("expired replacement activated: %v", err)
	}
}
