package main

import (
	"fmt"
	"io"
	"strings"

	"AskCore/pkg/protocol"
)

// contextOverflowCause is the cause that a cycle_end event gives to a run that
// ended because the conversation does not fit the context window.
const contextOverflowCause = "context_overflow"

const overflowAdvice = "Start a new session or choose a model with a larger context window."

// silentOverflowText is written after a reply that the provider accepted but
// whose input already filled the window: the next request would fail.
const silentOverflowText = "the provider reported more input tokens than the context window of the model"

// printReply writes the outcome of print mode, as Pi's runPrintMode does
// after the last prompt. Only the last message counts, after the trailing
// system reminders: a job notice that arrives after the reply must not hide
// it. An assistant reply writes each text block and a newline to stdout and
// gives 0. An error or aborted reply writes its error text to stderr and gives
// 1. Anything else writes nothing and gives 0. A failed stdout write gives 1;
// the caller reports it. When overflow is true the run ended with a context
// overflow: it gives 1 and names the overflow on stderr. A reply that came
// with the overflow (the silent form) is still written to stdout first.
func printReply(msgs []protocol.Message, overflow bool, stdout, stderr io.Writer) int {
	for len(msgs) > 0 {
		u, ok := msgs[len(msgs)-1].(protocol.UserMessage)
		if !ok || !protocol.IsSystemReminder(u) {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	if len(msgs) == 0 {
		return 0
	}
	last, ok := msgs[len(msgs)-1].(protocol.AssistantMessage)
	if !ok {
		return 0
	}
	if last.StopReason == protocol.StopError || last.StopReason == protocol.StopAborted {
		text := fmt.Sprintf("Request %s", last.StopReason)
		if last.ErrorMessage != nil && *last.ErrorMessage != "" {
			text = *last.ErrorMessage
		}
		if overflow {
			text = strings.TrimRight(text, ". ")
		}
		reportFailure(stderr, text, overflow)
		return 1
	}
	for _, b := range last.Content {
		if t, ok := b.(protocol.Text); ok {
			if _, err := fmt.Fprintln(stdout, t.Text); err != nil {
				return 1
			}
		}
	}
	if overflow {
		reportFailure(stderr, silentOverflowText, true)
		return 1
	}
	return 0
}

// reportFailure writes text to stderr, with the overflow advice when the
// failure is a context overflow.
func reportFailure(stderr io.Writer, text string, overflow bool) {
	if !overflow {
		report(stderr, text)
		return
	}
	report(stderr, "Context window exceeded: "+text+". "+overflowAdvice)
}
