package leader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"AskCore/pkg/protocol"
)

var (
	// ErrLeaderAbsent means no leader listens and Connect does not start one.
	ErrLeaderAbsent = errors.New("leader: no leader is running")
	// ErrLeaderStarting means a leader holds the lock but did not answer in time.
	ErrLeaderStarting = errors.New("leader: a leader is starting or does not answer")
	// ErrUnsafeEndpoint means the home, the socket or the peer failed a safety check.
	ErrUnsafeEndpoint = errors.New("leader: the endpoint is not safe")
	// ErrMalformedPeer means the process on the socket is not a leader that speaks this protocol.
	ErrMalformedPeer = errors.New("leader: the peer is not a leader")
	// ErrStartupFailed means a leader process was started and ended without serving.
	ErrStartupFailed = errors.New("leader: the leader did not start")
	// ErrVersionRefused means the leader and the client speak different protocols
	// and the leader stays. The text names the side that must upgrade.
	ErrVersionRefused = errors.New("leader: protocol version mismatch")
)

const (
	defaultConnectDeadline = 20 * time.Second
	dialTimeout            = time.Second
	registerTimeout        = 3 * time.Second
	readyTimeout           = 5 * time.Second
	managementTimeout      = 3 * time.Second
	pollInterval           = 25 * time.Millisecond
	// maxStarts is the number of leader starts one call makes when each one ends
	// with an error before it serves.
	maxStarts = 3
)

// ConnectConfig says how to reach the leader of one Ask home.
type ConnectConfig struct {
	Paths Paths
	// Hello is the register frame. A zero ProtocolVersion means the current one.
	Hello protocol.LeaderRegister
	// Deadline bounds the whole call: connect, spawn, register, readiness and replacement.
	// Zero means 20 seconds.
	Deadline time.Duration
	// CallerExe is the executable of the caller. An `ask` next to it is preferred.
	CallerExe string
	// CallerIsAsk tells that CallerExe is the ask program itself. Then a leader
	// starts from CallerExe, so the leader matches the caller, whatever the name
	// of its binary (`go run` names it after the package). Other programs, such
	// as ask-server, leave it false and find `ask` next to them or on the PATH.
	CallerIsAsk bool
	// LookPath finds `ask` on the PATH. Nil means exec.LookPath.
	LookPath func(string) (string, error)
	// Env is the environment of a started leader. Nil means the current one.
	Env []string
	// Warn receives one-line hints, such as a different build. Nil discards them.
	Warn func(string)
	// StartFn starts the leader process. Nil means StartLeader. Tests replace it.
	StartFn func(StartConfig) (*Child, error)
	// ZombieAfter is how long a leader must have served before ConnectOrSpawn
	// stops it for holding the lock without a socket. Zero means 30 seconds.
	ZombieAfter time.Duration
	// EvictWait bounds each wait for such a leader to end after SIGTERM and after
	// SIGKILL. Zero means 8 seconds.
	EvictWait time.Duration
}

func (c ConnectConfig) withDefaults() ConnectConfig {
	if c.Hello.ProtocolVersion == 0 {
		c.Hello.ProtocolVersion = protocol.LeaderProtocolVersion
	}
	if c.Deadline == 0 {
		c.Deadline = defaultConnectDeadline
	}
	if c.LookPath == nil {
		c.LookPath = exec.LookPath
	}
	if c.Warn == nil {
		c.Warn = func(string) {}
	}
	if c.StartFn == nil {
		c.StartFn = StartLeader
	}
	if c.ZombieAfter == 0 {
		c.ZombieAfter = defaultZombieAfter
	}
	if c.EvictWait == 0 {
		c.EvictWait = defaultEvictWait
	}
	return c
}

// Client is a connection that completed the handshake.
type Client struct {
	*Registered
	Conn net.Conn
}

// Close sends a disconnect frame and closes the socket. The leader and the
// sessions of the client go on.
func (c *Client) Close() error {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(errorWriteTimeout))
	_ = c.Writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameDisconnect})
	return c.Conn.Close()
}

// Connect reaches a running leader. It never starts or replaces one.
func Connect(ctx context.Context, cfg ConnectConfig) (*Client, error) {
	return newConnector(cfg).run(ctx, false)
}

// ConnectOrSpawn reaches the leader of the home, or starts one and reaches it.
// It starts a leader only when none runs and none holds the lock. A safety
// failure or a peer that is not a leader never starts a process.
func ConnectOrSpawn(ctx context.Context, cfg ConnectConfig) (*Client, error) {
	return newConnector(cfg).run(ctx, true)
}

type connector struct {
	cfg    ConnectConfig
	hinted bool
	// exe is the binary to start, found and probed once for all starts of a call;
	// exeInfo is what its version command reported.
	exe     string
	exeInfo protocol.VersionInfo
	// starts counts the leader processes that this call started.
	starts int
	// handled holds the owner records that this call already tried to evict.
	handled map[Owner]bool
	// evictNote says what an eviction did, for the error at the deadline.
	evictNote string
}

func newConnector(cfg ConnectConfig) *connector { return &connector{cfg: cfg.withDefaults()} }

type outcome int

const (
	outOK      outcome = iota
	outAbsent          // nothing answers on the socket
	outLive            // something may be starting or stopping; try again
	outVersion         // the leader refused the protocol version
	outFatal           // do not start anything
)

func (c *connector) run(ctx context.Context, spawn bool) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Deadline)
	defer cancel()
	p := c.cfg.Paths
	if err := EnsureHome(p.Home); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, err)
	}
	if err := CheckSocketPath(p.Socket); err != nil {
		return nil, err
	}
	var child *Child
	var failed error // the error of the last leader start that ended before it served
	replaced := false
	for {
		cl, out, err := c.attempt(ctx)
		switch out {
		case outOK:
			// A leader of an older build that a client started gives way to a newer
			// client, also when it is busy. One replacement for each call.
			if spawn && !replaced && newerBuild(stampOfHello(c.cfg.Hello), stampOfRegistered(cl.Info)) {
				replaced = true
				done, rerr := c.replaceOlder(ctx, cl)
				if rerr != nil {
					return nil, rerr
				}
				if done {
					child = nil
					continue
				}
			}
			c.hintBuild(cl)
			return cl, nil
		case outFatal:
			return nil, err
		case outVersion:
			if !spawn {
				return nil, fmt.Errorf("%w: %v", ErrVersionRefused, err)
			}
			if replaced {
				return nil, err
			}
			if rerr := c.replace(ctx, err); rerr != nil {
				return nil, rerr
			}
			replaced = true
			child = nil
			continue
		case outAbsent:
			held, herr := LockHeld(p)
			if herr != nil {
				return nil, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, herr)
			}
			switch {
			case held:
				// A leader holds the lock: it is starting, or it does not answer. Wait.
				// A serving leader that lost its socket is stopped, where that is safe.
				if spawn {
					c.evictZombie(ctx)
				}
			case !spawn:
				return nil, ErrLeaderAbsent
			case child == nil:
				var serr error
				if child, serr = c.start(ctx); serr != nil {
					return nil, serr
				}
			default:
				select {
				case <-child.Exited():
					if child.Err() != nil {
						failed = child.Err()
						if c.starts >= maxStarts {
							return nil, c.startupFailure(failed)
						}
						// A leader that ends with an error before it serves is started again.
						child = nil
						break
					}
					// A child that exits cleanly lost the lock race. The winner serves soon.
				default:
				}
			}
		}
		select {
		case <-ctx.Done():
			if child != nil && child.Err() != nil {
				failed = child.Err()
			}
			if failed != nil {
				return nil, c.startupFailure(failed)
			}
			if c.evictNote != "" {
				return nil, fmt.Errorf("%w: %s", ErrLeaderStarting, c.evictNote)
			}
			return nil, fmt.Errorf("%w: waited %s", ErrLeaderStarting, c.cfg.Deadline)
		case <-time.After(pollInterval):
		}
	}
}

// prepare finds and probes the ask binary once for all starts of the call.
func (c *connector) prepare(ctx context.Context) error {
	if c.exe != "" {
		return nil
	}
	exe, err := c.leaderExecutable()
	if err != nil {
		return err
	}
	info, err := ProbeAsk(ctx, exe, c.cfg.Hello.ProtocolVersion)
	if err != nil {
		return err
	}
	c.exe, c.exeInfo = exe, info
	return nil
}

// start starts a leader from the probed ask binary.
func (c *connector) start(ctx context.Context) (*Child, error) {
	if err := c.prepare(ctx); err != nil {
		return nil, err
	}
	c.starts++
	return c.cfg.StartFn(StartConfig{Executable: c.exe, Paths: c.cfg.Paths, Env: c.cfg.Env})
}

// startupFailure describes a leader that was started and ended without serving.
func (c *connector) startupFailure(err error) error {
	return fmt.Errorf("%w: %v after %d starts; log: %s", ErrStartupFailed, err, c.starts, LogTail(c.cfg.Paths, 2048))
}

// leaderExecutable returns the binary to start a leader from. A caller that is
// ask starts the leader from its own binary while that file exists; a caller
// that exists but is not usable is an error, as for FindAsk. Otherwise ask is
// found next to the caller or on the PATH.
func (c *connector) leaderExecutable() (string, error) {
	if c.cfg.CallerIsAsk && c.cfg.CallerExe != "" {
		if _, err := os.Lstat(c.cfg.CallerExe); err == nil {
			return checkExecutable(c.cfg.CallerExe)
		}
	}
	return FindAsk(c.cfg.CallerExe, c.cfg.LookPath)
}

func (c *connector) hintBuild(cl *Client) {
	if c.hinted || c.cfg.Hello.Build == "" {
		return
	}
	switch {
	case cl.Info.Build != c.cfg.Hello.Build:
		c.hinted = true
		c.cfg.Warn(fmt.Sprintf("ask: the leader runs build %s and this client is build %s; run `ask leader stop` to restart the leader with this build", cl.Info.Build, c.cfg.Hello.Build))
	case otherDevBuild(stampOfHello(c.cfg.Hello), stampOfRegistered(cl.Info)):
		c.hinted = true
		c.cfg.Warn("ask: the leader runs another dev build; run `ask leader stop` to restart the leader with this build")
	}
}

// keep warns once why an older leader stays, and tells that the leader is kept.
func (c *connector) keep(msg string) {
	if c.hinted {
		return
	}
	c.hinted = true
	c.cfg.Warn("ask: the leader runs an older build; " + msg)
}

// attempt tries one connection and one handshake.
func (c *connector) attempt(ctx context.Context) (*Client, outcome, error) {
	// Linux answers ECONNREFUSED for a dial to a regular file, as it does for a
	// stale socket, so the dial cannot tell them apart. The lock holder refuses
	// such a path too, so refuse it here instead of starting leaders that fail.
	if info, lerr := os.Lstat(c.cfg.Paths.Socket); lerr == nil &&
		(info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(info)) {
		return nil, outFatal, fmt.Errorf("%w: %s is not a socket of this user", ErrUnsafeEndpoint, c.cfg.Paths.Socket)
	}
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "unix", c.cfg.Paths.Socket)
	if err != nil {
		switch {
		case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ECONNREFUSED):
			return nil, outAbsent, err
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			return nil, outFatal, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, err)
		case isTimeout(err) || ctx.Err() != nil:
			return nil, outLive, err
		}
		return nil, outFatal, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, err)
	}
	if err := CheckPeer(conn, uint32(os.Geteuid())); err != nil {
		_ = conn.Close()
		return nil, outFatal, fmt.Errorf("%w: the socket is served by another user", ErrUnsafeEndpoint)
	}
	reg, err := Register(conn, c.cfg.Hello, minDuration(registerTimeout, remaining(ctx)))
	if err != nil {
		_ = conn.Close()
		out, cerr := c.classify(err)
		return nil, out, cerr
	}
	if !reg.Info.Ready {
		if err := reg.AwaitReady(minDuration(readyTimeout, remaining(ctx))); err != nil {
			_ = conn.Close()
			return nil, outLive, err
		}
	}
	return &Client{Registered: reg, Conn: conn}, outOK, nil
}

// classify sorts a handshake failure.
func (c *connector) classify(err error) (outcome, error) {
	var remote *RemoteError
	switch {
	case errors.As(err, &remote):
		switch remote.Kind {
		case protocol.LeaderErrPeerRejected:
			return outFatal, fmt.Errorf("%w: the leader does not accept this user", ErrUnsafeEndpoint)
		case protocol.LeaderErrVersionMismatch:
			return outVersion, remote
		case protocol.LeaderErrShuttingDown:
			return outLive, err
		}
		return outFatal, fmt.Errorf("%w: %v", ErrMalformedPeer, err)
	case isTimeout(err), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return outLive, err
	}
	return outFatal, fmt.Errorf("%w: %v", ErrMalformedPeer, err)
}

// replace asks an old, idle leader that this client started to stop, and waits
// until its lock and socket are free. It never stops a leader that is busy,
// that runs under a supervisor, or that is newer than this client.
func (c *connector) replace(ctx context.Context, cause error) error {
	var remote *RemoteError
	if !errors.As(cause, &remote) {
		return cause
	}
	if remote.Upgrade != protocol.LeaderUpgradeLeader {
		return fmt.Errorf("%w: %s; upgrade the client (ask)", ErrVersionRefused, remote.Message)
	}
	frame, err := c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlStatus})
	if err != nil || frame.Type != protocol.LeaderFrameControlReply {
		return fmt.Errorf("%w: %s; the old leader could not be asked for its status", ErrVersionRefused, remote.Message)
	}
	var st protocol.LeaderStatus
	if json.Unmarshal(frame.Payload, &st) != nil {
		return fmt.Errorf("%w: %s", ErrVersionRefused, remote.Message)
	}
	if !st.SpawnedByClient {
		return fmt.Errorf("%w: %s; the leader runs under a supervisor, restart it with the new binary", ErrVersionRefused, remote.Message)
	}
	frame, err = c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true, InstanceID: st.InstanceID})
	if err != nil || frame.Type != protocol.LeaderFrameControlReply {
		return fmt.Errorf("%w: %s; the old leader is busy, run `ask leader stop` when its work is done", ErrVersionRefused, remote.Message)
	}
	return c.waitReleased(ctx)
}

// waitReleased waits until the lock and the socket are free, so that a new
// leader can start.
func (c *connector) waitReleased(ctx context.Context) error {
	for {
		held, herr := LockHeld(c.cfg.Paths)
		if herr == nil && !held && !socketAnswers(c.cfg.Paths) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: the old leader did not release its lock in time", ErrLeaderStarting)
		case <-time.After(pollInterval):
		}
	}
}

// replaceOlder stops a leader of an older build, which this client started, and
// waits until its lock and socket are free. The binary that would start must be
// newer than the leader too, and it is probed before the old leader is asked to
// stop. It returns false with no error when the leader stays; it has then said
// why. If another instance took over during the probe, it closes the old link
// and returns true so the caller connects to that instance without stopping it.
// A leader that runs under a supervisor stays: the supervisor would start
// the old build again.
func (c *connector) replaceOlder(ctx context.Context, cl *Client) (bool, error) {
	if err := c.prepare(ctx); err != nil || !newerBuild(stampOfVersion(c.exeInfo), stampOfRegistered(cl.Info)) {
		c.keep("the binary that would start a new leader is not newer than it; run `ask leader stop` after you install the new binary")
		return false, nil
	}
	frame, err := c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlStatus})
	if err != nil || frame.Type != protocol.LeaderFrameControlReply {
		c.keep("it could not be asked for its status; run `ask leader stop` to restart it with this build")
		return false, nil
	}
	var st protocol.LeaderStatus
	if json.Unmarshal(frame.Payload, &st) != nil {
		c.keep("it could not be asked for its status; run `ask leader stop` to restart it with this build")
		return false, nil
	}
	if st.InstanceID != cl.Info.InstanceID {
		_ = cl.Close()
		return true, nil
	}
	if !st.SpawnedByClient {
		c.keep("it was not started by a client; restart it with the new binary")
		return false, nil
	}
	frame, err = c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlShutdown, InstanceID: st.InstanceID})
	if err != nil || frame.Type != protocol.LeaderFrameControlReply {
		c.keep("it refused to stop; run `ask leader stop` to restart it with this build")
		return false, nil
	}
	_ = cl.Close()
	return true, c.waitReleased(ctx)
}

// manage sends one control frame over a connection that the version gate
// left management-only, and returns the answer.
func (c *connector) manage(ctx context.Context, ctl protocol.LeaderControl) (protocol.LeaderFrame, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "unix", c.cfg.Paths.Socket)
	if err != nil {
		return protocol.LeaderFrame{}, err
	}
	defer func() { _ = conn.Close() }()
	if err := CheckPeer(conn, uint32(os.Geteuid())); err != nil {
		return protocol.LeaderFrame{}, err
	}
	timeout := minDuration(managementTimeout, remaining(ctx))
	reg, rerr := Register(conn, c.cfg.Hello, timeout)
	var w *FrameWriter
	var r *FrameReader
	if rerr == nil {
		w, r = reg.Writer, reg.Reader
	} else {
		var remote *RemoteError
		if !errors.As(rerr, &remote) || remote.Kind != protocol.LeaderErrVersionMismatch {
			return protocol.LeaderFrame{}, rerr
		}
		w, r = NewFrameWriter(conn, protocol.LeaderMaxFrame), NewFrameReader(conn, protocol.LeaderMaxFrame)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	payload, err := json.Marshal(ctl)
	if err != nil {
		return protocol.LeaderFrame{}, err
	}
	if err := w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: payload}); err != nil {
		return protocol.LeaderFrame{}, err
	}
	return r.Next()
}

// Serving tells whether something accepts connections on the leader socket.
func Serving(p Paths) bool { return socketAnswers(p) }

func socketAnswers(p Paths) bool {
	conn, err := net.DialTimeout("unix", p.Socket, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded)
}

func remaining(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			return d
		}
		return time.Millisecond
	}
	return time.Hour
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// ---- status and stop ----

// Status asks the leader for its status. It works across protocol versions,
// because the control frames are the same in every version.
func Status(ctx context.Context, cfg ConnectConfig) (protocol.LeaderStatus, error) {
	c := newConnector(cfg)
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Deadline)
	defer cancel()
	return c.status(ctx)
}

func (c *connector) status(ctx context.Context) (protocol.LeaderStatus, error) {
	frame, err := c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlStatus})
	if err != nil {
		if isAbsent(err) {
			return protocol.LeaderStatus{}, ErrLeaderAbsent
		}
		return protocol.LeaderStatus{}, err
	}
	if frame.Type != protocol.LeaderFrameControlReply {
		return protocol.LeaderStatus{}, fmt.Errorf("%w: unexpected %q frame", ErrMalformedPeer, frame.Type)
	}
	var st protocol.LeaderStatus
	if err := json.Unmarshal(frame.Payload, &st); err != nil {
		return protocol.LeaderStatus{}, fmt.Errorf("%w: %v", ErrMalformedPeer, err)
	}
	return st, nil
}

func isAbsent(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// StopResult tells what Stop did.
type StopResult struct {
	// WasRunning is false when no leader ran.
	WasRunning bool
	// Signalled is true when the control path did not stop the leader and the
	// verified process got SIGTERM.
	Signalled bool
}

// Stop stops the leader of the home. It sends the shutdown control first and
// waits for the lock and the socket to be free. If the control path does not
// work, it sends SIGTERM to the process in the lock file, but only after it
// has shown that the process is `ask leader` of this user that started at the
// recorded time. It never signals a process that it cannot verify.
func Stop(ctx context.Context, cfg ConnectConfig, wait time.Duration) (StopResult, error) {
	c := newConnector(cfg)
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Deadline)
	defer cancel()
	p := c.cfg.Paths
	if held, err := LockHeld(p); err != nil {
		return StopResult{}, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, err)
	} else if !held && !socketAnswers(p) {
		return StopResult{}, nil // a stale PID in the lock file is not a leader
	}
	res := StopResult{WasRunning: true}
	var controlErr error
	st, err := c.status(ctx)
	if err == nil {
		controlErr = c.requestShutdown(ctx, st.InstanceID)
	} else {
		controlErr = err
	}
	if controlErr == nil && waitFree(ctx, p, wait) {
		return res, nil
	}
	var remote *RemoteError
	if errors.As(controlErr, &remote) || errors.Is(controlErr, ErrVersionRefused) {
		// The leader answered and refused. A signal would stop a busy leader the
		// user did not ask to stop this way.
		return res, controlErr
	}
	// The control path did not work or did not finish: use the verified signal.
	owner, oerr := ReadOwner(p)
	if oerr != nil {
		return res, oerr
	}
	//nolint:staticcheck // The stub of a platform without process handles always fails; Linux does not.
	if err := signalLeaderProcess(p, owner); err != nil {
		return res, fmt.Errorf("leader: signal: %w", err)
	}
	res.Signalled = true
	if !waitFree(ctx, p, wait) {
		return res, fmt.Errorf("%w: the leader is still running after SIGTERM", ErrLeaderStarting)
	}
	return res, nil
}

// requestShutdown sends the shutdown control. A leader of another version
// accepts only a conditional shutdown, so the call falls back to that.
func (c *connector) requestShutdown(ctx context.Context, instance string) error {
	frame, err := c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlShutdown, InstanceID: instance})
	if err != nil {
		return err
	}
	if frame.Type == protocol.LeaderFrameControlReply {
		return nil
	}
	frame, err = c.manage(ctx, protocol.LeaderControl{Command: protocol.LeaderControlShutdown, InstanceID: instance, IfIdle: true})
	if err == nil && frame.Type == protocol.LeaderFrameControlReply {
		return nil
	}
	return fmt.Errorf("%w: the leader refused to stop (it is busy, runs another protocol version or runs under a supervisor)", ErrVersionRefused)
}

// waitFree waits until the lock is free and the socket does not answer.
func waitFree(ctx context.Context, p Paths, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		held, err := LockHeld(p)
		if err == nil && !held && !socketAnswers(p) {
			return true
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(pollInterval)
	}
}
