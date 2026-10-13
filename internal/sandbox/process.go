//go:build unix

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	// readChunk is the size of one read from a pipe.
	readChunk = 32 * 1024
	// killGrace is the time between SIGTERM and SIGKILL to the group.
	killGrace = 3 * time.Second
	// drainBound is the time the readers get after the main process ended. A
	// grandchild that holds a pipe open cannot block the process longer.
	drainBound = 3 * time.Second
	// groupPollLimit and groupPollEvery bound the wait for the group to vanish
	// after SIGKILL.
	groupPollLimit = time.Second
	groupPollEvery = 10 * time.Millisecond
)

// Stream names the pipe a chunk of output came from.
type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
)

func (s Stream) String() string {
	if s == StreamStderr {
		return "stderr"
	}
	return "stdout"
}

// Spec describes one command.
type Spec struct {
	// Argv is run as is; ShellArgv builds one for a shell command line.
	Argv []string
	// Dir is the working directory. It is required, so that a command never
	// runs in the working directory of the daemon.
	Dir string
	// Env is the whole environment of the command. A nil Env means
	// ChildEnv(os.Environ(), nil), never the unfiltered environment.
	Env []string
	// Clock times the kill grace and the drain bound. Nil means SystemClock.
	Clock Clock
	// SpillFile creates the file that receives the whole output once it
	// passes the memory limit. The file must have mode 0600. Nil means no
	// spill file. The process closes the file; the caller removes it.
	SpillFile func() (*os.File, error)
	// Observe gets every chunk after it entered the buffer, in buffer order,
	// one call at a time. It must not block and must copy a chunk it keeps.
	Observe func(stream Stream, chunk []byte)

	// spillLimit replaces defaultSpillLimit in tests.
	spillLimit int64
}

// Result is the end state of a process.
type Result struct {
	// ExitCode is nil when a signal ended the process.
	ExitCode *int
	// Signal is the name of the signal that ended the process, such as
	// "SIGKILL"; it is empty after a normal exit.
	Signal string
	// Err is an error from waiting for the process or for its group.
	Err error
}

// Process is one running command and its process group. It is safe for
// concurrent use. Done closes when the main process ended, its output is read
// and its group is gone; no signal is sent after that.
type Process struct {
	cmd   *exec.Cmd
	pgid  int
	clock Clock

	rOut, rErr *os.File
	readers    sync.WaitGroup

	buf     *buffer
	spill   *spill
	observe func(Stream, []byte)
	writeMu sync.Mutex // orders spill write, buffer write and Observe

	mu         sync.Mutex // guards the fields below
	terminated bool       // SIGTERM sent
	killStop   func() bool
	gone       bool
	result     Result

	done chan struct{}
}

// Start runs spec in a new process group and returns at once.
func Start(spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("sandbox: empty argv")
	}
	if spec.Dir == "" {
		return nil, errors.New("sandbox: working directory is required")
	}
	env := spec.Env
	if env == nil {
		env = ChildEnv(os.Environ(), nil)
	}
	clock := spec.Clock
	if clock == nil {
		clock = SystemClock
	}
	limit := spec.spillLimit
	if limit <= 0 {
		limit = defaultSpillLimit
	}

	rOut, wOut, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("sandbox: stdout pipe: %w", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		_ = rOut.Close()
		_ = wOut.Close()
		return nil, fmt.Errorf("sandbox: stderr pipe: %w", err)
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = env
	cmd.Stdout = wOut
	cmd.Stderr = wErr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	startErr := cmd.Start()
	// The child has its own copy of the write ends. Closing ours makes the
	// readers see end of file when the last holder of a pipe exits, and lets
	// Wait return without waiting for them.
	_ = wOut.Close()
	_ = wErr.Close()
	if startErr != nil {
		_ = rOut.Close()
		_ = rErr.Close()
		return nil, fmt.Errorf("sandbox: start %s: %w", spec.Argv[0], startErr)
	}

	p := &Process{
		cmd:     cmd,
		pgid:    cmd.Process.Pid,
		clock:   clock,
		rOut:    rOut,
		rErr:    rErr,
		buf:     &buffer{},
		spill:   &spill{open: spec.SpillFile, limit: limit},
		observe: spec.Observe,
		done:    make(chan struct{}),
	}
	p.readers.Add(2)
	go p.readLoop(StreamStdout, rOut)
	go p.readLoop(StreamStderr, rErr)
	go p.wait()
	return p, nil
}

// PGID returns the process group id, which is the pid of the main process.
func (p *Process) PGID() int { return p.pgid }

// Done returns a channel that closes when the process has ended and its group
// is gone.
func (p *Process) Done() <-chan struct{} { return p.done }

// Result returns the end state. It is complete after Done closed.
func (p *Process) Result() Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}

// Snapshot returns a copy of the output so far.
func (p *Process) Snapshot() Output {
	head, tail, total := p.buf.snapshot()
	path, capped, err := p.spill.state()
	return Output{
		Head:        head,
		Tail:        tail,
		Total:       total,
		Dropped:     total - int64(len(head)) - int64(len(tail)),
		SpillPath:   path,
		SpillCapped: capped,
		SpillErr:    err,
	}
}

// ReadFrom returns the output bytes from the byte offset cursor that memory
// still holds, the cursor for the next call, and the count of bytes that are
// no longer in memory (the spill file has them when one exists).
func (p *Process) ReadFrom(cursor int64) (data []byte, next, skipped int64) {
	return p.buf.readFrom(cursor)
}

// Kill sends SIGTERM to the group and arms a timer that sends SIGKILL after
// the grace. The timer stays armed when the main process exits, so a
// grandchild that ignores SIGTERM is still killed. A second call does nothing.
func (p *Process) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gone || p.terminated {
		return
	}
	p.terminated = true
	_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
	p.killStop = p.clock.AfterFunc(killGrace, p.KillNow)
}

// KillNow sends SIGKILL to the group and does not wait. Wait for Done to know
// that the group is gone.
func (p *Process) KillNow() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gone {
		return
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
}

// readLoop moves one pipe into the buffer until the pipe ends or is closed.
func (p *Process) readLoop(stream Stream, r *os.File) {
	defer p.readers.Done()
	defer func() { _ = r.Close() }()
	chunk := make([]byte, readChunk)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			p.ingest(stream, chunk[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *Process) ingest(stream Stream, chunk []byte) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.spill.start(p.buf, len(chunk))
	p.buf.write(chunk)
	p.spill.append(chunk)
	if p.observe != nil {
		p.observe(stream, chunk)
	}
}

// wait runs in its own goroutine: it reaps the main process, bounds the pipe
// drain, makes sure the group is gone, and then closes done.
func (p *Process) wait() {
	waitErr := p.cmd.Wait()
	p.drain()
	p.spill.close()
	groupErr := p.sweepGroup()

	res := Result{}
	if state := p.cmd.ProcessState; state != nil {
		if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			res.Signal = signalName(ws.Signal())
		} else {
			code := state.ExitCode()
			res.ExitCode = &code
		}
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		res.Err = waitErr
	}
	if res.Err == nil {
		res.Err = groupErr
	}

	p.mu.Lock()
	p.result = res
	p.gone = true
	if p.killStop != nil {
		p.killStop()
	}
	p.mu.Unlock()
	close(p.done)
}

// drain waits for both readers. After drainBound it kills the group and closes
// the read ends, which ends a read that a grandchild keeps blocked.
func (p *Process) drain() {
	readersDone := make(chan struct{})
	go func() {
		p.readers.Wait()
		close(readersDone)
	}()
	expired := make(chan struct{}, 1)
	stop := p.clock.AfterFunc(drainBound, func() { expired <- struct{}{} })
	select {
	case <-readersDone:
		stop()
	case <-expired:
		p.KillNow()
		_ = p.rOut.Close()
		_ = p.rErr.Close()
		<-readersDone
	}
}

// sweepGroup returns when no process of the group exists. Wait returning does
// not prove that: a background child of the command can outlive it.
func (p *Process) sweepGroup() error {
	if p.groupGone() {
		return nil
	}
	p.KillNow()
	deadline := time.Now().Add(groupPollLimit)
	for time.Now().Before(deadline) {
		time.Sleep(groupPollEvery)
		if p.groupGone() {
			return nil
		}
	}
	return fmt.Errorf("sandbox: process group %d still exists after SIGKILL", p.pgid)
}

func (p *Process) groupGone() bool {
	return errors.Is(syscall.Kill(-p.pgid, 0), syscall.ESRCH)
}

var signalNames = map[syscall.Signal]string{
	syscall.SIGHUP:  "SIGHUP",
	syscall.SIGINT:  "SIGINT",
	syscall.SIGQUIT: "SIGQUIT",
	syscall.SIGABRT: "SIGABRT",
	syscall.SIGKILL: "SIGKILL",
	syscall.SIGSEGV: "SIGSEGV",
	syscall.SIGPIPE: "SIGPIPE",
	syscall.SIGALRM: "SIGALRM",
	syscall.SIGTERM: "SIGTERM",
}

func signalName(sig syscall.Signal) string {
	if name, ok := signalNames[sig]; ok {
		return name
	}
	return fmt.Sprintf("signal %d", int(sig))
}
