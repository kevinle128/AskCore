package leader

import "sync"

// queue is an unbounded FIFO. A producer never blocks, so the router never waits
// on a slow socket. One consumer goroutine pops the items in order.
type queue[T any] struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []T
	closed bool
}

func newQueue[T any]() *queue[T] {
	q := &queue[T]{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push adds an item. It reports false when the queue is closed.
func (q *queue[T]) push(v T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.items = append(q.items, v)
	q.cond.Signal()
	return true
}

// pop waits for the next item. It reports false after close.
func (q *queue[T]) pop() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	var zero T
	if q.closed {
		return zero, false
	}
	v := q.items[0]
	q.items[0] = zero
	q.items = q.items[1:]
	return v, true
}

// purge removes the queued items for which drop is true and returns their count.
// An item that pop already took is not affected.
func (q *queue[T]) purge(drop func(T) bool) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	var removed int
	for _, v := range q.items {
		if drop(v) {
			removed++
			continue
		}
		kept = append(kept, v)
	}
	clear(q.items[len(kept):])
	q.items = kept
	return removed
}

// close wakes the consumer and drops what is queued.
func (q *queue[T]) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.items = nil
	q.cond.Broadcast()
}
