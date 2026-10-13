---
phase: 7
title: "Terminal lifecycle and ask entry"
status: completed
priority: P1
effort: "1d"
dependencies: [6]
---

# Phase 7: Terminal lifecycle and `ask` entry

## Goal

`ask` with no prompt starts the TUI through the composition root, and every exit path detaches and restores the terminal.

## Scope

- `cmd/tui` builds the leader client (phase 2) and the TUI model; replaces the phase 1 placeholder in `runInteractive`.
- Startup S1 (starting leader) and S2 (leader connect failed: start the in-process agent; footer `in-process`; quitting stops the run).
- Quit: ctrl+c clears the draft, then confirm (S15, 2 s window); ctrl+d on an empty draft; `/quit`; detached message (S16); the run continues in the leader.
- Signals: start the program with `tea.WithoutSignalHandler()` (Bubble Tea otherwise handles SIGINT/SIGTERM itself and SIGTERM exits 0; it has no SIGHUP handler). `ask` handles SIGINT, SIGTERM and SIGHUP: detach, restore, exit 130, 143, 129 (reuse `signalExitCodes`, `cmd/tui/headless.go:35-42`).
- Panics: a deferred restore in `runInteractive` and recovered client goroutines (phase 2) leave the terminal usable.
- Terminal restoration on normal exit, error and panic; stdin drain up to 1 s on exit (T-TRM-09); early input buffered before the loop (T-TRM-19); exit when stdin is lost.
- Capability detection needed by T1a only (color profile, keyboard enhancement reporting for shift+enter) (T-TRM-06 subset).

## Tests first (built `ask`, real leader, PTY)

| Test | Proves |
|---|---|
| `TestInteractiveStartsLeaderAndShowsEditor` | S1 then S3 |
| `TestInteractiveFallsBackInProcess` | Unsafe socket: footer `in-process`, a prompt gets a reply, no leader process is started |
| `TestInteractiveSignalsRestoreTerminal` (SIGINT/TERM/HUP) | Exit codes and restored termios |
| `TestInteractiveQuitDetachesRunContinues` | S16; the leader still finishes the turn |
| `TestInteractiveEarlyInputKept` | Keys typed during startup reach the editor |
| `TestInteractiveErrorExitRestores` | An error exit and a lost input restore termios (built `ask`) |
| `TestInteractiveErrorExitRestoresAfterPanic` | A recovered panic restores termios (same lifecycle function on a real PTY; the binary has no way to panic) |
| `TestInteractiveShiftEnterInsertsNewline` | Key disambiguation is requested and shift+enter reaches the editor |

## Verification

`go test -race ./cmd/tui/ ./internal/tui/`, `go vet ./...`, golangci-lint, `go test ./...`, then a manual check in Terminal.app or iTerm2 recorded in phase 8.

## Scout result

- `runInteractive` is called from `runWithDependencies` (`headless.go`) after `sigs` is registered for the three signals. It now takes `deps` and `sigs`, so no second `signal.Notify` exists.
- Bubble Tea ends its input loop quietly on EOF or a failed read, so a closed terminal without a hangup signal leaves the program running with no input. The entry wraps the input file and stops the program on the first failed read (exit 1).
- A `Quit` message never reaches `Update` (the event loop returns first). A stop from outside therefore uses an own message, `tui.StopMsg`.
- `Options.Open` receives `context.Background()` from the view; the entry ignores it and uses its own context, cancelled when the program ends, so a quit during "Starting leader…" does not wait for the leader.
- A leader closes the member and keeps the run when a client connection closes (`router.removeClient`, `driverLost`), so the quit needs no explicit `Detach`; closing the client is enough. The in-process client stops its run on close.
- On macOS the system revokes a terminal when its session leader exits, so the termios cannot be read afterwards. The tests start `ask` without a controlling terminal and send signals to the process.
- The entry puts the terminal in raw mode before anything else: keys typed during start wait unechoed in the input queue and reach the editor. The terminal answers to startup queries can arrive after Bubble Tea stops reading, so the entry drops late input (50 ms quiet, 1 s limit) before it restores the termios.
- Capability detection: the color profile comes from `colorprofile.Detect` for the first frame, and Bubble Tea sends `ColorProfileMsg` after it. Bubble Tea requests key disambiguation by default (`ESC [ > 1 u`), which is what makes shift+enter reach the editor; the entry adds nothing.
- The version in the startup header is `buildIdentity()` (the VCS revision), because the repository has no semantic version constant.

## Decisions to review

- The confirmation and detach texts depend on the state. The design texts are used word for word when a run is active (`Press ctrl+c again to quit · the run continues in the leader`, `Detached from session X. The run continues in leader Y.`). With no active run the clause "the run continues" is left out, because it would be false. This is a visible variant that the design does not show.
- A start error or a fatal client error also clears the live region before the error text is printed.

## Done

- [x] Tests first (failing before the code): quit flow tests in `internal/tui/session_test.go`, the interactive tests in `cmd/tui/interactive_test.go` (the placeholder test `TestInteractiveWithoutTUIFailsClearly` is removed).
- [x] `runInteractive` / `runTerminal` in `cmd/tui/interactive.go`: leader client with startup fallback through `openTUIClient`, model, `tea.WithoutSignalHandler()`, own signal handling (130, 143, 129), deferred restore, stdin drain, early input, lost input, bounded client close.
- [x] Quit flow in `internal/tui`: ctrl+c confirm within 2 s, ctrl+d, `/quit`, in-process wording, exit note, empty final frame, `StopMsg`.
- [x] Verification: `go test -race ./cmd/tui/ ./internal/tui/`, `go vet ./...`, golangci-lint, `go test ./...` (results in the phase report).
