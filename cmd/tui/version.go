package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"AskCore/pkg/protocol"
)

const versionUsage = `ask version - print the build and the protocol versions

Usage:
  ask version [--json]
  ask version --help

--json writes one JSON object on one line: name, build, leaderProtocol and acpVersion.
A client reads it before it starts "ask leader" from this binary.
`

// acpWireVersion is the ACP wire version that ask acp and ask leader speak.
const acpWireVersion = 1

// buildIdentity names this build. It is the VCS revision when the binary has
// one (with "+dirty" for uncommitted changes), else the module version, else "dev".
func buildIdentity() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	switch {
	case rev != "":
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if dirty {
			rev += "+dirty"
		}
		return rev
	case info.Main.Version != "" && info.Main.Version != "(devel)":
		return info.Main.Version
	}
	return "dev"
}

// runVersion implements "ask version".
func runVersion(argv []string, stdout, stderr io.Writer) int {
	jsonOut := false
	for _, a := range argv {
		switch a {
		case "--json":
			jsonOut = true
		case "--help", "-h":
			_, _ = io.WriteString(stdout, versionUsage)
			return 0
		default:
			report(stderr, "Error:", errors.New("unknown argument "+a+"; use ask version --help"))
			return 1
		}
	}
	if !jsonOut {
		_, _ = fmt.Fprintf(stdout, "ask %s\n", buildIdentity())
		return 0
	}
	data, err := json.Marshal(protocol.VersionInfo{Name: "ask", Build: buildIdentity(), LeaderProtocol: protocol.LeaderProtocolVersion, ACPVersion: acpWireVersion})
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s\n", data)
	return 0
}
