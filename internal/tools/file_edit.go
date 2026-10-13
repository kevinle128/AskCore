package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

const (
	editDescription = "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes."

	editParameters = `{"type":"object","properties":{` +
		`"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},` +
		`"edits":{"type":"array","description":"One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead.",` +
		`"items":{"type":"object","properties":{` +
		`"oldText":{"type":"string","description":"Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},` +
		`"newText":{"type":"string","description":"Replacement text for this targeted edit."}},` +
		`"required":["oldText","newText"]}}},` +
		`"required":["path","edits"]}`

	// noopLimit is the call count of one identical no-op edit at which the
	// error turns into an instruction to read the file again.
	noopLimit = 3
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Edit replaces text in one file. Each edits[].oldText is matched against the
// original content, so the edits of one call do not see each other. A match
// must be unique; findMatch describes the ladder that finds it.
//
// An Edit belongs to one session. It refuses a file that changed since the
// session read or wrote it, and it counts identical no-op calls.
type Edit struct {
	fs *FileState

	mu    sync.Mutex
	noops map[string]noopCount // by file path
}

type noopCount struct {
	signature string
	n         int
}

type editInput struct {
	Path  string `json:"path"`
	Edits []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	} `json:"edits"`
}

func newEdit(fs *FileState) *Edit {
	return &Edit{fs: fs, noops: map[string]noopCount{}}
}

func (*Edit) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "edit", Description: editDescription, Parameters: json.RawMessage(editParameters)}
}

func (*Edit) PromptSnippet() string {
	return "Make precise file edits with exact text replacement, including multiple disjoint edits in one call"
}

func (*Edit) PromptGuidelines() []string {
	return []string{
		"Use edit for precise changes (edits[].oldText must match exactly)",
		"When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls",
		"Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.",
		"Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions.",
	}
}

// PrepareArguments accepts the older shapes of the edit input: a top-level
// oldText and newText pair, edits as a JSON string, and edits as one object.
// It leaves input that it cannot read for validation to report.
func (*Edit) PrepareArguments(raw json.RawMessage) (json.RawMessage, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return raw, nil
	}
	changed := false

	if edits, ok := args["edits"]; ok {
		var asString string
		if json.Unmarshal(edits, &asString) == nil {
			if arr, ok := parseEditList([]byte(asString)); ok {
				args["edits"], changed = arr, true
			}
		} else if arr, ok := parseEditList(edits); ok && !bytes.Equal(arr, edits) {
			args["edits"], changed = arr, true
		}
	}

	var oldText, newText string
	if json.Unmarshal(args["oldText"], &oldText) == nil && json.Unmarshal(args["newText"], &newText) == nil &&
		args["oldText"] != nil && args["newText"] != nil {
		var list []json.RawMessage
		if json.Unmarshal(args["edits"], &list) != nil {
			list = nil
		}
		one, err := json.Marshal(map[string]string{"oldText": oldText, "newText": newText})
		if err != nil {
			return nil, fmt.Errorf("edit: %w", err)
		}
		list = append(list, one)
		out, err := json.Marshal(list)
		if err != nil {
			return nil, fmt.Errorf("edit: %w", err)
		}
		args["edits"] = out
		delete(args, "oldText")
		delete(args, "newText")
		changed = true
	}

	if !changed {
		return raw, nil
	}
	out, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("edit: %w", err)
	}
	return out, nil
}

// parseEditList reads data as a JSON array of edits or as one edit object, and
// returns it as an array.
func parseEditList(data []byte) (json.RawMessage, bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, false
	}
	switch data[0] {
	case '[':
		var list []json.RawMessage
		if json.Unmarshal(data, &list) != nil {
			return nil, false
		}
		return data, true
	case '{':
		var one struct {
			OldText *string `json:"oldText"`
			NewText *string `json:"newText"`
		}
		if json.Unmarshal(data, &one) != nil || one.OldText == nil || one.NewText == nil {
			return nil, false
		}
		return append(append([]byte{'['}, data...), ']'), true
	}
	return nil, false
}

func (e *Edit) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in editInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("edit: %w", err)
	}
	if len(in.Edits) == 0 {
		return protocol.ToolExecutionResult{}, failf("Edit tool input is invalid. edits must contain at least one replacement.")
	}
	abs, err := workspace.ResolvePath(tc.Cwd, in.Path)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("edit: %w", err)
	}
	abs, info, err := locateExisting(abs)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Could not edit file: %s. %s.", in.Path, reason(err))
	}
	if info.IsDir() {
		return protocol.ToolExecutionResult{}, failf("Could not edit file: %s. It is a directory.", in.Path)
	}
	if !info.Mode().IsRegular() {
		return protocol.ToolExecutionResult{}, failf("Could not edit file: %s. It is not a regular file.", in.Path)
	}

	unlock := PathLock(abs)
	defer unlock()

	if err := e.fs.Check(abs); err != nil {
		if errors.Is(err, ErrFileChanged) {
			return protocol.ToolExecutionResult{}, failf("%s changed on disk since you last read it. Read it again before you edit.", in.Path)
		}
		return protocol.ToolExecutionResult{}, fmt.Errorf("edit: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Could not edit file: %s. %s.", in.Path, reason(err))
	}

	// The model never sends the invisible BOM, and CRLF text is matched as LF
	// text; both are restored when the file is written.
	bom := bytes.HasPrefix(data, utf8BOM)
	text := string(bytes.TrimPrefix(data, utf8BOM))
	ending := detectLineEnding(text)
	base := normalizeToLF(text)

	edits := make([]editPair, len(in.Edits))
	for i, ed := range in.Edits {
		edits[i] = editPair{oldText: normalizeToLF(ed.OldText), newText: normalizeToLF(ed.NewText)}
	}
	updated, lines, err := applyEdits(base, edits, in.Path)
	if err != nil {
		var nc *noChangeError
		if errors.As(err, &nc) {
			return protocol.ToolExecutionResult{}, e.countNoop(abs, in.Path, in.Edits, nc)
		}
		return protocol.ToolExecutionResult{}, err
	}

	out := restoreLineEndings(updated, ending)
	if bom {
		out = string(utf8BOM) + out
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	if err := WriteFileAtomic(abs, []byte(out), 0o644); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	if st, err := os.Stat(abs); err == nil {
		e.fs.Record(abs, st)
	}
	e.clearNoop(abs)

	loc := make([]protocol.ToolLocation, len(lines))
	for i, l := range lines {
		loc[i] = protocol.ToolLocation{Path: abs, Line: l}
	}
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), in.Path)}},
		Details: mustDetails(protocol.ToolResultDetails{
			Kind:      protocol.ToolKindEdit,
			Path:      abs,
			Diff:      &protocol.ToolDiff{Path: abs, OldText: base, NewText: updated},
			Locations: loc,
		}),
	}, nil
}

// countNoop counts a no-op call. The first calls give the usual error. When the
// same no-op arrives for the third time, the model is not looking at the file
// any more, so the error tells it to read the file again.
func (e *Edit) countNoop(abs, display string, edits any, nc *noChangeError) error {
	sig, _ := json.Marshal(edits)
	e.mu.Lock()
	c := e.noops[abs]
	if c.signature == string(sig) {
		c.n++
	} else {
		c = noopCount{signature: string(sig), n: 1}
	}
	e.noops[abs] = c
	e.mu.Unlock()
	if c.n >= noopLimit {
		return failf("No changes made to %s for the %s time with the same edit. The file does not contain what you expect. Read %s again before you edit it.",
			display, ordinal(c.n), display)
	}
	return nc
}

func (e *Edit) clearNoop(abs string) {
	e.mu.Lock()
	delete(e.noops, abs)
	e.mu.Unlock()
}

func ordinal(n int) string {
	switch n {
	case 3:
		return "third"
	case 4:
		return "fourth"
	}
	return fmt.Sprintf("%dth", n)
}

type editPair struct{ oldText, newText string }

// failf returns an error whose text goes to the model as written. The texts
// follow the sentence style of the model-facing messages of the file tools,
// not the lower-case style of Go errors.
func failf(format string, args ...any) error {
	return &modelError{msg: fmt.Sprintf(format, args...)}
}

type modelError struct{ msg string }

func (e *modelError) Error() string { return e.msg }

// noChangeError reports an edit that leaves the file as it is.
type noChangeError struct{ msg string }

func (e *noChangeError) Error() string { return e.msg }

type matchedEdit struct {
	index      int
	start, end int
	newText    string
}

// applyEdits matches every edit against content, which uses "\n" line ends,
// and applies them together. It returns the new content and, for each edit in
// file order, the first line of its replacement in the new content.
func applyEdits(content string, edits []editPair, path string) (string, []int, error) {
	total := len(edits)
	matched := make([]matchedEdit, 0, total)
	for i, ed := range edits {
		if ed.oldText == "" {
			if total == 1 {
				return "", nil, failf("oldText must not be empty in %s.", path)
			}
			return "", nil, failf("edits[%d].oldText must not be empty in %s.", i, path)
		}
		if ed.oldText == ed.newText {
			return "", nil, &noChangeError{msg: fmt.Sprintf("No changes made to %s. edits[%d].oldText and newText are identical.", path, i)}
		}
		start, end, rung, err := findMatch(content, ed.oldText)
		if err != nil {
			var me *matchError
			if errors.As(err, &me) {
				return "", nil, matchFailure(me, path, i, total)
			}
			return "", nil, err
		}
		newText := ed.newText
		if rung >= rungTrimmed {
			newText = reindent(newText, ed.oldText, content[start:end])
		}
		matched = append(matched, matchedEdit{index: i, start: start, end: end, newText: newText})
	}

	slices.SortFunc(matched, func(a, b matchedEdit) int { return a.start - b.start })
	for i := 1; i < len(matched); i++ {
		if prev, cur := matched[i-1], matched[i]; prev.end > cur.start {
			return "", nil, failf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				prev.index, cur.index, path)
		}
	}

	var b strings.Builder
	lines := make([]int, 0, len(matched))
	line, cursor := 1, 0
	for _, m := range matched {
		b.WriteString(content[cursor:m.start])
		line += strings.Count(content[cursor:m.start], "\n")
		lines = append(lines, line)
		b.WriteString(m.newText)
		line += strings.Count(m.newText, "\n")
		cursor = m.end
	}
	b.WriteString(content[cursor:])
	updated := b.String()
	if updated == content {
		return "", nil, &noChangeError{msg: fmt.Sprintf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)}
	}
	return updated, lines, nil
}

// matchFailure builds the error text for an edit without a unique match.
func matchFailure(me *matchError, path string, index, total int) error {
	var b strings.Builder
	switch {
	case me.kind == matchMultiple && total == 1:
		fmt.Fprintf(&b, "Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", me.count, path)
	case me.kind == matchMultiple:
		fmt.Fprintf(&b, "Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", me.count, index, path)
	case total == 1:
		fmt.Fprintf(&b, "Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	default:
		fmt.Fprintf(&b, "Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", index, path)
	}
	for _, p := range me.previews {
		fmt.Fprintf(&b, "\n  line %d: %s", p.line, p.text)
	}
	if more := me.count - len(me.previews); more > 0 {
		fmt.Fprintf(&b, "\n  ... and %d more", more)
	}
	if c := me.closest; c != nil {
		fmt.Fprintf(&b, "\nClosest match (%d%% similar) at line %d:\n  expected: %s\n  actual:   %s", c.percent, c.line, c.expected, c.actual)
	}
	return failf("%s", b.String())
}

// detectLineEnding returns "\r\n" when the first line end of text is CRLF.
func detectLineEnding(text string) string {
	lf := strings.IndexByte(text, '\n')
	if lf > 0 && text[lf-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// normalizeToLF turns CRLF and lone CR into LF.
func normalizeToLF(text string) string {
	if !strings.Contains(text, "\r") {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}
