# Port: Grok display style into Ask (alt screen)

## Source

- Grok: `~/Desktop/workspace/opensources/grok-build` at `559751fd`, crate `crates/codegen/xai-grok-pager` (TUI) and `xai-grok-hooks`.
- Local: Ask, branch `new-session`, `internal/tui`.
- Decision (user, 2026-10-12): Grok style, compact by default, every block expands on demand, alt screen. See the decision change in [the earlier comparison](xia-261012-0211-t1b-tool-output-copy-compare.md).
- Mockup: [xia-261012-1238-grok-style-altscreen-mockup.html](xia-261012-1238-grok-style-altscreen-mockup.html).

## How Grok shows each element

| Element | Grok | Evidence |
|---|---|---|
| Screen | Alt screen; header, scrollback pane, prompt pane (1 row up to half the height), shortcut bar. A `--minimal` inline mode exists, and `/transcript` opens a pager. Nothing is printed to the terminal history on exit | `app/mod.rs:1534`, `app/agent_view/render.rs:562` |
| Focus and keys | `Tab` moves focus between prompt and scrollback; `j`/`k` select a block; `h`/`l` collapse/expand; `e` cycles the fold; `E` all blocks; `ctrl+e` all thinking; `Enter` opens the selected block in a full-screen viewer; `r` raw markdown | `actions/defaults.rs` |
| Fold model | Three modes per block: Collapsed, Truncated, Expanded | `scrollback/types.rs:63`, `block.rs:40-116` |
| User message | `❯ text`; folds when longer than 3 rows | `blocks/user.rs:450-514` |
| Assistant message | Markdown, no prefix, always expanded | `blocks/agent.rs` |
| Thinking | `◆ title (ctrl+e to expand)` when collapsed; title, `…` and the last lines when truncated; full markdown with a `┃` rail when expanded. Truncated while it streams, collapsed when it ends | `blocks/thinking.rs:284-560` |
| Tool call | `●` header line (`$ cmd`, `Read <path>`, `Edit <path>`, `Searched "<p>"`), spinner, then `✓`/`✗`. Collapsed by default. Truncated = first and last lines with a `⋮` hint; edit shows a diffstat when collapsed | `blocks/tool/*.rs`, `execute.rs:621-668` |
| Hooks | Muted rows without a glyph: `Turn blocked by hook in 0.3s.`, a hook annotation, and a deny/timeout outcome row under the tool row. Not foldable | `blocks/session_event.rs:65-156, 706-816`; events in `xai-grok-hooks/src/event.rs:78-180` |

## Dependency matrix

| Grok part | Ask today | Status |
|---|---|---|
| Alt-screen frame, own scroll | Inline renderer, commit to native scrollback (`render_inline.go`); patched Bubble Tea fork already supports `View.AltScreen` (transcript view) | CONFLICT: replace the screen model |
| Block list with selection and fold modes | Transcript blocks without selection; ctrl+t thinking global; ctrl+o transcript view | NEW (selection, per-block mode); reuse block renderers |
| Full-screen block viewer (`Enter`) | Transcript view (`transcript_view.go`) shows the whole session | EXISTS, adapt to one block |
| Tool previews, diffstat, truncation line | Done in `blocks_format.go`, `blocks_diff.go` | EXISTS, reuse as the Truncated mode |
| Thinking fold | `ctrl+t` toggle | EXISTS, change to per block plus `ctrl+e` |
| Hook rows | No hooks yet (H11 not started) | NEW, design the row now, data comes with H11 |
| Copy | `/copy` done; mouse selection is lost in alt screen | EXISTS; selection copy is a follow-up |

## Decision matrix

| Decision | Grok's way | Ask's way | Recommendation |
|---|---|---|---|
| Screen model | Alt screen by default, `--minimal` inline | Inline | Alt screen by default (user). Keep inline for `ask -p` only; no second interactive inline mode in this slice |
| History after exit | Lost; `/transcript` pager | Native scrollback | Print a short summary line on exit (session id and `ask --resume`) so the user can find it again |
| Keys | vim-like `j/k/h/l/e/E`, `Tab` focus | Editor-first | Grok keys only when the block list has focus; the editor keeps all its keys |
| Default fold | Tools and thinking collapsed, messages expanded | Previews shown | Grok defaults |
| Mouse | Captured (wheel, selection) | None | Wheel scroll yes; text selection with shift (terminal bypass); no app selection in this slice |
| Transcript view (style C code) | `/transcript` pager | `ctrl+o` view | Keep as `/transcript`; `ctrl+o` is free again |

## Risk

High. The inline screen model was the T0 gate decision; this replaces the renderer path, the commit and replay logic, the e2e golden screens and the docs (`docs/tui-architecture.md`, style guide). Much of the T1a and T1b inline work (commit frontier, replay on resize) is removed or kept only for `ask -p`. Recommend a separate design and plan, with a spike for frame cost on long sessions (virtualized drawing of only the visible blocks).

## Unresolved questions

1. Exit summary line: yes or no?
2. Keep `ctrl+o` transcript view as `/transcript`, or remove it?
3. Mouse capture: wheel only, or also in-app text selection (bigger work)?

## Mouse (Grok)

Grok captures the mouse in the alt screen (`app/mouse.rs`, `app/agent_view/selection.rs`). The main actions:

| Action | Effect | Evidence |
|---|---|---|
| Wheel | Scrolls the scrollback pane, with acceleration | `app/event_loop.rs` ~5387, `agent_view` `handle_scroll` |
| Scrollbar click or drag | Jumps or drags the scroll position | `app/mouse.rs` ~370, ~1205 |
| Click a block | Selects it | `app/mouse.rs` ~820-867 |
| Double or triple click | Selects a word, or a paragraph or table cell | `app/mouse.rs` ~824-833 |
| Drag over text | Highlights the text and copies it on release, with a toast | `selection.rs` ~870-900 |
| Drag from a block header | Selects whole blocks and copies them; scrolls at the edge | `selection.rs` ~1000-1072 |
| `[Copy]` / `[View]` on a block | Copies the block, or opens the full-screen viewer | `app/mouse.rs` ~389-393 |
| Ctrl/Cmd + click a link | Opens the URL or file | `app/mouse.rs` ~403, ~799 |
| Click in the prompt | Focuses the prompt and places the cursor | `app/mouse.rs` ~540-567 |
| Click the "follow" / "jump to response" marks | Goes to the bottom or the previous reply | `app/mouse.rs` ~247-256 |
| Hover | Highlights buttons and rows | `app/mouse.rs` ~928-1186 |
| Middle click | Has handling in `event_loop.rs` ~3174 (the subagent report missed it; not checked) | |

Proposed Ask subset for this slice: wheel scroll, click to select a block, click on the fold hint or header to fold, drag to select text with copy on release, `[Copy]` on a block, click in the prompt to focus. Out of this slice: link open, scrollbar, hover effects, dock and queue buttons (Ask has no such panes).

## Hook display decision (user, 2026-10-12)

H1: each hook run is a row in the tool row pattern, foldable, with the anchor glyph `⚓` and its own role; the status column shows `✓ allowed` or `✗ blocked: <reason>`; the folded body shows the hook output. Grok's muted text rows are rejected as hard to read. The style guide needs the new glyph and role. `⚓` (U+2693) has East Asian Width "W" in some terminals; the design must set its width (use U+2693 U+FE0E text form, or count it as two columns).

## Copy decision (user, 2026-10-12)

Copy is by mouse selection only: the selected text goes to the clipboard on release. No `[Copy]` button on blocks. `/copy` stays for the last reply.

## Click decision (user, 2026-10-12)

A click on the header row of a tool, thinking or hook block opens that block in the full-screen viewer, the same as `Enter`. `← back` (click) or `Esc` returns. Fold cycling stays on the keys `e` and `E`.

## Hook detail (user, 2026-10-12)

The user needs to know which hook ran and what it does.
- Collapsed: `⚓ <name> · <event enum, e.g. PreToolUse> · <target>` with the result on the right, then a muted second row with the hook description.
- Viewer and expanded body: event (the enum name only, e.g. `PreToolUse`; no translation), matcher (tools and file globs), description, source file and scope (project or user), command and timeout, result (decision, exit code, time), reason when blocked, then the hook output.
- Contract for H11: a hook config entry needs a `description` field; when it is missing, the row shows the command instead.

## Tool row and hook row decisions (user, 2026-10-12, from a Grok screenshot)

- Tool rows follow Grok: `◆ <bold verb> <model description>` (`Ran print numbers 1 through 40`, `Read 1 file`, `Edited parser.go +3 -1`); the command and output appear only when the block opens; `◆` turns to the error role on failure. Thinking: `◆ Thought for 0.5s`. The user prompt sits on a shaded band with `❯`.
- Hook rows, option (a): `⚓ <Event> hook (<scope>/<name>) · <target>` with the result on the right, a muted description row, and the detail viewer on click. Scope is `global` (user home config) or `project` (repo config), as in Grok `xai-grok-hooks/src/discovery.rs:164-224`. Data arrives with H11.

## Exit line decision (user, 2026-10-12)

Yes: after the alt screen closes, print one line, `ask: session <id> · <n> turns · resume: ask --resume <id>`. Until H8 ships `--resume`, the line shows only the session id.

## Scroll to bottom decision (user, 2026-10-12)

When the block pane is not at the bottom, a clickable marker sits on the last row of the block pane: `↓ new output · End` (accent) when new content arrived, `↓ bottom · End` (muted) otherwise. Grok has a similar follow indicator (`app/mouse.rs` ~247-256, `hit_follow_indicator`). A click on the marker, or `End`/`G` in block list focus, jumps to the bottom and turns follow mode on again (new output keeps the view at the bottom). Scrolling up turns follow off. A key that works from the prompt is an open question; recommendation `ctrl+End`. Design: [plans/261012-1337-t1b-grok-style-altscreen/design.md](../261012-1337-t1b-grok-style-altscreen/design.md) (states S1, S2).

Truncated form decision (user, 2026-10-12): Grok first 3 / ⋮ hint / last 3; the T1b preview form is dropped.

Marker position decision (user, 2026-10-12): centered on the last pane row.

Marker style decision (user, 2026-10-12): option 4, `▼ End` / `▼ <N> new lines · End`, centered, bold, no background.

## Arrow keys and expand indicator decision (user, 2026-10-12)

Port from Grok at `559751fd`:
- Keys, block list focus only (in prompt focus the editor keeps the arrows): `→` or `l` expands the selected block straight to Expanded (`actions/defaults.rs:271-283`, ActionId::Expand with alt Right; `scrollback/state/selection.rs:240-248`, expand_selected sets DisplayMode::Expanded, no change if the block is not foldable or already Expanded). `←` or `h` collapses the selected block to its collapse mode (`defaults.rs:258-270`; `selection.rs:224-237`; a running block goes to its running collapse mode). `e` stays the cycle key, `E` all.
- Grok also has `shift+←`/`shift+→` (`H`/`L`) for the previous or next turn (`defaults.rs:103-127`). Open question; recommendation yes.
- Expand indicator: a foldable block that is selected or under the mouse pointer and at its minimum fold (Collapsed, or running at its collapse mode) shows `›` (U+203A, 1 column) in place of `◆` on its first row (`scrollback/scrollback_pane.rs:1137-1192` paint_expandable_indicator; `xai-grok-pager-render/src/glyphs.rs:255-265` chevron). A block that is not foldable keeps its row. Expanded: back to `◆`; Grok shows `⌄` only for an expanded verb group on hover. Recommendation (open question): `⌄` (U+2304) on a selected or hovered Expanded block. Hooks: `⚓` becomes `›` the same way (recommendation; the other option is `›` before `⚓`).
- Design: [plans/261012-1337-t1b-grok-style-altscreen/design.md](../261012-1337-t1b-grok-style-altscreen/design.md), sketches A2b and A4, open questions 12-14. Hover needs `MouseModeAllMotion`.

## Selection highlight decision (user, 2026-10-12)

The first prototype shaded only the first paragraph of a selected reply. New rule: a selected Collapsed tool, thinking or hook block gets the `selected` background on its rows; any other selected block (reply, user prompt, Truncated or Expanded step) gets no background and a `┃` rail in the `accent` role on every row, usage row included. Grok paints the selection background only on a Collapsed ToolCall or Thinking entry and leaves markdown and prompts unpatched (`scrollback/scrollback_pane.rs:959-1000`). The rail for other selected blocks is an Ask choice: in Grok the `┃` rail marks open blocks (`scrollback/wrappers/entry_renderer.rs:690-737`), and Grok's reply block has no accent.
