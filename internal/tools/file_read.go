package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

const (
	// readLineCut is the longest line, in characters, that the model sees.
	readLineCut    = 768
	readLineMarker = " … [line cut]"
	// binarySniffBytes is the part of a file that decides if it is binary.
	binarySniffBytes = 8 * 1024
	// binaryControlShare is the share of control bytes above which a file is
	// treated as binary.
	binaryControlShare = 0.3

	nonVisionNote = "[Current model does not support images. The image will be omitted from this request.]"
)

// NewFileTools returns the file tools read, write, edit and ls for one
// session. state is the session's record of the files it read or wrote; it
// must not be shared with another session. modelInput returns the input kinds
// of the session's current model, for example "text" and "image"; read leaves
// out an image when the list has no "image". A nil modelInput means an image
// is always returned.
func NewFileTools(state *FileState, modelInput func() []string) []Tool {
	if state == nil {
		state = NewFileState()
	}
	return []Tool{
		&Read{fs: state, modelInput: modelInput, images: defaultImageLimits},
		&Write{fs: state},
		newEdit(state),
		&Ls{},
	}
}

// Read returns the content of a file with numbered lines, or an image.
type Read struct {
	fs         *FileState
	modelInput func() []string
	images     imageLimits
}

type readInput struct {
	Path   string   `json:"path"`
	Offset *float64 `json:"offset"`
	Limit  *float64 `json:"limit"`
}

func (*Read) ConcurrencySafe(json.RawMessage) bool { return true }

func (*Read) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "read",
		Description: fmt.Sprintf("Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). "+
			"Images are sent as attachments. For text files, output is truncated to %d lines or %dKB (whichever is hit first). "+
			"Use offset/limit for large files. When you need the full file, continue with offset until complete.",
			DefaultMaxLines, DefaultMaxBytes/1024),
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},` +
			`"offset":{"type":"number","description":"Line number to start reading from (1-indexed)"},` +
			`"limit":{"type":"number","description":"Maximum number of lines to read"}},` +
			`"required":["path"]}`),
	}
}

func (*Read) PromptSnippet() string { return "Read file contents" }

func (*Read) PromptGuidelines() []string {
	return []string{"Use read to examine files instead of cat or sed."}
}

func (r *Read) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in readInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("read: %w", err)
	}
	abs, err := workspace.ResolvePath(tc.Cwd, in.Path)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("read: %w", err)
	}
	abs, existing, err := locateExisting(abs)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	if !existing.IsDir() && !existing.Mode().IsRegular() {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: it is not a regular file.", in.Path)
	}
	f, err := os.Open(abs)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	defer func() { _ = f.Close() }() // read-only file
	info, err := f.Stat()
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	if info.IsDir() {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: it is a directory. Use ls to list it.", in.Path)
	}
	if !info.Mode().IsRegular() {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: it is not a regular file.", in.Path)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	r.fs.Record(abs, info)

	if mime := detectImage(data[:min(len(data), imageSniffBytes)]); mime != "" {
		return r.imageResult(data, mime, abs), nil
	}
	if looksBinary(data[:min(len(data), binarySniffBytes)]) {
		return protocol.ToolExecutionResult{}, failf("%s appears to be a binary file and cannot be read as text.", in.Path)
	}
	return readText(string(data), in, abs), nil
}

// imageResult returns an image as a text note and one image block.
func (r *Read) imageResult(data []byte, mime, abs string) protocol.ToolExecutionResult {
	details := mustDetails(protocol.ToolResultDetails{
		Kind:      protocol.ToolKindRead,
		Path:      abs,
		Locations: []protocol.ToolLocation{{Path: abs}},
	})
	if r.modelInput != nil && !slices.Contains(r.modelInput(), "image") {
		text := fmt.Sprintf("Read image file [%s]\n%s", mime, nonVisionNote)
		return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Details: details}
	}
	img, err := toInline(data, mime, r.images)
	if err != nil {
		text := fmt.Sprintf("Read image file [%s]\n%s", mime, err.Error())
		return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Details: details}
	}
	text := "Read image file [" + img.mime + "]"
	if len(img.hints) > 0 {
		text += "\n" + strings.Join(img.hints, "\n")
	}
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{
			protocol.Text{Text: text},
			protocol.Image{Data: base64.StdEncoding.EncodeToString(img.data), MimeType: img.mime},
		},
		Details: details,
	}
}

// readText numbers the lines of content, applies offset and limit, and cuts
// the output at the default limits.
func readText(content string, in readInput, abs string) protocol.ToolExecutionResult {
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}
	total := len(lines)
	if in.Offset != nil && *in.Offset >= 1 && math.Floor(*in.Offset) > float64(total) {
		return protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: fmt.Sprintf("Offset %.0f is beyond end of file (%d lines total)", math.Floor(*in.Offset), total)}},
			Details: mustDetails(protocol.ToolResultDetails{Kind: protocol.ToolKindRead, Path: abs}),
		}
	}

	start := 0
	if in.Offset != nil && *in.Offset > 1 {
		start = int(*in.Offset) - 1
	}
	details := protocol.ToolResultDetails{
		Kind:      protocol.ToolKindRead,
		Path:      abs,
		Locations: []protocol.ToolLocation{{Path: abs, Line: start + 1}},
	}
	text := func(s string) protocol.ToolExecutionResult {
		return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: s}}, Details: mustDetails(details)}
	}
	if total == 0 && start == 0 {
		return text("")
	}
	if start >= total {
		// A hint, not an error: the model asked for a page past the end.
		return text(fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", start+1, total))
	}

	end, limited := total, false
	if in.Limit != nil && *in.Limit >= 1 {
		end, limited = start+int(min(*in.Limit, float64(total-start))), true
	}
	var numbered strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			numbered.WriteByte('\n')
		}
		numbered.WriteString(strconv.Itoa(i + 1))
		numbered.WriteByte('|')
		numbered.WriteString(strings.TrimSuffix(lines[i], "\r"))
	}
	head := TruncateHead(numbered.String(), 0, 0)

	switch {
	case head.FirstLineExceedsLimit:
		details.Truncation = &head.Truncation
		return text(fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			start+1, formatSize(len(lines[start])), formatSize(DefaultMaxBytes), start+1, displayPath(abs, in.Path), DefaultMaxBytes))
	case head.Truncated:
		last := start + head.Truncation.OutputLines
		limit := ""
		if head.Truncation.TruncatedBy == "bytes" {
			limit = fmt.Sprintf(" (%s limit)", formatSize(DefaultMaxBytes))
		}
		details.Truncation = &head.Truncation
		details.NextOffset = last + 1
		return text(fmt.Sprintf("%s\n\n[Showing lines %d-%d of %d%s. Use offset=%d to continue.]",
			cutLongLines(head.Text), start+1, last, total, limit, last+1))
	case limited && end < total:
		details.NextOffset = end + 1
		return text(fmt.Sprintf("%s\n\n[%d more lines in file. Use offset=%d to continue.]",
			cutLongLines(head.Text), total-end, end+1))
	}
	return text(cutLongLines(head.Text))
}

// cutLongLines shortens each numbered line of text whose content is longer
// than readLineCut characters. It changes the model's view only; the counts of
// the result details stay those of the whole lines.
func cutLongLines(text string) string {
	if len(text) <= readLineCut {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		bar := strings.IndexByte(l, '|')
		if bar < 0 || len(l)-bar-1 <= readLineCut {
			continue
		}
		body := l[bar+1:]
		if utf8.RuneCountInString(body) > readLineCut {
			lines[i] = l[:bar+1] + string([]rune(body)[:readLineCut]) + readLineMarker
		}
	}
	return strings.Join(lines, "\n")
}

// looksBinary reports a NUL byte, or a share of control bytes above
// binaryControlShare, in head. A control byte is below 0x20 and is not tab,
// line feed, carriage return or form feed, or it is 0x7f.
func looksBinary(head []byte) bool {
	if len(head) == 0 {
		return false
	}
	control := 0
	for _, c := range head {
		switch {
		case c == 0:
			return true
		case c < 0x20 && c != '\t' && c != '\n' && c != '\r' && c != '\f', c == 0x7f:
			control++
		}
	}
	return float64(control)/float64(len(head)) > binaryControlShare
}

// locateExisting returns path when it exists. When it does not, it returns the
// first spelling from workspace.PathVariants that exists, for file names that
// a model copies with a plain space or an unescaped character.
func locateExisting(path string) (string, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err == nil {
		return path, info, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return path, nil, err
	}
	for _, v := range workspace.PathVariants(path) {
		if vi, verr := os.Stat(v); verr == nil {
			return v, vi, nil
		}
	}
	return path, nil, err
}

// reason says why a file operation failed, in a few words without the path.
func reason(err error) string {
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "file not found"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.As(err, &pe):
		return pe.Err.Error()
	}
	return err.Error()
}

// displayPath returns the path the model used when there is one, else abs.
func displayPath(abs, given string) string {
	if given != "" {
		return given
	}
	return abs
}

func mustDetails(d protocol.ToolResultDetails) json.RawMessage {
	b, err := json.Marshal(d)
	if err != nil {
		// ToolResultDetails holds only plain values, so this cannot happen.
		panic(fmt.Sprintf("tools: encode result details: %v", err))
	}
	return b
}

// formatSize writes a byte count the way Pi does: 512B, 50.0KB, 1.5MB.
func formatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}
