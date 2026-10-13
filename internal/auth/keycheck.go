package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"AskCore/internal/providers"
)

// ErrKeyCheckUnavailable means the provider gave no clear answer about a key:
// a network failure, a server error or an unexpected status.
var ErrKeyCheckUnavailable = errors.New("auth: key check unavailable")

// KeyRefusal means the provider refused the key. It holds only the HTTP status
// and a short code from the error object; the body is never kept.
type KeyRefusal struct {
	HTTPStatus int
	Code       string
}

func (e *KeyRefusal) Error() string {
	return fmt.Sprintf("auth: provider refused the key (HTTP %d)", e.HTTPStatus)
}

const (
	keyCheckBodyLimit = 64 << 10
	anthropicVersion  = "2023-06-01"
)

var providerCode = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// KeyCheckable reports whether a provider has a free read-only request that
// tells a good key from a bad one.
func KeyCheckable(provider string) bool {
	switch provider {
	case providers.ProviderOpenAI, providers.ProviderAnthropic, providers.ProviderXAI:
		return true
	default:
		return false
	}
}

// NativeKeyCheck returns a check that sends one GET of the model list to the
// provider. A 2xx answer means the key is valid. A 401 or 403 gives a
// *KeyRefusal. Any other answer or a transport failure gives
// ErrKeyCheckUnavailable, and a cancelled context gives its own error.
func NativeKeyCheck(o NativeOptions) func(ctx context.Context, provider, key string) error {
	client := boundedClient(o)
	return func(ctx context.Context, provider, key string) error {
		var endpoint string
		switch provider {
		case providers.ProviderOpenAI:
			endpoint = providers.OpenAIURL + "/models"
		case providers.ProviderXAI:
			endpoint = providers.XAIURL + "/models"
		case providers.ProviderAnthropic:
			endpoint = providers.AnthropicURL + "/v1/models"
		default:
			return ErrKeyCheckUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return ErrKeyCheckUnavailable
		}
		req.Header.Set("Accept", "application/json")
		if provider == providers.ProviderAnthropic {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", anthropicVersion)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrKeyCheckUnavailable
		}
		defer func() { _ = resp.Body.Close() }()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, keyCheckBodyLimit))
			code := refusalCode(body)
			if strings.Contains(code, key) {
				code = ""
			}
			return &KeyRefusal{HTTPStatus: resp.StatusCode, Code: code}
		default:
			return ErrKeyCheckUnavailable
		}
	}
}

// refusalCode takes only error.code or error.type, and only when it has the
// shape of a code, so provider message text can never reach a caller.
func refusalCode(body []byte) string {
	var answer struct {
		Error struct {
			Code any    `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	if code, ok := answer.Error.Code.(string); ok && providerCode.MatchString(code) {
		return code
	}
	if providerCode.MatchString(answer.Error.Type) {
		return answer.Error.Type
	}
	return ""
}
