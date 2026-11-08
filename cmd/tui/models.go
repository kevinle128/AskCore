package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"AskCore/internal/app"
	"AskCore/internal/auth"
)

const modelsUsage = `Usage:
  ask models refresh [--provider <id>]

Read the model lists of the signed-in accounts and the models.dev metadata
now, and write them to <ASK_HOME>/model-cache.json. No leader is needed.
ASK_DISABLE_MODEL_REFRESH does not stop this command.
Output: one line for each provider and one metadata line.
`

type modelsOptions struct {
	help     bool
	provider string
}

func parseModelsArgs(argv []string) (modelsOptions, error) {
	var o modelsOptions
	if len(argv) == 0 {
		return o, errors.New("models requires refresh")
	}
	if argv[0] == "--help" || argv[0] == "-h" {
		o.help = true
		return o, nil
	}
	if argv[0] != "refresh" {
		return o, errors.New("models requires refresh")
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.provider, "provider", "", "provider")
	fs.BoolVar(&o.help, "help", false, "help")
	fs.BoolVar(&o.help, "h", false, "help")
	if err := fs.Parse(argv[1:]); err != nil {
		return o, errors.New("invalid models option; use ask models --help")
	}
	if fs.NArg() != 0 {
		return o, errors.New("models refresh does not accept arguments")
	}
	if o.provider != "" && !slices.Contains(app.ModelListProviders(), o.provider) {
		return o, errors.New("unsupported model provider; use one of " + strings.Join(app.ModelListProviders(), ", "))
	}
	return o, nil
}

// runModelsCommand runs `ask models`. Like `ask auth`, it starts no leader and
// no database: it composes the native credentials and refreshes the cache in
// this process.
func runModelsCommand(argv []string, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	o, err := parseModelsArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.help {
		if _, err := io.WriteString(stdout, modelsUsage); err != nil {
			return 1
		}
		return 0
	}
	env := func(k string) (string, bool) { v := deps.getenv(k); return v, v != "" }
	home := deps.getenv("ASK_HOME")
	service, err := app.NewNativeAuth(home, env, auth.NativeOptions{HTTPClient: deps.authHTTP, Now: deps.now, Wait: deps.wait})
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	// The command is the explicit refresh, so it ignores the switch that turns
	// the automatic refresh off. It sets no logger, so the refresher logs
	// nothing: a vendor error can hold vendor text, and the output shows
	// states only.
	cfg := app.ModelRefresh{Enabled: true, HTTP: modelHTTP(deps), Now: deps.now}
	return runWithAuthSignals(service, sigs, stderr, func(ctx context.Context) int {
		return runModelsRefresh(ctx, o, home, service, cfg, stdout, stderr)
	})
}

func runModelsRefresh(ctx context.Context, o modelsOptions, home string, service *auth.Service, cfg app.ModelRefresh, stdout, stderr io.Writer) int {
	var names []string
	if o.provider != "" {
		names = []string{o.provider}
	}
	res, err := app.RefreshModelLists(ctx, home, service, cfg, names...)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	width := len("metadata")
	for _, p := range res.Providers {
		width = max(width, len(p.Provider))
	}
	var b strings.Builder
	for _, p := range res.Providers {
		fmt.Fprintf(&b, "%-*s  %d models  %s\n", width, p.Provider, p.Models, p.State)
	}
	fmt.Fprintf(&b, "%-*s  %s\n", width, "metadata", res.Metadata)
	if _, err := io.WriteString(stdout, b.String()); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	return 0
}
