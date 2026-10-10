# TUI style guide

This guide is the visual authority for the Ask terminal UI.
Every TUI design file (`plans/*/design.md`) and every component in `internal/tui` follows it.
Behavior and layout rules live in [tui-architecture.md](tui-architecture.md); this file covers only how things look.

Status: accepted direction **B4 "Soft + live rail"** (user, 2026-10-10).
Reference renderings: [T1a preview](../plans/261009-2239-t1a-local-tui/artifacts/preview.html), produced by [sketch-gen.py](../plans/261009-2239-t1a-local-tui/artifacts/sketch-gen.py).
The three directions that were compared are kept in [style-directions.html](../plans/261009-2239-t1a-local-tui/artifacts/style-directions.html).

## Principles

1. **Soft conversation.** Prompts sit on a quiet panel; replies are plain text with one accent mark. No boxes around messages.
2. **The rail means "not finished".** A dashed rail (`┊`, `┆`) appears only next to live things: work in progress and queued input. Committed history never has a rail.
3. **One accent, few colors.** Pink is the interactive accent, violet marks the assistant, teal marks tools. Other colors carry status only.
4. **Readable without color.** Every state must be clear with bold, dim and glyphs alone.
5. **Inline first.** Nothing draws a frame around the whole terminal. Only the editor and selectors use boxes.

## Roles and palette

Code and design files use role names, never color codes.
The theme maps roles to colors; lipgloss downsamples truecolor for 256-color terminals such as Terminal.app.

| Role | Dark truecolor | 256-color | No color | Used for |
|---|---|---|---|---|
| `fg` | `#d9dbe8` | 253 | default | Body text |
| `muted` | `#6f7390` | 244 | dim | Header details, footer text, secondary notes |
| `dim` | `#4c4f66` | 240 | dim | Hints, placeholders, meta lines, timestamps |
| `border` | `#4c4f66` | 240 | default | Box lines of the editor and selectors |
| `panel` (background) | `#1d1f2b` | 235 | reverse off; prompt is bold | User prompt band |
| `code-panel` (background) | `#252838` | 236 | none | Code blocks |
| `selected` (background) | `#2d3150` | 237 | reverse | Selected row in selectors and completion |
| `accent` | `#f472b6` | 205 | bold | `❯`, cursor, spinner, working text, box titles, context bar |
| `assistant` | `#a78bfa` | 141 | bold | `◆` reply mark, model name, thinking level, steer chips |
| `tool` | `#2dd4bf` | 43 | bold | Tool marks and names, follow-up chips |
| `thinking` | `#8f95b3` italic | 103 italic | italic | Thinking text and `∴` |
| `code` | `#fb923c` | 209 | default | Code text |
| `success` | `#4ade80` | 78 | bold | `✓`, connected dot, reconnected note |
| `warning` | `#facc15` | 220 | bold | Running tools, retries, observer, context ≥ 70% |
| `error` | `#f87171` | 203 | bold | `✗`, disconnected, context ≥ 90% |
| `prov-1..4` | `#fb923c` `#f472b6` `#2dd4bf` `#7aa2f7` | 209 205 43 111 | none | Provider dot in selectors |
| `logo` | gradient `assistant` → `accent` → `code` | `accent` | bold | `◢◤ ask` in the header |

A light theme is not designed yet; add it here before any component uses it.

## Glyphs

Use only these glyphs for these meanings.
All are one column wide in common monospace fonts; do not use emoji or Nerd Font icons.

| Glyph | Meaning |
|---|---|
| `❯` | User input: prompt marker and editor marker |
| `◆` | Start of an assistant reply |
| `∴` | Thinking |
| `◇` / `●` | Tool call running / finished |
| `✓` / `✗` | Success / failure |
| `↻` | Retry or reconnect note |
| `⊘` | Cancelled |
| `⏏` | Detached |
| `◌` | Work in progress on the live rail; also "starting" in the footer |
| `⧗` | Queued input |
| `┊` / `┆` | Live rail: status / queue |
| `◉` | Observer (watching another driver) |
| `○` | In-process agent (no leader) |
| `⌂` | Working directory |
| `◐` | Thinking level |
| `▰▱` | Context use bar, 8 cells |
| `⌕` | Filter field |
| `…` | Truncation and "in progress" |
| `╭─╮│╰─╯` | Editor and selector boxes (rounded) |
| `◢◤` | Logo mark |

## Patterns

Widths below assume an 80-column terminal; rows always fill the terminal width exactly.

- **Header** (once, scrollback): `◢◤ ask   v<version> · leader <id> · <cwd>`, then one empty row. In process: `in-process agent` replaces `leader <id>`.
- **User prompt**: full-width `panel` band: two spaces, `❯ ` (`accent`, bold), the prompt (bold), send time right-aligned in `dim`. Wrapped rows indent four spaces on the band. One empty row after.
- **Assistant reply**: `  ◆ ` (`assistant`, bold) on the first row, four-space indent after; rendered markdown.
- **Code block**: full-width `code-panel` rows: language name in `dim`, then code in `code`, indented four spaces.
- **Meta line**: after each finished reply, `    <model> · <duration> · ↑<in> ↓<out> · $<cost>` in `dim`, then one empty row.
- **Thinking**: `  ∴ ` then italic `thinking` text; hidden form is one row `  ∴ Thinking...`.
- **Tool line**: `  ◇ <tool>  <short args>` with `running ⠋` right-aligned in `warning`; when done `  ● <tool>  <short args>` with `✓ <duration>` (`success`) or `✗ <reason>` (`error`).
- **Notes in scrollback**: `  ↻ ` retry (`warning`) or reconnect (`success`), `  ✗ ` error (`error`, with a `muted` next-action row indented four spaces), `  ⊘ ` cancel (`muted`), `  ⏏ ` detach (`muted`).
- **Live rail** (live region only): `┊ ◌ <Status>` with the status word in `accent` (or `warning` for retry, `error` for disconnected) and a `muted` tail such as ` 4s · esc to interrupt`. Notices use the same rail in `dim`.
- **Queue**: `┆ ⧗ steer   <text>` (`assistant`) and `┆ ⧗ next    <text>` (`tool`), then `┆ alt+↑ edit queue · dashed rail = not sent yet` in `dim` italic.
- **Editor box**: rounded box, full width, `border` role; inside `│ ❯ ` then the draft; placeholder `Ask anything  ·  / commands  ·  ctrl+l model` in `dim` italic (`Ask anything` under 60 columns). Continuation rows indent two spaces after `│ `.
- **Selector box**: rounded box titled `╭─ <Title> ───╮` (title in `accent`, bold); filter row `⌕`; rows `❯ ● provider/model` with right-aligned columns; the selected row uses the `selected` background; errors inside the box; one `dim` italic hint row below the box.
- **Completion list**: rows above the editor, `  ❯ /command   description`, selected row on `selected`.
- **Footer** (one row): left `  ● leader <id>   ⌂ <cwd>`; right `<model>[ ◐ <level>]  ▰▰▱▱▱▱▱▱ <pct>%  $<cost> `. The dot is `success` (connected), `error` (leader lost), `warning` `◉` (observer), `muted` `○` (in-process) or `◌` (starting). Under 60 columns: `  ● <id>` left and `<model> <pct>% ` right.

## Motion

- Spinner frames for `◌`/`⠋` positions: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, about 80 ms per frame.
- Shimmer: the status word on the live rail sweeps a lighter color across its letters, about 2 s per sweep. Turn it off on 256-color and no-color terminals; keep the spinner.
- The cursor blinks only if the terminal does; do not draw a fake blink.

## Rules for new UI

1. Pick roles from the table; a new color needs a new role in this file first.
2. Pick glyphs from the table; a new meaning needs a new row here first.
3. Never put a rail next to committed content.
4. Never box a message; boxes are for input surfaces.
5. Check every sketch at 80 and 40 columns, and with color turned off.
6. Change this file in the same PR as any visual change, and regenerate the preview.
