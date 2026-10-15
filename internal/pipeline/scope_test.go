package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type beforeNext = Next[ToolCallInfo, *BeforeToolCallResult]

// marker is a BeforeTool handler that records its name and calls next.
func marker(ran *[]string, name string) BeforeToolHandler {
	return func(ctx context.Context, info ToolCallInfo, next beforeNext) (*BeforeToolCallResult, error) {
		*ran = append(*ran, name)
		return next(ctx, info)
	}
}

func dispatchBefore(t *testing.T, r *Registry) {
	t.Helper()
	_, err := r.BeforeTool(context.Background(), ToolCallInfo{})
	require.NoError(t, err)
}

func TestDisposedOwnerHandlersDoNotRun(t *testing.T) {
	var ran []string
	r := NewRegistry()
	plugin, other := NewOwner(), NewOwner()
	r.OnBeforeTool(marker(&ran, "plugin-before"), WithOwner(plugin))
	r.OnBeforeTool(marker(&ran, "core"))
	r.OnCompleteStep(func(context.Context, Turn) (TurnDecision, error) {
		ran = append(ran, "plugin-complete")
		return Proceed, nil
	}, WithOwner(plugin))
	r.OnBeforeTool(marker(&ran, "other"), WithOwner(other))

	dispatchBefore(t, r)
	assert.Equal(t, []string{"plugin-before", "core", "other"}, ran)

	plugin.Dispose()
	plugin.Dispose() // a second call does nothing

	ran = nil
	dispatchBefore(t, r)
	_, err := r.CompleteStep(context.Background(), Turn{})
	require.NoError(t, err)
	assert.Equal(t, []string{"core", "other"}, ran, "a disposed owner loses its handlers at every point, and others stay")
}

func TestHandlerRegisteredWithDisposedOwnerNeverRuns(t *testing.T) {
	var ran []string
	r := NewRegistry()
	owner := NewOwner()
	owner.Dispose()
	r.OnBeforeTool(marker(&ran, "late"), WithOwner(owner))
	dispatchBefore(t, r)
	assert.Empty(t, ran)
}

func TestRegistrationDisposerRemovesOnlyItsHandler(t *testing.T) {
	var ran []string
	r := NewRegistry()
	r.OnBeforeTool(marker(&ran, "a"))
	dispose := r.OnBeforeTool(marker(&ran, "b"))
	r.OnBeforeTool(marker(&ran, "c"))

	dispose()
	dispose()
	dispatchBefore(t, r)
	assert.Equal(t, []string{"a", "c"}, ran)
}

func TestInFlightChainKeepsDisposedHandler(t *testing.T) {
	var ran []string
	r := NewRegistry()
	owner := NewOwner()
	r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next beforeNext) (*BeforeToolCallResult, error) {
		ran = append(ran, "first")
		owner.Dispose() // disposed while the chain runs
		return next(ctx, info)
	})
	r.OnBeforeTool(marker(&ran, "owned"), WithOwner(owner))
	r.OnBeforeTool(marker(&ran, "last"))

	dispatchBefore(t, r)
	assert.Equal(t, []string{"first", "owned", "last"}, ran, "the chain in flight keeps its snapshot")

	ran = nil
	dispatchBefore(t, r)
	assert.Equal(t, []string{"first", "last"}, ran, "the next dispatch has no handler of the disposed owner")
}

func TestScopedRegistryRunsParentsFirstAndReadsThemAtDispatch(t *testing.T) {
	var ran []string
	app := NewRegistry()
	app.OnBeforeTool(marker(&ran, "app1"))
	agent := NewRegistry()
	agent.OnBeforeTool(marker(&ran, "agent"))

	scoped := Scoped(app, agent)
	scoped.OnBeforeTool(marker(&ran, "own"))
	dispatchBefore(t, scoped)
	assert.Equal(t, []string{"app1", "agent", "own"}, ran, "parents in the given order, then the handlers of the registry itself")

	owner := NewOwner()
	app.OnBeforeTool(marker(&ran, "app2"), WithOwner(owner))
	ran = nil
	dispatchBefore(t, scoped)
	assert.Equal(t, []string{"app1", "app2", "agent", "own"}, ran, "a parent handler registered later is seen")

	owner.Dispose()
	ran = nil
	dispatchBefore(t, scoped)
	assert.Equal(t, []string{"app1", "agent", "own"}, ran, "a parent handler that was disposed is gone")

	ran = nil
	dispatchBefore(t, agent)
	assert.Equal(t, []string{"agent"}, ran, "a parent does not run the handlers of its children")
}

func TestScopedRegistryIgnoresNilParent(t *testing.T) {
	var ran []string
	app := NewRegistry()
	app.OnBeforeTool(marker(&ran, "app"))
	dispatchBefore(t, Scoped(nil, app, nil))
	assert.Equal(t, []string{"app"}, ran)
}

func TestScopedRegistryDecisionPointsRunParentsFirst(t *testing.T) {
	var ran []string
	decider := func(name string, d TurnDecision) CompleteStepHandler {
		return func(context.Context, Turn) (TurnDecision, error) {
			ran = append(ran, name)
			return d, nil
		}
	}
	app := NewRegistry()
	app.OnCompleteStep(decider("app", Continue))
	agent := NewRegistry()
	agent.OnCompleteStep(decider("agent", End))
	got, err := Scoped(app, agent).CompleteStep(context.Background(), Turn{})
	require.NoError(t, err)
	assert.Equal(t, End, got)
	assert.Equal(t, []string{"app", "agent"}, ran)
}

func TestOwnerDisposedWhileRegisteringNeverRunsHandler(t *testing.T) {
	for range 200 {
		var runs atomic.Int32
		r := NewRegistry()
		owner := NewOwner()
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = r.BeforeTool(context.Background(), ToolCallInfo{})
				}
			}
		}()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.OnBeforeTool(func(ctx context.Context, info ToolCallInfo, next beforeNext) (*BeforeToolCallResult, error) {
				runs.Add(1)
				return next(ctx, info)
			}, WithOwner(owner))
		}()
		go func() { defer wg.Done(); owner.Dispose() }()
		wg.Wait()
		// After both finished the owner is disposed, so no dispatch may reach
		// the handler any more, whichever call won the race.
		close(stop)
		<-done
		before := runs.Load()
		_, err := r.BeforeTool(context.Background(), ToolCallInfo{})
		require.NoError(t, err)
		require.Equal(t, before, runs.Load(), "a handler of a disposed owner must not run")
		require.Empty(t, r.beforeTool.snapshot())
	}
}

func TestHandlerOfAlreadyDisposedOwnerIsNeverAdded(t *testing.T) {
	r := NewRegistry()
	owner := NewOwner()
	owner.Dispose()
	dispose := r.OnBeforeTool(marker(new([]string), "x"), WithOwner(owner))
	require.NotNil(t, dispose)
	dispose()
	assert.Empty(t, r.beforeTool.snapshot(), "nothing was added")
}

func TestOwnerForgetsRegistrationRemovedByItsDisposer(t *testing.T) {
	r := NewRegistry()
	owner := NewOwner()
	for range 100 {
		dispose := r.OnBeforeTool(marker(new([]string), "x"), WithOwner(owner))
		dispose()
	}
	owner.mu.Lock()
	n := len(owner.disposers)
	owner.mu.Unlock()
	assert.Zero(t, n, "the owner keeps no record of a registration that is gone")
}
