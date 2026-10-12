---
title: "T1b: tool output previews, transcript view and /copy"
description: "Show a short preview of each tool output, open a temporary alt-screen transcript view with ctrl+o, show model-side truncation, and add /copy."
status: superseded
priority: P1
branch: new-session
tags: [tui, tools, tdd]
created: 2026-10-12
tdd: true
---

# T1b: tool output previews, transcript view and /copy

> Superseded (user, 2026-10-12): the interactive TUI moves to the Grok-style alternate screen. See [261012-1337-t1b-grok-style-altscreen](../261012-1337-t1b-grok-style-altscreen/plan.md). `/copy`, the diff and the truncation line carry over; the previews and the `ctrl+o` transcript view are replaced.

Approved design: [design.md](design.md) (user, 2026-10-12). Inventory rows T-LAY-09, T-MD-10 and T-CMD-08 ([roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md), T1b). Source comparison: [report](../reports/xia-261012-0211-t1b-tool-output-copy-compare.md), [mockup](../reports/xia-261012-0220-tool-expand-three-styles-mockup.html).

## Outcome

Each tool line has a short preview with a `ctrl+o` hint. `ctrl+o` opens a temporary transcript view with all output, and `ctrl+o` or `Esc` gives back the same inline view and draft. A model-side truncation shows one muted line. `/copy` copies the last assistant text and reports the real result. The same behavior applies to the leader TUI and the in-process TUI. There is no protocol change.

## Decisions (user, 2026-10-12)

- Expand style C (Claude Code): separate transcript view; the inline scrollback is never redrawn for an expand. Per-block folding (Grok) is rejected.
- Thinking in the view follows `ctrl+t`. No mouse capture. `/copy` is a command only, with no key in M1.
- A real-terminal alt-screen return spike comes before the view is implemented.
- The provider box copy text is fixed in a follow-up, not in this slice.
- Spike result (cook, 2026-10-12): a write right after the leave of the alt screen was lost, and `tea.Println` did write into the alt screen. Fixed in the renderer of `third_party/bubbletea` (`insertAbove` and `replayInline`, two renderer rules, tests in `replay_test.go`, `PATCH.txt`), not with a timer in the model. The model still holds commits and the resize rebuild while the view is open, and does not open the view while a commit or replay runs.

## Source

| Source | Ref | Used for |
|---|---|---|
| Pi | `~/Desktop/workspace/opensources/pi` | Preview sizes, hint text, truncation line, `/copy` order of clipboard methods and the OSC 52 cap |
| Claude Code | installed 2.1.183 (binary strings only) | Transcript view model (`ctrl+o`) |
| Grok | `grok-build` at `559751fd` | Compared only; not ported |

No code is copied, so no license obligation applies.

## Steps

| # | Step | Files (main) | Status |
|---|---|---|---|
| 0 | Spike: enter and leave the alt screen from the inline program on Terminal.app, iTerm2, kitty and tmux, with a resize in the view (open question 4). Stop and re-plan if it fails | `e2e/tui` spike, report in `plans/reports/` | done: partial; [report](../reports/spike-261012-t1b-altscreen-return.md). Fork fix applied (see Decisions). Terminal.app, iTerm2, kitty not run: by-hand check open |
| 1 | Read `ToolResultDetails` and partial bash results in the translator; tool block holds output, diff stats and truncation | `internal/tui/translate.go`, `internal/tui/blocks.go` | done: `TestToolSnapshotResultShowsPreviewAndTruncation`, `TestToolStreamingBashReplacesPreviewWithLastFiveLines` |
| 2 | Collapsed preview by tool, hint row, truncation row, width cut (criteria 1-7) | `internal/tui/blocks_format.go`, screen tests | done: `blocks_preview_test.go`, `TestToolPreviewScreenAtEightyColumns`, `TestToolPreviewScreenAtFortyColumnsCutsLongLine` |
| 3 | Transcript view: alt screen through `tea.View.AltScreen`, scroll keys, follow and `new output · End`, thinking follows `ctrl+t`; commits and resize rebuild wait until return (criteria 8-14) | new `internal/tui/transcript_view.go`, `update_input.go`, `render_inline.go` | done: `transcript_view_test.go`; e2e `ask-transcript-view` passes on alacritty and ghostty |
| 4 | Terminal restore on ctrl+c and signals in the view (criterion 15) | `internal/tui/quit.go`, `cmd/tui` E2E | done: `TestTranscriptViewCtrlCClosesTheViewThenAsksToQuit`, `TestTranscriptViewStopSignalRestoresTheMainScreen`; e2e `ask-transcript-view-sigterm` passes on alacritty and ghostty |
| 5 | `/copy`: last assistant text, native command, then OSC 52 with the cap, result texts (criteria 16-20) | new `internal/tui/clipboard.go`, `commands.go` | done: `clipboard_test.go` |
| 6 | E2E with the built `ask` through the leader and in process; `python3 e2e/tui/run.py` | `e2e/tui`, `cmd/tui` | done: `python3 e2e/tui/run.py` passes on alacritty and ghostty, with view, SIGTERM, `/copy` and in-process view cases; `go test ./cmd/tui` passes |
| 7 | Docs: TUI README, inventory rows T-LAY-09/T-MD-10/T-CMD-08, roadmap T1b, CLAUDE.md commands | docs and plans | done (see Results) |

## Acceptance

Each of the 20 criteria in design section 10 has a named test that passes. `go test ./internal/tui ./cmd/tui`, `go vet ./...`, golangci-lint and `python3 e2e/tui/run.py` pass.

## Risk and rollback

- Risk: medium. The alt-screen return in an inline program is untested (step 0 checks it first). Commits that wait while the view is open must keep their order.
- Rollback: the slice touches only `internal/tui` and its tests. Revert the slice commits; the protocol and the leader do not change.

## Results

Verified on 2026-10-12 in this worktree:

- `go test ./internal/tui` passes (it holds the named tests for criteria 1 to 14 and 16 to 20, in `blocks_preview_test.go`, `transcript_view_test.go` and `clipboard_test.go`).
- `go -C third_party/bubbletea test ./...` passes (the renderer rules and their tests in `replay_test.go`).
- `go test ./cmd/tui` and `go vet ./...` pass.
- `python3 e2e/tui/run.py --driver ask` passes on alacritty and ghostty, including `ask-transcript-view` and `ask-transcript-view-sigterm` (criterion 15 through the leader).
- The alternate-screen return spike passes on alacritty, ghostty, rio and tmux with the renderer fix ([report](../reports/spike-261012-t1b-altscreen-return.md)).
- Lint of `internal/tui` reports one gofmt finding in `internal/tui/clipboard_test.go` (line 36). The acceptance line needs a clean lint, so fix it before the slice is accepted.

Open items:

- Hand check on Terminal.app, iTerm2 and kitty (spike and the view). Not scriptable here.
- `maxDiffEdits` is 1000 in `internal/tui/blocks_diff.go`. A change that needs more edits shows every line between the common start and end as removed and then added (the diff is not minimal). Check that the limit fits real edits.
- At 40 columns the `Not copied` notice is cut with `…` before the reason (`TestCopyNoticeIsCutAtNarrowWidth` pins this). The user cannot read the reason there.
- The OSC 52 cap is 75 KB (`osc52Limit` in `internal/tui/clipboard.go`). A larger reply is refused, not cut. The value follows Pi and was not checked against each terminal.
- No e2e case runs the view or `/copy` with the in-process agent; unit tests cover both paths through the same model.

### Review and final gate (2026-10-12)

- A code review found one redraw cost problem (transcript view redrew every block on each event), model-only decoration in real `bash` and `read` text (`exit: N`, spill notice, `N|` prefix, next-offset notice), an uncleaned spill path and a pending open that could take editor keys. All four are fixed with tests; fixtures are real producer output.
- `/copy` sends the OSC 52 text through `tea.SetClipboard` from the update loop, so the renderer serialises it with frames.
- Final gate: `go vet ./...`, golangci-lint (0 issues), `go test` for `internal/tui`, `cmd/tui`, `internal/tuiclient`, `pkg/...`, the fork tests, and `python3 e2e/tui/run.py` (alacritty and ghostty, including the transcript view and SIGTERM cases) all pass. `cmd/tui` background-job tests can time out when the machine is loaded; they pass alone.
- Known limit: blocks held while the view is open are not written when the program ends by a signal from inside the view (the inline view has the same backlog rule).
- E2E added and passing on alacritty and ghostty: `ask-copy` (OSC 52 route, one sequence, payload equals the reply) and `ask-transcript-view-in-process`.
- Open: by-hand alt-screen check on Terminal.app, iTerm2 and kitty.
