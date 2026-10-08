package main

import (
	"context"
	"errors"
	"io"
	"os"

	"go.uber.org/fx"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/auth"

	sdk "github.com/coder/acp-go-sdk"
)

const acpUsage = `ask acp - serve ACP v1 (Agent Client Protocol) over stdin and stdout

Usage:
  ask acp
  ask acp --help

Reads one JSON-RPC frame per line from stdin and writes one per line to stdout.
Diagnostics go to stderr. The command opens no network listener and no database.
Each session has its own agent. The connection never reads credentials: sign in
on the host with "ask auth login", then call authenticate or session/new.
Exit codes: 0 input ended, 1 error or lost output, 130 SIGINT, 143 SIGTERM, 129 SIGHUP.
A second signal ends the process before the cleanup finishes.
`

// parseACPArgs reads the arguments after "acp". The command takes none, because
// a prompt, a mode or a provider belongs to the protocol, not to the command line.
func parseACPArgs(argv []string) (help bool, err error) {
	switch {
	case len(argv) == 0:
		return false, nil
	case len(argv) == 1 && (argv[0] == "--help" || argv[0] == "-h"):
		return true, nil
	}
	return false, errors.New("acp does not accept arguments; use ask acp --help")
}

// runACP serves the protocol on stdin and stdout until the input ends, the
// output fails or a signal comes.
func runACP(argv []string, stdin io.Reader, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	help, err := parseACPArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if help {
		if _, err := io.WriteString(stdout, acpUsage); err != nil {
			report(stderr, err)
			return 1
		}
		return 0
	}

	// The protocol owns stdout. A stray print would corrupt a frame, so the
	// ordinary stdout of the process goes to stderr for the whole run.
	defer guardStdout(stderr)()

	o := options{provider: defaultProvider, model: defaultModel}
	if deps.inferenceHTTP != nil {
		o.transport = deps.inferenceHTTP.Transport
	}
	o.wait = deps.wait
	_, initial, err := openProvider(o, deps.getenv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	params := app.ACPParams{
		Home:     deps.getenv("ASK_HOME"),
		Env:      func(k string) (string, bool) { v := deps.getenv(k); return v, v != "" },
		AuthHTTP: deps.authHTTP,
		Now:      deps.now,
		Wait:     deps.wait,
		Initial:  initial,
		Info:     sdk.Implementation{Name: "ask", Version: "dev"},
		NewAgent: func(service *auth.Service, sessionID, cwd string) (*agent.Agent, error) {
			return newNativeAgent(o, deps.getenv, service, sessionID, cwd)
		},
	}
	var rt *app.ACPRuntime
	graph := fx.New(app.ACPModule, fx.Supply(params), fx.NopLogger, fx.Populate(&rt))
	if err := graph.Err(); err != nil {
		report(stderr, "Error:", err)
		return 1
	}

	exit := acp.ServeStdio(context.Background(), acp.StdioConfig{
		Adapter: rt.Config,
		In:      stdin,
		Out:     stdout,
		Signals: sigs,
		Cleanup: rt.Cleanup,
		Diag:    stderr,
	})
	if exit.Err != nil && !errors.Is(exit.Err, context.Canceled) {
		report(stderr, "ask acp:", exit.Err)
	}
	if exit.Forced {
		report(stderr, "ask acp: cleanup did not drain; forced exit")
	}
	switch {
	case exit.Signal != nil:
		return signalExitCodes[exit.Signal]
	case exit.Err != nil:
		return 1
	}
	return 0
}
