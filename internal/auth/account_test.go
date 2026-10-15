package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
)

func jwt(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	body := encode(map[string]any{"alg": "RS256", "kid": "native", "typ": "JWT"}) + "." + encode(claims)
	if key == nil {
		return body + ".c2ln"
	}
	hash := sha256.Sum256([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// chatGPTLoginAccount (the caller holds the callback port lock) runs a ChatGPT login whose ID token carries extra claims
// and returns the account names that the method reported.
func chatGPTLoginAccount(t *testing.T, extra map[string]any) []string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var nonce string
	client := &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case chatGPTToken:
			claims := map[string]any{"iss": chatGPTIssuer, "aud": "issued-1", "sub": "account-1", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
			for k, v := range extra {
				claims[k] = v
			}
			b, _ := json.Marshal(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "scope": chatGPTScope, "id_token": jwt(t, key, claims)})
			return nativeResponse(200, string(b)), nil
		case chatGPTIssuer + "/.well-known/jwks.json":
			b, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "native", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
			return nativeResponse(200, string(b)), nil
		}
		t.Errorf("unexpected destination %s", r.URL)
		return nil, errors.New("unexpected")
	})}
	methods := NativeMethods(NativeOptions{HTTPClient: client})
	var callback string
	var names []string
	_, err = methods[1].Login(context.Background(), LoginRequest{Interaction: "browser", HostID: "12345678-1234-1234-1234-123456789abc",
		Account: func(name string) { names = append(names, name) },
		Notify: func(_ context.Context, n LoginNotice) error {
			u, _ := url.Parse(n.URL)
			q := u.Query()
			nonce = q.Get("nonce")
			callback = q.Get("redirect_uri") + "?" + url.Values{"code": {"private-code"}, "state": {q.Get("state")}, "client_id": {"issued-1"}}.Encode()
			return nil
		}, Input: func(context.Context) (string, error) { return callback, nil }}, settings.Credential{})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestChatGPTAccountFromIDToken(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	if got := chatGPTLoginAccount(t, map[string]any{"email": "user@example.com", "name": "User Name"}); len(got) != 1 || got[0] != "user@example.com" {
		t.Fatalf("email claim: %v", got)
	}
	if got := chatGPTLoginAccount(t, map[string]any{"https://api.openai.com/profile": map[string]any{"email": "profile@example.com"}}); len(got) != 1 || got[0] != "profile@example.com" {
		t.Fatalf("profile claim: %v", got)
	}
	if got := chatGPTLoginAccount(t, map[string]any{"name": "Only Name"}); len(got) != 1 || got[0] != "Only Name" {
		t.Fatalf("name fallback: %v", got)
	}
	if got := chatGPTLoginAccount(t, nil); len(got) != 0 {
		t.Fatalf("no claim must report no account: %v", got)
	}
}

func TestAccountNameIsCleaned(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	got := chatGPTLoginAccount(t, map[string]any{"email": "a\x1b[31mb@example.com\u202e" + strings.Repeat("x", 400)})
	if len(got) != 1 {
		t.Fatalf("names: %v", got)
	}
	for _, r := range got[0] {
		if r < 0x20 || r == 0x7f || r == '\u202e' {
			t.Fatalf("control character in account name %q", got[0])
		}
	}
	if len(got[0]) > 128 {
		t.Fatalf("account name length %d", len(got[0]))
	}
}

func TestAnthropicAccountFromProfileThenTokenAnswer(t *testing.T) {
	type fixture struct {
		profileCode int
		profileBody string
		tokenBody   string
		want        []string
		wantProfile bool
	}
	token := func(account string) string {
		return `{"access_token":"access","refresh_token":"refresh","expires_in":3600` + account + `}`
	}
	for name, f := range map[string]fixture{
		"profile wins":                   {200, `{"account":{"email":"profile@example.com"}}`, token(`,"account":{"email_address":"token@example.com"}`), []string{"profile@example.com"}, true},
		"profile fails, token answer":    {500, `{}`, token(`,"account":{"email_address":"token@example.com"}`), []string{"token@example.com"}, true},
		"profile bad body, token answer": {200, `not json`, token(`,"account":{"email_address":"token@example.com"}`), []string{"token@example.com"}, true},
		"both fail":                      {403, `{}`, token(``), nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			profileCalls := 0
			methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case anthropicToken:
					return nativeResponse(200, f.tokenBody), nil
				case "https://api.anthropic.com/api/oauth/profile":
					profileCalls++
					if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer access" {
						t.Errorf("profile request: %s %q", r.Method, r.Header.Get("Authorization"))
					}
					return nativeResponse(f.profileCode, f.profileBody), nil
				}
				t.Errorf("unexpected destination %s", r.URL)
				return nil, errors.New("unexpected")
			})}})
			var names []string
			state := ""
			c, err := methods[0].Login(context.Background(), LoginRequest{Interaction: "copy-code", Account: func(n string) { names = append(names, n) },
				Notify: func(_ context.Context, n LoginNotice) error {
					u, _ := url.Parse(n.URL)
					state = u.Query().Get("state")
					return nil
				}, Input: func(context.Context) (string, error) { return "code#" + state, nil }}, settings.Credential{})
			if err != nil || c.OAuth == nil {
				t.Fatalf("login must not fail on account lookup: %v", err)
			}
			if strings.Join(names, ",") != strings.Join(f.want, ",") || (profileCalls == 1) != f.wantProfile {
				t.Fatalf("names %v profile calls %d", names, profileCalls)
			}
		})
	}
}

func TestAnthropicNoProfileRequestWithoutAccountCallback(t *testing.T) {
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != anthropicToken {
			t.Errorf("unexpected destination %s", r.URL)
		}
		return nativeResponse(200, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`), nil
	})}})
	state := ""
	_, err := methods[0].Login(context.Background(), LoginRequest{Interaction: "copy-code", Notify: func(_ context.Context, n LoginNotice) error {
		u, _ := url.Parse(n.URL)
		state = u.Query().Get("state")
		return nil
	}, Input: func(context.Context) (string, error) { return "code#" + state, nil }}, settings.Credential{})
	if err != nil {
		t.Fatal(err)
	}
}

func xaiDevice(t *testing.T, tokenBody string, now func() time.Time, req LoginRequest) (settings.Credential, error) {
	t.Helper()
	methods := NativeMethods(NativeOptions{Now: now, Wait: func(context.Context, time.Duration) error { return nil }, HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://auth.x.ai/oauth2/device/code" {
			return nativeResponse(200, `{"device_code":"private-device","user_code":"USER","verification_uri":"https://auth.x.ai/device","expires_in":600,"interval":2}`), nil
		}
		if strings.HasPrefix(tokenBody, "{\"error\"") {
			return nativeResponse(400, tokenBody), nil
		}
		return nativeResponse(200, tokenBody), nil
	})}})
	return methods[2].Login(context.Background(), req, settings.Credential{})
}

func TestXAIAccountFromIDToken(t *testing.T) {
	id := jwt(t, nil, map[string]any{"email": "grok@example.com", "sub": "s"})
	body, _ := json.Marshal(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "id_token": id})
	var names []string
	_, err := xaiDevice(t, string(body), time.Now, LoginRequest{Account: func(n string) { names = append(names, n) }, Notify: func(context.Context, LoginNotice) error { return nil }})
	if err != nil || len(names) != 1 || names[0] != "grok@example.com" {
		t.Fatalf("account: %v %v", names, err)
	}
	names = nil
	_, err = xaiDevice(t, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`, time.Now, LoginRequest{Account: func(n string) { names = append(names, n) }, Notify: func(context.Context, LoginNotice) error { return nil }})
	if err != nil || len(names) != 0 {
		t.Fatalf("no id token: %v %v", names, err)
	}
	// A broken ID token never fails the sign-in.
	_, err = xaiDevice(t, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"id_token":"x.!!.y"}`, time.Now, LoginRequest{Account: func(n string) { names = append(names, n) }, Notify: func(context.Context, LoginNotice) error { return nil }})
	if err != nil || len(names) != 0 {
		t.Fatalf("bad id token: %v %v", names, err)
	}
}

func TestXAINoticeCarriesExpiry(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	var notice LoginNotice
	_, err := xaiDevice(t, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`, func() time.Time { return now }, LoginRequest{Notify: func(_ context.Context, n LoginNotice) error { notice = n; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !notice.ExpiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("expiry: %v", notice.ExpiresAt)
	}
}

func TestExportedSentinelsKeepTheirText(t *testing.T) {
	for err, text := range map[error]string{
		ErrAuthorizationDenied: "auth: authorization denied",
		ErrCallbackPort:        "auth: callback port is unavailable",
		ErrDeviceExpired:       "auth: device code expired",
		ErrDeviceDenied:        "auth: device authorization denied",
	} {
		if err.Error() != text {
			t.Fatalf("text changed: %q", err)
		}
	}
	for _, c := range []struct {
		body string
		want error
	}{
		{`{"error":"access_denied"}`, ErrDeviceDenied},
		{`{"error":"authorization_denied"}`, ErrDeviceDenied},
		{`{"error":"expired_token"}`, ErrDeviceExpired},
	} {
		_, err := xaiDevice(t, c.body, time.Now, LoginRequest{Notify: func(context.Context, LoginNotice) error { return nil }})
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", c.body, err)
		}
	}
}

func TestBusyCallbackPortGivesSentinel(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	l, err := net.Listen("tcp", "127.0.0.1:53692")
	if err != nil {
		t.Skipf("callback port in use: %v", err)
	}
	defer func() { _ = l.Close() }()
	methods := NativeMethods(NativeOptions{HTTPClient: failTransport(t)})
	_, err = methods[0].Login(context.Background(), LoginRequest{Interaction: "browser", Notify: func(context.Context, LoginNotice) error { return nil }}, settings.Credential{})
	if !errors.Is(err, ErrCallbackPort) {
		t.Fatalf("busy port: %v", err)
	}
}
