# Spike: alt-screen enter and return in the inline TUI (T1b step 0)

Date: 2026-10-12. Scope: plan `261012-0226-t1b-tool-output`, step 0, design checks a to d.

## Verdict

**PARTIAL. Proceed with step 3, but with one small fork change (or an equal model-side wait) that the design does not list.**

The alt screen can be entered and left at run time through `View.AltScreen`, and every exit path restores the terminal. Two things in the design are wrong for the fork as it is today:

1. Any write after the leave that arrives before the renderer draws the next frame is lost or drawn on the wrong screen. This hits the held commit and the resize rebuild, which are exactly the writes that step 3 makes right after the return.
2. `tea.Println` does not print nothing in the alt screen. It writes into the alt screen and overwrites a row of the view.

With a 14-line change in `third_party/bubbletea/cursed_renderer.go` (below), every assertion passes on all four engines. With only a model-side wait of 100 ms after the leave, every assertion passes except one; the failing one is item 2, which the model avoids by holding commits.

No re-plan is needed. Step 3 gains a prerequisite (the fork change) and one rule for the model.

## Engines that really ran

| Engine | How | Result in this report |
|---|---|---|
| alacritty | tui-test 0.1.0-beta.2 `--backend alacritty` | run |
| ghostty | tui-test `--backend ghostty` | run |
| rio | tui-test `--backend rio` | run |
| tmux 3.6a | private server (`-L`, `-f /dev/null`), real `alternate_on` and `cursor_flag` flags | run |
| Terminal.app, iTerm2, kitty | not scriptable from this environment | **not run** |

Terminal.app, iTerm2 and kitty remain open. The design asked for them (open question 4). Run the spike by hand there before the user accepts the slice (command below).

## Setup

- Program: `e2e/tui/altscreen_spike/main.go`. It is an inline program on the fork, with the same signal rule as `cmd/tui` (`WithoutSignalHandler`, SIGTERM and SIGHUP send a stop message). Keys: `o` enter alt, `Esc` leave, `c` commit one row (held while in alt, written on return, or replayed in one write after a resize), `p` `tea.Println` at once, `ctrl+c` quit. It prints 8 start rows with `tea.Println`.
- Driver: `e2e/tui/altscreen_spike/run_spike.py`. It reuses `AskSession` of `run.py` (owned sessions, ownership checks, close and daemon stop). It starts the program through a small `sh` wrapper that echoes `EXIT=<status>` after the program ends, so the final screen shows the real state of the terminal after exit. `compile_spike.py` makes the binary.
- Sizes: 80x24, resize to 100x30 (wider) and to 50x20 (narrower).
- Process hygiene: every session and tmux server was closed; no spike process was left (checked with `ps`).

## Results

Rows are the assertion; columns are engines. "tree" is the fork as it is in the repository. "fork patch" is the change below. "wait 100" is the tree plus a 100 ms wait in the program after the leave, before the write of held rows or the replay.

| Check | Assertion | tree: alacritty / ghostty / rio / tmux | wait 100 ms (4 engines) | fork patch (4 engines) |
|---|---|---|---|---|
| a | Start rows printed before the enter are intact and once after the leave | PASS / PASS / PASS / PASS | PASS | PASS |
| a | Inline view drawn once, last, after the leave | PASS x4 | PASS | PASS |
| c | `tea.Println` during alt prints nothing | **FAIL x4** | **FAIL x4** | PASS |
| c | A row printed in alt is not in scrollback after return | PASS x4 | PASS | PASS |
| c | Held commit appears exactly once after the return | **FAIL x4** (row never appears) | PASS | PASS |
| b | Resize in alt, leave: rows exactly `SCROLL-01..08, COMMIT-01, COMMIT-02` (80 to 100) | **FAIL x4** (held row missing; in tmux the scrollback also shrinks to `SCROLL-08, COMMIT-01`) | PASS | PASS |
| b | Inline view at the new width, once (80 to 100) | PASS alacritty, rio / **FAIL** ghostty, tmux (the old 80-wide frame row stays) | PASS | PASS |
| b | Same two checks, 80 to 50 | rows check **FAIL x4**; view-once check **FAIL** alacritty, ghostty, tmux, PASS rio | PASS | PASS |
| d | `ctrl+c` in alt: exit 0, main screen, last alt switch is `?1049l`, last cursor op is `?25h` | PASS x4 | PASS | PASS |
| d | SIGTERM in alt: same | PASS x4 | PASS | PASS |
| d | SIGTERM while the last view still has `AltScreen` true (renderer `close` path): same | PASS x4 | PASS | PASS |
| d | tmux only: `alternate_on=0`, `cursor_flag=1` after exit (all three exit variants) | PASS | PASS | PASS |

Totals (assertions passed, from `results.json`): tree 20 of 25 alacritty, 19 of 25 ghostty, 21 of 25 rio, 25 of 31 tmux (tmux has 6 more assertions: the emulator flags). Fork patch: 25 of 25 on alacritty, ghostty and rio, and all tmux assertions. Wait 100 ms: every assertion except the `Println in alt` one, on all four engines.

Limits of the evidence: the tui-test engines do not expose the alt-screen flag or cursor visibility, so check d on them reads the raw output stream (last `?1049` and `?25` sequence) plus the final screen text, not the emulator flags. Only tmux gave the emulator flags. SIGINT as a signal and SIGHUP were not sent (`tui-test signal` offers INT, TERM, KILL, QUIT; raw mode makes `ctrl+c` a key, and SIGHUP uses the same stop path as SIGTERM in the program).

## Causes (verified)

1. **Commit after the leave is lost.** `Update` sets `AltScreen=false`, the cmd runs at once, and `insertAbove` writes. The renderer draws frames on its own ticker, so the terminal is still on the alt screen: the row is written into the alt screen and dropped when the frame switches back. Evidence: tmux capture of the alt screen and the main screen; `third_party/bubbletea/cursed_renderer.go` `insertAbove` has no alt-screen check at all.
2. **Replay after the leave draws on the alt screen.** `replayInline` checks `s.view.AltScreen` (the wanted view, already false) and then writes `ESC[2J ESC[3J` plus all rows, and only after those the `?1049l` from its own flush. Raw stream: `...\x1b[2J\x1b[3J\x1b[H<rows>\r\n...\x1b[?1049l...`. The rows land on the alt screen and vanish; the main screen keeps its old, reflowed rows plus the stale 80-wide frame row.
3. **`Println` in alt is not dropped.** The doc comment of `Println` says so, but the code does not check. A row printed in alt overwrote row 1 of the view (`PRINT-02` replaced `ALT-VIEW ...` in the tmux capture). The renderer diff does not know that row changed, so it stays wrong until the row content changes.

## Fork change that fixes all three

Tested in a temporary copy of the fork (`-modfile` with a `replace` to the copy); the repository tree is not changed.

```diff
@@ func (s *cursedRenderer) insertAbove(str string) error {
 	s.mu.Lock()
 	defer s.mu.Unlock()
+
+	// Nothing is written while the view asks for the alternate screen.
+	if s.view.AltScreen {
+		return nil
+	}
+	// The terminal is still on the alternate screen when the view already
+	// asks for the main screen. Draw that frame first, so the screen switch
+	// happens before this write.
+	if s.lastView != nil && s.lastView.AltScreen && !s.view.AltScreen {
+		_ = s.flushLocked(false)
+	}
@@ func (s *cursedRenderer) replayInline(entries []string) error {
 	if s.view.AltScreen {
 		return fmt.Errorf("inline replay is forbidden in alternate screen")
 	}
+	// (same second block as above)
+	if s.lastView != nil && s.lastView.AltScreen && !s.view.AltScreen {
+		_ = s.flushLocked(false)
+	}
```

If the owner prefers no fork change, the model-side alternative also passed: after the leave, wait one drawn frame (the spike used `tea.Tick(100ms)`) before the held commits or the rebuild run. It needs a real frame acknowledgement to be exact; a fixed tick is a guess.

## Rules that step 3 must add

- Hold every commit while the view is open (as the design says) and write them after the frame that left the alt screen.
- Do not enter the alt screen while a commit or a replay is running (`committing` or `replaying` true). With the fork change a `Println` that arrives after the view asked for alt is dropped, so an in-flight commit would be lost. Delay the open until the output path is idle, or hold the key press.
- Add a renderer test in `third_party/bubbletea/replay_test.go` for both writes right after a leave, and update `PATCH.txt` (the fork has five changes then).
- Design text, section 3 and 5: the line "`tea.Println` prints nothing while the alternate screen is active" is true only after the change above.

## Minor observation

When the program stops while its last view still has `AltScreen` true, `close` leaves the alt screen and the terminal returns to the saved cursor position inside the old inline frame; the shell output then starts on the frame row (`INLINE-DRAFT draft textEXIT=0`). The real model returns an empty non-alt view on stop (`View` when `quitting`), which gave a clean result in the `ctrl+c` and SIGTERM variants, so this is only a reminder to keep that behavior.

## Rerun

```sh
python3 e2e/tui/altscreen_spike/run_spike.py                      # tree: alacritty, ghostty, rio, tmux
python3 e2e/tui/altscreen_spike/run_spike.py --engine tmux --scenario resize-wider
python3 e2e/tui/altscreen_spike/run_spike.py --leave-delay-ms 100 # model-side wait variant
python3 e2e/tui/altscreen_spike/run_spike.py --binary /path/to/spike-built-with-patched-fork
```

By hand on Terminal.app, iTerm2 or kitty: compile `./e2e/tui/altscreen_spike` into a temporary directory, run it, press `c`, `o`, resize the window, press `c` then `Esc`, and compare the scrollback with the expected rows `SCROLL-01..08, COMMIT-01, COMMIT-02` once each. The run takes under a minute per engine.

## Unresolved questions

- Terminal.app, iTerm2 and kitty are not run. Who runs the hand check, and before or after step 3?
- Fork change or model-side wait? The change is the exact option; the wait is cheaper to roll back. Owner decision (this touches the vendored fork and its `PATCH.txt`).
