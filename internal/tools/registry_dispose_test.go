package tools_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func TestRegisterReturnsDisposerThatUnregisters(t *testing.T) {
	r := &tools.Registry{}
	dispose, err := r.Add(stub("search", `{"type":"object"}`), tools.SourceInfo{Kind: tools.SourceBuiltin})
	require.NoError(t, err)
	require.NoError(t, r.Register(stub("keep", `{"type":"object"}`), tools.SourceInfo{}))
	before := r.Snapshot()

	dispose()

	_, _, ok := r.Lookup("search")
	assert.False(t, ok, "the next snapshot has no such tool")
	_, _, ok = r.Lookup("keep")
	assert.True(t, ok, "other tools stay")
	_, _, ok = before.Lookup("search")
	assert.True(t, ok, "a snapshot taken before keeps the tool")

	dispose() // a second call does nothing
	_, _, ok = r.Lookup("keep")
	assert.True(t, ok)
}

func TestDisposerRemovesOnlyTheToolItRegistered(t *testing.T) {
	r := &tools.Registry{}
	dispose, err := r.Add(stub("search", `{"type":"object"}`), tools.SourceInfo{})
	require.NoError(t, err)
	require.True(t, r.Unregister("search"))
	_, err = r.Add(stub("search", `{"type":"object"}`), tools.SourceInfo{Name: "replacement"})
	require.NoError(t, err)

	dispose()

	_, src, ok := r.Lookup("search")
	require.True(t, ok, "a disposer never removes a tool that another registration put under the same name")
	assert.Equal(t, "replacement", src.Name)
}

func TestAddFailureReturnsNoDisposer(t *testing.T) {
	r := &tools.Registry{}
	dispose, err := r.Add(stub("bad", `{"type":"nope"}`), tools.SourceInfo{})
	require.Error(t, err)
	assert.Nil(t, dispose)
}

func TestConcurrentAddDisposeAndUnregister(t *testing.T) {
	r := &tools.Registry{}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("tool%d", g%3)
			for range 200 {
				dispose, err := r.Add(stub(name, `{"type":"object"}`), tools.SourceInfo{})
				if err == nil {
					go dispose()
					dispose()
				}
				r.Unregister(name)
				_ = r.Snapshot().Decls()
			}
		}()
	}
	wg.Wait()
	for g := range 3 {
		r.Unregister(fmt.Sprintf("tool%d", g))
	}
	assert.Empty(t, r.Decls(), "every registration is gone, and no operation corrupted the registry")
}
