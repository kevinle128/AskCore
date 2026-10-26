package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/jobs"
	"AskCore/pkg/protocol"
)

const (
	// printBgLimitEnv overrides the default background limit of print mode,
	// in milliseconds. Tests use it for a short real limit.
	printBgLimitEnv = "ASK_PRINT_BG_LIMIT_MS"
	// printBgCeilingEnv is the idle-wait cap of print mode in milliseconds;
	// 0 waits without one.
	printBgCeilingEnv = "ASK_PRINT_BG_WAIT_CEILING_MS"
	// disableBackgroundEnv turns background work off in every mode.
	disableBackgroundEnv = "ASK_DISABLE_BACKGROUND_TASKS"

	printBgDefault = 10 * time.Minute
	printBgMax     = 2 * time.Hour
	printCeiling   = 10 * time.Minute
	// progressAfter is the wait after which print mode names the background
	// work it waits for.
	progressAfter = 5 * time.Second
)

var (
	errMaxTurns     = errors.New("reached max turns")
	errCeilingEnded = errors.New("idle-wait cap reached")
)

// printBackgroundLimit is the background limit of print mode: 10 min by
// default, at most 2 h, as Claude Code's -p. A larger value of the env
// override also raises the maximum.
func printBackgroundLimit(getenv func(string) string) jobs.BackgroundLimit {
	l := jobs.BackgroundLimit{Default: printBgDefault, Max: printBgMax}
	if ms, err := strconv.ParseInt(getenv(printBgLimitEnv), 10, 64); err == nil && ms > 0 {
		l.Default = time.Duration(ms) * time.Millisecond
		l.Max = max(l.Max, l.Default)
	}
	return l
}

// printWaitCeiling is the idle-wait cap of print mode.
func printWaitCeiling(getenv func(string) string) time.Duration {
	v := getenv(printBgCeilingEnv)
	if v == "" {
		return printCeiling
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil || ms < 0 {
		return printCeiling
	}
	return time.Duration(ms) * time.Millisecond
}

// waitConfig is what print mode does after its last prompt. The zero value
// waits for nothing, has no turn limit and prints no progress line.
type waitConfig struct {
	// jobs is the job table to wait for. Nil means no background wait.
	jobs *jobs.Table
	// maxTurns ends the run after that many turns; 0 means no limit.
	maxTurns int
	// ceiling ends a wait in which the Agent stays idle and no bash job
	// runs; 0 means no cap.
	ceiling time.Duration
	// progress gets the waiting line; nil prints none.
	progress io.Writer
}

// replyPrinter writes the reply of the last prompt and then the reply of each
// run that a background notice wakes, as Claude Code's text output does. It
// reads the messages at agent_settled, which the run emits before it ends, so
// each reply is read from the run that produced it.
type replyPrinter struct {
	ag          *agent.Agent
	out, stderr io.Writer
	// prompts is the number of prompts; the run of the last one gives the
	// first reply.
	prompts int
	// armed is cleared by disarm without the lock, so a signal never waits
	// for a reply that blocks on a full stdout.
	armed atomic.Bool

	mu      sync.Mutex
	started int
	lastRun string
	first   bool // the reply of the last prompt was printed
	seen    int
	code    int
}

func newReplyPrinter(ag *agent.Agent, out, stderr io.Writer, prompts int) *replyPrinter {
	p := &replyPrinter{ag: ag, out: out, stderr: stderr, prompts: prompts}
	p.armed.Store(true)
	return p
}

// promptRun records the run of a prompt; agent.WithRunStart calls it before
// any event of that run.
func (p *replyPrinter) promptRun(runID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started++
	if p.started == p.prompts {
		p.lastRun = runID
	}
}

// settled writes the reply of the run that emitted agent_settled: the reply of
// the last prompt, then the reply of each run after it. Runs before the last
// prompt print nothing, as before the wait existed.
func (p *replyPrinter) settled(runID string) {
	if !p.armed.Load() {
		return
	}
	msgs := p.ag.State().Messages
	p.mu.Lock()
	var part []protocol.Message
	switch {
	case !p.first:
		if p.lastRun == "" || runID != p.lastRun {
			p.mu.Unlock()
			return
		}
		part, p.first = msgs, true
	case len(msgs) > p.seen:
		part = msgs[p.seen:]
	default:
		p.mu.Unlock()
		return
	}
	p.seen = len(msgs)
	p.mu.Unlock()

	// The write runs without the lock: it can block on a full stdout.
	code := printReply(part, p.out, p.stderr)
	p.mu.Lock()
	p.code = code
	p.mu.Unlock()
}

// disarm stops the printing of later runs: a signal or --max-turns ended the
// run, and an aborted run must not print an error.
func (p *replyPrinter) disarm() { p.armed.Store(false) }

func (p *replyPrinter) exitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

// turnLimit stops a run before turn max+1 starts. A retried attempt ends a
// wire turn too; its auto_retry_start takes it back, so only durable turns
// count.
type turnLimit struct {
	max   int
	ended int
	hit   bool
	stop  func()
}

func (l *turnLimit) observe(ev protocol.Event) {
	if l.max <= 0 || l.hit {
		return
	}
	switch ev.(type) {
	case *protocol.TurnEnd:
		l.ended++
	case *protocol.AutoRetryStart:
		l.ended--
	case *protocol.TurnStart:
		if l.ended >= l.max {
			l.hit = true
			l.stop()
		}
	}
}

// idleCeiling ends a wait in which the Agent has been idle for the whole cap
// while no bash job runs: only watches or other work are left, as Claude
// Code's -p does. The timer runs while the Agent is idle and stops when a run
// starts; it never polls.
type idleCeiling struct {
	after  time.Duration
	ag     *agent.Agent
	jobs   *jobs.Table
	cancel context.CancelCauseFunc
	done   <-chan struct{}

	mu    sync.Mutex
	timer *time.Timer
}

func (c *idleCeiling) arm() {
	if c.after <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(c.after, c.fire)
}

func (c *idleCeiling) disarm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (c *idleCeiling) fire() {
	if idle := c.ag.Idle(); !isClosedSignal(idle) {
		// A run or an event dispatch is in progress, so the Agent was not
		// idle for the whole cap. The cap starts again when it is idle.
		go func() {
			select {
			case <-idle:
				c.arm()
			case <-c.done:
			}
		}()
		return
	}
	for _, j := range c.jobs.Active() {
		if j.Kind == jobs.KindBash {
			// A background command keeps the wait open; its end wakes a run,
			// and the cap starts again when that run settles.
			return
		}
	}
	c.cancel(errCeilingEnded)
}

// waitBackground waits by signal until the Agent, its tasks and its jobs are
// quiet. The Agent answers each notice in a run of its own meanwhile.
func waitBackground(ctx context.Context, ag *agent.Agent, w waitConfig) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	ceil := &idleCeiling{after: w.ceiling, ag: ag, jobs: w.jobs, cancel: cancel, done: ctx.Done()}
	defer ceil.disarm()
	unsubscribe := ag.Subscribe(func(ev protocol.Event) error {
		switch ev.(type) {
		case *protocol.AgentStart:
			ceil.disarm()
		case *protocol.AgentSettled:
			ceil.arm()
		}
		return nil
	})
	defer unsubscribe()
	// The cap checks at expiry that the Agent is idle, so it can start now.
	ceil.arm()
	if w.progress != nil {
		progress := time.AfterFunc(progressAfter, func() { writeProgress(w.progress, w.jobs) })
		defer progress.Stop()
	}
	err := ag.WaitForQuiescence(ctx)
	if errors.Is(context.Cause(ctx), errCeilingEnded) {
		return nil
	}
	return err
}

// writeProgress names the background work that print mode waits for.
func writeProgress(out io.Writer, table *jobs.Table) {
	active := table.Active()
	if len(active) == 0 {
		return
	}
	names := make([]string, len(active))
	for i, j := range active {
		names[i] = fmt.Sprintf("%s (%s)", j.ID, j.Label)
	}
	_, _ = fmt.Fprintf(out, "Waiting for background work to finish: %s\n", strings.Join(names, ", "))
}

func isClosedSignal(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
