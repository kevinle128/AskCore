# Pi TUI and interactive mode: merged inventory for the Go rewrite (S3)

Date 2026-09-30. Pi 0.99.1, commit 2bbfcca4. Target: AskCore `cmd/tui` on the Charm stack (bubbletea, Lip Gloss, Glamour, Bubbles). Crush is a design reference only (FSL-1.1-MIT, no copying).

## 0. Outcome

1. The whole TUI is a client. This file has 177 feature rows: 75 P0, 62 P1, 30 P2, 10 skip (section 2). The P0 count is large because the editor, selectors and terminal negotiation are all needed for a usable coding-agent TUI; trim it when Q3 is answered.
2. The one item that decides the architecture is the inline commit path (T-REN-01, T-REN-02). Stock `tea.Println` has open bugs. Do not start full TUI work before the six PTY tests in section 3 pass.
3. Pi's default is regular (inline, native scrollback) mode. Fullscreen (alt-screen) has existed only since 0.84.0. Whether Ask ships fullscreen in the first milestone is an open question (Q3), so every alt-screen row is P1 or P2, never P0.
4. The editor is the largest custom component. `bubbles/textarea` has no undo, kill ring or paste markers, so build a custom editor core.
5. Charm coverage by row: native 33, partial 41, custom 92, gap 7 (section 2). The gaps are OSC 133 navigation, LaTeX, Mermaid, cell pixel size, stdin drain and Apple Terminal modifier state, and iTerm2 and sixel in scrollback. Highest risk: A1/A5, A13, A36, A40.
6. Extension UI primitives (`ctx.ui.*`) are owned by another agent. This file names them only (section 13).

Source shorthand: `T/x` = `packages/tui/src/x`, `CL:N` = `packages/tui/CHANGELOG.md` line N, `CA/x` = `packages/coding-agent/src/x`, `CAD/x` = `packages/coding-agent/docs/x`, `II` = `CA/modes/interactive/interactive-mode.ts`, `PLAN` = repo `tui-plan.md`, `EC§N` = edge-cases report section N, `CHL/CHE` = late/early changelog reports. Need ids A#/B# come from the lane D report; the coverage verdicts come from the lane H matrix.

Column key. Pi status: default, active, opt-in, experimental, removed. Since: first Pi version, `?` when the reports do not give it. Tier: P0 = MVP, P1 = after MVP, P2 = late, skip = do not build.

## 1. Version pin (recommendation, needs user confirmation)

| Module | Import path | Pin | Note |
|---|---|---|---|
| bubbletea | `charm.land/bubbletea/v2` | v2.0.10 (2026-09-24) | `go 1.26.0` in its go.mod |
| lipgloss | `charm.land/lipgloss/v2` | v2.0.6 | replaces lipgloss v1.1.0 |
| bubbles | `charm.land/bubbles/v2` | v2.2.1 | textarea, viewport, spinner, key, help |
| glamour | `charm.land/glamour/v2` | v2.0.1 | goldmark + chroma |
| ultraviolet | `github.com/charmbracelet/ultraviolet` | pseudo-version (no tag) | renderer under bubbletea v2; confine its types to `render/` and `overlay/` |

Facts behind the recommendation (lane H, verified 2026-09-30 in a scratch copy, not in the repo):
- AskCore now pins bubbletea v1.3.10 and lipgloss v1.1.0, and `go.mod` carries a floor for `x/cellbuf v0.0.15`. That floor exists only because lipgloss v1 and v2 are both in the graph.
- Moving to v2 removes lipgloss v1 and cellbuf. `go get` plus `go mod tidy` and `go build ./cmd/tui` passed in the scratch copy. `golangci-lint` and `go test ./...` were not run.
- The `go` directive rises from 1.25.10 to 1.26.0 for the whole repo (CI images, Docker, golangci-lint action). This is a certain cost.
- v1 is frozen (last release 2025-09-17). Crush and kiln both run v2.0.9.
- Risk: ultraviolet has no semver tag, so the build depends on a pseudo-version.

Status: RECOMMENDATION ONLY. The user must confirm the pin and the repo-wide Go 1.26.0 bump before any go.mod change (Q1). Ranked: (A) v2 with own scrollback committer; (B) v2 on stock `tea.Println`; (C) v1, not recommended.

## 2. Coverage summary by area

| Area | Rows | P0 | P1 | P2 | skip |
|---|---|---|---|---|---|
| Rendering (REN) | 15 | 10 | 2 | 2 | 1 |
| Components (CMP) | 16 | 8 | 5 | 2 | 1 |
| Layout and composition (LAY) | 18 | 11 | 6 | 1 | 0 |
| Editor (EDT) | 20 | 11 | 6 | 2 | 1 |
| Overlays and selectors (OVL) | 13 | 4 | 6 | 3 | 0 |
| Markdown, highlight, diff (MD) | 10 | 5 | 2 | 3 | 0 |
| Images (IMG) | 6 | 0 | 5 | 0 | 1 |
| Keybindings (KEY) | 11 | 6 | 4 | 1 | 0 |
| Themes (THM) | 10 | 3 | 5 | 2 | 0 |
| TUI slash commands (CMD) | 19 | 7 | 7 | 2 | 3 |
| Terminal and platform (TRM) | 22 | 7 | 7 | 6 | 2 |
| Alt-screen (ALT) | 12 | 0 | 5 | 6 | 1 |
| Width and Unicode (WID) | 5 | 3 | 2 | 0 | 0 |
| Total (excluding 5 EXT name rows) | 177 | 75 | 62 | 30 | 10 |

Rows with a split tier such as "P0 for `/`, P1 for `@`" are counted at the higher tier. Row Charm verdicts: native 33, partial 41, custom 92, gap 7, n/a 4. Row counts here are counts of inventory rows, not of the A1-A42/B1-B19 needs.

## 3. Inline prototype gate

Purpose: prove the inline commit path before building the rest. Run each as a PTY plus terminal-emulator test (kiln `internal/testkit/screen` is the model; design evidence only, kiln has no LICENSE). Put the harness in `cmd/tui/internal/testkit/`.

| # | Test | Needs | Pass criterion |
|---|---|---|---|
| G1 | Stream 2,000 committed lines plus a growing live tail with the editor pinned. Include one commit taller than the terminal (bubbletea #1822) and one commit while the live region shrinks at turn end. | A1, A5 | Screen rows, scrollback content and cursor position are correct. The live view is never removed. |
| G2 | Open a 12-row selector over a 3-row live area, then close it. | A13 | No stale rows after close. Scrollback is intact. |
| G3 | Resize narrower and wider during streaming, with CJK and emoji lines at width-1. | A4, A8 | Only the live area redraws. No duplicate input box (octo-agent PR 2467 case). |
| G4 | Commit one Kitty Unicode-placeholder image through the committer, then scroll up. | A36 | Image stays visible in scrollback. |
| G5 | Toggle inline to alt-screen and back, replaying from the block list. | A41 | Transcript is complete after both switches. Final transcript prints on exit. |
| G6 | Kitty keyboard negotiation on and off (shift+enter, ctrl+enter). Paste 2,000 lines as one `PasteMsg`. | B3, A25 | Newline key works when negotiated and falls back to alt+enter when not. One paste message arrives. |

Decision rule: start on stock `tea.Println` and add patches only when a test fails (lane H recommendation). If G1 to G3 need more than the five kiln-class fixes (flush before insertAbove, full live redraw instead of incremental diff, cursor repaint after insertAbove, first-insert flush, ultraviolet erase-on-shrink), switch to the fallback.

Fallback: a raw-write scrollback committer. One goroutine owns all scrollback writes. It uses `tea.Raw` to insert committed lines above the live area itself, and it never mixes `\e[2J\e[3J` with `Println` (bubbletea #1666). It pre-wraps every line to width-1 and splits commits to fewer rows than the terminal height. The vendored-fork route (kiln style) is the alternative. The choice between them is an open question (Q2).

Also verify during the gate (unverified in lane H): whether bubbletea forwards ultraviolet pixel-size and palette events (B11, B12), whether the progress bar keepalive is emitted (B13), and whether modifyOtherKeys can be negotiated (B3).

## 4. Rendering model (T-REN)

Pi renders `string[]` per component and diffs lines. Bubbletea v2 renders `View()` through an ultraviolet cell buffer and diffs cells. The result is comparable for the live area. The difference is in how scrollback is committed.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-REN-01 (A1) | Inline main-screen renderer | Diff of previous vs new lines. Only the changed range is rewritten. Appended lines commit to scrollback. | default | 0.10.0 | T/tui-main-screen.ts:364-395 | Empty rows under footer after shrink (CL:852). Stale lines at zero height (CL:327). Append past viewport must commit (EC§17). | partial | P0 | `View` with `AltScreen=false` plus `tea.Println`. Open bug #1822. Own committer, gate G1. |
| T-REN-02 (A5) | Append-only scrollback commit | Finished lines are written once. Scrolled-out lines are never repainted. | default | 0.79.0 fix | CL:909 | Long block taller than the terminal. Commit order under concurrency. | partial | P0 | Single committer goroutine owns `Println`. Chunk below terminal height. Commit finished blocks only. |
| T-REN-03 (A3) | Synchronized output | Every frame wrapped in `CSI ?2026 h/l`. | default | ? | T/tui-main-screen.ts:280 | Terminals without 2026 ignore it. | native | P0 | Automatic in the v2 renderer. |
| T-REN-04 (A4) | Redraw triggers | Full clear on width change. Height change redraws except on Termux. Optional `clearOnShrink`, default off. | default | 0.51.x (clearOnShrink) | T/tui-main-screen.ts:331-361; CAD/settings.md | Termux keyboard toggle must not replay history (0.61.1). Clear screen before wiping scrollback on session switch (0.58.2). | partial | P0 | Committed scrollback cannot be re-wrapped in any implementation. Redraw the live area only. Termux skip via a `WindowSizeMsg` filter. Ultraviolet erase-on-shrink bug (kiln patch). |
| T-REN-05 (A6) | Frame coalescing | `requestRender` limited to 16 ms. Keyboard input preempts the timer. | default | 0.65.2 | T/tui.ts:507,1010-1029,1116 | Windows input latency fix (0.84.0). Tests need a deterministic clock. | native | P0 | Renderer runs at 60 fps and flushes after each message. Tune `WithFPS` only if needed. |
| T-REN-06 (A7) | Per-line style reset | Each line ends with SGR reset and OSC 8 close. Styles never leak between lines. | default | ? | T/tui.ts:412; CL:1041 | Blockquote, heading and table style leaks (EC§16). | native | P0 | Cell buffer carries style and link per cell. Sanitize extension text separately (T-EXT-02). |
| T-REN-07 (A9) | Component tree with render caches | `Container`, `invalidate()` propagation, caches keyed by (text, width). Theme change invalidates the tree. | default | ? | T/tui.ts:347; CL:35 | Components must not cache colored strings across a theme change. | custom | P0 | Elm model has no tree. Cache rendered blocks by (id, width, version, theme). Crush `chat` and `list` do this. |
| T-REN-08 (A14) | Hardware cursor for IME | Focused component emits a zero-width marker. Renderer moves the real cursor there. Cursor visibility opt-in (`showHardwareCursor`, default off). | opt-in | 0.48.0 | T/tui.ts:180-196; CL:960 | Cursor must stay correct while autocomplete is open. Hidden cursor after exit if render pending (CL:29, CL:853). | native | P0 | `View.Cursor = tea.NewCursor(x, y)`. Compute the row relative to the live area. |
| T-REN-09 (A42) | Terminal restore and exit | Restore cursor, keyboard flags, bracketed paste, mouse. Restore on panic and on SIGTERM/SIGHUP. | default | ? | T/terminal.ts; EC§17 | Panic must restore the tty. Cursor hidden after exit. Stdin lost means exit. | native | P0 | Program restores raw mode and catches panics. Print the final output after `p.Run()` returns. |
| T-REN-10 (A2) | Second renderer with the same components | Regular and fullscreen share one component set. `tuiMode` setting picks one. | default (regular) | 0.84.0 | T/tui.ts:446; CAD/settings.md:92 | Runtime swap without replaying content (CL:155). | native | P1 | `View.AltScreen` from state. One model, two compositions. See ALT rows. |
| T-REN-11 (B17) | Debug capture | `PI_TUI_WRITE_LOG` raw ANSI log. `PI_TUI_DEBUG_REDRAW` logs redraw reasons. Crash dump. `/debug` writes lines plus messages. | active | 0.14.2 (/debug) | CL:70, CL:569, CL:814; CAD/usage.md | `/debug` file may hold secrets. | partial | P1 | Tee writer through `WithOutput` (about 20 lines). `tea.LogToFile` for logs. |
| T-REN-12 (A9) | Bounded writer | Output chunked so image-heavy frames stay under limits. | active | 0.84.4 | T/tui-main-screen.ts:18; CL:102 | JS string limit only. | native | skip | Go has no string limit. A buffered writer is enough. |
| T-REN-13 (A1) | Renderer handoff | `stop({preserveScreen})` plus render-state export so the renderer can be swapped at runtime. | active | 0.84.0 | T/tui.ts:448; CA/modes/interactive/tui-renderer.ts:54-81 | Component `tui` references must stay stable. | partial | P2 | Keep transcript state independent of screen mode (block list). Do this with ALT-08 only. |
| T-REN-14 (A10) | Layout helpers | Flexbox-lite `VStack`/`HStack` with basis, grow, shrink, min, max, gap, align. Rebuilt per frame. | active | 0.84.0 | T/components/stack.ts | No wrap, no grid, no percentages. Coding-agent uses only VStack plus ScrollView. | partial | P2 | Manual height math for dock plus transcript. Add `uv/layout` constraints only if extensions need stacks. |
| T-REN-15 (n/a) | Go-specific runtime rules | Single-writer goroutine for scrollback and session persistence. Cancel through `context.Context` everywhere. Goroutine-leak test on cancel. Deterministic clock for render tests. | n/a | n/a | EC§24 items 19, 25; EC§25 | Data races in event ordering. `bufio.Scanner` 64 KB token limit breaks long JSONL or SSE lines between TUI and daemon. | custom | P0 | Use `bufio.Reader.ReadBytes` for the client stream. Run TUI tests with `-race` and `goleak`. |

## 5. Components (T-CMP)

Built-in `pi-tui` components. The Bubbles library gives only a partial base. A 100-line custom list is lighter than `bubbles/list`.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-CMP-01 (A8) | Text, TruncatedText, Spacer, Box | Word-wrapped text with padding and bg. Single-line ellipsis. Blank lines. Padded box. | default | ? | T/components/{text,truncated-text,spacer,box}.ts | Narrow widths (property test: no line wider than width for widths 1 to 200). | native | P0 | `lipgloss` styles plus `x/ansi` `Wrap`, `Truncate`. |
| T-CMP-02 (A31) | Loader | Braille spinner (10 frames, 80 ms). Custom frames and interval. Empty frames hides it. | default | ? | T/components/loader.ts:11-12; CL:79 | Timers must stop on dispose (0.67.2). Shrinking TUI under clear-on-shrink (0.80.3). | native | P0 | `bubbles/spinner` with custom frames. |
| T-CMP-03 (A31) | CancellableLoader | Loader plus Esc with an abort signal. | default | ? | T/components/cancellable-loader.ts | Esc during "Working" after retry (0.39.0). | native | P0 | Cancel is key handling plus `context.CancelFunc`. |
| T-CMP-04 (A29) | SelectList | Filter, scroll window (`maxVisible`), primary and description columns, page keys, mouse, multi-line descriptions flattened. | default | 0.10.0 | T/components/select-list.ts:12-73; CL:64 | Hover must not move the selection (11.10). Ctrl+C mashing must exit (0.11.9). | partial | P0 | 100-line custom list. Filter is prefix match. Fuzzy is done by callers. |
| T-CMP-05 (A29) | SettingsList | Rows of label and value. Enter or Space cycles values. Submenu factory. Optional fuzzy search. | default | 0.29.1 | T/components/settings-list.ts:11-80; CL:785 | Narrow-width safe. | custom | P1 | Custom cycle-value rows. Used by `/settings`. |
| T-CMP-06 (A30) | Input | Single line with horizontal scroll, prompt, placeholder, word nav, kill ring, undo, paste normalizes tabs. | default | 0.29.0 | T/components/input.ts:16-49; CL:643-648 | Wide-char-aware scroll (0.76.0). CSI-u printable decoding (0.77.0). | partial | P1 | `bubbles/textinput`. Kill ring and undo shared with the editor core (T-EDT-03). |
| T-CMP-07 (A9) | Markdown component | See section 9. | default | 0.21.0 | T/components/markdown.ts | | partial | P0 | See T-MD rows. |
| T-CMP-08 (A36) | Image component | Kitty or iTerm2 inline image with max cell size, filename fallback. | default | 0.21.0 | T/components/image.ts:17-25 | See IMG rows. | partial | P1 | See T-IMG rows. |
| T-CMP-09 (A11) | ScrollView | Vertical only. `follow: none/end`, `primary`, overscroll chain/contain, scrollbar auto/always/hidden. | active | 0.84.0 | T/components/scroll-view.ts:6-46 | Drag must not snap back. | partial | P1 | `bubbles/viewport` plus custom scrollbar. Alt-screen only. |
| T-CMP-10 (A15) | MouseRegion | Wrap a child with a mouse callback. | active | 0.84.0 | T/components/mouse-region.ts:16 | | partial | P2 | Alt-screen only. `Compositor.Hit`. |
| T-CMP-11 (A19) | Flash notifications | Stacked transient messages (`flash(message, ms)`), alt-screen only. | active | 0.84.0 | T/components/alt-screen-flash.ts; CL:163 | | custom | P2 | Timed list rendered as a layer. Trivial. |
| T-CMP-12 (n/a) | Assistant message | Markdown plus thinking blocks (collapsible, hidden label). Streams via `updateContent`. | default | 0.10.0 | CA/modes/interactive/components/assistant-message.ts:47-91 | Reuse the streaming component, do not replace it. Partial code fence must not flicker. | custom | P0 | Block model in `transcript/`. Commit when complete, live tail while streaming. |
| T-CMP-13 (n/a) | Tool execution | Pending, success and error backgrounds. Tool-supplied `renderCall`/`renderResult`. Fallback 10-line preview. Expand toggle. Images. | default | 0.23.0 | CA/.../tool-execution.ts:17-40,166 | Edit errors rendered twice (0.63.1). `renderShell: "self"` for stable large diffs (0.67.3). | custom | P0 | For built-in tools use Go renderers. Out-of-process renderers need a decision (Q4). |
| T-CMP-14 (n/a) | Bash execution | Streaming output. 20-line preview. Visual-line truncation shared with tool output. Collapsed view shows the last N lines. | default | 0.22.3 | CA/.../bash-execution.ts:19,131 | Binary output. OSC 133 marks corrupting the terminal (0.12.11). | custom | P0 | Strip ANSI with `x/ansi.Strip` before display. |
| T-CMP-15 (n/a) | Message variants | User, custom, skill invocation, branch summary, compaction summary, custom-entry, status, keybinding hints. | default | ? | CA/modes/interactive/components/*.ts | | custom | P1 | One block type per variant. Keep the list open for extension entry renderers. |
| T-CMP-16 (n/a) | Decoration and easter eggs | Logo, announcement, dynamic border, `armin`, `daxnuts`. | active | ? | CA/modes/interactive/components/ | | custom | skip | Not needed. A simple startup header is enough (T-LAY-08). |

## 6. Interactive-mode composition (T-LAY)

Regular mode appends the same component instances vertically: header, loaded resources, chat, pending, status, widgets, editor, footer (II:597-624). Fullscreen puts the transcript in a ScrollView and a fixed dock (pending, status, widgets, editor, widgets, footer) below it.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-LAY-01 (A12) | Transcript then live area | Finished blocks in scrollback. Live area holds pending, status, widgets, editor, footer. | default | 0.10.0 | II:597-624; PLAN | Live area taller than the terminal is cut from the top by bubbletea. | native | P0 | Clamp height in `live/`. Scroll long overlays internally. |
| T-LAY-02 (n/a) | Streaming update model | Streaming calls `requestRender`. The streaming component is updated, not replaced. Editor is always visible. | default | 0.10.0 | T/tui.ts:1010-1116; assistant-message.ts:47-91 | Streaming must not wipe unsent editor input (0.37.4, 0.51.0). | custom | P0 | Commit finished blocks. Re-render only the last N rows each delta. |
| T-LAY-03 (n/a) | Pending messages area | Queued steering and follow-up messages shown above the editor. `Alt+Up` restores them to the editor. | default | 0.32.0 | II; CAD/usage.md | Queue survives compaction. Abort returns queued messages to the editor. | custom | P0 | Queue state comes from the daemon; the TUI renders it. |
| T-LAY-04 (n/a) | Status line and working indicator | "Working" message with spinner. Message, visibility and indicator frames are settable. Hidden-thinking label. | default | ? | II:2394 | Empty frames hides the indicator. | custom | P0 | `live/` slot. Extension setters by name only (T-EXT-01). |
| T-LAY-05 (n/a) | Widgets above and below editor | Keyed lists of lines. String widgets capped at 10 lines. Component widgets by factory. | default | 0.31.0 (setStatus) | II:606-607,2394; CA/core/extensions/types.ts:188-193 | | custom | P1 | `map[key][]string` per placement. Component widgets defer to the extension-UI lane. |
| T-LAY-06 (n/a) | Footer | cwd with `~`, git branch (watched), session name, token stats up/down/cache read/write, cache hit %, cost, context % colored, model, thinking level, extension statuses. | default | 0.10.2 | CA/modes/interactive/components/footer.ts:60-140 | Context % is unknown after compaction until the next response. NaN tokens (0.11.5). Cost must include pre-compaction messages (0.31.0). Narrow width overflow. | custom | P0 | Data comes from daemon events. Cache footer totals for long sessions. |
| T-LAY-07 (n/a) | Editor border tint | Border color follows thinking level. `bashMode` tint after `!`. | default | ? | II:3021-3056,3244-3255 | | custom | P1 | Theme tokens `thinking*` and `bashMode` (T-THM-02). |
| T-LAY-08 (n/a) | Startup header | Lists loaded resources (context files, skills, prompts, themes, extensions). `quietStartup` hides it. `--verbose` overrides. | default | ? | II; CAD/settings.md | `[Themes]` block removed (0.99.0). | custom | P1 | Plain text block committed once. |
| T-LAY-09 (n/a) | Tool output expand | `app.tools.expand` (ctrl+o) toggles tool output. `getToolsExpanded` and `setToolsExpanded` for extensions. | default | ? | CAD/keybindings.md; types.ts:296-299 | Expand cannot re-render committed scrollback in inline mode. | partial | P0 | Inline: expanded state applies to blocks not yet committed. Decide policy (Q5). |
| T-LAY-10 (n/a) | Thinking block toggle | `ctrl+t` hides or shows thinking. `hideThinkingBlock` setting. | default | ? | CAD/keybindings.md; settings.md | Same commit limit as tools expand. | partial | P0 | Same policy as T-LAY-09. |
| T-LAY-11 (n/a) | Pending and abort flow | Enter steers, Alt+Enter follows up, Esc aborts. Abort returns queue to editor. | default | 0.32.0 | CAD/usage.md; CAD/keybindings.md | Typed text must not be wiped. Pre-loop early input buffered (0.78.0). | custom | P0 | Key routing in `app/`. |
| T-LAY-12 (n/a) | Clear and exit keys | `ctrl+c` clears then exits on second press. `ctrl+d` exits on empty editor. Double-Escape opens tree or fork (`doubleEscapeAction`). | default | 0.12.14 | CAD/keybindings.md; settings.md | Ctrl+D drains stdin up to 1 s over SSH (B6). | native | P0 | `tea.Quit`. |
| T-LAY-13 (n/a) | Console title | `OSC 0` window title, set to session name. | active | 0.32.0 | T/terminal.ts:529 | | native | P1 | `View.WindowTitle`. |
| T-LAY-14 (n/a) | Notifications | `notify(message, level)` appears as a status line entry. | default | 0.31.0 | types.ts:160 | | custom | P1 | Timed line above the editor. |
| T-LAY-15 (n/a) | Event-driven UI state | TUI reacts to `queue_update{steering,followUp}`, `thinking_level_changed`, `session_info_changed`, `compaction_start/end`, `auto_retry_start/end`, `summarization_retry_*`, `agent_settled`. | default | 0.75.4-0.87.0 | CAD/json.md (docs report §15) | `agent_end` is not done; use `agent_settled`. `willRetry` on `agent_end`. Compaction lifecycle must not leave stale status. | custom | P0 | One reducer in `client/` maps daemon events to `tea.Msg`. Show compaction and retry as status-line text. |
| T-LAY-16 (n/a) | Virtual model display | Footer shows selected and routed model, for example `auto • high -> gpt-5.6-luna • medium`. `/session` shows cost per physical model. Virtual models appear in `/model`. | experimental | 0.99.0 | CAD/virtual-models.md | Context usage uses the last responding physical model. Sleeping llama.cpp models wake on select. | custom | P2 | Only if Ask ships virtual models. Footer needs a second model field. |
| T-LAY-17 (B19) | Display settings surface | `outputPad` (0/1, default 1), `showCacheMissNotices`, `treeFilterMode`, `hideThinkingBlock`, `collapseChangelog`, `warnings.anthropicExtraUsage`, `quietStartup`, `editorPaddingX`, `autocompleteMaxVisible`. In-code only env: `PI_CLEAR_ON_SHRINK`, `PI_TUI_DEBUG`. | default | various | CAD/settings.md (docs report §9, §10) | `/settings` edits the common ones. Project settings need trust. | native | P1 | viper maps keys to a TUI options struct. |
| T-LAY-18 (n/a) | Input while busy | Messages typed during compaction, summary or retry are delivered later and must not clobber the editor. `prompt` while streaming is an error unless steer or follow-up. | default | 0.37.4-0.84.0 | EC§24 item 28 | Tree summarization must not overwrite typed content (0.51.0). Queue survives compaction. | custom | P0 | Editor text is owned by the user; queue state is owned by the daemon. |

## 7. Editor (T-EDT)

Pi's editor is a bordered multi-line Emacs-style editor of about 2,470 lines. It has no vim mode, no redo, no selection and no clipboard cut/copy (T-EDT-20).

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-EDT-01 (A20) | Layout and wrap | Rules above and below, no side border. paddingX 0-3. Word wrap reserves a cursor column. Max height 30% of terminal (min 5). Scroll indicators in borders. | default | 0.47.0 | T/components/editor.ts:520-630; CL:982 | CJK and mixed Latin/CJK wrap. ZWJ, regional indicators, Indic, Thai/Lao (EC§16). Large `setEditorText` (0.47.0). | partial | P0 | Custom editor core (kiln `internal/tui/editor` pattern). Indicators are custom. |
| T-EDT-02 (A21) | Emacs key set | ctrl+a/e/b/f/d/k/u/w/y, alt+b/f/d/y, alt+backspace, word moves, page keys. Enter submits. Shift+Enter or ctrl+j inserts a newline. | default | 0.33.0 (configurable) | T/keybindings.ts; CL:254,865-867 | Ctrl+I equals Tab (0.32.0). Caps/Num Lock bits in modifiers. | partial | P0 | Bind through the registry (T-KEY-01). |
| T-EDT-03 (A22) | Kill ring | Ring buffer. Consecutive kills accumulate (prepend for backward). Yank and yank-pop. | default | early 0.x | T/kill-ring.ts:8-46; CL:949 | Readline ctrl+w skips trailing whitespace (0.76.0). | custom | P1 | Port `kill-ring.ts`. Kiln `editor/killring.go` shows the pattern. |
| T-EDT-04 (A23) | Undo | Snapshot stack with fish-style word coalescing. Restores the paste registry with the text. No redo. | default | early 0.x | T/undo-stack.ts:7; CL:212,941 | Marker registry corruption on delete or undo (11.9). | custom | P1 | Snapshot on each edit. Also used by Input. |
| T-EDT-05 (A24) | Prompt history | In-memory, newest first, max 100, no consecutive duplicates. Up/Down at edges browse. Cursor lands at start going up and end going down. Draft restored. Dedicated `historyPrevious/Next` actions. | default | 0.12.12 | T/components/editor.ts:427-437; CL:284,318,325 | Non-empty draft goes to line start first (0.79.5). Persistence is not in the editor. | custom | P0 | Persist history in the daemon or a file. Kiln `editor/history.go`. |
| T-EDT-06 (A26) | Sticky column and char jump | Vertical moves keep the visual column, also across paste markers. `ctrl+]` jumps forward, `ctrl+alt+]` backward. | default | early 0.x | CL:836,866 | | partial | P1 | Use `LineInfo`-like data from the custom editor. |
| T-EDT-07 (A26) | Word boundaries | Unicode segmenter but ASCII punctuation boundaries kept. | default | 0.29.0 | T/word-navigation.ts; CL:362 | | partial | P1 | `clipperhouse/uax29`. |
| T-EDT-08 (A25) | Bracketed paste | Buffered. Single-line paste inserted atomically (no per-char autocomplete). Tabs become spaces. | default | 0.11.2 | T/components/editor.ts:1296-1312; CL:631,639 | Paste text that looks like key events (`aa:bb:3F`, 0.42.5). CSI-u inside bracketed paste (0.56.0). | native | P0 | `tea.PasteMsg`. Bracketed paste is on by default. |
| T-EDT-09 (A25) | Large-paste markers | Over 10 lines or over 1000 chars becomes an atomic marker `[paste #N +L lines]` or `[paste #N C chars]`. Markers are indivisible. Registry renumbers on delete. `getExpandedText()` restores originals at submit, queue, export and external editor. | default | 0.34.0-0.5x | T/components/editor.ts:30-34,1296-1416; CL:212,631,706 | Deleted or undone markers on submit (EC§18). | custom | P0 | Marker registry in the editor core. Expand on submit. |
| T-EDT-10 (A25) | Path paste | Auto-space before a pasted path starting `/`, `~`, `.` after a word char. | default | 0.29-0.31 | CL:1208 | | custom | P2 | Small. |
| T-EDT-11 (A27) | Autocomplete dropdown | `SelectList` under the editor. `autocompleteMaxVisible` 3-20 (default 5). Tab or Enter accepts. Re-queries on cursor move. | default | 0.10.0 | T/components/editor.ts:247-250; CL:317,328 | IME cursor while the menu is open. Tab after a command name must not chain. | custom | P0 | Popup rows in the live area. Crush `completions` and kiln `autocomplete.go` as design refs. |
| T-EDT-12 (A27) | Autocomplete triggers | `/` only on the first line and not mid-word. `@` and `#` for files and attachments. Providers declare `triggerCharacters`. Tokens may follow `( [ { < \``. Quoted paths for spaces. Forced Tab completion auto-applies a single match. | default | 0.10.0 | T/components/editor.ts:253-267; CL:781,910,313,44 | Slash rule changed twice (anywhere, empty only, first line). Keep first line. | custom | P0 | Provider interface with trigger set. |
| T-EDT-13 (A27) | Autocomplete engine | Slash commands (fuzzy), async argument completions, path completion (`~`, `./`, drive letters), `@` fuzzy search through external `fd`, 20 ms debounce, abortable. | default | 0.11.1 (fd) | T/autocomplete.ts:148,194,303,770; CL:468,23 | Cancel in-flight search. Hidden files and symlinks. CJK punctuation. Ranking exact first, shallow first. | custom | P0 for `/`, P1 for `@` | `tea.Cmd` with context plus sequence numbers to drop stale results. Stacked extension providers by name only (T-EXT-01). |
| T-EDT-14 (B15) | File search backend | `fd` binary, .gitignore aware, follows symlinks, includes hidden. | default | 0.11.1 | T/autocomplete.ts:148-194 | Termux package name for fd. No `fd` means no `@` search. | custom | P1 | Go walker with gitignore lib, or `git ls-files`. No external binary. |
| T-EDT-15 (A28) | Fuzzy match | Consecutive-match bonus, gap penalty, exact match first, slash-separated queries. | default | early 0.x | T/fuzzy.ts:12-138; CL:1035,63 | | partial | P0 | Port the scoring (about 100 lines). `sahilm/fuzzy` scores differently. |
| T-EDT-16 (n/a) | Editor mouse | Click positions the cursor. | active | 0.84.0 | T/components/editor.ts:632-694 | | custom | P2 | Alt-screen only. |
| T-EDT-17 (B18) | External editor | `ctrl+g` opens `$VISUAL`, `$EDITOR` or a platform default and reads the result back. `externalEditor` setting. | default | 0.25.3 | CA/modes/interactive/external-editor.ts; CAD/keybindings.md | `EDITOR="code --wait"` needs shell parsing. Paste markers expanded first. Windows Notepad default. | native | P1 | `tea.ExecProcess`. |
| T-EDT-18 (n/a) | `!` and `!!` modes | `!cmd` runs bash and adds the output to context. `!!cmd` runs without sending output. Border tint `bashMode`. Dropped file paths are quoted. | default | 0.14.0 / 0.32.1 | II:3021-3056 | Cancel all running user commands (0.83.0). No `PI_*` session env. | custom | P0 | Editor detects the prefix; execution is a daemon operation. |
| T-EDT-19 (n/a) | App-level wrapper | Paste image (ctrl+v), interrupt (esc), exit on empty, history actions, then base editor. | default | ? | CA/.../custom-editor.ts:88-146 | | custom | P0 | Key router before the editor core. |
| T-EDT-20 (n/a) | Not present in Pi | No vim keys, no shift-selection, no redo, no cut/copy in the editor, no line numbers. `VimEditor` is only an extension example. | n/a | n/a | CA/core/extensions/types.ts:257-276 | | n/a | skip | Do not build. |

## 8. Overlays and selectors (T-OVL)

Overlays are the highest-risk area for inline mode. Bubbletea has no full-screen buffer inline, so an overlay becomes a replacement of, or an extension to, the live area.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-OVL-01 (A13) | Overlay options | `width`/`maxHeight`/`row`/`col` as number or `"NN%"`, `minWidth`, 9 anchors, offsets, margins, `visible(w,h)`, `nonCapturing`. | experimental to active | 0.39.0 | T/tui.ts:203-280 | Overlay wider than terminal. Center-anchored overlay re-centers on resize (0.5x, CL:911). | partial | P2 | Alt-screen: lipgloss `Canvas`/`Layer`. Inline: anchors and percents reduce to "live-area replacement". |
| T-OVL-02 (A13) | Overlay stack and focus | Stack with focus order. Focused one drawn on top. Pre-focus target restored on hide. Non-capturing ones skipped on restore. | active | 0.57.0 | T/tui.ts:326-345; CL:667,334 | Cursor hidden after overlay close during shutdown (0.99.0). | custom | P1 | One `focused` enum plus a dialog stack in `overlay/`. |
| T-OVL-03 (A13) | ANSI-safe compositing | Overlay lines spliced into base lines by column. Survives CJK wide cells at the start column, tabs, OSC 8, over-wide lines. | active | 0.39.0 | T/tui.ts:415; CL:294,335,1006 | Overlay padding inflating scrollback on widen (0.50.0). | native | P2 | Cell buffer handles wide cells. Alt-screen only. |
| T-OVL-04 (A13) | Selector replaces editor | Selectors and extension dialogs replace the editor in `editorContainer` instead of overlaying. | default | 0.10.0 | II:2633-2762 | Closing a shrinking region left stale rows (kiln ultraviolet patch). | custom | P0 | This is the inline model: swap the editor slot for the selector, clamp to terminal height. Gate G2. |
| T-OVL-05 (A29) | Model selector | `/model`, ctrl+l. Only models with usable auth. Fuzzy filter. Reloads `models.json` on open. Ctrl+S saves default. | default | 0.10.0 | CA/modes/interactive/components/; CAD/models.md | Model list must not multiply by thinking level (0.37.0). | custom | P0 | Data from the daemon. |
| T-OVL-06 (A29) | Scoped-models selector | Enable all (ctrl+a), clear (ctrl+x), toggle provider (ctrl+p), reorder (alt+up/down). | default | 0.10.2 | CAD/keybindings.md | Order kept. Still reachable after logout. | custom | P1 | |
| T-OVL-07 (A29) | Settings selector | `/settings` with submenus and cycle values. | default | 0.29.1 | CA/.../components; CAD/slash-commands.md | Replaced the separate `/thinking`, `/theme`, `/queue`, `/autocompact`, `/show-images` commands in 0.29.1. `/thinking` exists again in 0.99. | custom | P1 | Reuse T-CMP-05. |
| T-OVL-08 (A29) | Thinking selector | `/thinking [lvl]`, Shift+Tab cycles, ctrl+s saves. Levels off to max. Clamped to model. | default | 0.10.0 | CAD/keybindings.md | Level preserved across non-reasoning models. `off` must truly disable. | custom | P0 | |
| T-OVL-09 (A29) | Session picker | `/resume`. Search, rename (ctrl+r), delete (ctrl+d, uses `trash`), toggle path (ctrl+p), sort (ctrl+s), named only (ctrl+n). | default | 0.12.12 | CAD/sessions.md; keybindings.md | Rename must not reorder (0.50.0). Stored cwd deleted. | custom | P0 | |
| T-OVL-10 (A29) | Tree selector | `/tree`. Fold/unfold, label edit, filters (default, no-tools, user-only, labeled-only, all), timestamps toggle. Offers a branch summary. | default | 0.31.0 | CAD/sessions.md; keybindings.md | Deep branches cost quadratic time (0.79.9). Navigation racing compaction (0.86.0). Selecting a user message puts its text in the editor. | custom | P1 | Needs the session tree API. |
| T-OVL-11 (A29) | Dialogs | Login (OAuth and API key), OAuth callback, trust prompt, first-time setup, config selector, show-images. | default | ? | CA/modes/interactive/components | Headless login: paste URL or code. Callback port in use falls back to paste. | custom | P1 | |
| T-OVL-12 (A40) | Extension dialogs | `select`, `confirm`, `input`, `editor` with abort and timeout (live countdown). | default | 0.38.0 | types.ts:113-119,151-157,240 | Name only here (T-EXT-01). | custom | P1 | Rendered through the same editor-slot swap. |
| T-OVL-13 (A29) | Config selector | `pi config` TUI enables or disables discovered resources and built-in extensions. Tab switches user and project scope. | default | 0.50.0 | CAD/packages.md; cli.md | Project scope needs trust. | custom | P2 | Reuse the settings list. Only if Ask has resource packages. |

## 9. Markdown, highlighting and diff (T-MD)

Pi keeps highlighting, diff and theme in coding-agent, not in pi-tui. Glamour renders the whole document and resets wrap state between calls.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-MD-01 (A32) | Markdown blocks | Headings (H1 underline), lists (nested, task, loose), blockquotes, fences with border and indent, tables (dividers, wrapped cells), hr. | default | 0.21.0 | T/components/markdown.ts:200-230,529; CL:573,401,896,698 | Blockquote and table style leaks. List marker preservation. Huge files. Render at every prefix of a document (EC§16). | partial | P0 | `glamour/v2`. Map Pi theme tokens to a glamour style. |
| T-MD-02 (A32) | Streaming stability | Partial closing fence must not shrink or flicker a code block. Partial `$` math stays pending. | default | 0.84.x | CL:262; T/components/markdown.ts:47-58 | | partial | P0 | Inline: commit finished blocks, re-render only the live tail. Crush's stable-prefix cache is the fallback. |
| T-MD-03 (A32) | Inline styles and links | Bold, italic, strike (`~~x~~` only), underline, code. Links use OSC 8, or `text (url)` when unsupported. Email autolinks without `mailto:`. | default | 0.11.x | T/components/markdown.ts:200; CL:495,930 | OSC 8 forced off under screen, probed under tmux. HTML shown as text. | partial | P1 | Glamour has limited link control. Custom goldmark renderer if needed. |
| T-MD-04 (A32) | Markdown options and transform | Preserve ordered markers and backslash escapes. `transform(markdown, width)`. `codeBlockIndent`. Display-only markdown transformer for extensions. | default | 0.84.0 | T/components/markdown.ts:220-229; CHL | | custom | P2 | Name only for the extension hook (T-EXT-01). |
| T-MD-05 (A34) | Syntax highlighting | `highlightCode(code, lang)` hook filled with `cli-highlight`. Language validated first. 9 `syntax*` tokens. | default | ? | CA/.../theme.ts:1001-1018,1111-1126 | Do not auto-detect language (prose mislabelled, 0.62.0). | native | P0 | `chroma/v2`. Map the syntax tokens to a chroma style. |
| T-MD-06 (A35) | Diff view | Parses `+123 content` lines. Colors added, removed, context. Word-level inverse highlight only when exactly one removed and one added line. | default | 0.10.0 | CA/.../diff.ts:1-113 | Windows CRLF preview artifacts (0.56.2). Large diff stability. | custom | P0 for unified, P1 for word-level | `go-udiff` plus chroma plus lipgloss (about 300 lines). Crush `diffview` as design ref. |
| T-MD-07 (A33) | LaTeX to Unicode | Inline and display math: fractions, scripts, symbols, aligned, cases, matrices. 1,506 lines. | active | 0.84.0 | T/latex.ts; CL:153,75,44 | Many fixes 0.84 to 0.86. | gap | P2 | No Go library found. Show the source text. Probably out of scope. |
| T-MD-08 (A32) | Mermaid to Unicode | Top-level mermaid blocks replaced by Unicode diagrams. Modes off/final/streaming (default streaming). | active | 0.84.0 | CA/.../mermaid.ts:2,59; CAD/settings.md:111 | | gap | P2 | Show source. Do not port `grok-mermaid`. |
| T-MD-09 (A32) | Render perf | Render cache by (text, width). Token reuse across theme and width changes. | default | ? | CL:35 | Streaming highlight froze the UI on large `write` args (0.54.2). | custom | P1 | Block-level cache in `transcript/`. |
| T-MD-10 (n/a) | Tool output truncation display | Visual-line truncation. Expand for full output. 2,000 lines / 50 KB tail for the model. | default | 0.22.3 | CA/.../visual-truncate.ts | Trailing newline counted as a hidden line (0.50.0). | custom | P0 | Truncation is a daemon rule; the TUI shows the notice. |

## 10. Images (T-IMG)

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-IMG-01 (A36) | Protocols | Kitty graphics and iTerm2 `OSC 1337`. No sixel. | default | 0.21.0 | T/terminal-image.ts:198-199 | Kitty requires PNG (convert JPEG/GIF/WebP/BMP). | partial | P1 | `x/ansi/kitty`, `x/ansi/iterm2`. Do not add sixel. |
| T-IMG-02 (A36) | Kitty in scrollback | Image lines reserve rows. Stale image ids deleted on redraw. Placement moves without retransmit (alt-screen). | default | 0.21.0 | T/terminal-image.ts:201-215; T/tui-main-screen.ts:196-262 | Image escape anywhere in a line. Image ids bounded. | partial | P1 | Unicode placeholders keep the image in ordinary cells, so it survives `Println` and scrollback (crush `internal/ui/image/image.go`). Gate G4. |
| T-IMG-03 (B10) | Protocol detection | Env table: Kitty, Ghostty, WezTerm, Warp = kitty. iTerm2 = iterm2. Windows Terminal, Alacritty, VS Code, Zed = none. tmux, screen, cmux = none. Overrides `PI_IMAGE_PROTOCOL`, `terminal.images`. | default | 0.21.0 | T/terminal-image.ts:75-159; CL:416,86,271 | Ghostty in tmux needs an env var. WezTerm row-clear workaround. | partial | P1 | Env table plus a kitty query (`a=q`). Shared with T-TRM-06. |
| T-IMG-04 (B12) | Cell pixel size | `CSI 16 t` reply parsed exactly to compute image rows. Default width 60 cells. | default | 0.5x | T/tui.ts:965; CL:405,558 | Reply parser held back bytes and broke Ctrl+C on Apple Terminal (0.49.3). | gap | P1 | `tea.Raw("\x1b[16t")` plus a raw-input filter. Fallback to a fixed cell ratio. |
| T-IMG-05 (n/a) | Image fallback and settings | Text placeholder with a short clickable path. `terminal.showImages`, `imageWidthCells`, `images.blockImages`, `images.autoResize`. | default | 0.37.3 | CL:193; CAD/settings.md | Pasted image placeholders were removed in 0.34.0; the file path is inserted. | custom | P1 | Half-block rendering as a portable fallback (crush uses it). |
| T-IMG-06 (n/a) | iTerm2 and sixel in scrollback | iTerm2 OSC 1337 through `Println` breaks the width estimate in `insertAbove`. | n/a | n/a | lane H matrix A36 | | gap | skip | Kitty only in v1. Revisit only on demand (Q6). |

## 11. Keybindings and keybinding config (T-KEY)

Pi has one global `KeybindingsManager`. There are 46 `tui.*` ids (editor 21, input 4, select 6, altScreen 15) and about 45 `app.*` ids. The counts were tallied by hand. Verify with a script before using them as a spec.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-KEY-01 (A39) | Registry | Definitions `{defaultKeys, description}`. User overrides shadow defaults and never evict unrelated ones. Conflict reporting. Empty list disables. | default | 0.33.0; namespaced 0.61.0 | T/keybindings.ts; CL:597,1152 | Overrides must shadow globally (0.49.1). Extension shortcut conflicts at startup. | custom | P0 | About 200 lines on top of `bubbles/key`. |
| T-KEY-02 (A39) | Config file | `<agent-dir>/keybindings.json` maps `id` to a key or key list. `/reload` applies it. | default | 0.33.0 | CAD/keybindings.md:5-26 | Auto-migration of old ids at 0.61.0. | custom | P0 | JSON into `map[string][]string`, then `key.Binding`. Agent dir only. |
| T-KEY-03 (B4) | Key syntax | `modifier+key`. Modifiers ctrl, shift, alt, super. Letters, digits, named keys, f1-f12, 32 symbols. `super` needs the Kitty protocol. | default | 0.33.0 | CAD/keybindings.md "Key syntax"; T/keys.ts | `isXxx()` detectors removed (0.33.0). | native | P0 | `KeyPressMsg.String()`. Translate Pi's id format. |
| T-KEY-04 (A39) | Namespaces | `tui.editor.*`, `tui.input.*`, `tui.select.*`, `tui.altScreen.*`, `app.*`. Namespaced ids are part of the config contract. | default | 0.61.0 | T/keybindings.ts | Keep ids stable; they leak into `keybindings.json` and extensions. | custom | P0 | Keep Pi ids where the feature exists. Decide names before publishing (CHE lesson). |
| T-KEY-05 (A39) | Per-platform defaults | Windows and WSL: undo ctrl+z (alt+z WSL), follow-up ctrl+q, dequeue alt+q, paste alt+v, search ctrl+f, cycle-back alt+p, suspend none. | default | 0.55.x | CAD/keybindings.md | Windows Terminal reserves Alt+Enter. | custom | P1 | Defaults table keyed by GOOS and WSL detection. |
| T-KEY-06 (n/a) | Default app map | interrupt esc, clear ctrl+c, exit ctrl+d, external editor ctrl+g, paste ctrl+v, model ctrl+l, cycle ctrl+p, thinking shift+tab, tools ctrl+o, thinking toggle ctrl+t, copy ctrl+x, follow-up alt+enter, dequeue alt+up. | default | various | CAD/keybindings.md | See CHL and section 7 of docs report for the full table. | custom | P0 | Ship the default table as data. |
| T-KEY-07 (A21) | Input and select maps | Newline shift+enter or ctrl+j. Submit enter. Select up/down/page/confirm/cancel (esc or ctrl+c). | default | 0.33.0 | CAD/keybindings.md | Ctrl+C mashing must exit any selector. | custom | P0 | |
| T-KEY-08 (n/a) | Tree and session keys | Tree fold/unfold, label (shift+l), filters (ctrl+d/t/u/l/a/o). Session picker keys. Scoped-model keys. | default | 0.31.0 | CAD/keybindings.md | Keys overlap across contexts (ctrl+d means exit, delete and filter). Context-scope the bindings. | custom | P1 | |
| T-KEY-09 (n/a) | Alt-screen key map | `tui.altScreen.*`: page, half page, line, prompt jump, search, top, bottom. Shadows unmodified editor PageUp/PageDown/Home/End. | default | 0.84.0 | T/keybindings.ts; CL:181 | Ctrl variants remain for the editor. | custom | P2 | Only with ALT rows. |
| T-KEY-10 (n/a) | `/hotkeys` | Shows the effective keymap. | default | 0.24.0 | CAD/slash-commands.md | | custom | P1 | `bubbles/help` or a custom table. |
| T-KEY-11 (A39) | Misc app-key rules | `ctrl+x` copies the selected message in `/tree`, else the last assistant message. `ctrl+backspace` deletes in the session picker only on an empty query. `app.suspend` shows a status message when bound on native Windows. Select `copy` is `ctrl+c`. | default | 0.61.0 | CAD/keybindings.md | Same key means different things per context (`ctrl+d` exit, delete, filter). | custom | P1 | Scope bindings per context in the registry. |

## 12. Themes (T-THM)

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-THM-01 (A37) | Theme file | JSON `name`, `appearance?`, `vars`, `colors`, `export?`. 56 tokens, 51 required. Variables chain. Missing or circular is invalid. | default | 0.10.0 (tokens grew 46, 50, 56) | CAD/themes.md:52-74; theme-schema.json | Custom themes must add new tokens (0.14.0, 0.31.0). Give defaults for missing tokens. Name unique, no `/`, not `system`. | custom | P0 | One Go theme struct with `color.Color` fields. |
| T-THM-02 (A37) | Token groups | accent, border x3, status colors, muted/dim/text, selectedBg, scrollbar, search, user and custom message, tool bg/title/output, md x10, toolDiff x3, syntax x9, thinking x7, bashMode. | default | ? | theme-schema.json | | custom | P0 | Ship dark and light. Cut tokens for skipped features (scrollbar, search) until they exist. |
| T-THM-03 (A37) | Optional inheritance | scrollbarTrack to muted, scrollbarThumb to text, searchMatchBg to selectedBg, searchMatchText to text, thinkingMax to thinkingXhigh. | default | 0.84.x | CAD/themes.md:96-105 | | custom | P1 | |
| T-THM-04 (A38) | Color forms | `#rgb`, `#rrggbb`, `oklch()`, `okhsl()`, 0-255 index, var ref, `""` = terminal default. | default | 0.99.0 (oklch) | CAD/themes.md:62-70; T/colors.ts | Truecolor vs 256 quantization. | partial | P1 | Hex and index are P0. OKLCH/OKHSL and gamut map custom (small). `colorprofile` downsamples automatically. |
| T-THM-05 (A37) | `system` theme | Default. Queries terminal fg, bg and 16 ANSI colors (100 ms budget, late replies applied). Derives hues, enforces WCAG 4.5:1. Rebuilds on light/dark change. | default | 0.99.0 | CAD/themes.md:7-22; T/tui.ts:1466 | Batched color-scheme replies (0.99.0). Fallback: bg only, then ANSI indices. | partial | P1 | `RequestBackgroundColor` covers bg/fg. OSC 4 palette and mode 2031 go through `tea.Raw`. Ship dark and light first (P0). |
| T-THM-06 (A37) | Selection and appearance | `theme` or `"light/dark"` pair, `--use-theme`. Appearance from bg report, then color-scheme notification, then `COLORFGBG`, then dark. | default | 0.79.4/0.79.7 | CAD/themes.md:26-46 | | partial | P1 | `tea.BackgroundColorMsg`. |
| T-THM-07 (A37) | Loading and reload | Built-in dark/light. User dir (hot reload of the active file only). Project `.pi/themes` after trust. Package resources. `themes` setting. `--theme`, `--no-themes`. | default | 0.39.0 | CAD/themes.md:80-136 | Project themes need project trust. | custom | P1 | fsnotify on the active file. |
| T-THM-08 (A38) | Capability overrides | Truecolor from `COLORTERM`, `*-direct`, per-terminal table, `PI_TRUE_COLOR`, `terminal.trueColor`. | default | 0.22.3 | T/terminal-image.ts:75-159; CL:21 | Assume truecolor except `dumb`, empty, `linux`. Windows without `WT_SESSION`. | native | P0 | `colorprofile` detection plus override setting. |
| T-THM-09 (A37) | Theme use by extensions | `theme.fg/bg/bold/underline/inverse/style`, `theme.colors`, `theme.appearance`. Rebuild cached ANSI in `invalidate()`. | default | 0.39.0 | CAD/tui.md | Name only (T-EXT-01). | custom | P2 | Follows extension-UI decisions. |
| T-THM-10 (A37) | Theme export colors | Optional `export{pageBg,cardBg,infoBg}` colors for HTML export. | default | 0.31.0? | CAD/themes.md | Only used by HTML export. | custom | P2 | Needed only if Ask ships HTML export (T-CMD-09). |

## 13. TUI-only slash commands (T-CMD)

There are 24 built-ins in `CA/core/slash-commands.ts:19-43`. Rows say where the work lives. Server operations are listed so the TUI knows what to call; the operation itself belongs to other lanes. `/mcp` and `/debug` are missing from `slash-commands.md` but exist (docs report doc gaps).

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-CMD-01 (n/a) | Slash menu and dispatch | Autocomplete of built-ins, extension commands, templates, `/skill:name`. Optional `argumentHint`. Extension command wins over a same-named template. | default | 0.10.0 | CA/core/slash-commands.ts:20-43; CL:496 | `/quit` shadowed by fuzzy skill match (0.52.6). Numeric suffix on extension name conflicts. Commands work during streaming (0.32.2). | custom | P0 | Registry in `commands/`. Commands run client-side or call the daemon. |
| T-CMD-02 (n/a) | `/quit` | Exit. `/exit` was removed. | default | 0.33.0 | CAD/slash-commands.md | Do not add `/exit`. | native | P0 | `tea.Quit`. |
| T-CMD-03 (n/a) | `/model`, `/thinking`, `/scoped-models` | Open the selectors (T-OVL-05, 06, 08). `/model p/m` sets directly. | default | 0.10.0 | CAD/slash-commands.md | | custom | P0 | |
| T-CMD-04 (n/a) | `/settings`, `/hotkeys`, `/changelog` | Settings menu. Keymap display. Changelog display (`collapseChangelog`). | default | 0.24.0 | CAD/slash-commands.md | | custom | P1 | `/changelog` is Pi release notes. Ask needs its own or skips. |
| T-CMD-05 (n/a) | `/new`, `/resume`, `/name`, `/session` | New session. Picker. Rename. Stats (file, ID, counts, tokens, cost per physical model). | default | 0.12.12 | CAD/sessions.md | `/clear` was renamed `/new` (0.29.0). | custom | P0 | Server ops; TUI shows results. |
| T-CMD-06 (n/a) | `/tree`, `/fork`, `/clone` | Tree navigation. New session from an earlier user message. Copy of the active branch. | default | 0.31.0 / 0.43.0 / 0.66-0.70 | CAD/sessions.md | `/branch` was renamed `/fork` (0.43.0). Clone/fork before the first assistant response needs a clear message (0.80.9). | custom | P1 | Needs the session tree in the store. |
| T-CMD-07 (n/a) | `/compact [instructions]` | Manual compaction. Works when auto is off. | default | 0.11.x | CAD/compaction.md | Input blocked during auto compaction. | custom | P0 | Server op. |
| T-CMD-08 (n/a) | `/copy` | Copy the last assistant message. | default | 0.12.9 | CAD/slash-commands.md | OSC 52 unbounded writes; success reported when the terminal ignored OSC 52; Wayland `wl-copy` failure. | partial | P0 | `tea.SetClipboard` (OSC 52) plus native fallback (`pbcopy`, `wl-copy`, `xclip`, `termux-clipboard-set`). |
| T-CMD-09 (n/a) | `/export`, `/import` | HTML or JSONL export. Import and resume a JSONL. | default | 0.10.0 | CAD/slash-commands.md | HTML export XSS: sanitize link and image URLs with a scheme allow-list (EC§21). Import must not overwrite a file. | custom | P1 | Server op. HTML export is a separate decision. |
| T-CMD-10 (n/a) | `/login`, `/logout` | Provider login (OAuth or API key). `/login <provider>` autocomplete. | default | 0.22.0 | CAD/providers.md | Login must not claim success when `auth.json` is locked (0.80.4). | custom | P0 | Server op plus dialogs (T-OVL-11). |
| T-CMD-11 (n/a) | `/reload` | Re-read settings, keybindings, extensions, skills, templates, themes, context files. | default | 0.52.x | CAD/settings.md | | custom | P1 | |
| T-CMD-12 (n/a) | `/trust` | Project trust prompt. `--approve` and `--no-approve` flags. | default | 0.79.0 | CAD/security.md | `sessionDir` is read before trust is resolved. | custom | P1 | Depends on the security decision. |
| T-CMD-13 (n/a) | `/share`, `/bug` | Radius or gist share. Private bug report upload. | default | 0.31.0? | CAD/sessions.md | Uploads data to a Pi service. | n/a | skip | Not applicable to Ask. |
| T-CMD-14 (n/a) | `/debug` | Writes rendered lines and session messages to `pi-debug.log`. Not in `BUILTIN_SLASH_COMMANDS`. | active | 0.14.2 | CAD/usage.md; II:3217 | File may hold secrets. | partial | P1 | Same tee writer as T-REN-11. |
| T-CMD-15 (n/a) | `/skill:name` and prompt templates | Skill as command with args appended. Templates as `/name` with `$1`, `$@`, `${@:N}` substitutions. | default | 0.19.0 / 0.35.0 | CAD/skills.md; prompt-templates.md | `$1` before `$@` (no recursive substitution). Multiline unquoted args. `enableSkillCommands`. | custom | P1 | Expansion belongs to the harness; the TUI lists them in the menu. |
| T-CMD-16 (n/a) | `/mcp` and `/llama` | Provided by built-in extensions, not in the built-in list. | active | 0.99.0 | CAD/mcp.md; llama-cpp.md | | custom | P2 | Only if Ask ships MCP UI. |
| T-CMD-17 (n/a) | Removed command forms | `/clear`, `/branch`, `/exit`, `/theme`, `/queue`, `/autocompact`, `/show-images`, file `.txt` slash commands. | removed | 0.29-0.52 | CHE§3 | | n/a | skip | See section 16. |
| T-CMD-18 (n/a) | `/mcp` menu | Lists servers by state, tool count, exposure, source. Reconnect, sign in and out, change exposure, enable and disable (saved to the defining file). `/mcp login|logout|reconnect <server>`. Login accepts a pasted redirect URL for remote browsers. | active | 0.99.0 | CAD/mcp.md (docs report §13) | Non-TUI mode prints status. Missing from `slash-commands.md`. Extension `/mcp` replaces the built-in. | custom | P2 | Depends on the MCP lane. Reuse the selector and dialog pieces. |
| T-CMD-19 (n/a) | `/llama` dialogs | Load and unload models, download from Hugging Face, Esc cancels a load, Retry/Close on disconnect. Asks before unloading others. | active | 0.99.0 | CAD/llama-cpp.md | Extension-provided, not built in. | custom | skip | Not needed. Local servers work through the OpenAI-compatible provider. |

## 14. Terminal capability and platform handling (T-TRM)

Most of this section is Go-native work regardless of the framework. The right test method is a table-driven `parseKey(bytes)` test with captured byte sequences per terminal, plus a PTY smoke test for shutdown restoration (EC§19).

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-TRM-01 (B1) | Raw mode and paste mode | Enter raw mode. Enable bracketed paste. Restore on exit and panic. | default | 0.10.0 | T/terminal.ts:186-190,435 | SIGKILL cannot be handled by any library. | native | P0 | Program handles it. |
| T-TRM-02 (B2) | Stdin sequence buffer | Split CSI/OSC/SGR-mouse reassembly. 50 ms sequence and 10 ms lone-escape timeouts. `PI_TUI_ESC_TIMEOUT` (100 ms over SSH). Re-wrap pastes. | default | 0.51.1 | T/stdin-buffer.ts:20-30; CL:132,1081 | Split `Alt+Enter` over SSH read as Escape (0.51.1). Batched input dropped key presses (0.38.0). | partial | P1 | Ultraviolet reader. Timeout is internal with no env override. Test over SSH; patch if needed. |
| T-TRM-03 (B3) | Kitty keyboard negotiation | Push flags 7, query, DA1 sentinel. Response-driven fallback to modifyOtherKeys 2. Ignore mismatched replies. Pop on exit. | default | 0.24.0 | T/terminal.ts:12-14,366-397; CL:326,349 | Delayed or mismatched replies must not enable the protocol falsely. Duplicate chars with CSI-u plus raw (Italian layout, 0.5x). Non-Latin layouts still trigger Ctrl shortcuts. | partial | P0 | `View.KeyboardEnhancements`. modifyOtherKeys fallback not exposed; use alt+enter as the fallback. Gate G6. |
| T-TRM-04 (B4) | Key parser | CSI-u, modifyOtherKeys, legacy ESC prefix, keypad, base-layout fallback, release and repeat filtering. | default | 0.24.0 | T/keys.ts (1,401 lines); CL:1107 | Modifier-only events insert nothing. Caps/Num Lock bits. Keypad keys map to logical keys. | native | P0 | `KeyPressMsg` has `Code, Mod, ShiftedCode, BaseCode, IsRepeat`. Translate Pi ids. |
| T-TRM-05 (B5) | Resize and size fallback | `WindowSizeMsg`. Re-read size after suspend. `COLUMNS`/`LINES` fallback before 80x24. | default | 0.71.0 | CL:85,434,1049 | SIGWINCH lost while suspended. Restricted seccomp rejects self-signal. | native | P0 | `RequestWindowSize`, `ResumeMsg`. Env fallback in the app. |
| T-TRM-06 (B10) | Capability table | TERM, TERM_PROGRAM, COLORTERM, KITTY_WINDOW_ID, WT_SESSION, TMUX, screen, Warp, Zed, JetBrains. tmux hyperlink probe. Overrides via env and settings. | default | 0.21.0+ | T/terminal-image.ts:60-159 | Settings beat env; `auto` keeps detection. JetBrains truecolor but no OSC 8. Screen has 256 colors. | partial | P0 | Table in `term/` (about 150 lines). Probes: `RequestCapability`, `TerminalVersionMsg`, `ModeReportMsg`. |
| T-TRM-07 (B16) | Multiplexer handling | Downgrade mouse mode under tmux, Zellij, screen. Hyperlinks off under screen. Images off in tmux/screen/cmux. tmux `extended-keys` guidance. | default | 0.57.1 | CL:146,343,500; CAD/tmux.md | tmux 3.5 needs `extended-keys-format csi-u`. Warning hidden when server unreachable. | custom | P1 | `ansi.TmuxPassthrough` exists for image wrapping. Ship `/terminal` guidance text. |
| T-TRM-08 (B7) | Suspend and resume | `app.suspend` ctrl+z (Unix job control). None on native Windows. | default | 0.25.3 | CAD/keybindings.md | SIGINT while suspended. Process alive until SIGCONT. Ctrl+Z on Windows must not crash. | native | P1 | `tea.Suspend`, `SuspendMsg`, `ResumeMsg`. |
| T-TRM-09 (B6) | Stdin drain on exit | Drain stdin up to 1 s so Kitty release events do not leak to the parent shell. | default | 0.51.1 | CL:801,805 | Ctrl+D closed the parent SSH session (0.51.2). | gap | P0 | About 30 lines after `Run()` returns. |
| T-TRM-10 (B8) | Windows | VT input mode. Shift+Enter via native helper. Truecolor without `WT_SESSION`. Right-click paste. Windows/WSL keymap. External editor handoff. | default | 0.55.1 | CL:368,172,110; CAD/windows.md | Windows console `0x08` = Ctrl+Backspace. Right-click paste duplicates in VS Code terminals. | partial | P1 | Bubbletea v2 supports the Windows console. Shift+Enter needs Kitty or Windows Terminal. Test on Windows CI. |
| T-TRM-11 (B9) | Apple Terminal Shift+Enter | Local modifier state via a native addon (macOS only, not over SSH). | default | 0.49.3 | CL:359; CAD/terminal-setup.md | Shift+Enter sends plain Return. | gap | skip | Needs cgo. Document `alt+enter` or ctrl+j. |
| T-TRM-12 (B11) | Color queries | OSC 10/11/4 plus DA1 sentinel. 100 ms startup budget with late-reply apply. `CSI ?2031` scheme notifications. | default | 0.99.0 | T/tui.ts:168-172,926; CL:11 | Batched replies. Reply parser must not swallow Ctrl+C (0.49.3). | partial | P1 | `RequestBackgroundColor`/`RequestForegroundColor` cover part. Rest via `tea.Raw`. |
| T-TRM-13 (B13) | Progress and title | `OSC 9;4;3` progress with 1 s keepalive (opt-in, `terminal.showTerminalProgress`). `OSC 0` title. | opt-in | 0.69.0, default-off 0.70.0 | T/terminal.ts:9-10,529; CL:185,461 | Ghostty needs the keepalive. | native | P2 | `View.ProgressBar`. Keepalive unverified; add a ticker if absent. |
| T-TRM-14 (B14) | Clipboard | OSC 52 write. Native read of text, images, file URLs on macOS, Windows, X11, Wayland. | default | 0.33.0 (image) | T/tui-alt-screen.ts:1470; CL:42,17 | Finder-copied files paste as an icon (0.37.3). WSL BMP to PNG. Sandboxed macOS pasteboard denial must not abort. Termux text only. | partial | P1 | `tea.SetClipboard`. Native read by exec (`pbpaste`, `wl-paste`, `xclip`, `powershell`) rather than cgo. |
| T-TRM-15 (n/a) | Hyperlinks (OSC 8) | Auto-detect. `terminal.hyperlinks` and `PI_HYPERLINKS` override. Width code ignores them. | default | ? | T/utils.ts:359 | Forced off under screen. Probed under tmux. | native | P1 | `ansi.SetHyperlink`; lipgloss links. |
| T-TRM-16 (n/a) | Termux and Android | `/bin/bash` absent. Soft keyboard height change must not clear. Clipboard via `termux-clipboard-*`. Text only. | default | 0.51.6 / 0.61.1 | CAD/termux.md | Detect Termux by env. | custom | P2 | Height-only resize filter (T-REN-04). |
| T-TRM-17 (n/a) | Per-terminal quirks | Ghostty alt+backspace, WezTerm Alt+Enter, Zellij Shift+Enter, IntelliJ/xfce cannot distinguish modified Enter, VS Code sendSequence, Alacritty mapping. | default | various | CAD/terminal-setup.md; EC§19 | | custom | P2 | Docs and `/terminal-setup` text only. Fallback ctrl+j always works. |
| T-TRM-18 (n/a) | Focus reports | Focus in/out consumed. Losing focus must not repaint or clear selection. | default | 0.84.x | CL:135 | | native | P2 | `View.ReportFocus`. |
| T-TRM-19 (n/a) | Early input and lost stdin | Buffer input typed before the prompt loop starts. Exit if stdin is lost. | default | 0.78.0 / 0.73.0 | EC§18 | | native | P0 | |
| T-TRM-20 (n/a) | Native addons | koffi replaced by a vendored native helper for clipboard and modifier state. | removed (koffi) | 0.5x | CL:368 | | n/a | skip | Do not port. Use exec fallbacks. |
| T-TRM-21 (n/a) | Terminal-specific settings | iTerm2: set Advanced > "Trackpad scrolls fast?" to No for fullscreen scroll. Windows Terminal: Alt+Enter is fullscreen, so use Ctrl+Q for follow-up, and `sendInput \u001b[13;2u` for Shift+Enter. VS Code 1.109.5+ works, older needs `sendSequence`. Zed needs keymap entries. | default | 0.5x-0.84.0 | CAD/terminal-setup.md | xfce4-terminal, Terminator and IntelliJ cannot distinguish modified Enter. | custom | P2 | Docs text only. Ctrl+J always works. |
| T-TRM-22 (B14) | Termux clipboard | Text clipboard through `termux-clipboard-set/get` (needs Termux:API app). No image paste. Shared storage needs `termux-setup-storage`. | default | 0.52.10 | CAD/termux.md | Play Store build should be avoided. | custom | P2 | Exec fallback. Extends T-TRM-14 and T-TRM-16. |

## 15. Alt-screen (fullscreen) mode (T-ALT)

All rows depend on Q3. Pi added fullscreen in 0.84.0 and keeps regular mode as the default. Inline mode uses native terminal selection, search and wheel scroll, so most of these features replace what the terminal gives for free. Do them only if the user wants fullscreen.

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-ALT-01 (A2) | Alt buffer renderer | `?1049`, autowrap off, mouse on, cursor hidden. Repaints changed rows. Restores the main buffer on stop. | opt-in | 0.84.0 | T/tui-alt-screen.ts:62-72,372 | Exit prints the final transcript or a resume hint (`fullscreenExitOutput`). | native | P1 | `View.AltScreen = true`. |
| T-ALT-02 (A12) | Transcript scroll and sticky dock | ScrollView (follow end) plus fixed dock (pending, status, widgets, editor, footer). | opt-in | 0.84.0 | CA/.../chat-viewport.ts:21-46 | Dock must leave minimum rows for the transcript. | native | P1 | `bubbles/viewport` plus `JoinVertical`. Manual height math. |
| T-ALT-03 (A11) | Scrollbar | Proportional thumb (2-cell minimum). auto/always/hidden. Drag thumb. Click track. Jump-to-end label. | opt-in | 0.84.0 | CL:97,160,74 | `always` reserves the last column. | custom | P2 | Custom renderer plus hit test. |
| T-ALT-04 (A15) | Mouse routing | SGR press, release, drag, move, wheel. Click counts. Wheel to the view under the pointer. Wheel step 1, auto, or Alt x5. | opt-in | 0.84.0 | T/tui.ts:21-60; CL:15,60,168 | Step changed 3, 1, auto. All-motion tracking avoided under tmux. Hover must not move list selection. | partial | P1 | `MouseClickMsg` and related. Count clicks with a timer. Mouse mode off in inline. |
| T-ALT-05 (A16) | Text selection and copy | Char, word, paragraph granularity. Edge auto-scroll. Copy on release (`fullscreenCopyOnSelect`) by callback or OSC 52. | opt-in | 0.84.0 | CL:93,104,134,160,177 | Phantom selection on pane focus. Drag must not run over the editor. | custom | P2 | Selection map over rendered rows. Crush `model/ui.go` as design ref. |
| T-ALT-06 (A17) | Transcript search | Ctrl+Shift+F, incremental, next/prev, buttons, theme match styles. | opt-in | 0.84.2 | CL:119,80; T/alt-screen-search.ts | Large transcripts need an ASCII-run index. | custom | P2 | Search over block plain text. |
| T-ALT-07 (A18) | OSC 133 prompt jump | Marks skipped in width. `previousPrompt`/`nextPrompt` navigation. | opt-in | 0.84.0 | T/tui-alt-screen.ts:73; CL:162 | | gap | P2 | Emit markers as raw text; index block starts. |
| T-ALT-08 (A41) | Runtime mode swap | Swap regular and fullscreen without replaying content. | opt-in | 0.84.0 | CL:155; II:838-872 | Inline scrollback cannot be un-printed. | partial | P1 | Keep the block list. Redraw on enter. Print the full transcript on exit (gate G5). |
| T-ALT-09 (A13) | Alt overlays | Focused overlay still gets wheel and PageUp/PageDown. | opt-in | 0.84.0 | CL:131 | | partial | P1 | Lipgloss layers. See T-OVL-01. |
| T-ALT-10 (A36) | Alt image placement | Upload once, move placements, clip at layout boundaries. | opt-in | 0.84.0 | T/tui-alt-screen.ts:216,1693-1711; CL:179-181 | WezTerm row-clear workaround. | custom | P2 | Kitty placeholders again. |
| T-ALT-11 (A19) | Hyperlink click | OSC 8 activated on primary click. Links win over enclosing regions. | opt-in | 0.84.0 | T/tui-alt-screen.ts:186 | | custom | P2 | Hit test on link ranges. |
| T-ALT-12 (n/a) | Right-click paste | Optional handler, Windows only. | opt-in | 0.84.x | CL:110,142 | Duplicates in VS Code terminals. | custom | skip | Not needed. |

## 16. Width and Unicode (T-WID)

| id (need) | Feature | Detail | Pi status | Since | Source | Edge cases | Charm | Tier | Go note |
|---|---|---|---|---|---|---|---|---|---|
| T-WID-01 (A8) | Visible width | Grapheme based. Ignores SGR, OSC 8, OSC 133, APC. Tabs normalized. ASCII fast path. | default | 0.29-0.31 | T/utils.ts:250-313,394 | U+0600-0604 crashed width code (0.31.0). ZWJ emoji. Partial flags while streaming. Indic, Thai, Lao. | native | P0 | `x/ansi.StringWidth` with `displaywidth` and mode 2027. |
| T-WID-02 (A8) | Wrap, truncate, slice | SGR-preserving wrap. CRLF and CR aware. Truncate always closes SGR and OSC 8. Column slice used by overlays. | default | ? | T/utils.ts:891,1112,1254,1314 | Never cut mid-escape (test at every offset). | native | P0 | `Wrap`, `Hardwrap`, `Truncate`, `Cut`. |
| T-WID-03 (A8) | Width disagreement guard | Terminal and library can disagree for CJK, emoji, ambiguous width. | n/a | n/a | lane H (octo-agent PR 2467) | A line that wraps physically but counts as one leaves a duplicate input box. | partial | P0 | Cap live-frame line width at width-1. Gate G3. |
| T-WID-04 (A8) | Click and selection helpers | `getOsc8LinkAtColumn`, `getGraphemeCellRange`. | active | 0.84.0 | T/utils.ts:335,359 | | custom | P1 | Only with mouse features. |
| T-WID-05 (A8) | Line-width property test | No rendered line wider than width for widths 1 to 200, for every component. | n/a | n/a | EC§16 | Repeated Pi regression (footer, branch selector, settings, indicators, image fallback). | custom | P1 | Add as a shared test helper. |

## 17. Extension UI primitives (T-EXT): names only

Owned by another agent. Listed so TUI rows can point at them. Wire contract to reuse: Pi's serializable extension-UI subprotocol (`CAD/rpc-extension-ui.md`). Out-of-process `custom()` is not portable (Q4).

| id (need) | Name | Used by rows |
|---|---|---|
| T-EXT-01 (A40) | `ctx.ui`: `select`, `confirm`, `input`, `editor`, `notify`, `onTerminalInput`, `setStatus`, `setWorkingMessage/Visible/Indicator`, `setHiddenThinkingLabel`, `setWidget`, `setFooter`, `setHeader`, `setTitle`, `custom`, `pasteToEditor`, `setEditorText`, `getEditorText`, `addAutocompleteProvider`, `setEditorComponent`, theme accessors, tools-expanded accessors | T-LAY-04, 05, T-EDT-13, T-OVL-12, T-THM-09, T-MD-04 |
| T-EXT-02 (A7) | Untrusted extension text must be stripped to SGR before rendering (OSC 52, kitty graphics, cursor moves) | T-REN-06 |
| T-EXT-03 (A40) | `ctx.mode` (tui/rpc/json/print), `registerShortcut`, `registerCommand`, `registerMessageRenderer`, tool `renderCall`/`renderResult` | T-CMP-13, T-CMD-01, T-KEY-01 |
| T-EXT-04 (A40) | Non-TUI degradation: `custom()` returns undefined, `onTerminalInput` and header, footer, autocomplete, editor-component and working-indicator setters are no-ops, `getEditorText()` is empty, `setTheme()` returns an error in RPC mode. `ctx.hasUI` is true in RPC | T-EXT-01 |
| T-EXT-05 (A40) | Component set given to extensions: `Text, Markdown, Image, TruncatedText, Container, VStack, HStack, Box, Spacer, Input, Editor, SelectList, SettingsList, ScrollView, Loader, CancellableLoader, MouseRegion`, `CURSOR_MARKER`, `CustomEditor`, plus Ask-side replacements TBD | T-CMP-01 to T-CMP-11, T-EDT-19 |

## 18. Removed or deprecated: do not rebuild

| Item | Fate | Since | Source | Rule for Go |
|---|---|---|---|---|
| `isEnter()`, `isEscape()`, `isCtrlC()` key detectors | Removed for `matchesKey(data, "ctrl+c")` | 0.33.0 | CHE§3 | One key-matching API driven by the registry. |
| Editor-only keybinding store | Replaced by one global manager with `tui.*` namespaces | 0.61.0 | CL:597 | Single registry. Keep the ids stable. |
| koffi dependency | Replaced by a vendored native helper | 0.5x | CL:368 | No native addon. Use exec fallbacks. |
| `queryTerminalColorScheme/BackgroundColor` | Replaced by `queryTerminalColors` | 0.99.0 | CL:11 | One combined color query. |
| `PI_DEBUG_REDRAW` env | Renamed `PI_TUI_DEBUG_REDRAW`; env defaults removed from pi-tui | 0.5x | CL:70 | Prefix all TUI env vars. |
| Image placeholders on paste | Removed; the file path is inserted | 0.34.0 | CHE§3 | Insert the path. |
| Separate `/thinking`, `/queue`, `/theme`, `/autocompact`, `/show-images` | Folded into `/settings` (0.29.1). `/thinking` exists again in 0.99. | 0.29.1 | CHE§3 | Keep `/settings` plus `/thinking`. No other split commands. |
| `/clear` | Renamed `/new` | 0.29.0 | CHE§3 | Use `/new`. |
| `/branch` | Renamed `/fork` (in-place tree is `/tree`) | 0.43.0 | CHE§3 | Use `/fork`. |
| `/exit` | Removed; `/quit` only | 0.52.6 | CHE§3 | `/quit` only. |
| File-based `.txt` slash commands | Became `.md` prompt templates | 0.35.0 | CHE§3 | Prompt templates only. |
| Extension editor Ctrl+Enter for newline | Changed to Shift+Enter | 0.43.0 | CHE§3 | One newline rule across editors. |
| Hardware cursor on by default | Reversed: off by default (`PI_HARDWARE_CURSOR=1` opts in) | 0.48.0 | CHE§3 | Default off. Always position it for IME. |
| OSC 9;4 progress on by default | Reversed within one release; opt-in | 0.70.0 | CHL§3 | Default off. |
| Slash menu at any position, then empty-only | Reversed to first line only | 0.5x | CL:781,910 | First line and not mid-word. |
| `clearOnShrink` default | Stayed off after a flip | 0.51.x | CL:818-819 | Default off. |
| Alt-screen wheel step 3 | Changed to 1, then `auto` | 0.84.x | CL:168 | Only if ALT rows are built. |
| Always-full-redraw on height change | Removed | 0.61.1 | CL:591 | Redraw on width change only. |
| Overlays as experimental | Stabilized with focus order and non-capturing mode | 0.57.0 | CHL | Build the stack once, not the first version. |
| Not in Pi | Vim mode, sixel, redo, editor selection, virtualized transcript, sidebar | n/a | section 13 of lane D | Do not add; they were never Pi features. |

## Unresolved questions

1. Q1. Is a repo-wide `go 1.26.0` directive acceptable (CI, Docker, golangci-lint action)? Required by bubbletea v2.0.10. The version pin in section 1 stays a recommendation until the user confirms. Run `golangci-lint` and `go test ./...` on the migrated `go.mod` before merging.
2. Q2. If the gate fails: vendor and patch bubbletea and ultraviolet (kiln route), or write the raw-write committer? Lane H recommends stock first, then act on the first failing test.
3. Q3. Does the first milestone include fullscreen (T-ALT rows), or inline only? This decides whether overlays need the layer compositor at all. Pi's default is regular mode. The edge-cases report notes about 60 Pi fixes in fullscreen mouse, selection and Kitty-image handling that matter only if the answer is yes.
4. Q4. Does Ask need `custom()`-style extension components out of process? If yes, a declarative UI tree spec is required. This also decides who renders `renderCall`/`renderResult` for extension tools (T-CMP-13).
5. Q5. Policy for expand/collapse (tools, thinking) on blocks already committed to scrollback in inline mode. Options: apply to uncommitted blocks only, or re-print.
6. Q6. Image scope: is Kitty only acceptable, or must iTerm2 images work in scrollback?
7. Q7. Should the TUI keep Pi's `keybindings.json` ids exactly, or use Ask names? Ids leak into user config files and extensions, so decide before publishing.
8. Q8. Kiln has no LICENSE and Crush is FSL-1.1-MIT. Confirm the rule "read for design, re-implement, never paste" before anyone opens those repos for code.
9. Q9. Unverified in the source lanes: exact `tui.*`/`app.*` key counts (hand-tallied), some CL line numbers (first match of a phrase), `T/keys.ts`, `T/latex.ts`, alt-screen selection internals and `packages/tui/test` were not read line by line. The Pi test suite is the best source for exact expected outputs.
10. Q10. Several `since` values are `?` because the changelog reports do not give a version. Fill from `packages/tui/CHANGELOG.md` before quoting them.
11. Q11. Does Ask need Windows and WSL at launch? Pi added about 68 Windows fixes. The answer changes the T-TRM Windows rows, the per-platform keymap (T-KEY-05) and Windows CI.
