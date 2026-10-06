package auth

import (
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type nativeRT func(*http.Request) (*http.Response, error)

func (f nativeRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func nativeResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestAnthropicCopyExchange(t *testing.T) {
	var state string
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != anthropicToken {
			t.Fatal("token destination")
		}
		var fields map[string]string
		if e := json.NewDecoder(r.Body).Decode(&fields); e != nil {
			t.Fatal(e)
		}
		if fields["state"] != state || fields["code"] != "private-code" || fields["redirect_uri"] != "https://platform.claude.com/oauth/code/callback" || fields["code_verifier"] != state {
			t.Fatal("native exchange contract")
		}
		return nativeResponse(200, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`), nil
	})}})
	c, e := methods[0].Login(context.Background(), LoginRequest{Interaction: "copy-code", Notify: func(_ context.Context, n LoginNotice) error {
		u, _ := url.Parse(n.URL)
		state = u.Query().Get("state")
		if state == "" || u.Query().Get("code_challenge") == "" {
			t.Fatal("PKCE")
		}
		return nil
	}, Input: func(context.Context) (string, error) { return "private-code#" + state, nil }}, settings.Credential{})
	if e != nil || c.OAuth == nil || c.OAuth.AccessToken != "access" {
		t.Fatalf("exchange: %v", e)
	}
}
func TestXAIPollTimingAndDenial(t *testing.T) {
	for _, end := range []string{"success", "access_denied", "authorization_denied", "expired_token"} {
		t.Run(end, func(t *testing.T) {
			now := time.Now()
			var waits []time.Duration
			poll := 0
			methods := NativeMethods(NativeOptions{Now: func() time.Time { return now }, Wait: func(_ context.Context, d time.Duration) error { waits = append(waits, d); now = now.Add(d); return nil }, HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == "https://auth.x.ai/oauth2/device/code" {
					return nativeResponse(200, `{"device_code":"private-device","user_code":"USER","verification_uri":"https://auth.x.ai/device","expires_in":120,"interval":2}`), nil
				}
				if r.URL.String() != xaiToken {
					t.Fatal("poll destination")
				}
				poll++
				switch poll {
				case 1:
					return nativeResponse(400, `{"error":"authorization_pending"}`), nil
				case 2:
					return nativeResponse(400, `{"error":"slow_down","interval":1}`), nil
				case 3:
					return nativeResponse(400, `{"error":"slow_down"}`), nil
				default:
					if end == "success" {
						return nativeResponse(200, `{"access_token":"access","refresh_token":"refresh"}`), nil
					}
					return nativeResponse(400, `{"error":"`+end+`"}`), nil
				}
			})}})
			c, e := methods[2].Login(context.Background(), LoginRequest{Notify: func(context.Context, LoginNotice) error { return nil }}, settings.Credential{})
			if end == "success" && (e != nil || c.OAuth == nil) {
				t.Fatalf("success: %v", e)
			}
			if end != "success" && e == nil {
				t.Fatal("terminal error accepted")
			}
			if len(waits) != 4 || waits[0] != 2*time.Second || waits[1] != 2*time.Second || waits[2] != 2*time.Second || waits[3] != 7*time.Second {
				t.Fatalf("waits: %v", waits)
			}
		})
	}
}
func TestNativeRefreshRetention(t *testing.T) {
	old := settings.Credential{Method: "xai-oauth", OAuth: &settings.OAuthCredential{RefreshToken: "original"}}
	client := &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		return nativeResponse(200, `{"access_token":"new","expires_in":3600,"scope":"resource.invoke chatgpt.tokens.use.direct api:access"}`), nil
	})}
	methods := NativeMethods(NativeOptions{HTTPClient: client})
	c, e := methods[2].Refresh(context.Background(), old)
	if e != nil || c.OAuth.RefreshToken != "original" {
		t.Fatalf("xai retention: %v", e)
	}
	if _, e = methods[0].Refresh(context.Background(), old); e == nil {
		t.Fatal("anthropic retained missing refresh")
	}
	old.Method = "openai-chatgpt"
	old.OAuth.ClientID = "issued"
	old.OAuth.Subject = "account"
	old.OAuth.Issuer = chatGPTIssuer
	if _, e = methods[1].Refresh(context.Background(), old); e == nil {
		t.Fatal("chatgpt retained missing refresh")
	}
}
func TestDiscoveryUnknownDeniedAndGeneration(t *testing.T) {
	calls := 0
	body := `{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`
	code := 200
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != providers.OpenAIURL+"/models" || r.Header.Get("Authorization") != "Bearer access" {
			t.Fatal("discovery binding")
		}
		return nativeResponse(code, body), nil
	})}})
	c := settings.Credential{Method: "openai-chatgpt", Generation: 1, OAuth: &settings.OAuthCredential{AccessToken: "access", ClientID: "issued", Subject: "account", Issuer: chatGPTIssuer}}
	m := providers.OpenAIGPT55()
	if e := methods[1].CheckAccess(context.Background(), m, c); e != nil {
		t.Fatal(e)
	}
	code = 403
	body = `{}`
	if e := methods[1].CheckAccess(context.Background(), m, c); e != nil || calls != 1 {
		t.Fatal("cache")
	}
	c.Generation++
	if e := methods[1].CheckAccess(context.Background(), m, c); e == nil {
		t.Fatal("denial")
	}
	c.Generation++
	code = 503
	if e := methods[1].CheckAccess(context.Background(), m, c); e != nil {
		t.Fatal("unknown must allow")
	}
}
func TestNativeHTTPNoRedirectAndBound(t *testing.T) {
	for _, body := range []string{`{}`, strings.Repeat("x", (1<<20)+1)} {
		methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
			resp := nativeResponse(302, body)
			resp.Header.Set("Location", "https://other.invalid")
			return resp, nil
		})}})
		if _, e := methods[2].Refresh(context.Background(), settings.Credential{OAuth: &settings.OAuthCredential{RefreshToken: "original"}}); e == nil {
			t.Fatal("redirect or oversized response accepted")
		}
	}
}
func TestXAICancelWait(t *testing.T) {
	methods := NativeMethods(NativeOptions{Wait: func(ctx context.Context, _ time.Duration) error { return context.Canceled }, HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		return nativeResponse(200, `{"device_code":"private","user_code":"USER","verification_uri":"https://auth.x.ai/device","expires_in":120}`), nil
	})}})
	_, e := methods[2].Login(context.Background(), LoginRequest{Notify: func(context.Context, LoginNotice) error { return nil }}, settings.Credential{})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel: %v", e)
	}
}

func TestBrowserCallbackBoundAndCleanup(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		return nativeResponse(200, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`), nil
	})}})
	for attempt := 0; attempt < 2; attempt++ {
		_, e := methods[0].Login(context.Background(), LoginRequest{Interaction: "browser", Notify: func(_ context.Context, n LoginNotice) error {
			u, e := url.Parse(n.URL)
			if e != nil {
				return e
			}
			redirect := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			for _, suffix := range []string{"?code=" + strings.Repeat("x", 17000) + "&state=" + state, "?code=private&state=wrong", "?code=private&state=" + state} {
				resp, e := http.Get(redirect + suffix)
				if e != nil {
					return e
				}
				_ = resp.Body.Close()
				if strings.Contains(suffix, "wrong") || len(suffix) > 16384 {
					if resp.StatusCode != 400 {
						t.Fatal("invalid callback accepted")
					}
				} else if resp.StatusCode != 200 {
					t.Fatal("valid callback rejected")
				}
			}
			return nil
		}, Input: func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }}, settings.Credential{})
		if e != nil {
			t.Fatalf("browser attempt%d: %v", attempt, e)
		}
	}
}

func TestXAIExpiryBeforeFirstPoll(t *testing.T) {
	calls := 0
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		calls++
		return nativeResponse(200, `{"device_code":"private","user_code":"USER","verification_uri":"https://auth.x.ai/device","expires_in":1,"interval":2}`), nil
	})}})
	_, e := methods[2].Login(context.Background(), LoginRequest{Notify: func(context.Context, LoginNotice) error { return nil }}, settings.Credential{})
	if e == nil || calls != 1 {
		t.Fatal("expired code polled")
	}
}

func TestChatGPTVerifiedLoginReuseReplacement(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	store, e := settings.NewAuthStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	gate := now.Add(3550 * time.Second).Truncate(time.Second)
	refreshes := 0
	var nonce, authorizedClient, subject string
	subject = "account-1"
	issued := "issued-1"
	badNonce := false
	badGate := false
	sign := func() string {
		encode := func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			return base64.RawURLEncoding.EncodeToString(b)
		}
		claimNonce := nonce
		if badNonce {
			claimNonce = "bad"
		}
		body := encode(map[string]any{"alg": "RS256", "kid": "native", "typ": "JWT"}) + "." + encode(map[string]any{"iss": chatGPTIssuer, "aud": issued, "sub": subject, "nonce": claimNonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
		hash := sha256.Sum256([]byte(body))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return body + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	client := &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case chatGPTToken:
			if e := r.ParseForm(); e != nil {
				t.Fatal(e)
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshes++
				gate = now.Add(3550 * time.Second).Truncate(time.Second)
			}
			if r.Form.Get("client_id") != issued || r.Form.Get("grant_type") == "authorization_code" && (r.Form.Get("redirect_uri") != "http://127.0.0.1:1455/auth/callback" || r.Form.Get("code_verifier") == "") {
				t.Fatal("exchange binding")
			}
			responseGate := gate.Unix()
			if badGate {
				responseGate = -1
			}
			b, e := json.Marshal(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "scope": chatGPTScope, "id_token": sign(), "earliest_refresh_at": responseGate})
			if e != nil {
				t.Fatal(e)
			}
			return nativeResponse(200, string(b)), nil
		case chatGPTIssuer + "/.well-known/jwks.json":
			b, e := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "native", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
			if e != nil {
				t.Fatal(e)
			}
			return nativeResponse(200, string(b)), nil
		case providers.OpenAIURL + "/models":
			return nativeResponse(200, `{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`), nil
		default:
			t.Fatal("unexpected auth destination")
			return nil, errors.New("unexpected")
		}
	})}
	s := &Service{Store: store, Now: func() time.Time { return now }, Methods: NativeMethods(NativeOptions{HTTPClient: client, Now: func() time.Time { return now }})}
	request := func(newAccount bool) LoginRequest {
		var callback string
		return LoginRequest{Interaction: "browser", HostID: "12345678-1234-1234-1234-123456789abc", NewAccount: newAccount, Notify: func(_ context.Context, n LoginNotice) error {
			u, e := url.Parse(n.URL)
			if e != nil {
				return e
			}
			q := u.Query()
			nonce = q.Get("nonce")
			authorizedClient = q.Get("client_id")
			callback = q.Get("redirect_uri") + "?" + url.Values{"code": {"private-code"}, "state": {q.Get("state")}, "client_id": {issued}}.Encode()
			return nil
		}, Input: func(context.Context) (string, error) { return callback, nil }}
	}
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(false)); e != nil {
		t.Fatal(e)
	}
	if authorizedClient != "dynamic_agent_client" {
		t.Fatal("new registration not dynamic")
	}
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(false)); e != nil {
		t.Fatal(e)
	}
	if authorizedClient != "issued-1" {
		t.Fatal("returning client not reused")
	}
	subject = "account-2"
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(false)); e == nil {
		t.Fatal("account changed silently")
	}
	c, rev, e := store.Read(context.Background(), "openai")
	if e != nil || c.OAuth.Subject != "account-1" {
		t.Fatal("old account lost")
	}
	badNonce = true
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(true)); e == nil {
		t.Fatal("bad nonce accepted")
	}
	_, after, e := store.Read(context.Background(), "openai")
	if e != nil || after != rev {
		t.Fatal("invalid identity committed")
	}
	badNonce = false
	badGate = true
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(true)); e == nil {
		t.Fatal("invalid login gate accepted")
	}
	_, after, e = store.Read(context.Background(), "openai")
	if e != nil || after != rev {
		t.Fatal("invalid login gate changed prior credential")
	}
	badGate = false
	issued = "issued-2"
	if e = s.Login(context.Background(), "openai", "openai-chatgpt", request(true)); e != nil {
		t.Fatal(e)
	}
	c, _, e = store.Read(context.Background(), "openai")
	if e != nil || c.OAuth.Subject != "account-2" || c.OAuth.ClientID != "issued-2" || authorizedClient != "dynamic_agent_client" {
		t.Fatal("new-account replacement")
	}
	if !c.OAuth.RefreshNotBefore.Equal(gate) {
		t.Fatal("login lost refresh gate")
	}
	now = gate.Add(-time.Second)
	if _, err := s.Resolve(context.Background(), providers.OpenAIGPT55(), ""); err != nil || refreshes != 0 {
		t.Fatalf("premature refresh: %d %v", refreshes, err)
	}
	now = gate
	if _, err := s.Resolve(context.Background(), providers.OpenAIGPT55(), ""); err != nil || refreshes != 1 {
		t.Fatalf("allowed refresh: %d %v", refreshes, err)
	}
	c, rev, e = store.Read(context.Background(), "openai")
	if e != nil || !c.OAuth.RefreshNotBefore.Equal(gate) || !c.OAuth.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatal("refresh gate or actual expiry lost")
	}
	c.OAuth.ExpiresAt = now.Add(-time.Second)
	if _, err := store.Replace(context.Background(), "openai", rev, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(context.Background(), providers.OpenAIGPT55(), ""); !errors.Is(err, ErrNoCredential) || refreshes != 1 {
		t.Fatalf("expired token bypassed gate: %d %v", refreshes, err)
	}

}

func TestBrowserClosedInputAndStateBoundDenial(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback after EOF", true: "denied"}[denied], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, e := awaitAuthorization(ctx, LoginRequest{Input: func(context.Context) (string, error) { return "", io.EOF }, Notify: func(_ context.Context, _ LoginNotice) error {
				go func() {
					suffix := "?code=private&state=state"
					if denied {
						suffix = "?error=access_denied&state=state"
					}
					resp, e := http.Get("http://localhost:53692/callback" + suffix)
					if e == nil {
						_ = resp.Body.Close()
					}
				}()
				return nil
			}}, "https://claude.ai/oauth/authorize", "http://localhost:53692/callback", "state", true, false)
			if denied && !errors.Is(e, errAuthorizationDenied) {
				t.Fatalf("denial: %v", e)
			}
			if !denied && e != nil {
				t.Fatalf("EOF terminated browser: %v", e)
			}
		})
	}
}

func TestXAIHungPollUsesDeviceExpiry(t *testing.T) {
	started := time.Now()
	polls := 0
	methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != xaiToken {
			return nativeResponse(200, `{"device_code":"private","user_code":"USER","verification_uri":"https://auth.x.ai/device","expires_in":0.03,"interval":0.001}`), nil
		}
		polls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}})
	_, e := methods[2].Login(context.Background(), LoginRequest{Notify: func(context.Context, LoginNotice) error { return nil }}, settings.Credential{})
	if e == nil || polls != 1 || time.Since(started) > time.Second {
		t.Fatalf("poll expiry: polls%d err%v", polls, e)
	}
}

func TestNativeMalformedTokens(t *testing.T) {
	for _, body := range []string{`{"access_token":"access","refresh_token":null}`, `{"access_token":"access","refresh_token":"refresh","expires_in":null}`, `{"access_token":"access","refresh_token":"refresh","expires_in":-1}`, `{"access_token":"access","refresh_token":"refresh","scope":"openid"}`} {
		methods := NativeMethods(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) { return nativeResponse(200, body), nil })}})
		if _, e := methods[2].Refresh(context.Background(), settings.Credential{OAuth: &settings.OAuthCredential{RefreshToken: "original"}}); e == nil {
			t.Fatal("malformed token accepted")
		}
	}
}

func TestNativeRefreshGateField(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, test := range []struct {
		name, gate string
		want       time.Time
		bad        bool
	}{
		{"unix seconds", jsonNumber(now.Add(time.Hour).Unix()), now.Add(time.Hour), false},
		{"RFC3339", `"` + now.Add(time.Hour).Format(time.RFC3339) + `"`, now.Add(time.Hour), false},
		{"past", `0`, time.Unix(0, 0), false},
		{"omitted", "", time.Time{}, false},
		{"null", "null", time.Time{}, true},
		{"negative", "-1", time.Time{}, true},
		{"milliseconds", jsonNumber(now.UnixMilli()), time.Time{}, true},
		{"invalid string", `"tomorrow"`, time.Time{}, true},
		{"boolean", "true", time.Time{}, true},
		{"fraction seconds", `0.5`, time.Unix(0, 500000000), false},
		{"RFC3339 comma fraction", `"2026-10-06T12:00:00,5Z"`, time.Time{}, true},
		{"RFC3339 invalid zone", `"2026-10-06T12:00:00+24:00"`, time.Time{}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			field := ""
			if test.gate != "" {
				field = `,"earliest_refresh_at":` + test.gate
			}
			body := `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"scope":"resource.invoke chatgpt.tokens.use.direct"` + field + `}`
			methods := NativeMethods(NativeOptions{Now: func() time.Time { return now }, HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) { return nativeResponse(200, body), nil })}})
			c, e := methods[1].Refresh(context.Background(), settings.Credential{Method: "openai-chatgpt", OAuth: &settings.OAuthCredential{ClientID: "issued", Subject: "account", Issuer: chatGPTIssuer, RefreshToken: "old"}})
			if test.bad {
				if e == nil {
					t.Fatal("invalid refresh gate accepted")
				}
				return
			}
			if e != nil || !c.OAuth.RefreshNotBefore.Equal(test.want) || !c.OAuth.ExpiresAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("refresh metadata: %v", e)
			}
		})
	}
}
func jsonNumber(v int64) string { b, _ := json.Marshal(v); return string(b) }
