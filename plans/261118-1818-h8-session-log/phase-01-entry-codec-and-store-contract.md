---
phase: 1
title: "Entry codec, ids, store interface and contract suite"
status: pending
priority: P1
effort: "6h"
dependencies: []
---

# Phase 1: Entry codec, ids, store interface and contract suite

## Goal

Every entry type encodes to one tagged JSON line and decodes back, sessions get one id format, and one store interface plus one contract suite exist and pass on `MemoryLog`.

## Context

- Closed entry list: `internal/sessions/entry.go:13-15,200-211`; `Clone` follows at `:213`.
- `protocol.Message` is an interface (`pkg/protocol/message.go:65`); decode with `protocol.UnmarshalMessage` (used at `pkg/protocol/events.go:299`).
- `MessagesOf` is the history projection (`entry.go:297`); `Writer.Messages` uses it (`memory.go:55-59`).
- Storage interfaces live in `internal/store` (`docs/ask-architecture-reference.md:136-139`). The `ask` binary already links `internal/store`.
- `sessions` must not import `store` after this phase (store imports sessions; a cycle would follow). Its README import line changes in phase 6.

## Requirements

1. New entry types in the closed list: `ModelChange{Model ModelRef}`, `ThinkingChange{Level protocol.ThinkingLevel}`, `ContextEdit{Target int; Replacement *ContextReplacement}` (target is a seq; nil replacement means omit). Add `Clone` cases.
2. `MessagesOf` applies `ContextEdit`: the latest edit of a target wins; an omitted message leaves model history; `Entries` keeps it. Callers that get the projection (all model-context or state reads, none needs the raw list): `internal/agent/request_log.go:201` (it passes `entries[:at]`, so a later edit never reaches an earlier rebuild), `context_source.go:20`, `recover.go:48`, `loop_stream.go:189`, `agent.go:256,612,730`, `internal/sessions/memory.go:56`. Follow snapshots read `Entries`, so clients still see full history.
3. `codec.go`: `Header` (type, version, id, cwd, createdAt, parentSession, parentSeq), `EncodeEntry(seq, e)`, `DecodeEntry(line)`, `EncodeHeader`, `DecodeHeader`. One table maps each Go type to one fixed wire word. Unknown word: `ErrUnknownEntryType` (a newer writer made it) unless the envelope says `ignorable`. Header version above the supported one: `ErrFormatNewer`. Bad JSON: `ErrCorruptLine`.
4. `id.go`: one process node from `github.com/bwmarrin/snowflake` with a random node number 0-1023, made once (`sync.Once`). `NewID() string` returns `ID.String()`. The package globals `Epoch`, `NodeBits`, `StepBits` stay at their defaults. The id is a `string` in every API.
5. `writer.go`: add `ErrWriterLost` (the writer can no longer append: seq mismatch or failed write).
6. `internal/store/session_store.go`: `SessionStore`, `SessionWriter`, `CreateMode`, `ErrSessionOwned`, `ErrSessionNotFound`, `ErrSessionCorrupt`, `ErrSessionFormatNewer` (see [design.md](design.md#interface-sketch-names-may-change-in-review)). Do not add it to `store.Stores` (the GORM aggregate).
7. `internal/store/storetest/session_contract.go`: `RunSessionWriter(t, newWriter)` and `RunSessionStore(t, newStore)`. Writer part: atomic append, rejected unencodable entry leaves the log unchanged, deep copies, `CommitRef` positions, `LastSystemSnapshot`, entries read back in append order (write ordering). Store part (every call takes the canonical cwd and the id): create, open, reopen continues at the next seq, second open fails with `ErrSessionOwned`, close releases, lazy create leaves no file until the first assistant message, `List` returns headers only, sorted by id as strings, newest first.
8. Add `github.com/bwmarrin/snowflake` to `go.mod` and `go.sum`.

## Files owned

- Modify: `internal/sessions/entry.go`, `internal/sessions/writer.go`, `go.mod`, `go.sum`
- Create: `internal/sessions/codec.go`, `internal/sessions/codec_test.go`, `internal/sessions/id.go`, `internal/sessions/id_test.go`, `internal/sessions/context_edit_test.go`, `internal/sessions/memory_contract_test.go` (package `sessions_test`, runs `RunSessionWriter` on `MemoryLog`)
- Create: `internal/store/session_store.go`, `internal/store/storetest/session_contract.go`

## Tests written first

- `TestCodecRoundTripsEveryEntryType`: one value per type, including messages of every role and a `RequestDelta` with `Removed` and `Added`; `reflect.DeepEqual` after decode. It fails when a new entry type has no wire word.
- `TestCodecRefusesUnknownType`, `TestCodecSkipsIgnorableUnknownType`, `TestHeaderNewerVersion`, `TestCodecCorruptLine`.
- `TestNewIDFormat` (19 decimal digits), `TestNewIDOrderMatchesStringOrder`, `TestNewIDUniqueAcrossGoroutines`, `TestNewIDParseRoundTrip`.
- `TestContextEditOmitsFromMessagesKeepsEntries`, `TestContextEditLatestWins`.
- `TestMemoryLogWriterContract`.

## Steps

1. Write the failing tests above.
2. Add the three entry types and their `Clone` cases.
3. Add the codec table and functions; keep the decoder strict (`DisallowUnknownFields` only on the envelope, not on data, so additive fields stay readable).
4. Add the id node and `NewID`.
5. Extend `MessagesOf` for `ContextEdit`.
6. Add the store interface and errors, then the contract suite.
7. `go mod tidy` for the new module.

## Validation

```
go test ./internal/sessions/... ./internal/store/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| `MessagesOf` change alters `RequestDelta` baselines (`request_log.go:188-217`) | M x H | no `ContextEdit` exists in any live log yet; phase 3 adds a rebuild test with an edit |
| A future entry type ships without a codec word | M x M | round-trip test iterates the closed list from one table and fails on a gap |
| Import cycle store <-> sessions | L x H | `sessions` imports neither `store` nor `storetest`; contract test is an external test package |

## Rollback

Revert the commits. Nothing outside these files uses the new types until phase 2 and 3.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages. Comments state the invariant (for example "seq equals the slice position, so CommitRef stays valid after reopen").
