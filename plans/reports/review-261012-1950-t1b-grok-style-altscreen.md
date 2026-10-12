# T1b alternate-screen plan review

## Result

The implementation meets the original purpose: a compact full-screen session with blocks that open on demand, scroll and copy controls, and terminal restoration on exit.
The 13 plan steps and the 38 named acceptance checks are implemented.
The review found two resize defects and one missing help hint.
These are now corrected.

Authority: [plan](../261012-1337-t1b-grok-style-altscreen/plan.md), [approved design](../261012-1337-t1b-grok-style-altscreen/design.md), and [criterion-to-test map](tester-261012-1846-t1b-altscreen-acceptance-map.md).
The map is a test index; this review used source and fresh test runs to check its claims.

## Corrections

| Finding | Evidence before repair | Repair and check |
|---|---|---|
| A selected block could disappear after a smaller resize | In a real Bubble Tea program with an `x/vt` terminal, select step 7 at 80×24, then resize to 40×14; the final frame had no selected step | Reveal the selected unit after the pane gets its new size; `TestResizeTo40x14KeepsTheSelectedBlockOnTheTerminal` failed before repair and passes after repair |
| Resize could turn follow on without a user scroll | Scroll up at 80×24, then grow to 120×40 so the end fits; the marker disappeared; revealing a selected reply during a smaller resize had the same effect | Keep free scroll through resize and selection repair; `TestResizeTo120x40KeepsFreeScrollWhenTheEndFits` and `TestResizeKeepsFreeScrollWhenTheSelectedReplyWasBelowTheWindow` failed before repair and pass after repair |
| The native-selection bypass was absent from the shortcut bar | Design question 6 requires a bar hint as well as README guidance; only the README and architecture had it | Add `shift/option+drag select` when the complete hint fits; update and review the 120-column screen goldens |

The pane still follows after an explicit scroll to the end, `End`, `G`, `ctrl+End`, or a marker click.
A content shrink keeps its existing end rule.
A width or fold change alone is not evidence of a content shrink.
The review also corrected the architecture document's path to the renderer patch test.

An independent read-only review checked both resize repairs and found no further concrete defect.

## Verification

| Check | Result |
|---|---|
| `go test ./...` | Pass; `cmd/tui` 290.4 s, `internal/tui` 102.5 s |
| Focused resize, pane, marker and bottom-control tests | Pass |
| Compiled interactive terminal and exit-line tests in `cmd/tui` | Pass |
| `go -C third_party/bubbletea test ./...` | Pass |
| `go vet ./...` | Pass |
| golangci-lint on `./...` | Pass, 0 issues |
| `python3 e2e/tui/run.py` after the final code changes | Pass, 24/24 on Alacritty and Ghostty |
| `python3 e2e/tui/test-assertions.py` | Pass, 23 oracle checks |
| 2000-block frame and mouse budget tests | Pass; wheel with marker median 62 µs; resize with pending marker median 5.49 ms, worst 6.28 ms, below 16 ms |

Final engine evidence: [artifacts](../../e2e/tui/artifacts/1656e28850/).
These headless engine results do not certify Terminal.app or iTerm2.
The diff whitespace check reports the existing padded terminal sketch and literal context rows in the renderer patch file.
Those spaces are part of the sketches and patch syntax.

## Scope boundaries

The implementation notes in the plan remain explicit: thinking blocks are per output part, expanded text wraps, the exit line uses the full session id, and the old detach note stays above it.
The marker count can settle over several frames after resize to meet the frame budget.
Hook rendering uses fixtures until H11 supplies runtime events, as the plan requires.
H8 resume and the optional model-supplied `bash` description remain separate follow-up work.
They are not missing requirements in this slice.

## Unresolved questions

None.
