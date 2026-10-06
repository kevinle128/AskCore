package auth

import (
	"AskCore/internal/settings"
	"context"
	"errors"
	"net/url"
)

const anthropicClient = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
const anthropicToken = "https://platform.claude.com/v1/oauth/token"
const anthropicScopes = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"

func (n *native) anthropicLogin(ctx context.Context, r LoginRequest, _ settings.Credential) (settings.Credential, error) {
	if r.Interaction != "browser" && r.Interaction != "copy-code" {
		return settings.Credential{}, errors.New("auth: choose browser or copy-code interaction")
	}
	verifier, challenge, e := pkce()
	if e != nil {
		return settings.Credential{}, e
	}
	state := verifier
	redirect := "http://localhost:53692/callback"
	if r.Interaction == "copy-code" {
		redirect = "https://platform.claude.com/oauth/code/callback"
	}
	params := url.Values{"code": {"true"}, "client_id": {anthropicClient}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {anthropicScopes}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {state}}
	a, e := awaitAuthorization(ctx, r, "https://claude.ai/oauth/authorize?"+params.Encode(), redirect, state, r.Interaction == "browser", false)
	if e != nil {
		return settings.Credential{}, e
	}
	var t tokenResponse
	status, e := n.json(ctx, anthropicToken, map[string]string{"grant_type": "authorization_code", "client_id": anthropicClient, "code": a.code, "state": state, "redirect_uri": redirect, "code_verifier": verifier}, &t)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	return n.credential(t, "anthropic-oauth", settings.Credential{}, false)
}
func (n *native) anthropicRefresh(ctx context.Context, c settings.Credential) (settings.Credential, error) {
	if c.OAuth == nil {
		return settings.Credential{}, ErrRecovery
	}
	var t tokenResponse
	status, e := n.json(ctx, anthropicToken, map[string]string{"grant_type": "refresh_token", "client_id": anthropicClient, "refresh_token": c.OAuth.RefreshToken}, &t)
	if e = success(status, e); e != nil {
		return settings.Credential{}, e
	}
	return n.credential(t, "anthropic-oauth", c, false)
}
