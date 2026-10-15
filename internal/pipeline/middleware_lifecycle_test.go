package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestHandlerFailureCancelsAndJoinsAcceptedNext(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			cancelled := make(chan bool, 1)
			nextDone := make(chan struct{})
			finished := make(chan any, 1)
			r := NewRegistry()
			r.OnExecuteTool(func(_ context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				go func() {
					defer close(nextDone)
					_, _ = next(context.Background(), in)
				}()
				<-started
				if failure == "panic" {
					panic("handler panic")
				}
				return protocol.ToolExecutionResult{}, errHandler
			})
			go func() {
				defer func() {
					if panicked := recover(); panicked != nil {
						finished <- panicked
					}
				}()
				_, err := r.ExecuteTool(context.Background(), ExecuteToolInput{}, func(ctx context.Context, _ ExecuteToolInput) (protocol.ToolExecutionResult, error) {
					close(started)
					select {
					case <-ctx.Done():
						cancelled <- true
						<-release
					case <-release:
						cancelled <- false
					}
					return toolResult("body"), nil
				})
				finished <- err
			}()
			released := false
			select {
			case wasCancelled := <-cancelled:
				assert.True(t, wasCancelled)
			case <-time.After(100 * time.Millisecond):
				t.Error("handler failure did not cancel its accepted next context")
				close(release)
				released = true
				<-cancelled
			}
			if !released {
				select {
				case <-finished:
					t.Error("handler failure returned before its cancelled next finished")
					close(release)
					<-nextDone
					return
				case <-time.After(30 * time.Millisecond):
				}
				close(release)
			}
			<-nextDone
			select {
			case result := <-finished:
				if failure == "panic" {
					assert.Equal(t, "handler panic", result)
				} else {
					err, ok := result.(error)
					assert.True(t, ok)
					assert.True(t, errors.Is(err, errHandler))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("handler failure did not finish after its next returned")
			}
		})
	}
}

func TestAcceptedNextFinishesBeforeHandlerInvocationCloses(t *testing.T) {
	for _, tc := range []struct {
		name         string
		handlerPanic bool
		nextPanic    bool
		beforeBody   bool
	}{
		{name: "cached handler return"},
		{name: "handler panic", handlerPanic: true},
		{name: "next panic", nextPanic: true},
		{name: "both panic", handlerPanic: true, nextPanic: true},
		{name: "accepted before terminal entry", beforeBody: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			returning := make(chan struct{})
			nextDone := make(chan any, 1)
			closed := make(chan any, 1)
			var retained Next[ExecuteToolInput, protocol.ToolExecutionResult]
			r := NewRegistry()
			r.OnExecuteTool(func(ctx context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				retained = next
				go func() {
					defer func() { nextDone <- recover() }()
					_, _ = next(ctx, in)
				}()
				<-entered
				close(returning)
				if tc.handlerPanic {
					panic("handler panic")
				}
				return toolResult("cached"), nil
			})
			if tc.beforeBody {
				r.OnExecuteTool(func(ctx context.Context, in ExecuteToolInput, next Next[ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
					close(entered)
					<-release
					return next(ctx, in)
				})
			}
			go func() {
				defer func() { closed <- recover() }()
				out, err := r.ExecuteTool(context.Background(), ExecuteToolInput{}, func(ctx context.Context, _ ExecuteToolInput) (protocol.ToolExecutionResult, error) {
					if !tc.beforeBody {
						close(entered)
						if tc.handlerPanic {
							<-release
						} else {
							select {
							case <-release:
							case <-ctx.Done():
								t.Error("cached handler success cancelled its accepted next")
							}
						}
					}
					if tc.nextPanic {
						panic("next panic")
					}
					return toolResult("body"), nil
				})
				assert.NoError(t, err)
				assert.Equal(t, toolResult("cached"), out, "outer handler result still wins")
			}()
			select {
			case <-returning:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("handler did not accept next")
			}
			select {
			case <-closed:
				t.Error("handler invocation closed while its accepted next was blocked")
				close(release)
				<-nextDone
				return
			case <-time.After(30 * time.Millisecond):
			}
			_, err := retained(context.Background(), ExecuteToolInput{})
			assert.ErrorIs(t, err, ErrNextReused, "a second claim stays rejected during the join")
			close(release)
			select {
			case panicked := <-closed:
				if tc.handlerPanic {
					assert.Equal(t, "handler panic", panicked, "dispatcher must preserve handler panic")
				} else {
					assert.Nil(t, panicked)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("handler invocation did not close after next finished")
			}
			select {
			case panicked := <-nextDone:
				if tc.nextPanic {
					assert.Equal(t, "next panic", panicked, "panic stays with the goroutine that called next")
				} else {
					assert.Nil(t, panicked)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("accepted next did not finish")
			}
			_, err = retained(context.Background(), ExecuteToolInput{})
			assert.ErrorIs(t, err, ErrNextReused)
			require.Len(t, closed, 0)
		})
	}
}
