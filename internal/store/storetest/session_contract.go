// Package storetest holds the contract suites that every session backend must
// pass. A backend test calls the Run functions with a factory of its own.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
	"AskCore/pkg/protocol"
)

func userText(s string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: s}}, Timestamp: 1}
}

func assistantText(s string) protocol.Message {
	return protocol.AssistantMessage{
		Content:    []protocol.AssistantBlock{protocol.Text{Text: s}},
		StopReason: protocol.StopReason("stop"),
	}
}

// RunSessionWriter checks the append contract of a session writer. newWriter
// returns a new empty writer that the test may use and close.
func RunSessionWriter(t *testing.T, newWriter func(t *testing.T) sessions.Writer) {
	t.Helper()
	fresh := func(t *testing.T) sessions.Writer {
		t.Helper()
		w := newWriter(t)
		t.Cleanup(func() { _ = w.Close() })
		return w
	}

	t.Run("CommitRefPositions", func(t *testing.T) {
		w := fresh(t)
		ref, err := w.Append(sessions.CycleOpened{CycleID: "c"}, sessions.TurnOpened{CycleID: "c"})
		require.NoError(t, err)
		assert.Equal(t, sessions.CommitRef{Start: 0, End: 2}, ref)
		ref, err = w.Append(sessions.MessageEntry{Message: userText("a")})
		require.NoError(t, err)
		assert.Equal(t, sessions.CommitRef{Start: 2, End: 3}, ref)
		ref, err = w.Append()
		require.NoError(t, err)
		assert.Equal(t, sessions.CommitRef{Start: 3, End: 3}, ref)
	})

	t.Run("EntriesReadBackInAppendOrder", func(t *testing.T) {
		w := fresh(t)
		want := []sessions.Entry{
			sessions.CycleOpened{CycleID: "c"},
			sessions.TurnOpened{CycleID: "c"},
			sessions.MessageEntry{Message: userText("a")},
			sessions.ModelChange{Model: sessions.ModelRef{Provider: "p", API: "a", ID: "m"}},
			sessions.ThinkingChange{Level: protocol.ThinkingHigh},
			sessions.MessageEntry{Message: assistantText("b")},
			sessions.TurnClosed{CycleID: "c"},
			sessions.CycleClosed{CycleID: "c", Reason: "done"},
		}
		for _, e := range want[:3] {
			_, err := w.Append(e)
			require.NoError(t, err)
		}
		_, err := w.Append(want[3:]...)
		require.NoError(t, err)
		assert.Equal(t, want, w.Entries())
		assert.Equal(t, []protocol.Message{userText("a"), assistantText("b")}, w.Messages())
	})

	t.Run("AppendIsAtomic", func(t *testing.T) {
		w := fresh(t)
		_, err := w.Append(sessions.MessageEntry{Message: userText("a")})
		require.NoError(t, err)
		_, err = w.Append(sessions.MessageEntry{Message: userText("b")}, sessions.MessageEntry{})
		require.Error(t, err)
		assert.Equal(t, []sessions.Entry{sessions.MessageEntry{Message: userText("a")}}, w.Entries())
		ref, err := w.Append(sessions.MessageEntry{Message: userText("c")})
		require.NoError(t, err)
		assert.Equal(t, sessions.CommitRef{Start: 1, End: 2}, ref)
	})

	t.Run("RejectsNilEntry", func(t *testing.T) {
		w := fresh(t)
		_, err := w.Append(nil)
		require.Error(t, err)
		assert.Empty(t, w.Entries())
	})

	t.Run("CopiesInAndOut", func(t *testing.T) {
		w := fresh(t)
		temp := 0.5
		in := sessions.RequestDelta{
			AttemptID: "a",
			Added:     []protocol.Message{userText("x")},
			Model:     sessions.ModelRef{HeaderNames: []string{"X-One"}},
			Options:   sessions.RequestOptions{Temperature: &temp},
		}
		_, err := w.Append(in)
		require.NoError(t, err)

		in.Added[0] = userText("changed")
		in.Model.HeaderNames[0] = "changed"
		temp = 9

		got := w.Entries()
		delta := got[0].(sessions.RequestDelta)
		assert.Equal(t, []protocol.Message{userText("x")}, delta.Added)
		assert.Equal(t, "X-One", delta.Model.HeaderNames[0])
		assert.Equal(t, 0.5, *delta.Options.Temperature)

		delta.Added[0] = userText("again")
		*delta.Options.Temperature = 7
		again := w.Entries()[0].(sessions.RequestDelta)
		assert.Equal(t, []protocol.Message{userText("x")}, again.Added)
		assert.Equal(t, 0.5, *again.Options.Temperature)
	})

	t.Run("LastSystemSnapshot", func(t *testing.T) {
		w := fresh(t)
		_, ok := w.LastSystemSnapshot()
		assert.False(t, ok)
		_, err := w.Append(sessions.SystemSnapshot{SystemPrompt: "one"})
		require.NoError(t, err)
		_, err = w.Append(sessions.MessageEntry{Message: userText("a")}, sessions.SystemSnapshot{SystemPrompt: "two"})
		require.NoError(t, err)
		_, err = w.Append(sessions.MessageEntry{Message: userText("b")})
		require.NoError(t, err)
		snap, ok := w.LastSystemSnapshot()
		require.True(t, ok)
		assert.Equal(t, "two", snap.SystemPrompt)
	})
}

// RunSessionStore checks the ownership, reopen, lazy creation and listing
// contract of a session store. newStore returns a store with no sessions, and
// two calls in one test must return stores that share the same data.
func RunSessionStore(t *testing.T, newStore func(t *testing.T) store.SessionStore) {
	t.Helper()
	ctx := context.Background()
	created := time.Date(2026, 11, 18, 11, 0, 0, 0, time.UTC)
	header := func(cwd, id string) sessions.Header {
		return sessions.Header{Version: sessions.FormatVersion, ID: id, Cwd: cwd, CreatedAt: created}
	}
	reply := []sessions.Entry{
		sessions.MessageEntry{Message: userText("hi")},
		sessions.MessageEntry{Message: assistantText("hello")},
	}

	t.Run("CreateNowThenRead", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		w, err := s.Create(ctx, header(cwd, "100000000000000001"), store.CreateNow)
		require.NoError(t, err)
		id := w.ID()
		_, err = w.Append(reply...)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		h, entries, err := s.Read(ctx, cwd, id)
		require.NoError(t, err)
		assert.Equal(t, id, h.ID)
		assert.Equal(t, cwd, h.Cwd)
		assert.True(t, created.Equal(h.CreatedAt))
		assert.Equal(t, reply, entries)
	})

	t.Run("ReopenContinuesAtNextSeq", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		w, err := s.Create(ctx, header(cwd, "100000000000000002"), store.CreateNow)
		require.NoError(t, err)
		id := w.ID()
		_, err = w.Append(reply...)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		w2, err := s.Open(ctx, cwd, id)
		require.NoError(t, err)
		defer func() { _ = w2.Close() }()
		assert.Equal(t, id, w2.ID())
		assert.Equal(t, reply, w2.Entries())
		ref, err := w2.Append(sessions.TurnClosed{CycleID: "c"})
		require.NoError(t, err)
		assert.Equal(t, sessions.CommitRef{Start: 2, End: 3}, ref)
		require.NoError(t, w2.Close())

		_, entries, err := s.Read(ctx, cwd, id)
		require.NoError(t, err)
		assert.Len(t, entries, 3)
	})

	t.Run("SecondOpenFailsWhileHeld", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		w, err := s.Create(ctx, header(cwd, "100000000000000003"), store.CreateNow)
		require.NoError(t, err)
		id := w.ID()
		_, err = w.Append(reply...)
		require.NoError(t, err)

		_, err = s.Open(ctx, cwd, id)
		require.True(t, errors.Is(err, store.ErrSessionOwned), "got %v", err)
		other := newStore(t)
		_, err = other.Open(ctx, cwd, id)
		require.True(t, errors.Is(err, store.ErrSessionOwned), "got %v", err)

		require.NoError(t, w.Close())
		w2, err := s.Open(ctx, cwd, id)
		require.NoError(t, err)
		require.NoError(t, w2.Close())
	})

	t.Run("OpenMissingSession", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		_, err := s.Open(ctx, cwd, "100000000000000004")
		assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
		_, _, err = s.Read(ctx, cwd, "100000000000000004")
		assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	})

	t.Run("LazyCreateLeavesNothingUntilAssistantMessage", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		w, err := s.Create(ctx, header(cwd, "100000000000000005"), store.CreateLazy)
		require.NoError(t, err)
		defer func() { _ = w.Close() }()
		_, err = w.Append(
			sessions.CycleOpened{CycleID: "c"},
			sessions.MessageEntry{Message: userText("hi")},
		)
		require.NoError(t, err)

		list, err := s.List(ctx, cwd)
		require.NoError(t, err)
		assert.Empty(t, list)
		_, _, err = s.Read(ctx, cwd, w.ID())
		assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)

		_, err = w.Append(sessions.MessageEntry{Message: assistantText("hello")})
		require.NoError(t, err)
		id := w.ID()
		list, err = s.List(ctx, cwd)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, id, list[0].ID)
		_, entries, err := s.Read(ctx, cwd, id)
		require.NoError(t, err)
		require.Len(t, entries, 3, "entries held before the first reply are stored with it")
		assert.Equal(t, sessions.CycleOpened{CycleID: "c"}, entries[0])
	})

	t.Run("LazyCloseWithoutReplyLeavesNothing", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		w, err := s.Create(ctx, header(cwd, "100000000000000006"), store.CreateLazy)
		require.NoError(t, err)
		_, err = w.Append(sessions.MessageEntry{Message: userText("hi")})
		require.NoError(t, err)
		require.NoError(t, w.Close())
		list, err := s.List(ctx, cwd)
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("ListNewestFirstHeadersOnly", func(t *testing.T) {
		s, cwd := newStore(t), t.TempDir()
		ids := []string{"100000000000000020", "100000000000000030", "100000000000000010"}
		for _, id := range ids {
			w, err := s.Create(ctx, header(cwd, id), store.CreateNow)
			require.NoError(t, err)
			_, err = w.Append(reply...)
			require.NoError(t, err)
			require.NoError(t, w.Close())
		}
		list, err := s.List(ctx, cwd)
		require.NoError(t, err)
		got := make([]string, len(list))
		for i, h := range list {
			got[i] = h.ID
			assert.Equal(t, cwd, h.Cwd)
		}
		assert.Equal(t, []string{"100000000000000030", "100000000000000020", "100000000000000010"}, got)

		other, err := s.List(ctx, t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, other, "a session belongs to its own cwd")
	})
}
