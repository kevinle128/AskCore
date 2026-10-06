package auth

import (
	"AskCore/internal/settings"
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const chatGPTToken = "https://auth.openai.com/api/accounts/oauth/token"
const chatGPTIssuer = "https://auth.openai.com"
const chatGPTScope = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

var hostUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (n *native) chatGPTLogin(ctx context.Context, r LoginRequest, old settings.Credential) (settings.Credential, error) {
	if r.Interaction != "browser" {
		return settings.Credential{}, errors.New("auth: ChatGPT requires browser interaction")
	}
	if !hostUUID.MatchString(r.HostID) {
		return settings.Credential{}, errors.New("auth: stable host UUID required")
	}
	verifier, challenge, e := pkce()
	if e != nil {
		return settings.Credential{}, e
	}
	state, e := randomValue()
	if e != nil {
		return settings.Credential{}, e
	}
	nonce, e := randomValue()
	if e != nil {
		return settings.Credential{}, e
	}
	client := "dynamic_agent_client"
	returning := old.Method == "openai-chatgpt" && old.OAuth != nil && !r.NewAccount
	if returning {
		if old.OAuth.ClientID == "" || old.OAuth.Subject == "" || old.OAuth.Issuer != chatGPTIssuer {
			return settings.Credential{}, ErrRecovery
		}
		client = old.OAuth.ClientID
	}
	redirect := "http://127.0.0.1:1455/auth/callback"
	params := url.Values{"client_id": {client}, "agent_name_hint": {"Ask"}, "ext_agent_host_id": {"urn:uuid:" + strings.ToLower(r.HostID)}, "response_type": {"code"}, "redirect_uri": {redirect}, "resource": {"https://api.openai.com/v1"}, "scope": {chatGPTScope}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "nonce": {nonce}}
	a, e := awaitAuthorization(ctx, r, "https://auth.openai.com/api/accounts/authorize?"+params.Encode(), redirect, state, true, true)
	if e != nil {
		return settings.Credential{}, e
	}
	if returning && a.client != client {
		return settings.Credential{}, errors.New("auth: issued client changed")
	}
	var t tokenResponse
	status, e := n.form(ctx, chatGPTToken, url.Values{"grant_type": {"authorization_code"}, "client_id": {a.client}, "code": {a.code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "resource": {"https://api.openai.com/v1"}}, &t)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	c, e := n.chatGPTCredential(t, settings.Credential{})
	if e != nil {
		return c, e
	}
	v, e := NewChatGPTIdentityVerifier(chatGPTIssuer, chatGPTIssuer+"/.well-known/jwks.json", n.client)
	if e != nil {
		return c, e
	}
	subject, e := v.Verify(ctx, t.IDToken, a.client, nonce)
	if e != nil {
		return settings.Credential{}, errors.New("auth: identity verification failed")
	}
	if returning && subject != old.OAuth.Subject {
		return settings.Credential{}, errors.New("auth: account changed; use new-account")
	}
	c.OAuth.ClientID = a.client
	c.OAuth.Subject = subject
	c.OAuth.Issuer = chatGPTIssuer
	return c, nil
}
func (n *native) chatGPTCredential(t tokenResponse, old settings.Credential) (settings.Credential, error) {
	if t.Scope == nil {
		return settings.Credential{}, ErrRecovery
	}
	c, e := n.credential(t, "openai-chatgpt", old, false)
	if e != nil {
		return c, e
	}
	if !slices.Contains(c.OAuth.Scopes, "chatgpt.tokens.use.direct") || !slices.Contains(c.OAuth.Scopes, "resource.invoke") {
		return settings.Credential{}, errors.New("auth: inference scopes missing")
	}
	return c, nil
}
func (n *native) chatGPTRefresh(ctx context.Context, c settings.Credential) (settings.Credential, error) {
	if c.OAuth == nil || c.OAuth.ClientID == "" || c.OAuth.Subject == "" || c.OAuth.Issuer != chatGPTIssuer {
		return settings.Credential{}, ErrRecovery
	}
	var t tokenResponse
	status, e := n.form(ctx, chatGPTToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {c.OAuth.ClientID}, "refresh_token": {c.OAuth.RefreshToken}, "resource": {"https://api.openai.com/v1"}}, &t)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	if t.IDToken != "" {
		v, err := NewChatGPTIdentityVerifier(chatGPTIssuer, chatGPTIssuer+"/.well-known/jwks.json", n.client)
		if err != nil {
			return settings.Credential{}, err
		}
		subject, err := v.VerifyRefresh(ctx, t.IDToken, c.OAuth.ClientID)
		if err != nil || subject != c.OAuth.Subject {
			return settings.Credential{}, errors.New("auth: refreshed identity changed")
		}
	}
	return n.chatGPTCredential(t, c)
}
