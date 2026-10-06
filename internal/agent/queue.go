package agent

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"AskCore/pkg/protocol"
)

// DefaultMaxQueuedInputs is the bound of Config.MaxQueuedInputs when it is
// zero.
const DefaultMaxQueuedInputs = 100

var (
	// ErrDisposed is returned by every call that admits input or changes the
	// Agent after Dispose started.
	ErrDisposed = errors.New("agent: disposed")
	// ErrQueueFull is returned by Steer and FollowUp when the queues hold
	// Config.MaxQueuedInputs messages.
	ErrQueueFull = errors.New("agent: input queue is full")
	// ErrNoInput is returned by Steer and FollowUp for a nil message.
	ErrNoInput = errors.New("agent: no message to queue")
)

// input is one message with the ID that the Agent gave it. The ID names the
// message in queue_update removal and in the acknowledgment of a rejection.
type input struct {
	id  string
	msg protocol.Message
}

// inbox holds the two queues of an Agent: steering, which a turn boundary
// takes, and follow-up, of which a cycle boundary takes one. Agent.mu guards
// it; it has no lock of its own.
type inbox struct {
	context  []input
	steering []input
	followUp []input
	// seq numbers the IDs of the Agent for its whole life; Reset does not
	// restart it.
	seq uint64
}

func (q *inbox) len() int { return len(q.steering) + len(q.followUp) }

// nextID returns an ID that no earlier input of the Agent has.
func (q *inbox) nextID() string {
	q.seq++
	return "in-" + strconv.FormatUint(q.seq, 10)
}

// add appends msg to a queue and returns its ID.
func (q *inbox) add(followUp bool, msg protocol.Message) string {
	in := input{id: q.nextID(), msg: msg}
	if followUp {
		q.followUp = append(q.followUp, in)
	} else {
		q.steering = append(q.steering, in)
	}
	return in.id
}

// remove deletes the pending input with the ID. It reports whether the input
// was still pending: a claim takes it out of the queue.
func (q *inbox) remove(id string) bool {
	for _, list := range []*[]input{&q.context, &q.steering, &q.followUp} {
		for i, in := range *list {
			if in.id == id {
				*list = append((*list)[:i:i], (*list)[i+1:]...)
				return true
			}
		}
	}
	return false
}

// claimSteering takes all steering messages.
func (q *inbox) claimSteering() []input {
	got := append(q.context, q.steering...)
	q.context = nil
	q.steering = nil
	return got
}

// claimCycle takes all steering messages and one follow-up message, in that
// order.
func (q *inbox) claimCycle() []input {
	got := q.claimSteering()
	if len(q.followUp) > 0 {
		got = append(got, q.followUp[0])
		q.followUp = q.followUp[1:]
	}
	return got
}

// clear drops both queues. It reports whether anything was queued.
func (q *inbox) clear() bool {
	had := q.len() > 0 || len(q.context) > 0
	q.context, q.steering, q.followUp = nil, nil, nil
	return had
}

// update is the queue_update event of the current state.
func (q *inbox) update() *protocol.QueueUpdate {
	return &protocol.QueueUpdate{Steering: textsOf(q.steering), FollowUp: textsOf(q.followUp)}
}

func textsOf(list []input) []string {
	out := make([]string, len(list))
	for i, in := range list {
		out[i] = messageText(in.msg)
	}
	return out
}

// messageText is the text of the text blocks of a user message. Other
// messages and blocks give no text.
func messageText(m protocol.Message) string {
	u, ok := m.(protocol.UserMessage)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, blk := range u.Content {
		if t, ok := blk.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// messagesOf returns the messages of list.
func messagesOf(list []input) []protocol.Message {
	out := make([]protocol.Message, len(list))
	for i, in := range list {
		out[i] = in.msg
	}
	return out
}

// AbortOption changes what Abort does.
type AbortOption func(*abortConfig)

type abortConfig struct{ keepQueued bool }

// KeepQueued makes Abort keep the queued steering and follow-up messages. By
// default Abort drops them.
func KeepQueued(c *abortConfig) { c.keepQueued = true }

// Steer queues msg for the next turn boundary of the active run: the loop
// takes all steering messages there, after the tool batch. It returns the ID of
// the input, which Remove takes. On an idle Agent the message starts a run on
// its own goroutine; WaitForIdle returns when that run has settled. While the
// active run is cancelled or ending, the message goes to the follow-up queue
// and the Agent starts a new run when the old one is over. The Agent keeps its
// own copy of msg.
func (a *Agent) Steer(msg protocol.Message) (string, error) { return a.enqueue(msg, false) }

// FollowUp queues msg for a cycle of its own: a cycle boundary takes one
// follow-up message. It returns the ID of the input. Its start rules are those
// of Steer.
func (a *Agent) FollowUp(msg protocol.Message) (string, error) { return a.enqueue(msg, true) }

func (a *Agent) enqueue(msg protocol.Message, followUp bool) (string, error) {
	if msg == nil {
		return "", ErrNoInput
	}
	msg = protocol.CloneMessage(msg)
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return "", ErrDisposed
	}
	if a.in.len() >= a.maxQueued() {
		a.mu.Unlock()
		return "", ErrQueueFull
	}
	var started *run
	if r := a.active; r == nil {
		started = a.startRunLocked(context.Background())
	} else if r.closing || r.ctx.Err() != nil {
		// The active run cannot take the message: it is cancelled, or it has
		// decided to end. The message waits for the run after it.
		followUp = true
		a.wake = true
	}
	id := a.in.add(followUp, msg)
	a.postLocked(a.in.update())
	a.mu.Unlock()
	if started != nil {
		go a.drive(started)
	}
	return id, nil
}

// Remove deletes the pending input with the ID from its queue and publishes
// queue_update. It returns false when no queue holds the input: a claim took it
// already, it was removed or cleared, or the Agent is disposed. A removed input
// starts no run, even when it had asked for one.
func (a *Agent) Remove(inputID string) bool {
	a.mu.Lock()
	if a.disposed || !a.in.remove(inputID) {
		a.mu.Unlock()
		return false
	}
	a.postLocked(a.in.update())
	a.mu.Unlock()
	return true
}

// maxQueued is the bound on the queued messages. a.cfg does not change after
// New, so it needs no lock.
func (a *Agent) maxQueued() int {
	if a.cfg.MaxQueuedInputs > 0 {
		return a.cfg.MaxQueuedInputs
	}
	return DefaultMaxQueuedInputs
}

// runInputs is the view of the queues that one run has. The loop claims
// through it; it never touches the Agent directly.
type runInputs struct {
	a *Agent
	r *run
}

// claimSteering takes all steering messages. A cancelled run claims nothing.
func (s runInputs) claimSteering() []input {
	a := s.a
	a.mu.Lock()
	if s.r.ctx.Err() != nil {
		a.mu.Unlock()
		return nil
	}
	got := a.in.claimSteering()
	if len(got) > 0 {
		a.postLocked(a.in.update())
	}
	a.mu.Unlock()
	return got
}

// claimCycle takes the input of the next cycle: all steering messages and one
// follow-up message. When nothing is queued and the run ends, the same critical
// section marks the run as closing, so a message that arrives after this point
// goes to the run after it. A run that goes on is not closing. A cancelled run
// claims nothing and needs no mark: its context already routes new messages to
// the next run.
func (s runInputs) claimCycle(ending bool) []input {
	a := s.a
	a.mu.Lock()
	if s.r.ctx.Err() != nil {
		a.mu.Unlock()
		return nil
	}
	got := a.in.claimCycle()
	s.r.closing = ending && len(got) == 0
	if len(got) > 0 {
		a.postLocked(a.in.update())
	}
	a.mu.Unlock()
	return got
}

// nextID returns an ID for an input that came with the call that started the
// run, not through a queue.
func (s runInputs) nextID() string {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.a.in.nextID()
}

// addToolContext keeps post-tool context without waking a cancelled run.
func (s runInputs) addToolContext(messages []protocol.UserMessage) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.a.disposed {
		return
	}
	for _, msg := range messages {
		s.a.in.context = append(s.a.in.context, input{id: s.a.in.nextID(), msg: protocol.CloneMessage(msg)})
	}
}

func (l *loop) addToolContext(messages []protocol.UserMessage) {
	if s, ok := l.in.(interface{ addToolContext([]protocol.UserMessage) }); ok {
		s.addToolContext(messages)
		return
	}
	for _, msg := range messages {
		l.toolContext = append(l.toolContext, input{id: newRunID(), msg: protocol.CloneMessage(msg)})
	}
}
