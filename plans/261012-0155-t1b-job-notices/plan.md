---
title: "T1b slice N1: background job notices in the TUI"
description: "Hide system-reminder notices in the TUI, draw a job line for a finished job, and show the running job count on the status rail."
status: completed
priority: P1
branch: new-session
tags: [tui, jobs, tdd]
created: 2026-10-12
tdd: true
---

# T1b slice N1: background job notices in the TUI

Approved design: [design.md](design.md) (user, 2026-10-12). It closes the H5 release gate ([roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md), H5 status and T1b).

## Outcome

The TUI never shows a `<system-reminder>` message as a prompt or a queue row. A finished job is one job line in scrollback. The status rail shows the running job count. Same behavior in the leader TUI and the in-process TUI.

## Decisions (user, 2026-10-12)

- Finished job: one job line (design open question 1, A).
- Jobs that started before the client attached: accepted gap, no new ACP method (2, A).
- `session/close` on the leader wire: a separate H13 slice right after this one (3, A).

## Steps

| # | Step | Files | Status |
|---|---|---|---|
| 1 | Notice parsing in the protocol package: `IsSystemReminderText`, `SystemReminderBody`, `ParseJobNotice`, `ParseMonitorEvents`; round trip against the real job table text | `pkg/protocol/job_notice.go`, `pkg/protocol/job_notice_test.go`, `internal/jobs/notice_parse_test.go` | done |
| 2 | Translator: job line instead of a reminder prompt (live and snapshot); running job set from tool results and notices | `internal/tui/translate.go`, `internal/tui/translate_snapshot.go`, `internal/tui/blocks.go` | done |
| 3 | Session view: reminder texts left out of the queue; job count on the status rail | `internal/tui/update_session.go`, `internal/tui/status.go` | done |
| 4 | Screen tests for acceptance criteria 1-11 (80×24, 40×14) | `internal/tui/*_test.go`, goldens | done |
| 5 | E2E with the built `ask` (criterion 12), leader and in-process | `cmd/tui` | done |
| 6 | Docs: TUI README, roadmap H5 gate | `internal/tui/README.md`, roadmap | done |

## Acceptance

Every criterion of design section 10 has a named test that passes. `go test ./internal/tui ./cmd/tui ./pkg/protocol ./internal/jobs`, `go vet ./...`, golangci-lint pass.

## Acceptance map (design section 10)

| # | Test |
|---|---|
| 1 | `TestJobCountOnTheIdleRail`, golden `TestJobNoticeGoldens/running_80` |
| 2 | `TestJobCountOnTheIdleRailNarrow`, golden `TestJobNoticeGoldens/running_40` |
| 3 | `TestJobCountOnTheIdleRail` (second job) |
| 4 | `TestJobCountJoinsTheWorkingTail` |
| 5, 6 | `TestFinishedJobIsAJobLineAndLeavesTheRail`, golden `TestJobNoticeGoldens/finished_80` and `finished_40` |
| 7 | `TestQueuedNoticeHasNoRow` |
| 8, 9 | `TestOnlyANoticeQueuedDrawsNoQueue` |
| 10 | `TestFailedAndKilledJobLines`, `TestTranslateJobNoticeIsAJobLine` (text; the role is the `ToolFailed` path of the tool line, which the screen tests run without color) |
| 11 | `TestMonitorNoticeLine`, `TestTranslateMonitorNoticeCountsEvents` |
| 12 | `TestInteractiveBackgroundJobThroughTheLeader`, `TestInteractiveBackgroundJobInProcess` (built `ask` on a PTY, scripted Anthropic fixture) |

Parser and format guard: `TestParseJobNotice`, `TestParseMonitorEvents` (`pkg/protocol`), `TestNoticeTextParsesInTheProtocol`, `TestMonitorEventTextParsesInTheProtocol` (`internal/jobs`).

## Evidence (2026-10-12)

- `go vet ./...`, golangci-lint (0 issues), `go test ./internal/tui ./pkg/protocol ./internal/jobs ./internal/agent ./internal/acp ./cmd/tui` pass.
- Goldens `internal/tui/testdata/TestJobNoticeGoldens/` compared with the approved sketches S1 and S4 (80×24, 40×14).
- `python3 e2e/tui/run.py --driver ask`: 8/8 pass on alacritty and ghostty (artifacts `e2e/tui/artifacts/4bbc906fc6`).
- Found on the way: `ask-resize` failed on both engines before this slice's fix of the runner. Its `USAGE_ROW` pattern accepted only whole token counts (`↑850`); since H5 sends eleven tool declarations, the faux input is above 1000 tokens and shows as `↑2.6k`. The pattern now accepts `k` and `M`. No product change.
