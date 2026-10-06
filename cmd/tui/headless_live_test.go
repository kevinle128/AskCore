package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// mathTool is a test tool that applies op to two numbers a and b. It does
// not ship with ask; the test injects it into the real headless agent.
type mathTool struct {
	name, desc string
	op         func(a, b float64) float64
}

func (m mathTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name:        m.name,
		Description: m.desc,
		Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
	}
}

func (m mathTool) Execute(ctx context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("%s: %w", m.name, err)
	}
	text := strconv.FormatFloat(m.op(in.A, in.B), 'g', -1, 64)
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}}, nil
}

var (
	addTool = mathTool{"add", "Add two numbers and return a + b.", func(a, b float64) float64 { return a + b }}
	mulTool = mathTool{"mul", "Multiply two numbers and return a * b.", func(a, b float64) float64 { return a * b }}
)

// TestLiveHeadlessMathQwen runs "1+1*2" through the same agent setup and run
// code as
//
//	ask -p "1+1*2" --provider alibaba-token-plan --model qwen3.7-max --mode json
//
// with the add and mul test tools injected, and reads the JSONL output. It calls the real model, so it runs only with
// ASK_LIVE=1 and a Token Plan key. ASK_LIVE_MODEL picks another model.
func TestLiveHeadlessMathQwen(t *testing.T) {
	if os.Getenv("ASK_LIVE") != "1" {
		t.Skip("set ASK_LIVE=1 to call the real model")
	}
	if os.Getenv("ALIBABA_TOKEN_PLAN_API_KEY") == "" && os.Getenv("ASK_ALIBABA_TOKEN_PLAN_API_KEY") == "" {
		t.Skip("no Token Plan API key in the environment")
	}
	model := "qwen3.7-max"
	if m := os.Getenv("ASK_LIVE_MODEL"); m != "" {
		model = m
	}

	// ask uses the default HTTP client. Close its idle connections so the
	// goroutine leak check of TestMain does not see them.
	t.Cleanup(http.DefaultClient.CloseIdleConnections)

	ag, err := newHeadlessAgent(options{provider: anthropic.ProviderID, model: model}, os.Getenv, addTool, mulTool)
	require.NoError(t, err)
	var out, errb bytes.Buffer
	code := runHeadless(ag, []string{"1+1*2"}, modeJSON, &out, &errb, nil)
	require.Equal(t, 0, code, errb.String())

	type call struct {
		Name string
		Args map[string]float64
	}
	var calls []call
	var final string
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		var ev struct {
			Type     string          `json:"type"`
			ToolName string          `json:"toolName"`
			Args     json.RawMessage `json:"args"`
			Result   json.RawMessage `json:"result"`
			Message  struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
				Error      string `json:"errorMessage"`
				Content    []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Thinking string `json:"thinking"`
				} `json:"content"`
			} `json:"message"`
		}
		require.NoError(t, json.Unmarshal(sc.Bytes(), &ev))
		switch ev.Type {
		case "turn_start":
			t.Log("---- turn_start")
		case "tool_execution_start":
			var args map[string]float64
			_ = json.Unmarshal(ev.Args, &args)
			calls = append(calls, call{ev.ToolName, args})
			t.Logf("ACT     %s %s", ev.ToolName, ev.Args)
		case "tool_execution_end":
			t.Logf("OBSERVE %s -> %s", ev.ToolName, ev.Result)
		case "message_end":
			if ev.Message.Role != "assistant" {
				continue
			}
			require.NotEqual(t, "error", ev.Message.StopReason, ev.Message.Error)
			var text strings.Builder
			for _, c := range ev.Message.Content {
				switch c.Type {
				case "thinking":
					t.Logf("REASON  (thinking) %s", strings.Join(strings.Fields(c.Thinking), " "))
				case "text":
					text.WriteString(c.Text)
				}
			}
			if text.Len() > 0 {
				t.Logf("REASON  (text) %s", text.String())
			}
			final = text.String()
		}
	}
	require.NoError(t, sc.Err())
	t.Logf("FINAL ANSWER %q, tool calls %+v", final, calls)

	require.Contains(t, final, "3")
	mulAt, addAt := -1, -1
	for i, c := range calls {
		pair := (c.Args["a"] == 1 && c.Args["b"] == 2) || (c.Args["a"] == 2 && c.Args["b"] == 1)
		if c.Name == "mul" && pair && mulAt < 0 {
			mulAt = i
		}
		if c.Name == "add" && pair && addAt < 0 {
			addAt = i
		}
	}
	require.GreaterOrEqual(t, mulAt, 0, "no mul(1,2) call")
	require.GreaterOrEqual(t, addAt, 0, "no add(1,2) call")
	require.Less(t, mulAt, addAt, "add must use the result of mul")
}
