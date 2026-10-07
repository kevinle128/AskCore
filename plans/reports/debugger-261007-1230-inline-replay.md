# Inline replay verification

Status: DONE_WITH_CONCERNS

## Independent pending-write test

Added `e2e/tui/pending-resize.py` with an exported `pending_resize` function.
The function holds a real renderer insertion before its write, resizes the native terminal with ioctl/SIGWINCH through tui-test, then releases the write.
No WindowSizeMsg is injected.
The function owns its named session, FIFO descriptors, wrapper, and cleanup.
FD mapping first duplicates above reserved descriptors to avoid a descriptor 3/4 collision.

The oracle separates replay snapshots from ordinary commits.
It compares each replay prefix with its reported entry count, excludes the startup header from the commit count, and verifies that replay did not change the confirmed or scheduled frontier.
It requires increasing replay generations and exactly one ordinary output and acknowledgement for each of 2,000 commits.
It rejects isolated purge sequences and alternate-screen sequences within a generation.
Final native history must retain all 2,000 ordered markers.
The final draft report must preserve text, cursor, and SHA256 integrity.

The default function now resizes the held write to 13×6, then grows to 40×12 after all ordinary commits complete.
Both Alacritty and Ghostty pass this path against `/private/tmp/askcore-inline-replay-candidate/candidate`.
Each engine observed one resize snapshot with one committed marker, followed by 1,999 ordinary commits, then a complete 2,000-marker replay on grow.
All 2,000 ordinary commits and acknowledgements were exact-once.
The candidate binary hashes before and after these tests match.
The final rerun includes the forced-repaint and single synchronized-output-pair changes.
Its binary SHA256 is `f3e76e6b7e78c68b9e30af478af3023c514a096f10436ec7c3d16699b34a2ddd`.
Both engines pass, and the hash stayed equal before and after the run.
The oracle also verifies replay status dimensions as 13×6 followed by 40×12.
Final evidence is retained under [pending-final](../261007-1156-inline-resize-fix/artifacts/replay-capacity-controls/pending-final/results.json).
The earlier passing binary result remains under [pending-supported](../261007-1156-inline-resize-fix/artifacts/replay-capacity-controls/pending-supported/results.json).
Python syntax compilation also passed.

Ghostty fails the final native-history assertion at 52×15.
That dimension remains an explicit `capacity_probe=True` case.
It retains markers 0903 through 1999, with no internal gaps.
The raw recording contains all 2,000 ordinary markers, the correct replay prefix, and one purge generation.
The status is `replay 2 2 1 1`, which does not show a duplicate acknowledgement.
The live frame and hardware cursor are correct.
Evidence is in `/private/tmp/ask-debug-replay-pending/ghostty`.
The prior failed probe is retained under [pending-52](../261007-1156-inline-resize-fix/artifacts/replay-capacity-controls/pending-52/ghostty/status-lines.json).

## History-capacity controls

A plain raw-mode Python process wrote 2,000 markers directly to Ghostty at 52×15.
Without purge, native history retained 1,096 markers, 0904 through 1999.
With purge, it retained the same 1,096 markers.
The extra `DONE` row explains the one-row difference from the candidate fixture.
This reproduces the failure without T0, Bubble Tea, or insertion logic.
Evidence is `/private/tmp/ask-history-capacity-results.json`.

The installed CLI skill documents named profiles with a scrollback setting.
A control used an explicit profile with `scrollback = 100000`.
Ghostty still retained the same 1,096 markers at 52×15.
Evidence is `/private/tmp/ask-history-config-results.json`.
An explicit configuration file therefore does not yet prove a fix for this pinned backend limit.
Keep the exact history assertion failed.

Additional plain Ghostty controls at 13×6 and 40×12 each retained all 2,000 markers in exact order after purge.
Evidence is `/private/tmp/ask-history-supported-sizes.json`.
This supports testing pending resize at those dimensions while retaining the measured 52×15 capacity limit as a separate result.
It does not turn the 52-column failed assertion into a pass.
Plain controls, prior 52-column captures, and passing default pending evidence are retained in [replay-capacity-controls](../261007-1156-inline-resize-fix/artifacts/replay-capacity-controls/sha256.json).
The SHA256 manifest was verified against every retained file.

The earlier partial candidate also failed the same native pending-write history check on Ghostty, with 1,098 retained markers.
Its saved result is `/private/tmp/askcore-inline-resize-fix-5rjbma6z/resize-evidence/native-pending-checked-results.json`.
The new replay route fixes the measured renderer origin behavior but cannot be credited with passing the 52-column Ghostty history contract.

## Limits

Output-failure synchronized-output reset checks remain required in the retained Go suite.
This focused native test does not replace those failure-injection checks.
No production code, immutable archive, dependency cache, or candidate source changed during this verification.
All owned native sessions closed through bounded cleanup.
No real iTerm2 acceptance is claimed.
