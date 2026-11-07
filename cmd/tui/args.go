package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"AskCore/internal/jobs"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// outputMode is the value of --mode. The empty value means the flag was not
// given.
type outputMode string

const (
	outputText outputMode = "text"
	outputJSON outputMode = "json"
)

// options is the parsed command line. Pi's cli/args.ts is the reference.
type options struct {
	print    bool
	help     bool
	mode     outputMode
	provider string
	model    string
	apiKey   string
	thinking protocol.ThinkingLevel
	messages []string
	files    []string
	// transport carries the provider HTTP. Nil uses http.DefaultTransport.
	// Tests set a cassette here; ASK_CAPTURE sets a recording one.
	transport http.RoundTripper
	// wait is the retry clock. Nil uses the production timer.
	wait func(context.Context, time.Duration) error
	// environ returns the environment that the child processes of the tools
	// inherit from. Nil means os.Environ.
	environ func() []string
	// onJobError receives the errors that the job table of a session cannot
	// report to a caller, such as a notice that the Agent refused. Nil writes
	// one line to stderr, which suits print mode, ask acp and the leader (its
	// stderr is the leader log).
	onJobError func(error)
	// maxTurns ends a print-mode run after that many turns, turns that a
	// notice wakes included. Zero means no limit.
	maxTurns int
	// bgLimit is the background limit of the job table. Only print mode sets
	// it; the TUI, the leader and ask acp have none.
	bgLimit jobs.BackgroundLimit
	// noLeader makes the TUI run its agent in this process. It never connects
	// to a leader and never starts one, so a debugger on this process sees the
	// tool calls.
	noLeader bool
	// trust is the project trust override of --approve or --no-approve, or
	// empty.
	trust protocol.ACPTrustOverride
	// modelGiven is true when --provider or --model is on the command line.
	// Then print mode does not use the saved default model.
	modelGiven bool
	// providerGiven is true when --provider is on the command line.
	providerGiven bool
	// models are the scope entries of --models: patterns and globs.
	models []string
	// offline turns off the network refresh of the model list, as
	// ASK_DISABLE_MODEL_REFRESH=1 does, for this process and a leader that it
	// starts.
	offline bool
	// session holds the flags of one session: the prompt text (a file given on
	// the command line is already read) and the tool selection. The TUI sends
	// them to the host in session/new _meta, and print mode applies them
	// itself.
	session protocol.ACPSessionOptions
}

// diagnostic is one parse problem. Any error ends the program with exit 1; a
// warning is printed and the run goes on.
type diagnostic struct {
	err bool
	msg string
}

func (d diagnostic) String() string {
	if d.err {
		return "Error: " + d.msg
	}
	return "Warning: " + d.msg
}

const (
	defaultProvider = "faux"
	defaultModel    = "faux-1"
)

var thinkingLevels = []protocol.ThinkingLevel{
	protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
	protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax,
}

// valueFlags are the flags that take the next token as their value.
var valueFlags = map[string]func(o *options, v string) *diagnostic{
	"--provider": func(o *options, v string) *diagnostic {
		o.provider, o.modelGiven, o.providerGiven = v, true, true
		return nil
	},
	"--model":   func(o *options, v string) *diagnostic { o.model, o.modelGiven = v, true; return nil },
	"--api-key": func(o *options, v string) *diagnostic { o.apiKey = v; return nil },
	"--thinking": func(o *options, v string) *diagnostic {
		level := protocol.ThinkingLevel(v)
		if !slices.Contains(thinkingLevels, level) {
			names := make([]string, len(thinkingLevels))
			for i, l := range thinkingLevels {
				names[i] = string(l)
			}
			return &diagnostic{msg: fmt.Sprintf("Invalid thinking level %q. Valid values: %s", v, strings.Join(names, ", "))}
		}
		o.thinking = level
		return nil
	},
	"--max-turns": func(o *options, v string) *diagnostic {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return &diagnostic{err: true, msg: fmt.Sprintf("Invalid max turns %q. Use a whole number above 0", v)}
		}
		o.maxTurns = n
		return nil
	},
	"--system-prompt": func(o *options, v string) *diagnostic {
		text, d := promptFlagText("--system-prompt", v)
		o.session.SystemPrompt = text
		return d
	},
	"--append-system-prompt": func(o *options, v string) *diagnostic {
		text, d := promptFlagText("--append-system-prompt", v)
		o.session.AppendSystemPrompt = append(o.session.AppendSystemPrompt, text)
		return d
	},
	"--tools":  addToolNames,
	"-t":       addToolNames,
	"--models": addModelPatterns,
}

// addModelPatterns adds the entries of a comma list to the scope. Empty
// entries are dropped. The entries are resolved later, against the model list.
func addModelPatterns(o *options, v string) *diagnostic {
	for entry := range strings.SplitSeq(v, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			o.models = append(o.models, entry)
		}
	}
	return nil
}

// modelPattern returns the model pattern of the command line: "P/M" for
// --provider P with --model M or with the default model of P, the --model
// value alone without --provider, and "" when neither flag is given. A model
// that already starts with "P/" is not prefixed again.
func (o options) modelPattern() string {
	switch {
	case o.providerGiven && strings.HasPrefix(o.model, o.provider+"/"):
		return o.model
	case o.providerGiven:
		return o.provider + "/" + o.model
	case o.modelGiven:
		return o.model
	}
	return ""
}

// addToolNames adds the names of a comma list to the tool selection. A name
// that is not valid shows later, when the whole selection is parsed.
func addToolNames(o *options, v string) *diagnostic {
	for name := range strings.SplitSeq(v, ",") {
		o.session.Tools = append(o.session.Tools, strings.TrimSpace(name))
	}
	return nil
}

// promptFlagText returns the text of a prompt flag. A value that names an
// existing regular file is replaced by the content of that file, so the wire
// carries text and the host never reads a path of the client. Any other value
// is the text.
func promptFlagText(flag, v string) (string, *diagnostic) {
	info, err := os.Stat(v)
	if err != nil || !info.Mode().IsRegular() {
		return v, nil
	}
	f, err := os.Open(v)
	if err != nil {
		return "", &diagnostic{err: true, msg: fmt.Sprintf("%s: cannot read %s", flag, v)}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, protocol.ACPSessionPromptMaxBytes+1))
	if err != nil {
		return "", &diagnostic{err: true, msg: fmt.Sprintf("%s: cannot read %s", flag, v)}
	}
	return string(data), nil
}

// parseArgs parses argv without the program name. Unlike Pi, an unknown
// --flag is an error, because Ask has no extension flags yet.
func parseArgs(argv []string) (options, []diagnostic) {
	o := options{provider: defaultProvider}
	var diags []diagnostic
	var trust []protocol.ACPTrustOverride
	fail := func(format string, a ...any) {
		diags = append(diags, diagnostic{err: true, msg: fmt.Sprintf(format, a...)})
	}
	positional := func(arg string) {
		if path, ok := strings.CutPrefix(arg, "@"); ok {
			o.files = append(o.files, path)
		} else {
			o.messages = append(o.messages, arg)
		}
	}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if set, ok := valueFlags[arg]; ok {
			if i+1 >= len(argv) {
				fail("%s requires a value", arg)
				continue
			}
			i++
			if d := set(&o, argv[i]); d != nil {
				diags = append(diags, *d)
			}
			continue
		}
		switch {
		case arg == "--":
			for _, rest := range argv[i+1:] {
				positional(rest)
			}
			i = len(argv)
		case arg == "--help" || arg == "-h":
			o.help = true
		case arg == "--no-leader":
			o.noLeader = true
		case arg == "--offline":
			o.offline = true
		case arg == "--no-tools" || arg == "-nt":
			o.session.NoTools = true
		case trustFlags[arg] != "":
			trust = append(trust, trustFlags[arg])
		case arg == "--print" || arg == "-p":
			o.print = true
			if i+1 < len(argv) {
				next := argv[i+1]
				if !strings.HasPrefix(next, "@") && (!strings.HasPrefix(next, "-") || strings.HasPrefix(next, "---")) {
					o.messages = append(o.messages, next)
					i++
				}
			}
		case arg == "--mode":
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
				fail("--mode requires text or json")
				continue
			}
			i++
			switch m := outputMode(argv[i]); m {
			case outputText, outputJSON:
				o.mode = m
			default:
				fail("Invalid mode %q. Valid values: text, json", argv[i])
			}
		case strings.HasPrefix(arg, "@"):
			positional(arg)
		case strings.HasPrefix(arg, "-"):
			name, _, _ := strings.Cut(arg, "=")
			fail("Unknown option: %s", name)
		default:
			positional(arg)
		}
	}

	if t, err := trustOverride(trust); err != nil {
		fail("%s", err)
	} else {
		o.trust = t
	}
	if _, err := tools.FlagSelection(o.session.Tools, o.session.NoTools); err != nil {
		fail("%s", err)
	}
	if size := o.session.PromptBytes(); size > protocol.ACPSessionPromptMaxBytes {
		fail("the prompt flags are %d bytes; the limit is %d", size, protocol.ACPSessionPromptMaxBytes)
	}
	if o.apiKey != "" && o.model == "" && o.provider != anthropic.ProviderID {
		fail("--api-key requires a model to be specified via --model")
	}
	if o.model == "" {
		switch o.provider {
		case anthropic.ProviderID:
			o.model = anthropic.ModelID
		case providers.ProviderAnthropic:
			o.model = providers.ModelClaudeSonnet46
		case providers.ProviderOpenAI:
			o.model = providers.ModelGPT55
		case providers.ProviderXAI:
			o.model = providers.ModelGrok47
		default:
			o.model = defaultModel
		}
	}
	return o, diags
}

// trustFlags are the spellings of the project trust flags.
var trustFlags = map[string]protocol.ACPTrustOverride{
	"--approve":    protocol.ACPTrustApprove,
	"-a":           protocol.ACPTrustApprove,
	"--no-approve": protocol.ACPTrustDeny,
	"-na":          protocol.ACPTrustDeny,
}

// trustOverride returns the override that the trust flags give. A flag may
// repeat; --approve together with --no-approve is an error.
func trustOverride(flags []protocol.ACPTrustOverride) (protocol.ACPTrustOverride, error) {
	var out protocol.ACPTrustOverride
	for _, f := range flags {
		if out != "" && f != out {
			return "", errors.New("use only one of --approve and --no-approve")
		}
		out = f
	}
	return out, nil
}

const usage = `ask - the Ask agent harness

Usage:
  ask [options] [--] [@files...] [messages...]
  ask auth --help
  ask models --help
  ask settings --help
  ask acp --help
  ask connect --help
  ask leader --help
  ask version [--json]

Options:
  --print, -p           Non-interactive mode: run the prompt, print the reply and exit
  --mode <mode>         Output mode: text (default) or json
  --provider <name>     Provider name: faux, alibaba-token-plan, anthropic, openai, xai,
                        or a provider of models.json (default: the scope or the saved defaultModel, else faux)
  --model <pattern>     Model: [provider/]id[@api][:level], for example anthropic/claude-sonnet-4-6:high.
                        An id, an alias or a part of an id; an id on two providers is ambiguous.
                        With --provider P the pattern is P/<pattern> (default model of P when absent)
  --models <patterns>   Models to start on and to cycle through, comma separated; repeat it to add more.
                        A pattern or a glob with *, ? or [...], with an optional :level.
                        * does not match /, so openai/* matches openai/gpt-5.5 but not
                        openrouter/qwen/qwen3; use openrouter/*/* for that. Replaces enabledModels
  --api-key <key>       API key for the provider (requires --model)
  --thinking <level>    Thinking level: off, minimal, low, medium, high, xhigh, max; wins over :level
  --offline             Do not refresh the model list from the network (as ASK_DISABLE_MODEL_REFRESH=1);
                        a leader that this command starts gets it too, a running leader keeps its setting
  --max-turns <n>       Print mode: end the run after n turns, turns of background notices included
  --system-prompt <text|file>
                        Replace the default system prompt (a file path is read now)
  --append-system-prompt <text|file>
                        Add text after the system prompt; repeat it to add more
  --tools, -t <names>   Built-in tools to keep, comma separated (for example read,grep)
  --no-tools, -nt       Keep no built-in tools
  --no-leader           TUI: run the agent in this process; do not connect to or start a leader
  --approve, -a         Trust the project settings of this folder for this run; saves nothing
  --no-approve, -na     Do not trust the project settings of this folder for this run
  --                    End option parsing; the rest are messages and @files
  --help, -h            Show this help
`
