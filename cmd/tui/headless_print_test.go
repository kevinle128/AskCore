package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/pkg/protocol"
)

func textUser(text string) protocol.UserMessage {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}

func textAssistant(text string) protocol.AssistantMessage {
	return protocol.AssistantMessage{
		Content:    []protocol.AssistantBlock{protocol.Text{Text: text}},
		StopReason: protocol.StopStop,
	}
}

func TestPrintReplySkipsTrailingSystemReminders(t *testing.T) {
	notice := textUser(protocol.SystemReminder("Background job bash-1 finished."))
	tests := []struct {
		name string
		msgs []protocol.Message
		want string
	}{
		{"one notice after the reply", []protocol.Message{textUser("go"), textAssistant("the reply"), notice}, "the reply\n"},
		{"two notices after the reply", []protocol.Message{textUser("go"), textAssistant("the reply"), notice, notice}, "the reply\n"},
		{"notice before the reply", []protocol.Message{textUser("go"), notice, textAssistant("the reply")}, "the reply\n"},
		{"only notices", []protocol.Message{notice, notice}, ""},
		{"a plain user message still hides the reply", []protocol.Message{textAssistant("old"), textUser("new")}, ""},
		{"a plain user message before a notice hides the reply", []protocol.Message{textAssistant("old"), textUser("new"), notice}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := printReply(tc.msgs, false, &stdout, &stderr)
			assert.Equal(t, 0, code)
			assert.Equal(t, tc.want, stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

func TestPrintReplyReportsAnErrorBehindTrailingNotice(t *testing.T) {
	failed := textAssistant("")
	failed.StopReason = protocol.StopError
	msg := "boom"
	failed.ErrorMessage = &msg
	notice := textUser(protocol.SystemReminder("Background job bash-1 finished."))

	var stdout, stderr bytes.Buffer
	code := printReply([]protocol.Message{textUser("go"), failed, notice}, false, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "boom\n", stderr.String())
}

func TestPrintReplyNamesAnExplicitOverflow(t *testing.T) {
	failed := textAssistant("")
	failed.StopReason = protocol.StopError
	msg := "prompt is too long: 1000500 tokens > 1000000 maximum"
	failed.ErrorMessage = &msg

	var stdout, stderr bytes.Buffer
	code := printReply([]protocol.Message{textUser("go"), failed}, true, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "Context window exceeded: prompt is too long: 1000500 tokens > 1000000 maximum. "+
		"Start a new session or choose a model with a larger context window.\n", stderr.String())
}

func TestPrintReplyKeepsTheReplyOfASilentOverflow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := printReply([]protocol.Message{textUser("go"), textAssistant("the reply")}, true, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Equal(t, "the reply\n", stdout.String())
	assert.Equal(t, "Context window exceeded: the provider reported more input tokens than the context window of the model. "+
		"Start a new session or choose a model with a larger context window.\n", stderr.String())
}

func TestPrintReplyOverflowTextEndingInAPeriodGetsOnePeriod(t *testing.T) {
	failed := textAssistant("")
	failed.StopReason = protocol.StopError
	msg := "too long."
	failed.ErrorMessage = &msg

	var stdout, stderr bytes.Buffer
	printReply([]protocol.Message{textUser("go"), failed}, true, &stdout, &stderr)

	assert.Contains(t, stderr.String(), "Context window exceeded: too long. Start a new session")
}
