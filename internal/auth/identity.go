package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// ChatGPTIdentityVerifier checks tokens against a configured issuer and key set.
type ChatGPTIdentityVerifier struct {
	issuer string
	keys   oidc.KeySet
}

// NewChatGPTIdentityVerifier uses a trusted JWKS URL, never one from a token.
func NewChatGPTIdentityVerifier(issuer, jwksURL string, client *http.Client) (*ChatGPTIdentityVerifier, error) {
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme == "" || issuerURL.Host == "" || issuerURL.User != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, errors.New("auth: invalid identity issuer")
	}
	keysURL, err := url.Parse(jwksURL)
	if err != nil || keysURL.Scheme != issuerURL.Scheme || keysURL.Host != issuerURL.Host || keysURL.User != nil || keysURL.Fragment != "" || keysURL.RawQuery != "" {
		return nil, errors.New("auth: invalid identity key URL")
	}
	if issuerURL.Scheme != "https" && (issuerURL.Scheme != "http" || issuerURL.Hostname() != "127.0.0.1" && issuerURL.Hostname() != "localhost") {
		return nil, errors.New("auth: identity issuer requires HTTPS")
	}
	if client == nil {
		client = http.DefaultClient
	}
	bounded := *client
	if bounded.Timeout == 0 || bounded.Timeout > 5*time.Second {
		bounded.Timeout = 5 * time.Second
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := bounded.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	bounded.Transport = identityTransport{base: transport}
	ctx := oidc.ClientContext(context.Background(), &bounded)
	return &ChatGPTIdentityVerifier{issuer: issuer, keys: oidc.NewRemoteKeySet(ctx, jwksURL)}, nil
}

// Verify returns the signed subject for the issued client and login nonce.
func (v *ChatGPTIdentityVerifier) Verify(ctx context.Context, rawToken, issuedClientID, nonce string) (string, error) {
	if v == nil || strings.TrimSpace(rawToken) == "" || strings.TrimSpace(issuedClientID) == "" || strings.TrimSpace(nonce) == "" {
		return "", errors.New("auth: incomplete identity verification input")
	}
	return v.verify(ctx, rawToken, issuedClientID, nonce)
}

// VerifyRefresh checks a refreshed identity without requiring a login nonce.
func (v *ChatGPTIdentityVerifier) VerifyRefresh(ctx context.Context, rawToken, issuedClientID string) (string, error) {
	if v == nil || strings.TrimSpace(rawToken) == "" || strings.TrimSpace(issuedClientID) == "" {
		return "", errors.New("auth: incomplete identity verification input")
	}
	return v.verify(ctx, rawToken, issuedClientID, "")
}

func (v *ChatGPTIdentityVerifier) verify(ctx context.Context, rawToken, issuedClientID, nonce string) (string, error) {
	verifier := oidc.NewVerifier(v.issuer, v.keys, &oidc.Config{ClientID: issuedClientID, SupportedSigningAlgs: []string{oidc.RS256}})
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return "", fmt.Errorf("auth: invalid identity token: %w", err)
	}
	if token.Subject == "" || nonce != "" && token.Nonce != nonce {
		return "", errors.New("auth: identity subject or nonce mismatch")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err := token.Claims(&claims); err != nil {
		return "", fmt.Errorf("auth: invalid identity claims: %w", err)
	}
	if (len(token.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != issuedClientID {
		return "", errors.New("auth: identity authorized party mismatch")
	}
	return token.Subject, nil
}

// identityTransport limits the key response before the OIDC library reads it.
type identityTransport struct{ base http.RoundTripper }

func (t identityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > 1<<20 {
		_ = resp.Body.Close()
		return nil, errors.New("auth: identity keys exceed size limit")
	}
	resp.Body = &identityBody{Reader: io.LimitReader(resp.Body, (1<<20)+1), body: resp.Body, remaining: 1 << 20}
	return resp, nil
}

type identityBody struct {
	io.Reader
	body      io.ReadCloser
	remaining int
}

func (b *identityBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.remaining -= n
	if b.remaining < 0 {
		return n, errors.New("auth: identity keys exceed size limit")
	}
	return n, err
}
func (b *identityBody) Close() error { return b.body.Close() }
