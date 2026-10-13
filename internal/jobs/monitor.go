//go:build unix

package jobs

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"AskCore/internal/sandbox"
)

// The monitor numbers are in one place so that real dev-server logs can tune
// them without a design change.
const (
	// monitorLineMax cuts one line, in characters.
	monitorLineMax = 500
	// monitorBatchMax cuts one event, in characters.
	monitorBatchMax = 3000
	// monitorDebounce is the window in which lines form one event.
	monitorDebounce = 200 * time.Millisecond
	// monitorBucketSize is the burst of events; monitorRefillEvery gives one
	// token back.
	monitorBucketSize  = 10
	monitorRefillEvery = 2 * time.Second
	// monitorOverloadAfter is the time of continuous rate-limit violation
	// after which the table kills the process group.
	monitorOverloadAfter = 30 * time.Second
	// monitorDefaultTimeout applies when a non-persistent monitor has none.
	monitorDefaultTimeout = 10 * time.Hour

	monitorCutMarker  = "[... more lines cut]"
	overloadEventText = "%s stopped: output too fast for the monitor"
	overloadReason    = "overload"
	timeoutReason     = "timeout"
)

// MonitorSpec describes a monitor: a command whose every stdout line is an
// event for the model.
type MonitorSpec struct {
	Command string
	Cwd     string
	// Description names the watch in each event.
	Description string
	// Timeout stops the monitor after that time, with the kill reason
	// "timeout". Zero means 10 h. It is ignored when Persistent is true.
	Timeout time.Duration
	// Persistent runs the monitor until job_kill or the end of the session.
	Persistent bool
}

// StartMonitor starts a monitor as a job of kind monitor. It counts toward
// the cap of active jobs. Stdout lines go to the model as events through
// Notifier.Steer; stderr goes to the job buffer only, and the end of the
// command ends the watch with the normal completion notice.
func (t *Table) StartMonitor(spec MonitorSpec) (*Job, error) {
	m := &monitor{description: spec.Description}
	j, err := t.launch(KindMonitor, spec.Command, spec.Cwd, m, false)
	if err != nil {
		return nil, err
	}
	if spec.Persistent {
		return j, nil
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = monitorDefaultTimeout
	}
	stop := t.clock.AfterFunc(timeout, func() { t.killJob(j, timeoutReason, false) })
	t.mu.Lock()
	if j.finishedLocked() {
		t.mu.Unlock()
		stop()
		return j, nil
	}
	j.stopTimeout = stop
	t.mu.Unlock()
	return j, nil
}

// monitor turns the stdout of one process into batched, rate-limited events.
type monitor struct {
	description string

	t   *Table
	job *Job

	flushMu     sync.Mutex // finish waits for a debounce delivery already in progress
	mu          sync.Mutex // guards the fields below; never held across a call out
	clock       sandbox.Clock
	partial     []byte // the unfinished line
	discarding  bool   // the line passed its byte limit; skip it up to the line end
	batch       []string
	batchRunes  int
	batchCut    bool
	debounceOn  bool
	stopDeb     func() bool
	lineCount   int
	stopped     bool // no more events: the overload kill ran
	tokens      int
	lastRefill  time.Time
	suppressed  int
	lastDrop    time.Time
	violatingAt time.Time

	sendMu    sync.Mutex // orders deliveries; holds the merge state below
	pendingID string
	pending   string
}

// bind connects the monitor to its table and job before the process starts.
func (m *monitor) bind(t *Table, j *Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t, m.job, m.clock = t, j, t.clock
	m.tokens = monitorBucketSize
	m.lastRefill = t.clock.Now()
}

// received returns the number of lines the monitor has taken so far.
func (m *monitor) received() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lineCount
}

// observe takes one chunk of output. It runs inside the write lock of the
// process, so it only splits lines and arms the debounce timer.
func (m *monitor) observe(stream sandbox.Stream, chunk []byte) {
	if stream != sandbox.StreamStdout {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			m.appendPartialLocked(chunk)
			return
		}
		m.appendPartialLocked(chunk[:i])
		m.endLineLocked()
		chunk = chunk[i+1:]
	}
}

// appendPartialLocked copies seg into the unfinished line, up to the byte
// limit of a line. The chunk of the process is reused after the call.
func (m *monitor) appendPartialLocked(seg []byte) {
	if m.discarding {
		return
	}
	limit := monitorLineMax * utf8.UTFMax
	if room := limit - len(m.partial); len(seg) > room {
		m.partial = append(m.partial, seg[:room]...)
		m.discarding = true
		return
	}
	m.partial = append(m.partial, seg...)
}

// endLineLocked closes the unfinished line and adds it to the batch.
func (m *monitor) endLineLocked() {
	line := strings.TrimSuffix(string(m.partial), "\r")
	m.partial, m.discarding = m.partial[:0], false
	if strings.TrimSpace(line) == "" {
		return
	}
	line = cutRunes(strings.ToValidUTF8(line, "�"), monitorLineMax)
	m.lineCount++
	if m.stopped || m.batchCut {
		return
	}
	n := utf8.RuneCountInString(line)
	sep := 0
	if len(m.batch) > 0 {
		sep = 1 // the line end that joins the line to the previous one
	}
	if m.batchRunes+sep+n > monitorBatchMax {
		// Keep what fits so that the event is exactly the cap, then ignore
		// the rest of the window.
		if room := monitorBatchMax - m.batchRunes - sep; room >= 0 {
			m.batch = append(m.batch, cutRunes(line, room))
			m.batchRunes = monitorBatchMax
		}
		m.batchCut = true
	} else {
		m.batch = append(m.batch, line)
		m.batchRunes += sep + n
	}
	if !m.debounceOn {
		m.debounceOn = true
		m.stopDeb = m.clock.AfterFunc(monitorDebounce, m.flush)
	}
}

// takeBatchLocked returns the text of the batch and resets it.
func (m *monitor) takeBatchLocked() (text string, ok bool) {
	if len(m.batch) == 0 {
		return "", false
	}
	text = strings.Join(m.batch, "\n")
	if m.batchCut {
		text += "\n" + monitorCutMarker
	}
	m.batch, m.batchRunes, m.batchCut = nil, 0, false
	return text, true
}

// flush closes the debounce window and delivers the batch.
func (m *monitor) flush() {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	m.mu.Lock()
	m.debounceOn = false
	text, ok := m.takeBatchLocked()
	m.mu.Unlock()
	if ok {
		m.emit(text)
	}
}

// finish runs after the process ended and all output was read: the last
// unfinished line is an event, and the timers stop. Nothing is sent when the
// model stopped the job or the table is closing.
func (m *monitor) finish() {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	m.mu.Lock()
	if len(m.partial) > 0 {
		m.endLineLocked()
	}
	if m.stopDeb != nil {
		m.stopDeb()
	}
	m.debounceOn = false
	text, ok := m.takeBatchLocked()
	m.mu.Unlock()
	if ok {
		m.emit(text)
	}
}

// emit applies the flood guard to one event and delivers it.
func (m *monitor) emit(text string) {
	if m.t.eventsMuted(m.job) {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	now := m.clock.Now()
	m.refillLocked(now)
	if m.tokens > 0 {
		m.tokens--
		if m.suppressed > 0 {
			text += fmt.Sprintf("\n[%d events suppressed by the flood guard]", m.suppressed)
			m.suppressed = 0
		}
		m.mu.Unlock()
		m.deliver(text)
		return
	}
	m.suppressed++
	// A drop that comes within one refill interval of the previous drop
	// continues the violation; a quiet gap starts a new one.
	if m.violatingAt.IsZero() || now.Sub(m.lastDrop) > monitorRefillEvery {
		m.violatingAt = now
	}
	m.lastDrop = now
	overloaded := now.Sub(m.violatingAt) >= monitorOverloadAfter
	if overloaded {
		m.stopped = true
		m.batch, m.batchRunes, m.batchCut = nil, 0, false
		if m.stopDeb != nil {
			m.stopDeb()
		}
	}
	m.mu.Unlock()
	if overloaded {
		// The kill stops the whole group, not only the reader: the command
		// would otherwise keep running with nobody listening.
		m.t.killJob(m.job, overloadReason, false)
		m.deliver(fmt.Sprintf(overloadEventText, m.job.id))
	}
}

// refillLocked gives tokens back from the time that passed.
func (m *monitor) refillLocked(now time.Time) {
	if m.tokens >= monitorBucketSize {
		m.lastRefill = now
		return
	}
	n := int(now.Sub(m.lastRefill) / monitorRefillEvery)
	if n <= 0 {
		return
	}
	m.tokens = min(monitorBucketSize, m.tokens+n)
	m.lastRefill = m.lastRefill.Add(time.Duration(n) * monitorRefillEvery)
	if m.tokens >= monitorBucketSize {
		m.lastRefill = now
	}
}

// deliver sends one event with Steer. When the previous message is still
// queued, Remove takes it back and both events go out as one message, so the
// Agent reads one reminder per delivery.
func (m *monitor) deliver(text string) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	n, ok := m.t.notifierSnapshot()
	if !ok || n.Steer == nil {
		m.t.report(fmt.Errorf("events of job %s: %w", m.job.id, ErrNoNotifier))
		return
	}
	body := text
	if m.pendingID != "" && n.Remove != nil && n.Remove(m.pendingID) {
		body = strings.TrimPrefix(m.pending, monitorCutMarker+"\n") + "\n" + text
		runes := []rune(body)
		if len(runes) > monitorBatchMax {
			body = monitorCutMarker + "\n" + string(runes[len(runes)-monitorBatchMax:])
		} else if strings.HasPrefix(m.pending, monitorCutMarker+"\n") {
			body = monitorCutMarker + "\n" + body
		}
	}
	m.pendingID, m.pending = "", ""
	if m.t.eventsMuted(m.job) {
		return
	}
	element := monitorElement(m.job.id, m.description, body)
	id, err := n.Steer(m.t.reminderMessage(element))
	if err != nil {
		m.t.report(fmt.Errorf("events of job %s: %w", m.job.id, err))
		return
	}
	m.pendingID, m.pending = id, body
}

// monitorElement wraps the event lines of one delivery for the model.
func monitorElement(job, description, body string) string {
	return fmt.Sprintf("<monitor-event job=\"%s\" description=\"%s\">\n%s\n</monitor-event>",
		html.EscapeString(job), html.EscapeString(description), body)
}
