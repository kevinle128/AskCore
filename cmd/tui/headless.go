package main

import (
	"context"
	"crypto/rand"
	"errors"
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
	"AskCore/internal/jobs"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/sandbox"
	"AskCore/internal/tasks"
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
	// environ returns the environment that the child processes of the tools
	// inherit from. Nil means os.Environ.
	environ func() []string
	// onJobError replaces the stderr line for job table errors. The in-process
	// TUI sets it, because its stderr is the terminal under a full-screen
	// program.
	onJobError func(error)
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
	if len(argv) > 0 && argv[0] == "acp" {
		return runACP(argv[1:], stdin, stdout, stderr, deps, sigs)
	}
	if len(argv) > 0 && argv[0] == "connect" {
		exe, _ := os.Executable()
		return runConnect(argv[1:], stdin, stdout, stderr, deps, sigs, exe)
	}
	if len(argv) > 0 && argv[0] == "leader" {
		return runLeader(argv[1:], stdout, stderr, deps, sigs)
	}
	if len(argv) > 0 && argv[0] == "version" {
		return runVersion(argv[1:], stdout, stderr)
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
		return runInteractive(stdin, stdout, stderr, deps, sigs)
	}
	defer guardStdout(stderr)()

	if deps.inferenceHTTP != nil {
		o.transport = deps.inferenceHTTP.Transport
	}
	o.environ = deps.environ
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
	ag, jt, err := newHeadlessAgentWithAuth(o, deps.getenv, service)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	prompts, err := readPrompts(o, stdin, stdinTTY)
	if err != nil {
		report(stderr, "Error:", err)
		if err := ag.Dispose(); err != nil {
			report(stderr, "Dispose:", err)
		}
		return 1
	}

	code := runHeadless(ag, prompts, mode, stdout, stderr, sigs, service)
	// The signal path returns after abortGrace, which is shorter than the kill
	// grace of a job, so a job that traps SIGTERM can still live here.
	jt.KillRemaining()
	return code
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

// sessionTools are the tools that every agent registers: the file, search and
// shell tools over the state of one session, and the demo echo tool. modelInput
// returns the input kinds of the current model; nil means every model takes
// images.
func sessionTools(jt *jobs.Table, fs *tools.FileState, modelInput func() []string) []tools.Tool {
	ts := tools.NewFileTools(fs, modelInput)
	ts = append(ts, tools.Grep{}, tools.Find{})
	ts = append(ts, tools.NewShellTools(jt)...)
	return append(ts, tools.Echo{})
}

// newHeadlessAgent builds the in-process agent with the session tools, then
// the extra tools. ask passes no extra tools; a test passes the tools its
// scenario needs and still runs the same agent setup as ask. The job table
// closes when the Agent is disposed.
// faux is the default. alibaba-token-plan is the H3 Token Plan adapter.
func newHeadlessAgent(o options, getenv func(string) string, extra ...tools.Tool) (*agent.Agent, error) {
	ag, _, err := newHeadlessAgentWithAuth(o, getenv, nil, extra...)
	return ag, err
}

func newHeadlessAgentWithAuth(o options, getenv func(string) string, service *auth.Service, extra ...tools.Tool) (*agent.Agent, *jobs.Table, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	return newNativeAgent(o, getenv, service, newSessionID(), cwd, extra...)
}

// newNativeAgent builds one independent agent for the session and directory.
// The headless run and every ACP session use it, so both share one composition.
func newNativeAgent(o options, getenv func(string) string, service *auth.Service, sessionID, cwd string, extra ...tools.Tool) (*agent.Agent, *jobs.Table, error) {
	stream, model, err := openProvider(o, getenv)
	if err != nil {
		return nil, nil, err
	}
	return newNativeAgentOn(o, getenv, service, sessionID, cwd, stream, model, extra...)
}

// commandGuardEnv enables the command-guard when its value is "1".
const commandGuardEnv = "ASK_COMMAND_GUARD"

// newNativeAgentOn builds the agent on a given model. stream serves the demo
// model and may be nil when the model is a catalog model or when the agent has
// no model.
//
// The agent owns a file state and a job table of its own. The table closes
// when the Agent is disposed (Config.Release), which kills the background jobs
// of the session. The caller gets the table too, for a last sweep at exit. A
// failed constructor closes it before it returns.
func newNativeAgentOn(o options, getenv func(string) string, service *auth.Service, sessionID, cwd string, stream providers.StreamFn, model providers.Model, extra ...tools.Tool) (*agent.Agent, *jobs.Table, error) {
	reasoning := o.thinking
	if o.provider == anthropic.ProviderID && reasoning == "" {
		reasoning = protocol.ThinkingMedium
	}
	environ := o.environ
	if environ == nil {
		environ = os.Environ
	}
	onJobError := o.onJobError
	if onJobError == nil {
		onJobError = func(err error) { report(os.Stderr, "jobs:", err) }
	}
	// ag is bound after the constructor; the closures run later, at the start
	// of a tool call or a job, so they see the current model.
	var ag *agent.Agent
	jt := jobs.New(jobs.Options{
		Clock: sandbox.SystemClock,
		// A notice of a reset conversation or a disposed Agent is dropped on
		// purpose; the job is muted and nothing is reported.
		IsExpectedDrop: func(err error) bool { return errors.Is(err, tasks.ErrNotDelivered) },
		Env: func() []string {
			st := ag.State()
			return sandbox.ChildEnv(environ(), map[string]string{
				"ASK_SESSION_ID":      sessionID,
				"ASK_PROVIDER":        st.Model.Provider,
				"ASK_MODEL":           st.Model.ID,
				"ASK_REASONING_LEVEL": string(st.ThinkingLevel),
			})
		},
		// A notice that the Agent refused has no caller to report to. The job
		// stays readable with job_list and job_output.
		OnError: onJobError,
	})
	fail := func(err error) (*agent.Agent, *jobs.Table, error) {
		return nil, nil, errors.Join(err, jt.Close())
	}
	modelInput := func() []string { return ag.State().Model.Input }
	reg := &tools.Registry{}
	for _, t := range sessionTools(jt, tools.NewFileState(), modelInput) {
		if err := reg.Register(t, tools.SourceInfo{Kind: tools.SourceBuiltin}); err != nil {
			return fail(err)
		}
	}
	// Extra tools do not ship with ask, so they are not marked builtin.
	for _, t := range extra {
		if err := reg.Register(t, tools.SourceInfo{Kind: tools.SourceExtension, Name: "injected"}); err != nil {
			return fail(err)
		}
	}
	bound := providers.BoundKey{}
	if o.apiKey != "" {
		bound = providers.BoundKey{Provider: o.provider, Secret: o.apiKey}
	}
	cfg := agent.Config{
		LoopConfig: agent.LoopConfig{
			Model:    model,
			Stream:   stream,
			BoundKey: bound,
			Options:  providers.StreamOptions{Reasoning: reasoning, APIKey: o.apiKey},
			Cwd:      cwd,
			Wait:     o.wait,
		},
		Tools:        reg,
		SystemPrompt: tools.PromptSection(reg.Snapshot()),
		SessionID:    sessionID,
		Release:      jt.Close,
	}
	if getenv(commandGuardEnv) == "1" {
		pl := pipeline.NewRegistry()
		pl.OnBeforeTool(pipeline.CommandGuard)
		cfg.Pipeline = pl
	}
	native := app.NativeAgentConfig{
		Config: cfg, InitialStream: stream, Auth: service,
		Env: func(k string) (string, bool) { v := getenv(k); return v, v != "" },
	}
	if o.provider == "faux" {
		native.InitialAPI = "faux"
	}
	if o.transport != nil {
		native.HTTPClient = &http.Client{Transport: o.transport}
	}
	made, err := app.NewNativeAgent(native)
	if err != nil {
		return fail(err)
	}
	ag = made
	return ag, jt, nil
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
		// The reply is read before Dispose, which ends the background jobs
		// of the session. Dispose does not change the exit code.
		if derr := ag.Dispose(); derr != nil {
			report(stderr, "Dispose:", derr)
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

// promptTarget is the part of the Agent that promptAll uses.
type promptTarget interface {
	Prompt(ctx context.Context, msgs ...protocol.Message) error
	WaitForIdle(ctx context.Context) error
	Idle() <-chan struct{}
}

// promptAll sends the prompts in order. A job notice can start a run of its
// own, so each prompt waits for the Agent to be idle first and, when a notice
// run starts in between and Prompt returns ErrBusy, waits for the idle signal
// and retries the same prompt. It never sleeps or polls. After the last prompt it waits once more: a notice that
// came during the last prompt gets its answer before the caller reads the
// reply.
func promptAll(ctx context.Context, ag promptTarget, prompts []string) error {
	for _, p := range prompts {
		msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: p}}, Timestamp: time.Now().UnixMilli()}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := ag.WaitForIdle(ctx); err != nil {
				return err
			}
			err := ag.Prompt(ctx, msg)
			if err == nil {
				break
			}
			if !errors.Is(err, agent.ErrBusy) {
				return err
			}
			select {
			case <-ag.Idle():
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return ag.WaitForIdle(ctx)
}
