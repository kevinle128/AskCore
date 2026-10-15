# H1 stream, assembler, convert report (plan step 4.5)

Files added in `internal/providers`: `types.go`, `model.go`, `errors.go`, `stream.go`, `assembler.go`, `convert.go`, and `stream_test.go`, `assembler_test.go`, `convert_test.go`. `go.mod` now lists `go.uber.org/goleak v1.3.0` as a direct requirement (`go mod tidy`; `go.sum` needed no change). Small `pkg/protocol` fixes: `builder.go`, `stream_events.go`, `usage.go`, `content.go` and their tests.

## Verification (all pass)

- `go mod tidy`
- `go test -race -count=1 ./pkg/protocol/... ./internal/providers/...` passes; `./internal/providers` also passes with `-race -count=30`. `TestMain` runs `goleak.VerifyTestMain`.
- `go vet ./pkg/protocol/... ./internal/providers/...` clean.
- `golangci-lint run ./pkg/protocol/... ./internal/providers/...`: 0 issues.
- `go build ./...` was not used (known `internal/logs` gap).

## pkg/protocol fixes

- (a) `Builder.Apply` rejects `DoneEvent` when `Message.StopReason != Reason` and `ErrorEvent` when `Error.StopReason != Reason` (also for the setup error before start). `UnmarshalStreamEvent` enforces the same for `done` and `error`. I did NOT add the check to `MarshalStreamEvent`: it is outside the request, and the existing encode fixtures use mismatched pairs. I fixed three existing fixtures that built mismatched pairs (the golden `error` case, the builder "event after error" setup, and added mismatch cases to the bad-sequence table).
- (b) `func (u Usage) Clone() Usage` and `func CloneAssistantBlock(b AssistantBlock) AssistantBlock` are exported (the internal `clone` still exists as a thin wrapper).
- (c) `Builder.Snapshot()` before start returns `StopReason: StopPending` with an empty non-nil content slice.
- Tests: `TestSnapshotBeforeStartIsPending`, three new rows in `TestBuilderRejectsBadSequences`, `TestStreamEventDecodeRejectsReasonThatDiffersFromMessage`, `TestUsageCloneSharesNoPointer`, `TestCloneAssistantBlockSharesNothing`.

## Final exported API (from `go doc`)

```go
type Model struct {
	ID        string
	Name      string
	API       string
	Provider  string
	Reasoning bool
	Input     []string // "text", "image"
	ContextWindow int
	MaxTokens     int
}
type Request struct {
	SystemPrompt string
	Messages     []protocol.Message
	Tools        []protocol.ToolDecl
}
type TranscriptRequest struct{ Messages []protocol.Message }
type StreamOptions struct {
	SessionID      string
	CacheRetention string // "", "short", "long", "none" (constants CacheRetentionShort/Long/None)
	MaxTokens      int
	Temperature    *float64
	Reasoning      protocol.ThinkingLevel
	APIKey         string
}
type StreamFn func(ctx context.Context, m Model, req TranscriptRequest, opts StreamOptions) *Stream
type Provider interface {
	API() string
	Stream(ctx context.Context, m Model, req TranscriptRequest, opts StreamOptions) *Stream
}
type StreamItem struct {
	Event protocol.AssistantMessageEvent
	Usage protocol.Usage
}

var ErrStreamIncomplete = errors.New("stream ended without a terminal event")
var ErrStreamClosed     = errors.New("stream is already settled") // returned only by Assembler.Emit

func NewStream(ctx context.Context, buffer int, seed protocol.AssistantMessage, body func(a *Assembler)) *Stream
func (s *Stream) Events() <-chan StreamItem
func (s *Stream) Result(ctx context.Context) (protocol.AssistantMessage, error)

func ConvertToLLM(msgs []protocol.Message) []protocol.Message
func NormalizeRequest(r Request) TranscriptRequest

type Metadata struct {
	ResponseID, ResponseModel, ProviderThinkingLevel, RawStopReason *string
	ThinkingLevel *protocol.ThinkingLevel
	Diagnostics   []protocol.Diagnostic // appended on each SetMetadata
	EndTurn       *bool
}

func (a *Assembler) Context() context.Context
func (a *Assembler) Settled() bool
func (a *Assembler) Start()
func (a *Assembler) TextStart(initial string) int
func (a *Assembler) TextDelta(i int, s string)
func (a *Assembler) TextEnd(i int, content string, sig *string)
func (a *Assembler) ThinkingStart(initial string, sig *string, redacted *bool) int
func (a *Assembler) ThinkingDelta(i int, s string)
func (a *Assembler) ThinkingEnd(i int, content string, sig *string, redacted *bool)
func (a *Assembler) ToolStart(id, name string, initialArgs json.RawMessage, thoughtSig, ns *string) int
func (a *Assembler) ToolDelta(i int, raw string)
func (a *Assembler) ToolEnd(i int, final *protocol.ToolCall)
func (a *Assembler) SetUsage(u protocol.Usage)
func (a *Assembler) SetMetadata(m Metadata)
func (a *Assembler) Done(reason protocol.StopReason)
func (a *Assembler) Fail(reason protocol.StopReason, msg string, err error)
func (a *Assembler) Emit(ev protocol.AssistantMessageEvent) error
```

## Behavior notes for the faux author

- One goroutine runs `body`; the Assembler is not goroutine-safe. After the stream is settled every method is a no-op and the `*Start` methods return -1. Loop on `!a.Settled()` for long producers; the `a.Context()` is the request context.
- Order of calls: `Start()` first. `Fail` before `Start` is valid (setup error: one `error` event, message built from the seed). `Done` before `Start` is a producer bug and fails the stream.
- Any assembler call made after the request context is cancelled settles the stream as aborted (also `Done` and `Fail`). If body returns after cancellation without a terminal call, the result is aborted, not incomplete.
- `Done(reason)` with an open block fails the stream with stop reason `error`, except `StopLength`, which salvages (see Deviations).
- `Fail(reason, msg, err)`: reason other than error/aborted becomes error; nil `err` becomes `errors.New(msg)`.
- `Emit` returns the builder error for an invalid event and leaves the stream open; a raw `DoneEvent`/`ErrorEvent` settles with the event's own message exactly as given (no usage or metadata overlay). `Emit` keeps content index, tool start records and used ids in sync, so scripted and raw calls can be mixed.
- Tool end with nil `final`: raw delta bytes that are a complete JSON object are kept compact (key order and number text preserved); otherwise `partialjson.Parse` once, re-encoded without HTML escaping; no delta -> start arguments -> `{}`.
- Generated tool ids are `call_1`, `call_2`, ... from a per-stream counter, skipping ids seen at start or end.
- Items and results are clones: producer values (`initialArgs`, signature pointers, `Usage` pointers, final `ToolCall`) can be mutated after the call.
- The final result is always `Clone()`d on `Result`; the terminal event holds its own copy.

## Deviations and decisions

1. `Done` with an open block and a reason other than `length` is a producer bug (stop reason `error`, text `done "toolUse" with unfinished content block N`). The brief listed salvage only for Fail, Done(length) and early close; an unfinished tool call under `stop`/`toolUse` could be executed by H2.
2. A supplied tool-end `ToolCall` with empty id or name falls back to the start values; empty arguments become `{}`; non-object arguments fail the stream. `ToolStart` rejects non-object initial arguments the same way.
3. Valid raw tool JSON keeps its bytes (compacted) instead of being re-marshaled through a map. This keeps key order and large integers exact; broken JSON still goes through `partialjson.Parse` once.
4. `ErrStreamClosed` is an extra exported sentinel (only `Emit` returns it). Block methods return nothing; use `Settled()`.
5. `Metadata.Diagnostics` appends (later calls add), other fields overwrite when non-nil.
6. Terminal delivery: `select{send, ctx.Done}`, then one non-blocking try after cancellation. With buffer room the abort event is always delivered; with a full abandoned channel it may be missing (documented on `Stream`).
7. `ConvertToLLM` selects by concrete type (value or pointer of the four built-in messages), so a `RawMessage` whose role name is "user" is still dropped. It returns the same message values, not copies.
8. `Fail` with an empty `msg` and a non-nil `err` uses `err.Error()` as errorMessage.
9. `NormalizeRequest` with only tools gives a system message with an empty (non-nil) `Content`; Pi uses an empty string there. Messages are always copied into a new slice.

## Test list

`stream_test.go`: close without terminal (ErrStreamIncomplete, partial text and open tool kept, identity and timestamp), close with no calls (one error event, no start), Result without a reader, result settles before a blocking terminal send, Result wait cancelled before settle (stream still finishes), settled result wins over a cancelled ctx, result clones are independent, second terminal and later calls no-op, abort while drained (error event with reason aborted, usage, partial content, `context.Canceled`), deadline abort (`DeadlineExceeded`, terminal call after cancel aborts), abort before start (one event, seed identity), abort with a full abandoned channel (no leak via `goleak.Find`), panic in body, nil ctx and negative buffer, ordering under `-race`, sentinel text.

`assembler_test.go`: plan 3.4 sequence, items rebuild the result through a builder, initial content / signatures / redaction / empty blocks, authoritative final arguments with no deltas (bytes kept), final with empty fields, non-object final, raw JSON parse table (valid, tab repair, truncated, HTML, non-object), open-tool salvage matrix (5 inputs by fail / length / close: completed blocks kept, no `*_end`, generated id), length keeps open text, done with open block, no scratch fields in the JSON of the result, usage in items and result, usage copies, late metadata and seed metadata, metadata on failure, fail before start, fail reason coercion, scripted abort, id collision avoidance, id and name change finalized by index, queued items immutable, seed copy and normalization, 8 builder-error cases (no panic, stream fails), done before start, Emit through the builder (rejected event, raw ids, raw done), Emit raw error, Emit copies.

`convert_test.go`: role filter (value and pointer forms, no mutation, raw role named "user" dropped, empty/nil), normalize (prompt + tools, prompt only, tools only, nothing added when empty, timestamp 0, no shared slices or raw JSON, spare capacity untouched).

Status: DONE_WITH_CONCERNS
Summary: Stream, NewStream, Assembler, errors, ConvertToLLM, NormalizeRequest and the three protocol fixes are in place; tests, race, vet and lint are green and `go mod tidy` made goleak direct.
Concerns: Encode-side reason/message mismatch is not enforced in `MarshalStreamEvent` (decode and builder are). Consequence: `MarshalStreamEvent` can emit a `done` or `error` whose JSON `UnmarshalStreamEvent` now rejects, and `TestStreamEventTerminalReasonRules` deliberately encodes such pairs, so enforcing it on encode is a test-contract change for the orchestrator to decide. README and `AGENTS.md` updates for the new files were not in my ownership list and were not touched.
