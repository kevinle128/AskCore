package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"

	"AskCore/internal/providers"
)

func classifyResponsesError(err error) error {
	if err == nil {
		return nil
	}
	var pe *fantasy.ProviderError
	if !errors.As(err, &pe) {
		return err
	}
	var payload struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(pe.ResponseBody, &payload)
	code := payload.Code
	if code == "" {
		code = payload.Error.Code
	}
	var responseError *fopenai.ResponsesError
	if errors.As(err, &responseError) && responseError.Code != "" {
		code = responseError.Code
	}
	var class error
	switch {
	case code == "subscription_sharing_usage_limit_exceeded":
		class = providers.ErrAllowanceExhausted
	case pe.StatusCode == http.StatusTooManyRequests || code == "rate_limit_exceeded":
		class = providers.ErrRateLimited
	case pe.StatusCode == http.StatusUnauthorized || pe.StatusCode == http.StatusForbidden || pe.AuthError:
		class = providers.ErrAuthentication
	case pe.StatusCode == http.StatusBadRequest || pe.StatusCode == http.StatusUnprocessableEntity:
		class = providers.ErrUnsupportedRequest
	case pe.StatusCode == 0:
		class = providers.ErrTransport
	}
	if class != nil {
		return fmt.Errorf("%w: %w", class, err)
	}
	return err
}
