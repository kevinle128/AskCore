package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

type stubTool struct{ decl protocol.ToolDecl }

type safeStubTool struct{ stubTool }

func (safeStubTool) ConcurrencySafe(json.RawMessage) bool { return true }

func TestToolDeclCarriesOnlyNameDescriptionParameters(t *testing.T) {
	var registry tools.Registry
	tool := safeStubTool{stubTool{protocol.ToolDecl{Name: "safe", Description: "A safe tool", Parameters: json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object"}}}`)}}}
	require.NoError(t, registry.Register(tool, tools.SourceInfo{Kind: tools.SourceExtension, Name: "private-host"}))
	decls := registry.Snapshot().Decls()
	encoded, err := json.Marshal(decls)
	require.NoError(t, err)
	var projected []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &projected))
	require.Len(t, projected, 1)
	assert.ElementsMatch(t, []string{"name", "description", "parameters"}, keys(projected[0]))
	decls[0].Name = "changed"
	decls[0].Description = "changed"
	decls[0].Parameters[0] = '['
	assert.Equal(t, tool.Decl(), registry.Decls()[0])
}

func keys(values map[string]json.RawMessage) []string {
	var result []string
	for key := range values {
		result = append(result, key)
	}
	return result
}

func (s stubTool) Decl() protocol.ToolDecl { return s.decl }

func (stubTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	return protocol.ToolExecutionResult{}, nil
}

func stub(name, params string) stubTool {
	return stubTool{protocol.ToolDecl{Name: name, Parameters: json.RawMessage(params)}}
}

func TestRegistryRejectsSchemalessTool(t *testing.T) {
	var r tools.Registry
	err := r.Register(stub("bare", ""), tools.SourceInfo{Kind: tools.SourceBuiltin})
	require.EqualError(t, err, `tools: tool "bare": no parameters schema`)
	err = r.Register(stub("nulled", "null"), tools.SourceInfo{Kind: tools.SourceBuiltin})
	require.EqualError(t, err, `tools: tool "nulled": no parameters schema`)
	assert.Empty(t, r.Decls())
}

func TestRegistryRejectsDuplicateName(t *testing.T) {
	var r tools.Registry
	require.NoError(t, r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	err := r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceExtension, Name: "other"})
	require.EqualError(t, err, `tools: tool "echo" is already registered`)

	decls := r.Decls()
	require.Len(t, decls, 1)
	_, src, ok := r.Lookup("echo")
	require.True(t, ok)
	assert.Equal(t, tools.SourceInfo{Kind: tools.SourceBuiltin}, src)
}

func TestRegistryRejectsBadSchema(t *testing.T) {
	var r tools.Registry
	err := r.Register(stub("bad", `{"type":"nope"}`), tools.SourceInfo{})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), `tools: tool "bad": parameters schema: `), err.Error())

	err = r.Register(stub("remote", `{"$ref":"file:///etc/passwd"}`), tools.SourceInfo{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "external schema file:///etc/passwd is not allowed")

	_, _, ok := r.Lookup("bad")
	assert.False(t, ok)
}

func TestRegistryLookupIsCaseSensitive(t *testing.T) {
	var r tools.Registry
	require.NoError(t, r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}))

	_, _, ok := r.Lookup("Echo")
	assert.False(t, ok)
	got, _, ok := r.Lookup("echo")
	require.True(t, ok)
	assert.Equal(t, "echo", got.Decl().Name)

	_, err := r.Prepare("ECHO", json.RawMessage(`{"text":"hi"}`))
	require.EqualError(t, err, `tools: unknown tool "ECHO"`)
}

func TestRegistrySourceInfoRoundTrip(t *testing.T) {
	var r tools.Registry
	src := tools.SourceInfo{Kind: tools.SourceMCP, Name: "github", Path: "/home/u/.ask/mcp.json"}
	require.NoError(t, r.Register(stub("search", `{"type":"object"}`), src))

	_, got, ok := r.Lookup("search")
	require.True(t, ok)
	assert.Equal(t, tools.SourceInfo{Kind: "mcp", Name: "github", Path: "/home/u/.ask/mcp.json"}, got)
}

func TestRegistryDeclsKeepRegistrationOrder(t *testing.T) {
	var r tools.Registry
	for _, name := range []string{"zeta", "alpha", "mid"} {
		require.NoError(t, r.Register(stub(name, `{"type":"object"}`), tools.SourceInfo{}))
	}
	var names []string
	for _, d := range r.Decls() {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"zeta", "alpha", "mid"}, names)
}

func TestRegistryConcurrentPrepare(t *testing.T) {
	var r tools.Registry
	require.NoError(t, r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}))

	var wg sync.WaitGroup
	got := make([]string, 64)
	for i := range got {
		wg.Go(func() {
			out, err := r.Prepare("echo", json.RawMessage(`{"text":5}`))
			if err == nil {
				got[i] = string(out)
			}
		})
	}
	wg.Wait()
	for _, g := range got {
		assert.Equal(t, `{"text":"5"}`, g)
	}
}

func BenchmarkPrepare(b *testing.B) {
	var r tools.Registry
	require.NoError(b, r.Register(tools.Echo{}, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	raw := json.RawMessage(`{"text":"` + strings.Repeat("x", 189) + `"}`)
	require.Len(b, raw, 200)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Prepare("echo", raw); err != nil {
			b.Fatal(err)
		}
	}
}
