package auth

import (
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"context"
	"errors"
	"net/http"
	"slices"
	"time"
)

type accessKey struct {
	method, client, subject, issuer string
	generation                      uint64
}
type modelAccess struct {
	slugs         []string
	known, denied bool
	expires       time.Time
}

func (n *native) checkChatGPTAccess(ctx context.Context, m providers.Model, c settings.Credential) error {
	if c.OAuth == nil || c.OAuth.ClientID == "" || c.OAuth.Subject == "" || c.OAuth.Issuer != chatGPTIssuer || c.Method != "openai-chatgpt" || m.Provider != "openai" || m.BaseURL != providers.OpenAIURL {
		return ErrAccess
	}
	key := accessKey{c.Method, c.OAuth.ClientID, c.OAuth.Subject, c.OAuth.Issuer, c.Generation}
	n.accessMu.Lock()
	cached, ok := n.access[key]
	n.accessMu.Unlock()
	if !ok || !cached.expires.After(n.now()) {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, providers.OpenAIURL+"/models", nil)
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+c.OAuth.AccessToken)
		req.Header.Set("Accept", "application/json")
		var response struct {
			Models *[]struct {
				Slug       string `json:"slug"`
				Visibility string `json:"visibility"`
			} `json:"models"`
		}
		status, e := n.read(req, &response)
		if e != nil && status != http.StatusUnauthorized && status != http.StatusForbidden {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		cached = modelAccess{expires: n.now().Add(time.Minute)}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			cached.known = true
			cached.denied = true
		} else if status >= 200 && status < 300 && response.Models != nil {
			cached.known = true
			for _, model := range *response.Models {
				if model.Slug == "" {
					return nil
				}
				if model.Visibility == "list" {
					cached.slugs = append(cached.slugs, model.Slug)
				}
			}
		} else {
			return nil
		}
		n.accessMu.Lock()
		if n.access == nil {
			n.access = map[accessKey]modelAccess{}
		}
		for k := range n.access {
			if k.generation != key.generation {
				delete(n.access, k)
			}
		}
		n.access[key] = cached
		n.accessMu.Unlock()
	}
	if cached.known && (cached.denied || !slices.Contains(cached.slugs, m.ID)) {
		return errors.New("auth: model access denied")
	}
	return nil
}
