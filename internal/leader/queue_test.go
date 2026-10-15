package leader

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueueKeepsOrderAndNeverBlocksPush(t *testing.T) {
	q := newQueue[int]()
	for i := range 10000 {
		q.push(i) // no reader yet: push must not block
	}
	for i := range 10000 {
		got, ok := q.pop()
		require.True(t, ok)
		require.Equal(t, i, got)
	}
}

func TestQueuePopWaitsForPush(t *testing.T) {
	q := newQueue[string]()
	got := make(chan string, 1)
	go func() {
		v, _ := q.pop()
		got <- v
	}()
	select {
	case <-got:
		t.Fatal("pop returned from an empty queue")
	case <-time.After(30 * time.Millisecond):
	}
	q.push("x")
	require.Equal(t, "x", <-got)
}

func TestQueuePurgeRemovesMatchingQueuedItems(t *testing.T) {
	q := newQueue[int]()
	for i := range 6 {
		q.push(i)
	}
	require.Equal(t, 3, q.purge(func(v int) bool { return v%2 == 0 }))
	var rest []int
	for range 3 {
		v, _ := q.pop()
		rest = append(rest, v)
	}
	require.Equal(t, []int{1, 3, 5}, rest)
}

func TestQueueCloseWakesPopAndRefusesPush(t *testing.T) {
	q := newQueue[int]()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := q.pop()
		require.False(t, ok)
	}()
	time.Sleep(20 * time.Millisecond)
	q.close()
	wg.Wait()
	require.False(t, q.push(1), "a closed queue drops the item")
	_, ok := q.pop()
	require.False(t, ok, "close drops queued items")
}
