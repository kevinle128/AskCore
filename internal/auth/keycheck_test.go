package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"AskCore/internal/providers"
)

func TestKeyCheckableFlags(t *testing.T) {
	for provider, want := range map[string]bool{
		providers.ProviderOpenAI: true, providers.ProviderAnthropic: true, providers.ProviderXAI: true,
		providers.ProviderTokenPlan: false, "other": false,
	} {
		if got := KeyCheckable(provider); got != want {
			t.Fatalf("%s: %v", provider, got)
		}
	}
}

func TestKeyCheckProbeRequests(t *testing.T) {
	type seen struct {
		method, url string
		header      http.Header
	}
	var got seen
	check := NativeKeyCheck(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		got = seen{r.Method, r.URL.String(), r.Header.Clone()}
		return nativeResponse(200, `{"data":[]}`), nil
	})}})
	ctx := context.Background()
	if err := check(ctx, providers.ProviderOpenAI, "fake-openai-key"); err != nil {
		t.Fatal(err)
	}
	if got.method != "GET" || got.url != "https://api.openai.com/v1/models" || got.header.Get("Authorization") != "Bearer fake-openai-key" {
		t.Fatalf("openai: %+v", got)
	}
	if err := check(ctx, providers.ProviderXAI, "fake-xai-key"); err != nil {
		t.Fatal(err)
	}
	if got.method != "GET" || got.url != "https://api.x.ai/v1/models" || got.header.Get("Authorization") != "Bearer fake-xai-key" {
		t.Fatalf("xai: %+v", got)
	}
	if err := check(ctx, providers.ProviderAnthropic, "fake-anthropic-key"); err != nil {
		t.Fatal(err)
	}
	if got.method != "GET" || got.url != "https://api.anthropic.com/v1/models" || got.header.Get("x-api-key") != "fake-anthropic-key" || got.header.Get("anthropic-version") != "2023-06-01" || got.header.Get("Authorization") != "" {
		t.Fatalf("anthropic: %+v", got)
	}
	if err := check(ctx, providers.ProviderTokenPlan, "k"); err == nil {
		t.Fatal("an uncheckable provider was probed")
	}
}

func TestKeyCheckOutcomes(t *testing.T) {
	respond := func(code int, body string) func(context.Context, string, string) error {
		return NativeKeyCheck(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
			return nativeResponse(code, body), nil
		})}})
	}
	ctx := context.Background()
	var refusal *KeyRefusal
	err := respond(401, `{"error":{"code":"invalid_api_key","message":"Incorrect key provided: fake-key"}}`)(ctx, providers.ProviderOpenAI, "fake-key")
	if !errors.As(err, &refusal) || refusal.HTTPStatus != 401 || refusal.Code != "invalid_api_key" {
		t.Fatalf("refusal: %v", err)
	}
	if strings.Contains(err.Error(), "fake-key") || strings.Contains(err.Error(), "Incorrect") {
		t.Fatalf("refusal text carries provider text: %q", err)
	}
	err = respond(403, `{"type":"error","error":{"type":"permission_error","message":"x"}}`)(ctx, providers.ProviderAnthropic, "k")
	if !errors.As(err, &refusal) || refusal.HTTPStatus != 403 || refusal.Code != "permission_error" {
		t.Fatalf("type fallback: %v", err)
	}
	// A code from message text or with a bad shape is dropped.
	for _, body := range []string{`{"error":{"code":"Has Spaces","type":"UPPER"}}`, `{"error":{"message":"invalid_api_key"}}`, `not json`, `{"error":{"code":12}}`} {
		err = respond(401, body)(ctx, providers.ProviderOpenAI, "k")
		if !errors.As(err, &refusal) || refusal.Code != "" || refusal.HTTPStatus != 401 {
			t.Fatalf("body %q: %v", body, err)
		}
	}
	for _, code := range []int{429, 500, 503, 302, 404} {
		if err = respond(code, `{}`)(ctx, providers.ProviderOpenAI, "k"); !errors.Is(err, ErrKeyCheckUnavailable) {
			t.Fatalf("status %d: %v", code, err)
		}
	}
	failing := NativeKeyCheck(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}})
	if err = failing(ctx, providers.ProviderOpenAI, "k"); !errors.Is(err, ErrKeyCheckUnavailable) {
		t.Fatalf("transport error: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = failing(cancelled, providers.ProviderOpenAI, "k"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: %v", err)
	}
	// An oversized body is read with a limit.
	big := strings.Repeat("x", 4<<20)
	if err = respond(401, big)(ctx, providers.ProviderOpenAI, "k"); !errors.As(err, &refusal) {
		t.Fatalf("oversized body: %v", err)
	}
}
