# Inline resize candidate review

Status: DONE_WITH_CONCERNS

The isolated candidate must not replace the accepted E2E target.
The controller reports that the default strict G3 case passes on both engines, but all four strengthened Alacritty cases fail and the four Ghostty cases pass.
These failures block the accepted resize contract.
No runtime tests were started by this reviewer.

The reviewed source keeps one output owner, uses an explicit reflow capability, and confirms the resize baseline after a successful owner write.
It flushes a pending resize before transcript insertion.
These changes do not establish a boundary that native terminal resize must preserve.

The retained Alacritty middle case records three `editor>` rows in `failure-history.json`, compared with one in `initial-history.json`.
The final 40-column, 12-row state also contains the extra rows.
The evidence is in `/private/tmp/askcore-inline-resize-fix-5rjbma6z/resize-evidence/strengthened-ready/alacritty/middle/`.
The controller's diagnosis places live rows in native history before the application receives SIGWINCH.
Screen cursor repair cannot erase selected rows outside the addressable screen.

A custom committer can enforce ordered writes and successful-write tracking.
A local renderer fork can track screen geometry and hard or soft row boundaries more precisely.
Neither change alone proves that a native emulator will keep live rows out of history during an asynchronous resize.
An application-owned history boundary is state in the application, unless the terminal supplies and preserves the same boundary.
Do not present either implementation route as a proven solution to this failure.

The next narrow experiment can render a unique committed token and unique live token, perform the failing width and height changes, and inspect output, screen cells, cursor, and history after each change.
Test a restricted scroll region and disabled auto-wrap separately on both engines, including growth after shrink and a cursor inside the draft.
The experiment must also check transcript insertion and restoration of terminal modes.
It can reject a proposed boundary; a pass supports only the measured engine and transitions.
Keep the failed candidate and its captures unchanged for comparison.

An alternate-screen transcript viewport gives the application control of visible transcript and editor rows.
It changes the accepted native-history contract and permits transcript redraw on resize.
A bounded inline route can retain native history only for measured transitions, with explicit unsupported transitions.
It changes the accepted resize scope.
Both options require a user decision before implementation because the accepted plan reserves fallback selection and prohibits silent contract changes.

Root production code, root dependency pins, public APIs, and prior archives remain outside this correction's scope.
Independent source hashes and test results are still needed after the worker freezes the partial candidate.
Real iTerm2 evidence and D14 acceptance remain pending.
