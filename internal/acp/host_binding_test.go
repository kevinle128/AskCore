package acp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// heldFixture builds a host whose first model call waits for release.
func heldFixture(t *testing.T, release <-chan struct{}, write WriteEvent, opts ...HostOption) *Host {
	t.Helper()
	h := NewHost(context.Background(), func(_ context.Context, id, cwd string) (*agent.Agent, error) {
		p, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := p.Model("faux-1")
		p.Set(faux.Func(func(ctx context.Context, _ faux.Call) (faux.Step, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return faux.Say("held"), nil
		}), faux.Say("after"))
		return agent.New(agent.Config{SessionID: id, LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream, Cwd: cwd}})
	}, write, opts...)
	t.Cleanup(func() { require.NoError(t, h.Close()) })
	return h
}

func TestHostBusyCallDoesNotBindSiblingRun(t *testing.T) {
	h := hostFixture(t, nil, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	result, err := s.execute(hostDeadline(t), func(context.Context) error {
		if _, err := s.agent.Steer(hostUser("steered")); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			s.mu.Lock()
			bound := s.binding != nil && s.binding.runID != ""
			s.mu.Unlock()
			if bound {
				break
			}
			time.Sleep(time.Millisecond)
		}
		return agent.ErrBusy
	})
	require.ErrorIs(t, err, agent.ErrBusy)
	require.Empty(t, result.RunID, "a rejected call must not claim the run of another input")
	require.NoError(t, s.agent.WaitForIdle(hostDeadline(t)))
}

func TestHostRequestCancellationKeepsRunAndBarrier(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h := hostFixture(t, nil, func(ctx context.Context, _, _ string, ev protocol.Event) error {
		if ev.EventType() == protocol.TypeAgentSettled {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	var relOnce sync.Once
	t.Cleanup(func() { relOnce.Do(func() { close(release) }) })
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() { r, err := s.Prompt(reqCtx, hostUser("hi")); done <- outcome{r, err} }()
	<-entered
	cancel()
	select {
	case o := <-done:
		t.Fatalf("request cancellation released the barrier early: %+v", o)
	case <-time.After(100 * time.Millisecond):
	}
	relOnce.Do(func() { close(release) })
	o := <-done
	require.NoError(t, o.err)
	require.NotEmpty(t, o.res.RunID)
}

func TestHostRequestCancellationDoesNotAbortRun(t *testing.T) {
	release := make(chan struct{})
	h := heldFixture(t, release, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	reqCtx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { r, _ := s.Prompt(reqCtx, hostUser("hi")); done <- r }()
	require.Eventually(t, func() bool { return s.agent.State().Status == agent.Running }, 5*time.Second, time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)
	r := <-done
	require.Equal(t, "completed", r.Reason)
	msgs := s.agent.State().Messages
	require.Len(t, msgs, 2)
	require.Equal(t, protocol.StopStop, msgs[1].(protocol.AssistantMessage).StopReason)
}

func TestHostResultReasonAfterAbort(t *testing.T) {
	release := make(chan struct{})
	h := heldFixture(t, release, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	done := make(chan Result, 1)
	errs := make(chan error, 1)
	go func() { r, err := s.Prompt(context.Background(), hostUser("hi")); done <- r; errs <- err }()
	require.Eventually(t, func() bool { return s.agent.State().Status == agent.Running }, 5*time.Second, time.Millisecond)
	s.agent.Abort()
	r := <-done
	t.Logf("abort prompt error: %v", <-errs)
	require.Equal(t, "aborted", r.Reason)
	require.Equal(t, "user", r.Cause)
}

func TestHostQueueOverflowFailsConnection(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h := hostFixture(t, nil, func(ctx context.Context, _, _ string, _ protocol.Event) error {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	WithQueueLimit(3, 0)(h)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := s.Prompt(hostDeadline(t), hostUser("hi")); done <- err }()
	<-entered
	var got error
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("run did not stop after the queue overflowed")
	}
	close(release)
	require.True(t, errors.Is(got, ErrQueueOverflow), "got %v", got)
	require.ErrorIs(t, h.Failure(), ErrQueueOverflow)
}

func TestHostResetFencesEpochOfHeldEvents(t *testing.T) {
	var mu sync.Mutex
	got := map[uint64]string{}
	h := hostFixture(t, nil, func(_ context.Context, _, epoch string, ev protocol.Event) error {
		mu.Lock()
		got[ev.Env().Seq] = epoch
		mu.Unlock()
		return nil
	})
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	oldEpoch := s.Epoch()
	s.mu.Lock()
	s.resetting = true
	s.mu.Unlock()
	before := &protocol.QueueUpdate{Envelope: protocol.Envelope{Seq: 3}, Steering: []string{"old"}}
	after := &protocol.QueueUpdate{Envelope: protocol.Envelope{Seq: 9}, FollowUp: []string{"new"}}
	require.NoError(t, s.observe(before))
	require.NoError(t, s.observe(after))
	s.releaseHeld(Cut{Cursor: agent.Cursor{Epoch: "fresh", Seq: 5}}, nil)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 }, 5*time.Second, time.Millisecond)
	mu.Lock()
	require.Equal(t, oldEpoch, got[3])
	require.Equal(t, "fresh", got[9])
	mu.Unlock()
	steering, followUp := s.Queues()
	require.Empty(t, steering)
	require.Equal(t, []string{"new"}, followUp)
}

func TestHostContinueStateErrorDoesNotBindSiblingRun(t *testing.T) {
	for _, stateErr := range []error{agent.ErrContinueEmpty, agent.ErrContinueFromAssistant} {
		h := hostFixture(t, nil, nil)
		s, err := h.NewSession(hostDeadline(t), t.TempDir())
		require.NoError(t, err)
		result, err := s.execute(hostDeadline(t), func(context.Context) error {
			if _, err := s.agent.Steer(hostUser("steered")); err != nil {
				return err
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				s.mu.Lock()
				bound := s.binding != nil && s.binding.runID != ""
				s.mu.Unlock()
				if bound {
					break
				}
				time.Sleep(time.Millisecond)
			}
			return stateErr
		})
		require.ErrorIs(t, err, stateErr)
		require.Empty(t, result.RunID, "a state error must not claim the run of another input")
		require.NoError(t, s.agent.WaitForIdle(hostDeadline(t)))
	}
}

// A cancel that arrives after admission but before the run exists must still
// stop the run when it binds.
func TestHostCancelBeforeRunBindsStopsRun(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h := heldFixture(t, release, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	done := make(chan Result, 1)
	go func() {
		r, _ := s.execute(hostDeadline(t), func(runCtx context.Context) error {
			s.Abort()
			return s.agent.Prompt(runCtx, hostUser("hi"))
		})
		done <- r
	}()
	select {
	case r := <-done:
		require.Equal(t, "aborted", r.Reason)
	case <-time.After(3 * time.Second):
		t.Fatal("cancel before the run bound was lost")
	}
}

// A prompt that comes while the Agent still delivers the queue_update of a
// removed input must wait for that delivery instead of returning busy.
func TestHostPromptAfterRemoveWaitsForQueueDelivery(t *testing.T) {
	release := make(chan struct{})
	h := heldFixture(t, release, nil)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	var armed atomic.Bool
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	s.agent.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.QueueUpdate); ok && armed.CompareAndSwap(true, false) {
			entered <- struct{}{}
			<-hold
		}
		return nil
	})
	_, err = s.agent.FollowUp(hostUser("first"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return s.agent.State().Status == agent.Running }, 5*time.Second, time.Millisecond)
	id, err := s.agent.FollowUp(hostUser("second"))
	require.NoError(t, err)
	armed.Store(true)
	require.True(t, s.agent.Remove(id))
	<-entered
	close(release)

	done := make(chan error, 1)
	go func() { _, err := s.Prompt(hostDeadline(t), hostUser("next")); done <- err }()
	time.Sleep(50 * time.Millisecond)
	close(hold)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not finish")
	}
}

// blockCloseLog is a real in-memory log whose Close waits while a test holds
// Agent.Reset inside the close of the old log.
type blockCloseLog struct {
	*sessions.MemoryLog
	ctl *closeControl
}

type closeControl struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (l blockCloseLog) Close() error {
	if l.ctl.armed.CompareAndSwap(true, false) {
		l.ctl.entered <- struct{}{}
		<-l.ctl.release
	}
	return l.MemoryLog.Close()
}

// An input that comes while a reset runs must not start a run in the new
// context before the epoch cut is known. Events keep the order of the Agent and
// carry the epoch that they belong to.
func TestHostResetRefusesInputUntilCutIsKnown(t *testing.T) {
	ctl := &closeControl{entered: make(chan struct{}, 1), release: make(chan struct{})}
	type seen struct {
		epoch string
		seq   uint64
	}
	var mu sync.Mutex
	var got []seen
	h := hostFixture(t, func(c *agent.Config) {
		c.NewContext = func() sessions.Writer { return blockCloseLog{MemoryLog: &sessions.MemoryLog{}, ctl: ctl} }
	}, func(_ context.Context, _, epoch string, ev protocol.Event) error {
		mu.Lock()
		got = append(got, seen{epoch, ev.Env().Seq})
		mu.Unlock()
		return nil
	})
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)
	oldEpoch := s.Epoch()

	ctl.armed.Store(true)
	cutCh := make(chan Cut, 1)
	go func() {
		cut, err := s.Reset(hostDeadline(t))
		if err == nil {
			cutCh <- cut
		}
	}()
	<-ctl.entered
	_, steerErr := s.Steer(hostUser("during reset"))
	_, followErr := s.FollowUp(hostUser("during reset"))
	close(ctl.release)
	require.ErrorIs(t, steerErr, agent.ErrBusy)
	require.ErrorIs(t, followErr, agent.ErrBusy)
	cut := <-cutCh
	require.NotEqual(t, oldEpoch, cut.Cursor.Epoch)

	res, err := s.Prompt(hostDeadline(t), hostUser("after reset"))
	require.NoError(t, err)
	require.Equal(t, cut.Cursor.Epoch, res.Epoch)
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, got)
	var last uint64
	for _, e := range got {
		require.GreaterOrEqual(t, e.seq, last, "events leave in Agent order")
		last = e.seq
		if e.seq > cut.Cursor.Seq {
			require.Equal(t, cut.Cursor.Epoch, e.epoch)
		} else {
			require.Equal(t, oldEpoch, e.epoch)
		}
	}
}
