package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"golang.org/x/term"

	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/openai"
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
type runDependencies struct {
	getenv        func(string) string
	authHTTP      *http.Client
	inferenceHTTP *http.Client
	now           func() time.Time
	wait          func(context.Context, time.Duration) error
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithDependencies(argv, stdin, stdout, stderr, runDependencies{getenv: os.Getenv})
}

func runWithDependencies(argv []string, stdin io.Reader, stdout, stderr io.Writer, deps runDependencies) int {
	if deps.getenv == nil {
		deps.getenv = os.Getenv
	}
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, slices.Collect(maps.Keys(signalExitCodes))...)
	defer signal.Stop(sigs)
	env := func(k string) (string, bool) { v := deps.getenv(k); return v, v != "" }
	var service *auth.Service
	if len(argv) > 0 && argv[0] == "auth" {
		o, err := parseAuthArgs(argv[1:])
		if err != nil {
			report(stderr, "Error:", err)
			return 1
		}
		if o.help {
			_, err := io.WriteString(stdout, authUsage)
			if err != nil {
				return 1
			}
			return 0
		}
		service, err = app.NewNativeAuth(deps.getenv("ASK_HOME"), env, auth.NativeOptions{HTTPClient: deps.authHTTP, Now: deps.now, Wait: deps.wait})
		if err != nil {
			report(stderr, "Error:", err)
			return 1
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan int, 1)
		go func() { done <- runAuth(ctx, argv[1:], stdin, stdout, stderr, service) }()
		select {
		case code := <-done:
			return code
		case sig := <-sigs:
			service.StopRefresh()
			cancel()
			if err := app.AuthWait(service)(context.Background()); err != nil {
				report(stderr, "Auth shutdown:", err)
			}
			select {
			case <-done:
			case <-time.After(abortGrace):
			}
			return signalExitCodes[sig]
		}
	}
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

	if deps.inferenceHTTP != nil {
		o.transport = deps.inferenceHTTP.Transport
	}
	if _, err := startCapture(&o, deps.getenv, stderr, nil); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.provider != defaultProvider {
		var err error
		service, err = app.NewNativeAuth(deps.getenv("ASK_HOME"), env, auth.NativeOptions{HTTPClient: deps.authHTTP, Now: deps.now, Wait: deps.wait})
		if err != nil {
			report(stderr, "Error:", err)
			return 1
		}
	}
	ag, err := newHeadlessAgentWithAuth(o, deps.getenv, service)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	prompts, err := readPrompts(o, stdin, stdinTTY)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}

	return runHeadless(ag, prompts, mode, stdout, stderr, sigs, service)
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
	return newHeadlessAgentWithAuth(o, getenv, nil, extra...)
}

func newHeadlessAgentWithAuth(o options, getenv func(string) string, service *auth.Service, extra ...tools.Tool) (*agent.Agent, error) {
	stream, model, err := openProvider(o, getenv)
	if err != nil {
		return nil, err
	}
	reasoning := o.thinking
	if o.provider == anthropic.ProviderID && reasoning == "" {
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
	wires := providers.NewRegistry()
	wires.Register(model.API, stream)
	if o.provider != defaultProvider {
		env := func(k string) (string, bool) { v := getenv(k); return v, v != "" }
		wires.RegisterProvider(anthropic.New(anthropic.WithEnv(env), anthropic.WithHTTPClient(&http.Client{Transport: o.transport})))
		registerOpenAIWires(wires, o, getenv)
	}
	sessionID := newSessionID()
	bound := providers.BoundKey{}
	if o.apiKey != "" {
		bound = providers.BoundKey{Provider: o.provider, Secret: o.apiKey}
	}
	cfg := agent.Config{
		LoopConfig: agent.LoopConfig{
			Model:    model,
			Stream:   wires.Stream,
			BoundKey: bound,
			Options:  providers.StreamOptions{Reasoning: reasoning, APIKey: o.apiKey},
			Cwd:      cwd,
			Wait:     o.wait,
		},
		Registry:  wires,
		Tools:     reg,
		SessionID: sessionID,
	}
	if service != nil {
		cfg = app.BindAuth(cfg, service, wires)
	}
	return agent.New(cfg)
}

func registerOpenAIWires(reg *providers.Registry, o options, getenv func(string) string) {
	opts := []openai.Option{openai.WithEnv(func(k string) (string, bool) {
		v := getenv(k)
		if v == "" {
			return "", false
		}
		return v, true
	})}
	if o.transport != nil {
		opts = append(opts, openai.WithHTTPClient(&http.Client{Transport: o.transport}))
	}
	reg.RegisterProvider(openai.NewCompletions(opts...))
	reg.RegisterProvider(openai.NewResponses(opts...))
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// runHeadless runs the prompts in order. Print mode prints the reply at the
// end; JSON mode streams every event and exits 0 even when the assistant
// ends in error, as Pi does. A returned error or a failed stdout write stops
// the remaining prompts and gives exit 1; an assistant error does not stop
// them. The first signal disposes the Agent, waits up to abortGrace for the run
// to settle and for its last lines to reach stdout, and gives the signal's exit
// code without printing the reply. The grace bounds the exit even when a write
// is blocked on a full pipe or a tool does not return.
// Registered auth work drains first with its separate bound; another signal
// cannot shorten that drain.
func runHeadless(ag *agent.Agent, prompts []string, mode runMode, stdout, stderr io.Writer, sigs <-chan os.Signal, services ...*auth.Service) int {
	out := newProtocolOut(stdout)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	if mode == modeJSON {
		// A failed stdout write ends the run and the prompts that have not
		// started: nobody reads the replies.
		defer streamJSON(ag, out, func() {
			cancel(agent.ErrOutputFailure)
			ag.Abort()
		})()
	}
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
		var service *auth.Service
		if len(services) > 0 {
			service = services[0]
		}
		if service != nil {
			service.StopRefresh()
		}
		// Dispose closes admission first, so the prompts that have not started
		// fail with ErrDisposed, and cancels the active run. It waits for the
		// run to settle, which includes the tools that started, so it runs on
		// its own goroutine and the grace below bounds the exit.
		disposed := make(chan struct{})
		go func() {
			defer close(disposed)
			if err := ag.Dispose(); err != nil {
				report(stderr, "Dispose:", err)
			}
		}()
		if service != nil {
			if err := app.AuthWait(service)(context.Background()); err != nil {
				report(stderr, "Auth shutdown:", err)
			}
		}
		grace := time.NewTimer(abortGrace)
		defer grace.Stop()
		expired := false
		select {
		case <-disposed:
		case <-sigs:
		case <-grace.C:
			expired = true
		}
		if !expired {
			// The final lines wait for a reader that keeps up, but only within
			// the same grace: a write blocked on a full pipe holds the output
			// lock, and the exit never waits on that lock. A write error
			// changes nothing; the signal sets the code.
			select {
			case <-out.flushAsync():
			case <-sigs:
			case <-grace.C:
			}
		}
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
