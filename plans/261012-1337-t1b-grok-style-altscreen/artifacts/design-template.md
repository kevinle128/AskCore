---
title: "Grok-style alternate-screen TUI design"
design-status: approved
design-approved-by: "user (dale)"
design-approved-on: "2026-10-12"
---

# Grok-style alternate-screen TUI design

Source: the port report [xia-261012-1238-grok-style-display-port.md](../reports/xia-261012-1238-grok-style-display-port.md) (Grok anatomy, dependency and decision matrices, user decisions) and the [mockup](../reports/xia-261012-1238-grok-style-altscreen-mockup.html) that the user reviewed.
History: style C (Claude Code transcript view) was chosen and implemented in [t1b-tool-output](../261012-0226-t1b-tool-output/plan.md), then replaced by this style ([comparison](../reports/xia-261012-0211-t1b-tool-output-copy-compare.md), "Decision change").

## 1. Outcome

The interactive `ask` TUI runs on the alternate screen. The screen has four parts: a header row, a scrollable block pane, a prompt pane and a shortcut bar. Ask draws and scrolls all of it.
The session is compact by default. Each thinking, tool and hook step is one row (`◆ Ran seq 1 40`, `⚓ PreToolUse hook (project/lint-guard) · edit parser.go`); each block opens on demand in three fold modes (Collapsed, Truncated, Expanded) or in a full-screen viewer.
The user moves between the prompt and the block list with `Tab`, folds with `e`/`E` and `→`/`←` (`l`/`h`), opens with `Enter` or a click, scrolls with the wheel, and copies text by mouse drag. A marker at the bottom of the block pane jumps back to the end.
On exit the terminal is restored, and one line `ask: session <id>` stays in the terminal history.

**Deliberate change of the screen model (user decision, 2026-10-12).** The [ask-tui-design skill](../../.claude/skills/ask-tui-design/SKILL.md), [docs/tui-architecture.md](../../docs/tui-architecture.md) ("Inline component composition", "Inline transcript") and [docs/tui-style-guide.md](../../docs/tui-style-guide.md) (principle 5 "Inline first") say that Ask is inline and that nothing draws a frame around the whole terminal. This design replaces that model for the interactive TUI. It moves D15 (alt screen, planned optional in T2) into T1b as the default, and it reverses the inline part of the T0 gate. The skill self-check item "sketches follow the inline model" is therefore not met, on purpose. The docs are not changed in this design; section 12 lists them as follow-up items.

## 2. Non-goals

- `ask -p`, `ask acp`, `ask connect` and the dashboard do not change. `ask -p` does not import `internal/tui` (only `cmd/tui/interactive.go` and `cmd/tui/tui_client.go` do), so it keeps its own plain output.
- No second interactive inline mode (Grok `--minimal`). Open question 7.
- No hook runtime. Hooks are H11 (`internal/hooks` is a scaffold). This design defines the hook row and the data that H11 must send (section 3.4).
- No `--resume` (H8). The exit line shows only the session id until H8 ships.
- Mouse: no links, no scrollbar, no hover effects, no double or triple click selection, no `[Copy]` button on blocks.
- No change to `/copy`, its clipboard order or its notices.
- No new tool argument in this slice. A model-supplied description of a tool call is open question 1.
- No light theme.

## 3. Placement

### 3.1 Screen

| Part | Rows | Owner (`internal/tui/README.md` names) | Replaces |
|---|---|---|---|
| Header row | Row 1, then a `─` rule | header (new; takes the footer data) | Startup header block in scrollback and the footer row |
| Block pane | All rows that the other parts do not use | transcript (blocks), new block list (selection, fold, scroll) | Native scrollback and the transcript tail of the live region |
| Bottom marker | Last row of the block pane, centered, bold, no background, only when follow is off: `▼ End` or `▼ <N> new lines · End` | block list | Nothing |
| Status row | One row above the prompt rule, only when the rail has text | status | The live rail (`┊ ◌ Working…`, notices, jobs) |
| Queue rows | Above the status row, only when inputs wait | pending | Same rows of the live region |
| Prompt pane | Between two `─` rules; 1 row, grows with the draft up to half the screen height; the editor scrolls inside | editor | The rounded editor box |
| Selector, box, completion | Take the prompt pane slot (up to half the screen), keep the draft | selector, dialog, completion | Same as today, other slot geometry |
| Shortcut bar | Last row | footer (new text) | Footer row |
| Viewer | Whole screen while open | viewer (adapted from `transcript_view.go`) | Transcript view of `ctrl+o` |

Empty slots use no rows. The header and the bar always stay.

### 3.2 Reuse and removal

| Part | Fate |
|---|---|
| Tool previews (`blocks_preview.go`: sizes per tool, `… +N lines (ctrl+o to expand)` hint) | Removed (user, 2026-10-12: "we no longer have that form"). Truncated uses the Grok form (section 3.3). The truncation line (`model saw …`, spill path) stays and shows in Expanded and in the viewer |
| Line diff and counts (`blocks_diff.go`) | Reused: `+N -M` on the Collapsed edit row, the unified diff in Expanded and in the viewer |
| Transcript view (`transcript_view.go`): alt-screen frame, scroll keys, follow and the new-output marker | Code reused for the one-block viewer and for the follow rule of the block pane. The whole-session view and `ctrl+o` are removed (open question 2) |
| `/copy` (`clipboard.go`) | Stays. Mouse-selection copy uses the same clipboard path and notices |
| Block renderers (`blocks.go`, markdown) | Reused for Expanded content; new Collapsed rows |
| Inline commit path (`render_inline.go`, `commands.go` `tea.Println`, commit frontier in `transcript.go`, resize replay) | Removed from interactive mode. No other consumer exists (`ask -p` does not use it) |
| Fork renderer rules for inline (`third_party/bubbletea`: `insertAbove`, `replayInline`, `ReplayInlineTranscript`) | No consumer after this change. Removal or keep is open question 8 |
| `ctrl+t` global thinking toggle | Changes meaning: all thinking blocks Collapsed ↔ Expanded (section 6) |

### 3.3 Fold modes

| Block | Default | Collapsed | Truncated | Expanded |
|---|---|---|---|---|
| User prompt | Expanded if ≤ 3 rows, else Truncated | (same as Truncated) | First 3 rows and `… +N lines · e to expand` on the band | All rows |
| Assistant reply | Expanded | Not foldable | Not foldable | Markdown, then the meta line |
| Thinking | Truncated while it streams, Collapsed when it ends | `◆ Thought for <d>` | Title, `┃ …`, the last lines (2 while streaming), as in the mockup | Title, all lines on a `┃` rail |
| Tool | Collapsed | `◆ <Verb> <what>` and the right column | First 3 output lines, `⋮ <N> more lines · e to expand · Enter full screen`, last 3 lines; 6 lines or fewer: all, no hint. No command row | `$ <command>`, all output (wrapped), diff for edit/write, truncation line |
| Hook | Collapsed | Hook row and the description row | Plus the output in the same first 3 / `⋮` / last 3 form | Plus the detail fields (section 3.4) and all output |
| Notes, job lines, model lines | One row | Not foldable | | |

`e` cycles Collapsed → Truncated → Expanded → Collapsed. `→`/`l` sets the selected block straight to Expanded (no change if it is not foldable or already Expanded); `←`/`h` sets it to its collapse mode (Collapsed; a running block goes to its running collapse mode: a running tool is one row, streaming thinking is Truncated). Grok: `actions/defaults.rs:258-283`, `scrollback/state/selection.rs:224-248`.

**Expand indicator.** When a foldable block is selected or under the mouse pointer and is at its minimum fold, its first-row glyph (`◆` or `⚓`) becomes `›` (U+203A, 1 column; `⚓` is 2 columns, so one space follows `›` to keep the row width). Expanded and selected or hovered: `⌄` (U+2304), open question 13. Truncated: the glyph does not change. A block that is not foldable (reply, short user prompt, note) keeps its row. Grok: `scrollback/scrollback_pane.rs:1137-1192`, `xai-grok-pager-render/src/glyphs.rs:255-265`. The `$ <command>` row and the truncation line show only in Expanded and in the viewer. A Truncated block with 6 output lines or fewer shows all lines and no hint. The hint counts lines of the result text: N = all lines − 6.

### 3.4 Row text

Tool rows: `◆ <Verb> <what>`. The verb is bold. Ask has no model description today: the bash tool takes only `command`, `timeout` and `run_in_background` (`internal/tools/shell_bash.go:78-82`), and no other built-in tool has a description argument. So `<what>` uses the fallback below. When a description exists (open question 1), it replaces the fallback and the command moves to the `$ <command>` body row.

| Tool | Done | Running | `<what>` fallback |
|---|---|---|---|
| `bash` | `Ran` | `Running` | The command, first line |
| `read` | `Read` | `Reading` | Path (`1 file` style counts when one row groups many reads: not in this slice) |
| `edit` | `Edited` | `Editing` | Path, then `  +N -M` |
| `write` | `Wrote` | `Writing` | Path, then `  +N` |
| `grep` | `Searched` | `Searching` | `"<pattern>"` and path |
| `find` | `Found` | `Finding` | Pattern |
| `ls` | `Listed` | `Listing` | Path |
| `job_output`, `job_kill`, `job_list`, `monitor` | `Checked job`, `Stopped job`, `Listed jobs`, `Watched` | `-ing` form | Job id |
| Other (MCP, `echo`) | The tool name | The tool name | First string argument |

Right column: nothing on success; `✗ exit <n>` or `✗ <reason>` on failure (and `◆` in the error role); a spinner and seconds while it runs; `⊘ cancelled` when cancelled.

Thinking: `◆ Thought for <d>` (thinking role glyph). While streaming: `◆ Thinking… <d>` with a spinner.

User prompt: the existing `panel` band, `❯ ` and the text, send time right.

Assistant reply: no glyph (the `◆` now marks steps); text indented two columns; the meta line in `dim`.

Hook row (option a): `⚓ <Event> hook (<scope>/<name>) · <target>`, result right (`✓ allowed 0.1s`, `✗ blocked 0.0s`), then a `muted` description row. Scope is `global` (user home config) or `project` (repo config). Without a description, the second row shows the command.
Viewer and Expanded detail fields, in order: Event (enum name only, for example `PreToolUse`), Matcher (tools and file globs), Does (description), Source (file and scope), Command and timeout, Result (decision, exit code, time), Reason (only when blocked), then `── output ──` and the output.

**Protocol need list for H11** (a hook run must reach the TUI as an event with these fields):

| Field | Example | Used in |
|---|---|---|
| Hook run id, and the tool call id it belongs to | `hk-12`, `call-7` | Order: the hook row goes before its tool row |
| Event enum | `PreToolUse` | Row, detail |
| Name and scope (`global` or `project`) | `no-rm`, `global` | Row, detail |
| Description (new config field; may be empty) | `Blocks recursive removal…` | Second row, detail |
| Matcher (tools, file globs) | `bash`; `edit, write · *.go` | Detail |
| Source file | `~/.ask/hooks.json` | Detail |
| Command and timeout | `ask-hook-no-rm`, `5s` | Detail; second row fallback |
| Target (tool and short args) | `bash rm -rf build` | Row |
| State: running, done | | Spinner, result |
| Decision (allowed, blocked, error, timeout), exit code, duration | `blocked`, `2`, `0.0s` | Row result, detail |
| Reason | `recursive removal is not allowed…` | Detail |
| Output (stdout and stderr, capped as tool output) | | Truncated, Expanded, viewer |

### 3.5 Exit line

`cmd/tui` prints the line after `Program.Run` returns and the renderer has left the alternate screen (`tea.Println` cannot write into the main screen from the alt screen). Text: `ask: session <id>`. After H8: `ask: session <id> · <n> turns · resume: ask --resume <id>`. It prints on every exit path: `/quit`, `ctrl+d`, `ctrl+c` twice, SIGINT (130), SIGTERM (143), SIGHUP (129) and a fatal client error (the line then comes before the error text). It does not print when no session was created (start error).

### 3.6 Bubble Tea hooks

- `tea.View.AltScreen = true` for the whole program (`third_party/bubbletea/tea.go:161`). The renderer close leaves the alternate screen on every end path (T1b result, `TestTranscriptViewStopSignalRestoresTheMainScreen`).
- `tea.View.MouseMode = MouseModeAllMotion` (`tea.go:177`): wheel, click, drag, release, and motion without a button for the hover indicator.

## 4. Sketches

Every sketch is the whole alternate screen, except X1, which is the main screen after exit. At 120×40 the layout is the same with more rows in the block pane, so 120×40 is not drawn; section 7 gives the rules. The 40×14 cases show the short header, bar and cut rows.
The sketches are generated by [artifacts/sketch-gen.py](artifacts/sketch-gen.py). To change the design, edit [artifacts/design-template.md](artifacts/design-template.md) or the generator, then run `python3 artifacts/make-design.py` from the plan directory; it rebuilds this file, checks every sketch width and writes [artifacts/preview.html](artifacts/preview.html).
The cursor `▌` is in the prompt pane when it has focus. A selected Collapsed step (tool, thinking, hook) has the `selected` background on its rows (shown in the preview, not in plain text). Any other selected block shows a `┃` rail in column 1 on every row (visible in plain text).

{{SKETCHES}}

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| Start | `ask` on a terminal | Alternate screen, mouse on, empty block pane, prompt focus |
| Prompt focus (A1) | `Tab`, or a click in the block pane | Block list focus; the last block is selected (click: the clicked block) |
| Block list focus (A2) | `Tab`, `Esc`, a click in the prompt pane, or any printable key other than list keys | Prompt focus; a printable key is not typed (open question 5) |
| Block selected | `e` | Fold mode of that block cycles (A3, A4); the selected row stays in view |
| Block selected, foldable | `→` / `l` | Expanded (A4); no change if already Expanded |
| Block selected, foldable | `←` / `h` | Collapse mode (Collapsed, or the running collapse mode) |
| Foldable block at minimum fold | Selected, or mouse pointer over it | First-row glyph becomes `›` (A2b); back to `◆`/`⚓` when not selected and not hovered |
| Foldable block Expanded | Selected or hovered | First-row glyph `⌄` (open question 13) |
| Block selected | Fold changes from Collapsed to Truncated or Expanded (or back) | Mark changes between the `selected` background and the `┃` gutter rail |
| Block list focus | `E` | If any foldable block is Collapsed: all Expanded; else all Collapsed |
| Any focus | `ctrl+t` (and `ctrl+e` in list focus) | All thinking blocks: if any is Collapsed, all Expanded; else all Collapsed |
| Block selected (tool, thinking, hook) | `Enter`, or a click on its first row | Viewer (V1–V3) at the top of the block |
| Block selected (user, reply, note) | `Enter` | Nothing |
| Viewer | `Esc`, or a click on `← back` | The same list position and selection |
| Viewer | Event changes the block (streaming output) | Viewer follows the end if it is at the end; else it stays |
| Viewer | `ctrl+c` | Viewer closes; then the ctrl+c rule (clear draft, quit confirm, quit) |
| Turn streams (A5) | Thinking delta | Thinking block Truncated; at its end Collapsed (unless the user set a mode for it) |
| Tool runs (A5) | `tool_execution_start` / `_update` | Running row with spinner; Truncated or Expanded blocks show live output |
| Tool ends | `tool_execution_end` ok / error | Row `◆ Ran …` / `◆ Ran …  ✗ exit 1` |
| Hook ends | H11 event | Hook row with `✓ allowed` or `✗ blocked` |
| Follow on (pane at bottom) | New output | Pane stays at the bottom |
| Follow on | Wheel up, `k`/`↑` above the window, `PgUp`, `Home` | Follow off; the window stays; marker `▼ End` (S2) |
| Follow off | New output | Window does not move; marker becomes `▼ <N> new lines · End` (S1); N = pane rows below the window, and it grows with new output |
| Follow off | Click on the marker, `End` or `G` (list focus), `ctrl+End` (any focus), or scroll to the last row | Pane at the bottom, follow on, marker gone |
| Mouse drag in the block pane (M1) | Press, move, release | Range shows `selected` background; on release the plain text goes to the clipboard; notice `✓ Copied` or the `/copy` failure texts; the highlight stays until the next click or key |
| Drag at the pane edge | Pointer on the first or last pane row | Pane scrolls one row each 50 ms; follow off |
| Prompt pane (P1) | Draft grows | Pane grows up to half the screen; then the editor scrolls inside |
| Selector or box open | `/model`, `/providers`, `ctrl+l` | Takes the prompt pane slot; block pane keys and mouse do not work until it closes |
| Resize | Window size change | Draw again at the new size; the selected block stays in view; follow keeps its state |
| Any | Program ends (any path in 3.5) | Main screen restored; exit line (X1) |
| Leader lost / reconnect | Client event | Same notes as today, as one-row blocks; the header dot changes |

## 6. Keys and focus

Input order, as in the architecture doc: exit and cancel first, then the active dialog, then completion, then the focused component (viewer, editor or block list), then application shortcuts.

| Key | Receiver | Action |
|---|---|---|
| `ctrl+c` | Root | Viewer open: close it first. Then the existing rule (clear draft, confirm, quit) in any focus |
| `ctrl+d` | Root / editor | Unchanged |
| Keys of a selector, box or completion | That component | Unchanged |
| `Esc` | Viewer | Close the viewer |
| `↑` `↓` `j` `k`, `PgUp` `PgDn` `space` `b`, `Home` `End` `g` `G` | Viewer | Scroll (reused from `transcript_view.go`) |
| All editor keys (`ctrl+e` end of line, `ctrl+a`, `enter`, `alt+up`, …) | Editor (prompt focus) | Unchanged. `ctrl+e` stays end of line here |
| `Esc` | Editor (prompt focus), after the editor | Interrupt the run (unchanged). Interrupt needs prompt focus |
| `Tab` | Prompt focus (free today: no editor or shortcut uses it) | Focus the block list |
| `Tab`, `Esc` | Block list | Focus the prompt |
| `j` `k` `↓` `↑` | Block list | Select the next or previous block; scroll so that it is in view |
| `e` | Block list | Cycle the fold mode of the selected block |
| `→` `l` | Block list (in prompt focus the editor keeps `←` `→`) | Expand the selected block to Expanded |
| `←` `h` | Block list | Collapse the selected block to its collapse mode |
| `E` | Block list | Expand all or collapse all (section 5) |
| `ctrl+e` | Block list | All thinking blocks (same as `ctrl+t`) |
| `Enter` | Block list | Open the viewer for a tool, thinking or hook block |
| `PgUp` `PgDn` | Block list | Scroll the pane one page; selection moves into view |
| `Home` `g` / `End` `G` | Block list | First block / last block and follow on |
| `ctrl+End` | Application shortcut (any focus) | Jump to the bottom, follow on (open question 4) |
| `ctrl+t` | Application shortcut (any focus) | All thinking blocks Collapsed ↔ Expanded |
| `shift+tab`, `ctrl+l`, `alt+up` | Application shortcut | Unchanged |
| `ctrl+o` | | Removed (free) |
| Wheel | Block pane or viewer under the pointer | Scroll 3 rows; up turns follow off |
| Click in block pane | Block list | Select the block and focus the list. On the first row of a tool, thinking or hook block: open the viewer |
| Click on the bottom marker (`▼ End`, `▼ <N> new lines · End`) | Block list | Jump to the bottom, follow on |
| Click on `← back` | Viewer | Close the viewer |
| Click in the prompt pane | Editor | Focus the prompt; place the cursor at the cell (open question 6) |
| Drag, release | Block pane, viewer | Select text; copy on release |
| Pointer motion | Block pane | Hover: the expand indicator on the block under the pointer (needs `MouseModeAllMotion`, see 3.6) |

## 7. Limited space and resize

- The header row, the bar and one prompt row always stay. Under 8 rows the rules go first, then the status row; the block pane keeps at least one row.
- The prompt pane takes at most half the height (rounded down). The selector and box slot uses the same limit and scrolls its list inside.
- The block pane is virtual: it draws only the blocks that meet the window. Each block keeps its row count for the current width and fold mode; a resize or a fold change recounts only that block or, after a resize, all blocks lazily.
- Header: under 60 columns `◢◤ ask ● <id>` left and `<short model> <pct>%` right; the cost, the bar of 8 cells and the cwd go first.
- Bar: under 60 columns the short forms in the sketches.
- Tool, thinking and hook rows: the description (`<what>`) is cut with `…` first; the verb, the diffstat and the right column stay. Under 30 columns the right column goes, then the diffstat.
- The hook row cuts the target first, then `(<scope>/<name>)`.
- Expanded and viewer text wraps; nothing is cut there. Truncated rows are one row each, cut with `…`. The hint under 60 columns is `⋮ <N> more lines · e`.
- Bottom marker: centered on the last pane row (left pad = (width − marker width) / 2, rounded down). Under 30 columns the new-output form is `▼ <N> · End`, also centered; `▼ End` does not change.
- After a resize the selected block stays at the same pane row if it can; with follow on the pane stays at the bottom.

## 8. Text and width

- Widths in columns: CJK and emoji count 2; a cut never splits a wide character. The truncation marker is `…`.
- `⚓` is U+2693 without a variation selector and counts **2 columns** (East Asian Width W; `ansi.StringWidth` in `width.go` uses the same table). U+2693 U+FE0E is not used: terminals do not apply FE0E in the same way. The row reserves 2 columns for it. The style guide gets a row for `⚓` with this width rule.
- Tabs expand to 4; control characters show the existing marker; ANSI sequences are removed (T1b rules).
- The bash command in a row: the first line only; a multi-line command ends with ` …`.
- Paths: shown as the tool got them; cut at the end with `…` in rows; full in the viewer.
- Mouse copy: the plain text of the selected cells, row by row, trailing spaces removed, rows joined with a line break; a wrapped line is copied as one line when the selection covers its wrap point. Styles and glyph padding are not copied.
- User prompt fold counts wrapped rows at the current width (`… +N lines`).
- Exit line: plain text, no color.

## 9. Style intent

- Header: logo `logo`; dot `success`/`error`/`warning`/`muted` as the old footer; details `muted`; model `assistant` bold; context bar `accent`.
- Rules (`─`): `border`.
- Step glyph `◆`: `tool` for a finished tool, `warning` while it runs, `error` on failure, `thinking` for thinking. Verb bold `fg`; description `fg`.
- Hook glyph `⚓`: new role `hook` (proposed; the preview uses a teal near the mockup). Result `success` / `error`. Description row `muted`.
- Detail field names `muted`, values `fg`, reason `error`, `── output ──` `dim`.
- Selected block, Collapsed tool, thinking or hook: `selected` background on its whole rows (the header row; for a hook also the description row); reverse with no color.
- Selected block, any other (assistant reply, user prompt, Truncated or Expanded step): no background; `┃` in `accent` in the gutter (column 1) on every row of the block, wrapped rows, all paragraphs and the usage row included. Unselected blocks keep the gutter blank; the column is always reserved, so nothing reflows. Reason (Grok `scrollback/scrollback_pane.rs:959-1000`, `scrollback/wrappers/entry_renderer.rs:690-737`): a background patch would clobber line styles such as diff colors and the prompt band.
- The `›`/`⌄` indicator rules apply in both cases.
- Expand indicator `›` and `⌄`: `accent` bold, in place of the step glyph.
- Drag range: `selected` background.
- Thinking rail `┃` and text: `thinking`.
- Bottom marker: bold, no background. `▼ End` in `tool`; `▼ <N> new lines · End` in `accent`. The style guide gets a glyph row for `▼` (jump to the bottom).
- Viewer title row: `← back` in `accent` bold, title bold, result in its status role; key row `dim`.
- Bar: `dim`. Status row: the existing rail roles.
- Readable without color: a step row starts with a glyph and a bold verb; a reply has no glyph; the selected row is reverse; the failure has `✗`.

## 10. Acceptance criteria

1. At 80×24, after start, row 1 shows `◢◤ ask  ● leader` and the model name, the last row shows `Tab blocks · ctrl+t thinking`, and the terminal is on the alternate screen (`ESC[?1049h` in the output).
1. At 80×24, after a turn with thinking, `bash seq 1 40`, `read parser.go` and an edit, the block pane shows `◆ Thought for`, `◆ Ran seq 1 40`, `◆ Read parser.go` and `◆ Edited parser.go  +3 -1`, each on one row, and no output line `40`.
1. At 80×24, after a bash call that exits 1, its row shows `✗ exit 1` at the right edge.
1. At 80×24, while a bash call runs, its row shows `◆ Running` and a spinner, and the status row shows `Working…`.
1. At 80×24, while thinking streams, the pane shows `◆ Thinking…` and `┃` rows; after the turn ends, the block is one row `◆ Thought for <d>`.
1. At 80×24, after `Tab`, the bar shows `j/k select · e fold` and the last block (the reply) is selected; the prompt shows no cursor.
1. At 80×24, in list focus on the failed bash block (9 output lines), after `e`, the pane shows `--- FAIL: TestCount (0.00s)` as the first body row, then 2 more lines, `⋮ 3 more lines · e to expand · Enter full screen`, and the last 3 lines ending `FAIL`; no row shows `$ go test ./...`; the bar shows `e fold: truncated`.
1. At 80×24, after `e` on a tool block with 5 output lines, all 5 lines show and no `⋮` row shows.
1. At 80×24, after a second `e` on the failed bash block, the first body row is `$ go test ./...` and all output lines follow.
1. At 80×24, after `e` again on the edit block, the pane shows the diff rows `  10 - ` and `  10 + `; after a third `e` the block is one row.
1. At 80×24, in list focus on the collapsed failed bash block, after `→`, the bar shows `e fold: expanded` and the pane shows `$ go test ./...`; after `←` the block is one row and the bar shows `e fold: collapsed`. `l` and `h` do the same.
1. At 80×24, in list focus on the reply, after `→`, the screen does not change.
1. At 80×24, in prompt focus with the draft `abc`, after `←`, the cursor moves left in the draft and no block changes.
1. At 80×24, after `k` selects the reply (from list focus on a block below it, or `Tab` then `G`), every row of the reply, including the usage row, shows `┃` in the gutter and no row is shaded.
1. At 80×24, with the collapsed failed bash block selected, its row has the `selected` background and no `┃`; after `→` the background is gone and every row of the block shows `┃` in the gutter.
1. At 80×24, with the collapsed `no-rm` hook selected, both its rows (header and description) have the `selected` background.
1. At 80×24, after `Tab` with the failed bash block selected and Collapsed, its first row starts with `  › Ran go test ./...`; the reply rows and the user band keep their text.
1. At 80×24, with the mouse pointer over the collapsed lint-guard hook row (not selected), that row starts with `  ›  PreToolUse hook`; after the pointer leaves, it starts with `  ⚓ PreToolUse hook`.
1. At 80×24, in list focus, after `E`, every tool, thinking and hook block is Expanded; after `E` again all are one row.
1. At 80×24, after `ctrl+t` in prompt focus with a draft, every thinking block expands and the draft is unchanged; in prompt focus `ctrl+e` moves the cursor to the end of the line.
1. At 80×24, after a prompt that wraps to 6 rows, the band shows 3 rows and `… +3 lines · e to expand`.
1. At 80×24, in list focus on `◆ Ran seq 1 40`, after `Enter`, row 1 shows `← back  Ran seq 1 40`, the screen shows `$ seq 1 40` and the numbers `1` to `18`; after `Esc` the list is as before with the same selection.
1. At 80×24, a mouse click on the first row of `◆ Ran seq 1 40` opens the same viewer; a click on `← back` returns.
1. At 80×24, with a hook event (H11 fixture) that blocked `rm -rf build`, the pane shows `⚓ PreToolUse hook (global/no-rm) · bash rm -rf build` with `✗ blocked 0.0s` right and `Blocks recursive removal (rm -rf) inside the repo` under it; the `⚓` row is exactly 80 columns.
1. At 80×24, in the viewer of that hook, the screen shows the rows `Event    PreToolUse`, `Matcher  tool bash`, `Source   ~/.ask/hooks.json (global)`, `Command  ask-hook-no-rm · timeout 5s`, `Result   blocked · exit 2 · 0.0s`, `Reason   recursive removal…` and `── output ──`.
1. At 80×24, with a hook entry without a description, the second hook row shows the command.
1. At 80×24, after one wheel-up in the block pane, the last pane row shows `▼ End` centered; after a new tool event it shows `▼ <N> new lines · End` centered, with N equal to the pane rows below the window, and the top pane row does not change.
1. At 28×14, scrolled up after new output, the last pane row shows `▼ <N> · End` centered.
1. At 80×24, with the marker shown, after a click on it (or `End` in list focus, or `ctrl+End` in prompt focus), the pane shows the last block, the marker is gone, and a new event keeps the pane at the bottom.
1. At 80×24, after a mouse drag over `directory. TestCount still fails` and release, the clipboard path receives that text and the status row shows `┊ ✓ Copied` (or the `/copy` failure texts).
1. At 80×24, after a click in the prompt pane in list focus, the cursor `▌` is in the prompt pane and typed text goes to the draft.
1. At 80×24, with a 12-line draft, the prompt pane has 12 rows and the block pane 7 rows; a 13th line scrolls inside the prompt pane.
1. At 80×24, after `/model`, the selector is in the prompt pane slot; after `Esc` the draft is unchanged.
1. With the built `ask`, after `/quit`, `ctrl+c` twice, SIGTERM and SIGHUP (one run each), the terminal is on the main screen with the cursor visible, mouse reporting off, and the last output line is `ask: session <id>`.
1. At 40×14, every row of A1, A3, A5, A6, V1, V3 and S1 is 40 columns and the header shows `◢◤ ask ● 7c1e`.
1. At 120×40, after a resize from 80×24 with a block selected, the selected block is still in view and the bar is on row 40.
1. `/copy` after a reply gives the same result as before this change (T1b criteria 16–20).
1. In a session with 2000 blocks at 80×24, a wheel scroll draws the next frame in under 16 ms (performance spike, open question 3).

## 11. Open questions

All decided (user, 2026-10-12): every recommendation below is accepted.

1. **Tool description from the model.** The user's rows (`Ran print numbers 1 through 40`) need a description; Ask has none. Options: (a) add an optional `description` argument to `bash` (as Claude Code does) and later to other tools; (b) keep the command fallback only; (c) derive a text locally. Recommendation: (a) for `bash` only in this slice's follow-up, as a tool-schema change outside the TUI; the fallback stays for other tools and for old sessions.
2. **The whole-transcript view (`ctrl+o`).** Keep it as `/transcript`, or remove it? Recommendation: remove. `E` plus the scrollable pane shows the whole session; the viewer reuses the code for one block. `ctrl+o` becomes free.
3. **Performance on long sessions.** Recommendation: a spike before implementation: 2000 blocks, draw only the visible blocks, cache row counts per width and fold mode, measure frame time on wheel scroll and on a streaming delta.
4. **Key for "jump to bottom" from the prompt.** `ctrl+End` (some terminals do not send it; macOS Terminal.app needs a setting), `shift+G` (cannot work in the editor: it types `G`), or `alt+End`/`ctrl+g`. Recommendation: `ctrl+End` with `End`/`G` in list focus and the click as the always-working way.
5. **A printable key in list focus.** Should it focus the prompt and type the key (Grok-like convenience), or be ignored? Recommendation: focus the prompt and type the key, except list keys (`j k e E g G b`) and `space`.
6. **Terminal-native selection fallback.** Mouse capture stops the terminal's own selection. Most terminals bypass capture with `shift`+drag (iTerm2, kitty, Ghostty, Alacritty, Windows Terminal) or `option`+drag (Terminal.app, iTerm2). Recommendation: keep the app drag-copy, document the bypass in the bar help and the README; no `/mouse off` toggle in this slice.
7. **Inline mode for the interactive TUI.** Keep a `--inline` mode (Grok `--minimal`)? Recommendation: no; the cost of two screen models is high. Revisit if users ask.
8. **Inline renderer patch in the fork.** `insertAbove`/`replayInline` and `ReplayInlineTranscript` lose their only consumer. Remove them (smaller fork) or keep them for a later inline mode? Recommendation: remove with the inline commit path, and update `PATCH.txt` and `renderer_patch_test.go`.
9. **Selection mark.** The mockup marks the selected block with `▌` in column 1; the skill reserves `▌` for the cursor. Decided (user, 2026-10-12): the `selected` background for a Collapsed step, a `┃` gutter rail for any other block; no `▌`.
10. **Hook role color.** A new `hook` role is needed in the style guide. Recommendation: a teal close to the mockup, different from `tool` in the 256-color map.
11. **Exit line when the session has no turn.** Recommendation: still print `ask: session <id>`, so that the user can find it.
12. **Previous and next turn keys.** Grok has `shift+←`/`shift+→` (`H`/`L`) to jump to the previous or next user turn (`actions/defaults.rs:103-127`). Recommendation: yes, in list focus.
13. **Indicator for an Expanded block.** Grok shows `⌄` only for an expanded verb group on hover. Recommendation: show `⌄` on any selected or hovered Expanded foldable block (as drawn in A4).
14. **Hook indicator.** Replace `⚓` with `›` (as drawn) or keep `⚓` and add `›` before it. Recommendation: replace, so that all foldable rows behave the same.

## 12. Follow-up: documents to change after approval

These documents state the inline model. They change in the implementation plan, not in this design:

- `docs/tui-architecture.md`: "Decision and status", "Inline component composition", "Inline transcript" (commit frontier, scrollback rebuild), the `ctrl+o` exception, "Validation" (T0 inline decision).
- `docs/tui-style-guide.md`: principle 5 "Inline first"; glyphs `◆` (now a step mark, not a reply mark), `∴` and `●`/`◇` (no longer used in rows), new `⚓` (2 columns), `┃`, `▼`, `›` (expand indicator) and `⌄` (expanded indicator); new role `hook`; patterns for header row, rules, prompt pane, shortcut bar, step rows, hook rows, fold hints, bottom marker, viewer, selection.
- `.claude/skills/ask-tui-design/SKILL.md`: the "Ask is an inline TUI" paragraph, the live-region sketch rule, the self-check item and the example.
- `internal/tui/README.md`: status, state and rendering rules, render core contract (commit path), the `ctrl+o` contract text, component owners (block list, viewer, header).
- Roadmap: D15 (alt screen moves from T2 to T1b, default), the T0 gate note (inline decision reversed for the interactive TUI), T1b rows T-LAY-09.
- `CLAUDE.md` commands section (`ask` TUI description) and `e2e/tui/README.md` scenarios.
