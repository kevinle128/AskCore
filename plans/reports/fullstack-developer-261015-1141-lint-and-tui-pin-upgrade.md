# golangci-lint v2.14.0 and TUI pin upgrade

## Changed versions (root go.mod)

- golangci-lint v2.12.2 -> v2.14.0, staticcheck (honnef.co/go/tools) v0.7.0 -> v0.8.1.
- Pinned terminal stack: lipgloss v2.0.5 -> v2.0.6; Ultraviolet 20260703014108-f5a850f9c2b7 -> 20260811164956-006e29f97886; x/ansi v0.11.7 -> v0.11.8.
- Moved with them: go-colorful v1.4.0 -> v1.4.1, go-runewidth v0.0.23 -> v0.0.24, xo/terminfo 2022 -> v1.0.0 (lint dependency, indirect), x/sys v0.47.0 -> v0.48.0, x/term v0.45.0 -> v0.46.0, x/text v0.41.0 -> v0.42.0, santhosh-tekuri/jsonschema v6.0.2 -> v6.0.3 (direct requires that lint raised), plus about 90 lint-only linter modules.
- Not moved: bubbletea (v2.0.10 copy), glamour v2.0.1, chroma v2.27.0, x/vt, goldmark.

## Patched Bubble Tea copy

- `third_party/bubbletea/go.mod` and `go.sum` now use the same Ultraviolet, x/ansi, go-colorful, go-runewidth. x/sys and x/sync are at the minimum that Ultraviolet needs. I kept the upstream `go.mod` shape (tidy would drop x/exp/golden and go-udiff).
- The Go source of the copy and the patch are unchanged. `go -C third_party/bubbletea test ./...` passes, including the renderer patch test.

## Ultraviolet changes that affect rendering (diff of the module cache sources)

- `terminal_renderer.go`: a line that holds a wide cell or a cell with a width mismatch is repainted whole (erase from column 0, absolute move) and not diffed. Different escape sequences, same visible result.
- Fullscreen shrink forces a full repaint; the cursor model resets on resize; the old buffer is resized before the diff. This is near the resize guard in our patch, and the patch test and the `ask-resize` e2e case pass.
- `styled.go`: combining marks and grapheme clusters fold into the previous cell; any positive width is accepted (ZWJ emoji no longer dropped).
- `buffer.go`: `Line.Render` keeps trailing spaces (pending blanks).
- go-runewidth v0.0.24 and x/ansi v0.11.8 are width and decode fixes.

## Tests

- `go test -count=1 ./internal/tui/... ./internal/tuiclient/... ./internal/tuiext/... ./cmd/tui/...`: all pass. No golden file changed, none was regenerated.
- e2e `python3 e2e/tui/run.py`: 24 of 24 pass (12 cases on Alacritty and 12 on Ghostty: idle, turn, resize, selector, providers, mouse, exit-quit, exit-ctrl-c, exit-sigterm, exit-sighup, copy, in-process).

## Lint

- macOS (fresh cache): 0 issues. Linux (GOOS=linux, fresh cache): 0 issues.
- Staticcheck v0.8.1 reports SA4023 on `internal/leader` (client.go Stop, evict.go evictZombie) on macOS only, because the darwin stub of the process-handle functions always fails. The three `//nolint:staticcheck` lines stay, with the reason. Without them Linux is clean and macOS has 4 issues.

## Verification

- macOS: `go vet ./...` and `go test -count=1 ./...` pass.
- Linux container golang:1.27.0 (arm64), non-root user, real git repo, ripgrep, no API-key env: vet clean, full test run twice with no failure. Lint in the container: 0 issues (the first container run was cut by a client timeout after vet and both test runs, so lint ran again in a second container).

## Docs

- No doc outside `plans/` states these versions (checked AGENTS.md, docs/, READMEs). Old plans and journals are historical records and stay as they are.
- CHANGELOG: no entry. Users see no difference; no golden changed.

## Files changed

- Deps and pins: `go.mod`, `go.sum`, `third_party/bubbletea/go.mod`, `third_party/bubbletea/go.sum`, `internal/tui/deps_pin_test.go` (pin values only; the comment stays accurate).
- Nolint (kept): `internal/leader/client.go`, `internal/leader/evict.go`.
- Renderer or golden fix: none.
- Docs: none.
