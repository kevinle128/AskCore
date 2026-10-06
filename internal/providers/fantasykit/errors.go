package fantasykit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"charm.land/fantasy"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

var errStopUnavailable = errors.New("stop reason unavailable")

// Fail maps a stream error onto Assembler.Fail. Request abort wins, then
// idle timeout, then an incomplete stream, then the HTTP text of err.
func Fail(a *providers.Assembler, reqCtx, runCtx context.Context, idle error, err error) {
	if reqCtx != nil && reqCtx.Err() != nil {
		a.Fail(protocol.StopAborted, "Request was aborted", reqCtx.Err())
		return
	}
	if idle != nil && (errors.Is(err, idle) || (runCtx != nil && errors.Is(context.Cause(runCtx), idle))) {
		a.Fail(protocol.StopError, "idle timeout", idle)
		return
	}
	if err != nil && (errors.Is(err, io.ErrUnexpectedEOF) || isIncompleteStream(err)) {
		a.Fail(protocol.StopError, providers.ErrStreamIncomplete.Error(), providers.ErrStreamIncomplete)
		return
	}
	if err == nil {
		err = errors.New("stream error")
	}
	a.Fail(protocol.StopError, HTTPMessage(err), err)
}

func isIncompleteStream(err error) bool {
	var pe *fantasy.ProviderError
	if errors.As(err, &pe) && errors.Is(pe.Cause, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// HTTPMessage is the user-facing text of err. It reads ResponseBody only;
// RequestBody can hold the API key.
func HTTPMessage(err error) string {
	var pe *fantasy.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode == 0 {
		if err == nil {
			return ""
		}
		if pe != nil && len(pe.ResponseBody) > 0 {
			body := extractJSONBody(pe.ResponseBody)
			if len(body) > 512 {
				body = body[:512]
			}
			if pe.StatusCode != 0 {
				return fmt.Sprintf("HTTP %d: %s", pe.StatusCode, body)
			}
			return fmt.Sprintf("%s: %s", err.Error(), body)
		}
		return err.Error()
	}
	body := extractJSONBody(pe.ResponseBody)
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && (payload.Code != "" || payload.Message != "") {
		return fmt.Sprintf("HTTP %d %s: %s", pe.StatusCode, payload.Code, payload.Message)
	}
	if len(body) == 0 && pe.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", pe.StatusCode, pe.Message)
	}
	if len(body) > 512 {
		body = body[:512]
	}
	return fmt.Sprintf("HTTP %d: %s", pe.StatusCode, body)
}

func extractJSONBody(raw []byte) []byte {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return bytes.TrimSpace(raw[i+4:])
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return bytes.TrimSpace(raw[i+2:])
	}
	return bytes.TrimSpace(raw)
}
