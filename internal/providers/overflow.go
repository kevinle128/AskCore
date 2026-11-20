package providers

import (
	"net/http"
	"regexp"
	"strings"

	"AskCore/pkg/protocol"
)

// overflowPatterns match the text of a provider error that says the prompt does
// not fit the context window. The token patterns follow the overflow list of
// Pi (github.com/badlogic/pi-mono, packages/ai/src/utils/overflow.ts, commit
// 4c6fb7cfe, MIT license); the 429, 402 and 413 rules follow oh-my-pi
// (github.com/can1357/oh-my-pi, MIT license). The Anthropic request_too_large
// text is left out on purpose: it reports a body that is too big in bytes, which
// shortening the history does not always fix.
var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9])context[\s_-](?:length|window)[\s_-](?:exceed(?:ed|s)?|overflow(?:ed)?|limit[\s_-]exceeded)(?:$|[^a-z0-9])`),
	regexp.MustCompile(`(?i)\b(?:maximum|max)(?:\s+(?:allowed|supported))?\s+context\s+(?:length|window)\b`),
	regexp.MustCompile(`(?i)\b(?:request|prompt|input|messages?)\s+(?:is\s+|are\s+)?too\s+(?:large|long)\s+for\s+(?:(?:this|the)\s+)?(?:model(?:'s)?\s+)?context(?:\s+window)?\b`),
	regexp.MustCompile(`(?i)\b(?:input|prompt|request)\s+(?:is\s+)?too\s+(?:long|large)\s+for\s+(?:this|the)\s+model\b`),
	regexp.MustCompile(`(?i)\b(?:input|prompt|request|messages?)\b.{0,40}\b(?:exceed(?:s|ed)?|overflows?|is\s+larger\s+than)\b.{0,40}\b(?:the\s+)?(?:model(?:'s)?\s+)?context(?:\s+(?:length|window))?\b`),

	regexp.MustCompile(`(?i)prompt (?:is )?too long`),                                                                   // Anthropic, z.ai
	regexp.MustCompile(`(?i)prompt exceeds max length`),                                                                 // z.ai (CN endpoint)
	regexp.MustCompile(`(?i)input is too long for requested model`),                                                     // Amazon Bedrock
	regexp.MustCompile(`(?i)exceeds the context window`),                                                                // OpenAI (Completions and Responses)
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))`), // OpenAI-compatible proxies (LiteLLM)
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),                                                    // Google Gemini
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),                                                              // xAI
	regexp.MustCompile(`(?i)reduce the length of the messages`),                                                         // Groq
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),                                                      // OpenRouter
	regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`),                           // OpenRouter, Poolside
	regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`),            // Together AI
	regexp.MustCompile(`(?i)tokens?(?: count)?(?: of [\d,]+)? exceeds the limit of \d+`),                                // GitHub Copilot
	regexp.MustCompile(`(?i)exceeds the available context size`),                                                        // llama.cpp server
	regexp.MustCompile(`(?i)greater than the context length`),                                                           // LM Studio
	regexp.MustCompile(`(?i)context window exceeds limit`),                                                              // MiniMax
	regexp.MustCompile(`(?i)exceeded model token limit`),                                                                // Kimi
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),                                       // Mistral
	regexp.MustCompile(`(?i)prompt has [\d,]+ tokens?, but the configured context size is [\d,]+ tokens?`),              // DS4 server
	regexp.MustCompile(`(?i)model_context_window_exceeded`),                                                             // z.ai finish reason shown as error text
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),                                         // Ollama
	regexp.MustCompile(`(?i)range of input length should be`),                                                           // DashScope, Qwen
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),                                                             // generic
	regexp.MustCompile(`(?i)too many tokens`),                                                                           // generic
	regexp.MustCompile(`(?i)token limit exceeded`),                                                                      // generic
}

// overflowVetoPatterns match errors that look like an overflow but are
// throttling. "ThrottlingException: Too many tokens, please wait" would match the
// generic token pattern without this list. "throttl" covers Throttling and
// ThrottlingException in the error type, the code, or the message.
var overflowVetoPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)throttl`),
}

// notContextPatterns match a token limit that is not the context window: the
// size of the output that the request asked for. Shortening the history does
// not fix it, so it stays an invalid request.
var notContextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)too many tokens requested for output`),
}

// overflowTextStatus reports a status for which the overflow text rule applies:
// the statuses that providers use to refuse a prompt, and zero for an in-band
// error. A 5xx is a server fault whatever its text says, and it keeps its retry.
func overflowTextStatus(status int) bool {
	switch status {
	case 0, http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// throttlePrefix is the Bedrock text of an error that is not an overflow. It
// anchors at the start of the provider message.
var throttlePrefix = regexp.MustCompile(`(?i)^(?:Throttling error|Service unavailable):`)

// overflowVetoText reports a throttling text that blocks an overflow match.
func overflowVetoText(errType, errCode, message string) bool {
	if throttlePrefix.MatchString(strings.TrimSpace(message)) {
		return true
	}
	detail := errType + " " + errCode + " " + message
	for _, re := range overflowVetoPatterns {
		if re.MatchString(detail) {
			return true
		}
	}
	return false
}

// isOverflowText reports a provider error that says the prompt is too long for
// the model. A throttling text, a limit that is not the context window, and a
// status outside overflowTextStatus are never an overflow.
func isOverflowText(status int, errType, errCode, message string) bool {
	if !overflowTextStatus(status) || overflowVetoText(errType, errCode, message) {
		return false
	}
	detail := errType + " " + errCode + " " + message
	for _, re := range notContextPatterns {
		if re.MatchString(detail) {
			return false
		}
	}
	for _, re := range overflowPatterns {
		if re.MatchString(detail) {
			return true
		}
	}
	return false
}

// OverflowFlagVetoed reports that an overflow flag set by an SDK must be
// ignored for this error. A 429, a 402 or a 5xx, a rate-limit or quota error,
// and a throttling text are not an overflow, whatever the SDK guessed from the
// text.
func OverflowFlagVetoed(status int, errType, errCode, message string) bool {
	if !overflowTextStatus(status) || overflowVetoText(errType, errCode, message) {
		return true
	}
	detail := errType + " " + errCode + " " + message
	for _, re := range notContextPatterns {
		if re.MatchString(detail) {
			return true
		}
	}
	switch ClassifyProvider(status, errType, errCode, message) {
	case CodeAuth, CodeRateLimit, CodeQuota:
		return true
	}
	return false
}

// UsageOverflow reports a reply that the provider accepted but that shows the
// prompt filled the context window. A reply that stopped normally overflows when
// the prompt is above the window. A reply cut at the length limit with no output
// overflows when the prompt is at 99 percent of the window or more; some
// providers truncate the prompt to fit and then have no room left to answer. A
// window of 0 is unknown, so the result is false.
func UsageOverflow(m Model, msg protocol.AssistantMessage) bool {
	window := int64(m.ContextWindow)
	if window <= 0 {
		return false
	}
	u := msg.Usage
	prompt := u.Input + u.CacheRead + u.CacheWrite
	switch msg.StopReason {
	case protocol.StopStop:
		return prompt > window
	case protocol.StopLength:
		return u.Output == 0 && prompt*100 >= window*99
	}
	return false
}
