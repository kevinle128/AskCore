---
title: "Thinking and reply look: implementation plan"
status: in-progress
design: design.md (approved 2026-11-02)
---

# Thinking and reply look: implementation plan

The approved contract is [design.md](design.md): the general rules R1-R4, the thinking fold setting, style A markdown and the reply time.
Each phase is test first: write the failing golden or unit test from the design, then change the code, then refresh goldens only after a diff against the design sketch.

## Architecture rules for every phase

- One owner for each rule. A rule lives in one place and every block uses it; no block type gets its own copy.
  - R1 spacing: the pane layout owns the gap rows between blocks. Blocks stop adding their own leading or trailing blank rows.
  - R2 selection: one box overlay in the selection mark code (`blocks_mark.go`), drawn on the cached rows after layout. It replaces the accent gutter and the step shade.
  - R3 step mark and R4 open body: one shared step header and body helper in `blocks_step.go` that thinking, tool and hook steps call.
- Keep the import rules of `docs/ask-architecture-reference.md`. `internal/tui` reads no file: the TUI entry (`cmd/tui`) reads `thinkingFold` through `internal/settings` and passes it in `tui.Options`.
- Markdown style stays data: `markdownStyle` and a small post-processor in `markdown.go`, not string hacks spread in blocks.
- Keep the pane cache contract of `docs/tui-architecture.md`: a block draws for a width, a theme and a fold mode; selection never changes a row count.
- No plan or phase labels in code, test names or comments (repository rule).

## Phases

| Phase | Scope | Files it owns | Criteria |
|---|---|---|---|
| 1 | R1 spacing and R2 selection box | `internal/tui/pane.go`, `blocks.go` (spacing only), `blocks_mark.go`, their tests and goldens | 16, 17, 18, 19 (box part), 23 |
| 2 | R3 step mark and R4 open body; thinking body without rail, `thinking` role not italic | `internal/tui/blocks_step.go`, `blocks_preview.go`, `blocks_hook.go`, `theme.go` (italic off), tests and goldens | 1-4 (layout part), 19 (mark), 20, 21 |
| 3 | Thinking fold session mode, `ctrl+t` sets it, `thinkingFold` in `settings.json` | `internal/settings` (read the key), `internal/tui` options and fold state, `cmd/tui` wiring, tests | 1-10 |
| 4 | Style A markdown and the reply time | `internal/tui/markdown.go`, `markdown_code.go`, `theme.go` (roles `h1` `h2` `h3`), reply block time, tests and goldens | 11-15, 22 |
| 5 | Docs and end-to-end checks | `docs/tui-style-guide.md`, `internal/tui/README.md`, `internal/settings/README.md`, `docs/cli.md`, `CHANGELOG.md`, e2e runner | all, on the built `ask` |

Phases run one after the other because they share `internal/tui`.

## Validation for every phase

- `go test ./internal/tui ./cmd/tui ./internal/settings`
- `go vet ./...`
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`
- For goldens: `go test ./internal/tui -run '^TestName$' -update`, then read the diff against the design sketch before keeping it.

## Progress

- [x] Phase 1: accepted 2026-11-03. `go test ./internal/tui` passed 4 runs in a row (the failure on 2026-11-02 came from a run during the agent's own edits), `./cmd/tui` passes, lint 0 issues. A real Alacritty capture (`ask-turn`) shows the box and gaps as designed. The `ask-turn` e2e case still fails because it expects the old `┃` rail and the old step layout: update `e2e/tui/run.py` (lines 741-797) and `e2e/tui/test-assertions.py:178` at the end of Phase 2, when step marks and open bodies are final. The `\t` characters in the case's screen text come from the test reader, not from the TUI.
  - Details of the code: R1 lives in `pane.go` (`Pane.measure`, `Pane.endsUnit`); R2 in `blocks_mark.go` (`paintBox`, `boxEdgeRow`) and `pane.go` (`Pane.Frame`, `Pane.boxEdges`, `Pane.Reveal`). 59 goldens refreshed. The implementer added a 2-column right text margin (`textRightPad`, `textWidth` in `blocks.go`) so the box does not cover text; this agrees with design section 7.
  - Carried to Phase 2: step rows and bodies still reach the right edge (`stepRightPad = 1` and body widths in `blocks_step.go`, `blocks_hook.go`, `bodyWidth` in `blocks_preview.go`), so `✗ exit 1` and the spinner touch the box.
  - Decided by the user on 2026-11-03: a copy across blocks does not include the blank gap rows (the behavior today; `TestDragAcrossBlocksJoinsRowsWithLineBreaks` covers it). No code change.
- [x] Phase 2: accepted 2026-11-03. Checked by me: `go test ./internal/tui ./cmd/tui` pass, vet clean, lint 0 issues; real Alacritty capture shows `›`, one blank row, body at column 4, box and right margin. The agent's 5 e2e cases (ask-turn, ask-in-process, ask-idle, ask-selector, ask-resize) pass; e2e runner, `test-assertions.py` and the README row are updated. R3/R4 live in `blocks_step.go` (`stepDraw.lines`, `stepRow.render`, `stepBodyContext`) and serve thinking, tool and hook steps.
  - Decisions: (1) an open hook is header, description row, blank row, body (the description row is part of the header); (2) thinking stays italic in the no-color profile, because italic is the only cue left there (`TestNoColorFallbackReadable`); color profiles are not italic.
  - One assertion outside the phase files changed: `cmd/tui/interactive_flow_test.go` (`TestInteractiveThinkingToggle`), to the new layout.
  - Carried to Phase 5 (docs): stale "expand indicator" comments in `transcript.go:34`, `selection.go:19-20`, `block_list.go:16`, `mouse.go:148`; `internal/tui/README.md:40` and `docs/tui-style-guide.md` (about lines 35, 60, 106) still describe `⌄` and an accent mark; `GlyphRail` is used only in tests (remove it). A drag copy of an open step copies `›`, of a collapsed step `◆`.
- [x] Phase 3: accepted 2026-11-03. Checked by me: `go test ./internal/tui ./internal/settings ./cmd/tui ./internal/app` pass, vet clean, lint 0 issues, no leader left; 3 Alacritty e2e cases pass. The setting is read once in `cmd/tui/thinking_fold.go` (`thinkingFoldSetting`, 500 ms bound, any error gives expanded with no notice) through the typed getter `SettingsStore.ThinkingFold` in `internal/settings/settings.go`, and passed in `tui.Options.ThinkingFold`. The session mode lives in `Model.thinkingFold` (`internal/tui/model.go`), reaches panes through `Pane.SetThinkingFold`, and blocks read it through `foldDefaults` (no global state). `ctrl+t` and `ctrl+e` set it (`Model.toggleAllThinking`). One end-to-end test runs the real `ask` with `"thinkingFold":"collapsed"` (`TestInteractiveThinkingFoldSettingCollapsed`).
  - Accepted side effect: the getter uses the same locked path as `DefaultModel`, so a TUI start creates `ASK_HOME` (mode 0700) and `settings.json.lock` when they are missing. The leader already did this.
  - Notes: `Pane.DefaultFold` is used only by a test helper (remove later); tests of the default mode must use `sessionModel` or `startSessionFold`, because the compact rigs fix collapsed; criterion 10 is covered by a snapshot resync in process, not by a real quit and restart; one new untracked golden `TestBlockGoldens/thinking_expanded.golden` needs `git add` at commit time.
- [x] Phase 4: accepted 2026-11-03. Checked by me: `go test ./internal/tui ./cmd/tui` pass, vet clean, lint 0 issues, no leader left. Style A markdown and the reply time are in. glamour v2.0.1 cannot draw the design's table box (full-width table, no outer box), so the agent wrote a generic renderer `table_box.go` (`textTable.draw`) that knows nothing about markdown or the reply block, plus `markdown_table.go` (finds GFM tables, hands the rest to glamour). Reusable helpers: `textTable.draw` (called by `proseOrTableRows`), `rowTimeRoom` and `withRowTime` in `row_time.go` (called by the prompt band and the reply block), `muteListNumbers` (`markdown_list.go`). Style rules stay data in `markdownStyle` (`headingRoles`, `mark*` constants). New roles `h1`, `h2`, `h3` in `theme.go`.
  - Decided by the user on 2026-11-03: keep the blank row under a heading (as in the Grok screenshots). The approved mockup and sketch M1 did not show it: update the mockup and M1 to match.
  - Decided by me: the reply time shows only when the first row is a text row; a reply that starts with a table or a code block shows no time (as the code panel already does). To do in Phase 5.
  - Known limits: inline code in a table cell is plain; glamour breaks words at hyphens, also inside code spans; a reply restored from an open stream after a reconnect shows no time; the prompt band gap before the time is 2 columns (was 1).
  - e2e: `ask-idle` and `ask-resize` pass; `ask-turn` and `ask-in-process` fail only at `e2e/tui/run.py:815`, which expects a reply row without the time. Fix in Phase 5.
  - Outside the phase files: `cmd/tui/interactive_flow_test.go:314` (the reply row now ends with the time).
- [x] Phase 5: accepted 2026-11-03. Checked by me: `go test ./...` passes for every package, vet clean, lint 0 issues, no leader left; all 12 e2e cases pass on Alacritty (artifacts in the ignored `e2e/tui/artifacts`). Done: removed `GlyphRail` and `Pane.DefaultFold`; fixed stale rail and indicator comments; reply time only on a text first row (`mdRow.textRow`); `run.py` and its README row; docs (`tui-style-guide.md`, `tui-architecture.md`, `internal/tui/README.md`, `internal/settings/README.md`, `docs/cli.md`, `CHANGELOG.md`); `design.md` and `mockup.html` rebuilt with the blank row under headings.
  - Fixed 2026-11-03: a first chunk that starts with a table was drawn twice for each change while it streamed. `renderFirstRoom` (`markdown.go`) now chooses the width from the source with `startsWithTable` (`markdown_table.go`, next to `findTable`) and draws once. Test: `TestFirstChunkIsDrawnOnceForEachChange` (it failed before the fix).
  - Frame budget check 2026-11-03: `TestPaneFrameBudget2000Blocks` failed once (median 17.8 ms against 16 ms) when `./internal/tui` and `./cmd/tui` ran at the same time; alone it passed 3 of 3 with 6.7-6.9 ms for the resize case, against 6.3 ms in the spike report. No regression; the failure comes from CPU load of the parallel `cmd/tui` run. Not changed.
  - Known limit: `test-assertions.py` must run without `-I` (it imports its sibling `run`).
  - Not committed. Untracked files need `git add` at commit time (new `.go` and `_test.go` files, new goldens, the plan directory).
