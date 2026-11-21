package sessions_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func sampleMessages() []protocol.Message {
	think := protocol.ThinkingHigh
	return []protocol.Message{
		protocol.SystemMessage{Content: []protocol.Text{{Text: "sys"}}, Timestamp: 3},
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}, Timestamp: 1},
		protocol.AssistantMessage{
			Content:       []protocol.AssistantBlock{protocol.Text{Text: "hello"}},
			API:           "responses",
			Provider:      "openai",
			Model:         "gpt-5",
			ThinkingLevel: &think,
			Usage:         protocol.Usage{Input: 3, Output: 4, TotalTokens: 7},
			StopReason:    protocol.StopReason("stop"),
			Timestamp:     2,
		},
		protocol.ToolResultMessage{
			ToolCallID: "call-1",
			ToolName:   "echo",
			Content:    []protocol.UserBlock{protocol.Text{Text: "out"}},
			Details:    json.RawMessage(`{"k":1}`),
			IsError:    true,
			Timestamp:  4,
		},
		protocol.CustomMessage{
			CustomType: "task-notification",
			Content:    []protocol.UserBlock{protocol.Text{Text: "done"}},
			Display:    true,
			Timestamp:  5,
		},
	}
}

// sampleEntries holds one value for each wire word.
func sampleEntries() map[string]sessions.Entry {
	temp := 0.25
	msgs := sampleMessages()
	return map[string]sessions.Entry{
		"message":         sessions.MessageEntry{Message: msgs[1], InputID: "in-1"},
		"cycle_opened":    sessions.CycleOpened{CycleID: "c1"},
		"cycle_closed":    sessions.CycleClosed{CycleID: "c1", Reason: "aborted", Cause: "user", Code: "X"},
		"turn_opened":     sessions.TurnOpened{CycleID: "c1"},
		"turn_closed":     sessions.TurnClosed{CycleID: "c1"},
		"attempt_settled": sessions.AttemptSettled{AttemptID: "a1", Outcome: "failed", Usage: &protocol.Usage{Input: 1}, CostUnknown: true, NoOutput: true, Failure: &sessions.Failure{Code: "AUTH", Text: "no"}, Binding: &sessions.AuthBinding{Provider: "openai", Method: "oauth"}},
		"tool_call":       sessions.ToolCall{AssistantEntry: 4, CallID: "call-1"},
		"retry_scheduled": sessions.RetryScheduled{RetryID: "r1", CycleID: "c1", Turn: 1, Provider: "openai", PolicyKey: "k", Retry: 1, MaxRetries: 3, DelayMs: 500, Failure: sessions.Failure{Code: "SERVER", Text: "x"}},
		"retry_started":   sessions.RetryStarted{RetryID: "r1", Retry: 1},
		"request_delta": sessions.RequestDelta{
			AttemptID: "a1",
			Start:     2,
			Removed:   []protocol.Message{msgs[0], msgs[1]},
			Added:     []protocol.Message{msgs[2], msgs[3], msgs[4]},
			Model:     sessions.ModelRef{Provider: "openai", API: "responses", ID: "gpt-5", BaseURLHost: "https://example.test", HeaderNames: []string{"X-One"}},
			Options:   sessions.RequestOptions{MaxTokens: 10, Temperature: &temp, Reasoning: protocol.ThinkingLow, SessionID: "s"},
			Prepared:  &sessions.PreparedRequest{Provider: "openai", API: "responses", Model: "gpt-5", RetryPolicy: sessions.RetryPolicy{Key: "k", MaxRetries: 2}, Headers: map[string]string{"A": "b"}},
		},
		"system_snapshot": sessions.SystemSnapshot{SystemPrompt: "p", Tools: []protocol.ToolDecl{{Name: "t", Description: "d", Parameters: json.RawMessage(`{"a":1}`)}}},
		"input_outcome":   sessions.InputOutcome{InputID: "in-1", Accepted: false, Reason: "busy"},
		"model_change":    sessions.ModelChange{Model: sessions.ModelRef{Provider: "xai", API: "responses", ID: "grok"}},
		"thinking_change": sessions.ThinkingChange{Level: protocol.ThinkingXHigh},
		"context_edit":    sessions.ContextEdit{Target: 3, Replacement: &sessions.ContextReplacement{Message: msgs[1]}},
	}
}

func TestCodecRoundTripsEveryEntryType(t *testing.T) {
	samples := sampleEntries()
	words := sessions.WireWords()
	require.Equal(t, keys(words), keys(samples), "every wire word needs a sample and every sample a wire word")

	for word, e := range samples {
		t.Run(word, func(t *testing.T) {
			line, err := sessions.EncodeEntry(7, e)
			require.NoError(t, err)
			assert.NotContains(t, string(line), "\n")
			got, decoded, err := sessions.DecodeEntry(line)
			require.NoError(t, err)
			assert.Equal(t, 7, got)
			assert.True(t, reflect.DeepEqual(e, decoded), "want %#v got %#v", e, decoded)
		})
	}

	t.Run("context edit without replacement", func(t *testing.T) {
		line, err := sessions.EncodeEntry(0, sessions.ContextEdit{Target: 2})
		require.NoError(t, err)
		_, decoded, err := sessions.DecodeEntry(line)
		require.NoError(t, err)
		assert.Equal(t, sessions.ContextEdit{Target: 2}, decoded)
	})
}

// TestEveryEntryTypeHasWireWord reads entry.go, so an entry type added there
// without a codec word fails here.
func TestEveryEntryTypeHasWireWord(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "entry.go", nil, 0)
	require.NoError(t, err)
	var declared []string
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "entry" || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
			declared = append(declared, id.Name)
		}
	}
	require.NotEmpty(t, declared)
	var coded []string
	for _, e := range sessions.WireWords() {
		coded = append(coded, reflect.TypeOf(e).Name())
	}
	sort.Strings(declared)
	sort.Strings(coded)
	assert.Equal(t, declared, coded)
}

func TestCodecRefusesUnknownType(t *testing.T) {
	_, _, err := sessions.DecodeEntry([]byte(`{"seq":0,"type":"from_the_future","data":{}}`))
	assert.True(t, errors.Is(err, sessions.ErrUnknownEntryType), "got %v", err)
}

func TestCodecSkipsIgnorableUnknownType(t *testing.T) {
	seq, e, err := sessions.DecodeEntry([]byte(`{"seq":4,"type":"from_the_future","ignorable":true,"data":{"x":1}}`))
	require.NoError(t, err)
	assert.Equal(t, 4, seq)
	assert.Nil(t, e)
}

func TestCodecKeepsAdditiveDataFields(t *testing.T) {
	_, e, err := sessions.DecodeEntry([]byte(`{"seq":0,"type":"cycle_opened","data":{"cycleId":"c","later":true}}`))
	require.NoError(t, err)
	assert.Equal(t, sessions.CycleOpened{CycleID: "c"}, e)
}

func TestCodecCorruptLine(t *testing.T) {
	for name, line := range map[string]string{
		"not json":         `{"seq":0,"type":"cycle_op`,
		"empty":            ``,
		"no seq":           `{"type":"cycle_opened","data":{"cycleId":"c"}}`,
		"negative seq":     `{"seq":-1,"type":"cycle_opened","data":{"cycleId":"c"}}`,
		"no data":          `{"seq":0,"type":"cycle_opened"}`,
		"unknown envelope": `{"seq":0,"type":"cycle_opened","extra":1,"data":{"cycleId":"c"}}`,
		"bad data":         `{"seq":0,"type":"cycle_opened","data":"text"}`,
		"bad message":      `{"seq":0,"type":"message","data":{"message":{"content":[]}}}`,
		"trailing text":    `{"seq":0,"type":"cycle_opened","data":{"cycleId":"c"}} x`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := sessions.DecodeEntry([]byte(line))
			assert.True(t, errors.Is(err, sessions.ErrCorruptLine), "got %v", err)
		})
	}
}

func TestEncodeEntryRejectsUnencodable(t *testing.T) {
	for name, e := range map[string]sessions.Entry{
		"nil entry":     nil,
		"nil message":   sessions.MessageEntry{},
		"nil in delta":  sessions.RequestDelta{Added: []protocol.Message{nil}},
		"nil in edit":   sessions.ContextEdit{Replacement: &sessions.ContextReplacement{}},
		"pointer entry": &sessions.CycleOpened{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sessions.EncodeEntry(0, e)
			assert.True(t, errors.Is(err, sessions.ErrUnencodableEntry), "got %v", err)
		})
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	h := sessions.Header{
		Version:       sessions.FormatVersion,
		ID:            "1863000000000000000",
		Cwd:           "/abs/canonical",
		CreatedAt:     time.Date(2026, 11, 18, 11, 0, 0, 0, time.UTC),
		ParentSession: "1862000000000000000",
		ParentSeq:     9,
	}
	line, err := sessions.EncodeHeader(h)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(line), `{"type":"session","version":1,`), string(line))
	got, err := sessions.DecodeHeader(line)
	require.NoError(t, err)
	assert.True(t, h.CreatedAt.Equal(got.CreatedAt))
	got.CreatedAt = h.CreatedAt
	assert.Equal(t, h, got)

	// A zero version is written as the current one.
	line, err = sessions.EncodeHeader(sessions.Header{ID: "i", Cwd: "/c"})
	require.NoError(t, err)
	got, err = sessions.DecodeHeader(line)
	require.NoError(t, err)
	assert.Equal(t, sessions.FormatVersion, got.Version)
}

func TestHeaderNewerVersion(t *testing.T) {
	_, err := sessions.DecodeHeader([]byte(`{"type":"session","version":2,"id":"i","cwd":"/c","createdAt":"bad","extra":{}}`))
	assert.True(t, errors.Is(err, sessions.ErrFormatNewer), "got %v", err)
}

func TestHeaderCorrupt(t *testing.T) {
	for name, line := range map[string]string{
		"not json":     `{"type":"sess`,
		"wrong type":   `{"type":"message","version":1,"id":"i","cwd":"/c"}`,
		"no version":   `{"type":"session","id":"i","cwd":"/c"}`,
		"zero version": `{"type":"session","version":0,"id":"i","cwd":"/c"}`,
		"no id":        `{"type":"session","version":1,"cwd":"/c"}`,
		"bad time":     `{"type":"session","version":1,"id":"i","cwd":"/c","createdAt":"yesterday"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sessions.DecodeHeader([]byte(line))
			assert.True(t, errors.Is(err, sessions.ErrCorruptLine), "got %v", err)
		})
	}
	_, err := sessions.EncodeHeader(sessions.Header{})
	assert.Error(t, err)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestAttemptSettledWithoutCostUnknownDecodes(t *testing.T) {
	line := []byte(`{"seq":3,"type":"attempt_settled","data":{"attemptId":"a1","outcome":"completed","usage":{"input":5,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":6,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}}`)
	seq, e, err := sessions.DecodeEntry(line)
	require.NoError(t, err)
	assert.Equal(t, 3, seq)
	got, ok := e.(sessions.AttemptSettled)
	require.True(t, ok)
	assert.False(t, got.CostUnknown)
	assert.False(t, got.NoOutput)
	require.NotNil(t, got.Usage)
	assert.Equal(t, int64(5), got.Usage.Input)
}
