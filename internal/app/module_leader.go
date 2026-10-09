package app

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"go.uber.org/fx"
	"go.uber.org/zap"

	"AskCore/internal/acp"
	"AskCore/internal/leader"

	sdk "github.com/coder/acp-go-sdk"
)

// LeaderParams supplies the process facts of `ask leader`. The module reads no
// configuration file and opens no database. The socket, the lock and the log are
// the job of the command; this composition receives the listener in Serve.
type LeaderParams struct {
	// ACPParams are the facts of the shared agent host: the credential home,
	// the clocks, the initial model and the agent constructor.
	ACPParams
	// Server is the handshake configuration: instance and build identity, the
	// owner user, the register timeout. Nil OwnerUID means the current user.
	Server leader.ServerConfig
	// PID and SpawnedByClient go into the status reply of the leader.
	PID             int
	SpawnedByClient bool
	// Logger may be nil.
	Logger *zap.Logger
}

// LeaderRuntime is the composed leader: one shared ACP host behind one internal
// byte link, and the router that serves many clients on top of it.
type LeaderRuntime struct {
	// Server routes the clients. Serve starts it.
	Server *leader.Server
	// Adapter is the one ACP adapter and host that every client shares.
	Adapter *acp.Adapter

	cleanup func(ctx context.Context) error
	stop    sync.Once
	stopErr error
}

// LeaderModule provides the LeaderRuntime. Supply LeaderParams to use it.
var LeaderModule = fx.Options(fx.Provide(NewLeaderRuntime))

// linkEnd is the leader end of the internal ACP link. Closing it ends both
// directions, so the adapter sees the end of its input.
type linkEnd struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (l *linkEnd) Close() error {
	var errs []error
	for _, c := range l.closers {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}

// NewLeaderRuntime builds the shared host and the router. Nothing runs and
// nothing listens until Serve.
//
// A client that leaves never reaches Adapter.Fail, Adapter.Close or the host.
// Only the end of the link, or a stop, does.
func NewLeaderRuntime(p LeaderParams) (*LeaderRuntime, error) {
	acpRT, err := NewACPRuntime(p.ACPParams)
	if err != nil {
		return nil, err
	}
	cfg := acpRT.Config
	cfg.RequireRoute = true
	a, err := acp.NewAdapter(context.Background(), cfg)
	if err != nil {
		return nil, err
	}

	// agent input: leader writes, adapter reads. agent output: adapter writes, leader reads.
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cw := acp.NewCheckedWriter(outW, func(err error) {
		a.Fail(err)
		_ = inR.CloseWithError(err)
	})
	conn := sdk.NewAgentSideConnection(a, cw, acp.NewLineLimitReader(inR, leader.MaxLine))
	a.Bind(conn, cw, outW)

	srv, err := leader.NewServer(leader.Config{
		Server:          p.Server,
		Agent:           &linkEnd{Reader: outR, Writer: inW, closers: []io.Closer{outR, inW}},
		Logger:          p.Logger,
		PID:             p.PID,
		SpawnedByClient: p.SpawnedByClient,
		ActiveRuns:      a.ActiveRuns,
		QuiesceIfIdle:   a.QuiesceIfIdle,
	})
	if err != nil {
		_ = a.Close()
		return nil, err
	}
	return &LeaderRuntime{Server: srv, Adapter: a, cleanup: acpRT.Cleanup}, nil
}

// Serve starts the router and serves the clients of ln until ctx ends, the link
// to the agent fails, or the listener fails. It then stops in order and returns
// the first error.
func (rt *LeaderRuntime) Serve(ctx context.Context, ln net.Listener) error {
	if err := rt.Server.Start(); err != nil {
		_ = rt.Stop(context.WithoutCancel(ctx))
		return err
	}
	served := make(chan error, 1)
	go func() { served <- rt.Server.Serve(ln) }()
	var first error
	select {
	case <-ctx.Done():
	case <-rt.Server.Done():
		first = rt.Server.Err()
	case <-rt.Adapter.Failed():
		first = rt.Adapter.Failure()
	case first = <-served:
	}
	// The stop runs even when ctx ended: started tool bodies drain without a bound.
	stopErr := rt.Stop(context.WithoutCancel(ctx))
	return errors.Join(first, stopErr)
}

// Stop shuts the leader down in this order: the router stops taking clients and
// tells them, the link closes, the host disposes every session and waits for
// the started tool bodies. The credential refresh drains in parallel. It is safe to
// call twice.
func (rt *LeaderRuntime) Stop(ctx context.Context) error {
	rt.stop.Do(func() {
		errs := []error{rt.Server.Close()}
		cleaned := make(chan error, 1)
		go func() { cleaned <- rt.cleanup(ctx) }()
		errs = append(errs, rt.Adapter.Close())
		errs = append(errs, <-cleaned)
		rt.stopErr = errors.Join(errs...)
	})
	return rt.stopErr
}
