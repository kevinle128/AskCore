package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"AskCore/pkg/protocol"
)

const versionUsage = `ask version - print the build and the protocol versions

Usage:
  ask version [--json]
  ask version --help

--json writes one JSON object on one line: name, build, leaderProtocol, acpVersion and
commitTime (a binary with a VCS stamp) or devStamp (a binary without one).
A client reads it before it starts "ask leader" from this binary.
`

// acpWireVersion is the ACP wire version that ask acp and ask leader speak.
const acpWireVersion = 1

// buildFacts holds what the binary says about its VCS stamp.
type buildFacts struct {
	revision string
	dirty    bool
	time     string // vcs.time, the commit time of the revision
	module   string // the module version, "" for a local build
}

// readBuildFacts reads the build information of this binary once.
var readBuildFacts = sync.OnceValue(func() buildFacts {
	var f buildFacts
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return f
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			f.revision = s.Value
		case "vcs.modified":
			f.dirty = s.Value == "true"
		case "vcs.time":
			f.time = s.Value
		}
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		f.module = v
	}
	return f
})

// buildIdentity names this build. It is the VCS revision when the binary has
// one (with "+dirty" for uncommitted changes), else the module version, else "dev".
func buildIdentity() string {
	f := readBuildFacts()
	switch {
	case f.revision != "":
		rev := f.revision
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if f.dirty {
			rev += "+dirty"
		}
		return rev
	case f.module != "":
		return f.module
	}
	return "dev"
}

// buildStamps tells how new this build is. A binary with a VCS stamp reports
// the commit time of its revision. A binary without one, such as a `go run`
// build, is known by the time its file was written and the hash of its bytes;
// an unreadable file gives no stamp. The result is computed once: a `go run`
// binary can be removed while its process still runs.
var buildStamps = sync.OnceValues(func() (commitTime, devStamp string) {
	if t := readBuildFacts().time; t != "" {
		return t, ""
	}
	exe, err := os.Executable()
	if err != nil {
		return "", ""
	}
	return "", devStampOf(exe)
})

// devStampOf returns "<modification time> sha256:<hex>" of a file, or "" when
// the file cannot be read.
func devStampOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano) + " sha256:" + hex.EncodeToString(h.Sum(nil))
}

// leaderHello is the register frame of a client of the given kind.
func leaderHello(kind string) protocol.LeaderRegister {
	commit, dev := buildStamps()
	return protocol.LeaderRegister{ClientKind: kind, Build: buildIdentity(), CommitTime: commit, DevStamp: dev}
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
	commit, dev := buildStamps()
	data, err := json.Marshal(protocol.VersionInfo{Name: "ask", Build: buildIdentity(), LeaderProtocol: protocol.LeaderProtocolVersion, ACPVersion: acpWireVersion, CommitTime: commit, DevStamp: dev})
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s\n", data)
	return 0
}
