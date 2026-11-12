package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// writeLines writes a text file of at least size bytes. Every seventh line
// has a CR, and every 500th line is long.
func writeLines(t *testing.T, path string, size int) []byte {
	t.Helper()
	var b bytes.Buffer
	for i := 1; b.Len() < size; i++ {
		switch {
		case i%500 == 0:
			fmt.Fprintf(&b, "long %d %s\n", i, strings.Repeat("é", 1000))
		case i%7 == 0:
			fmt.Fprintf(&b, "row %d with carriage return\r\n", i)
		default:
			fmt.Fprintf(&b, "row %d\n", i)
		}
	}
	require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
	return b.Bytes()
}

func execRead(t *testing.T, ctx context.Context, cwd string, in map[string]any) (protocol.ToolExecutionResult, error) {
	t.Helper()
	args, err := json.Marshal(in)
	require.NoError(t, err)
	r := &Read{fs: NewFileState(), images: defaultImageLimits}
	return r.Execute(ctx, Context{CallID: "c1", Cwd: cwd}, args)
}

func allocDuring(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestReadLargeFileMatchesWholeFileText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	data := writeLines(t, path, 5<<20)
	require.Greater(t, len(data), readWholeMax)
	total := bytes.Count(data, []byte("\n"))

	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name          string
		offset, limit *float64
	}{
		{"start", nil, nil},
		{"offset one", f(1), nil},
		{"middle", f(float64(total / 2)), nil},
		{"middle with limit", f(float64(total / 2)), f(30)},
		{"end", f(float64(total - 3)), nil},
		{"last line", f(float64(total)), f(5)},
		{"limit only", nil, f(10)},
		{"limit over page", nil, f(5000)},
		{"past end", f(float64(total + 50)), nil},
		{"just past end", f(float64(total + 1)), nil},
		{"fraction", f(10.7), f(2.5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := map[string]any{"path": path}
			ri := readInput{Path: path, Offset: tc.offset, Limit: tc.limit}
			if tc.offset != nil {
				in["offset"] = *tc.offset
			}
			if tc.limit != nil {
				in["limit"] = *tc.limit
			}
			got, err := execRead(t, context.Background(), dir, in)
			require.NoError(t, err)
			want := readText(string(data), ri, path)
			assert.Equal(t, want.Content, got.Content)
			assert.JSONEq(t, string(want.Details), string(got.Details))
		})
	}
}

func TestReadLargeFileAllocatesOnlyThePage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	writeLines(t, path, 32<<20)

	var res protocol.ToolExecutionResult
	var err error
	grown := allocDuring(func() {
		res, err = execRead(t, context.Background(), dir, map[string]any{"path": path, "offset": 200000})
	})
	require.NoError(t, err)
	assert.Contains(t, resultTextOf(res), "Showing lines 200000-")
	assert.Less(t, grown, uint64(4<<20))
}

func resultTextOf(res protocol.ToolExecutionResult) string {
	var parts []string
	for _, b := range res.Content {
		if tb, ok := b.(protocol.Text); ok {
			parts = append(parts, tb.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestReadLargeSingleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "line.txt")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("a"), 5<<20), 0o644))

	res, err := execRead(t, context.Background(), dir, map[string]any{"path": "line.txt"})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("[Line 1 is %s, exceeds %s limit. Use bash: sed -n '1p' line.txt | head -c %d]",
		formatSize(5<<20), formatSize(DefaultMaxBytes), DefaultMaxBytes), resultTextOf(res))
}

func TestReadLargeBinaryIsRefusedBeforeDecode(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("text\n"), 1<<20)
	data[100] = 0
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.dat"), data, 0o644))

	_, err := execRead(t, context.Background(), dir, map[string]any{"path": "blob.dat"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appears to be a binary file")
}

func TestReadCancelledContextStopsTheScan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	writeLines(t, path, 5<<20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := execRead(t, ctx, dir, map[string]any{"path": path})
	require.ErrorIs(t, err, context.Canceled)

	// The scan itself checks the context between buffers.
	ctx, cancel = context.WithCancel(context.Background())
	src := &cancelAfterFirstRead{cancel: cancel, r: bytes.NewReader(bytes.Repeat([]byte("x\n"), 1<<20))}
	_, err = scanPage(ctx, src, readInput{Path: "x"}, "x")
	require.ErrorIs(t, err, context.Canceled)
}

type cancelAfterFirstRead struct {
	cancel context.CancelFunc
	r      *bytes.Reader
}

func (c *cancelAfterFirstRead) Read(p []byte) (int, error) {
	c.cancel()
	return c.r.Read(p)
}

// pngPrefix is a valid PNG signature and header followed by zeros: enough to
// be detected as an image, never decoded.
func pngPrefix(width, height int) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	b.Write([]byte{0, 0, 0, 13})
	b.WriteString("IHDR")
	for _, v := range []int{width, height} {
		b.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	b.Write([]byte{8, 0, 0, 0, 0})
	return b.Bytes()
}

func TestReadRefusesImageOver20MiB(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, readImageMax+1)
	copy(data, pngPrefix(10, 10))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.png"), data, 0o644))

	var res protocol.ToolExecutionResult
	var err error
	grown := allocDuring(func() {
		res, err = execRead(t, context.Background(), dir, map[string]any{"path": "huge.png"})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cannot read huge.png: the image is 20.0MB, over the 20.0MB image limit.")
	assert.Empty(t, res.Content)
	assert.Less(t, grown, uint64(4<<20))
}

func TestReadImageUpTo20MiBReachesPixelCheck(t *testing.T) {
	dir := t.TempDir()
	big := image.NewGray(image.Rect(0, 0, 4097, 4097))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.png"), pngBytes(t, big), 0o644))
	small := image.NewGray(image.Rect(0, 0, 8, 8))
	small.SetGray(1, 1, color.Gray{Y: 9})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.png"), pngBytes(t, small), 0o644))

	res, err := execRead(t, context.Background(), dir, map[string]any{"path": "wide.png"})
	require.NoError(t, err)
	assert.Contains(t, resultTextOf(res), imageTooManyPixels)
	assert.Zero(t, countImages(res))

	res, err = execRead(t, context.Background(), dir, map[string]any{"path": "small.png"})
	require.NoError(t, err)
	assert.Equal(t, 1, countImages(res))
}

func countImages(res protocol.ToolExecutionResult) int {
	n := 0
	for _, b := range res.Content {
		if _, ok := b.(protocol.Image); ok {
			n++
		}
	}
	return n
}
