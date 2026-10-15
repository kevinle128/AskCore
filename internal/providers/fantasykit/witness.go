package fantasykit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"unicode"
)

// Witness forwards every response body byte and keeps the last JSON string
// value of key. Fantasy's finish part has already rewritten that string.
type Witness struct {
	key  []byte
	base http.RoundTripper
	mu   sync.Mutex
	raw  string
	scan reasonScan
	done bool
	tail []byte
}

func NewWitness(key string) *Witness {
	if key == "" {
		key = "stop_reason"
	}
	return &Witness{key: []byte(`"` + key + `"`)}
}

func (w *Witness) SetBase(base http.RoundTripper) {
	if w == nil {
		return
	}
	w.base = base
}

func (w *Witness) RoundTrip(req *http.Request) (*http.Response, error) {
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

func (w *Witness) StopReason() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.raw
}

func (w *Witness) SawDone() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.done
}

type witnessBody struct {
	io.ReadCloser
	w *Witness
}

func (b *witnessBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.w.mu.Lock()
		marker := append(append([]byte(nil), b.w.tail...), p[:n]...)
		if bytes.Contains(marker, []byte("data: [DONE]\n")) || bytes.Contains(marker, []byte("data: [DONE]\r\n")) {
			b.w.done = true
		}
		if len(marker) > 16 {
			marker = marker[len(marker)-16:]
		}
		b.w.tail = marker
		if s, ok := b.w.scan.feed(p[:n], b.w.key); ok {
			b.w.raw = s
		}
		b.w.mu.Unlock()
	}
	return n, err
}

type reasonScan struct {
	buf []byte
}

func (s *reasonScan) feed(p, key []byte) (string, bool) {
	s.buf = append(s.buf, p...)
	if len(s.buf) > 1<<20 {
		s.buf = s.buf[len(s.buf)-1<<16:]
	}
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
