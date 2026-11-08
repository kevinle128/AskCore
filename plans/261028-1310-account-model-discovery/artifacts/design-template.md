---
title: "Footer status row and account model discovery TUI design"
design-status: approved
design-approved-by: "dale"
design-approved-on: "2026-10-31"
---

# Footer status row and account model discovery TUI design

This design has two parts:

- **Part A, layout.** The status data (working directory, model, thinking level, context use) moves from the header row at the top to a footer row above the shortcut bar. The header row stays as a slim row with only the logo `◢◤ ask` (D15, user 2026-10-29). The footer row shows no link state and no session cost (D9, D14). These are user decisions of 2026-10-28 and 2026-10-29; a footer existed in the inline layout before commit `5b6a16a` (2026-10-12, old footer: `git show 94a70d8:internal/tui/footer.go`).
- **Model line removed.** The block pane shows no model line: no start line and no change line. The footer row always names the model, so the pane does not repeat it (user decision, 2026-10-28, D12).
- **Part B, account model lists.** The new states of phase 6 of the [account model discovery plan](plan.md) (user decision U3, plan decisions P8 and P13): `?` for an unknown context window, the note `not in account list`, and a new fallback reason.

This file is generated: edit `artifacts/sketch-gen.py` and `artifacts/design-template.md`, then run `python3 artifacts/make-design.py` from the plan directory.
The script writes this file and `artifacts/preview.html`, and checks the width and the height of every sketch.
Review the [preview](artifacts/preview.html) first; this file is the written contract.

Parts that this design does not change copy the current code and goldens: the text of the old header row (`headerText`, now `footerText` in `internal/tui/footer.go`), the selector (`internal/tui/selector.go`), the fallback reason words (`internal/tui/model_line.go`, `fallbackReasonText`), the prompt block, the status row, the queue rows and the shortcut bar.

## 1. Outcome

A slim header row at the top holds only the logo `◢◤ ask`, with a rule under it. Under the prompt pane, one footer row shows the directory, the model as `<provider>/<model>` with its thinking level, and the context use; it shows no logo, no link state and no session cost. The shortcut bar stays the last row with the same text and keys.
`/model` can list models that match a family rule of Ask but have no metadata; such a row shows `?` in the window column, and when the session model has an unknown window the footer shows `?` in place of the bar and the percent.
When the account list no longer has the running model, `/model` still shows its row with the tag `current` and a muted note `not in account list` (under 60 columns, the tag `unlisted` in place of both); a saved default that is not in the account list falls back at session start with a note that gives this reason.

## 2. Non-goals

- New keys, or a change to the shortcut bar text. Both stay as they are.
- A change to the viewer. It takes the whole screen and shows no footer row, as it shows no header row today.
- A change to the content of the block pane, the status row, the queue rows, the prompt pane or the selector boxes. Only their position on the screen changes.
- The updates of goldens, end-to-end checks and documents. Section 3 lists them for the implementation; this design does not change them.
- A cost column in `/model`, and a refresh key in `/model` (plan "Out of scope").
- The text output of `ask models refresh`. It is CLI output, not TUI.
- A notice or a rail state when a refresh removes the running model from the account list. There is no push event (P8): the TUI reads the list each time `/model` opens.
- The output limit, the prices and the thinking words of a model.
- A session cost in the footer row. The footer row shows no cost in any form or at any width (user decision, 2026-10-28). Thus the "incomplete cost" marker and the usage answer field for it (`costUnknown`) are no longer needed; `ACPModel.CostUnknown` stays a catalog fact that no footer reads.
- The reply meta line. It keeps its cost part (`gpt-5.4-mini · 4.2s · ↑12.4k ↓1.8k · $0.040`); `metaText` already omits the cost when it is 0, so a reply on a model with an unknown price shows no `$0.000`. Decision D6 keeps it.
- The `$0.00` cost of Anthropic sessions (plan "Out of scope").
- Headless `ask -p`, `ask acp`, the leader, the web dashboard and a light theme.

## 3. Placement

### 3.1 Screen order (Part A, decided form)

| Before this design, top to bottom | This design, top to bottom |
|---|---|
| header row (logo, link, directory, model, context, cost) | header row (logo only) |
| rule | rule (D16) |
| block pane | block pane (starts on row 3) |
| queue rows | queue rows (only when input waits) |
| status row | status row (only when the rail has text) |
| rule | rule |
| prompt pane | prompt pane, or a selector or box in its slot |
| rule | rule |
| shortcut bar | **footer row** (directory, model, context) |
| | shortcut bar (last row) |

- The header row stays, with only the logo, and the rule under it stays (D16). The footer row comes between the lower rule of the prompt pane and the shortcut bar, with no rule between the footer row and the bar. The block pane has one row less than before this design at every height where the footer row fits: at 80×24 it has 17 rows (before: 18).
- **Header row** (D15). ` ◢◤ ask` with one space of padding at the left, as the other rows, then spaces to the full width. It has no link state, no directory and no model. It shows in every state, also with no model and when the link is lost.
- **Footer row text** (D13, D14). ` ⌂ <dir>  <provider>/<model> ◐ <level>  ▰▱▱▱▱▱▱▱ <pct>% `, with one space of padding at the left, for example ` ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4% `. No logo, no link state in any state (no `●`, `◌`, `○`, `◉`, `leader`, `starting`, `lost`, `in-process` or `observer`), no session cost (D9) and no wire format. The short form under 60 columns has no directory: `<provider>/<model> <pct>% ` at the right. The unknown window form replaces the bar and the percent with `?` (D5).
- **No model line in the block pane** (D12). Today `model_line.go` puts a start line `  model  <provider>/<model>` at the top of a session, under a divider and under a restore note, and a change line `  model  <new> · was <old>` when the model changes. Both go. What stays:
  - The divider (`session <id>`) and the restore note (`↻ …`) stay as they are, with no model line under them.
  - The fallback note becomes its own notice block (see section 3.2). It shows where the start line showed it today: at the start of the transcript and under a divider, not under a restore note, and only to the driver.
  - The `TEA_TRACE` warning, today a row of the start line block, becomes its own notice block at the start of the transcript: `  TEA_TRACE is on: typed and pasted text, keys included, is written to <file>.` in `warning`, indented 2 columns and wrapped. Its text and its once-at-start rule do not change.
  - A model change shows in the footer row, and, for a choice in `/model`, in the status row (`✓ Default model: <provider>/<model>`, today). The reply meta line keeps the bare model id of each reply.
- **Logo.** `◢◤ ask` is the only content of the header row (D15). The style guide has the `logo` role only for this mark. The footer row has no logo.
- **Left padding.** One space on the header row and the footer row, so that the logo and `⌂` start in the column of the prompt `❯` and of the shortcut bar text. Decision D4.
- **Layout priority** (`layout.go`). The prompt pane keeps one row, the bar and the footer row stay, the block pane keeps one row, and then the header row. With the header rule, the three rules show from 9 rows (`rulesMinHeight` becomes 9, because the footer row adds one row to the 8 rows of today's minimum frame). Under 5 rows the header row gives way first, then the block pane, then the footer row.
- **Mouse.** The block pane starts on row 3 (`FrameGeo.PaneTop` is 2), as today. Clicks on the header row, the footer row and the bar do nothing.
- **Selector and boxes.** They keep the prompt pane slot between the two rules; the footer row and the bar stay under them (S1, S4).

### 3.2 Changed parts for account model lists (Part B)

| Screen part (owner) | What changes | Data it reads |
|---|---|---|
| footer row (`footer.go`) | The context meter is `?` when the row of the session model is known and its `ContextWindow` is 0. The cost segment goes. | `ACPModel.ContextWindow` of the current row |
| selector (`selector.go`), `/model` | The window column shows `?` for a `ContextWindow` of 0. The row of the current model gets the muted note ` · not in account list` after its id when `ACPModel.NotInAccount` is set; under 60 columns, or when the note does not fit, the tag `unlisted` replaces `current`. | `ACPModelsResult.Models` |
| block pane, fallback notice (`model_line.go` today) | One more fallback reason: `not in account list`. The note is its own notice block, not a row under a model line: `  Default model <provider>/<model> is not usable (<reason>).` in `muted`, indented 2 columns at every width, wrapped at spaces, then one empty row. It has no glyph: the style guide has no glyph for an information notice, and `↻`, `✗`, `⊘`, `⏏` mean retry, error, cancel and detach. | `ACPStateResult.ModelFallback.Reason` |

- The footer shows `?` only when the TUI has the row of the current model. Before the first list answer, or with a peer that does not send the current row, it keeps today's form (`▱▱▱▱▱▱▱▱ 0%`), so no `?` changes to a bar a moment later at session start.
- `NotInAccount` is only set on the row of the running model (P8). An older leader does not send it, so the TUI shows no note.

### 3.3 What else changes (blast radius for the implementation)

Code (`internal/tui`):

- `layout.go`: the header row returns as a logo-only row: keep `alloc.header` and `alloc.headerRule`, add the footer row between the lower prompt rule and the bar, `rulesMinHeight` becomes 9, and `composeFrame` draws the header, its rule, the pane, the queue, the status, the prompt with its rules, the footer and the bar. `FrameGeo.PaneTop` is 2 again.
- `footer.go` (`footerText`, `footerLink`, `cwdOptions`): the logo and `footerLink` leave the footer row; the logo moves to a new header row text. The directory never goes from 60 columns, and the narrowing order is the one of section 7. The [package README](../../internal/tui/README.md) and `docs/tui-style-guide.md` ("Header row" and "Footer row" patterns; the `●`, `◌`, `○` and `◉` rows of the glyph table lose the link meaning) change.
- Goldens: every session golden changes again: row 1 is ` ◢◤ ask` and row 2 a rule, as before the footer change, and the footer row above the bar loses the logo and the link part. The goldens of `TestFooterUsageThresholds`, `TestReconnectStatesGoldens`, `TestSessionsSelectorAttachesAsObserver` and the in-process goldens no longer show any link state.
- Tests that read rows by index or by `PaneTop`: `first_run_test.go`, `model_line_test.go`, `layout_test.go`, `altscreen_scenarios_test.go`, the mouse tests, and the other `*_test.go` files that read `content()[n]`.

Goldens (`internal/tui/testdata`): every session golden shows the header row on row 1 and the footer row on the row above the bar: 90 or more golden files in 52 or more tests, for example `TestScreenGoldens`, `TestBlockGoldens`, `TestBlockListScreens`, `TestScrollScreens`, `TestScreens120Goldens`, `TestJobNoticeGoldens`, `TestReconnectStatesGoldens`, `TestFooterUsageThresholds`, `TestFirstRunGoldens`, `TestFallbackNoticeNamesModelInUse`, the `/model` goldens and the provider, key, login and removal box goldens. Each refreshed golden needs a review against the sketches of this design.

Model line removal (D12):

- `internal/tui/model_line.go`: `modelLineBlock` (start and change lines), `startSlot`, `startBlockFor`, `settleStarts`, `onModelUpdate` (it draws the change line; it still updates the model of the view), `restoredNote` (it loses its start line), `pairText` and `splitPair` where only the line uses them. `fallbackReasonText` and the trace warning text stay and move to the new notice blocks. The [package README](../../internal/tui/README.md) owner row "Model line (start and change lines)" changes.
- Tests: `model_line_test.go`, `first_run_test.go`, `session_test.go`, `providers_flow_test.go`, `restore_missed_model_test.go` in `internal/tui`; `interactive_flow_test.go` (the change line `· was faux/faux-1` and the model line under the divider), `providers_flow_test.go` (the start line, and "no model line"), `providers_reconnect_test.go` in `cmd/tui`.
- Goldens that show a model line: 34 files with the `  model  ` label and their 40-column variants without the label, in `TestModelLineGoldens`, `TestModelChangeLine`, `TestFallbackNoticeNamesModelInUse`, `TestScreens120Goldens`, `TestReconnectStatesGoldens`, `TestJobNoticeGoldens`, `TestTraceWarningOnceAtStart`, `TestTraceWarningOnKeyBox`, `TestNoTraceWarningWhenUnset`, `TestSessionsSelectorAttachesAsObserver`, `TestSelectorChangesModel`, `TestSelectorModelAccessFailureKeepsModel`, `TestSelectorReadinessFailureKeepsModel`, `TestModelSelectorTagsDefault`, `TestUncheckedKeyRefusedAtFirstPrompt`, and the provider, key, login, device and removal box goldens. `TestModelLineGoldens` and `TestModelChangeLine` lose their subject; the fallback and trace notices get their own goldens.
- `e2e/tui`: `run.py` line 394 (`assert_shell` finds `model  faux/faux-1` to prove the block pane is there); `test-assertions.py` (`slot_frame` and the pane of the fold check start with `  model  faux/faux-1`); `real_provider_scenarios.py` (`providers_first_run.no_model_line` and `oauth_rows_and_switch.start_line`).
- Docs: `internal/tui/README.md` (Status line 10, the owner table, lines 124 and 125 of the session view contract); `docs/tui-architecture.md` ("Provider setup, secret input and the model line": the in-process agent sentence and the "Model line" paragraph); `docs/tui-style-guide.md` (add the fallback and trace notices to "Notes"). The T1c design (`plans/261010-1424-t1c-provider-setup-design/design.md`, "Model line", sketches P28 to P30) is superseded on this point.

Go end-to-end (`cmd/tui`):

- `requireLeaderFooter` (`cmd/tui/interactive_test.go`, called in `interactive_flow_test.go`, `interactive_jobs_test.go` and others): no row shows `● leader` or the leader id. It must check the leader through `leader.Status` and that the session did not fall back in process, by a source other than the screen. `◢◤ ask` is on row 1 again.
- `exit_line_test.go` and `providers_security_test.go`: comments that name the header row; check that no assertion uses row 1.

Terminal end-to-end (`e2e/tui`):

- `run.py`: `LEADER_LABEL` (`● leader [0-9a-f]+`) and `FOOTER_LONG` no longer match any row. `ask_ready` (it gates every case: `ask-idle`, `ask-turn`, `ask-resize`, `ask-selector`, `ask-providers`, `ask-mouse`, `ask-exit-*`, `ask-copy`), `ask_ready_in_process`, `assert_shell` and `assert_idle_screen` find `◢◤ ask` on row 1 again and the footer row (` ⌂ …  faux/faux-1 …`) on the row above the bar. The screen has no `in-process` word, so `ask_ready_in_process` and the leader cases must tell the two modes apart by another source (the leader socket, `ask leader status`, or the process list).
- `test-assertions.py`: `frame()` builds the header, its rule, the pane, the prompt with its rules, the footer row and the bar; the in-process and starting checks lose their screen text.
- `README.md`: the case table (`ask-idle`: row 1 is the logo-only header; the footer row is above the bar) and the notes about the header and the leader id (lines 56 and 88).
- `real_provider_scenarios.py`: every check that reads `leader [0-9a-f]+` (lines 106, 381, 448, 452, 462, 478, 504, 530) must read the footer row by `⌂` and the model instead, and the leader through another source.

Documents:

- `docs/tui-style-guide.md`: principle 5 (the header, the footer row and the bar always stay); the `logo` role (header row only); the `◌`, `●`, `○` and `◉` rows of the glyph table (no link meaning on the screen); the screen order at the start of "Patterns"; the "Header row" pattern (logo only) and a new "Footer row" pattern; the "Rules" pattern (three rules, from 9 rows). Also the Part B rows: `?` (unknown value) and the selector note.
- `docs/tui-architecture.md`: "Screen composition" (the order, and "The header, the shortcut bar and one prompt row always stay"); "Provider setup, secret input and the model line" ("The header row and the reply meta line keep the bare model id": the footer row now shows `<provider>/<model>`; the meta line keeps the bare id). The same earlier decision is in section 3 of `plans/261010-1424-t1c-provider-setup-design/design.md`.
- `internal/tui/README.md`: "Status", the owner table row "Header row and shortcut bar", and the session view contract (header row, `no model`).
- `docs/cli.md` ("The header shows `no model`": now the footer row) and `AGENTS.md` ("the header row then shows `in-process`": the screen no longer shows the in-process fallback; say where the user can see it).
- `.claude/skills/ask-tui-design/SKILL.md`: the screen model text, the placement list, the example sketch and the self-check item add the footer row above the bar.
- `CHANGELOG.md`: one line under `[Unreleased]`.
- The designs `plans/261012-1337-t1b-grok-style-altscreen/design.md` and `plans/261010-1424-t1c-provider-setup-design/design.md` stay as historical records; the style guide points to this design for the screen order.

## 4. Sketches

Every sketch is a whole screen at the stated size, except sketch F, which lists footer rows. The names of the screen parts are under "Regions" and at the left of each terminal in the preview.
`▌` is the cursor. The goldens show the hardware cursor, not `▌`. In the prompt states the cursor is at the start of the placeholder; in the selector states it is after `⌕ ` in the filter row.
The prompt pane has the focus, so no block is selected. Every reply is Expanded, every prompt has three rows or fewer, and the pane follows the end of the transcript, so its first rows can be the end of an earlier block.
The footer row ends with one space; the goldens trim it. The header row is the logo and spaces.

{{SKETCHES}}

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| Start, link not up | The TUI opens | Header ` ◢◤ ask`; footer with the directory and the meter only (sketch F, row 4); no link word |
| L1 idle | A prompt is sent; a tool runs; input is queued | L2: queue rows and the status row above the prompt rules; the footer row and the bar stay |
| L2 | A usage answer arrives (during the run or when it ends; today's refresh points) | The footer shows the new context use; L2 shows 6% after the first tool call |
| L1 or L2 | Resize to under 60 columns | The short footer form; the bar short form (today) |
| Any | Resize to 9 rows | Header, rule, pane, the status row when it has text, rule, prompt, rule, footer, bar |
| Any | Resize to 8 rows or less | No rules. Under 5 rows the header row gives way, then the pane, then the footer row |
| L1 | `Enter` or a click opens the viewer | The viewer on the whole screen, no footer row (today); `Esc` returns to L1 |
| L1 | `ctrl+l` or `/model` | The selector in the prompt slot; the footer row stays under it |
| Leader lost, observer, in-process | Link event | The header and footer rows do not change (D14); the status rail and the notes of today still report a lost link |
| No model | State event | The footer row shows the directory and `no model` (sketch F, row 5) |
| Idle, session on a model with a known window | `ctrl+l` or `/model`; the list has a row with window 0 | S1: that row shows `?` |
| S1 | `↑` `↓` `ctrl+p` `ctrl+n` | S1 with the cursor on the next row; the cursor skips provider headers (today) |
| S1 | `Enter` on `claude-opus-4-9` | The footer row names the new model and the status shows `✓ Default model: …`; no change line: S2 |
| S1 | `Enter` on the row of the current model | The selector closes; no change (today) |
| S1 | `Esc` | The selector closes; the draft stays; no change |
| `/model` waits for its list | The list answer belongs to an older request (`applyModelList` token) | The answer is dropped; the screen does not change (stale result) |
| `/model` waits for its list | The list call fails | The error inside the box (today); no `?` and no note |
| S2 | The user changes to a model with a known window | Footer with the bar and the percent |
| S2 | Resize under 60 columns | The short footer: `anthropic/claude-opus… ? ` |
| Idle, the running model left the account list | `ctrl+l` or `/model` | S4: the current row has the note; the footer does not change |
| S4 | Resize under 60 columns | The note goes and the tag `unlisted` replaces `current`; a resize back to 80 columns shows the note and `current` again |
| S4 | `↓` then `Enter` on another row | The footer row names the new model; the next `/model` has no note and no `unlisted` |
| S4 | `Enter` on the current row, or `Esc` | The selector closes; no change |
| Session start; the saved default is in the catalog but not in the account list | The state answer has a fallback with the new reason | S5 |
| S5 | The model changes | The notice stays in the transcript; the footer row names the new model |
| S5 for an observer client | Session start | No fallback note (today) |
| Any state with an older leader | The leader sends no new field | No note; a window of 0 still shows `?` |
| Any state with an older client | A new leader sends the new fallback reason | The older client shows `not available` (today) |

## 6. Keys and focus

This design adds no key and changes no receiver.
The input order of the architecture doc stays: exit and cancel first, then the viewer, then the active selector or box, then completion, then the editor or the block list, then the application shortcuts.

| Key | Receiver | Effect |
|---|---|---|
| `ctrl+c` | root (exit and cancel) | Today's behavior |
| `Esc` | viewer, else selector, else the run | Today's behavior |
| `↑` `↓` `ctrl+p` `ctrl+n` | selector | Moves the cursor over model rows. A row with `?` and the row with the note are normal rows |
| `Enter` | selector | Chooses the row. The current row closes the selector with no change |
| Printable keys, `backspace`, `ctrl+u` | selector | Edit the filter. The filter matches `provider/id`; the note text and `?` are not part of the filter |
| `ctrl+l`, `/model` | editor, then application shortcut | Opens `/model` (today) |
| `Tab`, `j`/`k`, `e`, `E`, `Enter` | editor or block list | Today's behavior; the shortcut bar text does not change |
| Mouse | root, from the frame geometry | The pane starts on the first row of the screen (`PaneTop` 0); clicks on the footer row and the bar do nothing |

The footer row and the notice blocks take no keys.

## 7. Limited space and resize

- **Rows.** The prompt pane keeps one row, the bar and the footer row stay, the block pane keeps one row, then the header row. The three rules show from 9 rows. Then the status row, the prompt pane up to half of the height, the queue rows and the completion rows take rows, and the pane takes the rest. Under 5 rows the header row gives way first, then the pane, then the footer row: at 3 rows the screen is the prompt row, the footer row and the bar.
- **Footer row, narrowing order** (D13). When the row is too wide, the parts give way in this order:
  1. The directory shortens to its last element (`~/src/askcore` to `~/…/askcore`). From 60 columns the directory never goes.
  2. The end of the model id is cut with `…`. The `<provider>/` part stays whole, and at least `…` of the id stays.
  3. Last resort, only in a very narrow terminal: the model text is cut from its end, provider included.
  The thinking level, the bar and the percent do not give way inside a form.
- **Footer row, from 60 columns.** With no logo (7 columns) and no link part, the row has 79 columns of room at 80 columns. With the long directory `~/src/github.com/kevinle128/askcore`, both `anthropic/claude-sonnet-4-6 ◐ high  ▰▰▰▰▰▰▰▱ 93%` and `alibaba-token-plan/deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%` fit whole next to `⌂ ~/…/askcore` (sketch F, rows 6 and 7; the generator asserts it). `~/src/askcore` stays whole in every other 80-column form.
- **Footer row, under 60 columns.** `<provider>/<model> <pct>% ` at the right: no directory, no thinking level, no cost. At 40 columns `anthropic/claude-sonnet-4-6 93%` fits whole; `alibaba-token-plan/deepseek-v4.1-flash` is cut to `alibaba-token-plan/deepseek-v4.1…` (step 2, the provider stays). The unknown form is `<provider>/<model> ? `.
- **Header row.** The logo is 7 columns with its padding and never gives way; the rest of the row is spaces.
- **Selector.** The window column stays 4 columns wide and `?` is right-aligned in it. The tag column stays 9 columns wide. For the row that is not in the account list: from 60 columns the note ` · not in account list` (22 columns) shows after the id when it fits. Under 60 columns, or when the note does not fit, the note goes as a whole and the tag `unlisted` (8 columns, fits the tag column) replaces `current`. The id is never cut for the note. At 80 columns the note fits; at 40 columns the row shows `unlisted` (S4). Neither form adds a row.
- **Fallback notice.** It is indented 2 columns at every width, wraps at spaces and is never cut (S5).
- **120×40.** The order does not change. The footer row keeps the full directory and the bar adds `shift/option+drag select` (L1 at 120×40). The other states have no 120×40 sketch.
- **Resize.** Every part is drawn from state at each frame. After a resize, the footer row is always the second-to-last row and the bar the last row; the `?` and the note stay, and only the width rules above apply.

## 8. Text and width

- Model ids and provider ids are ASCII. In the footer row the model text is `<provider>/<model>`; the cut removes the end of the model id and never the provider until the last-resort step of section 7. In the selector a long id is cut with `…` after the note has given way.
- The footer model text has no wire format suffix; two rows with the same provider and id on two formats show the same footer text. With no model line, the wire format shows only in `/model`.
- The directory is cut by path elements (`~/…/askcore`), never inside a name.
- `?`, `·`, `⌂` and `◢◤` are one column each in common monospace fonts; the footer row has no wide character.
- The note text and the fallback reason are fixed English text with no tab, no path and no wide character.
- The fallback notice has a room of the width minus 2. At 80 columns a note of up to 78 columns is one row; with `anthropic/claude-opus-4-7` it is 76 columns and fits. A longer pair wraps at spaces with the same 2-column indent.
- A word longer than the room breaks at the column limit, as `wrapLines` does today.
- The truncation marker is `…`.

## 9. Style intent

- Header row: `logo` for `◢◤ ask`; no other segment.
- Footer row: `muted` for `⌂` and the directory; `assistant` for the model and the thinking level; `accent`, `warning` or `error` for the bar and the percent. No logo, no link state, no cost segment, no background, and no rule between the footer row and the bar.
- Footer `?`: `muted`. It does not use `accent`, `warning` or `error`, because there is no percent to rate.
- Window column `?`: `muted`, as the rest of the right column; on the selected row, the `selected` background.
- Selector note ` · not in account list` and the tag `unlisted`: `muted`, as the other tags; on the selected row, the `selected` background.
- Fallback notice: `muted`, no glyph. `TEA_TRACE` notice: `warning` (today's role).
- Without color, the glyphs, `?` and the note text carry the meaning alone; the footer row and the bar differ by their text.

## 10. Acceptance criteria

1. At 80×24, idle after one reply, row 1 is ` ◢◤ ask` followed only by spaces, row 2 is a rule of 80 `─`, row 3 is the prompt band `  ❯ why does TestLeaderStale fail on CI?` with `14:02` at the right, and no row starts with `  model  `.
2. At 80×24, idle, rows 20 and 22 are rules of 80 `─`, row 23 (footer) is ` ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%`, and row 24 is ` Tab blocks · ctrl+t thinking · ctrl+l model · / commands`. No row shows `●`, `leader`, `7c1e` or `$` outside the meta line.
3. At 120×40, idle, row 1 is ` ◢◤ ask`, row 39 (footer) starts with ` ⌂ ~/src/askcore` and row 40 ends with `shift/option+drag select`.
4. At 40×14, idle, row 1 is ` ◢◤ ask`, row 13 is `                 openai/gpt-5.4-mini 4%` (right-aligned, no directory) and row 14 is ` Tab blocks · / commands`.
5. At 80×24, while a tool runs and a steer waits, rows 17 to 19 are `┆ ⧗ steer   use the fake clock from internal/testsupport`, the queue hint and `┊ ◌ Working… 4s · esc to interrupt`, and rows 23 and 24 are the footer row and the bar.
6. After a resize from 80×24 to 40×14 and back to 80×24, the footer row is row 13 and then row 23, and the bar is the last row.
7. At 80×24, with the viewer open, no row shows `◢◤ ask` and no row shows `⌂`.
8. At 80×9, with text on the rail, the rows are the header, a rule, one pane row, the status row, a rule, the prompt row, a rule, the footer row and the bar.
8a. At 80×8, no row is a rule, and the last two rows are the footer row and the bar.
9. At 80×3, the rows are the prompt row, the footer row and the bar.
10a. At 80 columns, with the directory `~/src/github.com/kevinle128/askcore` and `anthropic/claude-sonnet-4-6 ◐ high` at 93%, the footer row is ` ⌂ ~/…/askcore                 anthropic/claude-sonnet-4-6 ◐ high  ▰▰▰▰▰▰▰▱ 93%`: the directory is short and the model text is whole.
10b. At 80 columns, with the same directory and `alibaba-token-plan/deepseek-v4.1-flash` at 18%, the footer row is ` ⌂ ~/…/askcore             alibaba-token-plan/deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%`; at 40 columns it is `  alibaba-token-plan/deepseek-v4.1… 18%`.
10c. At 80×24, after the leader link is lost, in process, as an observer and before the link is up, the header row is ` ◢◤ ask` and the footer row shows none of `●`, `◌`, `○`, `◉`, `lost`, `starting`, `in-process`, `observer` or `leader`.
10. At 80×24, before the link is up, the footer row is ` ⌂ ~/src/askcore` and the meter ` ▱▱▱▱▱▱▱▱ 0%` at the right.
11. At 80×24, after `ctrl+l` with a list that has `anthropic/claude-opus-4-9` with context window 0, the selector shows the row `claude-opus-4-9` with `?` as the last character of its 4-column window column and the tag `reasoning`, and the footer row stays under the lower rule.
12. At 80×24, in the same selector, the rows with a known window show `1M` and `400k` as today, and no row of the box shows `$`.
13. At 40×14, after `ctrl+l` and `↑` `↑` from `gpt-5.4-mini`, the selector shows `│ ❯   claude-opus-4-9    ?  reasoning  │`.
14. At 80×24, after `Enter` on `claude-opus-4-9` and before any prompt, the footer row ends with `⌂ ~/src/askcore   anthropic/claude-opus-4-9 ◐ medium  ?` and shows no `▰`, no `▱` and no `$`.
15. At 40×14, in the same state, the footer row is `            anthropic/claude-opus-4-9 ?`.
22. At 80×24, before the first list answer, the footer row shows `▱▱▱▱▱▱▱▱ 0%` and not `?`.
23. At 80×24, after `ctrl+l` with a list where the current row `anthropic/claude-opus-4-7` has `notInAccount`, the selector shows `│ ❯   claude-opus-4-7 · not in account list` and, at the right of the same row, `1M  current`.
24. At 80×24, in the same state, the footer row still shows `anthropic/claude-opus-4-7 ◐ medium  ▰▱▱▱▱▱▱▱ 12%`.
25. At 40×14, in the same state, the selector shows `│ ❯   claude-opus-4-7   1M  unlisted   │`, and the screen has no `not in account list` and no `current`.
25a. At 80×24, after `Enter` on `claude-opus-4-9` in `/model`, the block pane has no row with `· was` and no row that starts with `  model  `.
26. At 80×24, after `↓` and `Enter` in that selector and then `ctrl+l` again, the screen has no `not in account list` and no `unlisted`.
27. At 80×24, at session start with a saved default `openai/gpt-5.4-mini` and the fallback reason "not in account list", block pane row 1 (screen row 3) is `  Default model openai/gpt-5.4-mini is not usable (not in account list).`, no row starts with `  model  `, and the footer row names `openai/gpt-5.5`.
28. At 40×14, in the same state, block pane rows 1 and 2 (screen rows 3 and 4) are `  Default model openai/gpt-5.4-mini is` and `  not usable (not in account list).`
29. At 80×24, at session start of an observer client with the same fallback, the block pane has no `Default model` row.

## 11. Open questions

None. Every question of this design is decided (see "Decisions" below). The design still needs the approval of the user.

### Decisions (user, 2026-10-28 to 2026-10-31)

These were open questions of this design; the user answered them on the dates shown.

| # | Question | Decision |
|---|---|---|
| D1 | Footer row above the shortcut bar, or one merged bottom row in place of the bar | A footer row above the bar; the bar text and keys do not change |
| D2 | Move all status data to the footer, or keep a slim header with the logo and link | All status data moves to the footer row; there is no header row (changed by D15) |
| D3 | Place of the logo | First segment of the footer row (changed by D15) |
| D4 | Left padding of the footer row | One space |
| D5 | Footer form for an unknown context window | A lone `muted` `?` in place of the bar and the percent |
| D6 | Also remove the cost from the reply meta line | No: the meta line keeps its cost |
| D7 | Words of the fallback reason | `not in account list` |
| D8 | Name of the design file | `design.md`; the lead points `phase-06-cli-and-tui.md` to it |
| D9 | Session cost in the footer row (decided before the questions above) | Removed from every footer form and width |
| D10 | Model text in the footer row (new requirement) | `<provider>/<model>`; the narrowing order of section 7 keeps the provider |
| D11 | The note `not in account list` under 60 columns | The tag `unlisted` replaces `current`; the note shows from 60 columns when it fits |
| D14 | Link state in the footer row (2026-10-29; the open question on it, option B) | No link state in any state: no glyph and no word for starting, lost, in-process or observer |
| D15 | Logo (2026-10-29) | A slim header row at the top holds only `◢◤ ask`; the footer row has no logo |
| D16 | Rule under the slim header row (2026-10-31) | Keep the `─` rule. At 80×24 the block pane has 17 rows; the three rules show from 9 rows (`rulesMinHeight` 9) |
| D13 | Leader part and directory in the footer row (2026-10-29) | No `●` dot, `leader` label or id in the normal connected state. The directory always shows from 60 columns (shortened, never removed). Narrowing: directory to its last element, then the end of the model id, then the last resort |
| D12 | Model line in the block pane | Removed: no start line and no change line. The footer row names the model. The fallback note stays as its own `muted` notice block; the `TEA_TRACE` warning becomes its own notice block |
