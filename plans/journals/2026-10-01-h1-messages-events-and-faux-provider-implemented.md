---
title: "H1 messages, events and faux provider implemented"
date: 2026-10-01
summary: "Ported Pi message model, delta-only events, stream/assembler, partial JSON, SSE reader and faux provider to Go; all H1 criteria met"
---

# H1 messages, events and faux provider implemented

## What happened
- Implemented roadmap phase H1 from `plans/261001-0836-h1-messages-events-faux/plan.md`: `pkg/protocol` (messages, content, usage, stream and agent events, envelope, JSON/JSONL codec, builder) and `internal/providers` (stream, assembler, convert, `partialjson`, `sse`, `faux`).
- About 5,400 lines of production code and 5,000 lines of tests. `go test -race -count=3`, vet and golangci-lint are green on `./pkg/... ./internal/providers/...`.
- A code review found 2 Major defects: faux chunks and ids depended on earlier `Func` steps (shared PRNG), and `RawEvent` encode could break JSONL lines. Both are fixed with regression tests. The final advisory check found that `RawEvent` dropped envelope changes; fixed.

## Decision
- Stream design: one producer goroutine owns sends and the single close; the result settles before the terminal send; after cancel the terminal send is non-blocking. `Result(ctx)` never hangs (Pi's `EventStream.end()` without a result hangs).
- Faux tool ids are per call (`tool:<call>:<n>`); `faux.Raw` passes script identity through; decode accepts Pi `text_start`/`thinking_start` without `content`.
- `Done` with open blocks (except `length`) or with `error`/`aborted` fails the stream; terminal reason must equal message stop reason on encode, decode and in the builder.

## Next steps
- Owner decision: `go build ./...` fails because `internal/logs` is untracked (`.gitignore:44` `logs/` matches it).
- `ak plan update --status completed` failed with "plan not found"; plan status stays `ready` until fixed.
- H2: agent loop uses `Result(ctx)` after EOF instead of waiting for a terminal event.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
