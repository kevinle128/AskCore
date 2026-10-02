package main

import (
	"fmt"
	"io"

	"AskCore/pkg/protocol"
)

// printReply writes the outcome of print mode, as Pi's runPrintMode does
// after the last prompt. Only the last message counts. An assistant reply
// writes each text block and a newline to stdout and gives 0. An error or
// aborted reply writes its error text to stderr and gives 1. Anything else
// writes nothing and gives 0.
func printReply(msgs []protocol.Message, stdout, stderr io.Writer) int {
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
		report(stderr, text)
		return 1
	}
	for _, b := range last.Content {
		if t, ok := b.(protocol.Text); ok {
			if _, err := fmt.Fprintln(stdout, t.Text); err != nil {
				report(stderr, err)
				return 1
			}
		}
	}
	return 0
}
