//go:build unix

package jobs

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// feed writes line into the fifo and waits until the monitor has seen it, so
// that a test can move the fake clock without a race against the reader.
func feed(t *testing.T, f *fifo, j *Job, line string, seen int) {
	t.Helper()
	f.line(t, line)
	waitFor(t, 5*time.Second, "monitor to see the line", func() bool { return j.mon.received() >= seen })
}

func TestMonitorSendsEveryStdoutLineAsSteer(t *testing.T) {
	r := newRig(t)
	j := r.monitor(MonitorSpec{Command: "echo a; echo err >&2; echo b", Description: "demo"})
	waitDone(t, j)
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })

	var lines []string
	for _, m := range r.n.steers() {
		assert.True(t, protocol.IsSystemReminder(m.message), m.text)
		assert.Contains(t, m.text, `<monitor-event job="monitor-1" description="demo">`)
		lines = append(lines, eventLines(m.text)...)
	}
	assert.Equal(t, []string{"a", "b"}, lines)
	for _, m := range r.n.steers() {
		assert.NotContains(t, m.text, "err")
	}
	// The stderr line is still in the job buffer.
	assert.Contains(t, string(j.Snapshot().Head), "err")
}

func TestMonitorBatchesLinesWithin200ms(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})

	for i, s := range []string{"l1", "l2", "l3", "l4", "l5"} {
		feed(t, f, j, s, i+1)
	}
	require.Equal(t, 1, r.clock.Pending(), "one debounce timer for the batch")
	assert.Empty(t, r.n.steers(), "nothing is sent inside the window")

	r.clock.Advance(200 * time.Millisecond)
	steers := r.n.steers()
	require.Len(t, steers, 1)
	assert.Equal(t, []string{"l1", "l2", "l3", "l4", "l5"}, eventLines(steers[0].text))

	// The Agent took the first message, so the next batch stands alone.
	r.n.claim(steers[0].id)
	r.clock.Advance(100 * time.Millisecond)
	feed(t, f, j, "late", 6)
	r.clock.Advance(200 * time.Millisecond)
	steers = r.n.steers()
	require.Len(t, steers, 2)
	assert.Equal(t, []string{"late"}, eventLines(steers[1].text))
}

func TestMonitorExitEndsWatchWithNotice(t *testing.T) {
	r := newRig(t)
	j := r.monitor(MonitorSpec{Command: "echo only", Description: "x"})
	waitDone(t, j)
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })

	notice := r.n.followUps()[0].text
	assert.Contains(t, notice, "Background job monitor-1 (monitor: echo only) finished [status: completed, exit code: 0]")
	// The last events come before the notice and nothing follows it.
	all := r.n.all()
	require.NotEmpty(t, all)
	assert.True(t, all[len(all)-1].followUp)
	assert.Equal(t, []string{"only"}, eventLines(all[0].text))
}

func TestMonitorTimeoutKillsGroup(t *testing.T) {
	r := newRig(t, withFakeClock())
	command, _ := r.gate("never")
	j := r.monitor(MonitorSpec{Command: command, Timeout: time.Second})
	require.Equal(t, 1, r.clock.Pending(), "the timeout timer")

	r.clock.Advance(time.Second)
	waitDone(t, j)
	assert.Equal(t, StatusKilled, j.Status())
	assert.Equal(t, "timeout", j.KillReason())
	requireGroupGone(t, j.proc.PGID())
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })
	assert.Contains(t, r.n.followUps()[0].text, "[status: killed, reason: timeout]")
}

func TestMonitorPersistentHasNoTimer(t *testing.T) {
	r := newRig(t, withFakeClock())
	command, _ := r.gate("never")
	j := r.monitor(MonitorSpec{Command: command, Persistent: true})
	assert.Zero(t, r.clock.Pending())

	r.clock.Advance(11 * time.Hour)
	assert.Equal(t, StatusRunning, j.Status())

	_, err := r.tb.Kill(r.gen(), j.ID(), "")
	require.NoError(t, err)
	requireGroupGone(t, j.proc.PGID())
	assert.Empty(t, r.n.all(), "job_kill sends no notice")

	// Close stops a persistent monitor too.
	other := r.monitor(MonitorSpec{Command: command, Persistent: true})
	require.NoError(t, r.tb.Close())
	assert.Equal(t, StatusKilled, other.Status())
	requireGroupGone(t, other.proc.PGID())
}

func TestMonitorKillByModelDropsPendingLines(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})
	feed(t, f, j, "pending", 1)

	_, err := r.tb.Kill(r.gen(), j.ID(), "stop")
	require.NoError(t, err)
	r.clock.Advance(time.Second)
	assert.Empty(t, r.n.all(), "a killed monitor sends neither the batch nor a notice")
}

func TestMonitorCoalescesPendingMessageWithRemove(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})

	feed(t, f, j, "one", 1)
	r.clock.Advance(200 * time.Millisecond)
	first := r.n.steers()
	require.Len(t, first, 1)

	feed(t, f, j, "two", 2)
	r.clock.Advance(200 * time.Millisecond)

	assert.Equal(t, []string{first[0].id}, r.n.removedIDs())
	steers := r.n.steers()
	require.Len(t, steers, 2)
	assert.Equal(t, []string{"one", "two"}, eventLines(steers[1].text))
	assert.True(t, protocol.IsSystemReminder(steers[1].message))
	assert.Equal(t, 1, strings.Count(steers[1].text, "<system-reminder>"), "one reminder wraps the merged events")

	// The Agent took the merged message: the next batch stands alone.
	r.n.claim(steers[1].id)
	feed(t, f, j, "three", 3)
	r.clock.Advance(200 * time.Millisecond)
	steers = r.n.steers()
	require.Len(t, steers, 3)
	assert.Equal(t, []string{"three"}, eventLines(steers[2].text))
}

func TestMonitorFloodGuardSuppressesAndRefills(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})

	// One line every 250 ms, so each line is one event. The bucket holds 10
	// tokens and gets one more at 2 s, which the event at 2.2 s uses: events
	// 0 to 10 are delivered, 11 to 14 are suppressed.
	for i := range 15 {
		feed(t, f, j, fmt.Sprintf("e%d", i), i+1)
		r.clock.Advance(200 * time.Millisecond)
		r.clock.Advance(50 * time.Millisecond)
		// The Agent takes every message, so each event is its own message.
		for _, m := range r.n.steers() {
			r.n.claim(m.id)
		}
	}
	steers := r.n.steers()
	require.Len(t, steers, 11)
	for i, m := range steers {
		assert.Equal(t, []string{fmt.Sprintf("e%d", i)}, eventLines(m.text))
		assert.NotContains(t, m.text, "suppressed")
	}

	// After the refill the next event carries the count.
	r.clock.Advance(2 * time.Second)
	feed(t, f, j, "after", 16)
	r.clock.Advance(200 * time.Millisecond)
	steers = r.n.steers()
	require.Len(t, steers, 12)
	assert.Contains(t, steers[11].text, "[4 events suppressed by the flood guard]")
	assert.Contains(t, eventLines(steers[11].text), "after")
}

func TestMonitorOverloadKillsProcess(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})

	// The flood continues for far more than 30 s of fake time, but the kill
	// must come at about the 30 s mark.
	var lines int
	for i := 0; i < 200 && j.Status() == StatusRunning; i++ {
		lines++
		feed(t, f, j, fmt.Sprintf("flood %d", i), lines)
		r.clock.Advance(200 * time.Millisecond)
		r.clock.Advance(50 * time.Millisecond)
		for _, m := range r.n.steers() {
			r.n.claim(m.id)
		}
	}
	waitDone(t, j)
	assert.Equal(t, StatusKilled, j.Status())
	assert.Equal(t, "overload", j.KillReason())
	assert.Less(t, lines, 150, "the kill comes after 30 s of violation, not at the end of the flood")
	requireGroupGone(t, j.proc.PGID())

	steers := r.n.steers()
	require.NotEmpty(t, steers)
	last := steers[len(steers)-1]
	assert.Contains(t, last.text, "monitor-1 stopped: output too fast for the monitor")
	waitFor(t, 5*time.Second, "completion notice", func() bool { return len(r.n.followUps()) == 1 })
	assert.Contains(t, r.n.followUps()[0].text, "[status: killed, reason: overload]")
}

func TestMonitorCutsLongLinesAndBatch(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})

	feed(t, f, j, strings.Repeat("a", 800), 1)
	r.clock.Advance(200 * time.Millisecond)
	steers := r.n.steers()
	require.Len(t, steers, 1)
	lines := eventLines(steers[0].text)
	require.Len(t, lines, 1)
	assert.Equal(t, strings.Repeat("a", 500), lines[0])
	r.n.claim(steers[0].id)

	// 40 lines of 99 characters are 3,999 characters with the line ends.
	seen := 1
	var want []string
	for i := range 40 {
		line := fmt.Sprintf("%02d", i) + strings.Repeat("b", 97)
		want = append(want, line)
		seen++
		feed(t, f, j, line, seen)
	}
	r.clock.Advance(200 * time.Millisecond)
	steers = r.n.steers()
	require.Len(t, steers, 2)
	body := strings.Join(eventLines(steers[1].text), "\n")
	kept := strings.TrimSuffix(body, "\n[... more lines cut]")
	require.NotEqual(t, body, kept, "the cut marker ends the event")
	assert.Len(t, kept, 3000)
	assert.Equal(t, strings.Join(want, "\n")[:3000], kept)
}

func TestMonitorDefaultTimeoutApplies(t *testing.T) {
	r := newRig(t, withFakeClock())
	command, _ := r.gate("never")
	j := r.monitor(MonitorSpec{Command: command})

	r.clock.Advance(9*time.Hour + 59*time.Minute)
	assert.Equal(t, StatusRunning, j.Status())
	r.clock.Advance(time.Minute)
	waitDone(t, j)
	assert.Equal(t, "timeout", j.KillReason())
}

func TestMonitorDescriptionIsEscaped(t *testing.T) {
	r := newRig(t)
	j := r.monitor(MonitorSpec{Command: "echo x", Description: `a "b" <c>`})
	waitDone(t, j)
	waitFor(t, 5*time.Second, "steer", func() bool { return len(r.n.steers()) >= 1 })
	assert.Contains(t, r.n.steers()[0].text, `description="a &#34;b&#34; &lt;c&gt;"`)
}

func TestMonitorDeliversLongUnterminatedLine(t *testing.T) {
	r := newRig(t)
	j := r.monitor(MonitorSpec{Command: "printf '%s' '" + strings.Repeat("a", 2500) + "'"})
	waitDone(t, j)
	steers := r.n.steers()
	require.Len(t, steers, 1)
	assert.Equal(t, []string{strings.Repeat("a", monitorLineMax)}, eventLines(steers[0].text))
}

func TestMonitorCompletionWaitsForEventDelivery(t *testing.T) {
	r := newRig(t)
	delivering, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	owner := &gatedOwner{fakeNotifier: r.n, delivering: delivering, release: release}
	gate, open := r.gate("exit")
	j := r.monitor(MonitorSpec{Command: "echo line; " + gate, Owner: owner})
	select {
	case <-delivering:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor event delivery did not start")
	}
	open()
	select {
	case <-j.proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("monitor command did not end")
	}
	select {
	case <-j.Done():
		t.Error("monitor completed before its last event was delivered")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Empty(t, r.n.followUps(), "completion must follow the last event")
}

func TestMonitorBoundsPendingMessage(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true})
	for i := range 20 {
		feed(t, f, j, fmt.Sprintf("%02d", i)+strings.Repeat("界", 498), i+1)
		r.clock.Advance(2 * time.Second)
	}
	steers := r.n.steers()
	require.Len(t, steers, 20)
	body := strings.Join(eventLines(steers[len(steers)-1].text), "\n")
	assert.LessOrEqual(t, len([]rune(body)), monitorBatchMax+len([]rune(monitorCutMarker))+1)
	assert.Contains(t, body, monitorCutMarker)
	assert.Contains(t, body, "19"+strings.Repeat("界", 498))
	assert.Equal(t, 1, strings.Count(steers[len(steers)-1].text, "<monitor-event "))
}

func TestMonitorDropsDeliveryWhenClosedDuringMerge(t *testing.T) {
	r := newRig(t, withFakeClock())
	f := r.fifo()
	owner := &hookOwner{fakeNotifier: r.n}
	j := r.monitor(MonitorSpec{Command: f.command(), Persistent: true, Owner: owner})
	feed(t, f, j, "first", 1)
	r.clock.Advance(200 * time.Millisecond)
	merging, release := make(chan struct{}), make(chan struct{})
	owner.setRemove(func(id string) bool { close(merging); <-release; return r.n.Remove(id) })
	feed(t, f, j, "second", 2)
	flushed := make(chan struct{})
	go func() { defer close(flushed); r.clock.Advance(200 * time.Millisecond) }()
	<-merging
	r.tb.KillRemaining()
	close(release)
	<-flushed
	waitDone(t, j)
	assert.Len(t, r.n.steers(), 1, "closing must stop the merged delivery")
}

// gatedOwner holds the first monitor event until release closes.
type gatedOwner struct {
	*fakeNotifier
	delivering chan struct{}
	release    <-chan struct{}
	once       sync.Once
}

func (g *gatedOwner) Notify(m protocol.Message) (string, error) {
	if strings.Contains(messageText(m), "<monitor-event") {
		g.once.Do(func() { close(g.delivering) })
		<-g.release
	}
	return g.fakeNotifier.Notify(m)
}

// hookOwner lets a test replace Remove after the job started.
type hookOwner struct {
	*fakeNotifier
	mu     sync.Mutex
	remove func(id string) bool
}

func (h *hookOwner) setRemove(f func(id string) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove = f
}

func (h *hookOwner) Remove(id string) bool {
	h.mu.Lock()
	f := h.remove
	h.mu.Unlock()
	if f != nil {
		return f(id)
	}
	return h.fakeNotifier.Remove(id)
}
