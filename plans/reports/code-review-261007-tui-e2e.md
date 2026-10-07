# TUI E2E code review

Status: DONE_WITH_CONCERNS.
The runner has no unresolved source finding from this review.
The terminal target does not pass the full native-engine suite.
Strict G3 checks fail on both Alacritty and Ghostty and remain failed results.

## Reviewed scope

Reviewed the accepted [plan](../261007-1104-tui-e2e-automation/plan.md), [runner](../../e2e/tui/run.py), [setup](../../e2e/tui/README.md), assertion controls, version pin, ignore rules, and owning README and AGENTS changes.
The frozen runner SHA-256 is `b8375e26737e96727c52aff1457b9d4da8bae6888d2f5ba7c31702ad6b7982f4`.
The README SHA-256 is `6dba6953659c4ac30f97e8c01a043049c950a0ad2f441f84e69c668c66b694b6`.
The assertion-controls SHA-256 is `e3287460b0c0eb70200b167e3756faa07bbaf37cba7e2d385e4c23361480d9c5`.
This reviewer ran no tests, started no sessions, and changed no source files.

## Acceptance review

One documented Python command checks the CLI pin, copies the immutable target module, runs all 39 archived Go checks, builds a fresh binary, and runs the four terminal cases on both default engines.
Python uses only the standard library.
Go dependencies remain in the temporary module with GOWORK disabled.
The target override still runs the archived Go checks.
No production Go dependency or public product contract changed.

G1 checks all 2,000 identifiers in exact order across full terminal text, the editor cursor, and joined output bytes.
G2 checks the exact three-row selector window, retained selection, ignored input and paste, restored draft, and restored cursor.
Its native-engine case has no committed history; the retained Go fixture covers that history contract, as the README states.
G3 checks actual resize, hand-written CJK and emoji cells, cursor position, one editor, retained transcript, and no token replay.
G6 checks real input bytes, newline versus submit, negotiated modified keys where supported, Alt+Enter fallback, and full normalized 2,000-line paste text and SHA-256.
Unnegotiated modified keys have an explicit capability result rather than a modified-key pass.

Conditional waits use terminal-state predicates and deadlines.
Short polling delays do not replace the assertions.
Actual output events are joined before no-replay checks; input events do not count as output.
Negative controls reject missing, duplicate, and reordered transcript identifiers, wrong cursors, duplicate editors, replay, and global erase.
Optimized Python mode is rejected so assertions remain active.

Failures retain CLI commands, terminal state, full history, cells, screenshots, and recordings where IPC remains available.
Capture failures are recorded rather than hidden.
Unique session names prevent reuse of a user daemon.
Startup failure can recover ownership from the unique session PID file and verify its identity before cleanup.
Fallback signals apply only to saved matching PID identities and the matching target child.
Cleanup uses bounded SIGTERM followed by SIGKILL and reports failure if the owned daemon remains available.
There is no global daemon stop or broad process kill.

The archive manifest is checked before use.
Before and after hashes cover the archive, root manifests, and all files under cmd, internal, pkg, proto, and migrations.
Boundary changes fail the run and are saved as evidence.
Prior archives and the target remain unchanged by this implementation.
The owning README and AGENTS changes add the runner location, command, and headless-test boundary.

## Findings resolved

Startup failures now save capture attempts before cleanup and recover ownership when the run command does not return its JSON result.
Product hash coverage now includes non-Go production files, including protobuf and SQL files.
The strict duplicate-editor control replaces the earlier incomplete oracle.
The README explicitly rejects the earlier false pass as acceptance evidence.

## Observed failures and limits

Read the retained strict G3 results at `/private/tmp/askcore-tui-e2e-uklct51w/runner-strict-g3/results.json`.
Both Alacritty and Ghostty fail G3 without a cleanup or boundary error in that record.
The default runner returns exit code 1 while this failure remains.
The worker reports the other six engine/case combinations pass; independent verification is separate.

Paused output replay found correct native reflow before redraw, followed by stale editor rows after redraw.
The README identifies a pinned-renderer reflow compatibility failure and does not claim an engine defect was proved.
The separate extreme-size probe preserves the 1×1 failure and does not convert it into a pass.
The normal case still has strict duplicate-editor assertions.
No target or immutable archive patch was made to hide these failures.

Headless results do not certify Terminal.app or iTerm2.
Real-terminal rendering and restoration observations remain pending.
D14 readiness remains false.
The runnable E2E project is reviewable, but a fully passing terminal compatibility result is not achieved.
