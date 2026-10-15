# Independent TUI E2E verification

Status: DONE_WITH_CONCERNS.
The runner detects real resize rendering failures on both headless engines.
The default suite exits 1 with six passing cases and two failing G3 cases.
This is not an all-green product result.

## Commands and results

| Command | Exit | Result |
|---|---:|---|
| `python3 e2e/tui/test-assertions.py` | 0 | Four negative-control tests pass |
| Python AST syntax checks for both scripts | 0 | Pass |
| `tui-test --version` | 0 | 0.1.0-beta.2 |
| `GOMODCACHE=/private/tmp/askcore-t0-mod-cache python3 e2e/tui/run.py --artifacts /private/tmp/askcore-independent-tui-e2e-261007` | 1 | Eight startup infrastructure failures: sandbox denies session lock access |
| Same documented runner with narrowly escalated session access and artifacts path ending `-escalated` | 1 | Six cases pass; two G3 cases fail |

Frozen hashes for run.py, README.md, and test-assertions.py match the controller's supplied values.
The run's isolated target checks pass all 39 Go tests, with six explicit optional-capability subtest skips.
The Python controls reject missing, duplicate, and reordered transcript lines, wrong cursor positions, and replayed output.
No source or assertion was changed to convert a failure to PASS.

## Headless gate results

| Engine | G1 | G2 | G3 | G6 |
|---|---|---|---|---|
| Alacritty | PASS | PASS | FAIL | PASS |
| Ghostty | PASS | PASS | FAIL | PASS |

G1 asserts 2,000 ordered transcript markers, exact editor cursor, and recorded output without replay or global erase.
G2 asserts the exact three-item window, retained selection, ignored selector input, and draft/cursor restoration.
The standalone G2 case has no committed history; the retained Go fixture checks its history contract.
G6 asserts Alt+Enter versus submission, negotiated modified keys, and the full normalized 2,000-line paste report.
Modified-key negotiation passes on both test engines.

Alacritty G3 fails at 12 columns and 6 rows with stale editor rows: an old row precedes `editor> abc` and `界😀Z`.
Ghostty G3 fails at 13 columns and 6 rows because the expected wide-glyph row starts with a duplicate editor row instead.
The recorded states and cells directly support these errors.
They agree with the pinned renderer/reflow incompatibility described in the README.
This independent run establishes the visible defect; it does not by itself isolate every upstream cause.
The extreme-size probe was not rerun and is not assigned a PASS.

## Artifacts, cleanup, and boundaries

Empirical results and failure artifacts are in `/private/tmp/askcore-independent-tui-e2e-261007-escalated`.
Each G3 failure has commands.json, ownership.json, failure-state.json, failure-history.json, failure-cells.json, failure.svg, and output.cast.
The sandbox startup failure evidence remains in the separate non-escalated artifact directory.

All eight owned sessions end with daemon status exit 3 and running false.
No case records a cleanup_error.
A scoped process inspection finds none of the 16 recorded daemon and child PIDs alive.
No global daemon-stop command was used.
A separate unique startup-failure control exercised constructor cleanup after a target launch error; its final status also reports running false and it has no startup-cleanup.error.
That control failed before editor startup and is cleanup evidence, not a gate result.

The runner's before/after hashes match for the immutable archive and all 426 protected product files.
The original 27-product T0 baseline hashes also match.
No production source, root dependency, earlier archive, or root configuration was changed.

## Remaining acceptance

These are headless Alacritty and Ghostty results, not Terminal.app or iTerm2 certification.
D14 readiness remains false.
G3 is a reproduced product rendering failure and must remain failed until a cause-aligned change is reviewed and retested.
Unresolved questions: none for this independent verification.
