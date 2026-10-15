package acp

import (
	"strconv"

	"AskCore/pkg/protocol"
)

// eventIDs are the cycle and attempt that an event belongs to. Only some
// events name them, so the writer of a session keeps the last values.
type eventIDs struct {
	cycleID   string
	attemptID string
}

// note takes the identifiers that ev names.
func (s *eventIDs) note(ev protocol.Event) {
	switch e := ev.(type) {
	case *protocol.CycleStart:
		s.cycleID, s.attemptID = e.CycleID, ""
	case *protocol.CycleEnd:
		s.cycleID, s.attemptID = "", ""
	case *protocol.AttemptStart:
		s.attemptID = e.AttemptID
		if e.CycleID != "" {
			s.cycleID = e.CycleID
		}
	case *protocol.AttemptEnd:
		s.attemptID = ""
	}
}

// frameMeta is the _meta of one outbound frame. Counters go out as decimal
// text, because a number in typed _meta passes through float64. One source
// event can give several frames; index and count tell which one this is.
func frameMeta(epoch string, env protocol.Envelope, ids eventIDs, index, count int) map[string]any {
	ask := map[string]any{
		"epoch":      epoch,
		"seq":        strconv.FormatUint(env.Seq, 10),
		"frameIndex": index,
		"frameCount": count,
	}
	if env.RunID != "" {
		ask["runId"] = env.RunID
	}
	if ids.cycleID != "" {
		ask["cycleId"] = ids.cycleID
	}
	if ids.attemptID != "" {
		ask["attemptId"] = ids.attemptID
	}
	return map[string]any{"ask": ask}
}
