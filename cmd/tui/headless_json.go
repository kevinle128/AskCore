package main

import (
	"io"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"
)

// streamJSON writes every event of ag to w as one JSON line, as Pi's JSON
// mode does. There is no session header until sessions persist (H8). The
// listener blocks on the write, so a slow reader stalls the run, and a write
// error fails the run.
func streamJSON(ag *agent.Agent, w io.Writer) (unsubscribe func()) {
	return ag.Subscribe(protocol.NewJSONLWriter(w).Write)
}
