# T0 Phase 3 code review

Status: DONE.
The frozen implementation has no unresolved source defect in the accepted Phase 3 scope.
It is ready for independent tests.
This review ran no tests and changed no source files.

## Scope and isolation

Reviewed the accepted [Phase 3 contract](../261006-1649-t0-inline-prototype-gate/phase-03-ordered-scrollback.md), pinned Bubble Tea insertion and shutdown code, and the frozen [source archive](../261006-1649-t0-inline-prototype-gate/artifacts/phase-03-source/README.md).
The archived Go source, tests, README, and module files match the reviewed scratch files.
The frozen source manifest SHA-256 is `65b14db3371b3df33b92e270afb4b9ca2da4fe3e83802a4198619db5580499b7`.
All 27 protected product hashes match.
No root dependencies, product behavior, or public contracts changed.
Existing Phase 1 and Phase 2 checks remain in the full suite.

## Output and progress

The terminal file wrapper preserves the complete terminal file interface and FD.
One owner serializes insertion, frame, and cleanup writes.
Stock insertion uses WriteString, which the wrapper overrides to prevent the embedded file from bypassing observation.
Frame writes do not generate insertion confirmation, even if the editor contains a transcript marker.
Scheduled and confirmed progress are separate.
Only a complete successful insertion with the expected block identifiers advances confirmed progress.
A command return is not treated as write confirmation.
The ready prefix and pending flag permit one outstanding ordered insertion.
The observer has one reader, and the program joins it on shutdown.

Actual errors and short writes latch failure under the output lock before later content can be written.
The observer treats a short write with no error as a failure.
The failure record retains requested bytes, written bytes, exact prefix bytes, and the prefix hash.
An incomplete block does not advance the confirmed frontier.
Later insertion and frame content is rejected without automatic replay.
Cleanup records are separate from blocked content attempts.
After failure, strict cleanup parsing permits terminal mode restoration and removes close-frame cursor moves and erase bytes.
Unknown control sequences and clear-screen writes are rejected.
Recoverable synchronized-output failure resets mode 2026 through the same owner.
Permanent output failure reports terminal-byte restoration as unavailable.
Termios restoration is measured separately from terminal-byte restoration.

## Scenario coverage and evidence

The PTY cases assert 2,000 uniquely numbered lines in exact order across history and screen.
They cover a growing live tail with typing, an oversized 30-line block, live-region shrink, and completion in a different order from submission.
They check the visible editor, exact cursor position, and absence of stale tail rows.
Failure cases cover zero-byte, partial-error, short-write, and permanent output failures.
The reported prefix is checked against actual PTY output.
The pending resize case changes the real PTY size and sends SIGWINCH while insertion is held before the write.
It then checks model dimensions, all ordered lines, and the editor cursor.
One-byte and randomized output fragments preserve the terminal result.
Owned child and fixture readers are closed and joined.
Diagnostics remain separate from terminal output.

Stock oversized, ordered, and shrink baselines pass without a renderer patch, fork, pre-wrapping, or custom committer.
No kiln renderer fix class was used.
The initial observer timeout is recorded as an observation problem, not a renderer failure.
The owner RED artifacts show the WriteString bypass, missed error latch, false frame confirmation, and unsafe cleanup classification before their fixes.
The invalid manual-start test timeout flag is recorded as infrastructure evidence only.
No behavioral RED is claimed for that command addition.

The worker [suite log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-03-source/evidence/phase3-final-suite.txt) records 27 passing top-level tests.
The [race log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-03-source/evidence/phase3-final-race.txt) passes.
Saved vet, build, and gofmt logs have no errors or unformatted files.
These are worker results, not an independent test run by this reviewer.

## Remaining limits

Real iTerm2 3.7.3 observations remain pending.
The automated results do not replace manual native scrollback, editor, resize, exit, and shell-input observations.
Complete G1 and G6 acceptance is not claimed.
G2, G3, D14, and full T0 acceptance are not claimed.
Independent tests follow this source review under the user's gate waiver.
