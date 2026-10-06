package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/internal/tools"
)

// These tests run the real Token Plan adapter through the same agent setup
// and run code as ask -p. The model replies come from cassettes in
// testdata/cassettes. Re-record one with:
//
//	ASK_RECORD=1 go test -run '^TestName$' ./cmd/tui

// jsonEvent is the part of a JSON-mode event line that the tests read.
type jsonEvent struct {
	Type     string          `json:"type"`
	ToolName string          `json:"toolName"`
	Args     json.RawMessage `json:"args"`
	Message  struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// runCassetteJSON runs prompt in JSON mode on the cassette of the test and
// returns the events.
func runCassetteJSON(t *testing.T, prompt string, extra ...tools.Tool) []jsonEvent {
	t.Helper()
	o, getenv := useCassette(t, anthropic.ModelID)
	ag, err := newHeadlessAgent(o, getenv, extra...)
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{prompt}, modeJSON, &out, &errb, nil)
	require.Equal(t, 0, code, errb.String())

	var events []jsonEvent
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		var ev jsonEvent
		require.NoError(t, json.Unmarshal(sc.Bytes(), &ev), sc.Text())
		events = append(events, ev)
	}
	require.NoError(t, sc.Err())
	return events
}

// assistantEnds returns the assistant messages of the message_end events.
func assistantEnds(events []jsonEvent) []jsonEvent {
	var out []jsonEvent
	for _, ev := range events {
		if ev.Type == "message_end" && ev.Message.Role == "assistant" {
			out = append(out, ev)
		}
	}
	return out
}

func contentText(ev jsonEvent, kind string) string {
	var b strings.Builder
	for _, c := range ev.Message.Content {
		if c.Type == kind {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// toolCalls returns the tool name and arguments of each tool_execution_start.
func toolCalls(t *testing.T, events []jsonEvent) map[string]map[string]float64 {
	t.Helper()
	calls := map[string]map[string]float64{}
	for _, ev := range events {
		if ev.Type != "tool_execution_start" {
			continue
		}
		var args map[string]float64
		require.NoError(t, json.Unmarshal(ev.Args, &args))
		calls[ev.ToolName] = args
	}
	return calls
}

func TestCassetteTextReply(t *testing.T) {
	o, getenv := useCassette(t, anthropic.ModelID)
	ag, err := newHeadlessAgent(o, getenv)
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"Reply with exactly the word pong and nothing else."}, modePrint, &out, &errb, nil)
	require.Equal(t, 0, code, errb.String())
	assert.Equal(t, "pong", strings.ToLower(strings.TrimSpace(out.String())))
}

func TestCassetteOneToolCall(t *testing.T) {
	events := runCassetteJSON(t, "Use the add tool to compute 2 + 3. After the tool result, reply with only the number.", addTool, mulTool)
	assert.Equal(t, map[string]map[string]float64{"add": {"a": 2, "b": 3}}, toolCalls(t, events))
	ends := assistantEnds(events)
	require.Len(t, ends, 2, "one reply with the tool call, one with the answer")
	assert.Equal(t, "toolUse", ends[0].Message.StopReason)
	assert.Equal(t, "stop", ends[1].Message.StopReason)
	assert.Contains(t, contentText(ends[1], "text"), "5")
}

func TestCassetteParallelToolCalls(t *testing.T) {
	events := runCassetteJSON(t, "In one reply, call add with a=1 and b=2 and also call mul with a=3 and b=4. Do not wait between them. After the results, reply with both numbers.", addTool, mulTool)
	assert.Equal(t, map[string]map[string]float64{"add": {"a": 1, "b": 2}, "mul": {"a": 3, "b": 4}}, toolCalls(t, events))
	ends := assistantEnds(events)
	require.Len(t, ends, 2, "both calls in the first reply, the answer in the second")
	final := contentText(ends[1], "text")
	assert.Contains(t, final, "3")
	assert.Contains(t, final, "12")
}

func TestCassetteJSONEventOrder(t *testing.T) {
	events := runCassetteJSON(t, "Call the echo tool with the text hello. After the tool result, reply with only the word done.")
	var seq []string
	for _, ev := range events {
		switch ev.Type {
		case "message_update":
			continue // the delta count depends on the stream chunks
		case "message_start", "message_end":
			seq = append(seq, ev.Type+":"+ev.Message.Role)
		default:
			seq = append(seq, ev.Type)
		}
	}
	assert.Equal(t, []string{
		"agent_start",
		"cycle_start",
		"turn_start",
		"message_start:user", "message_end:user",
		"attempt_start",
		"message_start:assistant", "message_end:assistant",
		"attempt_end",
		"tool_execution_start", "tool_execution_end",
		"message_start:toolResult", "message_end:toolResult",
		"turn_end",
		"turn_start",
		"attempt_start",
		"message_start:assistant", "message_end:assistant",
		"attempt_end",
		"turn_end",
		"cycle_end",
		"agent_end",
		"agent_settled",
	}, seq)
}

func TestCassetteThinkingReply(t *testing.T) {
	events := runCassetteJSON(t, "Think step by step: what is 17 * 3? Reply with only the number.")
	ends := assistantEnds(events)
	require.Len(t, ends, 1)
	var thinking bool
	for _, c := range ends[0].Message.Content {
		if c.Type == "thinking" {
			thinking = true
		}
	}
	assert.True(t, thinking, "the medium thinking level must give a thinking block")
	assert.Contains(t, contentText(ends[0], "text"), "51")
}

// TestCapturedSessionReplays replays a cassette that ASK_CAPTURE wrote from a
// real run of
//
//	ask -p "Use the echo tool with the text captured, then reply with only the word ok." --provider alibaba-token-plan
//
// A captured session needs no re-record command; capture it again instead.
func TestCapturedSessionReplays(t *testing.T) {
	o, getenv := useCassette(t, anthropic.ModelID)
	ag, err := newHeadlessAgent(o, getenv)
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"Use the echo tool with the text captured, then reply with only the word ok."}, modePrint, &out, &errb, nil)
	require.Equal(t, 0, code, errb.String())
	assert.Equal(t, "ok", strings.ToLower(strings.TrimSpace(out.String())))
}
