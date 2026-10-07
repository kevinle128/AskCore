# Partial inline resize candidate

Status: not accepted and not selected as the default E2E target.
The original eight native E2E cases pass on both engines.
The stronger mid-cursor, hard-line, long-wrap, and trailing-space shrink/grow cases still fail on Alacritty.
Ghostty passes those four cases.
A held real write during resize preserves all 2,000 history markers on Alacritty but retains only 1,098 on Ghostty, despite 2,000 unique actual output markers.
That additional failure also prevents acceptance.
Do not use these partial results to claim real iTerm2 compatibility or D14 readiness.

## Source and capability boundary

The upstream pins remain Bubble Tea v2.0.10 and Ultraviolet v0.0.0-20260703014108-f5a850f9c2b7.
The root candidate go.mod retains the original graph without local replace directives.
A generated dependency tree at those pins contains the four source corrections from patches/.
Original upstream license files are retained with both dependency copies.
The separate bubbletea and ultraviolet directories retain unit regression tests and local module wiring for those tests.
No dependency-cache source or old archive was modified.

T0 defaults to `--terminal-reflow native` for the measured native terminal engines.
Use `--terminal-reflow none` for a terminal that resizes without cell reflow.
The x/vt fixture explicitly sets `T0_TERMINAL_REFLOW=none`, independently of its control/status descriptors.
The local library option defaults to disabled for upstream compatibility.
This is a declared terminal behavior, not a TERM-name heuristic.

The relative cursor correction maps old rendered cells up to the old cursor.
It keeps wide glyphs intact and preserves a resize-burst baseline until the owner successfully writes a frame.
The renderer also waits for a size-specific model view before tick redraw and drains a pending redraw before native insertion.
These changes do not recover every old live row that Alacritty has already placed in native history during shrink.
That remaining ownership problem prevents acceptance.

## Checks and evidence

Use Go 1.27 with GOTOOLCHAIN=local, GOWORK=off, a writable GOCACHE, and the task GOMODCACHE.
Run the candidate Go checks from this directory with `go test -v -count=1 -timeout=60s ./...`.
Run the added dependency regressions from bubbletea with `go test -run TestResizeWaitsForSizedView -count=1 -timeout=60s` and from ultraviolet with `go test -run TestInlineResizeCursorReflow -count=1 -timeout=60s`.
Build with `go build -o candidate .`.
From the repository root, run `python3 e2e/tui/run.py --target /absolute/path/to/candidate --artifacts /absolute/path/to/evidence`.
Run native-regressions.py from the repository root for the stronger matrix.
The driver uses unique owned sessions and closes them after each case.

The original source had behavioral RED for native cursor mapping.
The first cursor correction had behavioral RED for an unpainted resize burst.
The old renderer had behavioral RED for flushing an old-sized view after resize.
Their logs and later unit results are in resize-evidence.
Compiler, path, and initial network failures are infrastructure records, not behavioral RED.
The preliminary unconditional binary is hypothesis evidence only.
The final native matrix remains failed even where the original eight checks pass.
Go 1.27 passes all 39 candidate tests, all 39 under race, vet, build, and both added dependency regression tests under race.

## Measured fix ledger

One renderer fix class is in use: inline origin reconciliation and full redraw.
The saved-cursor hypothesis was rejected after a real two-engine probe.
Cell mapping corrects the original wide-glyph case but is incomplete for reclaimed live history on Alacritty.
The stale-sized-view flush guard has a source-backed regression and corrects that separate race within the same redraw class.
No custom committer, global screen erase, transcript replay, terminal-name branch, or product migration was added.
The original five-class fallback limit remains in force.
