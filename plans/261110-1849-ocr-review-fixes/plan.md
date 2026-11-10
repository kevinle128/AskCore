---
title: "Fix the 46 confirmed defects of the ocr review"
description: "Fix every real defect that the two triage rounds of the ocr review of master..develop confirmed, with one test for each behavior bug and the docs that the user-visible changes need."
status: pending
priority: P2
effort: 32h
branch: develop
tags: [bugfix, backend, agent, providers, leader, acp, tools, cli]
blockedBy: []
blocks: []
created: 2026-11-10
---

# Fix the 46 confirmed defects of the ocr review

## Overview

Two triage rounds of the ocr review (`develop` at `1940e70`) confirmed 46 real defects: 4 Medium (R1, R2, R3, N1) and 42 Low. The user approved the fix of each one in [the compare report](../reports/compare-261110-1826-triage-fixes-omp-grok.md), with three user decisions: R6 follows the Grok rule, R2 skips only targets outside the repo, R3 uses a 4 MiB whole-read cap. Q-write stays as it is (no change). Evidence: [round 1 summary](../reports/triage-261110-1809-ocr-summary.md), [round 2 list](../reports/triage-261110-1832-round2-real-list.md), and the group tables that they link.

Every location in the phase files was read again at `1940e70`. The plan may name finding codes (R1, N12). Code comments, test names, and commit messages must not: they describe the behavior.

## Outcome and acceptance

- Each defect has the accepted fix and, for a behavior bug, a test that fails before the fix and passes after it. Tests use the real entry points, fake clocks for time, and no network.
- No tool description changes, so `docs/reference/` and the six cassettes in `cmd/tui/testdata/cassettes/` stay as they are. The reference drift test and all cassette replays pass unchanged. If a phase must change a tool text that goes to the model, it re-records all six cassettes (`ASK_RECORD=1`, `OPENAI_API_KEY` from the environment, never printed).
- `go vet ./...`, `go test ./...`, `go test -race` on the touched packages, golangci-lint (depguard), and the reference drift test pass.
- `docs/cli.md`, the owning READMEs, and `CHANGELOG.md` `[Unreleased]` describe every user-visible change (phase 7 lists the lines).

## Phases

| Phase | Name | Status | Effort |
|-------|------|--------|--------|
| 1 | [Agent, pipeline and task registry](./phase-01-agent-pipeline-tasks.md) | Pending | 5h |
| 2 | [Providers](./phase-02-providers.md) | Pending | 6h |
| 3 | [Leader, ACP and app composition](./phase-03-leader-acp-app.md) | Pending | 4h |
| 4 | [Tools, context files, skills and sandbox](./phase-04-tools-bootstrap-skills-sandbox.md) | Pending | 7h |
| 5 | [Settings and auth](./phase-05-settings-auth.md) | Pending | 2h |
| 6 | [CLI commands, capture and e2e](./phase-06-cmd-tui-e2e.md) | Pending | 5h |
| 7 | [Docs, changelog and full gate](./phase-07-docs-and-gate.md) | Pending | 3h |

## Dependencies

Phases 1, 2, 4 and 5 have no blocker and can run in parallel. Phase 3 needs `bootstrap.SkipOutsideProject` from phase 4 for one step (the session warning text); its other steps can start at once. Phase 6 needs `acp.WireVersion` from phase 3 and `bootstrap.SkipOutsideProject` from phase 4. Phase 7 needs all code phases. Each step that waits compiles on its own, because the switches that render a skip reason have a default branch (`internal/app/session_start.go:382`, `cmd/tui/settings_command.go:354`).

## File ownership

Each file has one owner phase. The full lists are in the phase files. Two files sit outside their package group on purpose: `internal/app/session_models.go` belongs to phase 2 (compat default of N7), and `internal/providers/cassette/cassette.go` belongs to phase 6 (file mode of R11). `internal/settings/models_file.go` belongs to phase 5 (base URL trim of N3/N4). `pkg/protocol` is not changed.

## Data flow of the risky changes

- R1: `Continue` -> `begin` takes the slot (`internal/agent/agent.go:567-591`) -> under `a.mu`, one decision: queued input runs in this run, or the run is marked closing -> `enqueue` on a closing run sets `wake` (`internal/agent/queue.go:344-348`) -> `end` starts the next run (`agent.go:609-614`).
- R2: `LoadContext` (`internal/bootstrap/load.go:64-112`) -> for a project folder, resolve the file and the project root with `filepath.EvalSymlinks` -> a target outside the root becomes `Skip{Reason: SkipOutsideProject}` -> rendered by `contextSkipText` (session warning, ACP diagnostic) and `contextSkipWord` (`ask settings show`).
- R3: `read` -> stat -> sniff the first 8 KiB (image, binary) -> an image over 20 MiB is refused, a smaller image is read whole and then checked for 16 M pixels -> text of 4 MiB or less is read whole (today's path), larger text goes through a buffered scan that keeps only the page and counts the total lines.
- R6: `authRunner` sets `opts.Auth` (`internal/app/module_auth.go:46-58`) -> `profileClient` (`internal/providers/openai/profiles.go:115-139`) picks the `Authorization` value: the models.json header for a static key, the token for OAuth -> the guard checks that value.

## Rollback

Each phase is one or more focused commits. Revert the commits of a phase with `git revert`. No phase migrates data or changes a file format. Phase 3 and 6 steps that use the new enum or the exported constant revert with their phase; revert phase 6 before phase 3 or 4 when both are reverted.

## Decisions

User answers of 2026-11-10, applied in the owning phases:

1. N7 (phase 2, step 7): wire `supportsReasoningEffort`, default true; `false` means Ask does not send `reasoning_effort`. A test covers it.
2. R3 images (phase 4, step 2): `read` refuses an image file larger than 20 MiB (omp's cap) before it reads the file whole. An image of 20 MiB or less still goes through the 16 M pixel check (N25, phase 4, step 5). A test and a docs line (phase 7) cover it.
3. R3 text (phase 4, step 2): the page text stays the same for every file size; the large-file path still counts and reports the total lines.

## Open questions

None.

Note: N1 follows the approved rule, so a reply with only thinking now counts as empty and is retried.
