package acp

import (
	"context"
	"strings"
	"sync"
	"testing"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestFlagsFromMeta(t *testing.T) {
	t.Parallel()
	key := protocol.ACPSessionOptionsMetaKey
	half := strings.Repeat("x", protocol.ACPSessionPromptMaxBytes/2)

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		got, err := flagsFromMeta(nil)
		require.NoError(t, err)
		require.True(t, got.IsZero())
		got, err = flagsFromMeta(map[string]any{"other": 1})
		require.NoError(t, err)
		require.True(t, got.IsZero())
	})
	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		got, err := flagsFromMeta(map[string]any{key: map[string]any{
			"systemPrompt":       "p",
			"appendSystemPrompt": []any{"a", "b"},
			"tools":              []any{"read", "web_fetch2"},
			"noTools":            true,
			"unknown":            []any{1},
		}})
		require.NoError(t, err)
		require.Equal(t, protocol.ACPSessionOptions{SystemPrompt: "p", AppendSystemPrompt: []string{"a", "b"}, Tools: []string{"read", "web_fetch2"}, NoTools: true}, got)
	})
	t.Run("exactly at the cap", func(t *testing.T) {
		t.Parallel()
		_, err := flagsFromMeta(map[string]any{key: map[string]any{"systemPrompt": half, "appendSystemPrompt": []any{half}}})
		require.NoError(t, err)
	})
	for name, bad := range map[string]any{
		"not an object":      "yes",
		"null":               nil,
		"array":              []any{},
		"prompt not text":    map[string]any{"systemPrompt": 1},
		"append not array":   map[string]any{"appendSystemPrompt": "a"},
		"append item":        map[string]any{"appendSystemPrompt": []any{1}},
		"tools not array":    map[string]any{"tools": "read"},
		"noTools not bool":   map[string]any{"noTools": "true"},
		"oversize prompt":    map[string]any{"systemPrompt": half + half + "x"},
		"oversize in sum":    map[string]any{"systemPrompt": half, "appendSystemPrompt": []any{half, "x"}},
		"upper case tool":    map[string]any{"tools": []any{"Read"}},
		"empty tool":         map[string]any{"tools": []any{""}},
		"tool with dash":     map[string]any{"tools": []any{"a-b"}},
		"tool with newline":  map[string]any{"tools": []any{"read\n"}},
		"tool starts digit":  map[string]any{"tools": []any{"1a"}},
		"tool with space":    map[string]any{"tools": []any{"a b"}},
		"tool with slash":    map[string]any{"tools": []any{"../x"}},
		"tool is not string": map[string]any{"tools": []any{3}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := flagsFromMeta(map[string]any{key: bad})
			require.Error(t, err)
			require.Equal(t, protocol.ACPErrInvalidParams, classify(err))
		})
	}
}

func TestInvalidParamsIsSentWithItsDetail(t *testing.T) {
	t.Parallel()
	cfg := testConfig(says("x"), nil)
	cfg.Factory = func(context.Context, SessionRequest) (*agent.Agent, error) {
		return nil, InvalidParams("tool nope is unknown")
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.Contains(t, f.Error.Message, "tool nope is unknown")
}

func TestNewSessionPassesFlagsAndAcknowledgesThem(t *testing.T) {
	t.Parallel()
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
	key := protocol.ACPSessionOptionsMetaKey

	var withFlags, without struct {
		Meta map[string]any `json:"_meta"`
	}
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": map[string]any{key: map[string]any{"noTools": true}}}, &withFlags)
	require.Equal(t, map[string]any{key: true}, withFlags.Meta, "the ack is the whole result _meta")
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": map[string]any{key: map[string]any{}}}, &without)
	require.Empty(t, without.Meta, "a zero value is not acknowledged")
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &without)
	require.Empty(t, without.Meta)

	f := p.call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": map[string]any{key: map[string]any{"tools": []any{"Bad"}}}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 3, "a bad value never reaches the factory")
	require.Equal(t, protocol.ACPSessionOptions{NoTools: true}, seen[0].Flags)
	require.True(t, seen[1].Flags.IsZero())
	require.True(t, seen[2].Flags.IsZero())
}

func TestNewSessionFlagsOnRoutedLink(t *testing.T) {
	t.Parallel()
	rp := routedPeer(t, says("x"))
	cwd := t.TempDir()
	key := protocol.ACPSessionOptionsMetaKey

	params := withRoute(map[string]any{"cwd": cwd, "mcpServers": []any{}}, route("c1", 0, false, ""))
	params["_meta"].(map[string]any)[key] = map[string]any{"tools": []any{"read"}}
	var res struct {
		Meta map[string]any `json:"_meta"`
	}
	rp.ok("session/new", params, &res)
	require.Equal(t, true, res.Meta[key], "the ack sits next to the route")
	require.Contains(t, res.Meta, protocol.ACPRouteMetaKey)

	params = withRoute(map[string]any{"cwd": cwd, "mcpServers": []any{}}, route("c1", 0, false, ""))
	params["_meta"].(map[string]any)[key] = "nope"
	f := rp.call("session/new", params)
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}
