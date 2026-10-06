package tools_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const objParams = `{"type":"object"}`

func names(decls []protocol.ToolDecl) []string {
	out := make([]string, len(decls))
	for i, d := range decls {
		out[i] = d.Name
	}
	return out
}

func TestSnapshotDoesNotSeeLaterChanges(t *testing.T) {
	r := &tools.Registry{}
	require.NoError(t, r.Register(stub("a", objParams), tools.SourceInfo{}))
	require.NoError(t, r.Register(stub("b", objParams), tools.SourceInfo{}))
	snap := r.Snapshot()

	require.True(t, r.Unregister("a"))
	require.NoError(t, r.Register(stub("c", objParams), tools.SourceInfo{}))

	assert.Equal(t, []string{"a", "b"}, names(snap.Decls()))
	_, _, ok := snap.Lookup("a")
	assert.True(t, ok, "the snapshot keeps a tool unregistered after it was taken")
	_, _, ok = snap.Lookup("c")
	assert.False(t, ok, "the snapshot does not see a tool registered after it was taken")
	_, err := snap.Prepare("a", json.RawMessage(`{}`))
	assert.NoError(t, err)

	assert.Equal(t, []string{"b", "c"}, names(r.Decls()))
}

func TestUnregister(t *testing.T) {
	r := &tools.Registry{}
	require.NoError(t, r.Register(stub("a", objParams), tools.SourceInfo{}))
	require.NoError(t, r.Register(stub("b", objParams), tools.SourceInfo{}))
	require.NoError(t, r.Register(stub("c", objParams), tools.SourceInfo{}))

	assert.True(t, r.Unregister("b"))
	assert.False(t, r.Unregister("b"), "a second unregister finds nothing")
	assert.False(t, r.Unregister("missing"))
	assert.Equal(t, []string{"a", "c"}, names(r.Decls()))
	_, err := r.Prepare("b", json.RawMessage(`{}`))
	assert.ErrorContains(t, err, `unknown tool "b"`)

	// The name is free again.
	require.NoError(t, r.Register(stub("b", objParams), tools.SourceInfo{}))
	assert.Equal(t, []string{"a", "c", "b"}, names(r.Decls()))
}

func TestNilSnapshotIsEmpty(t *testing.T) {
	var r *tools.Registry
	snap := r.Snapshot()
	assert.Empty(t, snap.Decls())
	_, _, ok := snap.Lookup("a")
	assert.False(t, ok)
	_, err := snap.Prepare("a", nil)
	assert.Error(t, err)
	assert.Empty(t, (&tools.Registry{}).Snapshot().Decls())
}

func TestSnapshotDeclsAreCopies(t *testing.T) {
	r := &tools.Registry{}
	require.NoError(t, r.Register(stub("a", objParams), tools.SourceInfo{}))
	d := r.Snapshot().Decls()
	d[0].Parameters[0] = 'X'
	d[0].Name = "changed"
	assert.Equal(t, objParams, string(r.Snapshot().Decls()[0].Parameters))
	assert.Equal(t, "a", r.Snapshot().Decls()[0].Name)
}

func TestRegistryConcurrentChangeAndSnapshot(t *testing.T) {
	r := &tools.Registry{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		name := string(rune('a' + i))
		go func() {
			defer wg.Done()
			_ = r.Register(stub(name, objParams), tools.SourceInfo{})
			r.Unregister(name)
		}()
		go func() {
			defer wg.Done()
			snap := r.Snapshot()
			for _, d := range snap.Decls() {
				_, _, ok := snap.Lookup(d.Name)
				assert.True(t, ok, "a snapshot is consistent with itself")
			}
		}()
	}
	wg.Wait()
	assert.Empty(t, r.Decls())
}
