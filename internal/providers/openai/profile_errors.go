package openai

import (
	"context"
	"encoding/json"
	"errors"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"

	"AskCore/internal/providers/fantasykit"
)

// classifyResponsesError turns a provider error of the Responses stream into a
// typed failure. The error code can sit in the response body or in a failed
// response event; the shared classifier reads it as one more fact. An error
// that is no provider error stays as it is.
func classifyResponsesError(runCtx context.Context, err error) error {
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
	return fantasykit.Classify(runCtx, err, code, fantasykit.ErrIdleTimeout)
}
