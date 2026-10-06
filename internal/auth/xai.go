package auth

import (
	"AskCore/internal/settings"
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

const xaiClient = "b1a00492-073a-47ea-816f-4c329264a828"
const xaiToken = "https://auth.x.ai/oauth2/token"

type deviceResponse struct {
	Device   string   `json:"device_code"`
	User     string   `json:"user_code"`
	URI      string   `json:"verification_uri"`
	Complete string   `json:"verification_uri_complete"`
	Expires  float64  `json:"expires_in"`
	Interval *float64 `json:"interval"`
}

func verificationURI(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
func (n *native) xaiLogin(ctx context.Context, r LoginRequest, _ settings.Credential) (settings.Credential, error) {
	if r.Interaction != "" && r.Interaction != "device-code" {
		return settings.Credential{}, ErrMethod
	}
	var d deviceResponse
	status, e := n.form(ctx, "https://auth.x.ai/oauth2/device/code", url.Values{"client_id": {xaiClient}, "scope": {"openid profile email offline_access grok-cli:access api:access"}, "referrer": {"pi"}}, &d)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	if strings.TrimSpace(d.Device) == "" || strings.TrimSpace(d.User) == "" || len(d.User) > privateInputLimit || !positiveSeconds(d.Expires) || !verificationURI(d.URI) || (d.Complete != "" && !verificationURI(d.Complete)) {
		return settings.Credential{}, errors.New("auth: invalid device authorization")
	}
	interval := 5 * time.Second
	if d.Interval != nil {
		if !positiveSeconds(*d.Interval) {
			return settings.Credential{}, errors.New("auth: invalid polling interval")
		}
		interval = time.Duration(*d.Interval * float64(time.Second))
	}
	deadline := n.now().Add(time.Duration(d.Expires * float64(time.Second)))
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.Expires*float64(time.Second)))
	defer cancel()
	uri := d.URI
	if d.Complete != "" {
		uri = d.Complete
	}
	if e = notify(ctx, r, LoginNotice{URL: uri, UserCode: d.User, Message: "Enter the device code to sign in."}); e != nil {
		return settings.Credential{}, e
	}
	for {
		remaining := deadline.Sub(n.now())
		if remaining <= interval {
			return settings.Credential{}, errors.New("auth: device code expired")
		}
		if e = n.wait(ctx, interval); e != nil {
			return settings.Credential{}, e
		}
		remaining = deadline.Sub(n.now())
		if remaining <= 0 {
			return settings.Credential{}, errors.New("auth: device code expired")
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, remaining)
		var t tokenResponse
		status, e = n.form(pollCtx, xaiToken, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {xaiClient}, "device_code": {d.Device}}, &t)
		pollCancel()
		if e != nil {
			return settings.Credential{}, e
		}
		if status >= 200 && status < 300 {
			return n.credential(t, "xai-oauth", settings.Credential{}, true)
		}
		switch t.Error {
		case "authorization_pending":
		case "slow_down":
			if t.Interval != nil && positiveSeconds(*t.Interval) {
				candidate := time.Duration(*t.Interval * float64(time.Second))
				if candidate > interval {
					interval = candidate
				}
			} else {
				interval += 5 * time.Second
			}
		case "access_denied", "authorization_denied":
			return settings.Credential{}, errors.New("auth: device authorization denied")
		case "expired_token":
			return settings.Credential{}, errors.New("auth: device code expired")
		default:
			return settings.Credential{}, errors.New("auth: device polling failed")
		}
	}
}
func (n *native) xaiRefresh(ctx context.Context, c settings.Credential) (settings.Credential, error) {
	if c.OAuth == nil {
		return settings.Credential{}, ErrRecovery
	}
	var t tokenResponse
	status, e := n.form(ctx, xaiToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {xaiClient}, "refresh_token": {c.OAuth.RefreshToken}}, &t)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	return n.credential(t, "xai-oauth", c, true)
}
