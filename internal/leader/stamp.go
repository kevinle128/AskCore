package leader

import (
	"regexp"
	"strings"
	"time"

	"AskCore/pkg/protocol"
)

// buildStamp tells how new a build is. A build has a commit time (it carries a
// VCS stamp) or a dev stamp (it does not), never both.
type buildStamp struct {
	CommitTime string
	DevStamp   string
}

func stampOfHello(h protocol.LeaderRegister) buildStamp {
	return buildStamp{CommitTime: h.CommitTime, DevStamp: h.DevStamp}
}

func stampOfRegistered(r protocol.LeaderRegistered) buildStamp {
	return buildStamp{CommitTime: r.CommitTime, DevStamp: r.DevStamp}
}

func stampOfVersion(v protocol.VersionInfo) buildStamp {
	return buildStamp{CommitTime: v.CommitTime, DevStamp: v.DevStamp}
}

var devHash = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)

// parseDevStamp splits "<RFC 3339 time> sha256:<64 hex>" into the time and the
// hex hash.
func parseDevStamp(s string) (mod time.Time, hash string, ok bool) {
	when, sum, found := strings.Cut(s, " ")
	if !found {
		return time.Time{}, "", false
	}
	m := devHash.FindStringSubmatch(sum)
	if m == nil {
		return time.Time{}, "", false
	}
	mod, err := time.Parse(time.RFC3339Nano, when)
	if err != nil {
		return time.Time{}, "", false
	}
	return mod, m[1], true
}

// newerBuild tells whether the client build is strictly newer than the leader
// build. Two VCS builds compare by commit time. Two builds without a VCS stamp
// are newer when their bytes differ and the file of the client was written
// later; the same bytes are the same build. A VCS build and a build without a
// VCS stamp are never compared, and a missing or unreadable stamp keeps the
// leader.
func newerBuild(client, leader buildStamp) bool {
	switch {
	case client.CommitTime != "" && leader.CommitTime != "" && client.DevStamp == "" && leader.DevStamp == "":
		c, cerr := time.Parse(time.RFC3339, client.CommitTime)
		l, lerr := time.Parse(time.RFC3339, leader.CommitTime)
		return cerr == nil && lerr == nil && c.After(l)
	case client.DevStamp != "" && leader.DevStamp != "" && client.CommitTime == "" && leader.CommitTime == "":
		cm, ch, cok := parseDevStamp(client.DevStamp)
		lm, lh, lok := parseDevStamp(leader.DevStamp)
		return cok && lok && ch != lh && cm.After(lm)
	}
	return false
}

// otherDevBuild tells whether both builds have no VCS stamp and different bytes.
func otherDevBuild(a, b buildStamp) bool {
	_, ah, aok := parseDevStamp(a.DevStamp)
	_, bh, bok := parseDevStamp(b.DevStamp)
	return aok && bok && ah != bh
}
