package protocol

import "encoding/json"

// ToolDecl declares a tool to the model. Parameters is a JSON Schema document.
// A nil Parameters encodes as the empty object.
type ToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolRef names a tool without its declaration.
type ToolRef struct {
	Name string `json:"name"`
}

// MarshalJSON encodes the declaration and defaults empty parameters to {}.
func (d ToolDecl) MarshalJSON() ([]byte, error) {
	if len(d.Parameters) == 0 {
		d.Parameters = json.RawMessage(`{}`)
	}
	type plain ToolDecl
	return marshalJSON(plain(d))
}

func cloneToolDecls(in []ToolDecl) []ToolDecl {
	if in == nil {
		return nil
	}
	out := make([]ToolDecl, len(in))
	for i, d := range in {
		d.Parameters = cloneRaw(d.Parameters)
		out[i] = d
	}
	return out
}

// Tool kinds of ToolResultDetails.Kind.
const (
	ToolKindRead    = "read"
	ToolKindEdit    = "edit"
	ToolKindExecute = "execute"
	ToolKindSearch  = "search"
	ToolKindOther   = "other"
)

// ToolResultDetails is the typed form of ToolExecutionResult.Details that the
// built-in tools write and a client (ACP, TUI) reads to show a result. Every
// field is optional.
type ToolResultDetails struct {
	// Path is the file or directory the call worked on.
	Path string `json:"path,omitempty"`
	// Diff holds the old and new text of a file change.
	Diff *ToolDiff `json:"diff,omitempty"`
	// Locations are the file positions the call touched.
	Locations []ToolLocation `json:"locations,omitempty"`
	// Truncation is set when the output was cut.
	Truncation *ToolTruncation `json:"truncation,omitempty"`
	// NextOffset is the offset that continues a truncated read.
	NextOffset int `json:"nextOffset,omitempty"`
	// SpillPath names the file that holds the full output.
	SpillPath string `json:"spillPath,omitempty"`
	// SpillCapped is true when the spill file stopped at its size cap.
	SpillCapped bool `json:"spillCapped,omitempty"`
	// ExitCode is the exit code of a finished command. A pointer, because 0 is a value.
	ExitCode *int `json:"exitCode,omitempty"`
	// Signal names the signal that ended a command.
	Signal string `json:"signal,omitempty"`
	// JobID names the background job of a command.
	JobID string `json:"jobId,omitempty"`
	// Status is the job status, for example "running" or "completed".
	Status string `json:"status,omitempty"`
	// Cwd is the directory that a leading "cd X &&" of a bash command names.
	Cwd string `json:"cwd,omitempty"`
	// Kind is one of the ToolKind constants.
	Kind string `json:"kind,omitempty"`
}

// ToolDiff is the old and new text of one file.
type ToolDiff struct {
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// ToolLocation is a file path with an optional 1-based line.
type ToolLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// ToolTruncation describes how a tool output was cut. TruncatedBy is "lines"
// or "bytes", the limit that was hit first.
type ToolTruncation struct {
	TruncatedBy string `json:"truncatedBy"`
	TotalLines  int    `json:"totalLines"`
	TotalBytes  int    `json:"totalBytes"`
	OutputLines int    `json:"outputLines"`
	OutputBytes int    `json:"outputBytes"`
}

const (
	systemReminderOpen  = "<system-reminder>\n"
	systemReminderClose = "\n</system-reminder>"
)

// SystemReminder wraps text as a system reminder. The harness queues such text
// as a user message that a client can hide from the conversation view.
func SystemReminder(text string) string {
	return systemReminderOpen + text + systemReminderClose
}

// IsSystemReminder reports whether m is one text block in the shape that
// SystemReminder produces.
func IsSystemReminder(m UserMessage) bool {
	if len(m.Content) != 1 {
		return false
	}
	t, ok := m.Content[0].(Text)
	return ok && IsSystemReminderText(t.Text)
}
