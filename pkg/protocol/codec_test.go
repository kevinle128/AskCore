package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLWriterOneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONLWriter(&buf)
	evs := allEvents()
	for _, ev := range evs {
		require.NoError(t, w.Write(ev))
	}
	out := buf.String()
	assert.True(t, strings.HasSuffix(out, "\n"))
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, len(evs))

	r := NewJSONLReader(&buf)
	for i := range evs {
		line, err := r.Next()
		require.NoError(t, err)
		back, err := DecodeEvent(line)
		require.NoError(t, err)
		assert.Equal(t, evs[i], back)
	}
	_, err := r.Next()
	assert.ErrorIs(t, err, io.EOF)
}

func TestJSONLWriterKeepsEmbeddedSeparatorsInsideOneLine(t *testing.T) {
	var buf bytes.Buffer
	ev := &MessageEnd{Envelope: env(1), Message: UserMessage{Content: []UserBlock{Text{Text: "a\nb\r\nc d e"}}}}
	require.NoError(t, NewJSONLWriter(&buf).Write(ev))
	assert.Equal(t, 1, strings.Count(buf.String(), "\n"))
	assert.NotContains(t, buf.String(), "\r")

	line, err := NewJSONLReader(&buf).Next()
	require.NoError(t, err)
	back, err := DecodeEvent(line)
	require.NoError(t, err)
	assert.Equal(t, ev, back)
}

func TestJSONLWriterFailedEncodeWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONLWriter(&buf)
	require.Error(t, w.Write(&MessageEnd{}))
	assert.Zero(t, buf.Len())
}

type failWriter struct{ n int }

func (f failWriter) Write(p []byte) (int, error) {
	if f.n >= 0 {
		return f.n, nil
	}
	return 0, errors.New("disk full")
}

func TestJSONLWriterWriteErrors(t *testing.T) {
	require.ErrorContains(t, NewJSONLWriter(failWriter{n: -1}).Write(&AgentStart{}), "disk full")
	require.ErrorIs(t, NewJSONLWriter(failWriter{n: 2}).Write(&AgentStart{}), io.ErrShortWrite)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func TestJSONLWriterConcurrentWritesDoNotInterleave(t *testing.T) {
	var lb lockedBuffer
	w := NewJSONLWriter(&lb)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, w.Write(&MessageEnd{Envelope: env(uint64(i)), Message: UserMessage{Content: []UserBlock{Text{Text: strings.Repeat("x", 4096)}}}}))
		}()
	}
	wg.Wait()
	r := NewJSONLReader(&lb.buf)
	n := 0
	for {
		line, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		_, err = DecodeEvent(line)
		require.NoError(t, err)
		n++
	}
	assert.Equal(t, 32, n)
}

func readAll(t *testing.T, r io.Reader) []string {
	t.Helper()
	jr := NewJSONLReader(r)
	var out []string
	for {
		line, err := jr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, string(line))
	}
}

func TestJSONLReaderFraming(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"lf", "a\nb\n", []string{"a", "b"}},
		{"crlf strips one cr", "a\r\nb\r\n", []string{"a", "b"}},
		{"only one cr stripped", "a\r\r\n", []string{"a\r"}},
		{"final line without lf", "a\nb", []string{"a", "b"}},
		{"final line with cr only", "a\nb\r", []string{"a", "b"}},
		{"bare cr is not a separator", "a\rb\n", []string{"a\rb"}},
		{"u2028 and u2029 are not separators", "a b c\n", []string{"a b c"}},
		{"empty lines are returned", "a\n\nb\n", []string{"a", "", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, readAll(t, strings.NewReader(tc.in)))
			assert.Equal(t, tc.want, readAll(t, iotest.OneByteReader(strings.NewReader(tc.in))), "one byte per read")
		})
	}
}

func TestJSONLReaderHasNoLineLimit(t *testing.T) {
	for _, size := range []int{70 << 10, 1 << 20, 10 << 20} {
		line := strings.Repeat("x", size)
		got := readAll(t, strings.NewReader(line+"\n"+line))
		require.Len(t, got, 2)
		assert.Len(t, got[0], size)
		assert.Len(t, got[1], size)
	}
}

func TestJSONLReaderFinalLineWithoutLFDecodes(t *testing.T) {
	line, err := NewJSONLReader(strings.NewReader(`{"seq":1,"type":"agent_start"}`)).Next()
	require.NoError(t, err)
	ev, err := DecodeEvent(line)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), ev.Env().Seq)
}

func TestJSONLMalformedCompleteLineIsDecodeError(t *testing.T) {
	r := NewJSONLReader(strings.NewReader("{\"type\":\"agent_start\"\n{\"type\":\"turn_start\"}\n"))
	line, err := r.Next()
	require.NoError(t, err)
	_, err = DecodeEvent(line)
	require.Error(t, err)
	line, err = r.Next()
	require.NoError(t, err)
	_, err = DecodeEvent(line)
	require.NoError(t, err, "the reader recovers on the next line")
}

func TestJSONLReaderReadErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	r := NewJSONLReader(io.MultiReader(strings.NewReader("a\npartial"), iotest.ErrReader(boom)))
	line, err := r.Next()
	require.NoError(t, err)
	assert.Equal(t, "a", string(line))
	_, err = r.Next()
	assert.ErrorIs(t, err, boom)
}

func TestJSONLReaderReturnsOwnedSlices(t *testing.T) {
	r := NewJSONLReader(strings.NewReader("abc\ndef\n"))
	first, err := r.Next()
	require.NoError(t, err)
	_, err = r.Next()
	require.NoError(t, err)
	assert.Equal(t, "abc", string(first))
}
