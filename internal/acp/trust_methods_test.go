package acp

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// fakeTrustSetup is a TrustSetup that records its calls and answers from its fields.
type fakeTrustSetup struct {
	mu     sync.Mutex
	states []protocol.ACPTrustStateRequest
	sets   []protocol.ACPTrustSetRequest
	err    error
}

func (f *fakeTrustSetup) State(_ context.Context, req protocol.ACPTrustStateRequest) (protocol.ACPTrustStateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, req)
	if f.err != nil {
		return protocol.ACPTrustStateResult{}, f.err
	}
	return protocol.ACPTrustStateResult{Root: req.Cwd, Source: "prompt", NeedsDecision: true, Gated: []string{".ask/settings.json"}}, nil
}

func (f *fakeTrustSetup) Set(_ context.Context, req protocol.ACPTrustSetRequest) (protocol.ACPTrustStateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = append(f.sets, req)
	if f.err != nil {
		return protocol.ACPTrustStateResult{}, f.err
	}
	return protocol.ACPTrustStateResult{Root: req.Cwd, Trusted: req.Decision == protocol.ACPTrustDecisionTrust, Source: "saved"}, nil
}

func (f *fakeTrustSetup) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.states), len(f.sets)
}

func TestTrustMethodsUnsupportedWithoutSetup(t *testing.T) {
	for _, requireRoute := range []bool{false, true} {
		cfg := testConfig(says("x"), nil)
		cfg.RequireRoute = requireRoute
		p := newAdapterPeer(t, cfg, nil)
		p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
		cwd := t.TempDir()
		f := p.call(protocol.ACPTrustState, withRoute(map[string]any{"cwd": cwd}, route("c1", 0, false, "")))
		require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t))
		f = p.call(protocol.ACPTrustSet, withRoute(map[string]any{"cwd": cwd, "target": "folder", "decision": "trust"}, route("c1", 0, false, "")))
		require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t))
	}
}

func TestTrustMethodsNeedNoSession(t *testing.T) {
	setup := &fakeTrustSetup{}
	cfg := testConfig(says("x"), nil)
	cfg.RequireRoute = true
	cfg.TrustSetup = setup
	p := newAdapterPeer(t, cfg, nil)

	cwd := t.TempDir()
	f := p.call(protocol.ACPTrustState, withRoute(map[string]any{"cwd": cwd}, route("c1", 0, false, "")))
	require.Equal(t, protocol.ACPErrNotInitialized, f.errKind(t))
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)

	// The router forwards a trust call with the client id and no session.
	var st protocol.ACPTrustStateResult
	p.ok(protocol.ACPTrustState, withRoute(map[string]any{"cwd": cwd}, route("c1", 0, false, "")), &st)
	require.Equal(t, cwd, st.Root)
	require.True(t, st.NeedsDecision)
	p.ok(protocol.ACPTrustSet, withRoute(map[string]any{"cwd": cwd, "target": "parent", "decision": "trust"}, route("c1", 0, false, "")), &st)
	require.True(t, st.Trusted)
	require.Equal(t, []protocol.ACPTrustSetRequest{{Cwd: cwd, Target: protocol.ACPTrustTargetParent, Decision: protocol.ACPTrustDecisionTrust}}, setup.sets)

	// A route that is present must be valid, and the leader link needs one.
	f = p.call(protocol.ACPTrustState, map[string]any{"cwd": cwd, "_meta": map[string]any{protocol.ACPRouteMetaKey: map[string]any{"clientId": ""}}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	f = p.call(protocol.ACPTrustState, map[string]any{"cwd": cwd})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))

	// Parameters are checked before the setup sees them.
	for _, params := range []map[string]any{
		{"cwd": "relative"},
		{"cwd": ""},
	} {
		f = p.call(protocol.ACPTrustState, withRoute(params, route("c1", 0, false, "")))
		require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "%v", params)
	}
	for _, params := range []map[string]any{
		{"cwd": "relative", "target": "folder", "decision": "trust"},
		{"cwd": cwd, "target": "sibling", "decision": "trust"},
		{"cwd": cwd, "target": "folder", "decision": "maybe"},
		{"cwd": cwd, "target": "folder"},
	} {
		f = p.call(protocol.ACPTrustSet, withRoute(params, route("c1", 0, false, "")))
		require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "%v", params)
	}
	states, sets := setup.counts()
	require.Equal(t, 1, states)
	require.Equal(t, 1, sets)

	// A trust store that cannot be changed is invalid_state with its fixed warning.
	setup.mu.Lock()
	setup.err = &TrustStoreError{Warning: "trust store is corrupt or unsafe: /h/trust.json"}
	setup.mu.Unlock()
	f = p.call(protocol.ACPTrustSet, withRoute(map[string]any{"cwd": cwd, "target": "folder", "decision": "trust"}, route("c1", 0, false, "")))
	require.Equal(t, protocol.ACPErrInvalidState, f.errKind(t))
	require.Contains(t, f.Error.Message, "/h/trust.json")

	// Any other error text stays off the wire.
	setup.mu.Lock()
	setup.err = errors.New("open /secret/path: denied")
	setup.mu.Unlock()
	f = p.call(protocol.ACPTrustState, withRoute(map[string]any{"cwd": cwd}, route("c1", 0, false, "")))
	require.Equal(t, protocol.ACPErrInternal, f.errKind(t))
	require.NotContains(t, f.Error.Message, "secret")
}

// The in-process link has no route; the trust methods still work there.
func TestTrustMethodsOnLinkWithoutRoute(t *testing.T) {
	setup := &fakeTrustSetup{}
	cfg := testConfig(says("x"), nil)
	cfg.TrustSetup = setup
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	var st protocol.ACPTrustStateResult
	p.ok(protocol.ACPTrustState, map[string]any{"cwd": t.TempDir()}, &st)
	require.Equal(t, "prompt", st.Source)
}

func TestNewSessionPassesTrustMeta(t *testing.T) {
	var mu sync.Mutex
	var seen []SessionRequest
	cfg := testConfig(says("x"), nil)
	inner := cfg.Factory
	cfg.Factory = func(ctx context.Context, req SessionRequest) (*agent.Agent, error) {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		return inner(ctx, req)
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	cwd := t.TempDir()
	for _, word := range []string{"approve", "deny"} {
		p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": map[string]any{protocol.ACPTrustMetaKey: word}}, nil)
	}
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, nil)
	for _, bad := range []any{"yes", true, "", map[string]any{}} {
		f := p.call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": map[string]any{protocol.ACPTrustMetaKey: bad}})
		require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "%v", bad)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 3, "a bad override never reaches the factory")
	require.Equal(t, protocol.ACPTrustApprove, seen[0].Trust)
	require.Equal(t, protocol.ACPTrustDeny, seen[1].Trust)
	require.Equal(t, protocol.ACPTrustOverride(""), seen[2].Trust)
	for _, req := range seen {
		require.Equal(t, cwd, req.Cwd)
		require.NotEmpty(t, req.ID)
	}

	// The router keeps the override next to its route context.
	rp := routedPeer(t, says("x"))
	params := withRoute(map[string]any{"cwd": cwd, "mcpServers": []any{}}, route("c1", 0, false, ""))
	params["_meta"].(map[string]any)[protocol.ACPTrustMetaKey] = "nope"
	f := rp.call("session/new", params)
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestStateReportsSessionTrust(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	cfg := testConfig(says("x"), nil)
	cfg.StartTrust = func(id string) *SessionTrust {
		mu.Lock()
		calls[id]++
		mu.Unlock()
		return &SessionTrust{
			Trust:        protocol.ACPSessionTrust{Trusted: true, Source: "flag"},
			Diagnostics:  []protocol.ACPDiagnostic{{Layer: "project", Path: "/p/.ask/settings.json", Key: "env", Text: "a project file cannot set this key; it is ignored"}},
			ThinkingFold: "collapsed",
		}
	}
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	var st protocol.ACPStateResult
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, &st)
	require.Equal(t, &protocol.ACPSessionTrust{Trusted: true, Source: "flag"}, st.Trust)
	require.Len(t, st.Diagnostics, 1)
	require.Equal(t, "env", st.Diagnostics[0].Key)
	require.Equal(t, "collapsed", st.ThinkingFold)
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, &st)
	mu.Lock()
	require.Equal(t, 1, calls[sid], "the start values are read once")
	mu.Unlock()

	// A host without trust composition reports none.
	plain := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	other := plain.start()
	var none protocol.ACPStateResult
	plain.ok(protocol.ACPState, map[string]any{"sessionId": other}, &none)
	require.Nil(t, none.Trust)
	require.Empty(t, none.Diagnostics)
	require.Empty(t, none.ThinkingFold)
}

func TestSkillsReportsTheSkillsOfTheSessionStart(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	cfg := testConfig(says("x"), nil)
	cfg.StartSkills = func(id string) []protocol.ACPSkill {
		mu.Lock()
		calls[id]++
		mu.Unlock()
		return []protocol.ACPSkill{
			{Name: "demo", Description: "A demo", Path: "/p/.ask/skills/demo/SKILL.md", Scope: protocol.ACPSkillProject, ModelInvocable: true},
			{Name: "quiet", Description: "Manual", Path: "/h/skills/quiet/SKILL.md", Scope: protocol.ACPSkillUser},
		}
	}
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	var res protocol.ACPSkillsResult
	p.ok(protocol.ACPSkills, map[string]any{"sessionId": sid}, &res)
	require.Equal(t, []protocol.ACPSkill{
		{Name: "demo", Description: "A demo", Path: "/p/.ask/skills/demo/SKILL.md", Scope: protocol.ACPSkillProject, ModelInvocable: true},
		{Name: "quiet", Description: "Manual", Path: "/h/skills/quiet/SKILL.md", Scope: protocol.ACPSkillUser},
	}, res.Skills)
	p.ok(protocol.ACPSkills, map[string]any{"sessionId": sid}, &res)
	require.Len(t, res.Skills, 2)
	mu.Lock()
	require.Equal(t, 1, calls[sid], "the start list is read once")
	mu.Unlock()

	f := p.call(protocol.ACPSkills, map[string]any{"sessionId": "none"})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))

	plain := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	raw := plain.call(protocol.ACPSkills, map[string]any{"sessionId": plain.start()})
	require.JSONEq(t, `{"skills":[]}`, string(raw.Result), "a host without skills answers an empty list, not null")
}

func TestModelsDefaultUsesSessionCwd(t *testing.T) {
	cfg := testConfig(says("x"), nil)
	cfg.StartTrust = func(string) *SessionTrust {
		return &SessionTrust{Trust: protocol.ACPSessionTrust{Trusted: true, Source: "no_gated_files"}}
	}
	cfg.DefaultModel = func(_ context.Context, scope SettingsScope) (string, error) {
		if !scope.Trusted {
			return "", errors.New("the session trust did not reach the callback")
		}
		return "p/" + filepath.Base(scope.Cwd) + "@api", nil
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	root := t.TempDir()
	sids := map[string]string{}
	for _, name := range []string{"one", "two"} {
		var res struct {
			SessionID string `json:"sessionId"`
		}
		p.ok("session/new", map[string]any{"cwd": filepath.Join(root, name), "mcpServers": []any{}}, &res)
		sids[name] = res.SessionID
	}
	for name, sid := range sids {
		var models protocol.ACPModelsResult
		p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &models)
		require.Equal(t, "p/"+name+"@api", models.Default)
	}
}
