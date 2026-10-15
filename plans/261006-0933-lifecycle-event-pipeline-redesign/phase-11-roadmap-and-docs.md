---
phase: 11
title: "Roadmap and documentation update"
status: done
priority: P2
effort: 5h
dependencies: [phase-10]
---

# Phase 11: Roadmap and documentation update

## Goal

Make the roadmap and evergreen docs describe the implemented lifecycle, and remove the parts of H2, H8, H9, H11 and H12 that this redesign delivered or changed.

## Context links

- Roadmap: H2 (abort text, H-LOOP-09 whole-batch serial rule, test "hook mutates the arguments"), H3, H8, H9 (tests line "Mid-stream public output cannot be transparently replayed"), H11, H12, D19 (:62), D23 (:66), D25 (:68), D26-D29 (:69-72). Re-read the line numbers before editing; the roadmap changes often.
- `docs/ask-architecture-reference.md` section 7.2 and lines 86, 129, 201, 350 (`pipeline.Step`, `<name>_step.go` names) <!-- red-team #13 -->
- `internal/README.md`; package READMEs of `agent`, `pipeline`, `sessions`, `bus`, `scheduler`, `tools`

## Files to Create / Modify

- Modify: `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`
- Modify: `docs/ask-architecture-reference.md`, `internal/README.md`
- Modify: `internal/agent/README.md`, `internal/pipeline/README.md`, `internal/sessions/README.md`, `internal/bus/README.md`, `internal/scheduler/README.md`, `internal/tools/README.md`, `internal/providers/README.md` (`Prepare`, `Failure` codes), `pkg/protocol/README.md`
- Modify: `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md` status line ("implemented by plan `261006-0933-lifecycle-event-pipeline-redesign`"). Do not write "accepted" unless the user has accepted the report.

## Tasks & Steps

1. Roadmap revision line: "the lifecycle redesign is implemented (plan `261006-0933-lifecycle-event-pipeline-redesign`)".
2. D19 row: the revised model is implemented. H2/H3 Pi abort text is history. D26-D29 rows: implemented (commit after preparation, `Remove` and `AfterTool` context, provider `Prepare`, `Dispose`).
3. H2: H-LOOP-09 now reads "a call runs alone unless its tool declares itself concurrency-safe for these arguments; an exclusive call is a barrier" (D25). The H2 test "hook mutates the arguments" is replaced by "a pre-tool hook can allow, deny or cancel; arguments are frozen" (D25). Max-tokens: tool calls of a truncated message are dropped and the cycle stops (D25). <!-- red-team #10 -->
4. H8: "defines the entry types" becomes "makes the in-memory entries durable (SQLite), adds lease and resume"; D23 rebuild works in memory.
5. H9: remove queues, retry and `agent_settled` placement. Replace "Mid-stream public output cannot be transparently replayed" with "a retry after visible output is shown on the wire (Pi `auto_retry_*` sequence); the failed attempt is log-only" (D25). Keep overflow detection, usage totals, context-usage estimate, usage entries.
6. H11: typed dispatch exists; H11 keeps the 41-event taxonomy mapping, compiled-in handlers and fail-closed `tool_call`.
7. H12: follow path, bounded ring, cursor and resync exist; H12 keeps redaction policy for the dashboard, tracing spans, OTel export and hang detection.
8. Architecture reference 7.2: replace hook names with the control points; describe the queues (DeepSeek claim shape, no `all`/`one-at-a-time` modes); replace `pipeline.Step` and `<name>_step.go` at lines 86, 129, 201, 350.
9. Verify every changed claim against code (cite `file:line`) and every link.

## Tests

```sh
go test ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
grep -rn 'pipeline.Hooks\|GetSteeringMessages\|FinishTurn\|Sequential()\|one-at-a-time' docs internal --include='*.md'   # expect no stale hits
```

## DeepSeek conformance rows covered

None new. The matrix is linked from the architecture reference.

## Risks & rollback

- Stale docs claim behavior that is not built. Mitigation: step 9.
- Rollback: reset to tag `lifecycle-p11-base`.

## Done criteria

- No doc names a removed hook, the old tool-mode rule or the old retry rule; H2, H8, H9, H11, H12 list only remaining work; links resolve.
