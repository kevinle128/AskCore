package auth

import (
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type native struct {
	client   *http.Client
	now      func() time.Time
	wait     func(context.Context, time.Duration) error
	accessMu sync.Mutex
	access   map[accessKey]modelAccess
}

// NativeMethods returns the native OAuth strategies with fixed destination bindings.
func NativeMethods(o NativeOptions) []Method {
	client := http.Client{Timeout: 15 * time.Second}
	if o.HTTPClient != nil {
		client = *o.HTTPClient
	}
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := o.Now
	if now == nil {
		now = time.Now
	}
	wait := o.Wait
	if wait == nil {
		wait = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	n := &native{client: &client, now: now, wait: wait}
	return []Method{
		{Provider: "anthropic", ID: "anthropic-oauth", API: providers.APIAnthropicMessages, Profile: "claude-code", Endpoint: "https://api.anthropic.com", Lead: 10 * time.Minute, Login: n.anthropicLogin, Refresh: n.anthropicRefresh},
		{Provider: "openai", ID: "openai-chatgpt", API: providers.APIOpenAIResponses, Profile: "chatgpt", Endpoint: providers.OpenAIURL, Lead: 8 * time.Minute, Login: n.chatGPTLogin, Refresh: n.chatGPTRefresh, CheckAccess: n.checkChatGPTAccess},
		{Provider: "xai", ID: "xai-oauth", API: providers.APIOpenAIResponses, Profile: "xai", Endpoint: "https://api.x.ai/v1", Lead: 10 * time.Minute, Login: n.xaiLogin, Refresh: n.xaiRefresh},
	}
}

type tokenResponse struct {
	Earliest json.RawMessage `json:"earliest_refresh_at"`
	Access   string          `json:"access_token"`
	Refresh  *string         `json:"refresh_token"`
	IDToken  string          `json:"id_token"`
	Expires  *float64        `json:"expires_in"`
	Scope    *string         `json:"scope"`
	Error    string          `json:"error"`
	Interval *float64        `json:"interval"`
}

func (t *tokenResponse) UnmarshalJSON(b []byte) error {
	type plain tokenResponse
	var decoded plain
	if err := json.Unmarshal(b, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil || fields == nil {
		return errors.New("auth: invalid token object")
	}
	for _, name := range []string{"refresh_token", "expires_in"} {
		if v, ok := fields[name]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New("auth: null token field")
		}
	}
	*t = tokenResponse(decoded)
	return nil
}

func (n *native) request(ctx context.Context, endpoint, content string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("auth: invalid request")
	}
	req.Header.Set("Content-Type", content)
	req.Header.Set("Accept", "application/json")
	return n.read(req, out)
}
func (n *native) read(req *http.Request, out any) (int, error) {
	ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
	defer cancel()
	resp, err := n.client.Do(req.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, errors.New("auth: HTTP exchange failed")
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return resp.StatusCode, errors.New("auth: invalid response size")
	}
	if err = json.Unmarshal(b, out); err != nil {
		return resp.StatusCode, errors.New("auth: invalid JSON response")
	}
	return resp.StatusCode, nil
}
func (n *native) form(ctx context.Context, endpoint string, v url.Values, out any) (int, error) {
	return n.request(ctx, endpoint, "application/x-www-form-urlencoded", []byte(v.Encode()), out)
}
func (n *native) json(ctx context.Context, endpoint string, v map[string]string, out any) (int, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return 0, e
	}
	return n.request(ctx, endpoint, "application/json", b, out)
}
func success(status int, err error) error {
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return errors.New("auth: provider rejected exchange")
	}
	return nil
}
func positiveSeconds(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) && v <= float64((24*time.Hour)/time.Second)*365
}
func (n *native) credential(t tokenResponse, method string, old settings.Credential, retain bool) (settings.Credential, error) {
	if strings.TrimSpace(t.Access) == "" {
		return settings.Credential{}, ErrRecovery
	}
	refresh := ""
	if t.Refresh != nil {
		refresh = *t.Refresh
	} else if retain && old.OAuth != nil {
		refresh = old.OAuth.RefreshToken
	}
	if strings.TrimSpace(refresh) == "" {
		return settings.Credential{}, ErrRecovery
	}
	expiry := 3600.0
	if t.Expires != nil {
		expiry = *t.Expires
	} else if !retain {
		return settings.Credential{}, ErrRecovery
	}
	if !positiveSeconds(expiry) {
		return settings.Credential{}, ErrRecovery
	}
	o := settings.OAuthCredential{}
	if old.OAuth != nil {
		o = *old.OAuth
		o.Scopes = append([]string(nil), old.OAuth.Scopes...)
	}
	o.AccessToken = t.Access
	o.RefreshToken = refresh
	o.ExpiresAt = n.now().Add(time.Duration(expiry * float64(time.Second)))
	o.RefreshNotBefore = time.Time{}
	if method == "openai-chatgpt" {
		gate, err := refreshGate(t.Earliest)
		if err != nil {
			return settings.Credential{}, err
		}
		o.RefreshNotBefore = gate
	}
	if t.Scope != nil {
		o.Scopes = strings.Fields(*t.Scope)
		if method == "anthropic-oauth" && !slices.Contains(o.Scopes, "user:inference") || method == "xai-oauth" && !slices.Contains(o.Scopes, "api:access") {
			return settings.Credential{}, errors.New("auth: inference scopes missing")
		}
	}
	if t.IDToken != "" {
		o.IDToken = t.IDToken
	}
	return settings.Credential{Method: method, OAuth: &o}, nil
}

// refreshGate accepts an absolute Unix-second value or an RFC3339 timestamp.
func refreshGate(raw json.RawMessage) (time.Time, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return time.Time{}, nil
	}
	invalid := errors.New("auth: invalid earliest refresh time")
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return time.Time{}, invalid
		}
		gate, err := time.Parse(time.RFC3339Nano, value)
		strict := len(value) >= 20 && value[10] == 'T' && value[13] == ':' && value[16] == ':' && !strings.Contains(value, ",")
		if len(value) >= 6 && (value[len(value)-6] == '+' || value[len(value)-6] == '-') {
			zone := value[len(value)-5:]
			strict = strict && zone[:2] <= "23" && zone[3:] <= "59"
		}
		if err != nil || !strict || gate.Before(time.Unix(0, 0)) || gate.Year() > 9999 {
			return time.Time{}, invalid
		}
		return gate, nil
	}
	var seconds float64
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &seconds) != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 253402300799 {
		return time.Time{}, invalid
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*float64(time.Second))), nil
}
