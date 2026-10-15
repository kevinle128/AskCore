# TUI style guide

This guide is the visual authority for the Ask terminal UI.
Every TUI design file (`plans/*/design.md`) and every component in `internal/tui` follows it.
Behavior and layout rules live in [tui-architecture.md](tui-architecture.md); this file covers only how things look.

Status: accepted direction **B4 "Soft + live rail"** (user, 2026-10-10) for colors, roles and prompt bands.
The screen model is the alternate screen of the Grok-style design (user, 2026-10-12).
Reference sketches and the interactive prototype: [design.md](../plans/261012-1337-t1b-grok-style-altscreen/design.md) and its [prototype](../plans/261012-1337-t1b-grok-style-altscreen/artifacts/interactive.html).
The T1a renderings in [preview.html](../plans/261009-2239-t1a-local-tui/artifacts/preview.html) and the [style directions](../plans/261009-2239-t1a-local-tui/artifacts/style-directions.html) show the earlier inline layout; use them for colors only.

## Principles

1. **Soft conversation.** Prompts sit on a quiet panel; replies are plain text with one accent mark. No boxes around messages.
2. **The dashed rail means "not finished".** A dashed rail (`┊`, `┆`) appears only next to live things: work in progress and queued input. The solid rail `┃` marks a selected block or an open thinking body, never live work.
3. **One accent, few colors.** Pink is the interactive accent, violet marks the assistant, teal marks tools. Other colors carry status only.
4. **Readable without color.** Every state must be clear with bold, dim and glyphs alone.
5. **One screen, rows by priority.** The TUI owns the alternate screen. The header and the shortcut bar always stay, the prompt keeps one row, and the block pane takes what is left. Rules (`─`) separate the parts; nothing draws a box around the whole screen. Only selectors and dialogs use boxes.
6. **One row first.** A tool, a thinking part and a hook are one row. The user opens more with a key or the mouse, and the row count of a block never changes when the selection moves.

## Roles and palette

Code and design files use role names, never color codes.
The theme maps roles to colors; lipgloss downsamples truecolor for 256-color terminals such as Terminal.app.

| Role | Dark truecolor | 256-color | No color | Used for |
|---|---|---|---|---|
| `fg` | `#d9dbe8` | 253 | default | Body text |
| `muted` | `#6f7390` | 244 | dim | Header details, hints, secondary notes |
| `dim` | `#4c4f66` | 240 | dim | Hints, placeholders, meta lines, timestamps |
| `border` | `#4c4f66` | 240 | default | Box lines of the editor and selectors |
| `panel` (background) | `#1d1f2b` | 235 | reverse off; prompt is bold | User prompt band |
| `code-panel` (background) | `#252838` | 236 | none | Code blocks |
| `selected` (background) | `#2d3150` | 237 | reverse | Selected row in selectors and completion; a selected collapsed step or hook; a drag range |
| `accent` | `#f472b6` | 205 | bold | `❯`, cursor, spinner, working text, box titles, context bar, `┃` rail of a selected block, `›` and `⌄`, `← back`, `▼ <N> new lines · End` |
| `assistant` | `#a78bfa` | 141 | bold | Model name, thinking level, steer chips |
| `tool` | `#2dd4bf` | 43 | bold | Tool step marks, follow-up chips, the `▼ End` marker |
| `hook` | `#14b8a6` | 37 | bold | `⚓` mark of a hook row |
| `thinking` | `#8f95b3` italic | 103 italic | italic | Thinking text, its `┃` rail and the `◆` of a thinking step |
| `code` | `#fb923c` | 209 | default | Code text |
| `success` | `#4ade80` | 78 | bold | `✓`, connected dot, reconnected note |
| `warning` | `#facc15` | 220 | bold | Running tools, retries, observer, context ≥ 70% |
| `error` | `#f87171` | 203 | bold | `✗`, disconnected, context ≥ 90% |
| `prov-1..4` | `#fb923c` `#f472b6` `#2dd4bf` `#7aa2f7` | 209 205 43 111 | none | Provider dot in selectors |
| `logo` | gradient `assistant` → `accent` → `code` | `accent` | bold | `◢◤ ask` in the header |

A light theme is not designed yet; add it here before any component uses it.

## Glyphs

Use only these glyphs for these meanings.
All are one column wide in common monospace fonts, except `⚓`, which is two columns; do not use emoji or Nerd Font icons.

| Glyph | Meaning |
|---|---|
| `❯` | User input: prompt marker and editor marker |
| `◆` | One step: a tool call or a thinking part. The role tells which: `tool`, `warning` while it runs, `error` on failure, `muted` for a monitor note, `thinking` for thinking |
| `⚓` | Hook row. Two columns wide (U+2693 without a variation selector); the row reserves both columns |
| `›` | Expand indicator: the selected or hovered step is at its lowest fold mode and can open |
| `⌄` | Open indicator: the selected or hovered step is Expanded and can close |
| `┃` | Solid rail: a selected block that is not shaded, and the body of an open thinking step |
| `⋮` | Hidden output rows between the first and the last rows of a Truncated step |
| `▼` | Bottom marker: jump to the end of the pane |
| `✓` / `✗` | Success / failure |
| `↻` | Retry or reconnect note |
| `⊘` | Cancelled |
| `⏏` | Detached |
| `◌` | Work in progress on the status row; also "starting" in the header |
| `⧗` | Queued input |
| `┊` / `┆` | Dashed rail: status row / queue rows |
| `●` | Link dot in the header; provider dot in selectors |
| `◉` | Observer (watching another driver) |
| `○` | In-process agent (no leader) |
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
The screen, top to bottom: header row, rule, block pane, queue rows, status row, rule, prompt pane, rule, shortcut bar.
The owner of the order and the limits is [layout.go](../internal/tui/layout.go); the owner of each part is in the [component table](../internal/tui/README.md#component-owners).

- **Header row** (row 1): `◢◤ ask  ● leader <id>  ⌂ <cwd>` at the left and `<model> ◐ <level>  ▰▰▱▱▱▱▱▱ <pct>%  $<cost> ` at the right. The dot is `success` (connected), `error` (leader lost), `warning` `◉` (observer), `muted` `○` with `in-process` (in-process agent) or `muted` `◌` (starting). With no model the right side is `no model` in `muted`. The cwd gives way first (last element, then none), then the model name is cut. Under 60 columns: `◢◤ ask ● <id>` left and `<model> <pct>% ` right.
- **Rules**: `─` over the full width in `border`. They exist from 8 rows; under that the header, one pane row, one status row and the prompt stay.
- **Block pane**: blocks one after another, each drawn at the full width. Column 0 is the gutter that a selection mark uses; every block leaves it blank.
- **User prompt**: full-width `panel` band: two spaces, `❯ ` (`accent`, bold), the prompt (bold), send time right-aligned in `dim`. Wrapped rows indent four spaces on the band. A prompt over 3 rows is cut to 3 rows and one hint row `    … +<n> lines · e to expand` in `muted`.
- **Assistant reply**: no glyph. Rendered markdown indented two spaces; one empty row above a reply that follows a step.
- **Code block**: full-width `code-panel` rows: language name in `dim`, then code in `code`.
- **Meta line**: after each finished reply, `<model> · <duration> · ↑<in> ↓<out> · $<cost>` in `dim`.
- **Step row**: `  ◆ <Verb> <what>` then the right column. The verb is bold `fg`, the description is `fg` and is cut with `…` first. A file change shows `+<n>` (`success`) and `-<m>` (`error`) after the path. The right column is empty on success, `⠋ <seconds>s` in `warning` while it runs, `✗ <reason>` in `error` on failure. Under 30 columns the right column goes.
- **Thinking step**: `  ◆ Thought for <duration>` in the `thinking` role; while it streams `  ◆ Thinking… <seconds>s` with a spinner. Truncated and Expanded add the text on a `┃` rail in `thinking`; Truncated shows the last lines, with `…` first when lines are hidden.
- **Hook row**: `  ⚓ <Event> hook (<scope>/<name>) · <target>` with `⚓` in `hook`, then `✓ allowed <duration>` in `success` or `✗ <decision> <duration>` in `error` at the right, or a spinner while it runs. A second row in `muted` holds what the hook does, or its command when it has no description. The target is cut first, then `(<scope>/<name>)`. Expanded and the viewer add the detail fields (`Event`, `Matcher`, `Does`, `Source`, `Command`, `Result`, `Reason` when blocked) with names in `muted`, then `── output ──` in `dim` and all output.
- **Fold forms**: Collapsed is the row. Truncated adds the first 3 and the last 3 output rows with the fold hint between them. Expanded adds a `$ <command>` row for a shell call, all output wrapped, the diff of a file change, and the truncation line. A step with 6 output rows or fewer shows them all and no hint.
- **Fold hint**: `    ⋮ <n> more lines · e to expand · Enter full screen` in `muted`; under 60 columns `    ⋮ <n> more lines · e`. `n` counts lines of the result text, not terminal rows.
- **Truncation line**: `    model saw <n> of <total> lines · next offset <o>` or `    model saw <n> lines (<size>) of <size> · full output: <path>` in `muted`, after the output of an Expanded step and in the viewer. When the path does not fit, it moves to its own row and wraps by columns; it is never cut.
- **Diff line** (Expanded and viewer): four spaces, the new line number right-aligned (`dim`), the sign `+`, `-` or space and one space, then the text. Added rows use `success`, removed rows `error`; context uses `muted` for the sign and `fg` for the text. A long line wraps under its text column, and each change has three lines of context.
- **Selection**: a selected Collapsed step or hook has the `selected` background over all its rows (reverse without color). Any other selected block keeps its colors and shows `┃` in `accent` in the gutter on each of its rows. A selection never changes a row count.
- **Expand indicator**: `›` or `⌄` in `accent`, bold, in place of the step glyph, for a selected block and for the block under the mouse pointer. A two-column glyph keeps its width, so the text does not move.
- **Bottom marker** (last pane row, only while the pane is not at the end): centered, bold, no background. `▼ End` in `tool`; when new output came `▼ <n> new lines · End` in `accent`, and under 30 columns `▼ <n> · End`. `n` counts pane rows below the window and may be approximate for a frame or two after a resize.
- **Viewer** (whole screen): title row ` ← back  <title>` with `← back` in `accent` (bold), the title in bold `fg` and the outcome right-aligned in its status role; a rule; the body of the block, wrapped, nothing cut; a rule; the key row ` ↑↓ scroll · PgUp PgDn page · Home End · Esc back` in `dim` with the window position `<a>–<b> of <n>` at the right (the short form ` ↑↓ PgUp PgDn · Esc back` under 60 columns, without the position). Under 5 rows only the body shows.
- **Drag range**: the cells between the press and the pointer on the `selected` background. The text that a release copies is plain: no rail, no shade, no indicator.
- **Job line**: a step row for a finished background job, `  ◆ job <id> <label>`. A finished job shows `completed · exit 0` in `muted`; a failed or killed job shows `✗ failed · exit 2` or `✗ killed · <reason>` in `error`; monitor output shows `<n> events` in `muted`. It does not fold.
- **Notes**: `  ↻ ` retry (`warning`) or reconnect (`success`), `  ✗ ` error (`error`, with a `muted` next-action row indented four spaces), `  ⊘ ` cancel (`muted`), `  ⏏ ` detach (`muted`). Each is one block.
- **Status row**: `┊ ◌ <Status>` with the status word in `accent` (or `warning` for retry, `error` for disconnected) and a `muted` tail such as ` 4s · esc to interrupt`. Notices use the same rail in `dim`. Running background jobs use the row only when nothing else does: `┊ ◌ 1 background job · <id> <label>` in `muted` with a `dim` tail, no shimmer.
- **Queue rows**: `┆ ⧗ steer   <text>` (`assistant`) and `┆ ⧗ next    <text>` (`tool`), then `┆ alt+↑ edit queue · dashed rail = not sent yet` in `dim` italic. At most 3 rows; the last is `┆ +<n> more` when rows are cut.
- **Prompt pane**: between two rules; one column of left padding, `❯ ` then the draft. The placeholder `Ask anything  ·  / commands  ·  ctrl+l model` in `dim` italic (`Ask anything` under 60 columns). It grows with the draft up to half of the screen height and then scrolls inside.
- **Selector and dialog box**: take the prompt pane slot, up to half of the screen height. Rounded box titled `╭─ <Title> ───╮` (title in `accent`, bold); filter row `⌕`; rows `❯ ● provider/model` with right-aligned columns; the selected row uses the `selected` background; errors inside the box; one `dim` italic hint row below the box.
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
4. Never box a message; boxes are for input surfaces.
5. Never let a selection or hover change the row count of a block.
6. Check every sketch at 80 and 40 columns, and with color turned off.
7. Change this file in the same PR as any visual change, and regenerate the preview of the design.
