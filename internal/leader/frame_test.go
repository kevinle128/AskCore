package leader

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func acpFrame(payload string) protocol.LeaderFrame {
	return protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: json.RawMessage(payload)}
}

func encode(t *testing.T, f protocol.LeaderFrame) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, NewFrameWriter(&buf, protocol.LeaderMaxFrame).Write(f))
	return buf.Bytes()
}

func TestFrameRoundTrip(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":9007199254740993,"method":"session/new","params":{"a":"é<>&"}}`
	data := encode(t, acpFrame(payload))
	require.Equal(t, uint32(len(data)-4), binary.BigEndian.Uint32(data))

	got, err := NewFrameReader(bytes.NewReader(data), protocol.LeaderMaxFrame).Next()
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderFrameACP, got.Type)
	require.Equal(t, payload, string(got.Payload))
}

func TestFrameRejectsOversize(t *testing.T) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 17)
	_, err := NewFrameReader(bytes.NewReader(hdr[:]), 16).Next()
	require.ErrorIs(t, err, ErrFrameTooLarge)

	binary.BigEndian.PutUint32(hdr[:], 0)
	_, err = NewFrameReader(bytes.NewReader(hdr[:]), 16).Next()
	require.ErrorIs(t, err, ErrFrameInvalid)

	err = NewFrameWriter(io.Discard, 16).Write(acpFrame(`{"x":"0123456789"}`))
	require.ErrorIs(t, err, ErrFrameTooLarge)
}

func TestFrameBoundaryAtMaxFrame(t *testing.T) {
	const overhead = len(`{"type":"acp","payload":}`)
	frameOfSize := func(total int) protocol.LeaderFrame {
		return acpFrame(`{"a":"` + strings.Repeat("a", total-overhead-len(`{"a":""}`)) + `"}`)
	}
	max := protocol.LeaderMaxFrame
	var buf bytes.Buffer
	require.NoError(t, NewFrameWriter(&buf, max).Write(frameOfSize(max)))
	require.Equal(t, 4+max, buf.Len())
	got, err := NewFrameReader(&buf, max).Next()
	require.NoError(t, err)
	require.Len(t, got.Payload, max-overhead)

	buf.Reset()
	require.ErrorIs(t, NewFrameWriter(&buf, max).Write(frameOfSize(max+1)), ErrFrameTooLarge)
	require.Zero(t, buf.Len(), "no byte of an oversize frame may be written")

	require.NoError(t, NewFrameWriter(&buf, max).Write(frameOfSize(max-1)))
}

func TestFrameWriterKeepsHTMLCharactersLiteral(t *testing.T) {
	payload := `"` + strings.Repeat("<", 20) + `"`
	var buf bytes.Buffer
	require.NoError(t, NewFrameWriter(&buf, 1000).Write(acpFrame(payload)))
	require.Contains(t, buf.String(), payload)
}

type shortWriter struct{ n int }

func (w *shortWriter) Write([]byte) (int, error) { return w.n, nil }

func TestFrameWriterShortWrite(t *testing.T) {
	err := NewFrameWriter(&shortWriter{n: 3}, 1000).Write(acpFrame(`{}`))
	require.ErrorIs(t, err, io.ErrShortWrite)
}

func TestFrameRejectsBatchAndMalformed(t *testing.T) {
	cases := map[string]string{
		"batch":        `{"type":"acp","payload":[{"id":1}]}`,
		"no payload":   `{"type":"acp"}`,
		"scalar acp":   `{"type":"acp","payload":1}`,
		"bad json":     `{"type":`,
		"unknown type": `{"type":"nope"}`,
		"not object":   `[]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewFrameReader(bytes.NewReader(encodeSeed(body)), 1<<20).Next()
			require.ErrorIs(t, err, ErrFrameInvalid)
		})
	}
	for _, typ := range []string{protocol.LeaderFramePing, protocol.LeaderFramePong, protocol.LeaderFrameDisconnect} {
		data := encode(t, protocol.LeaderFrame{Type: typ})
		got, err := NewFrameReader(bytes.NewReader(data), 1<<20).Next()
		require.NoError(t, err)
		require.Equal(t, typ, got.Type)
	}
}

func TestFramePartialReadNoResync(t *testing.T) {
	data := encode(t, acpFrame(`{"id":1}`))
	pr, pw := io.Pipe()
	go func() {
		for i := range data {
			_, _ = pw.Write(data[i : i+1])
		}
		_, _ = pw.Write(data[:len(data)/2]) // half of a second frame
		_ = pw.CloseWithError(errors.New("link lost"))
	}()
	r := NewFrameReader(pr, protocol.LeaderMaxFrame)
	got, err := r.Next()
	require.NoError(t, err)
	require.JSONEq(t, `{"id":1}`, string(got.Payload))

	_, err = r.Next()
	require.Error(t, err)
	_, err2 := r.Next()
	require.Equal(t, err, err2, "an error ends the reader; parsing never restarts inside a frame")
}

func TestFrameHostileLengthDoesNotPreallocate(t *testing.T) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(protocol.LeaderMaxFrame))
	_, err := NewFrameReader(bytes.NewReader(hdr[:]), protocol.LeaderMaxFrame).Next()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func FuzzFrameReader(f *testing.F) {
	f.Add(encodeSeed(`{"type":"acp","payload":{"id":1}}`))
	f.Add(encodeSeed(`{"type":"ping"}`))
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewFrameReader(bytes.NewReader(data), 1<<16)
		for range 4 {
			if _, err := r.Next(); err != nil {
				return
			}
		}
	})
}

func encodeSeed(body string) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
}

func TestLineReaderBoundary(t *testing.T) {
	const limit = 64
	line := strings.Repeat("a", limit)
	r := NewLineReader(strings.NewReader(line+"\n"+line+"b\n"), limit)
	got, err := r.Next()
	require.NoError(t, err)
	require.Equal(t, line, string(got), "a line of exactly the limit, with its newline, is accepted")
	_, err = r.Next()
	require.ErrorIs(t, err, ErrLineTooLong)
	_, err = r.Next()
	require.ErrorIs(t, err, ErrLineTooLong, "an error ends the reader")
}

func TestLineReaderSkipsBlankAndReadsFinalUnterminatedLine(t *testing.T) {
	r := NewLineReader(strings.NewReader("\n{\"a\":1}\n\n{\"b\":2}"), 64)
	got, err := r.Next()
	require.NoError(t, err)
	require.Equal(t, `{"a":1}`, string(got))
	got, err = r.Next()
	require.NoError(t, err)
	require.Equal(t, `{"b":2}`, string(got))
	_, err = r.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestLineWriterCompactsMultilineJSON(t *testing.T) {
	var buf bytes.Buffer
	w := NewLineWriter(&buf, 1024)
	require.NoError(t, w.Write(json.RawMessage("{\n  \"a\": 1,\n  \"b\": \"x y\"\n}")))
	require.Equal(t, "{\"a\":1,\"b\":\"x y\"}\n", buf.String(), "one message is one physical line")
	require.Equal(t, 1, strings.Count(buf.String(), "\n"))
}

func TestLineWriterBounds(t *testing.T) {
	const limit = 32
	var buf bytes.Buffer
	w := NewLineWriter(&buf, limit)
	exact := json.RawMessage(`"` + strings.Repeat("a", limit-2) + `"`)
	require.NoError(t, w.Write(exact))
	require.Equal(t, limit+1, buf.Len())

	buf.Reset()
	over := json.RawMessage(`"` + strings.Repeat("a", limit-1) + `"`)
	require.ErrorIs(t, w.Write(over), ErrLineTooLong)
	require.Zero(t, buf.Len())
	require.ErrorIs(t, w.Write(json.RawMessage(`{`)), ErrFrameInvalid)
	require.ErrorIs(t, NewLineWriter(&shortWriter{n: 1}, 64).Write(json.RawMessage(`{}`)), io.ErrShortWrite)
}

func TestLineReaderLinesStayValidAfterNextRead(t *testing.T) {
	r := NewLineReader(strings.NewReader(`{"first":1}`+"\n"+`{"second":22}`+"\n"), 64)
	first, err := r.Next()
	require.NoError(t, err)
	second, err := r.Next()
	require.NoError(t, err)
	require.Equal(t, `{"second":22}`, string(second))
	require.Equal(t, `{"first":1}`, string(first), "an earlier line must not change when the reader moves on")
}

func TestLineBoundaryAtRealMaxLine(t *testing.T) {
	line := strings.Repeat("a", MaxLine)
	r := NewLineReader(strings.NewReader(line+"\n"+line+"b\n"), MaxLine)
	got, err := r.Next()
	require.NoError(t, err)
	require.Len(t, got, MaxLine, "a line of exactly the limit, then its newline, is accepted")
	_, err = r.Next()
	require.ErrorIs(t, err, ErrLineTooLong)

	var sink bytes.Buffer
	w := NewLineWriter(&sink, MaxLine)
	require.NoError(t, w.Write(json.RawMessage(`"`+line[:MaxLine-2]+`"`)))
	require.Equal(t, MaxLine+1, sink.Len())
	sink.Reset()
	require.ErrorIs(t, w.Write(json.RawMessage(`"`+line[:MaxLine-1]+`"`)), ErrLineTooLong)
	require.Zero(t, sink.Len())
}
