package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/pkg/protocol"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func testSeed() protocol.AssistantMessage {
	return protocol.AssistantMessage{API: "test-api", Provider: "test-provider", Model: "test-model", Timestamp: 42}
}

func newTestStream(ctx context.Context, buffer int, body func(a *Assembler)) *Stream {
	return NewStream(ctx, buffer, testSeed(), body)
}

func drain(s *Stream) []StreamItem {
	var out []StreamItem
	for it := range s.Events() {
		out = append(out, it)
	}
	return out
}

func waitResult(t *testing.T, s *Stream) (protocol.AssistantMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, err := s.Result(ctx)
	require.NoError(t, ctx.Err(), "result was not settled in time")
	return msg, err
}

func types(items []StreamItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Event.EventType()
	}
	return out
}

func sptr(s string) *string { return &s }

func i64(v int64) *int64 { return &v }

func bptr(v bool) *bool { return &v }

func TestStreamCloseWithoutTerminalIsIncomplete(t *testing.T) {
	s := newTestStream(context.Background(), 16, func(a *Assembler) {
		a.Start()
		i := a.TextStart("he")
		a.TextDelta(i, "llo")
		a.ToolStart("t1", "echo", nil, nil, nil)
		// The body returns with two open blocks and no terminal call.
	})
	items := drain(s)
	require.NotEmpty(t, items)
	last, ok := items[len(items)-1].Event.(protocol.ErrorEvent)
	require.True(t, ok, "the stream makes an error event for a producer that did not settle")
	assert.Equal(t, protocol.StopError, last.Reason)

	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, ErrStreamIncomplete)
	assert.Contains(t, err.Error(), "ended without")
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Equal(t, "stream ended without a terminal event", *msg.ErrorMessage)
	require.Len(t, msg.Content, 2)
	assert.Equal(t, protocol.Text{Text: "hello"}, msg.Content[0])
	call, ok := msg.Content[1].(protocol.ToolCall)
	require.True(t, ok)
	assert.JSONEq(t, `{}`, string(call.Arguments))
	assert.Equal(t, "test-model", msg.Model)
	assert.Equal(t, int64(42), msg.Timestamp)
}

func TestStreamCloseWithoutAnythingKeepsIdentity(t *testing.T) {
	s := newTestStream(context.Background(), 4, func(a *Assembler) {})
	items := drain(s)
	assert.Equal(t, []string{"error"}, types(items), "no start event is made up")
	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, ErrStreamIncomplete)
	assert.Equal(t, "test-provider", msg.Provider)
	assert.Empty(t, msg.Content)
	assert.NotNil(t, msg.Content)
}

func TestStreamResultWithoutReader(t *testing.T) {
	// Nobody reads the events while the producer runs. The result is settled
	// without a reader.
	s := newTestStream(context.Background(), 64, func(a *Assembler) {
		a.Start()
		i := a.TextStart("")
		a.TextDelta(i, "hi")
		a.TextEnd(i, "hi", nil)
		a.Done(protocol.StopStop)
	})
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	assert.Equal(t, protocol.Text{Text: "hi"}, msg.Content[0])
	items := drain(s)
	assert.Equal(t, []string{"start", "text_start", "text_delta", "text_end", "done"}, types(items))
}

func TestStreamResultSettlesBeforeTerminalSendCanBlock(t *testing.T) {
	// Buffer 1 holds the start event, so the terminal send must wait for a reader.
	s := newTestStream(context.Background(), 1, func(a *Assembler) {
		a.Start()
		a.Done(protocol.StopStop)
	})
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	assert.Equal(t, []string{"start", "done"}, types(drain(s)))
}

func TestStreamResultWaitCancelBeforeSettle(t *testing.T) {
	release := make(chan struct{})
	s := newTestStream(context.Background(), 8, func(a *Assembler) {
		a.Start()
		<-release
		a.Done(protocol.StopStop)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Result(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// The wait did not cancel the request: the stream still finishes.
	close(release)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	drain(s)
}

func TestStreamSettledResultWinsOverCancelledWait(t *testing.T) {
	s := newTestStream(context.Background(), 8, func(a *Assembler) {
		a.Start()
		a.Done(protocol.StopLength)
	})
	_, err := waitResult(t, s)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 20 {
		msg, err := s.Result(ctx)
		require.NoError(t, err)
		assert.Equal(t, protocol.StopLength, msg.StopReason)
	}
	drain(s)
}

func TestStreamResultReturnsIndependentCopies(t *testing.T) {
	s := newTestStream(context.Background(), 8, func(a *Assembler) {
		a.Start()
		a.ToolStart("a", "echo", nil, nil, nil)
		a.ToolEnd(0, &protocol.ToolCall{Arguments: json.RawMessage(`{"k":1}`)})
		a.SetMetadata(Metadata{ResponseID: sptr("r1")})
		a.Done(protocol.StopToolUse)
	})
	drain(s)
	first, err := waitResult(t, s)
	require.NoError(t, err)
	call := first.Content[0].(protocol.ToolCall)
	call.Arguments[2] = 'z'
	*first.ResponseID = "changed"
	second, err := waitResult(t, s)
	require.NoError(t, err)
	assert.JSONEq(t, `{"k":1}`, string(second.Content[0].(protocol.ToolCall).Arguments))
	assert.Equal(t, "r1", *second.ResponseID)
}

func TestStreamSecondTerminalIsNoOp(t *testing.T) {
	s := newTestStream(context.Background(), 16, func(a *Assembler) {
		a.Start()
		a.Done(protocol.StopStop)
		assert.True(t, a.Settled())
		a.Done(protocol.StopLength)
		a.Fail(protocol.StopError, "late", nil)
		a.TextStart("late")
		assert.Equal(t, -1, a.ToolStart("x", "y", nil, nil, nil))
		a.SetUsage(protocol.Usage{Input: 99})
		assert.ErrorIs(t, a.Emit(protocol.TextDeltaEvent{}), ErrStreamClosed)
	})
	items := drain(s)
	assert.Equal(t, []string{"start", "done"}, types(items))
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopStop, msg.StopReason)
	assert.Zero(t, msg.Usage.Input)
	assert.Nil(t, msg.ErrorMessage)
}

func TestStreamAbortWhileDrained(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	s := newTestStream(ctx, 32, func(a *Assembler) {
		a.Start()
		a.SetUsage(protocol.Usage{Input: 7, Output: 3, TotalTokens: 10})
		i := a.TextStart("")
		a.TextDelta(i, "abc")
		close(ready)
		<-a.Context().Done()
		// The body returns without a terminal call after the cancellation.
	})
	<-ready
	cancel()
	items := drain(s)
	last, ok := items[len(items)-1].Event.(protocol.ErrorEvent)
	require.True(t, ok, "the error event is queued because the channel has room")
	assert.Equal(t, protocol.StopAborted, last.Reason)
	assert.Equal(t, int64(10), items[len(items)-1].Usage.TotalTokens)

	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Equal(t, "Request was aborted", *msg.ErrorMessage)
	assert.Equal(t, protocol.Text{Text: "abc"}, msg.Content[0])
	assert.Equal(t, int64(7), msg.Usage.Input)
	assert.Equal(t, protocol.StopAborted, last.Error.StopReason)
}

func TestStreamAbortReportsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s := newTestStream(ctx, 8, func(a *Assembler) {
		a.Start()
		<-a.Context().Done()
		a.Done(protocol.StopStop) // a terminal call after cancellation aborts
	})
	items := drain(s)
	assert.Equal(t, []string{"start", "error"}, types(items))
	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
}

func TestStreamAbortBeforeStartMakesOneEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newTestStream(ctx, 4, func(a *Assembler) {
		a.Start()
		a.TextStart("x")
	})
	items := drain(s)
	require.Equal(t, []string{"error"}, types(items))
	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "test-model", msg.Model, "the message is built from the seed")
	assert.Equal(t, int64(42), msg.Timestamp)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
}

func TestStreamAbortWithFullAbandonedChannelLeaksNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newTestStream(ctx, 1, func(a *Assembler) {
		a.Start()
		i := a.TextStart("")
		for !a.Settled() {
			a.TextDelta(i, "x")
		}
	})
	// Nobody reads the channel. The producer blocks on the full channel.
	time.Sleep(20 * time.Millisecond)
	cancel()
	msg, err := waitResult(t, s)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	// The terminal event may be missing from the channel; only the result is
	// authoritative. The producer goroutine must be gone.
	require.NoError(t, goleak.Find())
}

func TestStreamPanicInBodyBecomesError(t *testing.T) {
	s := newTestStream(context.Background(), 8, func(a *Assembler) {
		a.Start()
		a.TextStart("partial")
		panic("boom")
	})
	items := drain(s)
	last, ok := items[len(items)-1].Event.(protocol.ErrorEvent)
	require.True(t, ok)
	assert.Equal(t, protocol.StopError, last.Reason)
	msg, err := waitResult(t, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, protocol.Text{Text: "partial"}, msg.Content[0])
}

func TestStreamNilContextAndNegativeBuffer(t *testing.T) {
	s := NewStream(nil, -3, testSeed(), func(a *Assembler) { //nolint:staticcheck // the nil context is the case under test
		a.Start()
		a.Done(protocol.StopStop)
	})
	assert.Equal(t, []string{"start", "done"}, types(drain(s)))
}

func TestStreamEventsAreInOrderUnderRace(t *testing.T) {
	const n = 500
	s := newTestStream(context.Background(), 2, func(a *Assembler) {
		a.Start()
		i := a.TextStart("")
		for range n {
			a.TextDelta(i, "x")
		}
		a.TextEnd(i, "done", nil)
		a.Done(protocol.StopStop)
	})
	items := drain(s)
	assert.Len(t, items, n+4)
	assert.Equal(t, "done", items[len(items)-1].Event.EventType())
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.Text{Text: "done"}, msg.Content[0])
}

func TestErrStreamIncompleteText(t *testing.T) {
	assert.Contains(t, ErrStreamIncomplete.Error(), "ended without")
	assert.False(t, errors.Is(ErrStreamIncomplete, context.Canceled))
}
