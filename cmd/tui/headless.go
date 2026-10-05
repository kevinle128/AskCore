package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"golang.org/x/term"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/tokenplan"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

type runMode uint8

const (
	modeInteractive runMode = iota
	modePrint
	modeJSON
)

// signalExitCodes are the exit codes of print and JSON mode per signal. Pi
// has no SIGINT handler; Ask aborts the run and exits 130, the shell's code
// for it.
var signalExitCodes = map[os.Signal]int{
	os.Interrupt:    130,
	syscall.SIGTERM: 143,
	syscall.SIGHUP:  129,
}

// abortGrace bounds the wait for the aborted run to settle after a signal.
const abortGrace = 2 * time.Second

// run is the whole program behind main. It returns the exit code.
func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, diags := parseArgs(argv)
	failed := false
	for _, d := range diags {
		report(stderr, d)
		failed = failed || d.err
	}
	if failed {
		return 1
	}
	if o.help {
		if _, err := io.WriteString(stdout, usage); err != nil {
			report(stderr, err)
			return 1
		}
		return 0
	}

	stdinTTY := isTerminal(stdin)
	mode := selectMode(o, stdinTTY, isTerminal(stdout))
	if mode == modeInteractive {
		return runInteractive(stdin, stdout, stderr)
	}
	defer guardStdout(stderr)()

	ag, err := newHeadlessAgent(o, os.Getenv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	prompts, err := readPrompts(o, stdin, stdinTTY)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, slices.Collect(maps.Keys(signalExitCodes))...)
	defer signal.Stop(sigs)
	return runHeadless(ag, prompts, mode, stdout, stderr, sigs)
}

// selectMode follows Pi's resolveAppMode. --mode text does not force print
// mode.
func selectMode(o options, stdinTTY, stdoutTTY bool) runMode {
	switch {
	case o.mode == outputJSON:
		return modeJSON
	case o.print || !stdinTTY || !stdoutTTY:
		return modePrint
	default:
		return modeInteractive
	}
}

// report writes one line to stderr. A failed write to stderr has nowhere to
// be reported.
func report(stderr io.Writer, a ...any) { _, _ = fmt.Fprintln(stderr, a...) }

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func readPrompts(o options, stdin io.Reader, stdinTTY bool) ([]string, error) {
	var piped string
	if !stdinTTY {
		var err error
		if piped, err = readPipedStdin(stdin); err != nil {
			return nil, err
		}
	}
	files, err := fileText(o.files)
	if err != nil {
		return nil, err
	}
	return buildPrompts(piped, files, o.messages), nil
}

// builtinTools are the tools that every headless agent registers.
func builtinTools() []tools.Tool { return []tools.Tool{tools.Echo{}} }

// newHeadlessAgent builds the in-process agent with the builtin tools, then
// the extra tools. ask passes no extra tools; a test passes the tools its
// scenario needs and still runs the same agent setup as ask.
// faux is the default. alibaba-token-plan is the H3 Token Plan adapter.
func newHeadlessAgent(o options, getenv func(string) string, extra ...tools.Tool) (*agent.Agent, error) {
	stream, model, err := openProvider(o, getenv)
	if err != nil {
		return nil, err
	}
	reasoning := o.thinking
	if o.provider == tokenplan.ProviderID && reasoning == "" {
		reasoning = protocol.ThinkingMedium
	}
	reg := &tools.Registry{}
	for _, t := range builtinTools() {
		if err := reg.Register(t, tools.SourceInfo{Kind: tools.SourceBuiltin}); err != nil {
			return nil, err
		}
	}
	// Extra tools do not ship with ask, so they are not marked builtin.
	for _, t := range extra {
		if err := reg.Register(t, tools.SourceInfo{Kind: tools.SourceExtension, Name: "injected"}); err != nil {
			return nil, err
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{
			Model:   model,
			Stream:  stream,
			Options: providers.StreamOptions{Reasoning: reasoning, APIKey: o.apiKey},
			Cwd:     cwd,
		},
		Tools: reg,
	})
}

// runHeadless runs the prompts in order. Print mode prints the reply at the
// end; JSON mode streams every event and exits 0 even when the assistant
// ends in error, as Pi does. A returned error or a failed stdout write stops
// the remaining prompts and gives exit 1; an assistant error does not stop
// them. The first signal aborts the run, waits up to abortGrace for it to
// settle (a second signal stops the wait) and gives the signal's exit code
// without printing the reply.
func runHeadless(ag *agent.Agent, prompts []string, mode runMode, stdout, stderr io.Writer, sigs <-chan os.Signal) int {
	out := newProtocolOut(stdout)
	if mode == modeJSON {
		defer streamJSON(ag, out)()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- promptAll(ctx, ag, prompts) }()

	select {
	case err := <-done:
		code := 0
		switch {
		case err != nil:
			code = 1
		case mode == modePrint:
			code = printReply(ag.State().Messages, out, stderr)
		}
		if out.failed(stderr) {
			return 1
		}
		if err != nil {
			report(stderr, err)
		}
		return code
	case sig := <-sigs:
		ag.Abort()
		// Abort ends only the active run; cancel also stops the prompts
		// that have not started.
		cancel()
		grace := time.NewTimer(abortGrace)
		defer grace.Stop()
		select {
		case <-done:
		case <-sigs:
		case <-grace.C:
		}
		_ = out.flush() // a write error changes nothing; the signal sets the code
		return signalExitCodes[sig]
	}
}

func promptAll(ctx context.Context, ag *agent.Agent, prompts []string) error {
	for _, p := range prompts {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: p}}, Timestamp: time.Now().UnixMilli()}
		if err := ag.Prompt(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}
