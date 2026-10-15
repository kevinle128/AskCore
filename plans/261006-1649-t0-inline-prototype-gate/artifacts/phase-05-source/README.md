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

# Phase 2 automated G6 fixture

The current program has a multiline draft and a grapheme cursor.
The full draft is retained; only three editor rows are shown, with help when space permits.
Widths 1, 2, and 10 and heights 1 and 2 are checked.
CJK and emoji use cell widths, and cursor movement and backspace use grapheme boundaries.
Control text is displayed safely while its original bytes remain in the draft.
Paste replaces CRLF with LF once and preserves all other draft bytes.
Enter submits without inserting a newline.
Alt+Enter inserts a newline when negotiation is absent.
Shift+Enter and Ctrl+Enter insert newlines after a real terminal capability response.

```sh
cd /private/tmp/askcore-t0-09kf6w9r
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
export GOMODCACHE=/private/tmp/askcore-t0-mod-cache
go run . --scenario g6 --keyboard auto
go run . --scenario g6 --keyboard off
go test -v -run 'TestEditor|TestInput|TestG6|TestRestore' -count=1 -timeout 90s ./...
go test -race -v -count=1 -timeout 90s ./...
go vet ./...
go build .
```

The current output owner is a wrapper that embeds the terminal file and preserves the complete `term.File` interface and FD.
It serializes renderer writes and fixture cleanup.
Control and status pipes stay separate from terminal input.
The inherited status pipe is nonblocking so Go can bound each status write to one second.
All draft mutation stays in Update; View performs no I/O.
Status checkpoints include the full SHA-256 hash, bytes, lines, semantic paste count, submit count, and negotiated capability.
The PTY tests send actual encoded keys and a 2,000-line bracketed paste in 37-byte transport fragments.
The payload includes tabs, CJK, emoji, CRLF, and CSI-u-like text.
Each supported and absent-negotiation case verifies exactly one PasteMsg and full draft integrity.
Visible cursor checks include the initial multiline draft and the final pasted row.

The terminal responder detects actual emitted Kitty and synchronized-output queries.
It supplies Kitty flags and a supported-but-disabled mode 2026 response over the PTY master.
The vt emulator does not implement Kitty negotiation; this responder is an explicit test terminal capability implementation.
Other emulator query responses remain intact.
The fixture checks keyboard pushes and pops, cursor visibility, bracketed paste, mouse modes, and synchronized output after cleanup.

SIGINT, SIGTERM, and SIGHUP use the fixture signal context and Bubble Tea cleanup.
Normal quit, context cancellation, actual partial output failure, model panic, and command panic have separate child cases.
Each restoration test takes the termios snapshot after tea.Run returns and before the child process exits.
The final terminal mode snapshot follows process exit and output drain.
Each child gets a separate stderr artifact and is closed and waited for.
An incomplete bracketed input payload exercises exit while input is pending.

The partial-output test writes the real eight-byte CSI sequence that enables synchronized output, then returns a write error before the frame is complete.
The stock cleanup left mode 2026 enabled: `evidence/phase2-partial-red.txt` records this reproduced failure.
After the renderer stops, fixture cleanup resets mode 2026 through the same ordered output owner.
`evidence/phase2-partial-green.txt` checks the written prefix, error cause, reset bytes, and absence of failed frame text replay.
This is a G6 cleanup check; G1 ordered transcript/frontier acceptance remains Phase 3 work.
Permanent output failure restores termios but reports terminal-byte restoration as unavailable.
The permanent test expects synchronized output to remain enabled and does not record a restoration PASS for terminal bytes.

`evidence/phase2-input-red.txt` is the initial behavioral RED for absent paste handling and Alt+Enter newline.
`phase2-input-green.txt` records the repaired behavior.
`phase2-pty-initial.txt` had a diagnostic artifact path error and is infrastructure evidence only.
`phase2-cursor-red.txt` had an incorrect expected column of 20; the hand-counted column is 19 (four digits, four tab cells, four CJK/emoji cells, and seven visible control-text cells).
That expectation error is not an application or renderer defect.
The bounded status change initially exposed an inherited pipe without deadline support; making that pipe nonblocking corrected the fixture.
Final suite, race, vet, and build logs use the `evidence/phase2-` prefix.

Real iTerm2 3.7.3 observations remain pending because computer-use app access was denied.
Run both manual commands in iTerm2 and record negotiated capabilities, Alt+Enter fallback, paste integrity, resize, each exit path, and shell input after exit.
No automated result is substituted for this evidence.
Optional pixel, palette, keepalive, and modifyOtherKeys probes are unverified and remain nonblocking Phase 5 evidence work.
No G1, G2, G3, D14, or complete G6 acceptance is claimed.

## Ordered transcript prototype

The G1 scenario uses stock Bubble Tea Println insertion.
No renderer patch, fork, or custom committer is used.
The output owner preserves the terminal file descriptor and distinguishes insertion WriteString calls from renderer frame Write calls.
Only a complete observed insertion can advance the confirmed frontier.
One insertion is outstanding at a time; completed blocks wait for a contiguous ready prefix.
A write error or short write stops the frontier and rejects later content without replay.
After the renderer stops, the same owner can emit terminal restoration bytes.
It removes close-frame cursor moves and erase bytes after failure to preserve history.
Permanent output failure makes terminal-byte restoration unavailable; termios restoration is checked separately.

Run this command in iTerm2 for the manual transcript check:

```sh
cd /private/tmp/askcore-t0-09kf6w9r
GOWORK=off GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache GOMODCACHE=/private/tmp/askcore-t0-mod-cache go run . --scenario g1 --lines 2000
```

The command starts the transcript without test control descriptors.
Type in the editor, change the terminal size, inspect scrollback order from G1[0000] through G1[1999], then use Ctrl+C and check shell input.
The lines flag accepts 1 through 9999 lines.
These manual observations remain pending.

The stock oversized, ordered 2000-line, and live-region shrink cases pass.
The initial status timeout was an observer matching issue: insertion adds erase-line sequences and CRLF between source lines.
It is not a renderer RED.
`phase3-stock-initial.txt` records the stock placement PASS after the observer correction.
The owner RED logs prove the WriteString bypass, missed underlying error latch, frame misclassification, and unsafe cleanup classification.
The manual startup pre-check used an invalid test timeout flag and is infrastructure evidence only; no initial behavioral RED is claimed for that small command addition.
`phase3-final-focused-valid.txt` records its valid check and the transcript-owner synchronized-output failure check.
The pending resize test changes the real PTY size while the insertion writer is held before its write, sends SIGWINCH, then checks model size, all 2000 ordered lines, and the exact editor cursor.
The fragmentation cases feed actual child output to the oracle in one-byte and random chunks.
All termios observations occur after tea.Run returns while the child is alive; final ANSI mode observations follow process exit and drain.
Full G1, G6, D14, and T0 acceptance still require the specified real-terminal observations.

## Selector and resize prototype

Run these commands in iTerm2:

```sh
cd /private/tmp/askcore-t0-09kf6w9r
export GOWORK=off GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache GOMODCACHE=/private/tmp/askcore-t0-mod-cache
go run . --scenario g2
go run . --scenario g3 --lines 2000
```

Tab opens the 12-item selector in the editor slot.
Up and Down move the selected item through its bounded three-row window.
Escape, Enter, or Tab closes it and restores the draft and its cursor.
Typing and paste while the selector is active do not alter the draft.
Ctrl+C always exits.
G3 starts the transcript with an editable CJK/emoji draft for manual resize checks.
Wide glyphs that cannot fit a one-column terminal display as question marks; draft bytes remain unchanged.

The layout measures the active slot at the final width, then reconciles its viewport if the measured rows exceed terminal height.
Optional tail and help rows use only the remaining height.
This local implementation follows the width-then-height dependency rule inspected in Crush commit 65865e01950368379ad1f9746b620a77fb7a5da8, internal/ui/model/ui.go:4182–4211.
No source body was copied; no new source license or dependency was introduced.
Crush's source license remains FSL-1.1-MIT under the accepted internal-use boundary if later copying is needed.

`phase4-model-red.txt` proves the missing selector route and overflowing two-row live layout.
`phase4-pty-red.txt` proves the missing selector from actual Tab bytes.
The first GREEN command had a local declaration compile error; `phase4-initial-green-valid.txt` records the corrected check.
`phase4-pty-final-focused.txt` checks the exact selector item window and cursor, draft restoration, preserved committed lines at stable height, actual resize, and no content replay.
The Unicode checks use hand-written cell expectations at widths 13 and 12 and test widths 2 and 1 with height 1.
Narrow/wide resize during G1 streaming retains all 2000 ordered lines and emits each committed token exactly once.
No renderer fix class, fork, global screen/history erase, or custom committer is used.

`phase4-emulator-resize-limit.txt` preserves the combined height-shrink failure.
Pinned x/vt Screen.Resize at screen.go:74–82 directly calls its render buffer Resize and does not save truncated screen rows to scrollback.
Thus shrinking a screen with visible committed rows can discard those rows in this emulator independently of application replay.
The height-shrink check proves valid live geometry and no repeated tokens or global erase bytes; it does not claim native history reflow preservation.
Stable-height selector close/history checks remain strict.
Real iTerm2 Unicode placement, history reflow, selector, and resize observations remain pending.
Full G2/G3, T0, and D14 acceptance are incomplete.

## Evidence package and manual checks

The evidence command validates completed automated test evidence and writes `evidence/manifest.json`.
Run it after the test suite, since terminal captures can change during tests:

```sh
export GOWORK=off GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache GOMODCACHE=/private/tmp/askcore-t0-mod-cache
go test -v -count=1 -timeout 60s ./...
go test -race -v -count=1 -timeout 60s ./...
go vet ./...
go build -o /private/tmp/askcore-t0-final .
go run . --scenario all --evidence evidence
```

The all scenario packages evidence; it does not run the suite or perform hardware checks.
Its blocking result records refer to the completed Phase 4 worker race log and current captured terminal bytes.
D14 remains false while real-terminal evidence is pending.
G4 image history, G5 alternate reconstruction, and the optional pixel/palette/keepalive/modifyOtherKeys probes have explicit not-run reasons.
They are not successful placeholders or blocking gate results.
Command tests use temporary evidence copies and do not overwrite the final manifest during a suite.
After the suite, generate the final manifest and run the validation checks once more without terminal cases.

The source and module files can be copied into a new directory with the evidence folder, excluding compiled binaries.
Use the same commands there with `GOWORK=off` and the pinned module cache.
The boundary check rejects root imports, replacements, and a scratch go.work file.
The source checksum manifest identifies the scratch code separately from the product baseline commit.

### iTerm2 checklist

Record iTerm2 version, macOS version, locale, and terminal columns/rows using `sw_vers`, `locale`, and `stty size`.
Keep the outputs with the observations; do not include personal terminal history.
Use the scratch or archived source directory for the commands below.

1. Run `go run . --scenario smoke`, type in the editor, and press Ctrl+C.
   Record startup text still in history, visible cursor, normal shell input, and the terminal size.
2. Run `go run . --scenario g1 --lines 2000`.
   Inspect native history from G1[0000] through G1[1999], the live tail, and usable editor input.
   Change width and height, including repeated shrink/grow, and record native reflow separately from repeated application tokens.
3. Run `go run . --scenario g2`.
   Type a draft, move Left, press Tab, move Down to item 12, and resize while it is open.
   Record the three-row window, retained selection, and exact draft/cursor after Escape.
4. Run `go run . --scenario g3 --lines 2000`.
   Record physical CJK/emoji cells and cursor at width 13, width 12, width 2, width 1, and height 1.
   Verify one editor after repeated narrow/wide and height-only changes.
5. Run `go run . --scenario g6 --keyboard auto --draft-report evidence/manual-keys.json`.
   Type a, Shift+Enter, b, Ctrl+Enter, c, then Ctrl+C.
   Inspect Negotiated and Text in the report.
   If negotiation is supported, expect `a\nb\nc`; if it is not negotiated, record that fact without a modified-key PASS.
6. Run `go run . --scenario g6 --keyboard off --draft-report evidence/manual-fallback.json`.
   Type a, Alt+Enter, b, ordinary Enter, then Ctrl+C.
   Expect Text `a\nb`, Submits 1, and Negotiated false.
7. Put the fixed payload on the clipboard with `pbcopy < evidence/manual-paste.txt`.
   Run `go run . --scenario g6 --keyboard auto --draft-report evidence/manual-draft.json`.
   Paste once, then use Ctrl+C without adding text or submitting.
   Verify it with the command below.
8. For each exit route, record shell input, cursor visibility, and terminal mode restoration separately.
   Automated signal/panic/error proof does not substitute for manual observations.
   Error injection uses the controlled PTY fixture; no hardware error result is claimed.

```sh
python3 - <<'PY'
import hashlib, json
from pathlib import Path
expected = Path('evidence/manual-paste.txt').read_bytes().replace(b'\r\n', b'\n')
report = json.loads(Path('evidence/manual-draft.json').read_text())
assert report['Text'].encode() == expected
assert report['SHA256'] == hashlib.sha256(expected).hexdigest()
assert report['Lines'] == 2000 and report['Pastes'] == 1 and report['Submits'] == 0
print('Manual paste integrity confirmed')
PY
```

The draft-report flag is optional, writes only after program cleanup, and reports its file error as a nonzero exit.
Use only the fixed test draft for archived evidence.
`phase5-manifest-red.txt` records the genuine missing command/artifact assertions before implementation.
`phase5-command-red.txt` records the absent packaging command.
`phase5-draft-report-red.txt` records the missing report flag, not a renderer defect.
All iTerm2 observations above are still pending because app access was denied.
The automated result supports stock Println with the ordered output owner, complete-write failure latch, and explicit owned cleanup adaptations.
It does not approve a product dependency migration or make D14 ready.
