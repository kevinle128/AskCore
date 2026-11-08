package tasks

import (
	"bytes"
	"os"
	"sync"
	"syscall"
	"unicode/utf8"
)

// ringSize is the number of output bytes that a sink keeps in memory.
const ringSize = 64 << 10

// Sink receives the output of a task. It keeps the last 64 KiB in memory for
// the notice and writes all output to a file. It is safe for concurrent use.
//
// The file opens on the first write, not at creation, so a task that writes
// nothing leaves no file. The open call uses O_EXCL and O_NOFOLLOW: it never
// reuses a file and never follows a symbolic link, so a planted path cannot
// redirect the output.
type Sink struct {
	path    string
	prepare func() error
	onWrite func()

	mu      sync.Mutex
	file    *os.File
	opened  bool // this sink created the file
	closed  bool
	fileErr error // first error of the file; later writes do not retry

	ring     []byte // allocated at the first write
	head     int    // index of the next byte to write
	filled   int    // number of valid bytes in the ring
	total    int64
	newlines int64
	lastNL   bool
}

// newSink returns a sink for the file at path. prepare, if set, runs before
// the file is opened. onWrite, if set, runs after every write with no lock
// held.
func newSink(path string, prepare func() error, onWrite func()) *Sink {
	return &Sink{path: path, prepare: prepare, onWrite: onWrite}
}

// Write records p in the ring and in the file. The ring always takes the
// bytes. If the file cannot be opened or written, Write returns the error and
// the count of file bytes written, and the sink does not try the file again.
// After Close, Write returns os.ErrClosed and records nothing.
func (s *Sink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, os.ErrClosed
	}
	s.record(p)
	n, err := s.writeFile(p)
	s.mu.Unlock()
	if s.onWrite != nil {
		s.onWrite()
	}
	return n, err
}

// record updates the ring and the counters. The caller holds s.mu.
func (s *Sink) record(p []byte) {
	s.total += int64(len(p))
	s.newlines += int64(bytes.Count(p, []byte{'\n'}))
	s.lastNL = p[len(p)-1] == '\n'

	if s.ring == nil {
		s.ring = make([]byte, ringSize)
	}
	if len(p) >= ringSize {
		copy(s.ring, p[len(p)-ringSize:])
		s.head, s.filled = 0, ringSize
		return
	}
	n := copy(s.ring[s.head:], p)
	if n < len(p) {
		copy(s.ring, p[n:])
	}
	s.head = (s.head + len(p)) % ringSize
	s.filled = min(ringSize, s.filled+len(p))
}

// writeFile appends p to the file and opens it if needed. The caller holds
// s.mu.
func (s *Sink) writeFile(p []byte) (int, error) {
	if s.fileErr != nil {
		return 0, s.fileErr
	}
	if s.file == nil {
		if s.prepare != nil {
			if err := s.prepare(); err != nil {
				s.fileErr = err
				return 0, err
			}
		}
		f, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			s.fileErr = err
			return 0, err
		}
		s.file, s.opened = f, true
	}
	n, err := s.file.Write(p)
	if err != nil {
		s.fileErr = err
	}
	return n, err
}

// Tail returns a copy of the bytes in the ring: the last 64 KiB of output, or
// all output if it is shorter. It does not start inside a UTF-8 sequence.
func (s *Sink) Tail() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.filled == 0 {
		return nil
	}
	out := make([]byte, 0, s.filled)
	if s.filled < ringSize {
		out = append(out, s.ring[:s.filled]...)
	} else {
		out = append(out, s.ring[s.head:]...)
		out = append(out, s.ring[:s.head]...)
	}
	if s.truncatedLocked() {
		// The ring dropped older bytes, so the first byte can be inside a rune.
		for i := 0; i < utf8.UTFMax-1 && len(out) > 0 && !utf8.RuneStart(out[0]); i++ {
			out = out[1:]
		}
	}
	return out
}

// Truncated reports whether the ring dropped older output. The file, if it
// opened, still holds everything.
func (s *Sink) Truncated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.truncatedLocked()
}

// truncatedLocked reports whether the ring dropped older output. The caller
// holds s.mu.
func (s *Sink) truncatedLocked() bool {
	return s.total > int64(s.filled)
}

// Stats returns the number of bytes and lines written. A last line without a
// newline counts as a line.
func (s *Sink) Stats() (bytes, lines int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines = s.newlines
	if s.total > 0 && !s.lastNL {
		lines++
	}
	return s.total, lines
}

// FilePath returns the path of the output file, or an empty string if this
// sink did not create a file.
func (s *Sink) FilePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return ""
	}
	return s.path
}

// Err returns the first error of the output file, if any.
func (s *Sink) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileErr
}

// Close closes the file. Later writes fail. Close is safe to call more than
// once.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	if err != nil && s.fileErr == nil {
		s.fileErr = err
	}
	return err
}
