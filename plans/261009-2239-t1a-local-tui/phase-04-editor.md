---
phase: 4
title: "Editor"
status: completed
priority: P1
effort: "1.5d"
dependencies: [3]
---

# Phase 4: Editor

Outline. Run a scout pass and expand this file before cooking. Reuse the grapheme cursor and paste handling from the T0 archive (`plans/261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/editor.go`) where it fits.

## Goal

The editor slot from design sections 3 and 6: rules above and below, `> ` marker, two-space continuation, growth to 30% then inner scroll, and the editor keys.

## Scope

- Draft text, grapheme cursor, word movement, kill ring, undo, prompt history (inventory T-EDT-01, -02, -05; kill ring and undo pulled from P1 by the approved design).
- New line on shift+enter (when reported) and ctrl+j; enter and alt+enter return submit intents to the root (send, steer, follow-up decided by run state in phase 6).
- Paste: one insertion, CRLF to LF; more than 10 lines shows `[paste #N, L lines]` and sends the full text (T-EDT-08, -09).
- Slash completion list above the editor for `/model`, `/new`, `/sessions`, `/take`, `/quit` (design S4).
- Draft revision number for "clear only after acceptance".
- Hardware cursor placement for IME (T-REN-08).

## Tests first

| Test | Proves |
|---|---|
| `TestEditorKeysTable` | Each key in design section 6 that the editor owns has the stated effect |
| `TestEditorNewlineKeys` | ctrl+j always, shift+enter when reported; alt+enter never inserts a new line |
| `TestPasteMarkerExpandsOnSubmit` | Marker shown, full text submitted, undo restores |
| `TestCompletionFiltersAndRuns` | `/` opens, typing filters, esc closes, enter runs |
| `TestEditorGrowsThenScrolls` (goldens S5) | Height cap and inner scroll |
| `TestDraftRevisionChangesOnEdit` | Revision increments on every edit |

## Scout result

- Render core gives the editor `Slots.Editor` (`EditorSlot{Rows, Cursor, Modal}`), `Slots.Menu` for the list and `BoxInnerWidth(width)` as the row limit. Layout scrolls the inner rows around `Cursor.Row`, so the editor returns all rows.
- The T0 editor kept byte offsets at grapheme boundaries with `uniseg`; the same model is used. Its 3-row cap and char wrap are replaced by word wrap and the layout cap.
- Bubble Tea reports ctrl+j as `ctrl+j`, ctrl+- on legacy terminals as `ctrl+_`, and bracketed paste as `tea.PasteMsg`. The editor accepts both ctrl+- spellings.
- The root model does not route keys yet (`update_input.go` is a scaffold). The editor is one object with `Handle(msg) Result`; the session view calls it, fills `Slots` from `Slot` and `MenuRows`, and acts on `Result`. Tests use a small wrapper model with the same routing in a real program.
- Decisions: history keeps the text with paste markers; the completion list matches command names by prefix; restoring queued input joins the items and the draft with single line feeds.

## Files

`editor.go` (state, edits, `Accept`), `editor_keys.go` (`Handle`), `editor_layout.go` (word wrap, cursor, `Slot`), `editor_history.go` (undo, kill ring, prompt history), `editor_paste.go` (paste, markers), `completion.go` (list state, `MenuRows`), `editor_test.go`, `testdata/TestEditorGrowsThenScrolls/*`, `testdata/TestCompletionListGolden.golden`.

## Done

- [x] Tests first: all tests of the table above, plus history, wrap, width and hardware cursor tests
- [x] Draft, grapheme cursor, word movement, kill ring, undo, prompt history
- [x] New line on shift+enter and ctrl+j; enter and alt+enter give submit intents
- [x] Paste: one insertion, CRLF to LF, marker above 10 lines, expansion on submit
- [x] Slash completion list for the five commands
- [x] Draft revision with clear-after-acceptance (`Editor.Accept`)
- [x] Hardware cursor position for the editor row
- [x] Command names are case-insensitive (user decision): `TestEditorCommandNameCaseInsensitive`; only the whole-draft command name is folded, text with arguments stays a prompt with its case
- [x] Completion edge cases pinned: uppercase query lists the row, partial draft runs the highlighted row, multi-line draft closes the list, trailing space still runs the command

## Verification

`go test ./internal/tui/...`
