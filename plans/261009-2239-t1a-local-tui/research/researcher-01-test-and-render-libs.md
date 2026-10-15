# Test and render libraries for the T1a TUI

Date: 2026-10-09. Researcher subagent; saved by the plan author.
Method: a scratch module in `/tmp/rs1` pinned `charm.land/bubbletea/v2 v2.0.10`, Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7` and `x/ansi v0.11.7`, added all candidates, and compiled and ran one test that used them.
Result: no candidate raised the Ultraviolet pin; `lipgloss/v2` resolved to v2.0.4.
The root `go.mod` still has Bubble Tea v1 and lipgloss v1; the root migration is T1 work (D14), so this is expected.

## Candidates

| Dependency | Version | License | Charm UI dependency | Ultraviolet pin |
|---|---|---|---|---|
| `github.com/charmbracelet/x/exp/teatest/v2` | `v2.0.0-20261008172826-faa4adf95555` | MIT | bubbletea v2 (requires rc.1; MVS selects v2.0.10) | safe |
| `github.com/charmbracelet/x/exp/golden` | v0.1.0 | MIT | none | safe |
| `github.com/charmbracelet/x/vt` | `v0.0.0-20261004011457-ad85c59fdf4e` (T0 used it) | MIT | Ultraviolet `…20260303` only | safe |
| `charm.land/glamour/v2` | v2.0.1 | MIT | lipgloss v2.0.4 | safe |
| `github.com/alecthomas/chroma/v2` | v2.27.0 | MIT | none | safe |
| `github.com/yuin/goldmark` | v1.8.6 | MIT | none | safe |

Do not use `charm.land/lipgloss/v2` v2.0.6 or later: it requires Ultraviolet `…20260811`, above the D14 pin.
The unversioned `x/exp/teatest` targets Bubble Tea v1; use the `/v2` module.

## Testing

teatest v2 API: `NewTestModel(tb, m, WithInitialTermSize(w, h))`, `WaitFor(tb, r, cond, WithDuration, WithCheckInterval)`, `Send`, `Type`, `Quit`, `FinalModel`, `FinalOutput`, `RequireEqualOutput` (golden files in `testdata/`, refreshed with `go test -update`; the flag is registered by `x/exp/golden`).

teatest output is the raw ANSI stream, which is brittle as a golden.
Feed the bytes into an `x/vt` emulator and golden the plain cell grid instead.
The read-back API of `x/vt` was not checked; T0 `terminal_test.go` uses it and is the reference.

Suggested layers: direct `Update`/`View` component tests; screen goldens through teatest or `x/vt` plus `golden.RequireEqual`; E2E of the real binary in a PTY (none of these libraries provide that).

## Markdown

glamour v2.0.1 renders whole documents; it has no incremental API.
Re-rendering the full reply on each delta is O(n²), and partial input (open fence, list, table) renders badly.
Crush (`internal/ui/chat/streaming_markdown.go`) caches a stable prefix up to the last blank line with no open fence, list, table, quote or setext heading, re-renders only the remainder, falls back to a full render when unsure, drops the cache on width change, and serialises renderer calls with a mutex.
Crush is FSL-1.1-MIT: read only, do not copy code.
Width: `glamour.WithWordWrap(w)`, one renderer per width.
Syntax highlighting: chroma v2 (already a glamour dependency).
Alternatives: goldmark with an own renderer (most code), or a minimal renderer for fences, inline code, bold and lists (cheapest, partial coverage).

## Not covered

Pi and OpenCode streaming approach; `x/vt` read-back API; glamour output on a half-open fence.
