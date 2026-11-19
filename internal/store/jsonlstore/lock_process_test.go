package jsonlstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

const (
	envMode = "ASK_JSONL_HELPER_MODE"
	envRoot = "ASK_JSONL_HELPER_ROOT"
	envCwd  = "ASK_JSONL_HELPER_CWD"
	envID   = "ASK_JSONL_HELPER_ID"
)

// TestSessionHelperProcess is the body of the child processes of this file. It
// does nothing in a normal test run.
func TestSessionHelperProcess(t *testing.T) {
	mode := os.Getenv(envMode)
	if mode == "" {
		return
	}
	s, err := New(os.Getenv(envRoot))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx := context.Background()
	cwd, id := os.Getenv(envCwd), os.Getenv(envID)
	switch mode {
	case "hold":
		w, err := s.Open(ctx, cwd, id)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		// A long-running child of the holder starts after the session is open.
		// It outlives the holder, so it must not keep the lock.
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		fmt.Printf("LOCKED %d\n", child.Process.Pid)
		time.Sleep(10 * time.Minute)
		// The files of w close when the collector finds w unused, which would
		// drop the lock while the holder still lives.
		runtime.KeepAlive(w)
	case "open":
		_, err := s.Open(ctx, cwd, id)
		switch {
		case err == nil:
			fmt.Println("OPENED")
		case errors.Is(err, store.ErrSessionOwned):
			fmt.Println("OWNED " + err.Error())
		default:
			fmt.Println("ERROR " + err.Error())
		}
	case "write":
		// Wait for the parent, so that two children start together.
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			os.Exit(5)
		}
		w, err := s.Create(ctx, testHeader(cwd, sessions.NewID()), store.CreateLazy)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		_, err = w.Append(sessions.MessageEntry{Message: userText("hi")}, sessions.MessageEntry{Message: assistantText("yo")})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(7)
		}
		fmt.Println("ID " + w.ID())
		if err := w.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(8)
		}
	}
	os.Exit(0)
}

func helper(root, mode, cwd, id string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSessionHelperProcess$")
	cmd.Env = append(os.Environ(), envMode+"="+mode, envRoot+"="+root, envCwd+"="+cwd, envID+"="+id)
	return cmd
}

// startHolder starts a child that holds the session and returns the pid of the
// long-running grandchild that it started.
func startHolder(t *testing.T, root, cwd, id string) (*exec.Cmd, int) {
	t.Helper()
	cmd := helper(root, "hold", cwd, id)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err, "holder said nothing")
	var pid int
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "LOCKED ")
	require.True(t, ok, "holder line %q", line)
	pid, err = strconv.Atoi(rest)
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return cmd, pid
}

func TestHeldSessionRefusesSecondProcess(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	s := mustNew(t, root)
	id := seedSession(t, s, cwd)
	startHolder(t, root, cwd, id)

	begin := time.Now()
	out, err := helper(root, "open", cwd, id).Output()
	require.NoError(t, err)
	text := strings.TrimSpace(string(out))
	assert.True(t, strings.HasPrefix(text, "OWNED "), "child said %q", text)
	assert.Contains(t, text, id)
	assert.Contains(t, text, s.lockPath(cwd, id))
	assert.Less(t, time.Since(begin), 5*time.Second)

	_, err = s.Open(context.Background(), cwd, id)
	assert.True(t, errors.Is(err, store.ErrSessionOwned), "got %v", err)
}

func TestKilledHolderReleasesSessionAtOnce(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	s := mustNew(t, root)
	id := seedSession(t, s, cwd)
	holder, grandchild := startHolder(t, root, cwd, id)

	_, err := s.Open(context.Background(), cwd, id)
	require.True(t, errors.Is(err, store.ErrSessionOwned), "got %v", err)

	require.NoError(t, holder.Process.Kill())
	_ = holder.Wait()
	require.NoError(t, syscall.Kill(grandchild, 0), "the grandchild must still run")

	begin := time.Now()
	w, err := s.Open(context.Background(), cwd, id)
	require.NoError(t, err, "a descriptor leaked into the grandchild keeps the lock")
	assert.Less(t, time.Since(begin), time.Second)
	require.NoError(t, w.Close())
}

func collectIDs(t *testing.T, root, cwd string, n int) []string {
	t.Helper()
	cmds := make([]*exec.Cmd, n)
	ins := make([]io.WriteCloser, n)
	outs := make([]*strings.Builder, n)
	for i := range cmds {
		cmds[i] = helper(root, "write", cwd, "")
		var err error
		ins[i], err = cmds[i].StdinPipe()
		require.NoError(t, err)
		outs[i] = &strings.Builder{}
		cmds[i].Stdout = outs[i]
		var stderr strings.Builder
		cmds[i].Stderr = &stderr
		require.NoError(t, cmds[i].Start())
	}
	for _, in := range ins {
		_, err := io.WriteString(in, "go\n")
		require.NoError(t, err)
		require.NoError(t, in.Close())
	}
	ids := make([]string, n)
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "child %d: %s", i, cmd.Stderr.(*strings.Builder).String())
		id, ok := strings.CutPrefix(strings.TrimSpace(outs[i].String()), "ID ")
		require.True(t, ok, "child %d said %q", i, outs[i].String())
		ids[i] = id
	}
	return ids
}

func assertTwoSessions(t *testing.T, root, cwd string, ids []string) {
	t.Helper()
	require.Len(t, ids, 2)
	assert.NotEqual(t, ids[0], ids[1])
	s := mustNew(t, root)
	list, err := s.List(context.Background(), cwd)
	require.NoError(t, err)
	assert.Len(t, list, 2)
	for _, id := range ids {
		_, entries, err := s.Read(context.Background(), cwd, id)
		require.NoError(t, err)
		assert.Len(t, entries, 2)
	}
}

func TestTwoProcessesWriteTwoSessions(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	assertTwoSessions(t, root, cwd, collectIDs(t, root, cwd, 2))
}

func TestTwoProcessesCreateFirstSessionOfFreshHome(t *testing.T) {
	root, cwd := filepath.Join(t.TempDir(), "fresh", "home"), t.TempDir()
	_, err := os.Stat(root)
	require.True(t, errors.Is(err, os.ErrNotExist))
	assertTwoSessions(t, root, cwd, collectIDs(t, root, cwd, 2))
}
