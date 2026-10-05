package tokenplan

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"unicode"
)

// stopWitness forwards every response body byte and keeps the last JSON
// string value of the key stop_reason. Fantasy's finish part has already
// rewritten that string.
type stopWitness struct {
	base http.RoundTripper
	mu   sync.Mutex
	raw  string
	scan stopReasonScan
}

func (w *stopWitness) RoundTrip(req *http.Request) (*http.Response, error) {
	base := w.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = &witnessBody{ReadCloser: resp.Body, w: w}
	return resp, nil
}

func (w *stopWitness) StopReason() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.raw
}

type witnessBody struct {
	io.ReadCloser
	w *stopWitness
}

func (b *witnessBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.w.mu.Lock()
		if s, ok := b.w.scan.feed(p[:n]); ok {
			b.w.raw = s
		}
		b.w.mu.Unlock()
	}
	return n, err
}

type stopReasonScan struct {
	buf []byte
}

func (s *stopReasonScan) feed(p []byte) (string, bool) {
	s.buf = append(s.buf, p...)
	if len(s.buf) > 1<<20 {
		s.buf = s.buf[len(s.buf)-1<<16:]
	}
	key := []byte(`"stop_reason"`)
	found := false
	val := ""
	rest := s.buf
	for {
		i := bytes.Index(rest, key)
		if i < 0 {
			break
		}
		j := i + len(key)
		for j < len(rest) && unicode.IsSpace(rune(rest[j])) {
			j++
		}
		if j >= len(rest) || rest[j] != ':' {
			rest = rest[i+1:]
			continue
		}
		j++
		for j < len(rest) && unicode.IsSpace(rune(rest[j])) {
			j++
		}
		if j >= len(rest) {
			break
		}
		dec := json.NewDecoder(bytes.NewReader(rest[j:]))
		var v any
		if err := dec.Decode(&v); err != nil {
			break
		}
		if str, ok := v.(string); ok {
			val = str
			found = true
		}
		rest = rest[i+1:]
	}
	if keep := bytes.LastIndex(s.buf, key); keep >= 0 {
		s.buf = s.buf[keep:]
	} else if len(s.buf) > 32 {
		s.buf = s.buf[len(s.buf)-32:]
	}
	return val, found
}
