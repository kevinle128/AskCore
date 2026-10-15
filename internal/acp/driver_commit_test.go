package acp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestTakeFencesModelPreparation(t *testing.T) {
	for _, preparation := range []string{"model auth", "provider readiness"} {
		t.Run(preparation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			wait := func() { close(entered); <-release }
			var edit func(*agent.Config)
			if preparation == "provider readiness" {
				edit = func(cfg *agent.Config) {
					cfg.Ready = func(context.Context, providers.Model, string) error { wait(); return nil }
				}
			}
			cfg := testConfig(says("ok"), edit)
			cfg.RequireRoute = true
			if preparation == "model auth" {
				cfg.ModelAuth = func(context.Context, providers.Model, string) error { wait(); return nil }
			}
			p := newAdapterPeer(t, cfg, nil)
			p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
			sid := p.newRouted("c1", "")
			s, _ := p.a.Session(sid)
			before := s.Agent().State().Model
			params := map[string]any{"sessionId": sid, "modelId": modelID(providers.AnthropicSonnet46())}
			if preparation == "model auth" {
				params["authMethodId"] = "api-key"
			}
			id := p.send(protocol.ACPSetModel, withRoute(params, route("c1", 1, true, "")))
			<-entered
			require.False(t, p.a.QuiesceIfIdle(), "preparation counts as work")
			require.Equal(t, uint64(2), p.takeGen(sid, "c2", false, ""))
			unblock()
			require.Equal(t, protocol.ACPErrNotDriver, p.await(id).errKind(t))
			require.Equal(t, before, s.Agent().State().Model)
		})
	}
}

func TestAuthenticateKeepsIdleAdmissionOpenUntilItEnds(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	cfg := testConfig(says("ok"), nil)
	cfg.Authenticate = func(context.Context, string) error { close(entered); <-release; return nil }
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	id := p.send("authenticate", map[string]any{"methodId": "api-key"})
	<-entered
	require.False(t, p.a.QuiesceIfIdle())
	unblock()
	require.Nil(t, p.await(id).Error)
	require.True(t, p.a.QuiesceIfIdle())
	f := p.call("authenticate", map[string]any{"methodId": "api-key"})
	require.Equal(t, protocol.ACPErrDisposed, f.errKind(t), "quiesce closes auth admission too")
}

func TestTakeFencesRunWaitingForIdle(t *testing.T) {
	for _, method := range []string{"session/prompt", protocol.ACPContinue} {
		t.Run(method, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "done")} })
			sid := p.newRouted("c1", "")
			s, _ := p.a.Session(sid)
			// An input-owned run has no ACP binding; the old call must wait for it.
			_, err := s.Agent().Steer(hostUser("queued"))
			require.NoError(t, err)
			params := map[string]any{"sessionId": sid}
			if method == "session/prompt" {
				params = promptParams(sid, "old driver")
			}
			id := p.send(method, withRoute(params, route("c1", 1, true, "")))
			require.Eventually(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.muts == 1
			}, time.Second, time.Millisecond)
			require.Equal(t, uint64(2), p.takeGen(sid, "c2", false, ""))
			unblock()
			require.Equal(t, protocol.ACPErrNotDriver, p.await(id).errKind(t))
			require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)))
			require.Len(t, s.Agent().State().Messages, 2, "the old driver added no turn")
		})
	}
}

func TestDriverMutationCommitChecksGenerationAfterAdmission(t *testing.T) {
	for _, method := range []string{protocol.ACPReset, protocol.ACPSetThinking, protocol.ACPSteer, protocol.ACPFollowUp, protocol.ACPRemove, "session/cancel"} {
		t.Run(method, func(t *testing.T) {
			runRelease := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(runRelease) }) }
			defer unblock()
			p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(runRelease, "done")} })
			sid := p.newRouted("c1", "")
			s, _ := p.a.Session(sid)
			beforeEpoch := s.Epoch()
			var runDone chan error
			if method == "session/cancel" {
				runDone = make(chan error, 1)
				go func() { runDone <- s.Agent().Prompt(context.Background(), hostUser("running")) }()
				require.Eventually(t, func() bool { return s.Agent().State().Status == agent.Running }, time.Second, time.Millisecond)
			}
			meta := route("c1", 1, true, "")
			params := map[string]any{"sessionId": sid, "level": "low", "inputId": "none", "content": []any{map[string]any{"type": "text", "text": "old"}}}
			raw, err := json.Marshal(withRoute(params, meta))
			require.NoError(t, err)
			// Hold the common commit point. Admission succeeds before ownership changes.
			s.commit.Lock()
			var unlockOnce sync.Once
			unlock := func() { unlockOnce.Do(s.commit.Unlock) }
			defer unlock()
			done := make(chan error, 1)
			go func() {
				if method == "session/cancel" {
					done <- p.a.Cancel(context.Background(), sdk.CancelNotification{SessionId: sdk.SessionId(sid), Meta: meta})
				} else {
					_, err := p.a.HandleExtensionMethod(context.Background(), method, raw)
					done <- err
				}
			}()
			require.Eventually(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.muts == 1
			}, time.Second, time.Millisecond)
			// Commit the same generation change as Take while owning its commit lock.
			s.mu.Lock()
			s.gen++
			s.mu.Unlock()
			unlock()
			err = <-done
			if method == "session/cancel" {
				require.NoError(t, err, "stale cancel notifications are dropped")
				unblock()
				require.NoError(t, <-runDone)
				messages := s.Agent().State().Messages
				require.Equal(t, protocol.StopStop, messages[len(messages)-1].(protocol.AssistantMessage).StopReason)
			} else {
				var response *sdk.RequestError
				require.True(t, errors.As(err, &response))
				require.Equal(t, protocol.ACPErrNotDriver, response.Data.(protocol.ACPErrorData).Kind)
			}
			if runDone == nil {
				require.Empty(t, s.Agent().State().Messages)
			}
			require.Equal(t, beforeEpoch, s.Epoch(), "the old reset did not change the epoch")
			require.Equal(t, protocol.ThinkingOff, s.Agent().State().ThinkingLevel)
		})
	}
}

func TestTakeChecksRunStatusAfterCommitLock(t *testing.T) {
	runRelease := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(runRelease) }) }
	defer unblock()
	p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(runRelease, "done")} })
	sid := p.newRouted("c1", "")
	s, _ := p.a.Session(sid)
	s.commit.Lock()
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(s.commit.Unlock) }
	defer unlock()
	done := make(chan error, 1)
	go func() {
		_, err := s.Take(protocol.ACPRouteMeta{ClientID: "c2", LiveDriver: true})
		done <- err
	}()
	// Take owns host admission before it waits for the commit lock.
	require.Eventually(t, func() bool {
		if s.host.admit.TryLock() {
			s.host.admit.Unlock()
			return false
		}
		return true
	}, time.Second, time.Millisecond)
	// Start input-owned work before Take can read its final idle status.
	_, err := s.Agent().Steer(hostUser("queued"))
	require.NoError(t, err)
	unlock()
	require.ErrorIs(t, <-done, agent.ErrBusy)
	require.Equal(t, uint64(1), s.Gen())
	unblock()
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)))
}
