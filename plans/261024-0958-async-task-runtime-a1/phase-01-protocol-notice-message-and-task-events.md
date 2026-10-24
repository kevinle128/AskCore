---
phase: 1
title: "Notice message and task events in protocol"
status: completed
priority: P1
effort: "0.5d"
dependencies: []
---

# Phase 1: Notice message and task events in protocol

## Goal

`pkg/protocol` has a built-in `CustomMessage` and the three task events, the converter gives the model a custom message as a user message, and `queue_update` shows its text.

Design: [section 7.4](../../docs/async-task-runtime.md) and [section 13](../../docs/async-task-runtime.md).

## Files to Create / Modify

- Modify: `pkg/protocol/message.go`: `reservedRole` (`:623-629`), `MarshalMessage` (`:657-684`), `UnmarshalMessage` (`:696-740`), `CloneMessage` (`:799-848`, explicit deep copy), and the docstrings that say "four built-in roles" (`:58-61`, `:631`, `:654-656`, `:796-798`)
- Modify: `pkg/protocol/events.go` (`TypeTaskStart`, `TypeTaskUpdate`, `TypeTaskEnd` and structs)
- Modify: `pkg/protocol/codec.go` (`EncodeEvent` `:16-147`, `DecodeEvent` `:235-425`)
- Modify: `internal/providers/convert.go` (`ConvertToLLM` maps `CustomMessage` to `UserMessage`)
- Modify: `internal/agent/queue.go` (`messageText` reads `CustomMessage`)
- Create tests: `pkg/protocol/custom_message_test.go`, `pkg/protocol/task_events_test.go`
- Modify tests: `pkg/protocol/hardening_test.go:28-30` (`TestIndentedUnknownRoleStaysOneLine`) and `pkg/protocol/events_test.go:44,71-80` (`TestEventRoundTripAllTypes`) use `"custom"` as an unknown role: change their fixtures to a free role name such as `branchSummary` (`events_test.go:180`) <!-- Red team #14 -->
- Modify: `pkg/protocol/README.md` (the role `custom` is now built in; an object with `"role":"custom"` no longer decodes to `RawMessage`)
- Modify tests: `internal/providers/convert_test.go` (rename `TestConvertToLLMKeepsFourRolesAndDropsTheRest` to `TestConvertToLLMKeepsBuiltInRolesAndDropsRaw`; raw messages are still dropped), `internal/agent/queue_test.go`

Scout evidence: [deep scout](../reports/scout-261024-1705-async-task-runtime-a1-deep.md).

## Types

```go
const RoleCustom = "custom"

// CustomMessage is an application message, as Pi's CustomMessage. The model
// sees it as a user message with the same content.
type CustomMessage struct {
	CustomType string          // "task-notification"
	Content    []UserBlock     // wire accepts a string or blocks, as UserMessage (message.go:505)
	Display    bool
	Details    json.RawMessage // a JSON object or absent; task ID, kind, status; never credentials
	Timestamp  int64
}

type TaskStart struct { Envelope; TaskID, Kind, Title string; StartedAt int64 }
type TaskUpdate struct { Envelope; TaskID string; OutputBytes, OutputLines int64 }
type TaskEnd struct { // no notice outcome: the registry emits it before Deliver
	Envelope
	TaskID, Status, OutputFile string
	ExitCode *int
	DurationMS int64
}
```

## Tests first (they fail before the change)

| Test | Proves |
|---|---|
| `TestCustomMessageRoundTrip` | Marshal then unmarshal keeps every field; JSON has `"role":"custom"` and `customType`; string content decodes to one text block; non-object `details` is rejected |
| `TestCustomRoleWithoutTypeIsAnError` | `{"role":"custom"}` with no `customType` gives a decode error, not a `RawMessage` |
| `TestRegisterRoleRejectsCustom` | `RegisterRole("custom", ...)` returns the reserved-role error |
| `TestCloneCustomMessageIsDeep` | A change to the clone's content or details does not change the original |
| `TestTaskEventsEncodeDecode` | Each task event round-trips with its envelope; `TaskUpdate` has no text field |
| `TestConvertToLLMMapsCustomToUser` | A log of user, custom, assistant gives user, user (same content), assistant; other unknown roles are still dropped |
| `TestQueueUpdateShowsCustomPreview` (agent package) | A queued `CustomMessage`, value or pointer, appears in `queue_update` as a preview of at most 200 bytes; a `task-notification` shows `task <id> <status>: <title>` from `Details` |

## Steps

1. Write the tests above. Run them and see them fail.
2. Add `RoleCustom` to the role constants and to `reservedRole`.
3. Add `CustomMessage` with `MarshalJSON` and `UnmarshalJSON` (reuse `decodeUserBlocks(w.Content, true)`), the `UnmarshalMessage` case, and an explicit deep-copy `CloneMessage` case (value and pointer forms, as the other roles). Update the four docstrings.
4. Add the three event types, `EventType()` methods, and the codec cases. Follow the pattern of `QueueUpdate` (`events.go:88-92`, `codec.go:22`, `codec.go:251-259`).
5. Change `ConvertToLLM` to append `protocol.UserMessage{Content: m.Content, Timestamp: m.Timestamp}` for a `CustomMessage` (value or pointer). Update its godoc.
6. Change `messageText` (`queue.go:118-132`): `UserMessage` value and pointer give their text as today; a `CustomMessage` value or pointer gives a preview of at most 200 bytes, built from `Details` for `task-notification`, else from its text. Full notice text never enters `queue_update`, so the event stays far below the 256 KiB bus cap (`bus/follow.go:15-16`). <!-- Red team #12 -->
7. Update the two existing tests that used `"custom"` as an unknown role, and the protocol README.
8. Run the tests and see them pass.

## Verification

- `go test -race ./pkg/protocol/... ./internal/providers/... ./internal/agent/...`
- `go vet ./pkg/protocol/... ./internal/providers/...`
- The existing codec and replay tests stay green, which shows that logs without custom messages project as before.
- Note for later phases: a custom `LoopConfig.ConvertToLLM` must map `CustomMessage` itself, because `request_log.go:114` uses the default converter.
