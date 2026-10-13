package tuiclient

import (
	"sync"

	"AskCore/internal/tui"
)

// mailbox is an unbounded queue between the reader of the link and the UI. The
// reader must never wait for the UI: a reader that waits cannot answer the
// pings of the leader, and the leader would close the link.
type mailbox struct {
	mu     sync.Mutex
	queue  []tui.Message
	wake   chan struct{}
	out    chan tui.Message
	stop   chan struct{}
	closed bool
}

func newMailbox() *mailbox {
	return &mailbox{wake: make(chan struct{}, 1), out: make(chan tui.Message), stop: make(chan struct{})}
}

// push queues a message. It never blocks. After close it drops the message.
func (m *mailbox) push(msg tui.Message) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.queue = append(m.queue, msg)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// close stops delivery. Queued messages that were not taken are dropped.
func (m *mailbox) close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.stop)
	}
	m.mu.Unlock()
}

func (m *mailbox) next() (tui.Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return nil, false
	}
	msg := m.queue[0]
	m.queue[0] = nil
	m.queue = m.queue[1:]
	return msg, true
}

// pump moves the queue to the output channel in order. It returns after close,
// and closes the output.
func (m *mailbox) pump() {
	defer close(m.out)
	for {
		msg, ok := m.next()
		if !ok {
			select {
			case <-m.wake:
				continue
			case <-m.stop:
				return
			}
		}
		select {
		case m.out <- msg:
		case <-m.stop:
			return
		}
	}
}
