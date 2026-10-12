package sandbox

import (
	"errors"
	"os"
	"slices"
	"sync"
)

const (
	// headSize and tailSize are the bytes of output that stay in memory: the
	// first headSize bytes and the last tailSize bytes.
	headSize = 20 * 1024
	tailSize = 30 * 1024
	// spillThreshold is the output size at which the spill file starts. Up to
	// this size the buffer holds every byte.
	spillThreshold = headSize + tailSize
	// defaultSpillLimit is the size at which the spill file stops growing.
	defaultSpillLimit = 64 << 20
)

// Output is a copy of the output of a process at one moment.
type Output struct {
	// Head holds the first bytes and Tail the last bytes of the output. When
	// Dropped is zero, Head followed by Tail is the whole output.
	Head, Tail []byte
	// Total counts every byte that the process wrote. Dropped counts the
	// bytes between Head and Tail that are not in memory.
	Total, Dropped int64
	// SpillPath is the file with the output after it passed spillThreshold.
	// It is empty when there is no spill file.
	SpillPath string
	// SpillCapped is true when the file stopped growing at its size limit and
	// later bytes were not saved. The process was not stopped.
	SpillCapped bool
	// SpillErr is the error that stopped the spill file, if any.
	SpillErr error
}

// buffer keeps the head and the tail of a byte stream and counts the rest.
// The zero value is ready to use and safe for concurrent use.
type buffer struct {
	mu      sync.Mutex
	head    []byte
	ring    []byte // tail ring, allocated on the first byte past the head
	ringPos int    // index of the next write; the oldest byte once the ring is full
	ringLen int
	total   int64
}

func (b *buffer) write(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	if room := headSize - len(b.head); room > 0 {
		n := min(room, len(p))
		b.head = append(b.head, p[:n]...)
		p = p[n:]
	}
	if len(p) == 0 {
		return
	}
	if b.ring == nil {
		b.ring = make([]byte, tailSize)
	}
	if len(p) >= tailSize {
		copy(b.ring, p[len(p)-tailSize:])
		b.ringPos, b.ringLen = 0, tailSize
		return
	}
	n := copy(b.ring[b.ringPos:], p)
	if n < len(p) {
		copy(b.ring, p[n:])
	}
	b.ringPos = (b.ringPos + len(p)) % tailSize
	b.ringLen = min(b.ringLen+len(p), tailSize)
}

// size returns the number of bytes written so far.
func (b *buffer) size() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// snapshot returns copies of the head and the tail and the byte count.
func (b *buffer) snapshot() (head, tail []byte, total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.head), b.tailLocked(), b.total
}

// tailLocked returns a copy of the kept tail in write order.
func (b *buffer) tailLocked() []byte {
	if b.ringLen == 0 {
		return nil
	}
	out := make([]byte, 0, b.ringLen)
	if b.ringLen < tailSize {
		return append(out, b.ring[:b.ringLen]...)
	}
	out = append(out, b.ring[b.ringPos:]...)
	return append(out, b.ring[:b.ringPos]...)
}

// readFrom returns the bytes at and after cursor that memory still holds, the
// cursor for the next call, and the count of bytes at or after cursor that
// memory no longer holds. The skipped bytes lie between the head and the
// tail; data joins what comes before and after them.
func (b *buffer) readFrom(cursor int64) (data []byte, next, skipped int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cursor = max(cursor, 0)
	if cursor >= b.total {
		return nil, b.total, 0
	}
	tail := b.tailLocked()
	headLen := int64(len(b.head))
	tailStart := b.total - int64(len(tail))
	switch {
	case cursor < headLen:
		data = append(slices.Clone(b.head[cursor:]), tail...)
		skipped = tailStart - headLen
	case cursor < tailStart:
		data = tail
		skipped = tailStart - cursor
	default:
		data = tail[cursor-tailStart:]
	}
	return data, b.total, skipped
}

// spill copies the whole output into a file once it passes spillThreshold.
// The writer methods (start, append, close) must run under one lock of the
// caller so that the file keeps the byte order of the buffer. The state
// methods are safe from any goroutine.
type spill struct {
	open  func() (*os.File, error)
	limit int64

	tried   bool
	file    *os.File
	written int64
	stopped bool

	mu     sync.Mutex
	path   string
	capped bool
	err    error
}

// start opens the file when the next chunk would pass spillThreshold and
// writes the output kept so far into it. It runs before the chunk enters the
// buffer, so the buffer still holds every earlier byte. open runs without the
// buffer lock, so it may call code that takes a snapshot.
func (s *spill) start(buf *buffer, incoming int) {
	if s.open == nil || s.tried || buf.size()+int64(incoming) <= spillThreshold {
		return
	}
	s.tried = true
	f, err := s.open()
	if err == nil && f == nil {
		err = errors.New("sandbox: spill file function returned no file")
	}
	if err != nil {
		s.fail(err)
		return
	}
	// The file may hold output that the model must not share: keep it private
	// even when the caller used a looser mode.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		s.fail(err)
		return
	}
	s.file = f
	s.mu.Lock()
	s.path = f.Name()
	s.mu.Unlock()
	head, tail, _ := buf.snapshot()
	s.append(head)
	s.append(tail)
}

// append writes p to the file until the size limit and then stops the tee.
func (s *spill) append(p []byte) {
	if s.file == nil || s.stopped || len(p) == 0 {
		return
	}
	cut := false
	if room := s.limit - s.written; int64(len(p)) > room {
		p, cut = p[:room], true
	}
	if len(p) > 0 {
		n, err := s.file.Write(p)
		s.written += int64(n)
		if err != nil {
			s.stopped = true
			s.fail(err)
			return
		}
	}
	if cut {
		s.stopped = true
		s.mu.Lock()
		s.capped = true
		s.mu.Unlock()
	}
}

func (s *spill) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// close closes the file. Call it after the last append.
func (s *spill) close() {
	if s.file == nil {
		return
	}
	if err := s.file.Close(); err != nil {
		s.fail(err)
	}
	s.file = nil
}

func (s *spill) state() (path string, capped bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path, s.capped, s.err
}
