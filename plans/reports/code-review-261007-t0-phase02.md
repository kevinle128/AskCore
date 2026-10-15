# T0 Phase 2 code review

Status: DONE.
The frozen code is ready for the human approval gate.
No unresolved source defect was found in the reviewed Phase 2 scope.

## Scope and evidence

Reviewed the accepted Phase 2 contract and the frozen [source archive](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/README.md).
All 12 source, test, module, and README files checked match the reviewed scratch files.
The 27 protected product snapshot hashes match.
No root source, dependency, import, workspace, or public contract changed.
The retained Phase 1 regression checks pass in the worker logs.
This review ran no tests and changed no source files.

## Findings resolved

The view now uses measured terminal dimensions and bounds the help row to the terminal width.
The output-error route now causes an actual writer failure rather than a generic error message or a duplicate cancellation case.
The output wrapper preserves the terminal file interface and FD.
The partial failure writes the real synchronized-output enable sequence before it returns an error.
Cleanup runs after the renderer stops and resets mode 2026 through the same ordered output owner.
The captured bytes, error cause, cleanup reset, and absence of the failed frame text have assertions.
The saved stock cleanup failure precedes the cleanup fix.
Permanent output failure reports terminal-byte restoration as unavailable and does not claim that those bytes were restored.
The query responder preserves sibling terminal responses.
Status writes have a one-second deadline on a nonblocking inherited pipe.

## Acceptance checks

Retained draft changes occur in Update, and View has no I/O.
The PTY tests answer actual terminal capability queries and send actual encoded keys.
They distinguish Enter submission from modified-key newline insertion and test the Alt+Enter fallback.
The bracketed paste arrives in transport fragments and produces one semantic PasteMsg.
Full draft hash, byte length, line count, paste count, and submit count are checked.
CRLF normalization preserves the other draft bytes, including tabs, CJK, emoji, and CSI-u-like paste text.
The editor checks Unicode cell geometry, grapheme boundaries, bounded viewport rows, and visible cursor positions.
Separate child cases cover normal quit, context cancellation, partial and permanent output errors, model and command panics, SIGINT, SIGTERM, and SIGHUP.
An incomplete bracketed paste tests exit with input pending.
The fixture joins the owned child, query pump, output reader, status reader, and output-error watcher.
Diagnostics use a separate output.
The macOS termios snapshot is taken after tea.Run returns and before the child exits.
Terminal mode checks follow process exit and output drain.

The worker's [final suite log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-tests-final.txt) records 12 passing top-level tests.
The [race log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-race-final.txt) passes.
The saved vet and build logs have no errors.
Independent tests await the human code approval gate.

## Remaining limits

Real iTerm2 3.7.3 observations remain pending because app access was denied.
Automated terminal evidence does not replace that manual check.
Complete G6 acceptance is not claimed.
G1 ordered transcript and commit-frontier checks remain Phase 3 work.
No G1, G2, G3, or D14 pass is claimed.
