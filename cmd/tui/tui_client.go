package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/app"
	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/internal/tuiclient"
	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

// inProcessStopWait bounds the wait for the in-process agent to stop when the
// client closes. A tool that does not return must not keep the terminal.
const inProcessStopWait = 10 * time.Second

// tuiClientOptions are the facts that openTUIClient cannot read from the
// process dependencies.
type tuiClientOptions struct {
	// callerExe is the executable that looks for the ask binary next to it when a
	// leader must be started.
	callerExe string
	// env is the environment of a leader that this client starts. Nil means the
	// current environment.
	env []string
	// warn receives one-line hints from the leader connect. Nil discards them.
	warn func(string)
	// initialBackoff and maxBackoff set the reconnect timing. Zero means the
	// design values, 1 s doubling to 30 s.
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// openedClient is the client that openTUIClient chose and why.
type openedClient struct {
	*tuiclient.Client
	// FallbackReason is the connect error that made the in-process agent the
	// choice. It is nil when the client talks to the leader.
	FallbackReason error
}

// openTUIClient connects to the leader of the Ask home and starts one when none
// runs. Only when that fails at startup does it run an agent in this process.
// This is the one place that makes the choice: a client that lost its leader
// later keeps reconnecting and never turns into an in-process agent.
func openTUIClient(ctx context.Context, deps runDependencies, o tuiClientOptions) (*openedClient, error) {
	reason := error(nil)
	paths, err := leader.ResolvePaths(deps.getenv("ASK_HOME"))
	if err == nil {
		var c *tuiclient.Client
		c, err = tuiclient.Open(ctx, tuiclient.Config{
			Dial: tuiclient.LeaderDial(leader.ConnectConfig{
				Paths:     paths,
				Hello:     protocol.LeaderRegister{ClientKind: "tui", Build: buildIdentity()},
				CallerExe: o.callerExe,
				Env:       o.env,
				Warn:      o.warn,
			}),
			ClientName:     "ask",
			Version:        buildIdentity(),
			InitialBackoff: o.initialBackoff,
			MaxBackoff:     o.maxBackoff,
		})
		if err == nil {
			return &openedClient{Client: c}, nil
		}
	}
	reason = err
	c, ierr := openInProcessClient(ctx, deps)
	if ierr != nil {
		return nil, fmt.Errorf("leader: %v; in-process agent: %w", reason, ierr)
	}
	return &openedClient{Client: c, FallbackReason: reason}, nil
}

// openInProcessClient runs the agent host of `ask acp` inside this process and
// connects a client to it over a pipe. The client code is the same as for the
// leader, so both modes treat prompts, queues and follow the same way. Closing
// the client stops the agent and its run.
func openInProcessClient(ctx context.Context, deps runDependencies) (*tuiclient.Client, error) {
	params, err := acpParams(deps)
	if err != nil {
		return nil, err
	}
	var rt *app.ACPRuntime
	graph := fx.New(app.ACPModule, fx.Supply(params), fx.NopLogger, fx.Populate(&rt))
	if err := graph.Err(); err != nil {
		return nil, err
	}
	// toAgent: the client writes, the host reads. fromAgent: the host writes.
	toAgentR, toAgentW := io.Pipe()
	fromAgentR, fromAgentW := io.Pipe()
	exited := make(chan acp.StdioExit, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				exited <- acp.StdioExit{Err: rpc.Recover("in-process agent", v)}
			}
		}()
		exited <- acp.ServeStdio(context.Background(), acp.StdioConfig{
			Adapter: rt.Config, In: toAgentR, Out: fromAgentW, Cleanup: rt.Cleanup,
		})
	}()
	link := tuiclient.Link{
		Transport: rpc.NewLineTransport(fromAgentR, toAgentW, toAgentW, fromAgentR),
		Info:      tui.ConnInfo{Mode: tui.ModeInProcess, Build: buildIdentity()},
	}
	linked := false
	c, err := tuiclient.Open(ctx, tuiclient.Config{
		Dial: func(context.Context) (tuiclient.Link, error) {
			if linked {
				return tuiclient.Link{}, errors.New("the in-process link cannot be opened twice")
			}
			linked = true
			return link, nil
		},
		NoReconnect: true,
		ClientName:  "ask",
		Version:     buildIdentity(),
		OnClose: func() error {
			select {
			case exit := <-exited:
				return inProcessExitError(exit.Err)
			case <-time.After(inProcessStopWait):
				return errors.New("the in-process agent did not stop in time")
			}
		},
	})
	if err != nil {
		_ = link.Transport.Close()
		<-exited
		return nil, err
	}
	return c, nil
}

func inProcessExitError(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, failure := range joined.Unwrap() {
			if failure = inProcessExitError(failure); failure != nil {
				failures = append(failures, failure)
			}
		}
		if len(failures) == 2 &&
			((failures[0] == acp.ErrOutputFailed && failures[1] == io.ErrClosedPipe) ||
				(failures[1] == acp.ErrOutputFailed && failures[0] == io.ErrClosedPipe)) {
			return nil
		}
		return errors.Join(failures...)
	}
	// Closing the client pipe can stop a final host notification.
	if errors.Is(err, context.Canceled) ||
		(errors.Is(err, acp.ErrOutputFailed) && errors.Is(err, io.ErrClosedPipe)) {
		return nil
	}
	return err
}
