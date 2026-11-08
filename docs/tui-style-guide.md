# TUI style guide

This guide is the visual authority for the Ask terminal UI.
Every TUI design file (`plans/*/design.md`) and every component in `internal/tui` follows it.
Behavior and layout rules live in [tui-architecture.md](tui-architecture.md); this file covers only how things look.

Status: accepted direction **B4 "Soft + live rail"** (user, 2026-10-10) for colors, roles and prompt bands.
The screen model is the alternate screen of the Grok-style design (user, 2026-10-12).
Reference sketches and the interactive prototype: [design.md](../plans/261012-1337-t1b-grok-style-altscreen/design.md) and its [prototype](../plans/261012-1337-t1b-grok-style-altscreen/artifacts/interactive.html).
The block pane look (spacing, the selection box, step marks, open steps, reply markdown and the reply time) is the approved [thinking and reply look design](../plans/261102-1800-thinking-fold-setting/design.md) (user, 2026-11-02).
The T1a renderings in [preview.html](../plans/261009-2239-t1a-local-tui/artifacts/preview.html) and the [style directions](../plans/261009-2239-t1a-local-tui/artifacts/style-directions.html) show the earlier inline layout; use them for colors only.

## Principles

1. **Soft conversation.** Prompts sit on a quiet panel; replies are plain text with one accent mark. No boxes around messages, except the thin selection box and the box of a markdown table.
2. **The dashed rail means "not finished".** A dashed rail (`┊`, `┆`) appears only next to live things: work in progress and queued input. There is no solid rail: an open thinking body and a selected block have no rail.
3. **One accent, few colors.** Pink is the interactive accent, violet marks the assistant, teal marks tools. Other colors carry status only.
4. **Readable without color.** Every state must be clear with bold, dim and glyphs alone.
5. **One screen, rows by priority.** The TUI owns the alternate screen. The footer row and the shortcut bar always stay, the prompt keeps one row, the block pane takes what is left, and the header row shows from 5 rows. Rules (`─`) separate the parts; nothing draws a box around the whole screen. Only selectors and dialogs use boxes.
6. **One row first, one blank row between.** A tool and a hook are one row. A thinking step is open while it streams, and an ended thinking step takes the session mode (all its lines by default; see the `thinkingFold` setting in [cli.md](cli.md)). The user opens more with a key or the mouse, and the row count of a block never changes when the selection moves. Exactly one blank row separates any two blocks, and one blank row follows the last block; the block pane owns these gap rows, not the blocks.

## Roles and palette

Code and design files use role names, never color codes.
The theme maps roles to colors; lipgloss downsamples truecolor for 256-color terminals such as Terminal.app.

| Role | Dark truecolor | 256-color | No color | Used for |
|---|---|---|---|---|
| `fg` | `#d9dbe8` | 253 | default | Body text |
| `muted` | `#6f7390` | 244 | dim | Footer details, hints, secondary notes, the `?` of an unknown value |
| `dim` | `#4c4f66` | 240 | dim | Hints, placeholders, meta lines, timestamps |
| `border` | `#4c4f66` | 240 | default | Box lines of the editor and selectors; the selection box of a block |
| `panel` (background) | `#1d1f2b` | 235 | reverse off; prompt is bold | User prompt band |
| `code-panel` (background) | `#252838` | 236 | none | Code blocks |
| `selected` (background) | `#2d3150` | 237 | reverse | Selected row in selectors and completion; a drag range |
| `accent` | `#f472b6` | 205 | bold | `❯`, cursor, spinner, working text, box titles, context bar, `← back`, `▼ <N> new lines · End` |
| `assistant` | `#a78bfa` | 141 | bold | Model name, thinking level, steer chips |
| `tool` | `#2dd4bf` | 43 | bold | Tool step marks, follow-up chips, the `▼ End` marker |
| `hook` | `#14b8a6` | 37 | bold | `⚓` mark of a hook row |
| `thinking` | `#8f95b3` | 103 | italic | Thinking text and the `◆` or `›` of a thinking step. Italic only in the no-color profile, where it is the only cue left |
| `code` | `#fb923c` | 209 | default | Code text |
| `success` | `#4ade80` | 78 | bold | `✓`, connected dot, reconnected note |
| `warning` | `#facc15` | 220 | bold | Running tools, retries, observer, context ≥ 70% |
| `error` | `#f87171` | 203 | bold | `✗`, disconnected, context ≥ 90% |
| `prov-1..4` | `#fb923c` `#f472b6` `#2dd4bf` `#7aa2f7` | 209 205 43 111 | none | Provider dot in selectors |
| `h1` | `#2dd4bf` (value of `tool`) | 43 | bold | Level 1 heading of a reply; always bold |
| `h2` | `#7aa2f7` (value of `prov-4`) | 111 | bold | Level 2 heading and inline code of a reply; always bold |
| `h3` | `#a78bfa` (value of `assistant`) | 141 | bold | Level 3 heading of a reply; always bold |
| `logo` | gradient `assistant` → `accent` → `code` | `accent` | bold | `◢◤ ask` in the header row; no other part uses the logo |

A light theme is not designed yet; add it here before any component uses it.

## Glyphs

Use only these glyphs for these meanings.
All are one column wide in common monospace fonts, except `⚓`, which is two columns; do not use emoji or Nerd Font icons.

| Glyph | Meaning |
|---|---|
| `❯` | User input: prompt marker and editor marker |
| `◆` | One step: a tool call or a thinking part. The role tells which: `tool`, `warning` while it runs, `error` on failure, `muted` for a monitor note, `thinking` for thinking |
| `⚓` | Hook row. Two columns wide (U+2693 without a variation selector); the row reserves both columns |
| `›` | Open step mark: in the place of the step glyph when the step is open (Truncated or Expanded, also while it streams), selected, or under the pointer |
| `│` `┌` `┐` `└` `┘` | Selection box of a block, in `border`; `┆` replaces a corner when the window clips the box |
| `┌ ┬ ┐ ├ ┼ ┤ └ ┴ ┘ │ ─` | Box of a markdown table in a reply, in `dim` |
| `•` | Bullet of a markdown list in a reply |
| `⋮` | Hidden output rows between the first and the last rows of a Truncated step |
| `▼` | Bottom marker: jump to the end of the pane |
| `✓` / `✗` | Success / failure |
| `↻` | Retry or reconnect note |
| `⊘` | Cancelled |
| `⏏` | Detached |
| `◌` | Work in progress on the status row |
| `⧗` | Queued input |
| `┊` / `┆` | Dashed rail: status row / queue rows |
| `●` | Provider dot in selectors |
| `◉` | Observer: the prompt row of a client that watches another driver |
| `⌂` | Working directory |
| `◐` | Thinking level |
| `▰▱` | Context use bar, 8 cells |
| `⌕` | Filter field |
| `…` | Truncation and "in progress" |
| `─` | Rule between screen parts |
| `╭─╮│╰─╯` | Selector and dialog boxes (rounded) |
| `◢◤` | Logo mark |

`∴`, `◇` and `●` no longer mark rows in the block pane. A reply has no mark.

## Patterns

Widths below assume an 80-column terminal; rows always fill the terminal width exactly.
The screen, top to bottom: header row, rule, block pane, queue rows, status row, rule, prompt pane, rule, footer row, shortcut bar. The [footer design](../plans/261028-1310-account-model-discovery/design.md) owns this order; earlier designs that put the status data in the header row are historical records.
The owner of the order and the limits is [layout.go](../internal/tui/layout.go); the owner of each part is in the [component table](../internal/tui/README.md#component-owners).

- **Header row** (row 1): ` ◢◤ ask` in `logo`, with one space of padding, then spaces to the full width. It has no link state, no directory and no model, and it is the same in every state. A rule sits under it. The header row is not drawn in the viewer.
- **Footer row** (second-to-last row, under the lower rule of the prompt pane, no rule between it and the bar): ` ⌂ <cwd>` in `muted` at the left and `<provider>/<model> ◐ <level>  ▰▰▱▱▱▱▱▱ <pct>% ` at the right (`assistant` for the model and the level, `accent`, `warning` or `error` for the bar and the percent), with one space of padding at each end. The row shows no logo, no link state and no session cost. When the context window of the model is unknown, a lone `?` in `muted` replaces the bar and the percent. With no model the right side is `no model` in `muted`. When the row is too wide the parts give way in this order: the cwd shortens to its last element (`~/…/askcore`; it never goes from 60 columns), then the end of the model id is cut with `…` (the provider stays whole), and last, in a very narrow terminal, the rest of the model text. Under 60 columns: `<provider>/<model> <pct>% ` at the right and nothing at the left. The footer row is not drawn in the viewer.
- **Rules**: `─` over the full width in `border`: one under the header row and two around the prompt pane. They exist from 9 rows; under that the header row, one pane row, one status row, the prompt, the footer row and the bar stay. Under 5 rows the header row gives way first, then the pane row, then the footer row.
- **Block pane**: blocks one after another, each drawn at the full width, with exactly one blank row between any two blocks and one blank row after the last. The pane owns the gap rows. Column 0 and the last column are for the selection box; every block leaves them blank, and block text and right-aligned parts end 2 columns before the edge.
- **User prompt**: full-width `panel` band: two spaces, `❯ ` (`accent`, bold), the prompt (bold), send time right-aligned in `dim`. Wrapped rows indent four spaces on the band. A prompt over 3 rows is cut to 3 rows and one hint row `    … +<n> lines · e to expand` in `muted`.
- **Assistant reply**: no glyph. Rendered markdown indented two spaces. The first row shows the 24h time of the reply at the right in `dim`, as the prompt band does, only when the first row is a row of text and keeps 2 blank columns before the time; a reply that starts with a table or a code block shows no time, and a narrow first row wraps without it.
- **Reply markdown**: no raw syntax. A heading shows its text without `#` marks, bold, in `h1`, `h2` or `h3` for levels 1 to 3 and in `fg` for levels 4 to 6, with one blank row under it. Inline code is `h2` bold without backticks. A list item starts with `•` in `muted`; the numbers of an ordered list are `muted`. A block quote uses `│` and a rule uses `────────`, both in `muted`. Links are `tool`, underlined. A table is a box in `dim` (`┌ ┬ ┐`, `├ ┼ ┤`, `└ ┴ ┘`) with bold `fg` header cells and one space of padding in each cell. A column is as wide as its widest cell; when the box is wider than the text width, the widest columns become narrower and their cells wrap inside the box. Only a table that does not fit with columns 2 cells wide, or a table in a list or a block quote, goes to glamour.
- **Code block**: full-width `code-panel` rows with 2 columns of padding and code in `code` with syntax colors. There is no language row; the language only selects the colors.
- **Meta line**: after each finished reply, `<model> · <duration> · ↑<in> ↓<out> · $<cost>` in `dim`.
- **Step row**: `  ◆ <Verb> <what>` then the right column. The verb is bold `fg`, the description is `fg` and is cut with `…` first. A file change shows `+<n>` (`success`) and `-<m>` (`error`) after the path. The right column is empty on success, `⠋ <seconds>s` in `warning` while it runs, `✗ <reason>` in `error` on failure. Under 30 columns the right column goes.
- **Step mark**: a step shows `›` in place of its own mark when it is open (Truncated or Expanded, also while it streams), selected or under the pointer. Its own mark (`◆`, or `⚓` for a hook) shows only when it is collapsed and not selected. The mark keeps the role color of the step. A two-column glyph keeps its width, so the text does not move.
- **Open step**: the header row, one blank row, then the body at column 4. There is no rail.
- **Thinking step**: `  ◆ Thought for <duration>` when collapsed; `  › Thought for <duration>` when open; while it streams `  › Thinking… <seconds>s` with a spinner. The body is in the `thinking` role, not italic (italic only in the no-color profile), and wraps at the width minus 6 columns. Truncated shows the last lines, with `…` first when lines are hidden; Expanded shows all lines. An ended step takes the session mode: the `thinkingFold` setting at the start of the TUI, then the last `ctrl+t`. A step that the user folded keeps its fold.
- **Hook row**: `  ⚓ <Event> hook (<scope>/<name>) · <target>` with `⚓` in `hook`, then `✓ allowed <duration>` in `success` or `✗ <decision> <duration>` in `error` at the right, or a spinner while it runs. A second row in `muted` holds what the hook does, or its command when it has no description. The target is cut first, then `(<scope>/<name>)`. An open hook is the header row, the description row, one blank row, then the body at column 4. Expanded and the viewer add the detail fields (`Event`, `Matcher`, `Does`, `Source`, `Command`, `Result`, `Reason` when blocked) with names in `muted`, then `── output ──` in `dim` and all output.
- **Fold forms**: Collapsed is the row. Truncated adds, after one blank row and at column 4, the first 3 and the last 3 output rows with the fold hint between them. Expanded adds a `$ <command>` row for a shell call, all output wrapped, the diff of a file change, and the truncation line. A step with 6 output rows or fewer shows them all and no hint.
- **Fold hint**: `    ⋮ <n> more lines · e to expand · Enter full screen` in `muted`; under 60 columns `    ⋮ <n> more lines · e`. `n` counts lines of the result text, not terminal rows.
- **Truncation line**: `    model saw <n> of <total> lines · next offset <o>` or `    model saw <n> lines (<size>) of <size> · full output: <path>` in `muted`, after the output of an Expanded step and in the viewer. When the path does not fit, it moves to its own row and wraps by columns; it is never cut.
- **Diff line** (Expanded and viewer): four spaces, the new line number right-aligned (`dim`), the sign `+`, `-` or space and one space, then the text. Added rows use `success`, removed rows `error`; context uses `muted` for the sign and `fg` for the text. A long line wraps under its text column, and each change has three lines of context.
- **Selection**: every selected block (prompt, thinking, tool, hook, reply) keeps its colors and gets a thin box in `border`: `│` in column 0 and in the last column of each block row, `┌ ┐` on the blank row above, `└ ┘` on the blank row below. When the window clips the box, the edge row that shows has `┆` in place of the corners. When the list scrolls to a selected block, the gap rows above and below stay in view. A selection never changes a row count.
- **Bottom marker** (last pane row, only while the pane is not at the end): centered, bold, no background. `▼ End` in `tool`; when new output came `▼ <n> new lines · End` in `accent`, and under 30 columns `▼ <n> · End`. `n` counts pane rows below the window and may be approximate for a frame or two after a resize.
- **Viewer** (whole screen): title row ` ← back  <title>` with `← back` in `accent` (bold), the title in bold `fg` and the outcome right-aligned in its status role; a rule; the body of the block, wrapped, nothing cut; a rule; the key row ` ↑↓ scroll · PgUp PgDn page · Home End · Esc back` in `dim` with the window position `<a>–<b> of <n>` at the right (the short form ` ↑↓ PgUp PgDn · Esc back` under 60 columns, without the position). Under 5 rows only the body shows.
- **Drag range**: the cells between the press and the pointer on the `selected` background. The text that a release copies is plain: no box and no pointer mark. An open step copies `›`, a collapsed step its own mark; the blank gap rows between blocks are not copied.
- **Job line**: a step row for a finished background job, `  ◆ job <id> <label>`. A finished job shows `completed · exit 0` in `muted`; a failed or killed job shows `✗ failed · exit 2` or `✗ killed · <reason>` in `error`; monitor output shows `<n> events` in `muted`. It does not fold.
- **Notes**: `  ↻ ` retry (`warning`) or reconnect (`success`), `  ✗ ` error (`error`, with a `muted` next-action row indented four spaces), `  ⊘ ` cancel (`muted`), `  ⏏ ` detach (`muted`). Each is one block.
- **Start notices**: the block pane shows no model line; the footer row names the model. Two notices can stand at the start of a transcript, each as its own block with no glyph, indented two columns at every width and wrapped at spaces: the fallback notice `  Default model <provider>/<model> is not usable (<reason>).` in `muted`, and the `TEA_TRACE` warning in `warning`.
- **Status row**: `┊ ◌ <Status>` with the status word in `accent` (or `warning` for retry, `error` for disconnected) and a `muted` tail such as ` 4s · esc to interrupt`. Notices use the same rail in `dim`. Running background jobs use the row only when nothing else does: `┊ ◌ 1 background job · <id> <label>` in `muted` with a `dim` tail, no shimmer.
- **Queue rows**: `┆ ⧗ steer   <text>` (`assistant`) and `┆ ⧗ next    <text>` (`tool`), then `┆ alt+↑ edit queue · dashed rail = not sent yet` in `dim` italic. At most 3 rows; the last is `┆ +<n> more` when rows are cut.
- **Prompt pane**: between two rules; one column of left padding, `❯ ` then the draft. The placeholder `Ask anything  ·  / commands  ·  ctrl+l model` in `dim` italic (`Ask anything` under 60 columns). It grows with the draft up to half of the screen height and then scrolls inside.
- **Selector and dialog box**: take the prompt pane slot, up to half of the screen height. Rounded box titled `╭─ <Title> ───╮` (title in `accent`, bold); filter row `⌕`; rows `❯ ● provider/model` with right-aligned columns; the selected row uses the `selected` background; errors inside the box; one `dim` italic hint row below the box. A model row with an unknown context window shows a right-aligned `?` in `muted` in the window column. The row of the running model that the account list no longer has shows the note ` · not in account list` in `muted` after its id; under 60 columns, or when the note does not fit, the tag `unlisted` replaces `current`.
- **Completion list**: rows above the editor inside the prompt pane, `  ❯ /command   description`, selected row on `selected`; at most 8 rows, the last is `… <n> more` when rows are cut.
- **Shortcut bar** (last row): `dim`, one space of padding. Prompt focus: `Tab blocks · ctrl+t thinking · ctrl+l model · / commands`. Block list focus: `j/k select · e fold: <mode> · E all · Enter open · Tab prompt`, where `: <mode>` shows only for a block that can fold. Under 60 columns: `Tab blocks · / commands` and `j/k · e fold · Enter open`.
  When the complete hint fits, the bar adds `shift/option+drag select` for terminal selection.

## Motion

- Spinner frames for `◌`/`⠋` positions: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, about 80 ms per frame. A running step and a running hook use the same frames.
- Shimmer: the status word on the live rail sweeps a lighter color across its letters, about 2 s per sweep. Turn it off on 256-color and no-color terminals; keep the spinner.
- The cursor blinks only if the terminal does; do not draw a fake blink.

## Rules for new UI

1. Pick roles from the table; a new color needs a new role in this file first.
2. Pick glyphs from the table; a new meaning needs a new row here first.
3. Never put the dashed rail next to finished content.
4. Never box a message; boxes are for input surfaces, the selection box and markdown tables.
5. Never let a selection or hover change the row count of a block.
6. Check every sketch at 80 and 40 columns, and with color turned off.
7. Change this file in the same PR as any visual change, and regenerate the preview of the design.
