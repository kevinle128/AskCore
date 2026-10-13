package leader

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The test binary starts itself as `<binary> leader`. That process holds the
// lifetime lock and writes an owner record like a real leader, but it listens
// on no socket, so it is a leader that lost its socket, or one that drains.
const (
	helperEnv      = "ASK_LEADER_TEST_HELPER"
	helperHomeEnv  = "ASK_LEADER_TEST_HOME"
	helperStateEnv = "ASK_LEADER_TEST_STATE"
)

// Helper modes: what the helper does on SIGTERM.
const (
	helperDefault     = "default" // it ends, as the default of a Go program
	helperIgnoreTerm  = "ignore"  // it ignores SIGTERM
	helperStopsOnTerm = "stops"   // it records the state stopping and goes on
)

// helperLock keeps the lock reachable, so that the garbage collector does not
// close its file and release the lock.
var helperLock *Lock

// runHelperLeader is the body of the helper process.
func runHelperLeader() {
	p, err := ResolvePaths(os.Getenv(helperHomeEnv))
	if err != nil {
		os.Exit(2)
	}
	lock, err := Acquire(p)
	if err != nil {
		os.Exit(3)
	}
	helperLock = lock
	exe, _ := os.Executable()
	err = lock.WriteOwner(Owner{PID: os.Getpid(), Start: time.Now().Unix(), Instance: "helper", Exe: exe, State: os.Getenv(helperStateEnv)})
	if err != nil {
		os.Exit(4)
	}
	switch os.Getenv(helperEnv) {
	case helperIgnoreTerm:
		signal.Ignore(syscall.SIGTERM)
	case helperStopsOnTerm:
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		go func() {
			<-ch
			_ = lock.MarkStopping()
		}()
	}
	time.Sleep(time.Hour)
}

// helperLeader is a running helper process.
type helperLeader struct {
	cmd    *exec.Cmd
	done   chan struct{}
	status syscall.WaitStatus // valid after done is closed
}

func (h *helperLeader) alive() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// startHelperLeader starts a helper that holds the lock of p with the given
// owner state, and waits until its record is written.
func startHelperLeader(t *testing.T, p Paths, state, mode string) *helperLeader {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "leader")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, helperHomeEnv+"="+p.Home, helperStateEnv+"="+state)
	require.NoError(t, cmd.Start())
	h := &helperLeader{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		h.status = cmd.ProcessState.Sys().(syscall.WaitStatus)
		close(h.done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-h.done
	})
	require.Eventually(t, func() bool {
		o, err := ReadOwner(p)
		return err == nil && o.PID == cmd.Process.Pid
	}, 10*time.Second, 10*time.Millisecond, "the helper writes its owner record")
	return h
}
