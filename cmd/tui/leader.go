package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"go.uber.org/fx"

	"AskCore/internal/app"
	"AskCore/internal/leader"
	"AskCore/internal/logs"
	"AskCore/pkg/protocol"
)

const leaderUsage = `ask leader - the local router that holds one agent for all local clients

Usage:
  ask leader                     run the leader in the foreground
  ask leader status [--json]     show identity and counts of the running leader
  ask leader list [--json]       list the leaders of this home (one)
  ask leader stop                stop the running leader
  ask leader --help

The leader listens on <ASK_HOME>/leader.sock (default ~/.ask), with mode 0600 in a
directory of mode 0700, and accepts only the user that owns that directory. Clients
start it on demand. "ask leader" started by hand keeps the terminal; one that a
client started has --spawned-by-client and writes to <ASK_HOME>/leader.log.
status, list and stop never start a leader. stop sends a shutdown request first and
falls back to SIGTERM on Linux only through a stable process handle that it
verified as ask leader. Other platforms require socket shutdown.
Exit codes: 0 stopped or nothing to do, 1 error, 130 SIGINT, 143 SIGTERM, 129 SIGHUP.
A second signal ends the process before the cleanup finishes.
`

// acquireWait is how long a starting leader waits for the lifetime lock while a
// client probes it for an instant.
const acquireWait = 2 * time.Second

// leaderLogMax is the size at which a leader that a client started rotates its log.
const leaderLogMax = 8 << 20

// leaderStopWait bounds how long ask leader stop waits for each step.
const leaderStopWait = 15 * time.Second

type leaderOptions struct {
	sub     string // "", "status", "list" or "stop"
	spawned bool
	json    bool
	help    bool
}

func parseLeaderArgs(argv []string) (leaderOptions, error) {
	var o leaderOptions
	for _, a := range argv {
		switch a {
		case "--help", "-h":
			o.help = true
		case "--spawned-by-client":
			o.spawned = true
		case "--json":
			o.json = true
		case "status", "list", "stop":
			if o.sub != "" {
				return o, errors.New("one subcommand only; use ask leader --help")
			}
			o.sub = a
		default:
			return o, fmt.Errorf("unknown argument %q; use ask leader --help", a)
		}
	}
	switch {
	case o.sub != "" && o.spawned:
		return o, errors.New("--spawned-by-client belongs to the foreground leader only")
	case o.json && o.sub != "status" && o.sub != "list":
		return o, errors.New("--json belongs to status and list")
	}
	return o, nil
}

// runLeader implements "ask leader".
func runLeader(argv []string, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	o, err := parseLeaderArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.help {
		_, _ = io.WriteString(stdout, leaderUsage)
		return 0
	}
	paths, err := leader.ResolvePaths(deps.getenv("ASK_HOME"))
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	switch o.sub {
	case "status":
		return leaderStatus(paths, o.json, stdout, stderr)
	case "list":
		return leaderList(paths, o.json, stdout, stderr)
	case "stop":
		return leaderStop(paths, stdout, stderr)
	}
	return serveLeader(o, paths, stderr, deps, sigs)
}

// leaderConnect is the configuration of the commands that talk to a leader.
func leaderConnect(paths leader.Paths) leader.ConnectConfig {
	exe, _ := os.Executable()
	return leader.ConnectConfig{
		Paths:     paths,
		Hello:     protocol.LeaderRegister{ClientKind: "cli", Build: buildIdentity()},
		CallerExe: exe,
		Deadline:  leaderStopWait * 3,
	}
}

func leaderStatus(paths leader.Paths, asJSON bool, stdout, stderr io.Writer) int {
	st, err := leader.Status(context.Background(), leaderConnect(paths))
	if errors.Is(err, leader.ErrLeaderAbsent) {
		report(stderr, "no leader is running")
		return 1
	}
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if asJSON {
		data, _ := json.Marshal(st)
		_, _ = fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "leader:            running\ninstance:          %s\npid:               %d\nbuild:             %s\nprotocol:          %d\nclients:           %d\nsessions:          %d\nactive runs:       %d\nspawned by client: %s\n",
		st.InstanceID, st.PID, st.Build, st.ProtocolVersion, st.Clients, st.Sessions, st.ActiveRuns, yesNo(st.SpawnedByClient))
	return 0
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// leaderList lists the leaders of this home. There is one endpoint, so there is
// at most one row. It never starts a leader.
func leaderList(paths leader.Paths, asJSON bool, stdout, stderr io.Writer) int {
	st, err := leader.Status(context.Background(), leaderConnect(paths))
	rows := []protocol.LeaderStatus{}
	switch {
	case err == nil:
		rows = append(rows, st)
	case !errors.Is(err, leader.ErrLeaderAbsent):
		report(stderr, "Error:", err)
		return 1
	}
	if asJSON {
		data, _ := json.Marshal(rows)
		_, _ = fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(stdout, "no leader is running")
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "%-18s %-8s %-14s %-9s %-8s %-9s %-5s %s\n", "INSTANCE", "PID", "BUILD", "PROTOCOL", "CLIENTS", "SESSIONS", "RUNS", "SPAWNED")
	for _, r := range rows {
		_, _ = fmt.Fprintf(stdout, "%-18s %-8d %-14s %-9d %-8d %-9d %-5d %s\n", r.InstanceID, r.PID, r.Build, r.ProtocolVersion, r.Clients, r.Sessions, r.ActiveRuns, yesNo(r.SpawnedByClient))
	}
	return 0
}

func leaderStop(paths leader.Paths, stdout, stderr io.Writer) int {
	res, err := leader.Stop(context.Background(), leaderConnect(paths), leaderStopWait)
	switch {
	case err != nil:
		report(stderr, "Error:", err)
		return 1
	case !res.WasRunning:
		_, _ = fmt.Fprintln(stdout, "no leader is running")
	case res.Signalled:
		_, _ = fmt.Fprintln(stdout, "leader stopped with SIGTERM")
	default:
		_, _ = fmt.Fprintln(stdout, "leader stopped")
	}
	return 0
}

// serveLeader runs the leader until it is told to stop. The order matters:
// lock, stale socket, socket with mode 0600, owner record, serve, then on the
// way out the socket (only if it is still ours) and last the lock.
func serveLeader(o leaderOptions, paths leader.Paths, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	started := time.Now()
	if err := leader.EnsureHome(paths.Home); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if err := leader.CheckSocketPath(paths.Socket); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	lock, err := leader.AcquireWait(paths, acquireWait, func() bool { return leader.Serving(paths) })
	if errors.Is(err, leader.ErrLeaderRunning) {
		if leader.Serving(paths) {
			report(stderr, "ask leader: a leader already runs; using it")
			return 0
		}
		report(stderr, "Error: another process holds the leader lock and does not answer")
		return 1
	}
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	// Last to run: the lock goes only after the socket is gone.
	defer func() { _ = lock.Release() }()

	if o.spawned {
		// This process won the lock, so it owns the log. It starts a fresh one when
		// the old one is large, and sends its error output there.
		if _, err := leader.RotateLog(paths, leaderLogMax); err != nil {
			report(stderr, "ask leader: log rotation:", err)
		}
		if f, err := leader.OpenLog(paths); err == nil {
			if err := leader.RedirectStderr(f); err == nil {
				stderr = f
			}
		}
	}
	if _, err := lock.RemoveStaleSocket(); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	instance := newInstanceID()
	acp, err := acpParams(deps)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	level := deps.getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}
	logger, err := logs.New(level)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	params := app.LeaderParams{
		ACPParams: acp,
		Server: leader.ServerConfig{
			InstanceID: instance, Build: buildIdentity(), Controls: []string{protocol.LeaderControlStatus, protocol.LeaderControlShutdown},
		},
		PID: os.Getpid(), SpawnedByClient: o.spawned, Logger: logger,
	}
	var rt *app.LeaderRuntime
	graph := fx.New(app.LeaderModule, fx.Supply(params), fx.NopLogger, fx.Populate(&rt))
	if err := graph.Err(); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	return serveLeaderRuntime(rt, lock, paths, leader.Owner{PID: os.Getpid(), Start: started.Unix(), Instance: instance}, stderr, sigs)
}

// serveLeaderRuntime owns startup cleanup until Serve takes over. A second
// signal can then return without waiting for the runtime's active drain.
func serveLeaderRuntime(rt *app.LeaderRuntime, lock *leader.Lock, paths leader.Paths, owner leader.Owner, stderr io.Writer, sigs <-chan os.Signal) int {
	serving := false
	defer func() {
		if !serving {
			_ = rt.Stop(context.Background())
		}
	}()
	ln, ident, err := listenPrivate(paths.Socket)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	defer func() {
		if err := lock.RemoveSocket(ident); err != nil {
			report(stderr, "ask leader: socket cleanup:", err)
		}
	}()
	if err := lock.WriteOwner(owner); err != nil {
		_ = ln.Close()
		report(stderr, "Error:", err)
		return 1
	}
	report(stderr, "ask leader: listening on", paths.Socket)
	serving = true
	return runUntilSignal(sigs, stderr, func(ctx context.Context) error { return rt.Serve(ctx, ln) })
}

// runUntilSignal runs serve until it ends by itself or a signal asks it to stop.
// The first signal cancels the context, so serve stops in order. A second signal
// ends the process before the cleanup has drained: it says so and does not
// report success. The result is the exit code.
func runUntilSignal(sigs <-chan os.Signal, stderr io.Writer, serve func(ctx context.Context) error) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var first os.Signal
	signaled := make(chan os.Signal, 1)
	forced := make(chan struct{})
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case s := <-sigs:
			signaled <- s
			cancel()
		case <-finished:
			return
		}
		select {
		case <-sigs:
			close(forced)
		case <-finished:
		}
	}()
	done := make(chan error, 1)
	go func() { done <- serve(ctx) }()
	var err error
	select {
	case err = <-done:
	case <-forced:
		report(stderr, "ask leader: cleanup did not drain; forced exit")
		first = <-signaled
		return signalExitCodes[first]
	}
	select {
	case first = <-signaled:
	default:
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		report(stderr, "ask leader:", err)
	}
	switch {
	case first != nil:
		return signalExitCodes[first]
	case err != nil:
		return 1
	}
	return 0
}

// listenPrivate listens on a Unix socket with mode 0600. It returns the file
// info of the socket, so cleanup can tell that the path still holds this socket.
func listenPrivate(path string) (net.Listener, os.FileInfo, error) {
	old := syscall.Umask(0o177) // no window in which another user can reach the socket
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		_ = ln.Close()
		return nil, nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	return ln, info, nil
}

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("pid%d", os.Getpid())
	}
	return strings.ToLower(hex.EncodeToString(b))
}
