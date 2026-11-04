package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"AskCore/internal/app"
	"AskCore/internal/settings"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

const settingsUsage = `Usage:
  ask settings show [--json] [--approve | --no-approve]
  ask settings --help

Print the settings of the current folder: the project trust decision, each
known key with its value and the layer that sets it (default, user or project),
and each problem that the read found. The command reads files only. It writes
nothing, and it starts no leader and no database. It does not read the project
settings of a project that is not trusted.

Options:
  --json                Print one JSON object
  --approve, -a         Show the result of a run that trusts the project
  --no-approve, -na     Show the result of a run that does not trust the project

Exit codes: 0 done, 1 usage or read error, 130 SIGINT, 143 SIGTERM, 129 SIGHUP.
`

type settingsOptions struct {
	help  bool
	json  bool
	trust protocol.ACPTrustOverride
}

// parseSettingsArgs reads the arguments after "settings". Only the show
// subcommand exists.
func parseSettingsArgs(argv []string) (settingsOptions, error) {
	var o settingsOptions
	if len(argv) == 0 {
		return o, errors.New("settings requires show; use ask settings --help")
	}
	if argv[0] == "--help" || argv[0] == "-h" {
		o.help = true
		return o, nil
	}
	if argv[0] != "show" {
		return o, fmt.Errorf("unknown settings command %q; use ask settings --help", argv[0])
	}
	var flags []protocol.ACPTrustOverride
	for _, arg := range argv[1:] {
		switch {
		case arg == "--help" || arg == "-h":
			o.help = true
		case arg == "--json":
			o.json = true
		case trustFlags[arg] != "":
			flags = append(flags, trustFlags[arg])
		default:
			return o, fmt.Errorf("settings show does not accept %q; use ask settings --help", arg)
		}
	}
	trust, err := trustOverride(flags)
	if err != nil {
		return o, err
	}
	o.trust = trust
	return o, nil
}

// runSettingsCommand runs `ask settings`. Like `ask models`, it starts no
// leader and no database. A signal cancels the read and ends the command with
// the exit code of the signal.
func runSettingsCommand(argv []string, stdout, stderr io.Writer, deps runDependencies, sigs <-chan os.Signal) int {
	o, err := parseSettingsArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.help {
		if _, err := io.WriteString(stdout, settingsUsage); err != nil {
			return 1
		}
		return 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runSettingsShow(ctx, o, stdout, stderr, deps) }()
	select {
	case code := <-done:
		return code
	case sig := <-sigs:
		cancel()
		select {
		case <-done:
		case <-time.After(abortGrace):
		}
		return signalExitCodes[sig]
	}
}

// Words of settingsProject.Layer.
const (
	projectLayerLoaded    = "loaded"
	projectLayerUntrusted = "untrusted"
	projectLayerNone      = "none"
)

// settingsDump is the --json shape of `ask settings show`. docs/cli.md
// documents it; a change of a field name is a change of the public contract.
type settingsDump struct {
	Home        string               `json:"home"`
	UserFile    string               `json:"userFile"`
	Project     settingsProject      `json:"project"`
	Settings    []settingsValue      `json:"settings"`
	Diagnostics []settingsDiagnostic `json:"diagnostics"`
}

type settingsProject struct {
	Root string `json:"root"`
	// Layer is "loaded", "untrusted" (the file is not read) or "none" (the
	// folder has no project layer, because its .ask folder is the Ask home).
	Layer   string `json:"layer"`
	Trusted bool   `json:"trusted"`
	// Source is a workspace.TrustSource word.
	Source string `json:"source"`
	// Entry is the saved trust entry that applies, or empty.
	Entry   string   `json:"entry"`
	Gated   []string `json:"gated"`
	Warning string   `json:"warning"`
}

type settingsValue struct {
	Key string `json:"key"`
	// Value is the effective value. An empty defaultModel means that no
	// default model is set.
	Value  string `json:"value"`
	Source string `json:"source"`
}

type settingsDiagnostic struct {
	Layer string `json:"layer"`
	Path  string `json:"path"`
	Key   string `json:"key"`
	Text  string `json:"text"`
}

// runSettingsShow decides the project trust of the cwd as print mode does,
// with no UI, then reads the settings layers. The decision comes first, so an
// untrusted project settings file is never opened.
func runSettingsShow(ctx context.Context, o settingsOptions, stdout, stderr io.Writer, deps runDependencies) int {
	dump, err := loadSettingsDump(ctx, deps.getenv("ASK_HOME"), o.trust)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	var out []byte
	if o.json {
		out, err = json.MarshalIndent(dump, "", "  ")
		if err != nil {
			report(stderr, "Error:", err)
			return 1
		}
		out = append(out, '\n')
	} else {
		out = []byte(dump.text())
	}
	if _, err := stdout.Write(out); err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	return 0
}

func loadSettingsDump(ctx context.Context, askHome string, trust protocol.ACPTrustOverride) (settingsDump, error) {
	home, err := settings.ResolveHome(askHome)
	if err != nil {
		return settingsDump{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return settingsDump{}, err
	}
	// Without a user home, every .agents/skills folder counts as a project
	// resource, which fails closed.
	userHome, _ := os.UserHomeDir()
	outcome, eff, err := app.LoadSessionSettings(ctx, home, userHome, cwd, workspace.Override(trust))
	if err != nil {
		return settingsDump{}, err
	}
	d := settingsDump{
		Home:     home,
		UserFile: filepath.Join(home, "settings.json"),
		Project: settingsProject{
			Root:    outcome.Root,
			Trusted: outcome.Decision.Trusted,
			Source:  string(outcome.Decision.Source),
			Gated:   append([]string{}, outcome.Gated...),
			Warning: outcome.Warning,
		},
		Settings:    make([]settingsValue, 0, 3),
		Diagnostics: make([]settingsDiagnostic, 0, len(eff.Diagnostics)),
	}
	switch {
	case !outcome.HasLayer:
		d.Project.Layer = projectLayerNone
	case outcome.Decision.Trusted:
		d.Project.Layer = projectLayerLoaded
	default:
		d.Project.Layer = projectLayerUntrusted
	}
	if outcome.Entry.Found {
		d.Project.Entry = outcome.Entry.Path
	}
	// Only the shipped keys, in a fixed order. Other keys of the files are not
	// printed, because they can hold values that are not for the screen.
	for _, v := range []struct{ key, value string }{
		{"defaultModel", eff.DefaultModel},
		{"thinkingFold", string(eff.ThinkingFold)},
		{"defaultProjectTrust", string(eff.DefaultProjectTrust)},
	} {
		source := eff.Sources[v.key]
		if source == "" {
			source = settings.LayerDefault
		}
		d.Settings = append(d.Settings, settingsValue{Key: v.key, Value: v.value, Source: string(source)})
	}
	for _, diag := range eff.Diagnostics {
		d.Diagnostics = append(d.Diagnostics, settingsDiagnostic{Layer: string(diag.Layer), Path: diag.Path, Key: diag.Key, Text: diag.Text})
	}
	return d, nil
}

// text returns the plain text form of the dump.
func (d settingsDump) text() string {
	var b strings.Builder
	trust := "untrusted"
	if d.Project.Trusted {
		trust = "trusted"
	}
	trust += " (" + d.Project.Source
	if d.Project.Entry != "" {
		trust += ", entry " + d.Project.Entry
	}
	trust += ")"
	gated := "none"
	if len(d.Project.Gated) > 0 {
		gated = strings.Join(d.Project.Gated, ", ")
	}
	layer := "loaded"
	switch d.Project.Layer {
	case projectLayerUntrusted:
		layer = "not read (untrusted)"
	case projectLayerNone:
		layer = "none (the .ask folder of this folder is the Ask home)"
	}
	rows := [][2]string{
		{"Ask home", d.Home},
		{"User settings", d.UserFile},
		{"Project root", d.Project.Root},
		{"Project trust", trust},
		{"Gated files", gated},
		{"Project settings", layer},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "%-17s %s\n", r[0]+":", r[1])
	}
	b.WriteString("\nSettings:\n")
	for _, v := range d.Settings {
		value := v.Value
		if value == "" {
			value = "(none)"
		}
		fmt.Fprintf(&b, "  %-20s %-32s %s\n", v.Key, value, v.Source)
	}
	if d.Project.Warning != "" {
		b.WriteString("\nWarning: " + d.Project.Warning + "\n")
	}
	if len(d.Diagnostics) > 0 {
		b.WriteString("\nProblems:\n")
		for _, diag := range d.Diagnostics {
			line := "  " + diag.Layer + " " + diag.Path
			if diag.Key != "" {
				line += ": " + diag.Key
			}
			b.WriteString(line + ": " + diag.Text + "\n")
		}
	}
	return b.String()
}
