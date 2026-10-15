package sse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// splitReader returns the input in two Read calls, cut at offset at.
type splitReader struct {
	data string
	at   int
	pos  int
}

func (s *splitReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	end := len(s.data)
	if s.pos < s.at && s.at < end {
		end = s.at
	}
	n := copy(p, s.data[s.pos:end])
	s.pos += n
	return n, nil
}

// readAll returns all events and the final error.
func readAll(r io.Reader, opts ...Option) ([]Event, error) {
	rd := NewReader(r, opts...)
	var out []Event
	for {
		ev, err := rd.Next(context.Background())
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
}

func mustRead(t *testing.T, in string, opts ...Option) []Event {
	t.Helper()
	evs, err := readAll(strings.NewReader(in), opts...)
	require.ErrorIs(t, err, io.EOF)
	return evs
}

func TestLargeDataLines(t *testing.T) {
	for _, size := range []int{200 << 10, 10 << 20} {
		payload := strings.Repeat("x", size)
		evs := mustRead(t, "data: "+payload+"\n\n")
		require.Len(t, evs, 1)
		assert.Equal(t, payload, evs[0].Data)
	}
}

func TestLineLimit(t *testing.T) {
	exact := "data: " + strings.Repeat("a", 14) // 20 bytes
	evs := mustRead(t, exact+"\n\n", WithMaxLineBytes(20))
	require.Len(t, evs, 1)

	rd := NewReader(strings.NewReader(exact+"b\n\n"), WithMaxLineBytes(20))
	_, err := rd.Next(context.Background())
	require.ErrorIs(t, err, ErrLineTooLong)
	_, err2 := rd.Next(context.Background())
	assert.Equal(t, err, err2, "reader stays failed")
}

func TestLineLimitAppliesBeforeLineEnd(t *testing.T) {
	// No line end ever arrives: the limit must stop the reader.
	_, err := readAll(strings.NewReader("data: "+strings.Repeat("a", 100000)), WithMaxLineBytes(1000))
	require.ErrorIs(t, err, ErrLineTooLong)
}

func TestEventLimit(t *testing.T) {
	two := "data: abcd\ndata: abcd\n\n" // 20 field bytes
	evs := mustRead(t, two, WithMaxEventBytes(20))
	require.Len(t, evs, 1)
	assert.Equal(t, "abcd\nabcd", evs[0].Data)

	rd := NewReader(strings.NewReader("data: abcd\ndata: abcd\ndata: a\n\n"), WithMaxEventBytes(20))
	_, err := rd.Next(context.Background())
	require.ErrorIs(t, err, ErrEventTooLong)
	_, err2 := rd.Next(context.Background())
	assert.Equal(t, err, err2)

	// A single line above the event limit but below the line limit.
	_, err = readAll(strings.NewReader("data: "+strings.Repeat("a", 50)+"\n\n"), WithMaxEventBytes(20))
	require.ErrorIs(t, err, ErrEventTooLong)
}

func TestEventLimitResetsPerEvent(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("data: abcd\n\n")
	}
	evs := mustRead(t, sb.String(), WithMaxEventBytes(10))
	assert.Len(t, evs, 50)
}

func TestLineEndings(t *testing.T) {
	want := []Event{
		{Name: "a", Data: "1\n2", Raw: []string{"event: a", "data: 1", "data: 2"}},
		{Data: "3", Raw: []string{"data: 3"}},
	}
	base := "event: a\ndata: 1\ndata: 2\n\ndata: 3\n\n"
	for name, nl := range map[string]string{"lf": "\n", "crlf": "\r\n", "cr": "\r"} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, mustRead(t, strings.ReplaceAll(base, "\n", nl)))
		})
	}
	t.Run("mixed", func(t *testing.T) {
		in := "event: a\r\ndata: 1\rdata: 2\n\r\ndata: 3\r\r"
		assert.Equal(t, want, mustRead(t, in))
	})
}

func TestSplitEverywhere(t *testing.T) {
	inputs := []string{
		"\xEF\xBB\xBFevent: a\r\ndata: héllo \U0001F600\r\n\r\ndata: x\r\r: c\r\ndata\r\n\r\n",
		"data: café\r\n\r\ndata: y",
		"\xEF\xBBdata: x\n\n",
	}
	for _, in := range inputs {
		want, werr := readAll(strings.NewReader(in))
		require.ErrorIs(t, werr, io.EOF)
		for at := 0; at <= len(in); at++ {
			got, err := readAll(&splitReader{data: in, at: at})
			require.ErrorIs(t, err, io.EOF)
			require.Equal(t, want, got, "split at %d of %q", at, in)
		}
		got, err := readAll(iotest.OneByteReader(strings.NewReader(in)))
		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, want, got)
	}
}

func TestSplitCRLFIsOneLineEnd(t *testing.T) {
	// A split CRLF must not create a blank line (a second event).
	in := "data: a\r\ndata: b\r\n\r\n"
	for at := 0; at <= len(in); at++ {
		evs, err := readAll(&splitReader{data: in, at: at})
		require.ErrorIs(t, err, io.EOF)
		require.Len(t, evs, 1, "split at %d", at)
		assert.Equal(t, "a\nb", evs[0].Data)
	}
}

func TestBOM(t *testing.T) {
	evs := mustRead(t, "\xEF\xBB\xBFdata: x\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, "x", evs[0].Data)

	// BOM bytes in the middle of the stream are text.
	evs = mustRead(t, "data: a\n\n\xEF\xBB\xBFdata: b\n\n")
	require.Len(t, evs, 1)

	// A partial BOM prefix is text.
	evs = mustRead(t, "data: a\n\n")
	require.Len(t, evs, 1)
	evs = mustRead(t, "\xEF\xBBdata: a\n\n")
	assert.Empty(t, evs, "field name is not data")
}

func TestComments(t *testing.T) {
	evs := mustRead(t, ": hi\ndata: x\n: mid\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, "x", evs[0].Data)
	assert.Equal(t, []string{"data: x"}, evs[0].Raw)
}

func TestLeadingSpace(t *testing.T) {
	evs := mustRead(t, "data:x\n\ndata: x\n\ndata:  x\n\n")
	require.Len(t, evs, 3)
	assert.Equal(t, "x", evs[0].Data)
	assert.Equal(t, "x", evs[1].Data)
	assert.Equal(t, " x", evs[2].Data)
}

func TestFieldRules(t *testing.T) {
	// Value keeps later colons; id and retry are ignored.
	evs := mustRead(t, "id: 7\nretry: 5\nevent: e:f\ndata: a:b\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, "e:f", evs[0].Name)
	assert.Equal(t, "a:b", evs[0].Data)

	// A line without a colon is a field with an empty value.
	evs = mustRead(t, "data\ndata: x\ndata\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, "\nx\n", evs[0].Data)

	evs = mustRead(t, "event\n\n")
	assert.Empty(t, evs, "empty name and no data")
}

func TestNamedEmptyDataEvent(t *testing.T) {
	evs := mustRead(t, "event: error\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, Event{Name: "error", Raw: []string{"event: error"}}, evs[0])

	evs = mustRead(t, "event: error")
	require.Len(t, evs, 1)
	assert.Equal(t, "error", evs[0].Name)
}

func TestEmptyFramesDispatchNothing(t *testing.T) {
	assert.Empty(t, mustRead(t, "\n\n\n"))
	assert.Empty(t, mustRead(t, ": a\n\n: b\n\nid: 1\n\n"))
	// State resets: comment-only frame does not leak into the next event.
	evs := mustRead(t, ": a\n\ndata: x\n\n")
	require.Len(t, evs, 1)
	assert.Equal(t, []string{"data: x"}, evs[0].Raw)
}

func TestCommentStreamKeepsNoRaw(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50000; i++ {
		sb.WriteString(": keep-alive ping\n")
	}
	sb.WriteString("data: x\n\n")
	// A tiny event limit proves comments are not stored or counted.
	rd := NewReader(strings.NewReader(sb.String()), WithMaxEventBytes(16))
	ev, err := rd.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "x", ev.Data)
	assert.Equal(t, []string{"data: x"}, ev.Raw)
	assert.Empty(t, rd.raw)
}

func TestRawIsBounded(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("data: " + strings.Repeat("é", 1000) + "\n")
	}
	sb.WriteString("\n")
	evs := mustRead(t, sb.String())
	require.Len(t, evs, 1)
	assert.LessOrEqual(t, len(evs[0].Raw), maxRawLines)
	for _, l := range evs[0].Raw {
		assert.LessOrEqual(t, len(l), maxRawLineSize)
		assert.True(t, strings.HasPrefix(l, "data: "))
	}
}

func TestEOFFlush(t *testing.T) {
	for _, in := range []string{"data: x\n", "data: x", "data: x\r", "data: x\n\n"} {
		evs := mustRead(t, in)
		require.Len(t, evs, 1, "%q", in)
		assert.Equal(t, "x", evs[0].Data)
	}
	// Unterminated final line plus an earlier unterminated frame.
	evs := mustRead(t, "event: a\ndata: x")
	require.Len(t, evs, 1)
	assert.Equal(t, Event{Name: "a", Data: "x", Raw: []string{"event: a", "data: x"}}, evs[0])

	rd := NewReader(strings.NewReader("data: x"))
	_, err := rd.Next(context.Background())
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = rd.Next(context.Background())
		require.ErrorIs(t, err, io.EOF)
	}
}

func TestReadErrorIsNotEOF(t *testing.T) {
	boom := errors.New("boom")
	in := io.MultiReader(strings.NewReader("data: a\n\ndata: b"), iotest.ErrReader(boom))
	rd := NewReader(in)
	ev, err := rd.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "a", ev.Data)
	for i := 0; i < 2; i++ {
		_, err = rd.Next(context.Background())
		require.ErrorIs(t, err, boom)
		assert.NotErrorIs(t, err, io.EOF)
	}
}

func TestCancelBlockedReadWithOwnedPipe(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, func() { _ = pr.CloseWithError(ctx.Err()) })
	defer stop()
	defer func() { _ = pw.Close() }()

	go func() { _, _ = pw.Write([]byte("data: a\n\ndata: part")) }()
	rd := NewReader(pr)
	ev, err := rd.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, "a", ev.Data)

	done := make(chan error, 1)
	go func() {
		_, err := rd.Next(ctx)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // Let Next block in Read.
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return after cancel")
	}
}

func TestNoEventAfterCancelEvenIfBuffered(t *testing.T) {
	in := "data: a\n\ndata: b\n\ndata: c\n\n"
	rd := NewReader(strings.NewReader(in))
	ctx, cancel := context.WithCancel(context.Background())
	ev, err := rd.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, "a", ev.Data)
	cancel()
	_, err = rd.Next(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// The reader is still usable with a live context.
	ev, err = rd.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "b", ev.Data)
}

func TestCancelBetweenBlankLineAndDispatchKeepsEvent(t *testing.T) {
	rd := NewReader(strings.NewReader("data: a\n\n"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rd.Next(ctx)
	require.ErrorIs(t, err, context.Canceled)
	ev, err := rd.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "a", ev.Data)
}

// A server keeps the connection open after the terminal event. The consumer
// stops at the terminal event and cancels the request; the server sees it.
func TestStopAfterTerminalEventClosesConnection(t *testing.T) {
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: delta\ndata: 1\n\nevent: done\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-req.Context().Done()
		close(closed)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	rd := NewReader(resp.Body)
	var names []string
	for {
		ev, err := rd.Next(ctx)
		require.NoError(t, err)
		names = append(names, ev.Name)
		if ev.Name == "done" {
			break
		}
	}
	assert.Equal(t, []string{"delta", "done"}, names)
	cancel()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not see the close")
	}
}

func TestOptionsIgnoreInvalid(t *testing.T) {
	rd := NewReader(strings.NewReader(""), WithMaxLineBytes(0), WithMaxEventBytes(-1))
	assert.Equal(t, DefaultMaxBytes, rd.maxLine)
	assert.Equal(t, DefaultMaxBytes, rd.maxEvent)
}
