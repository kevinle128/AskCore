# H1 faux provider and exit test report (plan steps 4.7 and 4.8)

Files added (all in `internal/providers/faux`): `faux.go` (options, provider, queue, records), `script.go` (steps, modifiers, blocks), `stream.go` (call path, planning, producer), `usage.go` (transcript serialization, estimate, cache), and tests `helpers_test.go`, `faux_test.go`, `usage_test.go`, `exit_test.go`. No other file was edited.

## Verification (all pass)

- `go test -race -count=1 ./internal/providers/faux/...` and `-count=10`: ok.
- `go test -race -count=1 ./pkg/protocol/... ./internal/providers/...`: ok.
- `go vet ./internal/providers/...`: clean.
- `golangci-lint run ./pkg/protocol/... ./internal/providers/...`: 0 issues.
- `TestMain` runs `goleak.VerifyTestMain`. No real sleeps: a manual fake clock drives all pacing tests.

## API

```go
func New(opts ...Option) (*Provider, error)
WithModels(...ModelDef), WithAPI, WithProvider, WithChunk(min,max), WithSeed(int64),
WithTokensPerSecond(float64), WithClock(Clock), WithBuffer(int)
type Clock interface { Now() time.Time; After(time.Duration) <-chan time.Time }
type ModelDef struct { ID, Name string; Reasoning bool; Input []string; ContextWindow, MaxTokens int }
(*Provider) API, Stream, Set, Append, Pending, Calls, Requests() []Record, Model(id) (providers.Model, bool)
type Record struct { Call int; Model providers.Model; Options providers.StreamOptions; Transcript providers.TranscriptRequest }
type Call struct { Number int; Model; Options; Request }   // what a Func factory sees
type Factory func(ctx context.Context, c Call) (Step, error)
Reply(blocks...), Say(text), Func(Factory), Fail(msg), Raw(events ...protocol.AssistantMessageEvent)
Step modifiers: Stop, WithUsage, Error, ResponseID, Timestamp(ms), Delay, Pace, Truncate(n)
Blocks: Text, Thinking, ToolCall(name, args, ...ToolOpt), ToolCallRaw(name, id, raw), ID(id)
```

Names added or changed against the brief: `Record` and `Call` types, `Factory` type, `Clock` interface, `ModelDef`, `API()` (needed by `providers.Provider`), and `Fail(msg)` is `Reply().Stop(error).Error(msg)` (start event, then the terminal error, like Pi's scripted error reply).

## Behavior notes and deviations

1. Call path: under one lock the call number, the immutable request record, the step, the cache estimate, ids and chunk draws are all taken in call order, before `providers.NewStream`. This includes exhausted calls.
2. Unknown model (not in the provider): call count and request record are taken, but the queue is NOT consumed, because a spent script step on a caller bug would hide the real error. The stream is a setup error `unknown faux model: <id>`. The plan says to take the step for exhausted calls only; this is my reading for the unknown-model case. Easy to flip.
3. Setup errors (exhausted, factory error or panic, bad tool arguments, unknown model) call `SetUsage(estimate)` then `Fail` before `Start`, so there is exactly one error event.
4. Tool-argument marshal errors and non-object arguments are kept in the block and fail the stream as a setup error. `ToolCall` encodes without HTML escaping, so the text equals Pi's `JSON.stringify`.
5. A `Func` step's modifiers are defaults for the step the factory returns (inner values win). A factory may not return a `Func` step or set `Timestamp` (the seed identity is fixed before the factory runs); both are setup errors with a clear message, not silent drops. A Func step's chunk draws and generated ids happen when the factory returns, under the lock.
6. Seed usage is the full estimate for non-Func steps (output known) and prompt-only for Func steps; `SetUsage` before `Start` makes every item and the result carry the final value either way. `WithUsage` replaces the estimate but the cache is still primed.
7. `Truncate(n)` with n equal to the total number of emitting calls also gives no terminal event (it returns right after the n-th event); only n larger than the total completes normally.
8. `Raw` honours Delay, Truncate and Timestamp. `WithUsage` (or the estimate) sets the usage that raw items carry; `ResponseID` reaches the result only when the raw events have no terminal event; Stop and Error do not apply. A raw terminal event settles with its own message.
9. `WithBuffer(n)` raises values below 32 to 32 (default 64).
10. Abort: cancel is checked before the start event, before each block, and in every chunk wait (clock wait or immediate). After a delta only the stream state is checked; the next assembler call observes a cancel anyway.
11. Messages: only `Pending` uses `Fail(error, "Faux response ended without a stop reason", nil)`. Scripted error and aborted use `errors.New(msg)` and never wrap `context.Canceled`.
12. A scripted `Stop(StopError)` with no `.Error(msg)` uses the default message `Faux response failed` (a gap-fill; Pi has no such case).
13. Lint: the exhausted-queue error uses the fixed Pi text (capitalized) with one `//nolint:staticcheck` and a reason.

## Tests

`faux_test.go`: option validation (9 cases), model lookup and per-model data, queue exhaustion (one error event, estimate, record), Set/Append/Calls, Set during an in-flight reply, Func call state (number, model, options, request, ctx) and immutable records, Func failures (error, panic, nil factory, nested Func, inner Timestamp), Func modifier defaults and explicit timestamp 0, unknown model keeps queue, event order with two tools and generated ids, id counter per instance and explicit ids, default stop reasons (stop, toolUse, empty, length, deferred), scripted error/aborted/pending/Fail, identity rewrite and unchanged scripts, empty blocks, metadata and timestamps, seeded chunks on rune boundaries (emoji), chunks independent of goroutine scheduling, `ToolCallRaw` exact bytes and repair, authoritative tool end (key order, HTML), non-object arguments, abort before start, abort in a chunk wait for text, thinking and tool (no `*_end`), abort between blocks and in the step delay, Pace override, `Raw` (valid, invalid, no terminal, after terminal), 24 concurrent calls with instance isolation, buffer floor with no reader.

`usage_test.go`: transcript serialization (system sections, tool changes, images, assistant tool call, tool result, pointer messages), rune counting, ASCII and emoji estimate, tool output counting, cache common prefix (first, grow, shrink, diverge), separate sessions, disabled cache (no session, `none` neither reads nor updates), `WithUsage` exact and cache-neutral and copied, setup error usage.

`exit_test.go` (plan 4.8): exact sequence of plan 3.4 (event order and payloads, result equals normalized script, done message equal); projection to agent events (message_start with the seed, twelve message_update with captured usage, message_end), JSONL write, read, decode equal, `Builder.ApplyAgentEvent` rebuild equals the script and `Result`, builder state before `message_end` already holds content, identity and usage; no start/done/error inside any `message_update`; setup-error projection (start and end only); `Truncate` at 0, 1, 3, 7, 9, 11 and a long tool argument cut after two deltas: n+1 items, `ErrStreamIncomplete`, preserved completed and open blocks, identity, usage, `Result` before any read does not block, and the cut stream also round-trips through JSONL.

## Defects in other packages

None found. One note: `protocol.CloneMessage` turns pointer messages into values (fine; the faux records values) and would panic on a typed nil pointer message; no caller here passes one.

Status: DONE
Summary: The faux provider (steps, modifiers, blocks, usage and cache estimate, pacing, truncation, raw events) and the exit test are in `internal/providers/faux`; race (including -count=10), vet and lint are green.
Concerns: Unknown-model calls do not consume a queued step (point 2). `Func` rejects an inner `Timestamp` (point 5). Both are small policy choices the orchestrator can flip.
