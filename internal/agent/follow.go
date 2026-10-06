package agent

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"

	"AskCore/internal/bus"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// trackedLog is the session log of an Agent. It remembers where the last
// Append ended, so the publication of an event can note how much of the log is
// committed without a copy of the log.
type trackedLog struct {
	sessions.Writer
	end atomic.Int64
}

func newTrackedLog(w sessions.Writer) *trackedLog {
	t := &trackedLog{Writer: w}
	t.end.Store(int64(len(w.Entries())))
	return t
}

// Append writes through and records the end of the commit.
func (t *trackedLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	ref, err := t.Writer.Append(entries...)
	if err == nil {
		t.end.Store(int64(ref.End))
	}
	return ref, err
}

// published is the cut that Follow reads: the last event given to the
// listeners, how much of the log was committed at that moment, and the
// assistant message that was streaming. The driver changes all fields in one
// critical section of Agent.pubMu, after the listeners ran.
type published struct {
	epoch       string
	seq         uint64
	commitIndex int
	writer      *trackedLog
	// stream rebuilds the assistant message that message_start opened and
	// message_end has not closed. It is nil when no message is open.
	stream    *protocol.Builder
	attemptID string
}

// Cursor says where a client stopped: the epoch of the conversation and the seq
// of the last event it has. Reset starts a new epoch.
type Cursor struct {
	Epoch string
	Seq   uint64
}

// StreamBaseline is the assistant message that streams at the cut. Message is a
// copy of what the model sent up to Seq, and Open holds the raw bytes of each
// block that is still open, among them the argument bytes of a tool call, which
// Message does not show. Seed a protocol.Builder with Builder.ResumeFrom and
// apply the events above Seq to it.
type StreamBaseline struct {
	AttemptID string
	protocol.Partial
	Seq uint64
}

// Follow is what Agent.Follow returns: a consistent cut of the log and the
// events after it.
type Follow struct {
	// Cursor is the cut: Entries holds what was committed when the event with
	// Cursor.Seq was published, and Events starts after it.
	Cursor Cursor
	// Entries is the snapshot of the log. It is nil when Resumed.
	Entries []sessions.Entry
	// Stream is set when an assistant message was open at the cut.
	Stream *StreamBaseline
	// Resumed is true when the cursor given was valid: Events starts after it
	// and there is no snapshot.
	Resumed bool
	// Resync is true when a cursor was given and refused. The follow holds a
	// new snapshot instead.
	Resync bool
	// Events gives the events after the cut as encoded wire events. It ends with
	// bus.ErrResync when the client must take a new snapshot.
	Events *bus.Follower
}

// FollowStats are the counters of observation.
type FollowStats struct {
	// ListenerFailures counts contained listener errors and panics.
	ListenerFailures uint64
	// Gaps counts breaks of the event sequence in the replay ring.
	Gaps uint64
	// DetachedFollowers counts followers that ended with bus.ErrResync.
	DetachedFollowers uint64
}

// Stats returns the counters of observation.
func (a *Agent) Stats() FollowStats {
	s := FollowStats{ListenerFailures: a.listenerFailures.Load()}
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	if a.ring != nil {
		rs := a.ring.Stats()
		s.Gaps, s.DetachedFollowers = rs.Gaps, rs.Detached
	}
	return s
}

// Follow joins the event stream for a client that is not a listener. The
// snapshot and the cursor come from one observation of the Agent: the log as
// committed when the last published event was published, and the events after
// it. A message that is committed but not yet published is not in the snapshot;
// its event reaches the follower once. When an assistant message streams, the
// follow carries a baseline of it, and the events above its seq complete it.
//
// A zero cursor asks for a snapshot. A cursor of this epoch whose later events
// are still held gives those events and no snapshot. Any other cursor is
// refused: Resync is set and the follow holds a new snapshot. The replay ring
// starts with the first call, so an earlier cursor is always refused.
//
// Only events reach a follower live. An entry that has no event, such as a
// SystemSnapshot or a RequestDelta, reaches a client only through a later
// snapshot. The ring stores an event up to bus.Limits.EventBytes, 256 KiB by
// default. A larger event, for example a turn_end or agent_end that carries a
// very large message, is not stored and not delivered: the followers that reach
// it end with bus.ErrResync and take a new snapshot. Local listeners always get
// the whole event.
func (a *Agent) Follow(cursor Cursor) *Follow {
	a.pubMu.Lock()
	if a.ring == nil {
		a.ring = bus.NewRing(a.cfg.FollowLimits, a.pub.epoch, a.pub.seq)
	}
	f := &Follow{}
	if cursor.Epoch != "" {
		if resumed, err := a.ring.Resume(a.cfg.SessionID, cursor.Epoch, cursor.Seq); err == nil {
			f.Cursor, f.Resumed, f.Events = cursor, true, resumed
		} else {
			f.Resync = true
		}
	}
	var writer *trackedLog
	var commit int
	if f.Events == nil {
		f.Events = a.ring.Follow(a.cfg.SessionID)
		f.Cursor = Cursor{Epoch: a.pub.epoch, Seq: a.pub.seq}
		if a.pub.stream != nil {
			f.Stream = &StreamBaseline{AttemptID: a.pub.attemptID, Partial: a.pub.stream.Partial(), Seq: a.pub.seq}
		}
		writer, commit = a.pub.writer, a.pub.commitIndex
	}
	a.pubMu.Unlock()
	if writer != nil {
		entries := writer.Entries()
		f.Entries = entries[:min(commit, len(entries))]
	}
	return f
}

// publish records ev as published: the cut moves to it, the streaming
// assistant message takes it in, and the ring stores it for the followers.
// attemptID is the open attempt of the run that published ev. The listeners
// have run already, and no Append or listener call happens while pubMu is held.
// Only the holder of emitMu calls it.
func (a *Agent) publish(ev protocol.Event, attemptID string) {
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	a.pub.seq = ev.Env().Seq
	a.pub.commitIndex = int(a.pub.writer.end.Load())
	a.trackStream(attemptID, ev)
	if a.ring == nil {
		return
	}
	// An event that cannot be encoded leaves a gap marker, like an oversized one.
	data, _ := protocol.EncodeEvent(ev)
	a.ring.Add(a.cfg.SessionID, a.pub.seq, data)
}

// trackStream keeps the accumulator of the assistant message that streams.
// pubMu is held.
func (a *Agent) trackStream(attemptID string, ev protocol.Event) {
	switch e := ev.(type) {
	case *protocol.MessageStart:
		a.pub.stream, a.pub.attemptID = nil, ""
		// Only a pending message streams. The failure message has its final
		// stop reason from the start and is not a stream.
		b := protocol.NewBuilder()
		if err := b.ApplyAgentEvent(e); err == nil && b.Started() {
			a.pub.stream, a.pub.attemptID = b, attemptID
		}
	case *protocol.MessageUpdate:
		if a.pub.stream != nil {
			if err := a.pub.stream.ApplyAgentEvent(e); err != nil {
				a.pub.stream = nil
			}
		}
	case *protocol.MessageEnd, *protocol.AgentEnd, *protocol.AgentSettled:
		a.pub.stream, a.pub.attemptID = nil, ""
	}
}

// newEpoch returns the random name of a conversation epoch.
func newEpoch() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // never fails; see crypto/rand.Read
	return hex.EncodeToString(b)
}

// resetPublished points the cut at a fresh log. Followers and cursors of the
// old epoch get bus.ErrResync.
func (a *Agent) resetPublished(w *trackedLog) {
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	a.pub = published{epoch: newEpoch(), seq: a.seq, commitIndex: int(w.end.Load()), writer: w}
	if a.ring != nil {
		a.ring.Reset(a.pub.epoch, a.pub.seq)
	}
}
