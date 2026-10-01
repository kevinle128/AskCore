# Lane H: Pi-style coding-agent TUI in Go on bubbletea

Date: 2026-09-30. Library decision (bubbletea) is fixed by the user; nothing here compares frameworks.
Method: cloned and read source of bubbletea main (ff51ba4, v2.0.10 content), lipgloss, bubbles, glamour, ultraviolet, charmbracelet/x, crush and kiln; queried proxy.golang.org for versions; read GitHub issues via `gh`; ran one scratch build of the go.mod migration (scratchpad, not in repo). Pi needs list (`researcher-260930-2254-pi-tui.md`) did not exist when I finished, so I could not check against its numbered needs. I checked against `pi/tui-plan.md` and `packages/tui/src` names only.

## 1. Outcome (read this first)

1. Pin bubbletea v2: `charm.land/bubbletea/v2 v2.0.10` (2026-09-24), with `charm.land/lipgloss/v2 v2.0.6`, `charm.land/bubbles/v2 v2.2.1`, `charm.land/glamour/v2 v2.0.1`. v2 is stable (v2.0.0 released 2026-02-24; ten patch releases since). Crush and kiln both run v2.0.9. v1 has no maintained future (last v1.3.10, 2025-09-17).
2. Inline mode works in v2 and is what Pi's main-screen mode needs (`tea.Println` inserts above a non-alt-screen `View`). It has real, open bugs. The only other production Go coding agent I found that does Pi-style inline (kiln, `github.com/6cclab/kiln`) vendored bubbletea and ultraviolet and patched four renderer bugs. Plan for that: budget a vendored fork or a custom scrollback committer from day one.
3. Crush (charmbracelet's own coding agent) does NOT do inline. It is alt-screen only (`internal/ui/model/ui.go:3705`, `v.AltScreen = true`). Use crush as a component quarry (dialogs, completions, diffview, list, streaming markdown, images), not as the inline reference.
4. The Pi editor, autocomplete, overlays, kill ring, undo, markdown streaming and image display are mostly custom work in Go. `bubbles/textarea` gives about 60 percent of the editor; it has no undo and no kill ring.
5. Extensions: Pi already defines a serializable UI subprotocol (`packages/coding-agent/docs/rpc-extension-ui.md`, `src/modes/rpc/rpc-types.ts:253-297`). Reuse it as the wire contract for out-of-process extensions. It is the right size.

Ranked recommendation: (A) bubbletea v2 + ultraviolet-based renderer, inline mode, own scrollback committer or a vendored patched bubbletea, alt-screen mode as a second View branch. (B) Same but stock bubbletea `tea.Println` (cheaper, ships the bugs in section 3). (C) v1: not recommended (section 2).

## 2. bubbletea v1 vs v2

### Versions and import paths (verified against proxy.golang.org, 2026-09-30)

| Module | Import path | Latest | Notes |
|---|---|---|---|
| bubbletea v1 | `github.com/charmbracelet/bubbletea` | v1.3.10 (2025-09-17) | AskCore pins this. Frozen. |
| bubbletea v2 | `charm.land/bubbletea/v2` | v2.0.10 (2026-09-24) | `go 1.26.0` in go.mod |
| lipgloss v2 | `charm.land/lipgloss/v2` | v2.0.6 (2026-08-11) | lipgloss v1.1.0 is the old line |
| bubbles v2 | `charm.land/bubbles/v2` | v2.2.1 (2026-08-24) | textarea, viewport, textinput, list, spinner, key, help, filepicker |
| glamour v2 | `charm.land/glamour/v2` | v2.0.1 (2026-06-12) | |
| huh v2 | `charm.land/huh/v2` | v2.0.3 (2026-03-10) | forms; probably not needed |
| ultraviolet | `github.com/charmbracelet/ultraviolet` | pseudo-version 20260929 (no tagged release) | renderer/cell buffer under bubbletea v2 |
| x/ansi | `github.com/charmbracelet/x/ansi` | v0.11.8 | sequences, width, kitty/iterm2/sixel encoders |
| x/cellbuf | `github.com/charmbracelet/x/cellbuf` | v0.0.15 | only pulled by lipgloss v1 |
| chroma | `github.com/alecthomas/chroma/v2` | v2.27.0 | |
| displaywidth | `github.com/clipperhouse/displaywidth` | v0.11.0 | |

Adoption risk of v2: ultraviolet has no semver tag, so builds depend on a pseudo-version; expect churn. Bubbletea moved to Go 1.26 (`go.mod:5` at tag v2.0.10). Charm's own apps (crush v2.0.9, go 1.27) and third parties (kiln) run it in production.

### API differences that matter (source: `bubbletea/UPGRADE_GUIDE_V2.md`, `tea.go`, `cursed_renderer.go`)

- `View()` returns `tea.View`, a struct, not a string. Terminal features are declarative fields: `AltScreen`, `MouseMode`, `ReportFocus`, `DisableBracketedPasteMode`, `WindowTitle`, `Cursor`, `ForegroundColor`, `BackgroundColor`, `ProgressBar`, `KeyboardEnhancements`, `OnMouse`. This makes Pi's "one component tree, two compositions" trivial: set `AltScreen` from state.
- Renderer: new "cursed renderer" on ultraviolet (`cursed_renderer.go`). It keeps a cell buffer and diffs cells (real differential rendering, better than v1's line diff). Synchronized output (mode 2026) is used automatically when the terminal supports it (`cursed_renderer.go:576-602`). Grapheme-cluster width mode 2027 is enabled when supported (`:743-750`).
- Keys: `tea.KeyPressMsg` / `tea.KeyReleaseMsg` (`tea.KeyMsg` is an interface). Fields `Code`, `Text`, `Mod`, `ShiftedCode`, `BaseCode`, `IsRepeat`. `String()` returns `"space"` for the space bar. Keyboard enhancements (kitty protocol) via `View.KeyboardEnhancements`; the terminal answers with `tea.KeyboardEnhancementsMsg` (`SupportsKeyDisambiguation`, `SupportsEventTypes`, `SupportsAlternateKeys`, ...). This covers Pi's `keys.ts` needs (shift+enter, ctrl+enter, modifier-aware bindings) on terminals that support kitty keyboard; legacy terminals still get ambiguity.
- Paste: `tea.PasteStartMsg`, `tea.PasteMsg{Content}`, `tea.PasteEndMsg`. Bracketed paste is on by default. This replaces Pi's `stdin-buffer.ts` paste-marker logic.
- Mouse: `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg`; `msg.Mouse()` gives X, Y, Button, Mod. Mode is a View field.
- Cursor: `View.Cursor = tea.NewCursor(x, y)` with shape, color, blink. Needed for IME positioning; bubbles v2 textareas expose `Cursor()` (`textarea.go:1751`) and `SetVirtualCursor(false)` to use the real cursor.
- Raw sequences: `tea.Raw(string)` sends bytes unprocessed (`raw.go`). This is the hook for OSC 52 clipboard, kitty graphics, window title extras. `tea.SetClipboard` / `tea.ReadClipboard` exist (`clipboard.go`).
- Inline mode: `AltScreen == false` means inline. The renderer sizes its cell buffer to the View's content height (`cursed_renderer.go:298-311`). Output above it: `tea.Println` / `tea.Printf` commands (`renderer.go:70,86`) and `Program.Println` (`tea.go:1399`), routed to `insertAbove` (`cursed_renderer.go:756`). Both print nothing while in alt-screen.
- Exec: `tea.ExecProcess` for launching `$EDITOR` (Pi's external editor) exists (`exec.go`).
- Companion pins: bubbletea v2.0.10 requires `ultraviolet v0.0.0-20260703...`, `x/ansi v0.11.7`, `colorprofile v0.4.3`, `x/term v0.2.2`. The scratch build resolved `x/ansi v0.11.8`.

### go.mod conflict: recommended resolution (tested in a scratch copy)

The current go.mod has a floor for `x/cellbuf v0.0.15` (lines 107-109) because lipgloss v1 pulls an old cellbuf that cannot compile against x/ansi v0.11, which golangci-lint v2.12 brings via `charm.land/lipgloss/v2`. The conflict exists only because AskCore uses lipgloss v1 and lipgloss v2 at once.

Fix: move to v2 for both, which removes lipgloss v1 and cellbuf from the graph. Tested in a scratch copy of AskCore's go.mod and go.sum with a trivial `cmd/tui/main.go` using `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2`: `go get charm.land/bubbletea/v2@v2.0.10 charm.land/lipgloss/v2@v2.0.6` then `go mod tidy` succeeded, `go build ./cmd/tui` succeeded. Results: `go` directive rises from 1.25.10 to 1.26.0 (bubbletea v2 requires it; toolchain here is go1.26.0), `x/cellbuf` and `lipgloss v1` disappear from go.mod, `charm.land/lipgloss/v2` becomes direct, `x/ansi` goes to v0.11.8. I did not run `golangci-lint` or `go test ./...` on that copy; do that before merging.

Open item: the Go directive bump to 1.26 affects the whole repo (CI images, Docker). Decision needed (see unresolved questions).

## 3. Inline mode: scrollback plus fixed live area

### How it works in stock bubbletea v2

- `View` content (non-alt-screen) is the live area. Its height is the content height. If content is taller than the terminal, the renderer drops lines from the TOP (`cursed_renderer.go:347-351`). So the live area can never exceed terminal height; long live content (streaming message, big overlay) must be clipped by the app.
- `tea.Println(str)` -> `insertAbove` (`cursed_renderer.go:756-800`): moves to the frame bottom, emits `\n` times (line count plus estimated wrap count), moves up, `InsertLine`s and writes the text, so the text lands in real terminal scrollback and the live area shifts down. It estimates wraps as `lineWidth / w`, so pre-wrap your text to width yourself (lipgloss/x/ansi `Wrap`/`Hardwrap`) and pass lines shorter than width, ideally width-1 (see octo-agent below).
- Selection, native wheel scroll, and search in the terminal all work because the transcript is real scrollback. Transcript survives exit. Mouse mode should stay OFF in inline mode, or wheel scrolling of native scrollback stops (same trade Pi makes).
- Streaming long messages: commit finished blocks only. Keep the in-flight tail (last N rows) in the live area and re-render it each delta; commit the block through `tea.Println` when complete (kiln design, `docs/architecture.md:451-475`). For a huge single block, split into chunks smaller than terminal height (see bug 1822).

### Known problems (evidence)

| Issue | Status (2026-09-30) | Impact |
|---|---|---|
| [bubbletea #1822](https://github.com/charmbracelet/bubbletea/issues/1822) one `Println` taller than the space above the View removes the rendered View (v2.0.10, main) | open | Commit long blocks in chunks below terminal height; test in a PTY emulator |
| [#1666](https://github.com/charmbracelet/bubbletea/issues/1666) `insertAbove` cursor math races with `\e[2J\e[3J` clear plus big history push; proposed `PrintlnRaw` | closed | Do not mix native clear-scrollback with Println; own the committer |
| [#1567](https://github.com/charmbracelet/bubbletea/issues/1567) duplicated content after resize in inline mode | closed; maintainer says leftover above the view after resize is expected because the terminal pushes lines to scrollback | Resize cannot re-wrap already committed scrollback in ANY implementation; same as Pi main-screen. Re-render only the live area |
| [octo-agent PR 2467](https://github.com/open-octo/octo-agent/pull/2467) frame line that physically wraps but counts as one leaves a duplicate input box | merged fix in app | Cap live-frame line width to width-1 (width-math disagreement for CJK, emoji, ambiguous width) |

Kiln's patches (`github.com/6cclab/kiln`, files `third_party/bubbletea/HARNESS-PATCH.md`, `third_party/ultraviolet/HARNESS-PATCH.md`, replace directives at `go.mod:64-66`, bubbletea 2.0.9 and ultraviolet 20260811):
1. Flush the pending frame before `insertAbove` (scroll offset used the stale frame height).
2. Use full live-region redraw (`Redraw`) instead of the incremental diff (the cell model desyncs after `insertAbove` scrolling; stale cells remained).
3. Repaint live region after `insertAbove` so the hardware cursor returns to the input caret.
4. Also flush on the very first `insertAbove` (race at startup).
5. ultraviolet: erase against the old buffer before resizing when the live region shrinks (closing an overlay left stale rows). Upstream still unfixed as of ultraviolet 20260922 per kiln's note.

These are the strongest evidence in this report: someone built the same thing (Claude Code-style inline TUI, transcript in scrollback, live region, dialogs, ctrl+f toggle between inline and alt-screen) and needed all five. Kiln has no LICENSE file in its repo root (so no reuse rights), so treat it as design evidence, not as a dependency; read the patch notes and re-derive.

### Real projects, ranked as references

| Project | Mode | Use |
|---|---|---|
| kiln `6cclab/kiln` (Go, bubbletea v2.0.9 vendored+patched) | inline default toggle with alt-screen (`--inline`, ctrl+f) | Best reference: `internal/tui/bridge.go` (single goroutine owns Println so commit order is stable), `internal/tui/app.go` (`liveLines`), `internal/tui/editor/{model,history,killring,view}.go` (custom editor with kill ring and history), `internal/tui/{dialog*,autocomplete,footer,changepreview,markdown}.go`, `internal/testkit/screen` (terminal-emulator test driver) |
| crush `charmbracelet/crush` (Go, v2.0.9) | alt-screen only | Component quarry: `internal/ui/dialog/dialog.go` (overlay stack drawing onto `uv.Screen`), `internal/ui/completions`, `internal/ui/diffview/diffview.go`, `internal/ui/chat/streaming_markdown.go`, `internal/ui/list/list.go`, `internal/ui/image/image.go`, `internal/ui/xchroma` |
| bubbletea `examples/chat`, `package-manager` | inline | Minimal Println pattern only |

License note: crush is FSL-1.1-MIT (verify before copying code); read it for design, do not paste without checking.

## 4. Need-by-need mapping

Legend: STOCK = use library as is; ADAPT = library + glue; CUSTOM = write it.

| Pi need | Go answer | Class | Gap / workaround |
|---|---|---|---|
| Inline transcript + fixed live area | `tea.Println` + non-alt `View` | ADAPT | Bugs in section 3; committer goroutine; chunking; width-1 cap |
| Differential rendering | ultraviolet cell diff inside bubbletea v2 | STOCK | Kiln found desync after insertAbove; fall back to full live redraw (cheap, live area is small) under sync output |
| Alt-screen mode (VStack/HStack/ScrollView) | `View.AltScreen=true`; `bubbles/viewport` for scroll; `lipgloss.JoinVertical/JoinHorizontal`; `uv/layout` (Cassowary, `Len/Min/Max/Percent/Ratio/Fill`) if real constraints needed | ADAPT | Pi's flex `basis/grow/shrink` has no direct match; ultraviolet layout gives constraints, simpler than flex. For 3 fixed regions (transcript, dock, sidebar) manual height math is enough (KISS) |
| Multi-line editor | `bubbles/textarea` v2: soft wrap, `DynamicHeight`, `MinHeight/MaxHeight`, `SetPromptFunc`, `SetVirtualCursor`, `Cursor()`, `LineInfo()`, selection (`selection.go`), memoized wrap | ADAPT/CUSTOM | No undo stack, no kill ring, no word-navigation parity (`kill-ring.ts`, `undo-stack.ts`, `word-navigation.ts` in Pi). Options: wrap textarea and add undo (snapshot on edit) and kill ring around it, OR custom editor like kiln's `internal/tui/editor` (history + killring in ~5 files). Recommend custom: Pi's autocomplete, paste markers, and external-editor need tight control; textarea internals are hard to extend |
| File/command autocomplete popup | none in bubbles for inline popup | CUSTOM | Fuzzy match: port `fuzzy.ts` (small); file scan: `filepath.WalkDir` + gitignore lib or `git ls-files`. Popup renders as extra live-area rows under the editor (inline) or as a layer (alt). Reference: crush `internal/ui/completions/completions.go`, kiln `internal/tui/autocomplete.go` |
| Overlays / modals / selectors | lipgloss v2 `Canvas`, `Layer` (X, Y, Z, ID), `Compositor.Hit(x,y)` (`lipgloss/layer.go`, `canvas.go`); ultraviolet `Screen` drawing (crush `dialog.Overlay.Draw(scr uv.Screen, area)`) | ADAPT | Compositing works on a full-screen cell buffer, so overlays fit alt-screen naturally. In INLINE mode there is no full screen: overlays must be rendered as part of the live area (replace the editor with the selector, grow live area up to terminal height). This matches Pi's "editor or temporary replacement UI" model (`tui-plan.md` dock list). Kiln does this; its closing-overlay bug required the ultraviolet patch |
| Selectors (model picker, session list) | `bubbles/list` (filtering, pagination, delegate) or `bubbles/table`; crush `dialog/models.go`, `dialog/sessions.go` | STOCK/ADAPT | Pi has a bespoke select list; bubbles list is heavier than needed, a 100-line custom list is fine |
| Markdown render | `glamour/v2` (goldmark + chroma) | ADAPT | Cost: whole-document render; glamour resets wrap state between calls. Crush solved streaming with a stable-prefix cache split at safe blank-line boundaries (`internal/ui/chat/streaming_markdown.go`, ~1050 lines with tests; its own notes say naive concatenation is not equal to a single render). In inline mode this is easier: commit finished blocks once, re-render only the live tail. Pi uses a marked-based custom `Markdown` component with caches; glamour theming is JSON styles, so map Pi themes to glamour styles |
| Syntax highlight | chroma v2.27.0 directly (formatters `terminal16m`, `terminal256`); glamour already uses it | STOCK | Crush wraps chroma in `internal/ui/xchroma`; copy the idea for theme mapping. Respect color profile via `colorprofile` (tea sends `tea.ColorProfileMsg`) |
| Diff view | `aymanbagabas/go-udiff` v0.4.1 for hunks; render via crush-style `diffview` (`internal/ui/diffview/diffview.go`, 826 lines, split/unified, chroma highlight) | ADAPT | No importable diff widget; write ~300 lines using udiff + chroma + lipgloss |
| Inline images | `x/ansi/kitty` (encoder, `Placeholder` U+10EEEE and `Diacritic()`), `x/ansi/iterm2`, `x/ansi/sixel` (all in charmbracelet/x ansi module) | CUSTOM | Kitty Unicode placeholders turn an image into ordinary text cells (`crush/internal/ui/image/image.go:159-276`): the image survives cell diffing, `Println`, and scrollback. Transmit the image once with `tea.Raw(kitty ... Action TransmitAndPut, unicode placeholder)`, then print placeholder cells. iTerm2 OSC 1337 and sixel have no placeholder equivalent; putting them through `Println` breaks the width estimate in `insertAbove`. Workaround: emit them via `tea.Raw` at commit time with reserved blank rows, or fall back to half-block rendering (crush's `EncodingBlocks` via go-ansi-paintbrush). Detection: query terminal (kitty `a=q`, XTGETTCAP, `tea.EnvMsg`/`TERM_PROGRAM`). I found no maintained third-party Go lib better than x/ansi for this; did not evaluate rasterm (module proxy rejected my query path) |
| Unicode width | ultraviolet/bubbletea use `x/ansi` `StringWidth` with `clipperhouse/displaywidth` and mode 2027; `rivo/uniseg` v0.4.7 is the legacy option | STOCK | Ambiguous-width / emoji disagreement with the terminal is the cause of the wrap-duplication bug (octo PR 2467). Cap width-1 in live area |
| Theming | `lipgloss/v2` styles + `charmtone` palette (`x/exp/charmtone`); light/dark detection with `tea.BackgroundColorMsg`/`View.BackgroundColor`; colors auto-downsample by `colorprofile` | ADAPT | Define one theme struct (Pi `colors.ts`/theme JSON -> Go struct with `color.Color` fields); no hot-reload provided; write a file watcher if needed. lipgloss v2 is pure (no global renderer, no `lipgloss.AdaptiveColor`; use `lipgloss.LightDark(isDark)`) |
| Keybindings config | `bubbles/key` (`key.Binding`, `key.Matches`, `help` bubble) | ADAPT | Bindings are plain strings like `"ctrl+shift+enter"`; Pi's config maps action name -> keys. Parse JSON into `map[string][]string`, build `key.Binding`s at startup. Modifier combos (shift+enter, ctrl+enter) only work when kitty keyboard is negotiated; fall back to alt+enter |
| Bracketed paste | `PasteMsg` (default on) | STOCK | Large paste "marker" collapse (Pi behavior) is app logic: on `PasteMsg`, if len > threshold, store content and insert placeholder |
| Mouse | `View.MouseMode`, wheel/click/motion messages, `OnMouse`; `Compositor.Hit` for hit testing | STOCK | Only in alt-screen mode; keep off in inline mode |
| Kill ring, undo, word nav | none | CUSTOM | Port Pi's small modules (`kill-ring.ts`, `undo-stack.ts`, `word-navigation.ts`) to Go; kiln has `editor/killring.go` as pattern |
| Clipboard | `tea.SetClipboard` (OSC 52), `tea.ReadClipboard` | STOCK | OSC 52 not honored by all terminals |
| Hyperlinks (OSC 8) | `ansi.SetHyperlink` (`x/ansi/hyperlink.go`); lipgloss v2 supports links in styles | STOCK | |
| Window title, progress bar, focus | `View.WindowTitle`, `View.ProgressBar`, `View.ReportFocus` | STOCK | |
| Spinner / working indicator | `bubbles/spinner` | STOCK | Ticks cause re-renders; live area is small so it is cheap |
| External editor | `tea.ExecProcess` | STOCK | |
| Terminal tests | none official besides golden files (`x/exp/golden`, `teatest` pattern) | CUSTOM | Kiln's `internal/testkit/screen` runs the real binary in a PTY plus terminal emulator and asserts on visible screen and scrollback. All five kiln patches were found this way or by real-terminal use (golden tests missed the startup race). Budget this early |

## 5. Extensions contributing UI when out-of-process

Pi's own answer is the contract to copy: `docs/rpc-extension-ui.md` and `src/modes/rpc/rpc-types.ts`. It defines:
- Dialog requests (`select`, `confirm`, `input`, `editor`) with `id` and optional `timeout`; client must answer `extension_ui_response` (value, confirmed, or cancelled).
- Fire-and-forget (`notify`, `setStatus`, `setWidget`, `setTitle`, `set_editor_text`).
- Explicit non-goals in RPC mode: `custom()` (arbitrary component), `setFooter/setHeader`, `addAutocompleteProvider`, `setEditorComponent`, themes.

Mapping to bubbletea:
1. Wire: JSON lines over stdio (or gRPC stream; AskCore already uses grpc-go, `internal/gateway`). One `ExtensionUIRequest` proto message with `oneof` matching the methods above.
2. Bridge goroutine per extension host converts each request into a `tea.Msg` via `program.Send`. The model keeps a queue of pending dialogs (`id -> request`), renders the head as the replacement UI in the live area (inline) or as an overlay layer (alt), and answers with a `tea.Cmd` that writes `extension_ui_response`. Timeouts are enforced by the agent side, per Pi, so the TUI needs no timers.
3. Widgets: `setWidget(key, lines[], placement)` maps to a `map[key][]string` rendered above or below the editor. Lines are plain ANSI strings; no live component. This is enough for statuses, progress, small dashboards.
4. Arbitrary component (`custom()`): not portable across processes. Options in ranking order: (a) do not support it out of process (same as Pi RPC); (b) declarative UI tree (JSON nodes: text, list, input, columns) rendered by the host; more work, defers to later; (c) extension owns a PTY and the host embeds its cell stream: heavy, rejected for KISS.
5. Autocomplete providers and key handlers: request/response over the same channel with a hard timeout (autocomplete must never block typing); run async via `tea.Cmd` and drop stale results by sequence number.
6. Untrusted extension text: strip or sanitize control sequences (OSC 52, kitty graphics, cursor moves) before rendering; use `x/ansi.Strip` and allow only SGR. Extension output that reaches `Println` can otherwise corrupt the cell model.

## 6. Suggested layout for cmd/tui

Constraint from CLAUDE.md: `cmd/tui` imports no `internal/*` package, so it talks to the daemon over gRPC/WS (`pkg/protocol`, `proto/`). The TUI is then a client-side package tree that only `cmd/tui` uses. Go's `internal` rule allows `cmd/tui/internal/...` (child of cmd/tui), which keeps it outside the repo `internal/` import rules. Suggestion:

```
cmd/tui/
  main.go                  # flags, connect, tea.NewProgram
  internal/
    app/                   # root model: mode (inline|alt), Update routing, View assembly
    transcript/            # block model (user, assistant, tool, diff, image), committer goroutine (owns Println), block render cache by (id,width)
    live/                  # live-area composer: pending, status, widgets above/below, editor slot, footer; width-1 clamp; height clamp
    editor/                # custom editor: buffer, undo, killring, history, wordnav, paste markers, external editor
    autocomplete/          # provider interface, fuzzy, file/command providers, popup
    overlay/               # selector, confirm, input, dialog stack; inline replacement + alt layer renderers
    render/                # markdown (glamour + stable-prefix streaming), highlight (chroma), diff, image (kitty placeholder, blocks fallback), width helpers
    theme/                 # theme struct, loader, glamour/chroma mapping
    keys/                  # action names, defaults, config parsing, key.Binding building
    extui/                 # extension UI protocol client: request/response types, pending queue
    client/                # gRPC/WS client to the daemon, event -> tea.Msg bridge
    term/                  # capability detection (kitty kbd, kitty gfx, iterm2, sync), tea.Raw helpers
    testkit/               # PTY + emulator screen driver
```

This follows the repo's dewee rule (one package per capability). Split further only when files grow.

Keep transcript state independent of screen mode so ctrl+f-style toggling (kiln) can replay the whole transcript from the block list. Pi's leave-alt-screen requirement ("print complete logical final document", `tui-plan.md`) is the same replay: on exit from alt, `Println` the whole rendered transcript.

## 7. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Inline renderer bugs (section 3) | High | Own the committer; chunk commits; PTY-emulator tests; keep option to vendor a fork with kiln-style patches; track #1822 |
| Go 1.26 directive bump | Certain | Check CI and Docker images now |
| ultraviolet has no tags; API drift | Medium | Pin pseudo-version in go.sum; avoid using ultraviolet types outside `render/` and `overlay/` |
| Terminal resize cannot re-wrap scrollback | Certain (inherent) | Accept like Pi main-screen; alt-screen mode re-wraps |
| Live area taller than terminal is silently cut from the top | Certain | Clamp height in `live/`; scroll long overlays internally |
| Image protocols in scrollback | Medium | Kitty placeholders only for v1; blocks fallback; gate by capability query |
| Editor scope (undo, kill ring, autocomplete, IME cursor) | High effort | Start from kiln pattern; do not extend textarea internals |
| Glamour streaming cost and inequality of split renders | Medium | Commit finished blocks; only live tail re-rendered; copy crush's safe-boundary approach if tail grows large |
| Crush license (FSL) if code is copied | Low | Read only; re-implement |
| Single-source evidence for the "five patches" claim (kiln) | Medium | I read the patch notes, not the diffs against upstream; verify with a PTY test before adopting any patch |

## 8. Limitations of this research

- Did not run an interactive inline-mode prototype; all inline behavior claims come from source reading and issue/patch notes.
- Did not benchmark glamour vs marked-equivalent or textarea performance.
- Did not evaluate image libraries beyond `charmbracelet/x/ansi` (kitty, iterm2, sixel) and `mattn/go-sixel` v0.0.12; `rasterm` not evaluated.
- Kiln and octo-agent are small projects; treat as evidence, not proof of maturity.
- Pi needs list from the other lane was unavailable; mapping in section 4 follows `pi/tui-plan.md` and `packages/tui/src` file names.

## Needs coverage matrix

Source of needs: `researcher-260930-2254-pi-tui.md` section A (A1-A42) and B (B1-B19). Verdicts: native = Charm/tea does it as is; partial = usable with glue; custom = write in Go, Charm gives parts; gap = nothing usable, must be built or dropped. Evidence for bubbletea facts: `tea.go`, `cursed_renderer.go`, `key.go`, `paste.go`, `mouse.go`, `color.go`, `termcap.go`, `xterm.go`, `clipboard.go`, `exec.go`, `raw.go` in charmbracelet/bubbletea v2.0.10 (github.com/charmbracelet/bubbletea); ultraviolet; lipgloss `layer.go`; bubbles textarea; crush and kiln paths as in sections 3-4. "Kiln-class fixes" means the five inline-renderer patches in section 3.

### A. Framework and widget needs

| ID | Need | Charm coverage | Verdict | Workaround / custom work | Risk |
|---|---|---|---|---|---|
| A1 | Inline diff render, native scrollback | `View` with `AltScreen=false` + ultraviolet cell diff; `tea.Println` (`cursed_renderer.go:756`) | partial | Diff is per cell (better than Pi). Commit path has open bugs (#1822) and desync after insertAbove; own committer, PTY tests, maybe vendored patches | high |
| A2 | Second alt-screen renderer, same components | `View.AltScreen` field; same Model | native | Components return strings/cells for both modes; only composition differs | low |
| A3 | CSI 2026 synchronized output | Automatic in renderer (`cursed_renderer.go:576-602`) when terminal supports it | native | None | low |
| A4 | Full redraw on width change, optional on height, clear-on-shrink | Renderer erases and redraws on frame area change (`:330-337`); ultraviolet shrink bug | partial | Kiln ultraviolet patch (erase against old buffer). No Termux height-skip switch; add via `WindowSizeMsg` filter. Cannot re-wrap committed scrollback | med |
| A5 | Append-only commit to scrollback, no repaint | `tea.Println` | partial | Same as A1; chunk commits below terminal height; single goroutine committer (kiln `internal/tui/bridge.go`) | high |
| A6 | 16 ms frame coalescing, immediate on input | Renderer runs on an fps ticker (default 60) and flushes after each message | native | Tune `WithFPS` if needed | low |
| A7 | SGR / OSC 8 leak guard per line | Cell buffer: each cell carries style and link, so leaks cannot cross lines | native | Sanitize extension text (section 5) | low |
| A8 | Width helpers (grapheme, ANSI, OSC 8, CJK, ZWJ, tabs) | `x/ansi` `StringWidth`, `Truncate`, `Wrap`, `Hardwrap`, `Cut`; `displaywidth`; mode 2027 | native | No OSC 133 awareness; width disagreement with terminal still needs width-1 cap (octo-agent PR 2467) | low |
| A9 | Component tree, invalidate, render caches | None; Elm model. Bubbles are sub-models | custom | Cache rendered strings per block keyed by (id,width,version); crush `chat` and `list` do this | med |
| A10 | VStack/HStack flex | `lipgloss.JoinVertical/Horizontal`; `ultraviolet/layout` (Cassowary constraints) | partial | No grow/shrink/basis. Use constraints or manual height math for dock + transcript; write small stack helper if extensions need it | med |
| A11 | ScrollView: follow-end, chained overscroll, scrollbar, drag | `bubbles/viewport` (scroll, `GotoBottom`, mouse wheel) | partial | No scrollbar, drag, chaining, jump label; write scrollbar renderer + hit testing (`Compositor.Hit`). Alt-screen only | med |
| A12 | Sticky dock beside scrolling transcript | Alt: viewport + fixed rows via JoinVertical. Inline: native | native | Height clamp: dock must leave min rows for transcript | low |
| A13 | Overlay stack: anchors, percent size, non-capturing, handle, focus restore | lipgloss `Canvas`/`Layer`/`Compositor` (X,Y,Z,ID, hit test); crush `dialog.Overlay` | partial (alt) / custom (inline) | Alt-screen: compositing works. Inline: no full-screen buffer, so overlays are live-area replacement or an extra region of the live area, clipped to terminal height; anchors/percent only relative to live area. Compositing over wide chars: cell buffer handles it | high |
| A14 | Single focus, hardware cursor follows text cursor (IME) | `View.Cursor` (position, shape, blink); textarea `Cursor()` | native | Focus routing is app code (one `focused` enum). Compute cursor row relative to live area | low |
| A15 | Mouse: press/release/drag/move/wheel, click counts, modifiers, dispatch | `MouseClickMsg/ReleaseMsg/WheelMsg/MotionMsg`, `Mod`; `View.MouseMode`, `OnMouse`; `Compositor.Hit` | partial | No click counts (count in app with timer); wheel accel and Alt multiplier in app; dispatch table custom. Inline: mouse mode off | med |
| A16 | App-owned selection over rendered rows, OSC 52 copy, link activation | `textarea/selection.go` covers editor only; `tea.SetClipboard` (OSC 52); OSC 8 via `x/ansi` | custom | Selection map over rendered rows in alt mode; crush `model/ui.go` has mouse selection over chat list to copy from. Inline: native terminal selection | med |
| A17 | Incremental transcript search + highlight | None | custom | Search over block plain text, highlight via style ranges (`lipgloss/ranges.go`); alt mode only | low |
| A18 | OSC 133 prompt-zone navigation | None; `x/ansi` has no helper I found | gap | Emit `ESC ] 133` markers as raw text in cells; navigation = index of block starts in app. Defer | low |
| A19 | Notification stack | None | custom | Timed list rendered as layer; trivial | low |
| A20 | Multi-line editor: wrap with cursor col, CJK, max height 30%, scroll indicators | `bubbles/textarea` (soft wrap, `DynamicHeight`, `MinHeight/MaxHeight`, `SetPromptFunc`, `LineInfo`, virtual/real cursor) | partial | Cap height by 30% of terminal in app; indicators not built in; grapheme correctness follows uniseg/displaywidth. Alternative: custom editor (kiln `internal/tui/editor`) | med |
| A21 | Emacs key set via registry | textarea has some Emacs bindings via `key.Binding` `KeyMap` (Ctrl-A/E/K/U/W etc.) | partial | Map registry to textarea `KeyMap`, fill missing ones | low |
| A22 | Kill ring with yank-pop | None in textarea | custom | Port `kill-ring.ts`; needs edit hooks textarea lacks, so custom editor is easier (kiln `editor/killring.go`) | med |
| A23 | Undo with word coalescing | None in textarea | custom | Snapshot stack around edits; custom editor | med |
| A24 | Prompt history with draft, edge-aware up/down | None | custom | Small; kiln `editor/history.go`; edge-aware using `LineInfo` | low |
| A25 | Bracketed paste + atomic large-paste markers, renumber, undo | `PasteMsg{Content}` on by default | partial | Marker registry, expansion at submit, undo integration all custom; markers imply custom editor | med |
| A26 | Sticky column, char-jump, Unicode word nav | textarea has word moves; sticky column partial via `LineInfo` | partial | Char-jump custom; word boundaries with uax29 (`clipperhouse/uax29`) | low |
| A27 | Autocomplete dropdown: async, debounced, abortable, quoted paths, stacked providers | crush `completions`, kiln `autocomplete.go` for reference; `tea.Cmd` + context for async | custom | Provider interface + seq numbers to drop stale results; popup as live-area rows/layer | med |
| A28 | Fuzzy scoring | `sahilm/fuzzy` (used by bubbles list) or port `fuzzy.ts` | partial | Pi scoring rules (consecutive bonus, gap penalty) differ; port ~100 lines | low |
| A29 | SelectList, SettingsList | `bubbles/list` (filter, pagination, delegates); `huh` select | partial | Pi columns, cycle-value submenus custom; a 100-line custom list is lighter than bubbles list | low |
| A30 | Single-line Input with scroll, kill ring, undo | `bubbles/textinput` (scroll, placeholder, paste) | partial | Kill ring/undo custom; share the editor core | low |
| A31 | Spinner, cancellable loader | `bubbles/spinner` (custom frames) | native | Cancel = key handling | low |
| A32 | Markdown -> ANSI incl. tables, task lists, streaming-safe partial fences | `glamour/v2` (goldmark + chroma) | partial | Renders whole document; wrap state resets between calls. Streaming: commit finished blocks, live tail only, or crush stable-prefix cache (`internal/ui/chat/streaming_markdown.go`). No OSC 8 fallback control, strict strikethrough, custom fence border need style tweaks or a custom goldmark renderer | med |
| A33 | LaTeX -> Unicode | None found in Go ecosystem | gap | Port `latex.ts` (large); defer, show source text | low |
| A34 | Syntax highlight per line | `chroma/v2` (formatters terminal16m/256) | native | Map theme tokens to chroma style (crush `xchroma`) | low |
| A35 | Word-level intra-line diff + coloring | `go-udiff`, `sergi/go-diff` (char/word diff) | custom | Diff widget custom (~300 lines); crush `diffview` as reference | low |
| A36 | Inline images: Kitty upload/move/delete, iTerm2, row reservation, fallback | `x/ansi/kitty` (encoder, `Placeholder`), `x/ansi/iterm2`, `x/ansi/sixel`; `tea.Raw` | partial | Kitty unicode placeholders survive scrollback and Println (crush `internal/ui/image/image.go`). Kitty placements in scroll regions, iTerm2 in scrollback, clip in ScrollView: custom or unsupported. Fallback blocks | high |
| A37 | Theme: JSON vars, 56 tokens, OKLCH, inheritance, light/dark pair, reload, system theme | lipgloss v2 styles, `LightDark`, `View.BackgroundColor`, `BackgroundColorMsg`, `colorprofile` | custom | Theme loader + token struct custom; reload via fsnotify; no inheritance/OKLCH built in | med |
| A38 | Color math: OKLCH/OKHSL, gamut map, 256 quantise | `lipgloss` `Blend1D/2D`, `Darken/Lighten/Alpha` (`lipgloss/blending.go`); `go-colorful` has Lab/LCh/OKLab; `colorprofile` downsamples | partial | OKHSL and gamut mapping custom (small); truecolor->256 is automatic | low |
| A39 | Keybinding registry: namespaced ids, overrides, conflicts, hot reload, per-platform | `bubbles/key` `Binding`, `help` | custom | Registry, overrides, conflict detection, empty-list disable custom (~200 lines); key strings via `KeyPressMsg.String()` | low |
| A40 | Extension UI API: dialogs w/ timeout/abort, status slots, widgets, header/footer/editor replacement, custom component, overlays | None. Pi RPC subprotocol as wire (`rpc-extension-ui.md`) | custom | Section 5: queue of dialogs, widget map, slot replacement. `custom()` out-of-process is not portable | high |
| A41 | Runtime swap regular/fullscreen without replay | `View.AltScreen` toggles live; renderer handles enter/exit | partial | Toggling is native, but inline scrollback cannot be un-printed. Keep block list to redraw viewport on enter and Println full transcript on exit (kiln does replay from session log) | med |
| A42 | Exit behaviour: print transcript/resume hint, restore terminal | `tea.Quit`, renderer restore; `Program.Println` after exit is not managed | native | Print final output after `p.Run()` returns using plain stdout | low |

### B. Process and environment needs

| ID | Need | Charm coverage | Verdict | Workaround / custom work | Risk |
|---|---|---|---|---|---|
| B1 | Raw mode, restore on exit and panic, paste on/off | `Program` sets raw mode via `x/term`, restores; `View.DisableBracketedPasteMode`; `WithoutCatchPanics` default recovers | native | Verify restore on SIGKILL is impossible in any lib | low |
| B2 | Stdin buffer: split CSI/OSC/mouse reassembly, lone-escape timeout, SSH override | ultraviolet decoder + reader (`terminal_reader.go`) | partial | Lone-escape timeout is internal; no env override. Test over SSH; patch if needed | med |
| B3 | Kitty keyboard negotiation, DA1 sentinel, modifyOtherKeys fallback, pop on exit | `View.KeyboardEnhancements` + `KeyboardEnhancementsMsg`; renderer pops on exit (`cursed_renderer.go:67-75`) | partial | Kitty path native. modifyOtherKeys fallback not exposed as far as I read; ultraviolet decodes CSI-u and xterm modifyOtherKeys input but negotiation of mode 2 is not in the View API. Terminals without kitty: use alt+enter | med |
| B4 | Key parser: CSI-u, modifyOtherKeys, ESC-prefix, keypad, base layout, release/repeat | `KeyPressMsg` fields `Code, Mod, ShiftedCode, BaseCode, IsRepeat`; `KeyReleaseMsg` | native | Key-id string format differs from Pi's; translate | low |
| B5 | Resize, dimension refresh after suspend, COLUMNS/LINES fallback | `WindowSizeMsg`, `RequestWindowSize`, SIGWINCH, `ResumeMsg`; `WithWindowSize` | native | COLUMNS/LINES fallback in app | low |
| B6 | Drain stdin on exit (Kitty release leakage over SSH) | Renderer resets keyboard enhancements on exit; no drain | gap | After `Run()` returns, read and discard stdin for up to 1 s (raw mode still off; use `x/term` timeouts). ~30 lines | low |
| B7 | Ctrl+Z suspend/resume | `tea.Suspend`, `SuspendMsg`, `ResumeMsg` | native | Bind key to `tea.Suspend`; Windows no-op | low |
| B8 | Windows: VT input, shift+enter helper, truecolor, right-click paste, WSL keymap | Bubbletea v2 supports Windows console (`tty_windows.go`, `signals_windows.go`); ultraviolet windows console | partial | Shift+Enter needs kitty/Windows Terminal; no native modifier-state helper; right-click paste custom | med |
| B9 | Apple Terminal Shift+Enter via native modifier state | None | gap | Requires cgo/CoreGraphics call; document fallback alt+enter or `/terminal-setup` guidance | low |
| B10 | Capability table (TERM_PROGRAM, tmux, etc.), hyperlink probe, overrides | `tea.EnvMsg`, `RequestCapability` (XTGETTCAP), `TerminalVersionMsg` (XTVERSION), `ModeReportMsg` (DECRQM), `colorprofile` | partial | Table of terminals and overrides custom (~150 lines); probes are native queries | med |
| B11 | OSC 10/11/4 color queries, DA1 sentinel, late reply, CSI ?2031 notifications | `RequestBackgroundColor/ForegroundColor` -> `BackgroundColorMsg`; `View.BackgroundColor` | partial | OSC 4 palette queries and mode 2031 not exposed; send via `tea.Raw` and parse via decoder events if ultraviolet surfaces them; 100 ms budget logic custom | med |
| B12 | Cell pixel size (`CSI 16 t`) | `WindowSizeMsg` has cells only (`screen.go:7`); pixel size not exposed in tea | gap | `tea.Raw("\x1b[16t")` and parse reply; ultraviolet defines `PixelSizeEvent` (`event.go:125`); `bubbletea/input.go` does not reference it, so it is likely not forwarded to Update; use `tea.Raw` plus `WithFilter`, or accept a gap | med |
| B13 | OSC 9;4 progress with keepalive, OSC 0 title | `View.ProgressBar`, `View.WindowTitle` | native | Keepalive: renderer re-emits? unverified; add ticker if not | low |
| B14 | Clipboard: OSC 52 write; native read of text/images/file URLs | `tea.SetClipboard` (write), `tea.ReadClipboard` (OSC 52 read, often disabled) | partial | Native read: `golang.design/x/clipboard` (cgo) or exec `pbpaste`/`wl-paste`/`xclip`; image read custom. Not evaluated in detail | med |
| B15 | `fd` dependency or Go walker with .gitignore | None in Charm | custom | Go walker with gitignore lib (`sabhiram/go-gitignore` or `go-git` ignore) or shell out to `git ls-files` / `fd`; abort by context | low |
| B16 | Multiplexer awareness: mouse downgrade, hyperlink/image gating | `EnvMsg`, XTVERSION | custom | Gate features in B10 table on `TMUX`, `STY`; tmux passthrough wrapping for kitty images is custom (`ansi.TmuxPassthrough` exists in `x/ansi/passthrough.go`) | med |
| B17 | Debug capture: raw ANSI log, redraw reasons, crash dump | `tea.LogToFile`; `WithOutput` can wrap the writer | partial | Tee writer for raw ANSI log is 20 lines; redraw-reason log custom | low |
| B18 | External editor spawn with terminal handoff | `tea.ExecProcess` (`exec.go:50`) | native | Release/restore terminal handled by tea | low |
| B19 | Settings surface consumed by TUI | viper is already in AskCore (`backend.config:go:viper`) | native | Map settings keys to a TUI options struct | low |

### Gaps and high-risk rows, priority order

Priority is by chance of forcing an architecture change, not by size of the work.

1. A1 / A5 (inline commit path; risk high). Stock `tea.Println` has open bugs (#1822 removing the view, #1666 racing clear) and desync after insertAbove that kiln patched. Everything else in inline mode sits on top of this.
2. A13 (overlays in inline mode; high). No full-screen buffer inline. Closing an overlay that shrinks the live region hit the ultraviolet erase bug (kiln patch). Anchors and percent sizes reduce to "replace or extend live area".
3. A36 (images; high). Only Kitty placeholders are safe inside `Println` and scrollback. iTerm2 and sixel break the width estimate in `insertAbove`; alt-screen placements need tracking and deletion.
4. A40 (extension UI; high). Design is clear (Pi RPC subset), but `custom()`, header/footer/editor replacement and stacked autocomplete providers need a decision on scope (unresolved question 4 of section 8).
5. A4 / A41 (resize, mode swap; med). Erase-on-shrink bug; committed scrollback never re-wraps; swap needs block list replay.
6. A20-A25 (editor core; med). Textarea lacks undo, kill ring, paste markers. Decide custom editor early; it is the largest single component.
7. A32 (streaming markdown; med). Glamour cost and wrap reset; solvable by commit-on-complete plus small live tail.
8. B3, B11, B12 (protocol negotiation; med). Kitty flags work; modifyOtherKeys fallback, OSC 4 palette, mode 2031, and pixel size replies are not in the tea API. Workarounds via `tea.Raw` plus decoder events need a check of what ultraviolet surfaces.
9. B2, B8, B14, B16 (SSH escape timeout, Windows, native clipboard read, tmux passthrough; med). Need real-terminal tests.
10. Pure gaps, low value: A18 (OSC 133), A33 (LaTeX), B6 (stdin drain), B9 (Apple Terminal modifier state).

### What the inline-mode prototype must prove first

In this order, each as a PTY-emulator test (kiln `internal/testkit/screen` is the model):

1. A1/A5: stream 2,000 lines of committed output plus a growing live tail with the editor pinned; assert screen rows, scrollback contents, and cursor position stay correct, including one commit taller than the terminal (issue #1822) and a commit while the live region shrinks (turn end).
2. A13: open a 12-row selector over a 3-row live area, close it, assert no stale rows and that scrollback is intact.
3. A4: resize narrower and wider during streaming; assert only the live area redraws and no duplicate input box (octo PR 2467 case), with CJK and emoji lines at width-1.
4. A36: one Kitty placeholder image committed via Println, then scrolled up; assert it stays visible in scrollback.
5. A41: toggle inline to alt-screen and back, replaying from the block list.
6. B3: kitty keyboard enhancement negotiation on and off (shift+enter, ctrl+enter), and paste of 2,000 lines as a single `PasteMsg`.

If tests 1-3 need more than the five kiln-class fixes, switch the committer to raw writes (own scrollback insert using `tea.Raw`, per #1666) instead of `tea.Println`.

Unverified in this section: whether bubbletea forwards ultraviolet pixel-size/palette events (B11, B12), whether progress-bar keepalive is emitted (B13), and modifyOtherKeys negotiation (B3). Check in `input.go` and ultraviolet `event.go` during the prototype.

## Sources

- bubbletea source and guide: https://github.com/charmbracelet/bubbletea (`UPGRADE_GUIDE_V2.md`, `cursed_renderer.go`, `tea.go`, `renderer.go`, `raw.go`), tag v2.0.10; pkg docs https://pkg.go.dev/charm.land/bubbletea/v2
- Issues: https://github.com/charmbracelet/bubbletea/issues/1822, /1666, /1567; discussion /1482
- lipgloss `layer.go`, `canvas.go`: https://github.com/charmbracelet/lipgloss
- bubbles textarea: https://github.com/charmbracelet/bubbles/tree/main/textarea
- ultraviolet: https://github.com/charmbracelet/ultraviolet (README architecture, `layout/`, `screen/`)
- x/ansi kitty, iterm2, sixel: https://github.com/charmbracelet/x/tree/main/ansi
- crush: https://github.com/charmbracelet/crush (`internal/ui/...`)
- kiln: https://github.com/6cclab/kiln (`third_party/*/HARNESS-PATCH.md`, `docs/architecture.md`, `internal/tui/`)
- octo-agent PR: https://github.com/open-octo/octo-agent/pull/2467
- Pi: `/Users/dale/Desktop/workspace/opensources/pi/tui-plan.md`, `packages/coding-agent/docs/rpc-extension-ui.md`, `packages/coding-agent/src/modes/rpc/rpc-types.ts`
- AskCore: `/Users/dale/Desktop/workspace/opensources/AskCore/go.mod`, `CLAUDE.md`
- Version data: proxy.golang.org `@latest` queries on 2026-09-30

## Unresolved questions

1. Is a repo-wide `go 1.26.0` directive acceptable (CI, Docker, golangci-lint action)? It is required by bubbletea v2.0.10.
2. Vendor-and-patch bubbletea/ultraviolet up front (kiln route) or start on stock `tea.Println` and add patches when a PTY test fails? I recommend stock first with the PTY test harness, vendor on first failing case.
3. Alt-screen mode in the first milestone, or inline only? Affects whether overlays need the layer/compositor path at all.
4. Does the Pi needs report (other lane) require `custom()`-style extension components? If yes, a declarative UI tree spec is needed (section 5, option b).
5. Kiln's license and crush's FSL terms: confirm before borrowing code.
6. Image support scope: kitty-only acceptable, or must iTerm2 inline images work in scrollback?

Status: DONE
Summary: bubbletea v2.0.10 + lipgloss v2.0.6 is the recommended pin and go.mod migration was verified in a scratch copy; inline mode works but needs a custom committer or vendored patches per kiln's evidence.
Concerns: Pi needs list did not exist, so cross-check against numbered needs was not done; go directive must rise to 1.26.0.
