package providers

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxDiagnosticBytes caps the text that CleanDiagnostic returns.
const maxDiagnosticBytes = 512

var (
	urlPattern     = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s"'<>]+`)
	userinfoInText = regexp.MustCompile(`(?i)((?:https?|wss?)://)[^/@\s"'<>]+@`)
	bearerPattern  = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=\-]+`)
	skKeyPattern   = regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]+`)
	googleKey      = regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{16,}`)
	// headerKey matches a key name, with or without quotes, and its value in
	// plain or quoted form: x-api-key: V, api_key=V, "api_key":"V".
	headerKey = regexp.MustCompile(`(?i)(\b(?:x-api-key|api-key|api_key|apikey)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;"'}]+)`)
)

// CleanDiagnostic returns the text of err in a form that is safe to store and
// to publish. It removes the query, the fragment and the userinfo of every URL,
// replaces bearer tokens, sk- keys and key headers, and cuts the text to 512
// bytes at a character boundary. A nil error gives an empty string.
func CleanDiagnostic(err error) string {
	if err == nil {
		return ""
	}
	text := urlPattern.ReplaceAllStringFunc(err.Error(), stripURL)
	text = userinfoInText.ReplaceAllString(text, "$1")
	text = bearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = skKeyPattern.ReplaceAllString(text, "[redacted]")
	text = googleKey.ReplaceAllString(text, "[redacted]")
	text = headerKey.ReplaceAllString(text, "${1}[redacted]")
	return capBytes(text, maxDiagnosticBytes)
}

// stripURL drops the userinfo, the query and the fragment of a URL. Trailing
// punctuation that the pattern took in stays after the URL.
func stripURL(raw string) string {
	trimmed := strings.TrimRight(raw, ".,;:)]}")
	tail := raw[len(trimmed):]
	u, err := url.Parse(trimmed)
	if err != nil {
		cut := trimmed
		if i := strings.IndexAny(cut, "?#"); i >= 0 {
			cut = cut[:i]
		}
		return cut + tail
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String() + tail
}

// capBytes cuts s to at most n bytes without splitting a character.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
