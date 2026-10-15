package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const (
	textAborted        = "Operation aborted"
	textBlocked        = "Tool execution was blocked"
	textBeforeDispatch = "Tool call aborted before dispatch"
)

type toolBatch struct {
	messages  []protocol.ToolResultMessage
	terminate bool
	ran       bool
}

type toolOutcome struct {
	call    protocol.ToolCall
	result  protocol.ToolExecutionResult
	isError bool
}

type preparedCall struct {
	call protocol.ToolCall
	tool tools.Tool
	args json.RawMessage
}

func (p preparedCall) handlerCall() protocol.ToolCall {
	return protocol.CloneAssistantBlock(p.call).(protocol.ToolCall)
}

func (l *loop) emitToolStart(c protocol.ToolCall) error {
	return l.emit(&protocol.ToolExecutionStart{ToolCallID: c.ID, ToolName: c.Name, Args: bytes.Clone(c.Arguments)})
}

func (l *loop) emitToolEnd(o toolOutcome) error {
	return l.emit(&protocol.ToolExecutionEnd{ToolCallID: o.call.ID, ToolName: o.call.Name, Result: o.result, IsError: o.isError})
}

func toolResultMessage(o toolOutcome) protocol.ToolResultMessage {
	content := o.result.Content
	if content == nil {
		content = []protocol.UserBlock{}
	}
	return protocol.ToolResultMessage{
		ToolCallID: o.call.ID,
		ToolName:   o.call.Name,
		Content:    content,
		Details:    o.result.Details,
		Usage:      o.result.Usage,
		IsError:    o.isError,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func errorOutcome(c protocol.ToolCall, text string) toolOutcome {
	return toolOutcome{
		call: c,
		result: protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: text}},
			Details: json.RawMessage(`{}`),
		},
		isError: true,
	}
}

func shouldTerminate(outcomes []toolOutcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, o := range outcomes {
		if o.result.Terminate == nil || !*o.result.Terminate {
			return false
		}
	}
	return true
}

func panicText(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(v)
}

func ptr[T any](v T) *T { return &v }
