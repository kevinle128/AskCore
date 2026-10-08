package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
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
	"--provider": func(o *options, v string) *diagnostic { o.provider = v; return nil },
	"--model":    func(o *options, v string) *diagnostic { o.model = v; return nil },
	"--api-key":  func(o *options, v string) *diagnostic { o.apiKey = v; return nil },
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
}

// parseArgs parses argv without the program name. Unlike Pi, an unknown
// --flag is an error, because Ask has no extension flags yet.
func parseArgs(argv []string) (options, []diagnostic) {
	o := options{provider: defaultProvider}
	var diags []diagnostic
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

const usage = `ask - the Ask agent harness

Usage:
  ask [options] [--] [@files...] [messages...]
  ask auth --help
  ask acp --help

Options:
  --print, -p           Non-interactive mode: run the prompt, print the reply and exit
  --mode <mode>         Output mode: text (default) or json
  --provider <name>     Provider name: faux (default), alibaba-token-plan, anthropic, openai, xai
  --model <id>          Model id (default: faux-1, or deepseek-v4.1-flash for alibaba-token-plan)
  --api-key <key>       API key for the provider (requires --model)
  --thinking <level>    Thinking level: off, minimal, low, medium, high, xhigh, max
  --                    End option parsing; the rest are messages and @files
  --help, -h            Show this help
`
