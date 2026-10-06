package providers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The codes of a Failure. They follow the codes of the DeepSeek transport, so
// that a retry policy can name them. A status that no rule maps has the code
// HTTP_<status>; see HTTPCode.
const (
	CodeAuth           = "AUTH"
	CodeQuota          = "QUOTA"
	CodeRateLimit      = "RATE_LIMIT"
	CodeContextWindow  = "CONTEXT_WINDOW_EXCEEDED"
	CodeInvalidRequest = "INVALID_REQUEST"
	CodeServer         = "SERVER"
	CodeTimeout        = "TIMEOUT"
	CodeTransport      = "TRANSPORT"
	CodeEmptyResponse  = "EMPTY_RESPONSE"
	CodeStreamClosed   = "STREAM_CLOSED"
	CodeMalformed      = "MALFORMED_RESPONSE"
	// CodeUnknown is for a Go error that holds no provider fact. A provider
	// response never gets it: it would change what a retry policy accepts.
	CodeUnknown = "UNKNOWN"
)

// HTTPCode is the code of an HTTP status that no other rule maps.
func HTTPCode(status int) string { return fmt.Sprintf("HTTP_%d", status) }

// Failure is the typed failure of a model request. Code is one of the Code
// constants or HTTP_<status>. Status is the HTTP status, or zero when the
// failure has none. RetryAfter is the delay that the provider asked for in
// Retry-After; it is zero when the provider sent none that is finite and
// above zero.
//
// A Failure unwraps to the sentinel of its code (ErrRateLimited for
// RATE_LIMIT and so on) and to the underlying cause, so errors.Is finds both.
type Failure struct {
	Code       string
	Status     int
	RetryAfter time.Duration
	Message    string
	cause      error
}

// NewFailure returns a Failure. message is the text that Error returns after
// CleanDiagnostic; an empty message takes the text of cause.
func NewFailure(code string, status int, retryAfter time.Duration, message string, cause error) *Failure {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	return &Failure{Code: code, Status: status, RetryAfter: retryAfter, Message: message, cause: cause}
}

// Error returns the cleaned text of the failure.
func (f *Failure) Error() string { return CleanDiagnostic(errors.New(f.Message)) }

// Unwrap returns the sentinel of the code and the cause.
func (f *Failure) Unwrap() []error {
	var out []error
	if s := sentinelOf(f.Code); s != nil {
		out = append(out, s)
	}
	if f.cause != nil {
		out = append(out, f.cause)
	}
	return out
}

func sentinelOf(code string) error {
	switch code {
	case CodeRateLimit:
		return ErrRateLimited
	case CodeAuth:
		return ErrAuthentication
	case CodeQuota:
		return ErrAllowanceExhausted
	case CodeInvalidRequest:
		return ErrUnsupportedRequest
	case CodeTransport:
		return ErrTransport
	case CodeStreamClosed:
		return ErrStreamIncomplete
	}
	return nil
}

// AsFailure finds the Failure in the chain of err.
func AsFailure(err error) (*Failure, bool) {
	var f *Failure
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// CodeOf is the code of the Failure in err. An error that wraps one of the
// sentinels of this package has the code of that sentinel. Any other error holds
// no provider fact and has CodeUnknown.
func CodeOf(err error) string {
	if f, ok := AsFailure(err); ok {
		return f.Code
	}
	return sentinelCode(err)
}

// sentinelCode is the code of the sentinel that err wraps, or CodeUnknown.
func sentinelCode(err error) string {
	switch {
	case err == nil:
	case errors.Is(err, ErrAuthentication):
		return CodeAuth
	case errors.Is(err, ErrAllowanceExhausted):
		return CodeQuota
	case errors.Is(err, ErrRateLimited):
		return CodeRateLimit
	case errors.Is(err, ErrUnsupportedRequest):
		return CodeInvalidRequest
	case errors.Is(err, ErrTransport):
		return CodeTransport
	case errors.Is(err, ErrStreamIncomplete):
		return CodeStreamClosed
	}
	return CodeUnknown
}

// typedFailure returns err as a *Failure when it is none yet and it wraps one of
// the sentinels, so that a failure of the adapter before the request, such as a
// missing key, has its code too. msg is the text of the failure. An error with
// no sentinel stays as it is.
func typedFailure(msg string, err error) error {
	if _, ok := AsFailure(err); ok || err == nil {
		return err
	}
	if code := sentinelCode(err); code != CodeUnknown {
		return NewFailure(code, 0, 0, msg, err)
	}
	return err
}

// ParseRetryAfter reads a Retry-After value: a number of seconds, or an HTTP
// date. It returns zero unless the delay is finite and above zero.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(value, 64); err == nil {
		if math.IsNaN(secs) || math.IsInf(secs, 0) || secs <= 0 || secs > float64(math.MaxInt64/int64(time.Second)) {
			return 0
		}
		return time.Duration(secs * float64(time.Second))
	}
	if at, err := http.ParseTime(value); err == nil {
		if d := at.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// ClassifyProvider maps the facts of a provider error to a code. status is the
// HTTP status, or zero for an in-band error event; errType and errCode are the
// provider error type and code, and message is the provider text. The order of
// the rules is the order of the DeepSeek transport.
func ClassifyProvider(status int, errType, errCode, message string) string {
	detail := errType + " " + errCode + " " + message
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden ||
		errType == "authentication_error" || errType == "permission_error":
		return CodeAuth
	case status == http.StatusPaymentRequired || errCode == "subscription_sharing_usage_limit_exceeded" || isQuotaDetail(detail):
		return CodeQuota
	case status == http.StatusTooManyRequests || errType == "rate_limit_error" || errCode == "rate_limit_exceeded":
		return CodeRateLimit
	case isContextWindowDetail(detail):
		return CodeContextWindow
	case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge || errType == "invalid_request_error":
		return CodeInvalidRequest
	case status >= 500 || errType == "api_error" || errType == "overloaded_error":
		return CodeServer
	case status == 0:
		return CodeServer
	}
	return HTTPCode(status)
}

var (
	quotaPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\binsufficient[\s_-]+(?:quota|balance|credits?)\b`),
		regexp.MustCompile(`(?i)\b(?:quota|usage[\s_-]+limit)[\s_-]+(?:exceeded|exhausted|reached)\b`),
		regexp.MustCompile(`(?i)\bexceed(?:ed|s)?[\s_-]+(?:(?:your|the)[\s_-]+)?(?:current[\s_-]+)?quota\b`),
		regexp.MustCompile(`(?i)\b(?:balance|credits?)[\s_-]+(?:exhausted|depleted)\b`),
		regexp.MustCompile(`(?i)\bout[\s_-]+of[\s_-]+(?:credits?|budget)\b`),
	}
	contextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[^a-z0-9])context[\s_-](?:length|window)[\s_-](?:exceed(?:ed|s)?|overflow(?:ed)?|limit[\s_-]exceeded)(?:$|[^a-z0-9])`),
		regexp.MustCompile(`(?i)\b(?:maximum|max)(?:\s+(?:allowed|supported))?\s+context\s+(?:length|window)\b`),
		regexp.MustCompile(`(?i)\b(?:request|prompt|input|messages?)\s+(?:is\s+|are\s+)?too\s+(?:large|long)\s+for\s+(?:(?:this|the)\s+)?(?:model(?:'s)?\s+)?context(?:\s+window)?\b`),
		regexp.MustCompile(`(?i)\b(?:input|prompt|request)\s+(?:is\s+)?too\s+(?:long|large)\s+for\s+(?:this|the)\s+model\b`),
		regexp.MustCompile(`(?i)\b(?:input|prompt|request|messages?)\b.{0,40}\b(?:exceed(?:s|ed)?|overflows?|is\s+larger\s+than)\b.{0,40}\b(?:the\s+)?(?:model(?:'s)?\s+)?context(?:\s+(?:length|window))?\b`),
	}
)

func isQuotaDetail(detail string) bool {
	for _, re := range quotaPatterns {
		if re.MatchString(detail) {
			return true
		}
	}
	return false
}

func isContextWindowDetail(detail string) bool {
	for _, re := range contextPatterns {
		if re.MatchString(detail) {
			return true
		}
	}
	return false
}
