package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/internal/providers/anthropic"
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
		{"-p leaves a flag", []string{"-p", "--model", "m"}, base(func(o *options) { o.print = true; o.model = "m" }), nil},
		{"-p takes a --- token", []string{"-p", "---x"}, base(func(o *options) { o.print = true; o.messages = []string{"---x"} }), nil},
		{"-p at the end", []string{"-p"}, base(func(o *options) { o.print = true }), nil},
		{"-- ends options", []string{"--", "-a", "@b", "--mode"}, base(func(o *options) {
			o.messages = []string{"-a", "--mode"}
			o.files = []string{"b"}
		}), nil},
		{"@file and message", []string{"@a.md", "hi"}, base(func(o *options) { o.files = []string{"a.md"}; o.messages = []string{"hi"} }), nil},
		{"--mode text", []string{"--mode", "text"}, base(func(o *options) { o.mode = outputText }), nil},
		{"--mode json", []string{"--mode", "json"}, base(func(o *options) { o.mode = outputJSON }), nil},
		{"--mode missing", []string{"--mode"}, base(nil), []string{"Error: --mode requires text or json"}},
		{"--mode then a flag", []string{"--mode", "-p", "hi"}, base(func(o *options) { o.print = true; o.messages = []string{"hi"} }),
			[]string{"Error: --mode requires text or json"}},
		{"--mode invalid", []string{"--mode", "x", "-p", "hi"}, base(func(o *options) { o.print = true; o.messages = []string{"hi"} }),
			[]string{`Error: Invalid mode "x". Valid values: text, json`}},
		{"--mode rpc is not an Ask mode", []string{"--mode", "rpc"}, base(nil), []string{`Error: Invalid mode "rpc". Valid values: text, json`}},
		{"--provider", []string{"--provider", "openai"}, base(func(o *options) { o.provider = "openai"; o.model = "gpt-5.5" }), nil},
		{"token plan defaults the model", []string{"--provider", anthropic.ProviderID}, base(func(o *options) {
			o.provider = anthropic.ProviderID
			o.model = anthropic.ModelID
		}), nil},
		{"token plan api key does not need --model", []string{"--provider", anthropic.ProviderID, "--api-key", "k"}, base(func(o *options) {
			o.provider = anthropic.ProviderID
			o.model = anthropic.ModelID
			o.apiKey = "k"
		}), nil},
		{"--provider missing", []string{"--provider"}, base(nil), []string{"Error: --provider requires a value"}},
		{"--model", []string{"--model", "faux-2"}, base(func(o *options) { o.model = "faux-2" }), nil},
		{"--model missing", []string{"--model"}, base(nil), []string{"Error: --model requires a value"}},
		{"--api-key with --model", []string{"--api-key", "k", "--model", "faux-1"}, base(func(o *options) { o.apiKey = "k" }), nil},
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
		{"a value flag takes a dash token, as in Pi", []string{"--model", "--nope"}, base(func(o *options) { o.model = "--nope" }), nil},
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
