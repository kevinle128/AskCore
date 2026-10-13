---
phase: 1
title: "Root Bubble Tea v2 migration and dependencies"
status: completed
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: Root Bubble Tea v2 migration and dependencies

## Goal

The root module builds against the D14 graph (patched Bubble Tea v2.0.10, exact Ultraviolet pin), Ask packages import no Bubble Tea v1 or lipgloss v1, and a test fails if a pin moves. Each later phase adds its own library in the same change as its first import, because `go mod tidy` removes unused requirements.

## Evidence (scouted 2026-10-09)

- Root `go.mod:9-10` pins `github.com/charmbracelet/bubbletea v1.3.10` and `github.com/charmbracelet/lipgloss v1.1.0`. `cmd/tui/main.go` is the only importer: a demo list model (`main.go:37-118`) started by `runInteractive` (`main.go:125`), called from `headless.go:140` when no prompt is given.
- Patched Bubble Tea source: `plans/261007-1156-inline-resize-fix/artifacts/replay-candidate/source/bubbletea/` with `patches/{tea,input,screen,cursed_renderer}.go.patch`, new `inline_replay.go` and `replay_test.go`, provenance in `PROVENANCE.md` (MIT license kept).
- Precedent for a patched copy: `third_party/acp-go-sdk` with `PATCH.txt` and `go.mod:411-412` (`replace github.com/coder/acp-go-sdk => ./third_party/acp-go-sdk`).
- Compatible versions ([research](research/researcher-01-test-and-render-libs.md)): lipgloss v2 ≤ v2.0.5, glamour v2.0.1, chroma v2.27.0, `x/exp/teatest/v2 v2.0.0-20261008172826-faa4adf95555`, `x/exp/golden v0.1.0`, `x/vt v0.0.0-20261004011457-ad85c59fdf4e`. None raises Ultraviolet.
- Depguard: `.golangci.yml:28-46` (tui-client-boundary) allows terminal UI libraries in `internal/tui` and `cmd/tui`.

## Files

- Create: `third_party/bubbletea/` (copy of `source/bubbletea`, non-test files plus `replay_test.go`, `LICENSE`, `go.mod`), `third_party/bubbletea/PATCH.txt`.
- Create: `internal/tui/deps_pin_test.go` (pin guard), `internal/tui/renderer_patch_test.go` (patch guard through the public API).
- Modify: `go.mod`, `go.sum` (add `charm.land/bubbletea/v2 v2.0.10` with `replace charm.land/bubbletea/v2 => ./third_party/bubbletea`; require Ultraviolet at the exact pin; add lipgloss v2.0.5 and x/vt, which the phase 1 tests import; drop the direct v1 Bubble Tea and lipgloss requirements). glamour and chroma arrive in phase 5; teatest and golden in phase 3. The tool dependency `github.com/MirrexOne/unqueryvet@v1.5.4` (through golangci-lint) still requires v1 in the module graph; that is expected.
- Modify: `cmd/tui/main.go` (remove the demo model; `runInteractive` returns a clear "interactive TUI not built yet" error with exit code 1 until phase 7 replaces it).
- Modify: `.golangci.yml` only if depguard names the old module paths.

## Tests first

| Test | Proves | Fails before the change because |
|---|---|---|
| `TestDependencyPins` (`internal/tui/deps_pin_test.go`) | `go list -m` shows Bubble Tea v2.0.10 replaced by `./third_party/bubbletea`, Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7`, lipgloss v2.0.5; `go list -deps ./cmd/... ./internal/... ./pkg/...` contains no `github.com/charmbracelet/bubbletea` or `github.com/charmbracelet/lipgloss` v1 package | Modules are absent; `cmd/tui` imports v1 |
| `TestInlineReplayPatchPresent` (`internal/tui/renderer_patch_test.go`) | The replay API added by `inline_replay.go` exists and one replay call redraws the committed lines once, read through an `x/vt` emulator. The 120 ms settle timer is application code and is tested in phase 3 | Bubble Tea v2 is not in the graph |
| `replay_test.go` (copied upstream-patch test) | The patch behavior inside the library | Not yet copied |
| `TestInteractiveWithoutTUIFailsClearly` (`cmd/tui`) | `ask` with no prompt exits 1 with the message, no demo list | Demo still runs |

## Steps

1. Write the four tests; run `go test ./internal/tui ./cmd/tui` and record the expected failures.
2. Copy `source/bubbletea` into `third_party/bubbletea`; write `PATCH.txt` in the acp-go-sdk style: upstream URL, version v2.0.10, module sum before the copy, license, the four changed files and two new files, why, and how to re-create the copy from the patches.
3. Update `go.mod` with the replace line and comment, the exact Ultraviolet requirement, and the new libraries at the versions above; run `go mod tidy`.
4. Remove the demo from `cmd/tui/main.go`; keep `main` and the mode dispatch.
5. Check the resolved graph: `go list -m charm.land/bubbletea/v2 github.com/charmbracelet/ultraviolet charm.land/lipgloss/v2` prints the pins; `go list -deps ./cmd/... ./internal/... ./pkg/... | grep -E '^github.com/charmbracelet/(bubbletea|lipgloss)(/|$)'` prints nothing.
6. Run the verification commands.

## Verification

- `go test ./internal/tui/... ./cmd/tui/...`
- `go -C third_party/bubbletea test ./...` (the copy is its own module; `go test ./...` from the root does not run it, as with `third_party/acp-go-sdk`)
- `go vet ./...`
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`
- `go test ./...` (no regressions in headless, leader and ACP tests)

## Rollback

Revert the `go.mod`/`go.sum` changes and delete `third_party/bubbletea`; no runtime behavior depends on it until phase 3.

## Risks

- `go mod tidy` may pick a newer Ultraviolet through another module: the explicit requirement and `TestDependencyPins` catch it.
- golangci-lint pulls lipgloss v2.0.3 indirectly, below the pin; `TestDependencyPins` catches a future rise.

## Result (2026-10-10)

Verified by `go test ./...`, `go vet ./...`, golangci-lint (0 issues) and `go -C third_party/bubbletea test ./...`. Resolved graph: Bubble Tea v2.0.10 replaced by `./third_party/bubbletea`, Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7`, lipgloss v2.0.5, no v1 package in `./cmd/... ./internal/... ./pkg/...`.

Deviations:
- `github.com/creack/pty v1.1.24` was added as a test dependency; `TestInteractiveWithoutTUIFailsClearly` needs a real terminal on stdin and stdout. Phases 7 and 8 reuse it.
- `TestInlineReplayPatchPresent` checks the committed lines and exactly one purge sequence after shutdown, because the program erases its live view on exit. The live-region redraw after a resize settle is asserted in `TestResizeRebuildKeepsDraftAndFocus` (phase 3).
- The four patch files are copied into `third_party/bubbletea/patches/`, so `PATCH.txt` does not depend on the plans folder.
