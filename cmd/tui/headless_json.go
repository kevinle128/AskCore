package main

import (
	"AskCore/internal/agent"
	"AskCore/pkg/protocol"
)

// streamJSON writes every event of ag to out as one JSON line, as Pi's JSON
// mode does. The stream ends with agent_settled, so it leaves out
// agent_disposed, which only listeners in Go get. There is no session header
// until sessions persist (H8). The listener blocks on the write, so a slow
// reader stalls the run. A listener failure does not stop a run, so any failure of this listener, a failed write
// or an event that cannot be encoded, becomes the sticky error of out and
// calls stop: the run owner ends the run through it, and the exit code is 1.
func streamJSON(ag *agent.Agent, out *protocolOut, stop func()) (unsubscribe func()) {
	out.setOnError(func(error) { stop() })
	w := protocol.NewJSONLWriter(out)
	return ag.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.AgentDisposed); ok {
			return nil
		}
		err := w.Write(ev)
		if err != nil {
			out.fail(err)
		}
		return err
	})
}
