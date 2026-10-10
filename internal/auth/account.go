package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const anthropicProfileURL = "https://api.anthropic.com/api/oauth/profile"

// anthropicAccount returns the account e-mail of a fresh Anthropic sign-in. It
// is best effort: any failure gives the e-mail of the token answer, and then
// an empty name. A failure never stops the sign-in.
func (n *native) anthropicAccount(ctx context.Context, accessToken, fallback string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, anthropicProfileURL, nil)
	if err != nil {
		return fallback
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	var profile struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
	}
	status, err := n.read(req, &profile)
	if err != nil || status < 200 || status >= 300 || strings.TrimSpace(profile.Account.Email) == "" {
		return fallback
	}
	return profile.Account.Email
}

// idTokenEmail reads the e-mail claim of an ID token without checking its
// signature. The value is used only as a display name for an account that the
// provider has just returned over TLS; it never grants access or selects a
// credential. A malformed token gives an empty name.
func idTokenEmail(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Email
}
