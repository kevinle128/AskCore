//go:build unix

package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// sentMessage is one message that the fake notifier accepted.
type sentMessage struct {
	followUp bool
	id       string
	text     string
	message  protocol.UserMessage
}

// fakeNotifier records the messages of a table. A message stays pending until
// Remove takes it, as in the queue of an idle Agent that has not started its
// next cycle yet.
type fakeNotifier struct {
	mu      sync.Mutex
	sent    []sentMessage
	pending map[string]bool
	removed []string
	err     error
	n       int
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{pending: map[string]bool{}}
}

func (f *fakeNotifier) notifier() Notifier {
	return Notifier{
		FollowUp: func(m protocol.Message) (string, error) { return f.accept(m, true) },
		Steer:    func(m protocol.Message) (string, error) { return f.accept(m, false) },
		Remove:   f.remove,
	}
}

func (f *fakeNotifier) accept(m protocol.Message, followUp bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	um, ok := m.(protocol.UserMessage)
	if !ok {
		return "", fmt.Errorf("message is %T, want a user message", m)
	}
	text := ""
	for _, b := range um.Content {
		if tb, ok := b.(protocol.Text); ok {
			text += tb.Text
		}
	}
	f.n++
	id := fmt.Sprintf("in-%d", f.n)
	f.sent = append(f.sent, sentMessage{followUp: followUp, id: id, text: text, message: um})
	f.pending[id] = true
	return id, nil
}

func (f *fakeNotifier) remove(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	if !f.pending[id] {
		return false
	}
	delete(f.pending, id)
	return true
}

// claim marks a message as taken by the Agent, so that Remove returns false.
func (f *fakeNotifier) claim(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, id)
}

func (f *fakeNotifier) all() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.sent...)
}

func (f *fakeNotifier) kind(followUp bool) []sentMessage {
	var out []sentMessage
	for _, m := range f.all() {
		if m.followUp == followUp {
			out = append(out, m)
		}
	}
	return out
}

func (f *fakeNotifier) followUps() []sentMessage { return f.kind(true) }
func (f *fakeNotifier) steers() []sentMessage    { return f.kind(false) }

func (f *fakeNotifier) removedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// rig is a table with a fake notifier, an optional fake clock and a working
// directory. Its cleanup closes the table and checks that every process group
// that the test started is gone.
type rig struct {
	t     *testing.T
	tb    *Table
	n     *fakeNotifier
	clock *fakeClock
	dir   string

	mu    sync.Mutex
	errs  []error
	pgids []int
}

type rigOption func(*rigConfig)

type rigConfig struct {
	fakeClock  bool
	noNotifier bool
	mutate     []func(*Options)
}

func withFakeClock() rigOption { return func(c *rigConfig) { c.fakeClock = true } }

func withoutNotifier() rigOption { return func(c *rigConfig) { c.noNotifier = true } }

func withOptions(f func(*Options)) rigOption {
	return func(c *rigConfig) { c.mutate = append(c.mutate, f) }
}

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	var cfg rigConfig
	for _, o := range opts {
		o(&cfg)
	}
	r := &rig{t: t, n: newFakeNotifier(), dir: t.TempDir()}
	o := Options{OnError: func(err error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.errs = append(r.errs, err)
	}}
	if cfg.fakeClock {
		r.clock = newFakeClock()
		o.Clock = r.clock
	}
	for _, m := range cfg.mutate {
		m(&o)
	}
	r.tb = New(o)
	if !cfg.noNotifier {
		r.tb.SetNotifier(r.n.notifier())
	}
	t.Cleanup(func() {
		_ = r.tb.Close()
		r.mu.Lock()
		pgids := append([]int(nil), r.pgids...)
		r.mu.Unlock()
		for _, pgid := range pgids {
			requireGroupGone(t, pgid)
		}
	})
	return r
}

// bash starts command as a bash job.
func (r *rig) bash(command string) *Job {
	r.t.Helper()
	j, err := r.tb.Start(StartSpec{Command: command, Cwd: r.dir})
	require.NoError(r.t, err)
	r.track(j)
	return j
}

func (r *rig) monitor(spec MonitorSpec) *Job {
	r.t.Helper()
	if spec.Cwd == "" {
		spec.Cwd = r.dir
	}
	if spec.Description == "" {
		spec.Description = "watch"
	}
	j, err := r.tb.StartMonitor(spec)
	require.NoError(r.t, err)
	r.track(j)
	return j
}

func (r *rig) track(j *Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pgids = append(r.pgids, j.proc.PGID())
}

func (r *rig) errors() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.errs...)
}

// gate returns a command that runs until the file at path exists, and a
// function that creates the file.
func (r *rig) gate(name string) (command string, open func()) {
	path := filepath.Join(r.dir, name)
	command = fmt.Sprintf("while [ ! -f %q ]; do sleep 0.05; done", path)
	return command, func() {
		r.t.Helper()
		require.NoError(r.t, os.WriteFile(path, nil, 0o600))
	}
}

// fifo is a named pipe that a test writes lines into; a monitor command reads
// it with cat, so the lines take the real output path of the runner.
type fifo struct {
	path string
	w    *os.File
}

func (r *rig) fifo() *fifo {
	r.t.Helper()
	path := filepath.Join(r.dir, "lines.fifo")
	require.NoError(r.t, syscall.Mkfifo(path, 0o600))
	// O_RDWR keeps the open from blocking until cat opens its end.
	w, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(r.t, err)
	f := &fifo{path: path, w: w}
	r.t.Cleanup(f.close)
	return f
}

func (f *fifo) command() string { return fmt.Sprintf("cat %q", f.path) }

func (f *fifo) line(t *testing.T, s string) {
	t.Helper()
	_, err := f.w.WriteString(s + "\n")
	require.NoError(t, err)
}

func (f *fifo) close() { _ = f.w.Close() }

func requireGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still exists: kill(-pgid, 0) = %v", pgid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", limit, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitDone(t *testing.T, j *Job) {
	t.Helper()
	select {
	case <-j.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("job %s did not end", j.ID())
	}
}

// ignoreTerm is a command that ignores SIGTERM and prints "ready" once it does.
const ignoreTerm = "trap '' TERM; echo ready; while :; do sleep 0.05; done"

// waitReady waits for the "ready" line of ignoreTerm.
func waitReady(t *testing.T, j *Job) {
	t.Helper()
	waitFor(t, 5*time.Second, j.ID()+" ready", func() bool {
		out := j.Snapshot()
		return strings.Contains(string(out.Head)+string(out.Tail), "ready")
	})
}

func background() context.Context { return context.Background() }

// eventLines returns the lines inside the monitor-event elements of text.
func eventLines(text string) []string {
	var out []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "<monitor-event "):
			inside = true
		case line == "</monitor-event>":
			inside = false
		case inside:
			out = append(out, line)
		}
	}
	return out
}
