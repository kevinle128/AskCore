# T1b alt-screen acceptance map (design section 10)

All paths are under `internal/tui/` unless marked `cmd/tui/`. "new" = added in `altscreen_scenarios_test.go`.

| # | Test function(s) |
|---|---|
| 1 | `TestStartShowsHeaderAndBar/80x24` (session_test.go), `TestViewUsesTheAlternateScreenAndTheMouse` (model_test.go) |
| 2 | `TestCompactSessionShowsOneRowForEachStepAt80x24` (blocks_fold_test.go) |
| 3 | `TestCompactSessionShowsOneRowForEachStepAt80x24`, `TestFailedToolRowIsExactlyTheWidth` (blocks_fold_test.go) |
| 4 | new `TestRunningToolShowsTheSpinnerAndTheWorkingStatusAt80x24` (real program, real status row); `TestRunningToolRowHasVerbAndSpinnerAt80x24` |
| 5 | `TestStreamingThinkingRowsAt80x24` (blocks_fold_test.go); new `TestStreamingThinkingFoldsToOneThoughtRowWhenTheTurnEndsAt80x24` |
| 6 | `TestTabFocusesTheBlockListWithTheReplySelected` (block_list_test.go) |
| 7 | `TestEFoldsTheSelectedBlockThroughThreeModes` (block_list_test.go), `TestTruncatedFailedToolOnTheScreen` |
| 8 | `TestEOnAToolWithFiveLinesShowsAllAndNoHint` (block_list_test.go) |
| 9 | new `TestSecondFoldKeyOnTheFailedToolShowsTheCommandRowThenEveryOutputLine`; `TestEFoldsTheSelectedBlockThroughThreeModes` |
| 10 | new `TestFoldKeyOnAnEditOfALaterLineShowsTheDiffRowsThenOneRow`; `TestEOnAnEditShowsTheDiffThenOneRow` |
| 11 | `TestRightAndLExpandLeftAndHCollapse` (block_list_test.go) |
| 12 | `TestRightOnTheReplyChangesNothing` (block_list_test.go) |
| 13 | `TestLeftAndRightMoveTheCursorInThePrompt` (block_list_test.go) |
| 14 | new `TestSelectedReplyHasTheRailOnEveryRowWhenReachedFromBelowAndWithG`; `TestSelectedReplyHasARailOnEveryRowAndNoShade` |
| 15 | `TestSelectedCollapsedStepIsShadedAndHasNoRail` (block_list_test.go) |
| 16 | `TestSelectedCollapsedHookIsShadedOnBothRows` (blocks_hook_test.go) |
| 17 | `TestSelectedCollapsedStepShowsTheExpandIndicator` (block_list_test.go) |
| 18 | new `TestPointerOverACollapsedHookShowsTheIndicatorUntilItLeaves` (real pointer messages); `TestHookIndicatorReplacesTheGlyphOnHoverAndKeepsTheColumns` |
| 19 | new `TestFoldAllKeyMovesEveryToolThinkingAndHookBlockThereAndBack`; `TestShiftEFoldsAndUnfoldsEveryStep`, `TestFoldAllMovesHooksWithTheTools` |
| 20 | `TestCtrlTTogglesThinkingInEveryFocusAndCtrlEOnlyInTheList` (block_list_test.go) |
| 21 | `TestPromptOfSixRowsShowsThreeAndPlusThreeLines` (blocks_fold_test.go) |
| 22 | new `TestEnterOnTheSelectedToolRowOpensTheViewerAndEscReturnsToTheSameList` (keys only); `TestViewerShowsTheCommandAndTheOutputOfATool`, `TestViewerEscReturnsToTheSameListAndSelection` (transcript_view_test.go) |
| 23 | `TestClickOnTheFirstRowOfAStepSelectsAndOpensTheViewer` (mouse_test.go; includes the click on `← back`) |
| 24 | `TestBlockedHookShowsItsRowAndDescriptionAt80Columns` (blocks_hook_test.go) |
| 25 | `TestHookViewerShowsTheDetailFieldsAndNoDescriptionRow` (blocks_hook_test.go) |
| 26 | `TestHookWithoutDescriptionShowsItsCommandOnTheSecondRow` (blocks_hook_test.go) |
| 27 | new `TestWheelUpShowsTheEndMarkerThenANewToolEventCountsTheRowsBelow` (wheel message, tool events); `TestWheelFreeScrollUpShowsTheEndMarkerCentered`, `TestMarkerShowsTheNewLineCountAndTheTopRowDoesNotMove` (scroll_follow_test.go) |
| 28 | `TestMarkerUsesTheShortFormUnder30Columns` (scroll_follow_test.go) |
| 29 | `TestEveryWayToTheBottomRemovesTheMarkerAndKeepsFollowOn`, `TestClickOnTheBottomMarkerJumpsToTheBottomAndFollowStaysOn` (mouse_test.go) |
| 30 | new `TestDragOverWrappedReplyTextCopiesItAndTheStatusRowSaysCopied` (text `directory. TestCount still fails`); `TestDragCopiesThePlainTextOfTheCells`, `TestScrollScreens/drag_copied_80x24`; failure texts in clipboard_test.go |
| 31 | `TestClickInThePromptFocusesItAndPlacesTheCursor` (mouse_test.go) |
| 32 | `TestPromptPaneGrowsToHalfTheScreen` (session_test.go) |
| 33 | `TestSelectorTakesThePromptPaneSlot` (session_test.go) |
| 34 | `cmd/tui` `TestInteractiveExitLineAndTerminalState` (quit command, ctrl+c twice, SIGTERM, SIGHUP; real `ask`, via `requireMainScreenRestored`: alt screen off, modes 1000/1002/1003/1006 off, cursor visible, last line `ask: session <id>`); `TestRunTerminalPrintsTheExitLineAfterTheScreenIsRestoredOnASignal` |
| 35 | new `TestEveryRowOfTheNarrowScreensFitsFortyColumns` (compact session, truncated failed tool, streaming turn, long prompt, tool viewer, hook viewer, scrolled up with new output) |
| 36 | new `TestResizeTo120x40KeepsTheSelectedBlockInViewAndTheBarOnTheLastRow`; `TestResizeKeepsDraftFocusAndFollow` (model_test.go) |
| 37 | `TestCopyNativeCommandShowsCopiedNotice`, `TestCopyUsesFinishedReplyAndSkipsThinkingAndToolOutput`, `TestCopyUsesReplyOfRestoredHistory`, `TestHookIsNotPartOfTheCopiedReply` |
| 38 | `TestMarkerHoverAndSelectionDrawingStayUnderTheFrameBudget` (mouse_test.go: 2000 blocks, 80x24, wheel messages through the Model, median < 16 ms), `TestPaneFrameBudget2000Blocks` (pane_test.go) |

Count: 26 criteria were already asserted as written; 12 new tests were added (4, 5 end state, 9, 10, 14, 18, 19, 22, 27, 30, 35, 36). One test-race fix in `cmd/tui/exit_line_test.go` (see below). No product bug found.

## Criteria that cannot be asserted exactly as written

- 35, "every row is exactly 40 columns": the renderer does not pad a row with trailing blanks (a blank cell and a missing cell look the same on a terminal; the sketches are padded by their generator). The test asserts: 14 rows, no row wider than 40, the header and every rule exactly 40 columns, the header starts with `◢◤ ask ● 7c1e`.
- 35, header on V1 and V3: the viewer replaces the header row with ` ← back  <title> <outcome>` (as the V1/V3 sketches show), so the `◢◤ ask ● 7c1e` header is asserted on A1, A3, A5, A6 and S1 only; the viewer rows assert the `← back` row of exactly 40 columns.
- 10, `  10 - ` and `  10 + `: the shared compact-session fixture edits line 4, not line 10. The new test uses an edit whose change is at line 10 and asserts `10 - n++`, `10 + if f != ""`, then the one-row form.
- 14, "`k` from a block below the reply": the compact session has no block below the reply, so the test appends one tool step after it; the `Tab`, `g`, `G` variant also runs.
- 25, `Reason   recursive removal…`: the existing test asserts the full reason text, which contains the criterion prefix.

## Fix outside the plan's files

- `cmd/tui/exit_line_test.go` `onlySessionID`: the header shows `● leader` before the session exists, so the SIGTERM case failed once in about five runs with "ask created one session: 0". The helper now polls (up to `exitWait`) for the session. No product code changed.
- `block_list_rig_test.go`: `listModelAt` now calls a new `listModelWithTranslator` (also returns the translator) and `playEvents`; behavior of existing callers is the same.

## Verification

- `go vet ./internal/tui ./cmd/tui` clean; `go test ./internal/tui` ok (95.6 s); golangci-lint on `./internal/tui/... ./cmd/tui/...` 0 issues; `go test ./cmd/tui -run TestInteractiveExitLineAndTerminalState -count=3` ok.
- While the other agent's renderer patch was unfinished, `model_test.go` used `tea.WindowSizeMsg.Generation` and the package did not compile; I ran the first full pass through a temporary `go test -overlay` that only removed that field from the file (nothing in the tree changed), and the final run was without it.

## Unresolved questions

- None.
