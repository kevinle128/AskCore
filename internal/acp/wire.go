package acp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
)

// ErrLineTooLong means one inbound frame went over the byte cap.
var ErrLineTooLong = errors.New("acp: inbound frame too long")

// CheckedWriter turns a lost write into a permanent connection failure.
// The SDK ignores short writes and discards the error of a response write,
// so it would report success for bytes that never left the process.
type CheckedWriter struct {
	mu     sync.Mutex
	w      io.Writer
	err    error
	onFail func(error)
	// observe sees each frame that was written in full.
	observe func([]byte)

	// watchMu guards the stall watch. Write holds mu while it writes, so the
	// watch has its own lock.
	watchMu sync.Mutex
	// started is the start of the write in progress; zero when none runs.
	started time.Time
	// stallAfter and onStall describe the watch; onStall is nil when none.
	stallAfter time.Duration
	onStall    func()
	stallTimer *time.Timer
}

// NewCheckedWriter wraps w. onFail runs once, on the first failed write.
// It must stop the connection, for example by closing its input side.
// It may be nil.
func NewCheckedWriter(w io.Writer, onFail func(error)) *CheckedWriter {
	return &CheckedWriter{w: w, onFail: onFail}
}

// Write fails on an error or on a short count, and then fails forever.
func (c *CheckedWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return 0, err
	}
	c.beginWrite()
	n, err := c.w.Write(p)
	c.endWrite()
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err == nil {
		seen := c.observe
		c.mu.Unlock()
		if seen != nil {
			seen(p)
		}
		return n, nil
	}
	c.err = err
	fail := c.onFail
	c.mu.Unlock()
	if fail != nil {
		fail(err)
	}
	return n, err
}

// WatchStall calls onStall when one write runs longer than after. A write that
// is in progress when the watch starts counts from its own start. The watch
// is a timer for each write, not a periodic check. The returned function ends
// the watch. onStall can run more than once, once for each stalled write.
func (c *CheckedWriter) WatchStall(after time.Duration, onStall func()) (stop func()) {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	c.stopTimerLocked()
	c.stallAfter, c.onStall = after, onStall
	if !c.started.IsZero() {
		c.stallTimer = time.AfterFunc(max(after-time.Since(c.started), 0), onStall)
	}
	return func() {
		c.watchMu.Lock()
		defer c.watchMu.Unlock()
		c.stopTimerLocked()
		c.onStall = nil
	}
}

func (c *CheckedWriter) beginWrite() {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	c.started = time.Now()
	if c.onStall != nil {
		c.stallTimer = time.AfterFunc(c.stallAfter, c.onStall)
	}
}

func (c *CheckedWriter) endWrite() {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	c.started = time.Time{}
	c.stopTimerLocked()
}

func (c *CheckedWriter) stopTimerLocked() {
	if c.stallTimer != nil {
		c.stallTimer.Stop()
		c.stallTimer = nil
	}
}

// Err returns the first write failure, or nil.
func (c *CheckedWriter) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// LineLimitReader fails a read when one line passes max bytes.
// It counts bytes as they arrive, so the SDK scanner never buffers more
// than max bytes of one frame, whatever the split of the input.
type LineLimitReader struct {
	r   io.Reader
	max int
	cur int
	err error
}

// NewLineLimitReader wraps r. max is the most bytes of one line, without the newline.
func NewLineLimitReader(r io.Reader, max int) *LineLimitReader {
	return &LineLimitReader{r: r, max: max}
}

// Read returns ErrLineTooLong, without data, when a line goes over the cap.
func (l *LineLimitReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.r.Read(p)
	for i := 0; i < n; i++ {
		if p[i] == '\n' {
			l.cur = 0
			continue
		}
		if l.cur++; l.cur > l.max {
			l.err = ErrLineTooLong
			return 0, l.err
		}
	}
	return n, err
}

// Observe sets a function that sees each frame after it was written in full.
// A failed write is not shown. It runs on the goroutine of the writer, after
// the write and before Write returns, so it must return quickly.
func (c *CheckedWriter) Observe(fn func(frame []byte)) {
	c.mu.Lock()
	c.observe = fn
	c.mu.Unlock()
}

// NewQuietLogger returns the logger for the SDK connection. It writes the time,
// the level and the message of an entry and no attribute, because the SDK puts
// raw inbound lines and error texts into attributes.
func NewQuietLogger(w io.Writer) *slog.Logger {
	keep := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && (a.Key == slog.TimeKey || a.Key == slog.LevelKey || a.Key == slog.MessageKey) {
			return a
		}
		return slog.Attr{}
	}
	return slog.New(&quietHandler{inner: slog.NewTextHandler(w, &slog.HandlerOptions{ReplaceAttr: keep})})
}

// quietHandler drops the attributes of a record before the text handler sees them.
type quietHandler struct{ inner slog.Handler }

func (h *quietHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *quietHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, slog.NewRecord(r.Time, r.Level, r.Message, r.PC))
}

func (h *quietHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *quietHandler) WithGroup(string) slog.Handler      { return h }
