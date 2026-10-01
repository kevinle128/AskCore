# Pi TUI feature inventory (lane D)

Scope: feature-level inventory of the Pi TUI (`@earendil-works/pi-tui` 0.99.1 plus the coding-agent interactive mode) as input to a Go/bubbletea rewrite. Pi repo commit 2bbfcca4, read only.

Source shorthand used in every row:
- `T/x` = `packages/tui/src/x`
- `CL:N` = `packages/tui/CHANGELOG.md` line N (history and edge cases)
- `CA/x` = `packages/coding-agent/src/x`
- `CAD/x` = `packages/coding-agent/docs/x`
- `PLAN:` = `tui-plan.md` at repo root
- `II` = `CA/modes/interactive/interactive-mode.ts`

## 0. Outcome in ten lines

1. Pi TUI is a line-oriented framework: a component renders `string[]` (ANSI-styled lines) for a width. It is not a cell-grid framework. Everything else is built on that (T/tui.ts:117).
2. Two interchangeable renderers share one `TUI` interface: `TuiMainScreen` (inline, terminal scrollback preserved) and `TuiAltScreen` (alt buffer, app-owned viewport, mouse, scrollbars). The user picks with the `tuiMode` setting, default `regular` (CAD/settings.md:92, T/tui.ts:446).
3. Direction of travel is fullscreen: `PLAN:` and CL:153-166 (0.84.0) add VStack/HStack/ScrollView, scrollbars, search, selection, OSC 133 prompt jumps. Regular mode stays the default and is not being changed.
4. Main-screen rendering is a diff of previous vs new line arrays with CSI 2026 wrapping (T/tui-main-screen.ts:280-302). Width change means full clear-and-redraw (T/tui-main-screen.ts:341).
5. Alt-screen rendering diffs a row array and repaints whole rows with `CSI row;1H` + `CSI 2K` (T/tui-alt-screen.ts:1727-1733).
6. Editor is a bordered multi-line Emacs-style editor with kill ring, undo, paste markers, history, autocomplete (`@` files via external `fd`, `/` commands, extension providers). No vim mode and no in-editor selection (see section 8).
7. Syntax highlighting, diff rendering and the theme are NOT in pi-tui. They live in coding-agent (cli-highlight, `diff` npm package, JSON themes).
8. pi-tui does bundle: Markdown (marked) plus LaTeX-to-Unicode, Kitty and iTerm2 images, OKLCH/OKHSL color math, Kitty keyboard protocol with modifyOtherKeys fallback, fuzzy match.
9. Biggest Go cost centres are not widgets. They are (a) keyboard protocol negotiation and key-id matching (1401 LOC in T/keys.ts), (b) grapheme/ANSI/OSC-aware width, wrap, truncate, slice (T/utils.ts 1385 LOC), (c) the editor (2472 LOC), (d) alt-screen mouse selection/search/scrollbars (1750 LOC).
10. Extension surface is broad: `ctx.ui` has ~25 methods, including arbitrary `custom(factory)` components, overlays, replacing editor/header/footer, raw input hooks (section 9).

## 1. Ring 0 - render loop and terminal model

| # | Feature | Detail | Source |
|---|---|---|---|
| 1.1 | Component contract | `render(width): string[]`, optional `handleInput(data)`, `handleMouse(event)`, `wantsKeyRelease`, required `invalidate()`. Every line must fit width. | T/tui.ts:117-142 |
| 1.2 | Container | Ordered children, concatenates child lines. Loop-push (no spread) to avoid stack overflow on long sessions. | T/tui.ts:347, CL:530 |
| 1.3 | Render scheduling | `requestRender()` coalesced to a 16 ms minimum interval; `requestRender(true)` forces; keyboard input preempts the timer (Windows latency fix). | T/tui.ts:507,1010-1029,1116; CL:542, CL:171 |
| 1.4 | Main-screen diff | Compares `previousLines` vs `newLines`, finds first/last changed line, rewrites only that range. Appended lines commit to scrollback. | T/tui-main-screen.ts:364-395 |
| 1.5 | Main-screen full redraw triggers | First render (no clear); width change (clear screen + scrollback `2J H 3J`); height change (except Termux); optional `clearOnShrink` (default false). | T/tui-main-screen.ts:331-361; CL:818-819, CL:591 |
| 1.6 | Synchronized output | Every frame wrapped in `CSI ?2026h` ... `?2026l`. | T/tui-main-screen.ts:280,302,460,567; T/tui-alt-screen.ts:71 |
| 1.7 | Per-line style reset | Each rendered line gets SGR reset plus OSC 8 close appended so styles/links never leak between lines. | T/tui.ts:412 (`SEGMENT_RESET`), CL:1041 |
| 1.8 | Bounded writer | Output is chunked so image-heavy frames do not exceed V8 string limits. Go equivalent: a buffered writer. | T/tui-main-screen.ts:18; CL:102 |
| 1.9 | Alt-screen renderer | Enters `?1049h`, disables autowrap `?7l`, enables mouse, hides cursor; repaints changed rows only; on stop restores main buffer and prints the complete final document (or resume hint). | T/tui-alt-screen.ts:62-72,372,395-407; CAD/settings.md:93 |
| 1.10 | Alt-screen frame cost | Full-width layout rows are painted as line references, not recomposited per frame (9-18x fewer allocations). | CL:139 |
| 1.11 | Hardware cursor | Focused component emits zero-width `CURSOR_MARKER` (`ESC _ pi:c BEL`, an APC); renderer finds it (scans from bottom, bounded to terminal height), strips it, positions the real cursor. Cursor visibility opt-in (`showHardwareCursor`, default off) but always positioned for IME. | T/tui.ts:180-196; CL:960, CL:317 |
| 1.12 | Soft cursor | Editor draws its own inverse-video cursor cell; cleared on shutdown before restoring hardware cursor. | CL:210; T/components/editor.ts:520 |
| 1.13 | Resize | Node `resize` event; dimensions re-read after suspend/resume because SIGWINCH is lost while stopped; env `COLUMNS`/`LINES` fallback before 80x24; seccomp-safe startup. | T/terminal.ts:189-197; CL:85, CL:434, CL:1049 |
| 1.14 | Height change on Termux | Full redraw skipped so soft keyboard toggle does not replay history. | T/tui-main-screen.ts:347; CL:591 |
| 1.15 | Debug tooling | `PI_TUI_WRITE_LOG` raw ANSI capture (per-instance file), `PI_TUI_DEBUG_REDRAW` logs redraw reasons, crash dump to log dir, `fullRedraws` counter, `onDebug` (Shift+Ctrl+D). | CL:70, CL:569, CL:814, T/tui-main-screen.ts:321 |
| 1.16 | Renderer handoff | `stop({preserveScreen})` plus `TuiMainScreenRenderState` let coding-agent swap renderer at runtime without replaying content. A proxy keeps component `tui` references stable across swaps. | T/tui.ts:448; CL:155; CA/modes/interactive/tui-renderer.ts:54-81 |
| 1.17 | Kitty image reserved rows | Redraw logic expands the changed range to cover image blocks and deletes stale image ids before repaint. | T/tui-main-screen.ts:196-262 |

## 2. Ring 1 - built-in components

| # | Component | Behaviour | Source |
|---|---|---|---|
| 2.1 | `Text` | Word-wrapped text with paddingX/Y, optional bg fn, cached by (text,width). | T/components/text.ts:7-18 |
| 2.2 | `TruncatedText` | Single line truncated with ellipsis, padding. | T/components/truncated-text.ts:12 |
| 2.3 | `Spacer` | N blank lines. | T/components/spacer.ts:9 |
| 2.4 | `Box` | Padding plus background applied to child lines; render cache. | T/components/box.ts:24 |
| 2.5 | `Container` / `VStack` / `HStack` | Stack entries with `basis` (number or auto), `grow`, `shrink`, `minSize`, `maxSize`, `visible(viewport)`; stack options `gap`, `align` (stretch/start/center/end). Flexbox-lite, no wrap, no grid, no percentages. | T/components/stack.ts:19-24, PLAN: "Stack entries" |
| 2.6 | `ScrollView` | Vertical only (throws on other axis). `follow: none|end`, `primary` (receives transcript keys/search), `overscroll: chain|contain`, `scrollbar: auto|always|hidden`, track/thumb styles, hide delay, `scrollTo` with `disableFollow`. | T/components/scroll-view.ts:6-46 |
| 2.7 | `MouseRegion` | Wraps a child with a `handleMouse` callback. | T/components/mouse-region.ts:16 |
| 2.8 | `Input` | Single line, horizontal scroll by visual width, prompt, placeholder, grapheme-safe cursor, word nav, kill ring plus undo, paste normalises tabs. | T/components/input.ts:16-49; CL:643-648 |
| 2.9 | `Editor` | See section 4. | T/components/editor.ts |
| 2.10 | `Markdown` | See section 5. | T/components/markdown.ts |
| 2.11 | `Image` | Kitty or iTerm2 inline image with maxWidthCells/maxHeightCells, filename fallback text, reusable Kitty id. | T/components/image.ts:17-25 |
| 2.12 | `Loader` | Braille spinner (10 frames, 80 ms), custom frames/interval, empty frames hides indicator, message coloring. | T/components/loader.ts:11-12; CL:79 |
| 2.13 | `CancellableLoader` | Loader plus Escape/cancel with `AbortSignal`. | T/components/cancellable-loader.ts |
| 2.14 | `SelectList` | Filter, scroll window (`maxVisible`), primary/description columns, custom primary truncation, page keys, mouse click/hover, multi-line descriptions flattened, `onSelect/onCancel/onSelectionChange`. Filter is prefix match; fuzzy is done by callers. | T/components/select-list.ts:12-73; CL:64 |
| 2.15 | `SettingsList` | Rows label/value, Enter/Space cycles `values`, `submenu` factory opens child component, optional fuzzy search (`enableSearch`), narrow-width safe, mouse forwarding. | T/components/settings-list.ts:11-80; CL:785 |
| 2.16 | `AltScreenFlashContainer` | Stacked transient notifications (`flash(message, ms)`), alt-screen only. | T/components/alt-screen-flash.ts; T/tui-alt-screen.ts:651; CL:163 |
| 2.17 | `AltScreenSearchComponent` | Bordered placeholder input with result count and clickable prev/next buttons. | T/alt-screen-search.ts:197; CL:97 |

### Coding-agent components (composition layer, not in pi-tui)

| # | Component | Behaviour | Source |
|---|---|---|---|
| 2.20 | Fullscreen layout | `VStack[ ScrollView(document, follow end, primary, chain, scrollbar) grow 1 ; dock VStack[pending, status, widgetsAbove, editor(minSize 3), widgetsBelow, footer] ]`. | CA/modes/interactive/chat-viewport.ts:21-46 |
| 2.21 | Regular layout | Same component instances appended vertically: header, loaded resources, chat, pending, status, widgets, editor, footer. | II:597-624, PLAN: "Why main-screen and alternate-screen layouts differ" |
| 2.22 | Assistant message | Markdown plus thinking blocks (collapsible, hidden label), streaming via `updateContent(message, isStreaming)` and re-render on `invalidate`. | CA/modes/interactive/components/assistant-message.ts:47-91 |
| 2.23 | Tool execution | Pending/success/error backgrounds, custom `renderCall`/`renderResult` from tools, fallback 10-line preview, expand toggle (`app.tools.expand`), images. | CA/modes/interactive/components/tool-execution.ts:17-40,166 |
| 2.24 | Bash execution | Streaming output, 20-line preview, visual-line truncation shared with tool output. | CA/modes/interactive/components/bash-execution.ts:19,131; visual-truncate.ts |
| 2.25 | Diff view | Parses `+123 content` lines, colors added/removed/context, word-level inverse highlighting only when exactly one removed and one added line. Uses `diff` npm `diffWords`. | CA/modes/interactive/components/diff.ts:1-113 |
| 2.26 | Footer | cwd with `~`, git branch (watched), session name, token stats up/down/cache read/write/cache-hit %, cost, context % colorized, model, thinking level, extension statuses. | CA/modes/interactive/components/footer.ts:60-140 |
| 2.27 | Selectors/dialogs | model, scoped-models, settings (+submenus), theme, thinking, session (tree with search), tree (fold/unfold, label edit, filters), user-message, config, trust, oauth, login dialog, show-images, first-time setup, extension select/input/editor with countdown timeout. | CA/modes/interactive/components/*.ts (listing) |
| 2.28 | Misc messages | user, custom, skill invocation, branch summary, compaction summary, custom-entry, status indicator, dynamic border, keybinding hints, pi logo, announcement, easter eggs (armin, daxnuts). | same dir |
| 2.29 | Mermaid | Top-level mermaid code blocks are replaced by Unicode diagrams via `grok-mermaid`; modes off/final/streaming. Hooks Markdown `transform`. | CA/modes/interactive/components/mermaid.ts:2,59; CAD/settings.md:111 |
| 2.30 | Slash commands | 23 built-ins (settings, model, tree, thinking, scoped-models, export, import, share, bug, copy, name, session, changelog, hotkeys, fork, clone, trust, login, logout, new, compact, resume, reload, quit) with optional `argumentHint`. | CA/core/slash-commands.ts:20-43; CL:496 |
| 2.31 | Editor modes | `!cmd` runs bash with context, `!!cmd` without; border tint `bashMode`; thinking-level border color; dropped file paths quoted. | II:3021-3056,3244-3255 |
| 2.32 | External editor | `app.editor.external` (ctrl+g) opens `$VISUAL/$EDITOR` and reads the result back. | CA/modes/interactive/external-editor.ts; CAD/keybindings.md |

## 3. Layout, overlays, mouse, focus

### 3a. Overlays (both renderers)

| # | Feature | Detail | Source |
|---|---|---|---|
| 3.1 | Options | `width` / `maxHeight` / `row` / `col` as number or `"NN%"`, `minWidth`, `anchor` (9 positions), `offsetX/Y`, `margin` (number or per-side), `visible(termW, termH)`, `nonCapturing`. | T/tui.ts:203-280 |
| 3.2 | Handle | `hide`, `setHidden`, `isHidden`, `focus`, `unfocus({target})`, `isFocused`, `getBounds`. | T/tui.ts:298-322 |
| 3.3 | Stack semantics | Overlays are a stack with focus order; focused overlay is drawn on top; pre-focus target is restored on hide; non-capturing ones are skipped on restore. | T/tui.ts:326-345; CL:667, CL:334 |
| 3.4 | Compositing | Overlay lines are spliced into base lines with ANSI-aware column slicing. Must survive CJK wide cells at the start column, tabs, OSC 8 links, lines wider than terminal. | T/tui.ts:415 `compositeTuiLine`; CL:294, CL:335, CL:1006 |
| 3.5 | Re-centering | Center-anchored overlays stay centered when terminal grows after shrink. | CL:911 |
| 3.6 | Alt-screen overlays | Focused fullscreen overlay still receives wheel and PageUp/PageDown. | CL:131 |

### 3b. Alt-screen only

| # | Feature | Detail | Source |
|---|---|---|---|
| 3.10 | Constrained layout tree | Rebuilt every requested render from the component tree, never persisted; leaves keep their own render caches. Layout internals are not public API. | PLAN: "Core decisions" 5-7 |
| 3.11 | Sticky dock + transcript | Transcript scrolls, dock stays fixed. Pending/status inside the dock so queue state is visible while reading history. | PLAN: "Why main-screen..."; CA/.../chat-viewport.ts |
| 3.12 | Wheel routing | Wheel goes to the scroll view under the pointer; unhandled scroll chains outward (`overscroll: chain`). Step: 1 line default, `"auto"` accelerates, Alt = 5x. | CL:15, CL:60, CL:168; T/wheel-scroll.ts |
| 3.13 | Keyboard scroll | `tui.altScreen.pageUp/pageDown/halfPage*/line*/top/bottom`, prompt jump (OSC 133 A marks), Ctrl-variants of editor page/home/end kept. | T/keybindings.ts; CL:162, CL:181 |
| 3.14 | Scrollbars | Proportional thumb (2-cell minimum), thin muted track, `auto` reveals on hover and while scrolling, `always` reserves the last column, drag thumb, click track to jump, runtime mode change. | CL:97, CL:160 |
| 3.15 | Jump-to-end indicator | Clickable label centered on last row while follow-end view is scrolled away. | CL:74 |
| 3.16 | Transcript search | Incremental, Ctrl+Shift+F, Enter/Ctrl+G next, Shift+Enter previous, Esc close, match styles via theme, ASCII-run index and cached results for large transcripts, drag/scroll does not snap back. | CL:119, CL:80, T/alt-screen-search.ts:156-197 |
| 3.17 | Text selection | App-owned: press/drag/release, double-click word, triple-click paragraph, granularity-aware drag, edge auto-scroll across off-screen content, phantom-selection guard, drag does not run over the editor. Copy on release via callback or OSC 52; failure surfaces actionable error. | CL:93, CL:104, CL:134, CL:160, CL:177; T/tui-alt-screen.ts:1470 |
| 3.18 | Hyperlink click | OSC 8 links activated on primary click via `openUrl`; links take precedence over enclosing click regions. | T/tui-alt-screen.ts:186; T/utils.ts:359; CAD/tui.md (Handle mouse input) |
| 3.19 | Right-click paste | Optional handler, enabled on Windows only. | CL:110, CL:142 |
| 3.20 | Mouse mode | Button-motion tracking (`1000/1002/1004/1006`), all-motion (`1003`) avoided under tmux/Zellij/screen to cut event volume. Focus in/out reports consumed. | T/tui-alt-screen.ts:66-68; CL:147 |
| 3.21 | Mouse dispatch | Events carry local and screen coords, size, modifiers, `wheelDelta`, `clickCount`; handler result: `handled`, `capture` (drag routing), `focus`, `render`. Hover must not move list selection. | T/tui.ts:21-60; CL:64 |
| 3.22 | Regular mode mouse | None. Terminal owns scrollback, so no mouse capture; keyboard path is mandatory. | CAD/tui.md "Handle mouse input"; PLAN: "Goals" 10 |

### 3c. Focus

| # | Feature | Source |
|---|---|---|
| 3.30 | Single focused component (`setFocus`), `Focusable.focused` flag; containers wrapping Input/Editor must propagate `focused` for IME. | T/tui.ts:180; CAD/tui.md |
| 3.31 | Focus restored to prior target after overlay hide; blocked/eligible restore state machine. | T/tui.ts:253-275 |
| 3.32 | Keyboard focus survives child removal during mouse forwarding. | CL:28 |

## 4. Editor (ring 1 deep dive)

| # | Feature | Detail | Source |
|---|---|---|---|
| 4.1 | Layout | Horizontal rules above and below (no side borders), paddingX 0-3, word wrap reserving a cursor column, max height 30% of terminal (min 5 lines), scroll indicators in borders, page up/down. | T/components/editor.ts:520-630; CL:982 |
| 4.2 | Wrapping | Word wrap with backtracking, CJK breaks at grapheme boundaries, mixed Latin+CJK, wide char at boundary, ZWJ/regional indicators/Indic/Thai/Lao clusters. | CL:306, CL:319, CL:435, CL:704, CL:1202, CL:176 |
| 4.3 | Keys (defaults) | Emacs style: ctrl+a/e/b/f/d/k/u/w/y, alt+b/f/d/y, alt+backspace, ctrl+left/right word, ctrl+] char jump, ctrl+- undo, pageUp/Down. Enter submits, shift+enter or ctrl+j newline. Up on first visual line jumps to line start before history. | T/keybindings.ts (TUI_KEYBINDINGS); CL:254, CL:865-867 |
| 4.4 | Word boundaries | Unicode-aware segmenter but ASCII punctuation boundaries preserved. | T/word-navigation.ts; CL:362 |
| 4.5 | Kill ring | Ring buffer, consecutive kills accumulate (prepend for backward), yank and yank-pop. | T/kill-ring.ts:8-46; CL:949 |
| 4.6 | Undo | Snapshot stack (structuredClone) with fish-style coalescing of word chars; restores paste registry with text. No redo. Also on `Input`. | T/undo-stack.ts:7; CL:941; CL:212 |
| 4.7 | History | In-memory, newest first, 100 max, no consecutive duplicates. Up/Down at edges browse; cursor lands at start going up and end going down; draft restored on return; dedicated `historyPrevious/Next` actions ignore cursor position. Persistence is not in the editor. | T/components/editor.ts:427-437; CL:318, CL:325, CL:284 |
| 4.8 | Sticky column | Vertical moves restore the preferred visual column, also across paste markers. | CL:836, CL:531 |
| 4.9 | Paste handling | Bracketed paste buffered; over 10 lines or over 1000 chars becomes atomic marker `[paste #N +L lines]` / `[paste #N C chars]`; markers are indivisible for wrap and cursor; registry renumbered on delete; `getExpandedText()` substitutes originals; tabs to spaces; literal content preserved; single-line paste inserted atomically (no per-char autocomplete). | T/components/editor.ts:30-34,1296-1312,1397-1416; CL:212, CL:631, CL:639, CL:706, CL:1208 |
| 4.10 | Path paste | Auto-space before pasted path starting `/`, `~`, `.` after a word char. | CL:1208 |
| 4.11 | Char jump | ctrl+] then char jumps forward; ctrl+alt+] backward; same key cancels. | CL:866 |
| 4.12 | Programmatic API | `setText`, `getText`, `getExpandedText`, `getLines`, `getCursor`, `insertTextAtCursor`, `pasteToEditor`, `addToHistory`, `setAutocompleteProvider`, `onSubmit`, `onChange`, `disableSubmit`, `borderColor`, `isShowingAutocomplete`. | T/components/editor.ts:373-437,1085-1133,2464; T/editor-component.ts |
| 4.13 | Mouse | Click positions cursor (synthesized click); press/drag/release left to renderer selection. | T/components/editor.ts:632-694 |
| 4.14 | Autocomplete UI | Dropdown `SelectList`, `autocompleteMaxVisible` 3-20 (default 5), slash menu uses 12-32 col primary layout, Tab/Enter accept, re-query on cursor move, IME cursor stays correct while menu is open. | T/components/editor.ts:247-250,2464; CL:317, CL:328 |
| 4.15 | Autocomplete triggers | `/` commands only when first line and not mid-word (changed twice: empty-only at CL:910, then first line at CL:781); `@` and `#` file/attachment triggers; providers can declare `triggerCharacters`; tokens may follow `( [ { < \``; quoted paths for spaces; forced completion via Tab auto-applies a single match. | T/components/editor.ts:253-267; CL:781, CL:910, CL:313, CL:44 |
| 4.16 | Autocomplete engine | `CombinedAutocompleteProvider`: slash commands (fuzzy), args via async `getArgumentCompletions`, path completion (`~`, `./`, Windows drive letters), `@` fuzzy search spawning external `fd` (respects .gitignore, follows symlinks, includes hidden), 20 ms debounce, abortable. | T/autocomplete.ts:148,194,303,770; CL:468, CL:23 |
| 4.17 | Fuzzy | Consecutive-match bonus, gap penalty, exact match first, slash-separated queries, native substring search for long text. | T/fuzzy.ts:12-138; CL:1035, CL:63 |
| 4.18 | Not present | No vim keys (`VimEditor` is only a doc example built on `CustomEditor`), no shift-selection, no redo, no clipboard cut/copy inside editor, no mouse selection inside editor, no line numbers. | grep result, CA/core/extensions/types.ts:257-276 |
| 4.19 | App-level wrapper | `CustomEditor` layers app keys on the base editor: paste image (ctrl+v), interrupt (esc), exit (ctrl+d when empty), history actions, then delegates. | CA/modes/interactive/components/custom-editor.ts:88-146 |

## 5. Markdown, math, highlighting, diff

| # | Feature | Detail | Source |
|---|---|---|---|
| 5.1 | Parser | `marked` (bundled, re-exported) with strict `~~` strikethrough tokenizer; HTML rendered as plain text. | T/components/markdown.ts:1-24; CL:512, CL:156 |
| 5.2 | Blocks | Headings (underline H1), paragraphs, lists (ordered/unordered, nested, loose, task checkboxes, continuation indent, optional source marker preservation), blockquotes (nested lists, isolated style), code fences (indent configurable, border, `highlightCode` hook), tables (row dividers, min col width, wrapped cells, links without color leak), hr. | T/components/markdown.ts:200-230,529; CL:573, CL:401, CL:896, CL:698, CL:262 |
| 5.3 | Streaming stability | Partial closing fences do not shrink or flicker code blocks; partial `$` math kept pending. | CL:262; T/components/markdown.ts:47-58 |
| 5.4 | Inline | bold, italic, strike, underline, code, links (OSC 8 when supported else `text (url)`), autolink emails without `mailto:`. | T/components/markdown.ts:200; CL:495, CL:930 |
| 5.5 | Options | `preserveOrderedListMarkers`, `preserveBackslashEscapes`, `transform(markdown,width)`, `renderLatex`, default text style, paddingX/Y. | T/components/markdown.ts:220-229 |
| 5.6 | Perf | Cached render by (text,width); token reuse across theme/width changes. | CL:35 |
| 5.7 | LaTeX | Inline and display math to Unicode: fractions, scripts, symbols, aligned, cases, matrices, relational algebra, legacy font switches. 1506 LOC. | T/latex.ts; CL:153, CL:75, CL:44 |
| 5.8 | Syntax highlighting | Not in pi-tui. `MarkdownTheme.highlightCode(code, lang) -> string[]` is filled by coding-agent with `cli-highlight`, language validated first. Theme tokens `syntax*` (9). | T/components/markdown.ts:215; CA/modes/interactive/theme/theme.ts:1001-1018,1111-1126 |
| 5.9 | Diff | Coding-agent only (component 2.25). Tokens `toolDiffAdded/Removed/Context`. | CA/.../diff.ts |

## 6. Images

| # | Feature | Detail | Source |
|---|---|---|---|
| 6.1 | Protocols | Kitty graphics (`ESC _ G`), iTerm2 (`OSC 1337;File=`). No sixel (grep for "sixel" returns nothing). | T/terminal-image.ts:198-199 |
| 6.2 | Detection | Env based: Kitty, Ghostty, WezTerm, Warp = kitty; iTerm2 = iterm2; Windows Terminal, Alacritty, VS Code, Zed = no images; tmux, screen, cmux = no images. Overrides `PI_IMAGE_PROTOCOL`, setting `terminal.images`. | T/terminal-image.ts:75-159; CL:416, CL:86, CL:271 |
| 6.3 | Sizing | Cell pixel size via `CSI 16 t` reply (exact `CSI 6;h;w t` only); rows computed from dimensions parsed from PNG/JPEG/GIF/WebP headers; portrait capped by height; default width 60 cells. | T/tui.ts:965; T/terminal-image.ts (get*Dimensions); CL:405, CL:558, CAD/settings.md:102 |
| 6.4 | Image lines | `isImageLine` detects escape anywhere in line; image rows reserved; ids allocated random and deleted when lines change or exit. | T/terminal-image.ts:201-215; CL:848, CL:859 |
| 6.5 | Alt-screen images | Upload once, move placements without retransmit, clip at layout boundaries and sticky regions, WezTerm row-clear workaround. | T/tui-alt-screen.ts:216,1693-1711; CL:179-181, CL:84 |
| 6.6 | Fallback | Text placeholder with shortened clickable path. | CL:193, T/terminal-image.ts `imageFallback` |
| 6.7 | Clipboard image paste | `app.clipboard.pasteImage` (ctrl+v); native readers for macOS/Windows/X11 incl. file URLs. | CL:42, CL:17; T/native-platform.ts |

## 7. Input, keys, keybindings, config

### 7a. Escape/CSI table (what a Go terminal layer must emit or parse)

| Sequence | Purpose | Source |
|---|---|---|
| `CSI ?2026 h/l` | Synchronized output | T/tui-main-screen.ts:280 |
| `CSI ?1049 h/l` | Alt buffer | T/tui-alt-screen.ts:62 |
| `CSI ?7 l/h` | Autowrap off/on in alt screen | T/tui-alt-screen.ts:64 |
| `CSI ?1000/1002/1003/1004/1006 h/l` | Mouse press, button-motion, any-motion, focus events, SGR encoding | T/tui-alt-screen.ts:66-68 |
| `CSI ?2004 h/l` | Bracketed paste | T/terminal.ts:187,435 |
| `CSI ?25 h/l` | Cursor visibility | T/terminal.ts:508 |
| `CSI >7u`, `CSI ?u`, `CSI c` | Kitty keyboard protocol push flags 7 (disambiguate, event types, alternate keys), query, then DA1 as end marker | T/terminal.ts:12-14 |
| `CSI <u` | Pop Kitty keyboard flags | T/terminal.ts:397 |
| `CSI >4;2m` / `>4;0m` | xterm modifyOtherKeys 2 on/off (fallback when no Kitty answer) | T/terminal.ts:366,372 |
| `CSI 2J`, `CSI H`, `CSI 3J`, `CSI n;1H`, `CSI 2K`, `CSI nA/B`, `\r\n` | Clear screen/scrollback, addressing, line clear, relative moves | T/tui-main-screen.ts:283,428; T/tui-alt-screen.ts:1727 |
| `CSI ?2031 h/l` | Terminal color-scheme change notifications | T/tui.ts:926,956 |
| `CSI 16 t` | Query cell pixel size | T/tui.ts:967 |
| `OSC 10/11/4;n ;?` + `CSI c` | Query default fg/bg and 16-color palette in one round trip; DA1 reply marks the end | T/tui.ts:168-172; CL:11 |
| `OSC 0` | Window title | T/terminal.ts:529 |
| `OSC 8` | Hyperlinks | T/tui.ts:412; T/utils.ts:359 |
| `OSC 9;4;3` / `9;4;0` | Taskbar/tab progress, keepalive every 1 s | T/terminal.ts:9-10; CL:185, CL:461 |
| `OSC 52` | Clipboard write | T/tui-alt-screen.ts:1470 |
| `OSC 133 A/B/C` | Semantic prompt zones (skipped in width, used for prompt jump) | T/tui-alt-screen.ts:73; CL:162 |
| `OSC 1337;File=` / `ESC _ G ...` | iTerm2 / Kitty images | T/terminal-image.ts:198-199 |
| `ESC _ pi:c BEL` | Private APC cursor marker (internal) | T/tui.ts:196 |
| Parsed inputs | CSI-u, modifyOtherKeys, legacy ESC prefix, SGR mouse, focus in/out, bracketed paste, CPR-like replies | T/keys.ts, T/stdin-buffer.ts |

### 7b. Keys and keybindings

| # | Feature | Detail | Source |
|---|---|---|---|
| 7.1 | Key ids | `modifier+key`; modifiers ctrl, shift, alt, super; letters, digits, named keys, f1-f12, 32 symbols. `matchesKey(data,id)`, `parseKey`, `Key.*` constants. | CAD/keybindings.md "Key syntax"; T/keys.ts; CL:1147, CL:655, CL:518 |
| 7.2 | Input normalisation | Handles Kitty CSI-u incl. alternate/base-layout keys (non-QWERTY), keypad normalisation, key release/repeat (filtered unless `wantsKeyRelease`), modifyOtherKeys, legacy Alt+letter and Ctrl+symbol, Windows Terminal `0x08` = ctrl+backspace, tmux quirks. | CL:1091, CL:1107, CL:681, CL:349, CL:326, CL:359, CL:522, CL:705 |
| 7.3 | Stdin buffer | Splits batched input into whole sequences; incomplete sequences wait (50 ms sequence, 10 ms lone-escape timeouts; `PI_TUI_ESC_TIMEOUT` raises for SSH); bracketed paste re-wrapped; SGR mouse fragments not leaked. | T/stdin-buffer.ts:20-30; CL:132, CL:1081 |
| 7.4 | Registry | One global `KeybindingsManager`; definitions `{defaultKeys, description}`; user overrides shadow defaults and never evict unrelated ones; conflict reporting; empty list disables. Namespaces `tui.editor.*`, `tui.input.*`, `tui.select.*`, `tui.altScreen.*`, `app.*`. | T/keybindings.ts; CL:597, CL:1152 |
| 7.5 | User config | `<agent-dir>/keybindings.json`, `{ id: key | key[] }`, `/reload` applies. | CAD/keybindings.md:5-26 |
| 7.6 | Counts | 46 `tui.*` ids (editor 21, input 4, select 6, altScreen 15 incl. search) plus ~45 `app.*` ids (session, model, thinking, tree, scoped models, message, tools). | T/keybindings.ts; CAD/keybindings.md |
| 7.7 | Per-platform defaults | Windows/WSL swap: undo ctrl+z, followUp ctrl+q, paste alt+v, search ctrl+f, suspend none. | CAD/keybindings.md |
| 7.8 | Suspend / exit | `app.suspend` ctrl+z (Unix job control), Ctrl+D drains stdin up to 1 s to avoid Kitty release leaks over SSH. | CL:801-805; CAD/keybindings.md |
| 7.9 | Input listeners | `addInputListener` sees raw data before components; may `consume` or rewrite `data`. Extensions get it as `onTerminalInput`. | T/tui.ts:144; CA/core/extensions/types.ts:163 |
| 7.10 | Terminal quirk workarounds | Apple Terminal Shift+Enter via local modifier state (native), Windows VT input + Shift+Enter via native helper, Zellij, WezTerm Alt+Enter, Ghostty alt+backspace, IntelliJ no Shift+Enter, tmux `extended-keys`. | CL:359, CL:172, CAD/terminal-setup.md, CAD/tmux.md |

## 8. Theme and color

| # | Feature | Detail | Source |
|---|---|---|---|
| 8.1 | Theme file | JSON: `name`, `appearance?`, `vars`, `colors`, `export?`. 56 color tokens, 51 required. Variables chain; missing or circular = invalid. | CAD/themes.md:52-74; CA/modes/interactive/theme/theme-schema.json |
| 8.2 | Token groups | accent, border x3, success/error/warning, muted, dim, text, thinkingText; selectedBg, scrollbarTrack/Thumb, searchMatchBg/Text; userMessage x2, customMessage x3; toolPending/Success/ErrorBg, toolTitle, toolOutput; md x10; toolDiff x3; syntax x9; thinking x7 (off..max); bashMode. | theme-schema.json (enumerated) |
| 8.3 | Optional inheritance | scrollbarTrack->muted, scrollbarThumb->text, searchMatchBg->selectedBg, searchMatchText->text, thinkingMax->thinkingXhigh. | CAD/themes.md:96-105 |
| 8.4 | Color forms | `#rgb`, `#rrggbb`, `oklch()`, `okhsl()`, 0-255 index, var ref, `""` = terminal default. | CAD/themes.md:62-70; T/colors.ts |
| 8.5 | Color math | `Color` = indexed / sRGB / OKLCH; gamut mapping; `mixColors`; OKHSL; truecolor vs 256 quantisation; `styleText({fg,bg,bold,...})`. 367 + 233 LOC. | T/colors.ts; T/oklab.ts |
| 8.6 | `system` theme | Default. Queries terminal fg/bg/16 ANSI (waits at most 100 ms, applies late replies), derives hues from ANSI, enforces WCAG 4.5:1 body contrast, rebuilds on light/dark change. Fallbacks: bg only; nothing = ANSI indices. | CAD/themes.md:7-22; T/tui.ts:1466 |
| 8.7 | Selection | `theme: "name"` or `"light/dark"` pair; `--use-theme`; appearance from reported bg, then color-scheme notification, then `COLORFGBG`, then dark. | CAD/themes.md:26-46 |
| 8.8 | Loading | Built-in dark/light; user dir (hot-reload of active file only), project `.pi/themes` (after trust), package resources, `themes` setting. | CAD/themes.md:80-136 |
| 8.9 | Extension styling | `theme.fg/bg/bold/underline/inverse/style({fg,bg,bold})`, `theme.colors`, `theme.appearance`. Cached ANSI strings must be rebuilt in `invalidate()`. | CAD/tui.md "Apply themes correctly" |
| 8.10 | Capability override | trueColor: `COLORTERM`, `*-direct`, per-terminal table, `PI_TRUE_COLOR`, `terminal.trueColor`. | T/terminal-image.ts:75-159; CL:21 |

## 9. Text width and Unicode

| # | Feature | Source |
|---|---|---|
| 9.1 | `visibleWidth`: grapheme-based (Intl.Segmenter), ASCII fast path, ignores SGR, OSC 8, OSC 133, APC, tabs normalised. | T/utils.ts:250-313,394; CL:35, CL:1202 |
| 9.2 | Special clusters: ZWJ emoji, regional indicators (partial flags while streaming), Indic conjuncts, Thai/Lao AM, CJK fullwidth. | CL:1202, CL:704, CL:176, CL:435 |
| 9.3 | `wrapTextWithAnsi`: preserves SGR across lines, CRLF/CR aware, iterative (no stack overflow), CJK break rules, OSC 8 BEL terminators kept. | T/utils.ts:891; CL:211, CL:413 |
| 9.4 | `truncateToWidth`: streaming for huge strings, optional pad, always closes SGR and OSC 8. | T/utils.ts:1112; CL:1112 |
| 9.5 | `sliceByColumn` / `extractSegments`: ANSI-aware column slicing used by overlays. | T/utils.ts:1254,1314; CL:271 |
| 9.6 | `getOsc8LinkAtColumn`, `getGraphemeCellRange` for click and selection. | T/utils.ts:335,359 |

## 10. Ring 2/3 - extension-facing UI primitives and interactions

### 10a. `ctx.ui` (ExtensionUIContext)

| # | Method | Source (CA/core/extensions/types.ts) |
|---|---|---|
| 10.1 | `select`, `confirm`, `input`, `editor` dialogs, all with `AbortSignal` and `timeout` (live countdown) | :113-119,151-157,240 |
| 10.2 | `notify(message, info|warning|error)` | :160 |
| 10.3 | `onTerminalInput(handler)` raw input with consume/rewrite | :131,163 |
| 10.4 | `setStatus(key,text)` footer status slots | :166 |
| 10.5 | `setWorkingMessage`, `setWorkingVisible`, `setWorkingIndicator({frames,intervalMs})`, `setHiddenThinkingLabel` | :169-185 |
| 10.6 | `setWidget(key, string[] | factory, {placement: aboveEditor|belowEditor})`; string widgets capped at 10 lines | :188-193; II:2394 |
| 10.7 | `setFooter`, `setHeader` component factories | :201-208 |
| 10.8 | `setTitle` | :211 |
| 10.9 | `custom(factory, {overlay, overlayOptions, onHandle})` returns Promise resolved by `done()`; component disposed on done | :214-228 |
| 10.10 | `pasteToEditor`, `setEditorText`, `getEditorText` | :231-237 |
| 10.11 | `addAutocompleteProvider(factory)` stacked providers, `shouldTriggerFileCompletion` | :243; CL:468 |
| 10.12 | `setEditorComponent(factory)` replacing editor (CustomEditor base) | :278-281 |
| 10.13 | Theme: `theme`, `getAllThemes`, `getTheme`, `setTheme` | :284-293 |
| 10.14 | `getToolsExpanded` / `setToolsExpanded` | :296-299 |
| 10.15 | Outside `ctx.ui`: tool `renderCall`/`renderResult` returning Components, `registerMessageRenderer`, `registerShortcut`, `registerCommand` | :636-639,1628-1663 |
| 10.16 | Mode guard: `ctx.mode` = tui / rpc / json / print; dialogs also work in RPC mode. | :318-323 |

### 10b. Interaction matrix (ring 3)

| # | Interaction | How Pi handles it | Source |
|---|---|---|---|
| 10.20 | Streaming output x editor | Streaming updates call `requestRender()`; coalescing at 16 ms; keyboard preempts; streaming component reused and `updateContent` called, not replaced. Editor is in the dock (fullscreen) or last children (regular), so it never scrolls away. | T/tui.ts:1010-1116; CA/.../assistant-message.ts:47-91 |
| 10.21 | Streaming x scrollback | Regular: appended lines commit to scrollback; diff only touches lines still in viewport. Fullscreen: `follow: end` keeps pinned until user scrolls; jump-to-end indicator restores. | CL:909, CL:74 |
| 10.22 | Overlay x editor | Capturing overlay steals focus, editor keeps rendering; non-capturing overlay keeps editor focus. Selectors replace the editor in `editorContainer` rather than overlaying (extension select/input/editor). | II:2633-2762; T/tui.ts:326 |
| 10.23 | Widgets x streaming | Widgets are containers above/below editor, part of dock. | II:606-607, chat-viewport.ts |
| 10.24 | Fullscreen keys vs editor keys | `tui.altScreen.*` deliberately shadow unmodified editor PageUp/PageDown/Home/End; Ctrl variants remain for the editor. | T/keybindings.ts comment; CL:181 |
| 10.25 | Mouse x extensions | Unhandled wheel scrolls nearest ScrollView; unhandled primary drag becomes selection; extension can `capture`. | CAD/tui.md |
| 10.26 | Theme change x caches | Theme change calls `invalidate()` on the tree; components must not cache colored strings. | CAD/tui.md; CL:35 |
| 10.27 | Mode switch at runtime | Renderer replaced without replaying content; layout root cleared on old renderer. | II:838-872; CL:155 |
| 10.28 | Exit | Alt-screen prints full final transcript (or resume hint) after leaving alt buffer; regular moves cursor to end of content. | T/tui-alt-screen.ts:395-407; CL:1040 |
| 10.29 | Terminal focus events | Losing focus must not repaint or clear selection. | CL:135 |

## 11. Ring 4 - edge cases worth regression tests (Fixed entries)

| # | Edge case | Source |
|---|---|---|
| 11.1 | Lines wider than terminal via padding, overlays, OSC/APC sequences, tabs; narrow editor indicator; narrow settings list | CL:111, CL:202, CL:294, CL:335, CL:1006, CL:645, CL:785 |
| 11.2 | Empty rows below footer after content shrinks; stale lines at zero height | CL:852, CL:327 |
| 11.3 | Hidden cursor after exit when render pending or overlay closed during shutdown | CL:29, CL:210, CL:853 |
| 11.4 | Kitty keyboard mismatched or delayed replies must not enable the protocol falsely | CL:349, CL:326 |
| 11.5 | Split `Alt+Enter` over SSH read as Escape | CL:132 |
| 11.6 | Kitty release events leaking to shell after exit | CL:801, CL:805 |
| 11.7 | Pasted text that looks like key events (`:3F`, CSI-u ctrl sequences) | CL:1050, CL:455 |
| 11.8 | Duplicate printable input from CSI-u plus raw char | CL:447 |
| 11.9 | Paste marker registry corruption on delete/undo | CL:212, CL:639 |
| 11.10 | Mouse: hover changes selection; generic release button; phantom selection on pane focus | CL:64, CL:127, CL:177 |
| 11.11 | Huge markdown, huge single line, huge image output | CL:102, CL:402 |
| 11.12 | Blockquote/heading/table style leaks | CL:112, CL:573, CL:584 |
| 11.13 | Multiplexers: hyperlinks forced off under screen, probed under tmux, images off | T/terminal-image.ts:82-90; CL:343, CL:500 |
| 11.14 | Terminal reports batched color-scheme replies | CL:184 |

## 12. Ring 5 - history (what was added, what was removed)

| Version | Change | Source |
|---|---|---|
| 0.29-0.31 (2025-12/2026-01) | Word nav in Input, full Unicode input, ZWJ width, grapheme `visibleWidth`, auto-space path paste, `TUI.onDebug` | CL:1204-1210, CL:1202 |
| Early 0.x | Key release (Kitty flag 2), `StdinBuffer`, `EditorComponent`, overlays experimental, `OverlayOptions`, `OverlayHandle`, `Focusable`/IME cursor, Editor scrolling | CL:1081-1107, CL:998, CL:975-982 |
| Early 0.x | Kill ring, undo, char jump, sticky column, fuzzy slash autocomplete, hardware cursor default off | CL:941-960, CL:1035 |
| Removed | `isXxx()` key detectors (use `matchesKey`); editor-only keybinding store replaced by single global manager with `tui.*` namespaces; koffi dependency replaced by tiny vendored native helper; env defaults removed from pi-tui, `PI_DEBUG_REDRAW` renamed `PI_TUI_DEBUG_REDRAW`; `queryTerminalColorScheme/BackgroundColor` replaced by `queryTerminalColors` | CL:1147, CL:597, CL:368, CL:70, CL:11 |
| Behaviour reversals | Slash menu: any position, then only when editor empty (CL:910), then first line (CL:781). `clearOnShrink` default false. Alt-screen wheel step 3 to 1 to auto. Hardware cursor default off. Height change no longer always full redraw. | CL:781,910,818-819,168,960 |
| 0.84.0 | Big bang: dual renderers, VStack/HStack/ScrollView, scrollbars, LaTeX, OSC 133, notifications | CL:153-166 |
| 0.84.x-0.99 | Search, selection granularity, copyOnSelect, OSC 52, capability overrides, color values API, OKLCH, system theme, native clipboard | CL:92-126, CL:12-14, CL:8 |

## 13. Negative findings (for the bubbletea lane)

1. No vim/modal editing in pi-tui. Extension example only (`modal-editor.ts`).
2. No sixel.
3. No text selection inside the Editor; selection exists only as alt-screen renderer-level text selection over rendered rows.
4. `ScrollView` is vertical only; `HStack` exists but coding-agent composes only VStack + ScrollView. No sidebar shipped yet (PLAN: "Future uses").
5. No virtualization: whole transcript is rendered each frame, relying on leaf render caches (PLAN: "Non-goals").
6. Syntax highlight, diff, mermaid, theme JSON, footer, selectors are coding-agent code, not TUI framework code.
7. `@` autocomplete needs external `fd`; clipboard image/file readers and macOS/Windows modifier-state are native addons (darwin/linux/win32 in `packages/tui/native`).
8. No redo, no persistent prompt history inside editor (history is fed by caller, max 100).
9. Regular mode has no mouse, no scrollbars, no search, no sticky regions by design.
10. Windows has no job-control suspend.

## 14. Needs list

Group A: framework and widget needs (what to test bubbletea against). Group B: process/environment needs (mostly Go-native work regardless of framework).

### A. Framework and widget needs

| N | Need | Source |
|---|---|---|
| A1 | Render loop that draws inline in the main buffer, keeps native scrollback, and updates only changed lines by diffing prior vs new line arrays (not full-frame redraw) | T/tui-main-screen.ts:364-395 |
| A2 | A second, alt-screen renderer with the same component interface and an app-owned viewport | T/tui.ts:453, T/tui-alt-screen.ts |
| A3 | Wrap every frame in CSI 2026 synchronized output | T/tui-main-screen.ts:280 |
| A4 | Full clear-and-redraw on width change; skip on height change when configured (Termux); optional clear-on-shrink | T/tui-main-screen.ts:341-361 |
| A5 | Append-only growth that commits lines to native scrollback without repainting scrolled-out lines | CL:909 |
| A6 | Frame coalescing at 16 ms with immediate render on input | T/tui.ts:507,1116 |
| A7 | Per-line style/hyperlink reset guarantee so components cannot leak SGR or OSC 8 | T/tui.ts:412 |
| A8 | Components render to width-bounded ANSI strings; width helpers (visibleWidth, wrap, truncate with pad, slice by column) grapheme-, ANSI-, OSC 8-, OSC 133-, tab-, CJK-, ZWJ-, regional-indicator-aware | T/utils.ts |
| A9 | Component tree with Container, invalidate propagation, render caches keyed by width and content | T/tui.ts:347 |
| A10 | Flexbox-lite VStack/HStack: basis/grow/shrink/min/max, gap, align, visible predicate; rebuilt per frame | T/components/stack.ts |
| A11 | Vertical ScrollView with follow-end, primary flag, chained overscroll, proportional scrollbar (auto/always/hidden, drag, track click), jump-to-end label | T/components/scroll-view.ts |
| A12 | Sticky bottom dock beside a scrolling transcript, shrinkable with min sizes | CA/.../chat-viewport.ts |
| A13 | Overlay stack with percent/absolute size, 9 anchors, offsets, margins, visible predicate, non-capturing mode, handle API, focus restore, ANSI-safe compositing over wide chars | T/tui.ts:203-345,415 |
| A14 | Single-focus model with `Focusable` and cursor marker so hardware cursor follows the text cursor for IME | T/tui.ts:180-196 |
| A15 | Mouse: SGR press/release/drag/move/wheel, click counts, modifiers, per-component dispatch with handled/capture/focus/render results, wheel accel and Alt multiplier | T/tui.ts:21-60 |
| A16 | App-owned text selection over rendered rows: char/word/paragraph granularity, edge auto-scroll, copy via callback or OSC 52, OSC 8 link activation | CL:93,134-136,157-158 |
| A17 | Incremental transcript search with highlight styling, next/prev, buttons | T/alt-screen-search.ts |
| A18 | OSC 133 prompt-zone navigation | T/tui-alt-screen.ts:73 |
| A19 | Transient notification stack (fullscreen) | T/components/alt-screen-flash.ts |
| A20 | Multi-line editor: word wrap with cursor column, CJK/grapheme correct, max height 30% with scroll indicators, padding | T/components/editor.ts:520 |
| A21 | Editor keys: Emacs set (section 4.3), configurable through the keybinding registry | T/keybindings.ts |
| A22 | Kill ring with accumulation, yank, yank-pop | T/kill-ring.ts |
| A23 | Undo stack with word coalescing and paste-registry restoration | T/undo-stack.ts, CL:212 |
| A24 | Prompt history with draft preservation, edge-aware up/down, dedicated history actions | CL:318 |
| A25 | Bracketed paste with atomic large-paste markers (over 10 lines or 1000 chars), expansion on submit, renumbering, undo integration | T/components/editor.ts:1296-1416 |
| A26 | Sticky visual column; char-jump mode; word navigation with Unicode boundaries | CL:836, CL:866, T/word-navigation.ts |
| A27 | Autocomplete dropdown with pluggable providers, trigger characters, async abortable debounced lookup, quoted paths, single-match auto-apply, stacked extension providers | T/autocomplete.ts, T/components/editor.ts |
| A28 | Fuzzy filter/match with scoring (consecutive bonus, gap penalty, exact-first) | T/fuzzy.ts |
| A29 | SelectList (filter, scroll window, columns, mouse) and SettingsList (cycle values, submenu, search) | T/components/select-list.ts, settings-list.ts |
| A30 | Single-line Input with scroll, prompt, placeholder, kill ring, undo | T/components/input.ts |
| A31 | Loader/spinner with custom frames and cancellable variant | T/components/loader.ts |
| A32 | Markdown to ANSI: headings, lists (nested, task, loose), tables, blockquotes, fences with highlight hook and border, hr, inline styles, links (OSC 8 or text fallback), strict strikethrough, streaming-safe partial fences, width-aware source transform | T/components/markdown.ts |
| A33 | LaTeX to Unicode (inline, display, fractions, scripts, matrices, cases, aligned) | T/latex.ts |
| A34 | Syntax highlighting per language returning per-line ANSI (any Go lib such as chroma) | CA/.../theme.ts:1001 |
| A35 | Word-level intra-line diff highlighting plus +/-/context coloring | CA/.../diff.ts |
| A36 | Inline images: Kitty (upload once, move placements, delete ids, clip in scroll regions) and iTerm2, dimension parsing, row reservation, text fallback | T/terminal-image.ts, T/tui-alt-screen.ts:1693 |
| A37 | Theme system: JSON with vars, 56 tokens, OKLCH/OKHSL/hex/256/default forms, optional inheritance, light/dark pair, runtime reload, `system` theme derived from terminal palette with contrast enforcement | CAD/themes.md |
| A38 | Color math: sRGB/OKLCH/OKHSL conversion, gamut mapping, mix, truecolor to 256 quantisation | T/colors.ts, T/oklab.ts |
| A39 | Keybinding registry: namespaced ids, defaults, user overrides that shadow, empty-list disable, conflict detection, hot reload, per-platform defaults | T/keybindings.ts |
| A40 | Extension-facing UI API surface (section 10a): dialogs with timeout/abort, notify, status slots, widgets (above/below), header/footer/editor replacement, custom component with completion callback, overlays with handle, raw input hook, theme access | CA/core/extensions/types.ts:149-299 |
| A41 | Runtime swap between regular and fullscreen renderers without replaying content | CL:155 |
| A42 | Exit behaviour: fullscreen prints final transcript or resume hint; restore cursor and modes | T/tui-alt-screen.ts:395-407 |

### B. Process and environment needs

| N | Need | Source |
|---|---|---|
| B1 | Raw mode, restore on exit and panic; bracketed paste on/off | T/terminal.ts:186-190,435 |
| B2 | Stdin sequence buffer: reassemble split CSI/OSC/SGR-mouse, lone-escape timeout (10 ms default, env override for SSH), re-wrap pastes | T/stdin-buffer.ts; CL:132 |
| B3 | Keyboard protocol negotiation: push Kitty flags 7, query, DA1 sentinel, response-driven (not timeout) fallback to modifyOtherKeys 2, ignore mismatched replies; pop on exit | T/terminal.ts:12-14,366-397; CL:326, CL:349 |
| B4 | Key-id parser/matcher over CSI-u, modifyOtherKeys, legacy ESC-prefix, keypad, base-layout fallback, release/repeat filtering | T/keys.ts; CL:1107 |
| B5 | Resize handling and dimension refresh after suspend/resume; COLUMNS/LINES fallback | CL:1049, CL:85 |
| B6 | Drain stdin up to 1 s on exit (Kitty release leakage over SSH) | CL:801,805 |
| B7 | Ctrl+Z suspend/resume on Unix; none on Windows | CAD/keybindings.md |
| B8 | Windows: enable VT input (needs native call), Shift+Enter modifier via native helper, truecolor without WT_SESSION, right-click paste, Windows/WSL default keymap | CL:368, CL:172, CL:110 |
| B9 | Apple Terminal Shift+Enter via local modifier state (macOS only, not over SSH) | CL:359; CAD/terminal-setup.md |
| B10 | Capability detection table (TERM, TERM_PROGRAM, COLORTERM, KITTY_WINDOW_ID, WT_SESSION, TMUX, screen, Warp, Zed, JetBrains) plus tmux hyperlink probe (`termfeatures`) plus env and settings overrides for hyperlinks, images, truecolor | T/terminal-image.ts:60-159 |
| B11 | Terminal color queries: OSC 10/11/4 plus DA1 sentinel, 100 ms startup budget with late-reply apply, `CSI ?2031` scheme notifications | T/tui.ts:168-172,926 |
| B12 | Cell pixel size query (`CSI 16 t`), exact-reply parsing | T/tui.ts:965; CL:405 |
| B13 | OSC 9;4 progress with 1 s keepalive; OSC 0 title | T/terminal.ts:9-10,529 |
| B14 | Clipboard: OSC 52 write; native read of text, images, file URLs on macOS/Windows/X11 (cgo or exec fallback) | T/tui-alt-screen.ts:1470; CL:42, CL:17 |
| B15 | `fd` binary dependency (or a Go walker with .gitignore support) for `@` fuzzy file search, symlink following, hidden files, abort | T/autocomplete.ts:148-194 |
| B16 | Multiplexer awareness: mouse mode downgrade, hyperlink/image gating, tmux extended-keys guidance | CL:146, CAD/tmux.md |
| B17 | Debug capture: raw ANSI write log, redraw-reason log, crash dump | CL:1077, CL:70 |
| B18 | External editor spawn with terminal handoff | CA/modes/interactive/external-editor.ts |
| B19 | Settings surface consumed by TUI: tuiMode, fullscreen scrollbar/copy/wheel/exit output, editorPaddingX, autocompleteMaxVisible, showHardwareCursor, terminal.* | CAD/settings.md:90-111 |

## 15. Adoption note for Go rewrite (short)

- Pi renders `[]string` per component, which resembles bubbletea's `View() string`. Bubbletea behaviour was not verified in this lane. Inline mode vs alt-screen maps to A1/A2. The bubbletea lane should check A1, A3, A5, A13, A15-A16, B2-B4, B11 first, because they are the most likely gaps.
- The 16 hardest items by LOC are A8, A20-A27, A32-A33, B3-B4. Widgets in the Bubbles library cover only A29-A31 partially.

## Unresolved questions

1. Exact key count per namespace was tallied from T/keybindings.ts by hand; verify with a script before using it as a spec.
2. CL line refs were re-checked by grep on a distinctive phrase, but some rows cite the "first match" of a phrase; re-grep before quoting in a spec.
3. Not read line by line: T/keys.ts (1401 LOC), T/tui-alt-screen.ts selection internals, T/latex.ts, T/utils.ts wrap internals. Behaviour inferred from headers, exports and changelog.
4. Test suite (`packages/tui/test`) was not inspected; it is the best source for exact expected outputs.
5. Whether the user wants regular-mode parity only, or also the fullscreen feature set (A11-A19), is not stated. The Pi default is regular mode, so A11-A19 may be phase 2.
6. GitNexus MCP was not used; direct file reads covered the scope.

Status: DONE_WITH_CONCERNS
Summary: Inventory of Pi TUI written with 42 framework needs and 19 environment needs, each with a source.
Concerns: keys.ts, latex.ts, alt-screen selection internals and the test suite were not read in full; a few CL line numbers may be off by a few lines.
