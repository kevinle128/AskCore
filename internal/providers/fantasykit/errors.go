package fantasykit

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"charm.land/fantasy"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

var errStopUnavailable = errors.New("stop reason unavailable")

// Fail maps a stream error onto Assembler.Fail. Request abort wins; every
// other error becomes a *providers.Failure (see Classify), so a caller finds
// the code, the status and the Retry-After delay with errors.As.
func Fail(a *providers.Assembler, reqCtx, runCtx context.Context, idle error, err error) {
	if reqCtx != nil && reqCtx.Err() != nil {
		a.Fail(protocol.StopAborted, "Request was aborted", reqCtx.Err())
		return
	}
	f := Classify(runCtx, err, "", idle)
	a.Fail(protocol.StopError, f.Message, f)
}

// Classify maps err to a typed failure. An error that is a Failure already
// stays as it is. idle is the idle-timeout error; when it is the error, or the
// cancel cause of runCtx, the failure is TIMEOUT. An unexpected end of file
// after a body that ended cleanly (see TrackBody) is STREAM_CLOSED; the same
// error after a failed read is TRANSPORT.
// extraCode is a provider error code that the adapter read from the wire for
// itself, such as the code of a failed Responses event; it can be empty.
//
// The rules follow the DeepSeek transport: an HTTP error or an in-band error
// event gets its code from the status, the error type and the message
// (providers.ClassifyProvider). An error with no response, such as a reset
// connection or a body that failed to read, is TRANSPORT. A body that is not
// valid JSON is MALFORMED_RESPONSE. A Go error that holds no provider fact is
// UNKNOWN.
func Classify(runCtx context.Context, err error, extraCode string, idle error) *providers.Failure {
	if f, ok := providers.AsFailure(err); ok {
		return f
	}
	var runCause error
	if runCtx != nil {
		runCause = context.Cause(runCtx)
	}
	if idle != nil && (errors.Is(err, idle) || errors.Is(runCause, idle)) {
		return providers.NewFailure(providers.CodeTimeout, 0, 0, "idle timeout", idle)
	}
	if err == nil {
		return providers.NewFailure(providers.CodeUnknown, 0, 0, "stream error", nil)
	}
	if isIncompleteStream(err) && bodyEndOf(runCtx).endedClean() {
		return providers.NewFailure(providers.CodeStreamClosed, 0, 0, providers.ErrStreamIncomplete.Error(), nil)
	}
	var pe *fantasy.ProviderError
	if errors.As(err, &pe) {
		return classifyProviderError(err, pe, extraCode)
	}
	text := HTTPMessage(err)
	switch {
	case isReadError(err):
		return providers.NewFailure(providers.CodeTransport, 0, 0, text, err)
	case isMalformed(err):
		return providers.NewFailure(providers.CodeMalformed, 0, 0, text, err)
	}
	return providers.NewFailure(providers.CodeUnknown, 0, 0, text, err)
}

func classifyProviderError(err error, pe *fantasy.ProviderError, extraCode string) *providers.Failure {
	// An error event inside a stream that answered 200 has a success status; it
	// is an in-band error, so it has no HTTP status of its own.
	status := pe.StatusCode
	if status < http.StatusBadRequest {
		status = 0
	}
	detail := wireError(extractJSONBody(pe.ResponseBody))
	if detail == (wireDetail{}) {
		// The SDK puts the payload of an error event into the message text.
		detail = wireError(jsonIn(pe.Message))
	}
	if detail.code == "" {
		detail.code = extraCode
	}
	message := cmp.Or(detail.message, pe.Message)
	text := HTTPMessage(err)
	if pe.AuthError {
		return providers.NewFailure(providers.CodeAuth, status, retryAfter(pe), text, err)
	}
	if pe.ContextTooLargeErr {
		return providers.NewFailure(providers.CodeContextWindow, status, retryAfter(pe), text, err)
	}
	inBand := status == 0 && (detail.typ != "" || detail.code != "" || detail.message != "" || pe.TransientError)
	if status == 0 && !inBand {
		return providers.NewFailure(providers.CodeTransport, 0, 0, text, err)
	}
	code := providers.ClassifyProvider(status, detail.typ, detail.code, message)
	return providers.NewFailure(code, status, retryAfter(pe), text, err)
}

// retryAfter reads the Retry-After header of a provider response. The header
// map has no fixed key case, so the lookup ignores it.
func retryAfter(pe *fantasy.ProviderError) time.Duration {
	for name, value := range pe.ResponseHeaders {
		if strings.EqualFold(name, "Retry-After") {
			return providers.ParseRetryAfter(value, time.Now())
		}
	}
	return 0
}

// jsonIn returns the first JSON object in text, or nil when there is none.
func jsonIn(text string) []byte {
	i := strings.IndexByte(text, '{')
	if i < 0 {
		return nil
	}
	var raw json.RawMessage
	if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&raw); err != nil {
		return nil
	}
	return raw
}

// wireDetail is the error type, code and message of a provider error body.
type wireDetail struct{ typ, code, message string }

// wireText reads a JSON string, or the text of a number; any other value is empty.
type wireText string

func (t *wireText) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = wireText(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*t = wireText(n.String())
	}
	return nil
}

// wireError reads the three fields from the two body shapes in use: the
// "error" object of the Messages and Completions APIs, and the flat object of
// a Responses failure. A body that does not parse gives empty fields.
func wireError(body []byte) wireDetail {
	var env struct {
		Type    wireText `json:"type"`
		Code    wireText `json:"code"`
		Message wireText `json:"message"`
		Error   *struct {
			Type    wireText `json:"type"`
			Code    wireText `json:"code"`
			Message wireText `json:"message"`
		} `json:"error"`
	}
	if len(body) == 0 || json.Unmarshal(body, &env) != nil {
		return wireDetail{}
	}
	if env.Error != nil {
		return wireDetail{typ: string(env.Error.Type), code: string(env.Error.Code), message: string(env.Error.Message)}
	}
	typ := string(env.Type)
	if typ == "error" { // the envelope of an in-band event carries no class
		typ = ""
	}
	return wireDetail{typ: typ, code: string(env.Code), message: string(env.Message)}
}

// isReadError reports a failure of the connection or of a body read.
func isReadError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// isMalformed reports a stream event that is not valid JSON.
func isMalformed(err error) bool {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	return errors.As(err, &syn) || errors.As(err, &typ) || strings.Contains(err.Error(), "unexpected end of JSON input")
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

// isIncompleteStream reports a body that ended inside an event.
func isIncompleteStream(err error) bool {
	var pe *fantasy.ProviderError
	if errors.As(err, &pe) && errors.Is(pe.Cause, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}
