# T0 Phase 1 scratch fixture

This module is isolated from AskCore.
It does not import or change root product code.
Use Go 1.27.0 on macOS arm64.

```sh
cd /private/tmp/askcore-t0-09kf6w9r
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
export GOMODCACHE=/private/tmp/askcore-t0-mod-cache
go test -v -count=1 -timeout 60s ./...
go test -race -v -count=1 -timeout 60s ./...
go vet ./...
go run . --scenario smoke
```

Type text in the inline editor and use Ctrl+C to quit the manual smoke run.
The startup block uses stock `tea.Println`.
The program uses unwrapped stdin/stdout terminal files.
The parent captures the real PTY master output.
There is one renderer output owner.
Test control FD3 and status FD4 are separate from terminal input/output.
Child diagnostics go to a separate temporary file and evidence artifact, with a 64 KiB copy limit.
Control messages quit Bubble Tea and then release the child after restoration is measured.
Real ioctl and SIGWINCH report the resized model dimensions on the status pipe.
No test injects WindowSizeMsg.

The parent compares retained-slave termios before raw mode, during raw mode, and after `tea.Run` returns.
The child stays alive for this last snapshot.
On macOS, session-leader process exit revokes the slave and a later ioctl returns ENOTTY.
The final emulator snapshot follows process exit and master output drain.
Its 15 rows, one empty history row, cursor (0,1), and main buffer are checked against a hand-written expectation.
Cancellation uses SIGTERM, with a one-second kill fallback, and closes and reaps the owned child.
This check does not claim a signal-exit restoration gate.

The oracle fixtures have hand-written rows, history, cursor, erase, wrap, and main/alternate buffer results.
They reject wrong cursor and history expectations.
They test ASCII control fragmentation and split UTF-8 for `é界`.
Combining characters and ZWJ grapheme fragmentation are outside this Phase 1 proof.
CPR responses are tested and the PTY driver pumps emulator query responses back through the master.
Mode callbacks check cursor visibility, paste, mouse, and synchronized output after normal quit.
Kitty keyboard negotiation is not proved by this emulator.
No G1, G2, G3, G6 acceptance is claimed.

`evidence/oracle-initial.txt` records the initial stock oracle PASS.
The first smoke behavioral failure was missing the live editor, but that run also had a cleanup fault.
`evidence/smoke-red.txt` is an explicit replay against the saved stock baseline after fixture cleanup was corrected.
It is not presented as the original clean RED run.
`evidence/model-baseline.go.txt` and `evidence/model-green.go.txt` retain the compared model sources.
`evidence/tests-green.txt`, `race-green.txt`, `vet.txt`, `build.txt`, and `module-graph.txt` record final checks.
`evidence/smoke-output.ansi` retains captured output.

Pins are Bubble Tea v2.0.10, creack/pty v1.1.24, and x/vt commit ad85c59fdf4e (`v0.0.0-20261004011457-ad85c59fdf4e`).
All three use MIT licenses; notices are in `evidence/licenses/`.
No upstream implementation source was copied or adapted.
The exact graph is in go.mod, go.sum, and evidence/module-graph.txt.
Source APIs were read from the pinned module cache, including Bubble Tea tea.go/key.go, PTY start.go, and vt emulator.go/screen.go/scrollback.go/callbacks.go.

A real iTerm2 smoke test is pending.
The controller found iTerm2 version 3.7.3 from its local plist, but app access was denied.
An emulator result does not replace this manual check.
Record iTerm2 version, window size, locale, visible startup/editor/cursor, resize, quit, and shell input after the manual run.
