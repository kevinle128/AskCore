package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/anthropic"
	"AskCore/pkg/protocol"
)

func TestArgs(t *testing.T) {
	base := func(edit func(o *options)) options {
		o := options{provider: "faux", model: "faux-1"}
		if edit != nil {
			edit(&o)
		}
		return o
	}
	tests := []struct {
		name  string
		argv  []string
		want  options
		diags []string
	}{
		{"defaults", nil, base(nil), nil},
		{"positional messages", []string{"a", "b"}, base(func(o *options) { o.messages = []string{"a", "b"} }), nil},
		{"-p eats the next token", []string{"-p", "hello"}, base(func(o *options) { o.print = true; o.messages = []string{"hello"} }), nil},
		{"--print is -p", []string{"--print", "hello"}, base(func(o *options) { o.print = true; o.messages = []string{"hello"} }), nil},
		{"-p leaves an @file", []string{"-p", "@f.txt"}, base(func(o *options) { o.print = true; o.files = []string{"f.txt"} }), nil},
		{"-p leaves a flag", []string{"-p", "--model", "m"}, base(func(o *options) { o.print = true; o.model = "m"; o.modelGiven = true }), nil},
		{"-p takes a --- token", []string{"-p", "---x"}, base(func(o *options) { o.print = true; o.messages = []string{"---x"} }), nil},
		{"-p at the end", []string{"-p"}, base(func(o *options) { o.print = true }), nil},
		{"-- ends options", []string{"--", "-a", "@b", "--mode"}, base(func(o *options) {
			o.messages = []string{"-a", "--mode"}
			o.files = []string{"b"}
		}), nil},
		{"@file and message", []string{"@a.md", "hi"}, base(func(o *options) { o.files = []string{"a.md"}; o.messages = []string{"hi"} }), nil},
		{"--no-leader", []string{"--no-leader"}, base(func(o *options) { o.noLeader = true }), nil},
		{"--mode text", []string{"--mode", "text"}, base(func(o *options) { o.mode = outputText }), nil},
		{"--mode json", []string{"--mode", "json"}, base(func(o *options) { o.mode = outputJSON }), nil},
		{"--mode missing", []string{"--mode"}, base(nil), []string{"Error: --mode requires text or json"}},
		{"--mode then a flag", []string{"--mode", "-p", "hi"}, base(func(o *options) { o.print = true; o.messages = []string{"hi"} }),
			[]string{"Error: --mode requires text or json"}},
		{"--mode invalid", []string{"--mode", "x", "-p", "hi"}, base(func(o *options) { o.print = true; o.messages = []string{"hi"} }),
			[]string{`Error: Invalid mode "x". Valid values: text, json`}},
		{"--mode rpc is not an Ask mode", []string{"--mode", "rpc"}, base(nil), []string{`Error: Invalid mode "rpc". Valid values: text, json`}},
		{"--provider", []string{"--provider", "openai"}, base(func(o *options) { o.provider = "openai"; o.model = "gpt-5.5"; o.modelGiven = true }), nil},
		{"token plan defaults the model", []string{"--provider", anthropic.ProviderID}, base(func(o *options) {
			o.provider = anthropic.ProviderID
			o.model = anthropic.ModelID
			o.modelGiven = true
		}), nil},
		{"token plan api key does not need --model", []string{"--provider", anthropic.ProviderID, "--api-key", "k"}, base(func(o *options) {
			o.provider = anthropic.ProviderID
			o.model = anthropic.ModelID
			o.modelGiven = true
			o.apiKey = "k"
		}), nil},
		{"--provider missing", []string{"--provider"}, base(nil), []string{"Error: --provider requires a value"}},
		{"--model", []string{"--model", "faux-2"}, base(func(o *options) { o.model = "faux-2"; o.modelGiven = true }), nil},
		{"--model missing", []string{"--model"}, base(nil), []string{"Error: --model requires a value"}},
		{"--api-key with --model", []string{"--api-key", "k", "--model", "faux-1"}, base(func(o *options) { o.apiKey = "k"; o.modelGiven = true }), nil},
		{"--api-key missing", []string{"--api-key"}, base(nil), []string{"Error: --api-key requires a value"}},
		{"--api-key without a model", []string{"--api-key", "k"}, base(func(o *options) { o.apiKey = "k" }),
			[]string{"Error: --api-key requires a model to be specified via --model"}},
		{"--thinking", []string{"--thinking", "high"}, base(func(o *options) { o.thinking = "high" }), nil},
		{"--thinking invalid warns", []string{"--thinking", "huge", "hi"}, base(func(o *options) { o.messages = []string{"hi"} }),
			[]string{`Warning: Invalid thinking level "huge". Valid values: off, minimal, low, medium, high, xhigh, max`}},
		{"--thinking missing", []string{"--thinking"}, base(nil), []string{"Error: --thinking requires a value"}},
		{"unknown --flag", []string{"--nope", "-p", "hi"}, base(func(o *options) { o.print = true; o.messages = []string{"hi"} }),
			[]string{"Error: Unknown option: --nope"}},
		{"unknown --flag=value", []string{"--nope=1"}, base(nil), []string{"Error: Unknown option: --nope"}},
		{"unknown -x", []string{"-x"}, base(nil), []string{"Error: Unknown option: -x"}},
		{"--help", []string{"--help"}, base(func(o *options) { o.help = true }), nil},
		{"-h", []string{"-h"}, base(func(o *options) { o.help = true }), nil},
		{"a value flag takes a dash token, as in Pi", []string{"--model", "--nope"}, base(func(o *options) { o.model = "--nope"; o.modelGiven = true }), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := parseArgs(tt.argv)
			assert.Equal(t, tt.want, got)
			var texts []string
			for _, d := range diags {
				texts = append(texts, d.String())
			}
			assert.Equal(t, tt.diags, texts)
		})
	}
}

func TestArgsSelectMode(t *testing.T) {
	tests := []struct {
		name             string
		o                options
		stdinTTY, outTTY bool
		want             runMode
	}{
		{"both TTYs", options{}, true, true, modeInteractive},
		{"--mode text on TTYs stays interactive", options{mode: outputText}, true, true, modeInteractive},
		{"-p on TTYs", options{print: true}, true, true, modePrint},
		{"piped stdin", options{}, false, true, modePrint},
		{"piped stdout", options{}, true, false, modePrint},
		{"--mode json wins over -p", options{mode: outputJSON, print: true}, false, false, modeJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectMode(tt.o, tt.stdinTTY, tt.outTTY))
		})
	}
}

func TestParseApproveFlags(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want protocol.ACPTrustOverride
	}{
		{nil, ""},
		{[]string{"--approve", "-p", "hi"}, protocol.ACPTrustApprove},
		{[]string{"-a"}, protocol.ACPTrustApprove},
		{[]string{"--no-approve"}, protocol.ACPTrustDeny},
		{[]string{"-p", "hi", "-na"}, protocol.ACPTrustDeny},
		{[]string{"-a", "--approve"}, protocol.ACPTrustApprove},
	} {
		o, diags := parseArgs(tc.argv)
		assert.Empty(t, diags, "argv %v", tc.argv)
		assert.Equal(t, tc.want, o.trust, "argv %v", tc.argv)
	}
	for _, argv := range [][]string{{"--approve", "--no-approve"}, {"-na", "-a"}, {"-a", "-na", "-p", "x"}} {
		_, diags := parseArgs(argv)
		assert.Equal(t, []diagnostic{{err: true, msg: "use only one of --approve and --no-approve"}}, diags, "argv %v", argv)
	}
	// -p does not take a flag as its message.
	o, _ := parseArgs([]string{"-p", "-a"})
	assert.Empty(t, o.messages)
	assert.Equal(t, protocol.ACPTrustApprove, o.trust)
	assert.Contains(t, usage, "--approve, -a")
	assert.Contains(t, usage, "--no-approve, -na")
}

func TestParseSessionFlags(t *testing.T) {
	dir := t.TempDir()
	promptFile := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(promptFile, []byte("from a file\n"), 0o644))
	big := filepath.Join(dir, "big.md")
	require.NoError(t, os.WriteFile(big, []byte(strings.Repeat("x", protocol.ACPSessionPromptMaxBytes+1)), 0o644))
	third := filepath.Join(dir, "third.md")
	require.NoError(t, os.WriteFile(third, []byte(strings.Repeat("y", protocol.ACPSessionPromptMaxBytes/2+1)), 0o644))

	for _, tc := range []struct {
		name  string
		argv  []string
		want  protocol.ACPSessionOptions
		diags []string
	}{
		{"none", nil, protocol.ACPSessionOptions{}, nil},
		{"system prompt text", []string{"--system-prompt", "be brief"}, protocol.ACPSessionOptions{SystemPrompt: "be brief"}, nil},
		{"system prompt file is read", []string{"--system-prompt", promptFile}, protocol.ACPSessionOptions{SystemPrompt: "from a file\n"}, nil},
		{"a missing path is text", []string{"--system-prompt", "no/such/file.md"}, protocol.ACPSessionOptions{SystemPrompt: "no/such/file.md"}, nil},
		{"a folder is text", []string{"--system-prompt", dir}, protocol.ACPSessionOptions{SystemPrompt: dir}, nil},
		{"append repeats", []string{"--append-system-prompt", "one", "--append-system-prompt", promptFile}, protocol.ACPSessionOptions{AppendSystemPrompt: []string{"one", "from a file\n"}}, nil},
		{"tools list", []string{"--tools", "read,grep"}, protocol.ACPSessionOptions{Tools: []string{"read", "grep"}}, nil},
		{"-t repeats", []string{"-t", "read", "-t", "ls, bash"}, protocol.ACPSessionOptions{Tools: []string{"read", "ls", "bash"}}, nil},
		{"no tools", []string{"-nt"}, protocol.ACPSessionOptions{NoTools: true}, nil},
		{"--no-tools", []string{"--no-tools"}, protocol.ACPSessionOptions{NoTools: true}, nil},
		{"tools with no tools", []string{"-t", "read", "-nt"}, protocol.ACPSessionOptions{Tools: []string{"read"}, NoTools: true},
			[]string{"Error: --tools and --no-tools cannot be used together"}},
		{"empty tool name", []string{"-t", ""}, protocol.ACPSessionOptions{Tools: []string{""}}, []string{`Error: --tools: invalid tool name ""`}},
		{"bad tool syntax", []string{"-t", "Read"}, protocol.ACPSessionOptions{Tools: []string{"Read"}}, []string{`Error: --tools: invalid tool name "Read"`}},
		{"tool needs a value", []string{"-t"}, protocol.ACPSessionOptions{}, []string{"Error: -t requires a value"}},
		{"a prompt file over the cap", []string{"--system-prompt", big}, protocol.ACPSessionOptions{SystemPrompt: strings.Repeat("x", protocol.ACPSessionPromptMaxBytes+1)},
			[]string{"Error: the prompt flags are 262145 bytes; the limit is 262144"}},
		{"the cap covers all prompt flags", []string{"--system-prompt", third, "--append-system-prompt", third},
			protocol.ACPSessionOptions{SystemPrompt: strings.Repeat("y", protocol.ACPSessionPromptMaxBytes/2+1), AppendSystemPrompt: []string{strings.Repeat("y", protocol.ACPSessionPromptMaxBytes/2+1)}},
			[]string{"Error: the prompt flags are 262146 bytes; the limit is 262144"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, diags := parseArgs(tc.argv)
			assert.Equal(t, tc.want, o.session)
			var texts []string
			for _, d := range diags {
				texts = append(texts, d.String())
			}
			assert.Equal(t, tc.diags, texts)
		})
	}
	for _, flag := range []string{"--system-prompt <text|file>", "--append-system-prompt <text|file>", "--tools, -t <names>", "--no-tools, -nt"} {
		assert.Contains(t, usage, flag)
	}
}

func TestACPCommandStillRejectsSessionFlags(t *testing.T) {
	for _, flag := range []string{"--system-prompt", "--append-system-prompt", "--tools", "-t", "-nt", "--no-tools"} {
		_, _, err := parseACPArgs([]string{flag})
		assert.Error(t, err, flag)
	}
}
