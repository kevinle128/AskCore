//go:build unix

package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"AskCore/internal/sandbox"
)

const (
	defaultMaxActive = 10
	defaultKillGrace = 3 * time.Second
	// labelMax is the length of the label of a job, in characters.
	labelMax = 80
	// killByModelReason names the kill when the model gave no reason.
	killByModelReason = "job_kill"
	limitReason       = "background time limit"
	closeReason       = "session closed"
	cancelReason      = "cancelled"
)

var (
	// ErrClosed is returned by a start after the table was closed.
	ErrClosed = errors.New("jobs: table is closed")
	// ErrTooManyJobs is returned by a start when the cap of active jobs is
	// reached. The error text tells the model what to do.
	ErrTooManyJobs = errors.New("Too many active jobs") //nolint:staticcheck // the text is shown to the model
	// ErrUnknownJob is returned for an id that the table does not hold: it
	// never existed, or the model already saw the end of the job.
	ErrUnknownJob = errors.New("jobs: unknown job")
	// ErrNoNotifier is reported through Options.OnError when a notice or an
	// event is ready and the job has no owner.
	ErrNoNotifier = errors.New("jobs: job has no owner")
	// ErrBackgroundDisabled is returned by a start of background work when
	// Options.DisableBackground is set.
	ErrBackgroundDisabled = errors.New("background jobs are turned off in this session")
)

// Kind tells what a job is.
type Kind string

const (
	KindBash    Kind = "bash"
	KindMonitor Kind = "monitor"
)

// Status is the state of a job.
type Status string

const (
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	// StatusCompleted means the command exited; its exit code can be non-zero.
	StatusCompleted Status = "completed"
	// StatusKilled means the table, the model or a limit stopped the job.
	StatusKilled Status = "killed"
	// StatusFailed means a signal that the table did not send ended the
	// command, or waiting for it failed.
	StatusFailed Status = "failed"
)

// WaitOutcome tells how Wait returned.
type WaitOutcome int

const (
	// Done: the job ended inside the budget. The table removed it.
	Done WaitOutcome = iota + 1
	// Background: the budget passed; the job keeps running.
	Background
	// Cancelled: the context ended; the table killed the job and removed it.
	Cancelled
	// NotFound: the table holds no job with the id.
	NotFound
)

func (o WaitOutcome) String() string {
	switch o {
	case Done:
		return "done"
	case Background:
		return "background"
	case Cancelled:
		return "cancelled"
	case NotFound:
		return "not found"
	}
	return fmt.Sprintf("WaitOutcome(%d)", int(o))
}

// Options configures a Table. The zero value is valid.
type Options struct {
	// Clock times the budgets, the monitor timers and, through sandbox, the
	// kill grace of each process. Nil means sandbox.SystemClock.
	Clock sandbox.Clock
	// Env returns the whole environment of a new job. Nil means
	// sandbox.ChildEnv(os.Environ(), nil). It runs outside the table lock.
	Env func() []string
	// MaxActive is the cap of running and stopping jobs. Zero means 10.
	MaxActive int
	// KillGrace is the time that Close gives the jobs after SIGTERM before it
	// sends SIGKILL to what is left. Zero means 3 s.
	KillGrace time.Duration
	// OnError gets the errors that have no caller: a refused notice, a job
	// with no owner. Nil drops them.
	OnError func(error)
	// IsExpectedDrop tells a refusal that is part of the contract, such as a
	// notice of a conversation that was reset. Such a refusal is not reported
	// and mutes the job. Nil treats every refusal as an error.
	IsExpectedDrop func(error) bool
	// BackgroundLimit stops a job that runs in the background for too long.
	// The zero value means no limit, as in an interactive session.
	BackgroundLimit BackgroundLimit
	// DisableBackground turns background work off: a foreground command stops
	// at its wait budget, and a background start or a monitor is refused.
	DisableBackground bool
}

// BackgroundLimit is the time a job may run in the background. It counts from
// the start of a background job, or from the move of a foreground job.
type BackgroundLimit struct {
	// Default applies when the start asks for no limit and to a moved job.
	// Zero turns limits off.
	Default time.Duration
	// Max caps a limit that a start asks for. Zero means no cap.
	Max time.Duration
}

// of returns the limit for a request, zero when limits are off.
func (l BackgroundLimit) of(requested time.Duration) time.Duration {
	if l.Default <= 0 {
		return 0
	}
	d := requested
	if d <= 0 {
		d = l.Default
	}
	if l.Max > 0 && d > l.Max {
		d = l.Max
	}
	return d
}

// StartSpec describes a bash job.
type StartSpec struct {
	// Kind is KindBash or empty; use StartMonitor for a monitor.
	Kind Kind
	// Command is a shell command line.
	Command string
	// Cwd is the working directory of the command.
	Cwd string
	// Foreground tells that the caller calls Wait for this job next. The job
	// starts with that wait registered, so a command that ends before the call
	// is reported by Wait and gets no notice. A wait that gives up turns the job
	// into a background job with a notice.
	Foreground bool
	// BackgroundLimit asks for a limit for a background job; zero takes the
	// default of Options.BackgroundLimit, and Max caps it. It is ignored for a
	// foreground job, which gets the default when it moves.
	BackgroundLimit time.Duration
	// Owner is the conversation that starts the job. Its notices go there,
	// and only that conversation sees the job.
	Owner Owner
}

// JobInfo is a snapshot of one job for job_list.
type JobInfo struct {
	ID         string
	Kind       Kind
	Status     Status
	Label      string
	Elapsed    time.Duration
	ExitCode   *int
	Signal     string
	KillReason string
}

// Job is one process of the table. A Job value stays valid after the table
// removed the job, so that the caller can read the output and the result.
type Job struct {
	t       *Table
	id      string
	kind    Kind
	label   string
	seq     int
	started time.Time
	proc    *sandbox.Process
	done    chan struct{}
	mon     *monitor

	// The fields below are guarded by t.mu.
	status        Status
	ended         time.Time
	exitCode      *int
	signal        string
	failure       error
	killReason    string
	awaited       bool // the model saw the end: no notice
	noticeStarted bool // completion delivery was claimed under the table lock
	killedByModel bool // the model stopped the job: no notice, no events
	waiters       int
	held          bool // Start registered the first Wait as a waiter
	cursor        int64
	stopTimeout   func() bool
	muted         bool // the owner refused a notice as expected: no more events
	stopLimit     func() bool

	owner Owner  // set at start, never changed
	gen   uint64 // generation of owner
}

// ID returns bash-N or monitor-N.
func (j *Job) ID() string { return j.id }

// Kind returns the kind of the job.
func (j *Job) Kind() Kind { return j.kind }

// Label returns the first line of the command, cut to 80 characters.
func (j *Job) Label() string { return j.label }

// Done closes when the job ended and its process group is gone.
func (j *Job) Done() <-chan struct{} { return j.done }

// Status returns the current state.
func (j *Job) Status() Status {
	j.t.mu.Lock()
	defer j.t.mu.Unlock()
	return j.status
}

// KillReason returns why the table or the model stopped the job; it is empty
// for a job that nobody stopped.
func (j *Job) KillReason() string {
	j.t.mu.Lock()
	defer j.t.mu.Unlock()
	return j.killReason
}

// StatusLine returns the status as `[status: ...]`.
func (j *Job) StatusLine() string {
	j.t.mu.Lock()
	defer j.t.mu.Unlock()
	return j.statusLineLocked()
}

// Snapshot returns a copy of the output so far.
func (j *Job) Snapshot() sandbox.Output { return j.proc.Snapshot() }

// Result returns the end state of the process; it is complete after Done.
func (j *Job) Result() sandbox.Result { return j.proc.Result() }

// Info returns a snapshot of the job.
func (j *Job) Info() JobInfo {
	j.t.mu.Lock()
	defer j.t.mu.Unlock()
	return j.infoLocked()
}

func (j *Job) infoLocked() JobInfo {
	end := j.ended
	if end.IsZero() {
		end = j.t.clock.Now()
	}
	return JobInfo{
		ID:         j.id,
		Kind:       j.kind,
		Status:     j.status,
		Label:      j.label,
		Elapsed:    max(end.Sub(j.started), 0),
		ExitCode:   j.exitCode,
		Signal:     j.signal,
		KillReason: j.killReason,
	}
}

func (j *Job) finishedLocked() bool {
	return j.status != StatusRunning && j.status != StatusStopping
}

// Table is the job table of one session. It is safe for concurrent use.
type Table struct {
	clock     sandbox.Clock
	env       func() []string
	maxActive int
	killGrace time.Duration
	onError   func(error)
	expected  func(error) bool
	limit     BackgroundLimit
	noBg      bool

	mu       sync.Mutex // guards the fields below and the state of every Job
	jobs     map[string]*Job
	counters map[Kind]int
	seq      int
	closing  bool
	// busy counts the jobs whose notice is not handed off yet; idle is open
	// while it is above zero. See Idle.
	busy   int
	idle   chan struct{}
	owners sync.WaitGroup

	spillMu     sync.Mutex // never taken together with mu
	spillDir    string
	spillClosed bool

	closeOnce sync.Once
	closeErr  error
}

// New returns an empty table. It owns no file and no goroutine until a job
// starts.
func New(o Options) *Table {
	t := &Table{
		clock:     o.Clock,
		env:       o.Env,
		maxActive: o.MaxActive,
		killGrace: o.KillGrace,
		onError:   o.OnError,
		expected:  o.IsExpectedDrop,
		limit:     o.BackgroundLimit,
		noBg:      o.DisableBackground,
		jobs:      make(map[string]*Job),
		counters:  make(map[Kind]int),
	}
	if t.clock == nil {
		t.clock = sandbox.SystemClock
	}
	if t.maxActive <= 0 {
		t.maxActive = defaultMaxActive
	}
	if t.killGrace <= 0 {
		t.killGrace = defaultKillGrace
	}
	return t
}

func (t *Table) report(err error) {
	if t.onError != nil && err != nil {
		t.onError(err)
	}
}

// Start runs spec as a background-capable bash job. The process belongs to
// the table, not to the context of the call that starts it.
func (t *Table) Start(spec StartSpec) (*Job, error) {
	if spec.Kind != "" && spec.Kind != KindBash {
		return nil, fmt.Errorf("jobs: Start runs bash jobs, not %q; use StartMonitor", spec.Kind)
	}
	if t.noBg && !spec.Foreground {
		return nil, ErrBackgroundDisabled
	}
	j, err := t.launch(KindBash, spec.Command, spec.Cwd, nil, spec.Foreground, spec.Owner)
	if err != nil {
		return nil, err
	}
	if !spec.Foreground {
		t.armLimit(j, t.limit.of(spec.BackgroundLimit))
	}
	return j, nil
}

// armLimit stops j after d unless it ends first. Zero arms nothing.
func (t *Table) armLimit(j *Job, d time.Duration) {
	if d <= 0 {
		return
	}
	stop := t.clock.AfterFunc(d, func() { t.killJob(j, limitReason, false) })
	t.mu.Lock()
	defer t.mu.Unlock()
	if j.finishedLocked() || j.stopLimit != nil {
		stop()
		return
	}
	j.stopLimit = stop
}

// Idle returns a channel that is closed while every job has handed off its
// completion notice or has none to send. A job counts from its start until
// then, so a quiescence check never sees the end of a job before its notice
// reached the Agent.
func (t *Table) Idle() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle == nil {
		return closedChan
	}
	return t.idle
}

var closedChan = func() chan struct{} { ch := make(chan struct{}); close(ch); return ch }()

// launch starts the process of a job and its owner goroutine.
func (t *Table) launch(kind Kind, command, cwd string, mon *monitor, held bool, owner Owner) (*Job, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("jobs: empty command")
	}
	env := t.childEnv()

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		return nil, ErrClosed
	}
	gen := genOf(owner)
	t.pruneLocked(gen)
	if active := t.activeLocked(); active >= t.maxActive {
		if older := t.activeOtherLocked(gen); older > 0 {
			return nil, fmt.Errorf("%w (%d): %d of them belong to an earlier conversation and end when the session closes.", ErrTooManyJobs, t.maxActive, older) //nolint:staticcheck // the text is shown to the model
		}
		return nil, fmt.Errorf("%w (%d). Stop one with job_kill or wait for one to finish.", ErrTooManyJobs, t.maxActive) //nolint:staticcheck // the text is shown to the model
	}
	id := fmt.Sprintf("%s-%d", kind, t.counters[kind]+1)
	j := &Job{
		t:       t,
		id:      id,
		kind:    kind,
		label:   labelOf(command),
		started: t.clock.Now(),
		done:    make(chan struct{}),
		status:  StatusRunning,
		mon:     mon,
		owner:   owner,
		gen:     gen,
	}
	if held {
		// The wait of the caller is registered before the process can end.
		j.held, j.waiters = true, 1
	}
	spec := sandbox.Spec{
		Argv:      sandbox.ShellArgv(command),
		Dir:       cwd,
		Env:       env,
		Clock:     t.clock,
		SpillFile: func() (*os.File, error) { return t.createSpill(id) },
	}
	if mon != nil {
		mon.bind(t, j)
		spec.Observe = mon.observe
	}
	// Starting a process is a fork and an exec; the lock keeps the cap and the
	// ids exact without a reservation step.
	p, err := sandbox.Start(spec)
	if err != nil {
		return nil, err
	}
	j.proc = p
	t.counters[kind]++
	t.seq++
	j.seq = t.seq
	t.jobs[id] = j
	t.busy++
	if t.idle == nil {
		t.idle = make(chan struct{})
	}
	t.owners.Add(1)
	go t.own(j)
	return j, nil
}

func (t *Table) childEnv() []string {
	if t.env != nil {
		return t.env()
	}
	return sandbox.ChildEnv(os.Environ(), nil)
}

func (t *Table) activeLocked() int {
	n := 0
	for _, j := range t.jobs {
		if !j.finishedLocked() {
			n++
		}
	}
	return n
}

// activeOtherLocked counts the active jobs of other generations.
func (t *Table) activeOtherLocked(gen uint64) int {
	n := 0
	for _, j := range t.jobs {
		if j.gen != gen && !j.finishedLocked() {
			n++
		}
	}
	return n
}

// pruneLocked removes the finished jobs of generations older than gen. Their
// conversation was reset, so nobody can read them any more. Generations only
// grow, so a newer caller is the proof.
func (t *Table) pruneLocked(gen uint64) {
	for id, j := range t.jobs {
		if j.gen < gen && j.finishedLocked() && j.waiters == 0 {
			j.awaited = true
			delete(t.jobs, id)
		}
	}
}

// lookupLocked returns the job id of generation gen. A job of another
// generation is unknown to the caller.
func (t *Table) lookupLocked(gen uint64, id string) (*Job, bool) {
	t.pruneLocked(gen)
	j, ok := t.jobs[id]
	if !ok || j.gen != gen {
		return nil, false
	}
	return j, true
}

// genOf returns the generation of owner, 0 when there is none.
func genOf(owner Owner) uint64 {
	if owner == nil {
		return 0
	}
	return owner.Gen()
}

// labelOf returns the first line of command, cut to labelMax characters.
func labelOf(command string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(command), "\n")
	return cutRunes(strings.TrimSpace(line), labelMax)
}

// cutRunes returns s cut to n characters.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// own waits for the end of the process of j, settles the final state, and
// sends the completion notice when nobody saw the end.
func (t *Table) own(j *Job) {
	defer t.owners.Done()
	<-j.proc.Done()
	if j.mon != nil {
		j.mon.finish()
	}
	res := j.proc.Result()

	t.mu.Lock()
	j.ended = t.clock.Now()
	j.exitCode, j.signal, j.failure = res.ExitCode, res.Signal, res.Err
	switch {
	case j.killReason != "":
		j.status = StatusKilled
	case res.ExitCode != nil && res.Err == nil:
		j.status = StatusCompleted
	default:
		j.status = StatusFailed
	}
	// A waiter that registered before this point reports the end itself.
	if j.waiters > 0 {
		j.awaited = true
	}
	stop, stopLimit := j.stopTimeout, j.stopLimit
	close(j.done)
	t.mu.Unlock()

	if stop != nil {
		stop()
	}
	if stopLimit != nil {
		stopLimit()
	}
	t.sendNotice(j)
	// The hand-off is done or was not needed: the job no longer counts.
	t.mu.Lock()
	t.busy--
	if t.busy == 0 && t.idle != nil {
		close(t.idle)
		t.idle = nil
	}
	t.mu.Unlock()
}

// killJob stops a running job. It does not wait. byModel suppresses the
// notice and the monitor events of the job.
func (t *Table) killJob(j *Job, reason string, byModel bool) {
	t.mu.Lock()
	if j.finishedLocked() {
		if byModel {
			j.awaited = true
		}
		t.mu.Unlock()
		return
	}
	if byModel {
		j.killedByModel = true
	}
	if j.killReason == "" {
		j.killReason = reason
	}
	j.status = StatusStopping
	t.mu.Unlock()
	j.proc.Kill()
}

// Wait waits for the end of the job for at most budget. A budget of zero or
// less returns Background at once. When ctx ends first, the job is killed as
// by the model and Wait returns after the process group is gone.
func (t *Table) Wait(ctx context.Context, id string, budget time.Duration) WaitOutcome {
	t.mu.Lock()
	j, ok := t.jobs[id]
	if !ok {
		t.mu.Unlock()
		return NotFound
	}
	// The first Wait of a foreground job takes over the waiter that Start registered.
	held := j.held
	j.held = false
	if j.finishedLocked() {
		if held {
			j.waiters--
		}
		t.takeLocked(j)
		t.mu.Unlock()
		return Done
	}
	if budget <= 0 {
		if held {
			j.waiters--
		}
		t.mu.Unlock()
		t.armLimit(j, t.limit.of(0))
		return Background
	}
	// A waiter that is registered when the job ends owns the report of the
	// end: the owner goroutine sets awaited under this lock.
	if !held {
		j.waiters++
	}
	t.mu.Unlock()

	expired := make(chan struct{})
	stop := t.clock.AfterFunc(budget, func() { close(expired) })
	defer stop()

	select {
	case <-j.done:
		return t.leaveWait(j)
	case <-expired:
		if t.noBg {
			// Background work is off: the command stops at its budget.
			t.killJob(j, timeoutReason, false)
			<-j.done
			return t.leaveWait(j)
		}
		t.mu.Lock()
		j.waiters--
		if j.finishedLocked() {
			// The end came with the budget; the owner already skipped the
			// notice, so this result is the only report.
			t.takeLocked(j)
			t.mu.Unlock()
			return Done
		}
		t.mu.Unlock()
		t.armLimit(j, t.limit.of(0))
		return Background
	case <-ctx.Done():
		t.killJob(j, cancelReason, true)
		<-j.done
		t.leaveWait(j)
		return Cancelled
	}
}

// leaveWait ends the registration of a waiter that saw the job end.
func (t *Table) leaveWait(j *Job) WaitOutcome {
	t.mu.Lock()
	defer t.mu.Unlock()
	j.waiters--
	t.takeLocked(j)
	return Done
}

// takeLocked marks the end of j as seen and removes it from the table. The
// spill file stays until Close because the result names it.
func (t *Table) takeLocked(j *Job) {
	j.awaited = true
	delete(t.jobs, j.id)
}

// Kill stops the job of generation gen as asked by the model, waits until the process group is
// gone and removes the job. No notice follows. The returned Job holds the
// final output and status. Killing a job that already ended only removes it.
func (t *Table) Kill(gen uint64, id, reason string) (*Job, error) {
	t.mu.Lock()
	j, ok := t.lookupLocked(gen, id)
	t.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownJob, id)
	}
	if reason == "" {
		reason = killByModelReason
	}
	t.killJob(j, reason, true)
	<-j.done
	t.mu.Lock()
	t.takeLocked(j)
	t.mu.Unlock()
	return j, nil
}

// List returns the jobs of generation gen, in start order: the active jobs
// and the finished jobs that nobody has read.
func (t *Table) List(gen uint64) []JobInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(gen)
	jobs := make([]*Job, 0, len(t.jobs))
	for _, j := range t.jobs {
		if j.gen == gen {
			jobs = append(jobs, j)
		}
	}
	slices.SortFunc(jobs, func(a, b *Job) int { return a.seq - b.seq })
	out := make([]JobInfo, len(jobs))
	for i, j := range jobs {
		out[i] = j.infoLocked()
	}
	return out
}

// Close kills every job and waits for the process groups to go: one SIGTERM to
// all at once, then SIGKILL to what is left after the grace, so the whole
// table closes in about the time of one job. It then removes the spill
// directory. No notice or event is sent. Close is idempotent; a second call
// returns after the first finished.
func (t *Table) Close() error {
	t.closeOnce.Do(func() { t.closeErr = t.close() })
	return t.closeErr
}

func (t *Table) close() error {
	live := t.stopAll(closeReason)
	for _, j := range live {
		j.proc.Kill()
	}
	stop := t.clock.AfterFunc(t.killGrace, func() {
		for _, j := range live {
			j.proc.KillNow()
		}
	})
	for _, j := range live {
		<-j.done
	}
	stop()
	t.owners.Wait()
	return t.removeSpillDir()
}

// KillRemaining sends SIGKILL to every live group, removes the spill directory,
// and returns without waiting.
// The table is closing from then on: no notice is sent and no job starts. It
// is the exit sweep of print mode, which cannot wait for the kill grace.
func (t *Table) KillRemaining() {
	for _, j := range t.stopAll(closeReason) {
		j.proc.KillNow()
	}
	t.report(t.removeSpillDir())
}

// stopAll sets the closing state and returns the jobs that were live.
func (t *Table) stopAll(reason string) []*Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closing = true
	var live []*Job
	for _, j := range t.jobs {
		if j.finishedLocked() {
			continue
		}
		if j.killReason == "" {
			j.killReason = reason
		}
		j.status = StatusStopping
		live = append(live, j)
	}
	return live
}

// createSpill makes the spill file of one job. It runs inside the write lock
// of the process, so it takes only the spill lock.
func (t *Table) createSpill(id string) (*os.File, error) {
	t.spillMu.Lock()
	defer t.spillMu.Unlock()
	if t.spillClosed {
		return nil, ErrClosed
	}
	if t.spillDir == "" {
		dir, err := os.MkdirTemp("", "ask-jobs-*")
		if err != nil {
			return nil, fmt.Errorf("jobs: spill directory: %w", err)
		}
		t.spillDir = dir
	}
	return os.OpenFile(filepath.Join(t.spillDir, id+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func (t *Table) removeSpillDir() error {
	t.spillMu.Lock()
	defer t.spillMu.Unlock()
	t.spillClosed = true
	if t.spillDir == "" {
		return nil
	}
	return os.RemoveAll(t.spillDir)
}

// spillDirPath returns the spill directory, or "" when no job spilled.
func (t *Table) spillDirPath() string {
	t.spillMu.Lock()
	defer t.spillMu.Unlock()
	return t.spillDir
}
