//go:build unix

package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	// readWaitDefault and readWaitMax bound the wait of Read.
	readWaitDefault = 30 * time.Second
	readWaitMax     = 600 * time.Second
	// stillRunningLine answers a read of a running job, so that the model
	// does not poll.
	stillRunningLine = "The job is still running. A notice will arrive when it finishes; do not poll."
)

// ReadResult is the answer of Read.
type ReadResult struct {
	JobID string
	// Output is the output since the previous read that memory still holds.
	// It does not include the notes and the status line.
	Output string
	// Skipped counts the bytes since the previous read that memory no longer
	// holds; SpillPath names the file that has them, when there is one.
	Skipped   int64
	SpillPath string
	// SpillCapped means the spill file stopped at its size limit.
	SpillCapped bool
	Status      Status
	// Running is true while the job runs or stops.
	Running    bool
	ExitCode   *int
	Signal     string
	KillReason string
	// StatusLine is `[status: ...]`.
	StatusLine string
	// Text is the whole answer for the model: the output with the note of the
	// skipped bytes at its gap, the status line, and for a running job the
	// line that says a notice will arrive.
	Text string
}

// Read returns the output of the job since the previous read and its status
// (job_output). A running job answers at once, unless wait is true: then Read
// blocks until the job ends, timeout passes (zero or less means 30 s, more
// than 600 s means 600 s) or ctx ends. Reading the final status marks the end
// as seen and removes the job, so no notice follows. A canceled read keeps
// the cursor and the job for a later call.
func (t *Table) Read(ctx context.Context, gen uint64, id string, wait bool, timeout time.Duration) (ReadResult, error) {
	t.mu.Lock()
	j, ok := t.lookupLocked(gen, id)
	if !ok {
		t.mu.Unlock()
		return ReadResult{}, fmt.Errorf("%w: %s", ErrUnknownJob, id)
	}
	if wait && ctx.Err() == nil && !j.finishedLocked() {
		j.waiters++
		t.mu.Unlock()
		t.awaitEnd(ctx, j, timeout)
		t.mu.Lock()
		j.waiters--
	}
	defer func() {
		t.mu.Unlock()
		if ctx.Err() != nil {
			t.sendNotice(j)
		}
	}()

	data, next, skipped := j.proc.ReadFrom(j.cursor)
	from := j.cursor
	canceled := ctx.Err() != nil
	if !canceled {
		j.cursor = next
	}
	finished := j.finishedLocked()
	if finished && !canceled {
		t.takeLocked(j)
	} else if finished && j.waiters == 0 && t.jobs[j.id] == j && canceled {
		// A canceled reader did not report the end. Restore notice delivery
		// if the owner skipped it while that reader was registered.
		j.awaited = false
	}
	out := j.proc.Snapshot()
	res := ReadResult{
		SpillCapped: out.SpillCapped,
		JobID:       j.id,
		Skipped:     skipped,
		Status:      j.status,
		Running:     !finished,
		ExitCode:    j.exitCode,
		Signal:      j.signal,
		KillReason:  j.killReason,
		StatusLine:  j.statusLineLocked(),
	}
	if skipped > 0 {
		res.SpillPath = out.SpillPath
	}
	res.Output = string(data)
	res.Text = readText(j, res, data, from, skipped)
	return res, nil
}

// awaitEnd blocks until j ends, the timeout passes or ctx ends.
func (t *Table) awaitEnd(ctx context.Context, j *Job, timeout time.Duration) {
	if timeout <= 0 {
		timeout = readWaitDefault
	}
	timeout = min(timeout, readWaitMax)
	expired := make(chan struct{})
	stop := t.clock.AfterFunc(timeout, func() { close(expired) })
	defer stop()
	select {
	case <-j.done:
	case <-expired:
	case <-ctx.Done():
	}
}

// readText builds the model text of a read. The skipped bytes lie between the
// head and the tail of the buffer: after the head part of data when the read
// started inside the head, else before all of data.
func readText(j *Job, res ReadResult, data []byte, from, skipped int64) string {
	var b strings.Builder
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf("[%d bytes not kept in memory", skipped)
		if res.SpillPath != "" {
			note += "; see " + res.SpillPath
		}
		note += "]\n"
	}
	headKept := int64(0)
	if skipped > 0 {
		headKept = int64(len(j.proc.Snapshot().Head)) - from
	}
	switch {
	case skipped > 0 && headKept > 0 && headKept <= int64(len(data)):
		b.Write(data[:headKept])
		writeLineEnd(&b)
		b.WriteString(note)
		b.Write(data[headKept:])
	case skipped > 0:
		b.WriteString(note)
		b.Write(data)
	default:
		b.Write(data)
	}
	writeLineEnd(&b)
	b.WriteString(res.StatusLine)
	if res.Running {
		b.WriteString("\n" + stillRunningLine)
	}
	if res.SpillCapped {
		b.WriteString("\n[spill file capped at 64 MB]")
	}
	return b.String()
}

func writeLineEnd(b *strings.Builder) {
	if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
}
