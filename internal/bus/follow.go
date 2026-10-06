package bus

import (
	"errors"
	"sync"
)

// ErrResync says that a follower or a cursor cannot continue from the events
// the ring holds. The client takes a new snapshot and follows again.
var ErrResync = errors.New("bus: resync required")

// Default bounds of a Ring. They apply where a Limits field is zero.
const (
	defaultEvents     = 4096
	defaultBytes      = 8 << 20
	defaultEventBytes = 256 << 10
	defaultBuffer     = 1024
)

// Limits bound a Ring and its followers.
type Limits struct {
	// Events is the most events the ring holds.
	Events int
	// Bytes is the most payload bytes the ring holds.
	Bytes int
	// EventBytes is the largest payload the ring stores. A larger event leaves
	// a gap marker at its seq, never a cut payload, and every follower that
	// reaches the marker ends with ErrResync. The default is 256 KiB; an event
	// that carries a whole large message, such as agent_end or turn_end, can
	// exceed it.
	EventBytes int
	// Buffer is how many events one follower may hold unread.
	Buffer int
}

func (l Limits) withDefaults() Limits {
	if l.Events <= 0 {
		l.Events = defaultEvents
	}
	if l.Bytes <= 0 {
		l.Bytes = defaultBytes
	}
	if l.EventBytes <= 0 {
		l.EventBytes = defaultEventBytes
	}
	if l.Buffer <= 0 {
		l.Buffer = defaultBuffer
	}
	return l
}

// Item is one event of a follow, as the encoded wire form.
type Item struct {
	Seq  uint64
	Data []byte
}

// Stats are the counters of a Ring.
type Stats struct {
	// Gaps counts sequence breaks, which clear the ring.
	Gaps uint64
	// Detached counts followers that ended with ErrResync.
	Detached uint64
	// Bytes is the payload size that the ring holds now.
	Bytes int
}

// record is one stored event. A nil data is a gap marker: the payload was above
// the size cap and was not stored.
type record struct {
	seq     uint64
	session string
	data    []byte
}

// Ring keeps the newest events of one conversation, in seq order without a
// hole, for a bounded replay, and feeds the followers. Add never blocks: a
// follower that does not keep up is detached with ErrResync.
type Ring struct {
	lim Limits

	mu        sync.Mutex
	epoch     string
	head      uint64 // seq of the newest event, stored or not
	items     []record
	bytes     int
	followers map[*Follower]struct{}
	stats     Stats
}

// NewRing returns a ring in epoch whose newest event has the seq head. Events
// up to head are not held.
func NewRing(lim Limits, epoch string, head uint64) *Ring {
	return &Ring{lim: lim.withDefaults(), epoch: epoch, head: head, followers: map[*Follower]struct{}{}}
}

// Add stores event seq, which must be the successor of the newest one, and
// gives it to the followers of session. A seq that breaks the order clears the
// ring and detaches every follower. A payload above the size cap, or a nil
// payload, is stored as a gap marker, and the followers that reach it get
// ErrResync.
func (r *Ring) Add(session string, seq uint64, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if seq != r.head+1 {
		r.stats.Gaps++
		r.clear()
		r.head = seq
		r.detachAll()
		return
	}
	r.head = seq
	rec := record{seq: seq, session: session, data: data}
	if data == nil || len(data) > r.lim.EventBytes {
		rec.data = nil
	}
	r.items = append(r.items, rec)
	r.bytes += len(rec.data)
	for len(r.items) > r.lim.Events || r.bytes > r.lim.Bytes {
		r.bytes -= len(r.items[0].data)
		r.items[0] = record{}
		r.items = r.items[1:]
	}
	for f := range r.followers {
		if f.session != session {
			continue
		}
		f.deliver(rec)
	}
}

// Reset starts a new epoch. Older cursors and every follower get ErrResync.
func (r *Ring) Reset(epoch string, head uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch = epoch
	r.clear()
	r.head = head
	r.detachAll()
}

// Close ends every follower with no error, and Err returns nil for it. Events
// that were queued stay readable. The Agent calls it when it is disposed.
func (r *Ring) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for f := range r.followers {
		f.finish(nil)
	}
}

// Epoch returns the epoch and the seq of the newest event.
func (r *Ring) Epoch() (string, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.epoch, r.head
}

// Stats returns a copy of the counters.
func (r *Ring) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats
	s.Bytes = r.bytes
	return s
}

// Follow returns a follower that gets the events after the newest one now.
func (r *Ring) Follow(session string) *Follower {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attach(session, nil)
}

// Resume returns a follower that first gets the held events after seq, and then
// the live ones, for a cursor (epoch, seq). It returns ErrResync when the
// epoch differs, when seq is above the newest event, or when an event after seq
// is no longer held.
func (r *Ring) Resume(session, epoch string, seq uint64) (*Follower, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if epoch != r.epoch || seq > r.head || seq+uint64(len(r.items)) < r.head {
		return nil, ErrResync
	}
	var replay []record
	for _, rec := range r.items {
		if rec.seq > seq && rec.session == session {
			replay = append(replay, rec)
		}
	}
	return r.attach(session, replay), nil
}

// attach registers a follower with replay already queued. The caller holds mu.
func (r *Ring) attach(session string, replay []record) *Follower {
	f := &Follower{ring: r, session: session, ch: make(chan Item, r.lim.Buffer+len(replay))}
	r.followers[f] = struct{}{}
	for _, rec := range replay {
		if !f.deliver(rec) {
			break
		}
	}
	return f
}

func (r *Ring) clear() {
	r.items = nil
	r.bytes = 0
}

func (r *Ring) detachAll() {
	for f := range r.followers {
		f.finish(ErrResync)
	}
}

// Follower reads the events of one session from a Ring. Read Events until it
// is closed, then call Err.
type Follower struct {
	ring    *Ring
	session string
	ch      chan Item

	// closed and err belong to ring.mu.
	closed bool
	err    error
}

// Events returns the channel of events. It is closed when the follower ends.
func (f *Follower) Events() <-chan Item { return f.ch }

// Err tells why Events is closed: ErrResync, or nil after Close. It is
// meaningful once Events is closed.
func (f *Follower) Err() error {
	f.ring.mu.Lock()
	defer f.ring.mu.Unlock()
	return f.err
}

// Close ends the follower. It is safe to call more than once.
func (f *Follower) Close() {
	f.ring.mu.Lock()
	defer f.ring.mu.Unlock()
	f.finish(nil)
}

// deliver queues rec without blocking. It reports whether the follower is still
// attached. The caller holds ring.mu.
func (f *Follower) deliver(rec record) bool {
	if f.closed {
		return false
	}
	if rec.data == nil {
		f.finish(ErrResync)
		return false
	}
	select {
	case f.ch <- Item{Seq: rec.seq, Data: rec.data}:
		return true
	default:
		f.finish(ErrResync)
		return false
	}
}

// finish detaches the follower and closes its channel. Queued items stay
// readable. The caller holds ring.mu.
func (f *Follower) finish(err error) {
	if f.closed {
		return
	}
	f.closed = true
	f.err = err
	if err != nil {
		f.ring.stats.Detached++
	}
	delete(f.ring.followers, f)
	close(f.ch)
}
