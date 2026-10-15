# Feature comparison: T1b tool output (T-LAY-09, T-MD-10) and `/copy` (T-CMD-08)

## Sources

- Pi: `~/Desktop/workspace/opensources/pi` (TypeScript, differential renderer).
- Grok: `~/Desktop/workspace/opensources/grok-build` at `559751fd` (Rust, `xai-grok-pager`).
- Local: Ask `new-session` branch, `internal/tui`.

Slice N1 (job notices) already has an approved design and a plan (`plans/261012-0155-t1b-job-notices`). It is out of scope here.

## Head-to-head

| Aspect | Pi | Grok | Ask today | Recommendation |
|---|---|---|---|---|
| Screen model | Inline; whole component tree drawn again on change | Alt screen (`app/mod.rs:1534` EnterAlternateScreen), own pager | Inline; committed rows written once; `Rebuild()` draws all committed rows again (`render_inline.go:82`) | Keep inline. Use `Rebuild()`. |
| Expand state | One global flag, `ctrl+o` (`interactive-mode.ts:4483-4502`) | Per block (`DisplayMode` Collapsed/Truncated/Expanded, `block.rs:88-93`), keys `h`/`l`/Enter on a selected block | None. `ctrl+t` is a global thinking toggle that calls `Rebuild()` (`update_input.go:534-537`) | Global flag, `ctrl+o`, same path as `ctrl+t`. Per block needs block selection, which an inline TUI does not have. |
| Collapsed preview | Per tool: bash 5 visual lines (tail), read 10, grep 15, ls/find 20, other 10 | 3 lines for every tool | One line, status only; `ToolResultDetails` is not read (`translate.go:497-501`) | Per-tool small previews (Pi), tail for bash, head for the others. Edit shows a diffstat when collapsed and the diff when expanded. |
| Hint | `… N more lines (ctrl+o to expand)` | `... (N more lines, press Enter to view)` | None | Pi text. |
| Model-side truncation display | `[Truncated: X of Y lines (limit Z)]`, `[Full output: <path>]` from `details.truncation`, `fullOutputPath`; always visible | Not separated from the view truncation | `Truncation`, `SpillPath`, `SpillCapped` exist in `pkg/protocol/tool.go:51-78`, unused | One muted line from those fields, visible in both modes. |
| `/copy` | Last assistant text, no thinking (`agent-session.ts:4298-4320`). Order: native, platform command, OSC 52 on SSH/headless; 100 KB OSC 52 cap with an error | No `/copy`; mouse selection copy; native first, OSC 52 on SSH (`clipboard.rs:340-374`) | `tea.SetClipboard` used only in provider setup (`update_providers.go:463`) | Pi model: copy last assistant text. Native command first (`pbcopy`, `wl-copy`, `xclip`/`xsel`, `termux-clipboard-set`), OSC 52 when on SSH or no native command. Cap OSC 52 and report the real result. |

## Why not the Grok way

Grok runs in an alt screen with its own scrollback, so it can fold any block. Ask decided on inline with native scrollback (T0 gate, D15 keeps alt screen optional for T2). Per-block folding would need block selection and focus in an inline view. That is new UI for little gain. Pi's global flag fits the `Rebuild()` path that Ask already has.

## Risk

- `Rebuild()` clears and writes the scrollback again. Long sessions write many rows. `ctrl+t` already pays this cost, so this is the same accepted cost.
- The inventory note on T-LAY-09 ("Expand cannot re-render committed scrollback") is out of date, because `Rebuild()` exists. Update the inventory row when the slice lands.
- OSC 52 gives no acknowledgement. Say "sent to the terminal clipboard" for OSC 52 and "Copied" only for a native command that succeeded.
- On the leader TUI the client runs on the user's host, so the clipboard is local. No protocol change.

Risk score: low (TUI only, no protocol change, existing rebuild path).

## Unresolved questions

1. Global `ctrl+o` (Pi) or per block (Grok)? Recommendation: global.
2. Does `/copy` need a key binding too (Pi uses `ctrl+x`)? Recommendation: command only for M1.

## Decision (user, 2026-10-12)

Tool output expand uses style C (Claude Code): the main inline view stays compact and is never redrawn; `ctrl+o` opens a separate transcript view (alt screen) with all tool output in full; `ctrl+o` or `Esc` returns. Evidence: installed Claude Code 2.1.183 strings `(ctrl+o to expand)`, `stdout shown in transcript mode (ctrl+o)`, `Showing detailed transcript`. Mockup: [three-styles mockup](xia-261012-0220-tool-expand-three-styles-mockup.html).

This brings a temporary alt-screen view into M1. D15 (optional alt-screen mode for the whole TUI) stays in T2.

## Decision change (user, 2026-10-12 12:40)

The user replaces style C with the Grok style: compact by default, every block expands on demand, in an alt-screen TUI ("chuyển sang alt-screen"). This reverses the inline screen model (T0 gate) and moves D15 (alt screen, planned optional in T2) into T1b as the default. The style C code (transcript view) is already implemented in `plans/261012-0226-t1b-tool-output`; its fate is decided in the new design.
