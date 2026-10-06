# H1 protocol implementation report (steps 4.2 and 4.3)

Files created in `pkg/protocol`: `content.go`, `usage.go`, `tool.go`, `message.go`, `stream_events.go`, `events.go`, `codec.go`, `builder.go`, and one `_test.go` for each of them (`message_test.go` covers `message.go`). Production code imports the standard library only. Nothing else was edited.

## Verification

- `go test -race -count=3 ./pkg/protocol/` passes. Statement coverage is 90.4 percent.
- `go vet ./pkg/protocol/...` is clean.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./pkg/protocol/...` reports 0 issues.
- `go build ./pkg/... ./internal/providers/...` passes.
- `go build ./...` FAILS, and not because of this work: `internal/app/app.go:17` imports `AskCore/internal/logs`, which is not in the tree. `.gitignore:44` has the pattern `logs/`, which matches `internal/logs`, so the directory was never tracked. This needs an owner decision (anchor the pattern as `/logs/`, or add the package).

## API summary

The required names are all present with the required shapes. Points the callers need to know:

- Events are pointers (`*MessageEnd`, `*AgentStart`). `Envelope.Env()` has a pointer receiver, so only pointers satisfy `Event`. `DecodeEvent` returns pointers. Provider stream events are values; `MarshalStreamEvent` rejects pointer forms.
- `Message` values are values (`AssistantMessage`), and `MarshalMessage`/`CloneMessage` also accept the pointer forms. `UnmarshalMessage` returns values.
- Every built-in message and block type also implements `json.Marshaler` and `json.Unmarshaler` with role and type checks, so plain `json.Marshal` works on them.
- Type name constants: agent events `TypeAgentStart` and so on; stream events `StreamTypeStart`, `StreamTypeTextDelta` and so on. Nested status constants `NestedStatusOK/Error/Unfinished`. Thinking levels `ThinkingOff` through `ThinkingMax`.
- `Sections` is `[]Section`. On the wire it is an ordered JSON object. `nil` Value is null. A repeated key keeps the position of the first and the value of the last.
- `DiagnosticError.Code` is `json.RawMessage` (absent, a string or a finite number, form kept). `Name` and `Stack` are `*string`. `Details` is `json.RawMessage`, an object only.
- `NestedToolCallRecord.DurationMs` is `*float64` (Pi can emit fractional milliseconds). `ArgumentsBytes` is `*int64`.
- `Builder` extra method `ApplyAgentEvent(Event) error`: `message_start` seeds, `message_update` applies the block event and the usage, `message_end` replaces the message. Other roles and event types are ignored. This is the "agent `message_start`" path in plan 4.3.

## Deviations and decisions

- Start event JSON uses the key `message` for the seed (Pi calls it `partial` and never sends it on the wire). Text and thinking `*_start` events carry `content` as a full block object, while `*_end` carries `content` as a string, as the required API implies.
- `StopReason` is validated on both encode and decode of an assistant message. A zero `AssistantMessage{}` therefore fails to encode (stop reason is empty). Producers must set the stop reason (the builder seed uses `StopPending`).
- `Timestamp` and other plain numbers are always written; a missing timestamp decodes to 0, so absent and zero cannot be told apart for those fields.
- A JSON string for user content with value `""` becomes one empty text block. Tool result content must be an array (a string is an error).
- A `StartEvent` seed that already holds blocks is accepted; those blocks start closed. The plan only says the seed content is empty; this is lenient on purpose.
- `RawToolJSON` returns the delta bytes only (not the start arguments), and returns nil after the block ends. `Snapshot` shows an open tool call with its start arguments.
- `RawEvent.Data` and `RawMessage.Data` hold the complete original object and re-encode unchanged; later edits to `RawEvent.Envelope` are not written back.
- `EncodeEvent` and all codecs write without HTML escaping. U+2028 and U+2029 are still escaped by `encoding/json`, which keeps JSONL lines safe.
- JSONL reader: empty lines are returned as empty slices (the caller skips them). A lone trailing CR on the final unterminated line is stripped.
- The `deferred` handle of Pi is not ported (plan F1.5).
- Cost fields are `int64` micro-USD. A Pi float such as `0.003` fails to decode; it does not round silently.

## Test list

- Content: golden JSON for all blocks, empty signature and `redacted:false` kept, tool arguments must be an object, discriminator checks, no HTML escape, lone surrogate gives U+FFFD (Go behavior, documented in the test), U+2028/2029/LF escaped.
- Usage and tool: zero usage is an object, optional zero kept, micro-USD round trip, fractional cost rejected, tool declaration defaults.
- Messages: golden assistant JSON, full round trip with every optional field, empty content encodes `[]`, null or absent content gives an empty slice, invalid input table (image in assistant, unknown block, string content, bad or missing stop reason, bad tool arguments), user string to block, image-only user, system string to block, ordered sections with null and duplicate key rules, tool result round trip with nested call omissions (`{}` kept, omitted kept), diagnostic string and number codes (and `1e999`, bool, object rejected), unknown role kept raw and re-encoded, role registry (duplicate, reserved, nil, race), deep copy tests for all kinds, tool execution result JSON.
- Stream events: golden JSON for all twelve types with round trip, terminal reason rules on encode and decode, malformed input table, the nine block events versus start, done and error.
- Agent events: round trip for all ten types and the raw type, flat envelope golden JSON, `uint64` max seq, filling the envelope through `Env()`, `message_update` rejects non-block events, unknown event kept raw, unknown role inside an event stays raw, malformed table, empty arrays encode as arrays.
- JSONL: one line per event, embedded LF, CR, U+2028, U+2029 stay inside one line, failed encode writes nothing, short and failed writes, concurrent writers do not interleave, framing table (LF, CRLF, one CR only, final line without LF, bare CR, U+2028/2029, empty lines) also with one-byte reads, 70 KiB / 1 MiB / 10 MiB lines, malformed line then recovery, read error returned, owned slices.
- Builder: plan 3.4 exit sequence, in-progress snapshot, interleaved blocks, initial content with no deltas, signed empty blocks and redacted thinking, end replaces metadata and removes absent optional values, content only at end, final tool id, name, namespace and arguments, done and error replace the whole partial, setup error before start, seed with blocks, 27 bad-sequence cases (no panic, state unchanged), recovery after a rejected event, immutability (producer mutation, snapshot mutation, repeated results, `RawToolJSON` copy), agent event application, JSONL projection rebuild, and a bytes and allocation scaling test at 2,000 and 20,000 deltas for text, thinking and tool buffers (no wall-clock gate).

Status: DONE_WITH_CONCERNS
Summary: All files, tests, vet, race and lint pass for `pkg/protocol`. The API matches the required names, with the additions listed above.
Concerns: (1) `go build ./...` fails on the missing `internal/logs` package, caused by the `.gitignore` pattern `logs/`; it is outside my file ownership. (2) Events must be used as pointers because of the pointer-receiver `Env()`. (3) Stop reason validation on encode means a zero `AssistantMessage{}` cannot be encoded.
