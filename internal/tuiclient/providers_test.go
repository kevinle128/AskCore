package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// sent is one request that the client wrote.
type sent struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

// script is the peer of a scripted link. It records every request and answers
// the ones that have a rule. A rule returns the result, or a fault message. A
// rule that returns hold set answers nothing; the test answers by hand.
type script struct {
	mu    sync.Mutex
	link  *scriptedLink
	sent  []sent
	rules map[string]func(s sent) (result any, fault string, hold bool)
}

func newScript(rules map[string]func(s sent) (any, string, bool)) *script {
	sc := &script{rules: rules}
	sc.link = newScriptedLink(nil)
	sc.link.onSend = sc.onSend
	return sc
}

func (sc *script) onSend(raw json.RawMessage) error {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	s := sent{ID: req.ID, Method: req.Method, Params: req.Params}
	sc.mu.Lock()
	sc.sent = append(sc.sent, s)
	rule := sc.rules[req.Method]
	sc.mu.Unlock()
	if rule == nil || len(req.ID) == 0 {
		return nil
	}
	result, fault, hold := rule(s)
	if hold {
		return nil
	}
	sc.answer(s.ID, result, fault)
	return nil
}

func (sc *script) answer(id json.RawMessage, result any, fault string) {
	if fault != "" {
		sc.link.replies <- json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":%q,"data":{"kind":"internal"}}}`, id, fault))
		return
	}
	body, _ := json.Marshal(result)
	sc.link.replies <- json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, body))
}

func (sc *script) methods() []string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	var out []string
	for _, s := range sc.sent {
		out = append(out, s.Method)
	}
	return out
}

func (sc *script) count(method string) int {
	n := 0
	for _, m := range sc.methods() {
		if m == method {
			n++
		}
	}
	return n
}

func (sc *script) last(method string) sent {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for i := len(sc.sent) - 1; i >= 0; i-- {
		if sc.sent[i].Method == method {
			return sc.sent[i]
		}
	}
	return sent{}
}

// wait blocks until the client has sent a request of the method.
func (sc *script) wait(t *testing.T, method string) sent {
	t.Helper()
	require.Eventually(t, func() bool { return sc.count(method) > 0 }, 5*time.Second, 5*time.Millisecond, "%s is sent", method)
	return sc.last(method)
}

func openScripted(t *testing.T, sc *script, mode tui.Mode, instance string) *Client {
	t.Helper()
	c, err := Open(context.Background(), Config{
		NoReconnect: true,
		Dial: func(context.Context) (Link, error) {
			return Link{Transport: sc.link, Info: tui.ConnInfo{Mode: mode, InstanceID: instance}}, nil
		},
	})
	require.NoError(t, err)
	return c
}

func fixed(v any) func(sent) (any, string, bool) {
	return func(sent) (any, string, bool) { return v, "", false }
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func TestClientProviderCallsRoundTrip(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPProvidersList: fixed(map[string]any{"generation": "7",
			"options":   []any{map[string]any{"providerId": "openai", "methodId": "api-key", "name": "OpenAI API key", "state": "env", "envVar": "OPENAI_API_KEY"}},
			"providers": []any{map[string]any{"providerId": "openai", "usable": true, "modelCount": 3}}}),
		protocol.ACPProvidersAPIKey: fixed(protocol.ACPLoginOutcome{LoginID: "k1", Status: protocol.ACPLoginStatusSaved, NotChecked: true}),
		protocol.ACPLoginStart:      fixed(protocol.ACPLoginStartResult{LoginID: "L1", Kind: protocol.ACPLoginKindBrowser, URL: "https://example.test/auth"}),
		protocol.ACPLoginInput:      fixed(struct{}{}),
		protocol.ACPLoginWait:       fixed(map[string]any{"loginId": "L1", "status": "signed_in", "account": "dev@example.test", "generation": "8"}),
		protocol.ACPLoginCancel:     fixed(protocol.ACPLoginCancelResult{Cancelled: true}),
		protocol.ACPProvidersLogout: fixed(map[string]any{"removed": true, "envVar": "OPENAI_API_KEY", "clearedModel": "openai/gpt-5.5@responses", "generation": "9"}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()

	list, err := c.Providers(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(7), list.Generation)
	require.Len(t, list.Options, 1)
	require.Equal(t, protocol.ACPLoginStateEnv, list.Options[0].State)
	require.Equal(t, 3, list.Providers[0].ModelCount)

	out, err := c.SaveAPIKey(ctx, "k1", "alibaba-token-plan", "fake-key-0001")
	require.NoError(t, err)
	require.True(t, out.NotChecked)
	require.Equal(t, protocol.ACPAPIKeyRequest{LoginID: "k1", ProviderID: "alibaba-token-plan", Key: "fake-key-0001"},
		decode[protocol.ACPAPIKeyRequest](t, sc.last(protocol.ACPProvidersAPIKey).Params))

	start, err := c.StartLogin(ctx, "openai", "openai-chatgpt")
	require.NoError(t, err)
	require.Equal(t, "L1", start.LoginID)
	require.Equal(t, protocol.ACPLoginKindBrowser, start.Kind)
	require.Equal(t, protocol.ACPLoginStartRequest{ProviderID: "openai", MethodID: "openai-chatgpt"},
		decode[protocol.ACPLoginStartRequest](t, sc.last(protocol.ACPLoginStart).Params))

	require.NoError(t, c.SendLoginCode(ctx, "L1", "fake-code"))
	require.Equal(t, protocol.ACPLoginInputRequest{LoginID: "L1", Code: "fake-code"},
		decode[protocol.ACPLoginInputRequest](t, sc.last(protocol.ACPLoginInput).Params))

	done, err := c.WaitLogin(ctx, "L1")
	require.NoError(t, err)
	require.Equal(t, protocol.ACPLoginStatusSignedIn, done.Status)
	require.Equal(t, "dev@example.test", done.Account)
	require.Equal(t, uint64(8), done.Generation)

	cancelled, err := c.CancelLogin(ctx, "L1")
	require.NoError(t, err)
	require.True(t, cancelled)

	lo, err := c.Logout(ctx, "openai", "api-key")
	require.NoError(t, err)
	require.True(t, lo.Removed)
	require.Equal(t, "openai/gpt-5.5@responses", lo.ClearedModel)
	require.Equal(t, uint64(9), lo.Generation)
	req := decode[protocol.ACPLogoutRequest](t, sc.last(protocol.ACPProvidersLogout).Params)
	require.Equal(t, protocol.ACPLogoutRequest{ProviderID: "openai", MethodID: "api-key"}, req, "no session, so no session id")
}

func TestClientLogoutNamesCurrentSession(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPProvidersLogout: fixed(protocol.ACPLogoutResult{Removed: true}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	c.mu.Lock()
	c.sess = sessionState{has: true, id: "sess-1"}
	c.mu.Unlock()
	_, err := c.Logout(context.Background(), "openai", "api-key")
	require.NoError(t, err)
	require.Equal(t, "sess-1", decode[protocol.ACPLogoutRequest](t, sc.last(protocol.ACPProvidersLogout).Params).SessionID)
}

func TestClientProviderBusyIsRequestError(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){})
	sc.rules[protocol.ACPLoginStart] = func(s sent) (any, string, bool) {
		sc.link.replies <- json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32010,"message":"busy","data":{"kind":"provider_busy"}}}`, s.ID))
		return nil, "", true
	}
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	_, err := c.StartLogin(context.Background(), "openai", "openai-chatgpt")
	var re *tui.RequestError
	require.ErrorAs(t, err, &re)
	require.Equal(t, string(protocol.ACPErrProviderBusy), re.Kind)
}

func TestClientProvidersNeedLeaderInProcess(t *testing.T) {
	sc := newScript(nil)
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()

	_, err := c.Providers(ctx)
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	_, err = c.SaveAPIKey(ctx, "k1", "openai", "fake-key-0001")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	_, err = c.StartLogin(ctx, "openai", "openai-chatgpt")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	require.ErrorIs(t, c.SendLoginCode(ctx, "L1", "fake-code"), tui.ErrNeedsLeader)
	_, err = c.WaitLogin(ctx, "L1")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	_, err = c.CancelLogin(ctx, "L1")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	_, err = c.Logout(ctx, "openai", "api-key")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)

	require.Empty(t, sc.methods(), "nothing but the handshake reached the pipe")
}

func providersChanged(gen string, provider, change string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","method":"_ask/providers/changed","params":{"generation":%q,"providerId":%q,"change":%q}}`, gen, provider, change))
}

func eventNote(seq int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","method":"_ask/session/event","params":{"subscriptionId":"sub","sessionId":"s","epoch":"e","seq":"%d","event":{"type":"agent_start","seq":%d}}}`, seq, seq))
}

func isProvidersChanged(m tui.Message) bool { _, ok := m.(tui.ProvidersChangedMsg); return ok }

func TestClientProvidersChangedMessage(t *testing.T) {
	sc := newScript(nil)
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	c.mu.Lock()
	c.sess = sessionState{has: true, id: "s", sub: "sub", epoch: "e"}
	c.mu.Unlock()

	sc.link.replies <- eventNote(1)
	sc.link.replies <- providersChanged("4", "openai", "saved")
	sc.link.replies <- eventNote(2)
	got := drain(t, c, func(m tui.Message) bool {
		e, ok := m.(tui.SessionEventMsg)
		return ok && e.Seq == 2
	})
	var kinds []string
	for _, m := range got {
		switch v := m.(type) {
		case tui.SessionEventMsg:
			kinds = append(kinds, fmt.Sprintf("event%d", v.Seq))
		case tui.ProvidersChangedMsg:
			kinds = append(kinds, "changed")
			require.Equal(t, tui.ProvidersChangedMsg{Generation: 4, Provider: "openai", Change: protocol.ACPProviderChangeSaved}, v)
		}
	}
	require.Equal(t, []string{"event1", "changed", "event2"}, kinds, "in the order of the link")
}

func TestClientDropsStaleProvidersChanged(t *testing.T) {
	sc := newScript(nil)
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()

	for _, g := range []string{"5", "3", "5", "6"} {
		sc.link.replies <- providersChanged(g, "openai", "removed")
	}
	var gens []uint64
	drain(t, c, func(m tui.Message) bool {
		if v, ok := m.(tui.ProvidersChangedMsg); ok {
			gens = append(gens, v.Generation)
			return v.Generation == 6
		}
		return false
	})
	require.Equal(t, []uint64{5, 6}, gens, "an older or equal generation is dropped")
}

func TestClientResetsProvidersGenerationOnNewLeader(t *testing.T) {
	first := newScript(nil)
	second := newScript(nil)
	var mu sync.Mutex
	dials := 0
	c, err := Open(context.Background(), Config{
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		Dial: func(context.Context) (Link, error) {
			mu.Lock()
			defer mu.Unlock()
			dials++
			if dials == 1 {
				return Link{Transport: first.link, Info: tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "leader-a"}}, nil
			}
			return Link{Transport: second.link, Info: tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "leader-b"}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()

	first.link.replies <- providersChanged("5", "openai", "saved")
	drain(t, c, isProvidersChanged)
	require.NoError(t, first.link.Close())
	drain(t, c, func(m tui.Message) bool {
		_, ok := m.(tui.ConnectedMsg)
		return ok && m.(tui.ConnectedMsg).Info.InstanceID == "leader-b"
	})

	second.link.replies <- providersChanged("1", "openai", "saved")
	got := drain(t, c, isProvidersChanged)
	require.Equal(t, uint64(1), got[len(got)-1].(tui.ProvidersChangedMsg).Generation, "a new leader counts from the start")
}

func TestClientSameInstanceAfterReconnectKeepsGeneration(t *testing.T) {
	first := newScript(nil)
	second := newScript(nil)
	var mu sync.Mutex
	dials := 0
	c, err := Open(context.Background(), Config{
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		Dial: func(context.Context) (Link, error) {
			mu.Lock()
			defer mu.Unlock()
			dials++
			sc := first
			if dials > 1 {
				sc = second
			}
			return Link{Transport: sc.link, Info: tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "leader-a"}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()

	first.link.replies <- providersChanged("5", "openai", "saved")
	drain(t, c, isProvidersChanged)
	require.NoError(t, first.link.Close())
	drain(t, c, func(m tui.Message) bool { _, ok := m.(tui.ConnectedMsg); return ok && dialsOf(&mu, &dials) > 1 })

	second.link.replies <- providersChanged("5", "openai", "saved")
	second.link.replies <- providersChanged("6", "openai", "removed")
	got := drain(t, c, isProvidersChanged)
	require.Equal(t, uint64(6), got[len(got)-1].(tui.ProvidersChangedMsg).Generation, "the repeat of 5 is dropped")
}

func dialsOf(mu *sync.Mutex, n *int) int {
	mu.Lock()
	defer mu.Unlock()
	return *n
}

func TestClientCancelKeySaveReportsBeforeCommit(t *testing.T) {
	var keyMu sync.Mutex
	var keyReq sent
	var sc *script
	sc = newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPProvidersAPIKey: func(s sent) (any, string, bool) {
			keyMu.Lock()
			defer keyMu.Unlock()
			keyReq = s
			return nil, "", true
		},
		protocol.ACPLoginCancel: func(s sent) (any, string, bool) {
			// The leader stops the check, answers the cancel, then ends the save.
			keyMu.Lock()
			defer keyMu.Unlock()
			sc.answer(s.ID, protocol.ACPLoginCancelResult{Cancelled: true}, "")
			sc.answer(keyReq.ID, protocol.ACPLoginOutcome{LoginID: "k1", Status: protocol.ACPLoginStatusCancelled, Reason: protocol.ACPLoginReasonCancelled}, "")
			return nil, "", true
		},
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()

	type res struct {
		out protocol.ACPLoginOutcome
		err error
	}
	saved := make(chan res, 1)
	go func() {
		out, err := c.SaveAPIKey(ctx, "k1", "openai", "fake-key-0001")
		saved <- res{out, err}
	}()
	sc.wait(t, protocol.ACPProvidersAPIKey)

	cancelled, err := c.CancelLogin(ctx, "k1")
	require.NoError(t, err)
	require.True(t, cancelled, "the leader says that nothing was stored")
	select {
	case r := <-saved:
		require.NoError(t, r.err)
		require.Equal(t, protocol.ACPLoginStatusCancelled, r.out.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("the key save did not end")
	}
}

func TestClientWaitLoginLinkLost(t *testing.T) {
	first := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPLoginWait: func(sent) (any, string, bool) { return nil, "", true },
	})
	second := newScript(nil)
	var mu sync.Mutex
	dials := 0
	c, err := Open(context.Background(), Config{
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		Dial: func(context.Context) (Link, error) {
			mu.Lock()
			defer mu.Unlock()
			dials++
			if dials == 1 {
				return Link{Transport: first.link, Info: tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "leader-a"}}, nil
			}
			return Link{Transport: second.link, Info: tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: "leader-a"}}, nil
		},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()

	errc := make(chan error, 1)
	go func() {
		_, err := c.WaitLogin(context.Background(), "L1")
		errc <- err
	}()
	first.wait(t, protocol.ACPLoginWait)
	require.NoError(t, first.link.Close())
	select {
	case err := <-errc:
		require.ErrorIs(t, err, tui.ErrOutcomeUnknown)
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end")
	}
	drain(t, c, func(m tui.Message) bool { _, ok := m.(tui.ConnectedMsg); return ok && dialsOf(&mu, &dials) > 1 })
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, second.methods(), "the wait and the cancel are not sent again")
	require.Equal(t, 1, first.count(protocol.ACPLoginWait))
}

func TestClientSecretNotInErrors(t *testing.T) {
	const secret = "fake-secret-value-7788"
	type call func(c *Client) error
	calls := map[string]call{
		"api key": func(c *Client) error {
			_, err := c.SaveAPIKey(context.Background(), "k1", "openai", secret)
			return err
		},
		"code": func(c *Client) error { return c.SendLoginCode(context.Background(), "L1", secret) },
	}
	for name, run := range calls {
		method := protocol.ACPProvidersAPIKey
		if name == "code" {
			method = protocol.ACPLoginInput
		}
		t.Run(name+"/closed link", func(t *testing.T) {
			sc := newScript(nil)
			c := openScripted(t, sc, tui.ModeLeader, "leader-a")
			defer func() { _ = c.Close() }()
			sc.link.onSend = func(raw json.RawMessage) error {
				if strings.Contains(string(raw), method) {
					return fmt.Errorf("write failed for frame %s", raw)
				}
				return nil
			}
			err := run(c)
			require.Error(t, err)
			require.NotContains(t, err.Error(), secret)
		})
		t.Run(name+"/refused", func(t *testing.T) {
			sc := newScript(map[string]func(sent) (any, string, bool){
				method: func(sent) (any, string, bool) { return nil, "refused " + secret, false },
			})
			c := openScripted(t, sc, tui.ModeLeader, "leader-a")
			defer func() { _ = c.Close() }()
			err := run(c)
			var re *tui.RequestError
			require.ErrorAs(t, err, &re)
			require.NotContains(t, err.Error(), secret)
		})
		t.Run(name+"/bad answer", func(t *testing.T) {
			sc := newScript(map[string]func(sent) (any, string, bool){
				method: fixed(map[string]any{"status": []string{secret}}),
			})
			c := openScripted(t, sc, tui.ModeLeader, "leader-a")
			defer func() { _ = c.Close() }()
			err := run(c)
			if err != nil {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestClientModelsAsksUsableOnly(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPModels: fixed(protocol.ACPModelsResult{Current: "faux/faux-1@faux", UsableOnly: true, Default: "faux/faux-1@faux"}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	c.mu.Lock()
	c.sess = sessionState{has: true, id: "s"}
	c.mu.Unlock()

	res, err := c.Models(context.Background())
	require.NoError(t, err)
	require.True(t, res.UsableOnly, "the echo reaches the UI")
	require.Equal(t, "faux/faux-1@faux", res.Default)
	req := decode[protocol.ACPModelsRequest](t, sc.last(protocol.ACPModels).Params)
	require.Equal(t, protocol.ACPModelsRequest{SessionID: "s", UsableOnly: true}, req)
}

func TestClientPassesDefaultSavedAndFallback(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPSetModel: fixed(protocol.ACPSetModelResult{ModelID: "m1", DefaultSaved: true}),
		protocol.ACPState: fixed(protocol.ACPStateResult{ModelID: "m2",
			ModelFallback: &protocol.ACPModelFallback{SavedDefault: "m1", Reason: protocol.ACPFallbackNoCredential}}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	c.mu.Lock()
	c.sess = sessionState{has: true, id: "s"}
	c.mu.Unlock()
	ctx := context.Background()

	res, err := c.SetModel(ctx, "m1", true)
	require.NoError(t, err)
	require.True(t, res.DefaultSaved)
	require.True(t, decode[protocol.ACPModelRequest](t, sc.last(protocol.ACPSetModel).Params).SaveDefault)

	_, err = c.SetModel(ctx, "m1", false)
	require.NoError(t, err)
	require.False(t, decode[protocol.ACPModelRequest](t, sc.last(protocol.ACPSetModel).Params).SaveDefault)

	st, err := c.State(ctx)
	require.NoError(t, err)
	require.NotNil(t, st.ModelFallback)
	require.Equal(t, protocol.ACPFallbackNoCredential, st.ModelFallback.Reason)
	require.Equal(t, "m1", st.ModelFallback.SavedDefault)
}

func TestClientCancelsOpenLoginOnClose(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPLoginStart:  fixed(protocol.ACPLoginStartResult{LoginID: "L1", Kind: protocol.ACPLoginKindBrowser}),
		protocol.ACPLoginCancel: fixed(protocol.ACPLoginCancelResult{Cancelled: true}),
		protocol.ACPLoginWait:   fixed(protocol.ACPLoginOutcome{LoginID: "L2", Status: protocol.ACPLoginStatusSignedIn}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	ctx := context.Background()

	_, err := c.StartLogin(ctx, "openai", "openai-chatgpt")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.Equal(t, 1, sc.count(protocol.ACPLoginCancel), "one cancel for the open login")
	require.Equal(t, "L1", decode[protocol.ACPLoginIDRequest](t, sc.last(protocol.ACPLoginCancel).Params).LoginID)
	methods := sc.methods()
	require.Equal(t, protocol.ACPLoginCancel, methods[len(methods)-1], "the cancel is the last request")
}

func TestClientEndedLoginIsNotCancelledOnClose(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPLoginStart: fixed(protocol.ACPLoginStartResult{LoginID: "L1", Kind: protocol.ACPLoginKindBrowser}),
		protocol.ACPLoginWait:  fixed(protocol.ACPLoginOutcome{LoginID: "L1", Status: protocol.ACPLoginStatusSignedIn}),
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	ctx := context.Background()
	_, err := c.StartLogin(ctx, "openai", "openai-chatgpt")
	require.NoError(t, err)
	_, err = c.WaitLogin(ctx, "L1")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.Zero(t, sc.count(protocol.ACPLoginCancel))
}

func TestClientCancelLoginRequestError(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPLoginCancel: func(sent) (any, string, bool) { return nil, "unknown sign-in", false },
	})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	_, err := c.CancelLogin(context.Background(), "gone")
	var re *tui.RequestError
	require.True(t, errors.As(err, &re))
}
