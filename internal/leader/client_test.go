package leader

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// procLeader runs leaders in this process, one at a time, the way `ask leader`
// runs them in their own process: it takes the lifetime lock, replaces a stale
// socket, serves, and on the end of the server it removes its socket and
// releases the lock.
type procLeader struct {
	t       *testing.T
	starts  atomic.Int32
	servers atomic.Int32
	delay   time.Duration
	// server edits the config of each leader that wins the lock.
	server func(*Config)
	mu     sync.Mutex
	srvs   []*Server
	closed bool // set by the cleanup: a leader that starts later must not stay
	wg     sync.WaitGroup
}

func newProcLeader(t *testing.T) *procLeader {
	pl := &procLeader{t: t}
	t.Cleanup(func() {
		pl.mu.Lock()
		pl.closed = true
		list := append([]*Server(nil), pl.srvs...)
		pl.mu.Unlock()
		for _, s := range list {
			_ = s.Close()
		}
		pl.wg.Wait()
	})
	return pl
}

func (pl *procLeader) isClosed() bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.closed
}

func socketServing(p Paths) bool {
	c, err := net.DialTimeout("unix", p.Socket, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// start is a StartFn. It returns at once, as a process start does.
func (pl *procLeader) start(cfg StartConfig) (*Child, error) {
	pl.starts.Add(1)
	child := &Child{exited: make(chan struct{})}
	pl.wg.Add(1)
	go func() {
		defer pl.wg.Done()
		defer close(child.exited)
		time.Sleep(pl.delay)
		if pl.isClosed() {
			return // the test ended while this start was still waiting
		}
		lock, err := AcquireWait(cfg.Paths, time.Second, func() bool { return socketServing(cfg.Paths) })
		if err != nil {
			return // a loser: the winner serves
		}
		defer func() { _ = lock.Release() }()
		if _, err := lock.RemoveStaleSocket(); err != nil {
			child.err = err
			return
		}
		fa, end := newFakeAgent(pl.t)
		conf := Config{
			Server: ServerConfig{InstanceID: "inst-" + time.Now().Format("150405.000000"), Build: "dev", RegisterTimeout: 2 * time.Second},
			Agent:  end, LinkTimeout: 2 * time.Second, SpawnedByClient: true,
			QuiesceIfIdle: func() bool { return true },
		}
		if pl.server != nil {
			pl.server(&conf)
		}
		srv, err := NewServer(conf)
		if err != nil || srv.Start() != nil {
			child.err = errors.New("leader did not start")
			return
		}
		ln, err := net.Listen("unix", cfg.Paths.Socket)
		if err != nil {
			child.err = err
			_ = srv.Close()
			return
		}
		pl.mu.Lock()
		if pl.closed {
			// The cleanup already took its list. This server would never be closed.
			pl.mu.Unlock()
			_ = srv.Close()
			_ = ln.Close()
			_ = os.Remove(cfg.Paths.Socket)
			return
		}
		pl.servers.Add(1)
		pl.srvs = append(pl.srvs, srv)
		pl.mu.Unlock()
		serving := make(chan struct{})
		go func() { defer close(serving); _ = srv.Serve(ln) }()
		<-srv.Done()
		_ = srv.Close()
		_ = ln.Close()
		<-serving
		<-fa.done
		_ = os.Remove(cfg.Paths.Socket)
	}()
	return child, nil
}

func connectConfig(p Paths) ConnectConfig {
	return ConnectConfig{
		Paths:    p,
		Hello:    protocol.LeaderRegister{ClientKind: "test", ProtocolVersion: protocol.LeaderProtocolVersion, Build: "dev"},
		Deadline: 10 * time.Second,
	}
}

// askDir returns a directory with an `ask` that reports the current protocol.
func askDir(t *testing.T) string { return askDirProto(t, protocol.LeaderProtocolVersion) }

func askDirProto(t *testing.T, proto int) string {
	dir := t.TempDir()
	fakeAsk(t, dir, versionScript("dev", proto))
	return dir
}

func TestConnectDoesNotSpawn(t *testing.T) {
	p := testPaths(t)
	_, err := Connect(context.Background(), connectConfig(p))
	require.ErrorIs(t, err, ErrLeaderAbsent)
	require.NoFileExists(t, p.Socket)
}

func TestConnectToRunningLeader(t *testing.T) {
	h := startHarness(t)
	c, err := Connect(context.Background(), connectConfig(h.paths))
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, "inst-1", c.Info.InstanceID)
}

func TestConnectOrSpawnStartsOneLeaderForTwoClients(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	pl.delay = 50 * time.Millisecond
	askBin := filepath.Join(askDir(t), "ask")
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := connectConfig(p)
			cfg.CallerExe = askBin
			cfg.StartFn = pl.start
			c, err := ConnectOrSpawn(context.Background(), cfg)
			if err != nil {
				t.Errorf("connect: %v", err)
				return
			}
			defer func() { _ = c.Close() }()
			ids <- c.Info.InstanceID
		}()
	}
	wg.Wait()
	close(ids)
	var got []string
	for id := range ids {
		got = append(got, id)
	}
	require.Len(t, got, 2)
	require.Equal(t, got[0], got[1], "both clients adopt the winner")
	require.Equal(t, int32(1), pl.servers.Load(), "one lifetime lock owner")
	require.GreaterOrEqual(t, pl.starts.Load(), int32(1))
}

func TestConnectOrSpawnPassesTheFoundBinary(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	var exe string
	start := func(cfg StartConfig) (*Child, error) {
		exe = cfg.Executable
		return pl.start(cfg)
	}
	sibDir := askDir(t)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(sibDir, "ask-server")
	cfg.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	cfg.StartFn = start
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
	require.Equal(t, filepath.Join(sibDir, "ask"), exe, "the sibling of the caller is started")
}

func TestConnectOrSpawnAskOnPathOnly(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	pathDir := askDir(t)
	var exe string
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(t.TempDir(), "ask-server") // another folder, no ask there
	cfg.LookPath = func(string) (string, error) { return filepath.Join(pathDir, "ask"), nil }
	cfg.StartFn = func(sc StartConfig) (*Child, error) { exe = sc.Executable; return pl.start(sc) }
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
	require.Equal(t, filepath.Join(pathDir, "ask"), exe)
}

func TestConnectOrSpawnAskMissing(t *testing.T) {
	p := testPaths(t)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(t.TempDir(), "ask-server")
	cfg.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrAskNotFound)
	require.Contains(t, err.Error(), "PATH")
}

func TestConnectOrSpawnIncompatibleAsk(t *testing.T) {
	p := testPaths(t)
	dir := t.TempDir()
	fakeAsk(t, dir, versionScript("old", 99))
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(dir, "ask-server")
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrAskIncompatible)
}

func TestConnectOrSpawnLiveLockNoSocketIsBounded(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p) // a leader that is starting, or hung: it holds the lock and has no socket
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.Deadline = 300 * time.Millisecond
	cfg.StartFn = func(StartConfig) (*Child, error) {
		t.Error("must not start a second leader")
		return nil, errors.New("no")
	}
	start := time.Now()
	_, err = ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestConnectOrSpawnStartupFailureShowsTheLog(t *testing.T) {
	p := testPaths(t)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.StartFn = func(sc StartConfig) (*Child, error) {
		require.NoError(t, os.WriteFile(sc.Paths.Log, []byte("ask leader: cannot open the agent\n"), 0600))
		ch := &Child{exited: make(chan struct{}), err: errors.New("exit status 1")}
		close(ch.exited)
		return ch, nil
	}
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrStartupFailed)
	require.Contains(t, err.Error(), "cannot open the agent")
}

func TestConnectOrSpawnAdoptsAWinnerThatStartsLate(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	pl.delay = 200 * time.Millisecond
	// A first start wins the lock after a delay. The child of this client exits at
	// once, as a loser does, and the client keeps waiting for the winner.
	_, err := pl.start(StartConfig{Paths: p})
	require.NoError(t, err)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.StartFn = func(StartConfig) (*Child, error) {
		ch := &Child{exited: make(chan struct{})}
		close(ch.exited)
		return ch, nil
	}
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
}

func TestConnectOrSpawnNoSpawnForUnsafeHome(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))
	p, err := ResolvePaths(filepath.Join(link, "home"))
	require.NoError(t, err)
	cfg := connectConfig(p)
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err = ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrUnsafeEndpoint)
}

func TestConnectOrSpawnNoSpawnForOtherUserEndpoint(t *testing.T) {
	h := startHarness(t, harnessOpts{edit: func(c *Config) {
		other := uint32(os.Geteuid()) + 1
		c.Server.OwnerUID = &other
	}})
	cfg := connectConfig(h.paths)
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrUnsafeEndpoint)
}

func TestConnectOrSpawnNoSpawnForMalformedPeer(t *testing.T) {
	p := testPaths(t)
	ln, err := net.Listen("unix", p.Socket)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
				_ = c.Close()
			}()
		}
	}()
	cfg := connectConfig(p)
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err = ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrMalformedPeer)
}

func TestConnectBuildHintOnce(t *testing.T) {
	h := startHarness(t)
	var hints []string
	cfg := connectConfig(h.paths)
	cfg.Hello.Build = "other-build"
	cfg.Warn = func(s string) { hints = append(hints, s) }
	c, err := Connect(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
	require.Len(t, hints, 1)
	require.Contains(t, hints[0], "other-build")
	require.Contains(t, hints[0], "ask leader stop")

	cfg.Hello.Build = "dev"
	hints = nil
	c, err = Connect(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
	require.Empty(t, hints, "the same build gives no hint")
}

// ---- version mismatch ----

func olderLeader(t *testing.T, edit func(*Config)) *harness {
	return startHarness(t, harnessOpts{edit: func(c *Config) {
		c.Server.ProtocolVersion = 1
		if edit != nil {
			edit(c)
		}
	}})
}

func newerClient(p Paths) ConnectConfig {
	cfg := connectConfig(p)
	cfg.Hello.ProtocolVersion = 2 // this client is newer than the leader
	return cfg
}

func TestVersionMismatchOlderClientIsRefusedWithoutReplacement(t *testing.T) {
	h := startHarness(t, harnessOpts{edit: func(c *Config) {
		c.Server.ProtocolVersion = 5
		c.SpawnedByClient = true
		c.QuiesceIfIdle = func() bool { t.Error("an older client must not stop a newer leader"); return true }
	}})
	cfg := connectConfig(h.paths)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.StartFn = func(StartConfig) (*Child, error) { t.Error("must not start"); return nil, errors.New("no") }
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrVersionRefused)
	require.Contains(t, err.Error(), "upgrade the client")
	select {
	case <-h.srv.Done():
		t.Fatal("the leader stopped")
	default:
	}
}

func TestReplaceOnlyIdleSpawnedLeader(t *testing.T) {
	cases := map[string]struct {
		spawned bool
		idle    bool
		hint    string
	}{
		"supervised":   {spawned: false, idle: true, hint: "supervisor"},
		"busy":         {spawned: true, idle: false, hint: "busy"},
		"idle spawned": {spawned: true, idle: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := olderLeader(t, func(c *Config) {
				c.SpawnedByClient = tc.spawned
				c.QuiesceIfIdle = func() bool { return tc.idle }
			})
			pl := newProcLeader(t)
			pl.server = func(c *Config) { c.Server.ProtocolVersion = 2 } // the new leader speaks the new version
			cfg := newerClient(h.paths)
			cfg.CallerExe = filepath.Join(askDirProto(t, 2), "ask")
			cfg.StartFn = pl.start
			c, err := ConnectOrSpawn(context.Background(), cfg)
			if tc.spawned && tc.idle {
				require.NoError(t, err)
				defer func() { _ = c.Close() }()
				require.Equal(t, 2, c.Info.ProtocolVersion, "the idle leader was replaced")
				require.Equal(t, int32(1), pl.starts.Load())
				return
			}
			require.ErrorIs(t, err, ErrVersionRefused)
			require.Contains(t, err.Error(), tc.hint)
			require.Zero(t, pl.starts.Load(), "a kept leader is never joined by a competing one")
			select {
			case <-h.srv.Done():
				t.Fatal("a leader that must stay was stopped")
			default:
			}
		})
	}
}

func TestReplacementWaitsForTheLockToBeReleased(t *testing.T) {
	// The old leader holds the lifetime lock like a real one and releases it after it stops.
	p := testPaths(t)
	old := newProcLeader(t)
	old.server = func(c *Config) { c.Server.ProtocolVersion = 1 }
	cfg0 := connectConfig(p)
	cfg0.CallerExe = filepath.Join(askDir(t), "ask")
	cfg0.StartFn = old.start
	c0, err := ConnectOrSpawn(context.Background(), cfg0)
	require.NoError(t, err)
	_ = c0.Close()

	fresh := newProcLeader(t)
	fresh.server = func(c *Config) { c.Server.ProtocolVersion = 2 }
	cfg := newerClient(p)
	cfg.CallerExe = filepath.Join(askDirProto(t, 2), "ask")
	cfg.StartFn = fresh.start
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, 2, c.Info.ProtocolVersion)
	require.Equal(t, int32(1), old.servers.Load())
	require.Equal(t, int32(1), fresh.servers.Load())
}

// ---- status and stop ----

func TestStatusOfRunningLeader(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.StartFn = pl.start
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	st, err := Status(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 2, st.Clients, "the open client and the status call, which registers like any client")
	require.True(t, st.SpawnedByClient)
	require.Equal(t, protocol.LeaderProtocolVersion, st.ProtocolVersion)
}

func TestStatusWithoutLeader(t *testing.T) {
	_, err := Status(context.Background(), connectConfig(testPaths(t)))
	require.ErrorIs(t, err, ErrLeaderAbsent)
}

func TestStatusAcrossVersions(t *testing.T) {
	h := olderLeader(t, nil)
	st, err := Status(context.Background(), newerClient(h.paths))
	require.NoError(t, err)
	require.Equal(t, "inst-1", st.InstanceID)
}

func TestStopByControl(t *testing.T) {
	p := testPaths(t)
	pl := newProcLeader(t)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.StartFn = pl.start
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()

	res, err := Stop(context.Background(), cfg, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, StopResult{WasRunning: true}, res)
	held, _ := LockHeld(p)
	require.False(t, held)
	require.False(t, socketAnswers(p))
	require.FileExists(t, p.Lock, "the lock file stays")
}

func TestStopWithoutLeader(t *testing.T) {
	res, err := Stop(context.Background(), connectConfig(testPaths(t)), time.Second)
	require.NoError(t, err)
	require.Equal(t, StopResult{}, res)
}

func TestStopRefusesUnverifiedPID(t *testing.T) {
	p := testPaths(t)
	sleeper := exec.Command("sleep", "30")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })

	// A lock that is held, a socket that does not answer, and an owner record that
	// names a live process which is not `ask leader`.
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	require.NoError(t, l.WriteOwner(Owner{PID: sleeper.Process.Pid, Start: time.Now().Unix(), Instance: "x"}))

	cfg := connectConfig(p)
	cfg.Deadline = 3 * time.Second
	_, err = Stop(context.Background(), cfg, 200*time.Millisecond)
	require.ErrorIs(t, err, ErrNotLeaderProcess)
	require.NoError(t, syscall.Kill(sleeper.Process.Pid, 0), "the unrelated process was not signalled")
}

func TestStopIgnoresAStalePIDInTheLockFile(t *testing.T) {
	p := testPaths(t)
	sleeper := exec.Command("sleep", "30")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	require.NoError(t, EnsureHome(p.Home))
	require.NoError(t, os.WriteFile(p.Lock, []byte(fmt.Sprintf("%d\n%d\ninst\n", sleeper.Process.Pid, time.Now().Unix())), 0600))

	res, err := Stop(context.Background(), connectConfig(p), time.Second)
	require.NoError(t, err)
	require.False(t, res.WasRunning, "nobody holds the lock, so the PID is stale")
	require.NoError(t, syscall.Kill(sleeper.Process.Pid, 0), "the process of a stale PID was not signalled")
}

func TestStopRefusedByABusyLeaderDoesNotSignal(t *testing.T) {
	h := olderLeader(t, func(c *Config) {
		c.SpawnedByClient = true
		c.QuiesceIfIdle = func() bool { return false }
	})
	_, err := Stop(context.Background(), newerClient(h.paths), time.Second)
	require.ErrorIs(t, err, ErrVersionRefused)
	select {
	case <-h.srv.Done():
		t.Fatal("a busy leader of another version was stopped")
	default:
	}
}

// A process that is `ask leader` by its command line, that handles SIGTERM, and
// that the lock file names: Stop reaches it through the verified signal when no
// socket answers.
func TestStopSignalsAVerifiedLeaderProcess(t *testing.T) {
	p := testPaths(t)
	dir := t.TempDir()
	marker, ready := filepath.Join(dir, "got-term"), filepath.Join(dir, "ready")
	ask := fakeAsk(t, dir, fmt.Sprintf(`trap 'echo yes > %s; exit 0' TERM; echo ok > %s; while true; do sleep 0.05; done`, marker, ready))
	proc := exec.Command(ask, "leader", "--spawned-by-client")
	require.NoError(t, proc.Start())
	exited := make(chan struct{})
	go func() { _ = proc.Wait(); close(exited) }()
	t.Cleanup(func() { _ = proc.Process.Kill(); <-exited })

	// The test holds the lock for the fake leader and gives it back when the fake leader leaves.
	l, err := Acquire(p)
	require.NoError(t, err)
	require.NoError(t, l.WriteOwner(Owner{PID: proc.Process.Pid, Start: time.Now().Unix(), Instance: "fake"}))
	go func() { <-exited; _ = l.Release() }()
	require.Eventually(t, func() bool { return VerifyLeaderProcess(proc.Process.Pid, time.Now().Unix()) == nil }, 5*time.Second, 20*time.Millisecond,
		"a shell script named ask that runs `leader` is accepted")

	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 20*time.Millisecond, "the script handles SIGTERM")

	cfg := connectConfig(p)
	cfg.Deadline = 20 * time.Second
	res, err := Stop(context.Background(), cfg, 5*time.Second)
	if runtime.GOOS != "linux" {
		require.ErrorIs(t, err, ErrNotLeaderProcess)
		require.Equal(t, StopResult{WasRunning: true}, res)
		_, markerErr := os.Stat(marker)
		require.ErrorIs(t, markerErr, os.ErrNotExist, "fallback must not signal without a stable handle")
		return
	}
	require.NoError(t, err)
	require.Equal(t, StopResult{WasRunning: true, Signalled: true}, res)
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "yes\n", string(data), "the process got SIGTERM")
}

func TestStopWithoutHomeIsNotRunning(t *testing.T) {
	p, err := ResolvePaths(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	held, err := LockHeld(p)
	require.NoError(t, err)
	require.False(t, held)
	result, err := Stop(context.Background(), connectConfig(p), time.Second)
	require.NoError(t, err)
	require.Equal(t, StopResult{}, result)
}
