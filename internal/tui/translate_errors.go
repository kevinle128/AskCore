package tui

import (
	"regexp"
	"strconv"
	"strings"
)

// errorKind is how the error block names a failure and what it tells the user
// to do next. The codes are the failure codes of the model providers; a code
// that this table does not know gets the generic text.
type errorKind struct {
	kind string
	next string
}

var errorKinds = map[string]errorKind{
	"AUTH":                    {"auth", ""}, // the next action names the provider; see classifyError
	"QUOTA":                   {"quota", "Check the plan or the credit of the account, then send again."},
	"RATE_LIMIT":              {"rate limit", "Wait a moment, then send again."},
	"CONTEXT_WINDOW_EXCEEDED": {"context", "The conversation is too long for this model. Start a new session with /new, then send again."},
	"INVALID_REQUEST":         {"request", "Change the prompt or the model (ctrl+l), then send again."},
	"SERVER":                  {"server", "The provider has a problem. Send again in a moment."},
	"TIMEOUT":                 {"timeout", "Send again."},
	"TRANSPORT":               {"network", "Check the network connection, then send again."},
	"EMPTY_RESPONSE":          {"provider", "Send again."},
	"STREAM_CLOSED":           {"provider", "Send again."},
	"MALFORMED_RESPONSE":      {"provider", "Send again."},
}

const genericNext = "Send again."

// classifyError returns the kind word and the next action for a failure code.
// provider names the model provider of the failed request; the sign-in command
// of an auth failure uses it.
func classifyError(code, provider string) (kind, next string) {
	if k, ok := errorKinds[code]; ok {
		kind, next = k.kind, k.next
		if code == "AUTH" {
			next = "Run in a shell: " + loginCommand(provider) + ". Then send again."
		}
		return kind, next
	}
	if strings.HasPrefix(code, "HTTP_") {
		return "provider", genericNext
	}
	return "error", genericNext
}

// loginCommand is the shell command that signs in to a provider. Every
// message that tells the user to sign in uses it, so the text has one owner.
func loginCommand(provider string) string {
	cmd := "ask auth login"
	if provider != "" {
		cmd += " --provider " + provider
	}
	return cmd
}

var httpFailure = regexp.MustCompile(`^HTTP (\d{3})\b`)

// retryCause is the cause of a failed attempt as one line. A provider text that
// starts with an HTTP status also holds the body of the answer; the body can be
// long and is not for the user, so the cause is the status in words.
func retryCause(msg string) string {
	msg = oneLine(msg)
	m := httpFailure.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}
	code, _ := strconv.Atoi(m[1])
	word := "request failed"
	switch {
	case code == 429:
		word = "rate limit"
	case code == 408 || code == 504:
		word = "timeout"
	case code == 401 || code == 403:
		word = "auth"
	case code >= 500:
		word = "server error"
	}
	return word + " (HTTP " + m[1] + ")"
}

var apiKeyText = regexp.MustCompile(`\bsk-[A-Za-z0-9_*-]{6,}`)

// providerErrorText is the failure text of a provider as one short line for the
// error block. A text that starts with an HTTP status also holds the body of
// the answer, which can be long and can hold a masked part of the API key. The
// text then says what the status means and keeps the status code. Any other
// text stays, except that a string that looks like an API key is hidden.
func providerErrorText(msg, provider string) string {
	msg = oneLine(msg)
	m := httpFailure.FindStringSubmatch(msg)
	if m == nil {
		return apiKeyText.ReplaceAllString(msg, "[redacted]")
	}
	if provider == "" {
		provider = "The provider"
	}
	code, _ := strconv.Atoi(m[1])
	var what string
	switch {
	case code == 401:
		what = "rejected the credentials"
	case code == 403:
		what = "denied access"
	case code == 429:
		what = "is rate limited"
	case code == 408 || code == 504:
		what = "timed out"
	case code >= 500:
		what = "had a server error"
	default:
		what = "rejected the request"
	}
	return provider + " " + what + " (HTTP " + m[1] + ")"
}

// sentence makes a message end with a full stop, and puts it on one line.
func sentence(s string) string {
	s = oneLine(s)
	if s == "" {
		return s
	}
	switch s[len(s)-1] {
	case '.', '!', '?':
		return s
	}
	return s + "."
}

// firstLine returns the first non-empty line of s, without surrounding space.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return oneLine(line)
		}
	}
	return ""
}

// --- split text ---

var blankLineRun = regexp.MustCompile(`\n[ \t]*\n`)

// closedCut returns the length of the text that cannot change any more: the
// closed chunks of a markdown text, or the paragraphs of a thinking text that
// are followed by more text. It is zero when nothing is closed.
func closedCut(thinking bool, rest string) int {
	if thinking {
		cut := 0
		for _, loc := range blankLineRun.FindAllStringIndex(rest, -1) {
			if strings.TrimSpace(rest[loc[1]:]) != "" {
				cut = loc[0]
			}
		}
		return cut
	}
	cut := 0
	for _, c := range splitMarkdown(rest) {
		if !c.closed {
			break
		}
		cut = c.end
	}
	return cut
}

// leadingBlankBytes is the length of the empty lines at the start of s. For a
// thinking text it is all white space, because the paragraphs are drawn again
// with their own spacing.
func leadingBlankBytes(s string, thinking bool) int {
	if thinking {
		return len(s) - len(strings.TrimLeft(s, " \t\r\n"))
	}
	n := 0
	for {
		i := strings.IndexByte(s[n:], '\n')
		if i < 0 || strings.TrimSpace(s[n:n+i]) != "" {
			return n
		}
		n += i + 1
	}
}

// splitParagraphs cuts a thinking text at its empty lines.
func splitParagraphs(s string) []string {
	var out []string
	for _, p := range blankLineRun.Split(strings.TrimSpace(s), -1) {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}
