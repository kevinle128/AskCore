package tools

import (
	"bufio"
	"bytes"
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

	// readWholeMax is the largest text file that read loads whole. A larger
	// file is scanned through a small buffer, so one read cannot grow the
	// memory of a shared process with the file size.
	readWholeMax = 4 << 20
	// readImageMax is the largest image file that read loads. A larger image
	// is refused before it is read.
	readImageMax = 20 << 20
	// readScanBuffer is the size of the buffer that the page scan reuses.
	readScanBuffer = 64 << 10

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
	head := make([]byte, binarySniffBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	head = head[:n]
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	r.fs.Record(abs, info)

	if mime := detectImage(head[:min(len(head), imageSniffBytes)]); mime != "" {
		if info.Size() > readImageMax {
			return protocol.ToolExecutionResult{}, failf("Cannot read %s: the image is %s, over the %s image limit.",
				in.Path, formatSize(int(info.Size())), formatSize(readImageMax))
		}
		data, err := readRest(f, head, readImageMax)
		if err != nil {
			return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
		}
		if len(data) > readImageMax {
			return protocol.ToolExecutionResult{}, failf("Cannot read %s: the image is over the %s image limit.",
				in.Path, formatSize(readImageMax))
		}
		if err := ctx.Err(); err != nil {
			return protocol.ToolExecutionResult{}, err
		}
		return r.imageResult(data, mime, abs), nil
	}
	if looksBinary(head) {
		return protocol.ToolExecutionResult{}, failf("%s appears to be a binary file and cannot be read as text.", in.Path)
	}
	src := io.MultiReader(bytes.NewReader(head), f)
	if info.Size() <= readWholeMax {
		data, err := readRest(f, head, readWholeMax)
		if err != nil {
			return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
		}
		if err := ctx.Err(); err != nil {
			return protocol.ToolExecutionResult{}, err
		}
		if len(data) <= readWholeMax {
			return readText(string(data), in, abs), nil
		}
		// The file grew after the stat. Scan what was read, then the rest.
		src = io.MultiReader(bytes.NewReader(data), f)
	}
	res, err := scanPage(ctx, src, in, abs)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.ToolExecutionResult{}, ctx.Err()
		}
		return protocol.ToolExecutionResult{}, failf("Cannot read %s: %s.", in.Path, reason(err))
	}
	return res, nil
}

// readRest returns head followed by the rest of f. It reads at most limit+1
// bytes in all, so a file that grows after the stat cannot grow the buffer
// without bound; a result longer than limit tells the caller that it grew.
func readRest(f io.Reader, head []byte, limit int) ([]byte, error) {
	rest, err := io.ReadAll(io.LimitReader(f, int64(limit)-int64(len(head))+1))
	if err != nil {
		return nil, err
	}
	return append(head, rest...), nil
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
	// A string reader cannot fail and the context is never cancelled, so the
	// error is always nil.
	res, _ := scanPage(context.Background(), strings.NewReader(content), in, abs)
	return res
}

// pageScan is what one pass over a file keeps: the counts of the whole file
// and of the requested page, and the lines of the page that the output can
// show. It never holds the whole file.
type pageScan struct {
	// total is the number of lines of the file. A final empty segment after
	// the last newline is not a line.
	total int
	// lines are the page lines without a trailing CR, from the first page
	// line. They stop after the line that crosses a default output limit, so
	// they are the whole page or enough of it to decide the cut.
	lines []string
	// pageLines and pageBytes are the size of the whole numbered page, as
	// TruncateHead would count it, also for lines that were not kept.
	pageLines, pageBytes int
	// firstLen is the byte length of the first page line, with its CR.
	firstLen int
}

// scanPage reads r once and formats the page that in asks for.
func scanPage(ctx context.Context, r io.Reader, in readInput, abs string) (protocol.ToolExecutionResult, error) {
	start := 0
	if in.Offset != nil && *in.Offset > 1 {
		start = int(min(*in.Offset, 1<<40)) - 1
	}
	pageEnd := math.MaxInt
	if in.Limit != nil && *in.Limit >= 1 {
		pageEnd = start + int(min(*in.Limit, 1<<40))
	}
	ps, err := collectPage(ctx, r, start, pageEnd)
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	return formatPage(ps, in, abs, start), nil
}

// collectPage counts the lines of r and keeps those from start up to pageEnd
// (exclusive, 0-based) that the output can show.
func collectPage(ctx context.Context, r io.Reader, start, pageEnd int) (pageScan, error) {
	var (
		ps      pageScan
		br      = bufio.NewReaderSize(r, readScanBuffer)
		idx     int    // index of the line being read
		rawLen  int    // bytes of the line so far, without the newline
		inLine  bool   // some byte of the line was seen
		cur     []byte // kept bytes of the line, capped
		keptNum int    // numbered bytes of the kept lines, with separators
		full    bool   // the kept lines already cross a default limit
		lastCR  bool
	)
	inPage := func() bool { return idx >= start && idx < pageEnd }
	finish := func() {
		if inPage() {
			trimmed := rawLen
			if lastCR {
				trimmed--
			}
			numbered := digits(idx+1) + 1 + trimmed
			if ps.pageLines > 0 {
				ps.pageBytes++
			}
			ps.pageBytes += numbered
			ps.pageLines++
			if ps.pageLines == 1 {
				ps.firstLen = rawLen
			}
			if !full {
				line := strings.TrimSuffix(string(cur), "\r")
				if keptNum > 0 {
					keptNum++
				}
				keptNum += digits(idx+1) + 1 + len(line)
				ps.lines = append(ps.lines, line)
				if len(ps.lines) > DefaultMaxLines || keptNum > DefaultMaxBytes {
					full = true
				}
			}
		}
		ps.total++
		idx++
		rawLen, inLine, cur, lastCR = 0, false, cur[:0], false
	}
	for {
		if err := ctx.Err(); err != nil {
			return pageScan{}, err
		}
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			body := chunk
			nl := chunk[len(chunk)-1] == '\n'
			if nl {
				body = chunk[:len(chunk)-1]
			}
			inLine = true
			if inPage() && !full && len(cur) <= DefaultMaxBytes {
				cur = append(cur, body[:min(len(body), DefaultMaxBytes+1-len(cur))]...)
			}
			rawLen += len(body)
			if len(body) > 0 {
				lastCR = body[len(body)-1] == '\r'
			}
			if nl {
				finish()
			}
		}
		switch {
		case err == nil, errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			if inLine {
				finish()
			}
			return ps, nil
		default:
			return pageScan{}, err
		}
	}
}

// digits is the number of decimal digits of n, which is positive. It counts
// without the allocation that formatting the number would make for each line.
func digits(n int) int {
	d := 1
	for ; n >= 10; n /= 10 {
		d++
	}
	return d
}

// formatPage writes the text of a page that collectPage kept. start is the
// 0-based first line that the offset asked for.
func formatPage(ps pageScan, in readInput, abs string, start int) protocol.ToolExecutionResult {
	total := ps.total
	if in.Offset != nil && *in.Offset >= 1 && math.Floor(*in.Offset) > float64(total) {
		return protocol.ToolExecutionResult{
			Content: []protocol.UserBlock{protocol.Text{Text: fmt.Sprintf("Offset %.0f is beyond end of file (%d lines total)", math.Floor(*in.Offset), total)}},
			Details: mustDetails(protocol.ToolResultDetails{Kind: protocol.ToolKindRead, Path: abs}),
		}
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
	for i, line := range ps.lines {
		if i > 0 {
			numbered.WriteByte('\n')
		}
		numbered.WriteString(strconv.Itoa(start + i + 1))
		numbered.WriteByte('|')
		numbered.WriteString(line)
	}
	head := TruncateHead(numbered.String(), 0, 0)
	if head.Truncated {
		// The kept lines may be only the start of the page; the counts are
		// those of the whole page.
		head.Truncation.TotalLines, head.Truncation.TotalBytes = ps.pageLines, ps.pageBytes
	}

	switch {
	case head.FirstLineExceedsLimit:
		details.Truncation = &head.Truncation
		return text(fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			start+1, formatSize(ps.firstLen), formatSize(DefaultMaxBytes), start+1, displayPath(abs, in.Path), DefaultMaxBytes))
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
