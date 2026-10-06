package bus_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/bus"
)

const session = "s1"

func fill(r *bus.Ring, from, to uint64, payload string) {
	for seq := from; seq <= to; seq++ {
		r.Add(session, seq, []byte(payload))
	}
}

// drain reads what the follower has buffered, without blocking.
func drain(f *bus.Follower) []bus.Item {
	var out []bus.Item
	for {
		select {
		case it, ok := <-f.Events():
			if !ok {
				return out
			}
			out = append(out, it)
		default:
			return out
		}
	}
}

func seqs(items []bus.Item) []uint64 {
	var out []uint64
	for _, it := range items {
		out = append(out, it.Seq)
	}
	return out
}

func TestFollowerGetsLiveEventsInOrder(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	f := r.Follow(session)
	defer f.Close()

	fill(r, 1, 3, "x")

	assert.Equal(t, []uint64{1, 2, 3}, seqs(drain(f)))
}

func TestResumeReplaysAfterCursor(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	fill(r, 1, 5, "x")

	f, err := r.Resume(session, "e1", 2)
	require.NoError(t, err)
	defer f.Close()
	fill(r, 6, 6, "x")

	assert.Equal(t, []uint64{3, 4, 5, 6}, seqs(drain(f)))
}

func TestOldCursorGetsResync(t *testing.T) {
	r := bus.NewRing(bus.Limits{Events: 3}, "e1", 0)
	fill(r, 1, 6, "x")

	_, err := r.Resume(session, "e1", 1)
	assert.ErrorIs(t, err, bus.ErrResync, "seq 2 was evicted")

	f, err := r.Resume(session, "e1", 3)
	require.NoError(t, err, "seq 4 is the oldest event held")
	f.Close()
}

func TestFutureCursorGetsResync(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	fill(r, 1, 3, "x")

	_, err := r.Resume(session, "e1", 4)

	assert.ErrorIs(t, err, bus.ErrResync)
}

func TestResetForcesResyncForOldCursor(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	fill(r, 1, 3, "x")
	live := r.Follow(session)
	defer live.Close()

	r.Reset("e2", 3)

	_, err := r.Resume(session, "e1", 3)
	assert.ErrorIs(t, err, bus.ErrResync, "a cursor of the old epoch is refused")
	assert.Empty(t, drain(live))
	_, open := <-live.Events()
	assert.False(t, open, "a live follower of the old epoch is detached")
	assert.ErrorIs(t, live.Err(), bus.ErrResync)
	f, err := r.Resume(session, "e2", 3)
	require.NoError(t, err)
	f.Close()
}

func TestFollowerOverflowEndsWithResync(t *testing.T) {
	r := bus.NewRing(bus.Limits{Buffer: 2}, "e1", 0)
	f := r.Follow(session)
	defer f.Close()

	fill(r, 1, 5, "x")

	assert.Equal(t, []uint64{1, 2}, seqs(drain(f)), "the buffered events stay readable")
	_, open := <-f.Events()
	assert.False(t, open)
	assert.ErrorIs(t, f.Err(), bus.ErrResync)
	assert.Equal(t, uint64(1), r.Stats().Detached)
	// The ring itself is not harmed by a slow follower.
	late, err := r.Resume(session, "e1", 3)
	require.NoError(t, err)
	late.Close()
}

func TestReplayNeverCrossesSessions(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	live := r.Follow("a")
	defer live.Close()
	r.Add("a", 1, []byte("a1"))
	r.Add("b", 2, []byte("b2"))
	r.Add("a", 3, []byte("a3"))

	replay, err := r.Resume("a", "e1", 0)
	require.NoError(t, err)
	defer replay.Close()

	assert.Equal(t, []uint64{1, 3}, seqs(drain(live)))
	assert.Equal(t, []uint64{1, 3}, seqs(drain(replay)))
}

func TestRingEvictsByBytes(t *testing.T) {
	r := bus.NewRing(bus.Limits{Bytes: 10, EventBytes: 10}, "e1", 0)

	fill(r, 1, 4, "abcd") // 4 bytes each: two fit

	_, err := r.Resume(session, "e1", 1)
	assert.ErrorIs(t, err, bus.ErrResync, "seq 2 was evicted by the byte bound")
	f, err := r.Resume(session, "e1", 2)
	require.NoError(t, err)
	defer f.Close()
	assert.Equal(t, []uint64{3, 4}, seqs(drain(f)))
	assert.LessOrEqual(t, r.Stats().Bytes, 10)
}

func TestOversizedEventGivesResyncNotTruncatedPayload(t *testing.T) {
	r := bus.NewRing(bus.Limits{EventBytes: 8}, "e1", 0)
	live := r.Follow(session)
	defer live.Close()
	r.Add(session, 1, []byte("small"))

	r.Add(session, 2, []byte(strings.Repeat("z", 9)))

	got := drain(live)
	require.Equal(t, []uint64{1}, seqs(got))
	assert.Equal(t, "small", string(got[0].Data))
	assert.ErrorIs(t, live.Err(), bus.ErrResync)

	r.Add(session, 3, []byte("after"))
	replay, err := r.Resume(session, "e1", 1)
	require.NoError(t, err)
	defer replay.Close()
	items := drain(replay)
	assert.Empty(t, items, "a replay that reaches the gap delivers no payload for it")
	assert.ErrorIs(t, replay.Err(), bus.ErrResync)
}

func TestSequenceBreakDetachesFollowers(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	live := r.Follow(session)
	defer live.Close()
	fill(r, 1, 2, "x")

	r.Add(session, 5, []byte("x"))

	assert.Equal(t, []uint64{1, 2}, seqs(drain(live)))
	assert.ErrorIs(t, live.Err(), bus.ErrResync)
	assert.Equal(t, uint64(1), r.Stats().Gaps)
}

func TestClosedFollowerLeavesNoGoroutineAndGetsNothing(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	f := r.Follow(session)
	f.Close()
	f.Close()

	fill(r, 1, 2, "x")

	_, open := <-f.Events()
	assert.False(t, open)
	assert.NoError(t, f.Err())
}

func TestCloseEndsFollowersWithNoError(t *testing.T) {
	r := bus.NewRing(bus.Limits{}, "e1", 0)
	f := r.Follow(session)
	fill(r, 1, 2, "x")

	r.Close()

	assert.Equal(t, []uint64{1, 2}, seqs(drain(f)), "queued events stay readable")
	_, open := <-f.Events()
	assert.False(t, open)
	assert.NoError(t, f.Err(), "a close is not a resync")
}
