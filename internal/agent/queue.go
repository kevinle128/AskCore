package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

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

// origin tells where an input came from. Abort drops human input and keeps
// the notices of background tasks.
type origin uint8

const (
	originHuman origin = iota
	originTask
)

// input is one message with the ID that the Agent gave it. The ID names the
// message in queue_update removal and in the acknowledgment of a rejection.
type input struct {
	id     string
	msg    protocol.Message
	origin origin
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

// humanLen counts the queued inputs that count toward the queue bound. Notices
// of tasks do not count: a full queue must never drop a notice, and notices
// must not take the slots of the human.
func (q *inbox) humanLen() int {
	n := 0
	for _, list := range [][]input{q.steering, q.followUp} {
		for _, in := range list {
			if in.origin == originHuman {
				n++
			}
		}
	}
	return n
}

// nextID returns an ID that no earlier input of the Agent has.
func (q *inbox) nextID() string {
	q.seq++
	return "in-" + strconv.FormatUint(q.seq, 10)
}

// add appends msg to a queue and returns its ID.
func (q *inbox) add(followUp bool, msg protocol.Message) string {
	return q.addFrom(originHuman, followUp, msg)
}

// addFrom is add with the origin of the input.
func (q *inbox) addFrom(o origin, followUp bool, msg protocol.Message) string {
	in := input{id: q.nextID(), msg: msg, origin: o}
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

// claimSteering takes all steering messages. With withContext it takes the
// context items first, which ride with the messages. Without it the context
// items stay: they ask for no model request, so they must not be the only
// reason for one.
func (q *inbox) claimSteering(withContext bool) []input {
	var got []input
	if withContext {
		got = q.context
		q.context = nil
	}
	got = append(got, q.steering...)
	q.steering = nil
	return got
}

// claimCycle takes all steering messages and one follow-up message, in that
// order.
func (q *inbox) claimCycle() []input {
	got := q.claimSteering(true)
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

// abort drops the human input and the tool context. It moves the notices of
// tasks to the context queue, which rides with the next input and wakes
// nothing. It reports whether anything was queued.
func (q *inbox) abort() bool {
	had := q.len() > 0 || len(q.context) > 0
	var keep []input
	for _, list := range [][]input{q.context, q.steering, q.followUp} {
		for _, in := range list {
			if in.origin == originTask {
				keep = append(keep, in)
			}
		}
	}
	q.context, q.steering, q.followUp = keep, nil, nil
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

// maxPreviewBytes bounds the text that queue_update shows for a custom
// message. A notice can hold a long output tail, and the event must stay far
// below the size limit of the event bus.
const maxPreviewBytes = 200

// messageText is the text of the text blocks of a user message, and a short
// preview of a custom message. Other messages and blocks give no text.
func messageText(m protocol.Message) string {
	switch v := m.(type) {
	case protocol.UserMessage:
		return userText(v)
	case *protocol.UserMessage:
		if v == nil {
			return ""
		}
		return userText(*v)
	case protocol.CustomMessage:
		return customPreview(v)
	case *protocol.CustomMessage:
		if v == nil {
			return ""
		}
		return customPreview(*v)
	}
	return ""
}

func userText(u protocol.UserMessage) string {
	var b strings.Builder
	for _, blk := range u.Content {
		if t, ok := blk.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// customPreview is the queue_update text of a custom message, cut to
// maxPreviewBytes on a rune boundary. A task notice reads as
// "task <id> <status>: <title>" from its details, so the UI shows what ended
// and not the full output. Other messages, and notices whose details hold none
// of the three fields, show the start of their text blocks.
func customPreview(c protocol.CustomMessage) string {
	if c.CustomType == "task-notification" {
		if s := taskNoticeLine(c.Details); s != "" {
			return cutOnRune(s, maxPreviewBytes)
		}
	}
	var b strings.Builder
	for _, blk := range c.Content {
		if b.Len() >= maxPreviewBytes {
			break
		}
		if t, ok := blk.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return cutOnRune(b.String(), maxPreviewBytes)
}

// taskNoticeLine builds "task <id> <status>: <title>" from the details of a
// task notice. A missing field is skipped. It returns "" when details cannot be
// read or hold none of the fields.
func taskNoticeLine(details json.RawMessage) string {
	var d struct {
		TaskID string `json:"taskId"`
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	if len(details) == 0 || json.Unmarshal(details, &d) != nil {
		return ""
	}
	if d.TaskID == "" && d.Status == "" && d.Title == "" {
		return ""
	}
	line := "task"
	if d.TaskID != "" {
		line += " " + d.TaskID
	}
	if d.Status != "" {
		line += " " + d.Status
	}
	if d.Title != "" {
		line += ": " + d.Title
	}
	return line
}

// cutOnRune returns s cut to at most n bytes without splitting a rune.
func cutOnRune(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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
	if a.in.humanLen() >= a.maxQueued() {
		a.mu.Unlock()
		return "", ErrQueueFull
	}
	var started *run
	if r := a.active; r == nil {
		if a.cfg.Model.ID == "" {
			a.mu.Unlock()
			return "", ErrNoModel
		}
		started = a.startRunLocked(context.Background())
	} else if r.closing || r.ctx.Err() != nil {
		// The active run cannot take the message: it is cancelled, or it has
		// decided to end. The message waits for the run after it.
		followUp = true
		a.wake = true
	}
	id := a.in.add(followUp, msg)
	a.work++
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

// claimSteering takes all steering messages and, with withContext, the context
// items. A cancelled run claims nothing.
func (s runInputs) claimSteering(withContext bool) []input {
	a := s.a
	a.mu.Lock()
	if s.r.ctx.Err() != nil {
		a.mu.Unlock()
		return nil
	}
	got := a.in.claimSteering(withContext)
	if len(got) > 0 {
		a.postLocked(a.in.update())
	}
	a.mu.Unlock()
	return got
}

// claimCycle takes the input of the next cycle: all steering messages and one
// follow-up message. When nothing is queued and the run ends, the same critical
// section marks the run as closing, so a message that arrives after this point
// goes to the run after it. When the run ends and only context items are
// queued, it claims nothing: they stay for the next admission. A run that goes
// on is not closing. A cancelled run claims nothing and needs no mark: its
// context already routes new messages to the next run.
func (s runInputs) claimCycle(ending bool) []input {
	a := s.a
	a.mu.Lock()
	if s.r.ctx.Err() != nil {
		a.mu.Unlock()
		return nil
	}
	if ending && len(a.in.steering) == 0 && len(a.in.followUp) == 0 {
		// Only context items are queued, and they ask for no request. They stay
		// for the next admission, and the run closes.
		s.r.closing = true
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
