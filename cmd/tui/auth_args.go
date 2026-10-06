package main

import (
	"errors"
	"flag"
	"io"
)

type authOptions struct {
	action, provider, method, interaction string
	newAccount, help                      bool
}

const authUsage = `Usage:
  ask auth login --provider <id> --method <id> [--interaction browser|copy-code] [--new-account]
  ask auth logout --provider <id> [--method <id>]

Providers: anthropic, openai, xai, alibaba-token-plan
Methods: api-key, anthropic-oauth, openai-chatgpt, xai-oauth
Read API keys and manual callback input from private stdin.
Login runs on the inference host and its ASK_HOME.
Logout deletes the local record only; environment keys can still be used.
`

func parseAuthArgs(argv []string) (authOptions, error) {
	var o authOptions
	if len(argv) == 0 {
		return o, errors.New("auth requires login or logout")
	}
	if argv[0] == "--help" || argv[0] == "-h" {
		o.help = true
		return o, nil
	}
	o.action = argv[0]
	if o.action != "login" && o.action != "logout" {
		return o, errors.New("auth requires login or logout")
	}
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.provider, "provider", "", "provider")
	fs.StringVar(&o.method, "method", "", "method")
	fs.StringVar(&o.interaction, "interaction", "", "interaction")
	fs.BoolVar(&o.newAccount, "new-account", false, "new account")
	fs.BoolVar(&o.help, "help", false, "help")
	fs.BoolVar(&o.help, "h", false, "help")
	if err := fs.Parse(argv[1:]); err != nil {
		return o, errors.New("invalid auth option; use ask auth --help")
	}
	if fs.NArg() != 0 {
		return o, errors.New("auth does not accept prompt or credential arguments")
	}
	if o.help {
		return o, nil
	}
	if o.provider == "" {
		return o, errors.New("auth requires --provider")
	}
	if o.action == "logout" && (o.interaction != "" || o.newAccount) {
		return o, errors.New("logout does not accept login options")
	}
	if o.newAccount && (o.provider != "openai" || o.method != "openai-chatgpt") {
		return o, errors.New("--new-account requires openai-chatgpt login")
	}
	if o.interaction != "" && (o.method != "anthropic-oauth" || (o.interaction != "browser" && o.interaction != "copy-code")) {
		return o, errors.New("--interaction requires Anthropic browser or copy-code login")
	}
	return o, nil
}
