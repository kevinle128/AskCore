---
phase: 4
title: "Docs sync and full verification"
status: completed
priority: P2
effort: "0.5d"
dependencies: [3]
---

# Phase 4: Docs sync and full verification

## Goal

Every document that said "planned" for A1 states the implemented contract, and the whole repository passes tests, race detection, and lint.

## Files to Create / Modify

- Modify: `docs/async-task-runtime.md` (mark A1 done in section 17; fix any name that changed in code)
- Modify: `docs/ask-architecture-reference.md` (section 3 row for `internal/tasks` without "planned")
- Modify: `internal/agent/README.md` (source owners: `tasks.go`, `idle.go`; remove "Planned")
- Modify: `internal/tools/README.md` (`Context.Tasks` is real; imports list)
- Modify: `pkg/protocol/README.md` (`CustomMessage` and task events move from "Planned" to the owner table)
- Confirm: `internal/tasks/README.md` (written in phase 2) matches the code

## Steps

1. Read each file before editing. Change only the lines that describe A1.
2. Check every link and every `file:line` reference in the changed docs against the code.
3. Run the full verification below.
4. Write a short review section in `plan.md`: what shipped, test evidence, deviations from the design (if any) with their reason.

## Verification

- `go test -race ./...`
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`
- `go vet ./...`
- `grep -rn "time.Sleep\|NewTicker" internal/tasks internal/agent --include='*.go' | grep -v _test.go` prints nothing new.
- `ASK_FAUX_TPS=0 go run ./cmd/tui -p "hello"` still prints the faux answer and exits 0 (no regression of the headless path).
