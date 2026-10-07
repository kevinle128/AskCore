# T0 Phase 5 independent tests

Status: DONE_WITH_CONCERNS.
Independent automated checks pass in a separate fresh module copy.
All real iTerm2 blocking-gate observations remain pending, and D14Ready remains false.

## Test context and results

Tests and executable runs used `/private/tmp/askcore-t0-tester-phase5-wly3m2_w`, copied from frozen scratch source with compiled binaries excluded.
The copy used GOWORK=off and the task GOCACHE and GOMODCACHE paths.
The owned copy was removed after verification; no detached process was started.
Original frozen source, captures, and manifest were not changed.

| Check | Exit | Result |
|---|---:|---|
| Focused evidence, boundary, manual paste report, capability checks | 0 | 6 top-level tests, 14 passing subtests, 6 capability skips |
| Full race suite, 60-second timeout | 0 | 39 top-level tests, 44 passing subtests, 6 capability skips |
| Vet | 0 | No output |
| Build | 0 | No output |
| Formatting | 0 | No output |
| Package evidence after all terminal tests | 0 | D14 pending |
| Final evidence/boundary validation without terminal tests | 0 | 4 top-level tests, 14 passing subtests, 6 capability skips |

Exact commands, exits, and logs are in [independent evidence](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/tester-phase5/result.json).
The actual draft-report test name is TestManualPasteReport.
It verifies the optional report against a complete normalized 2,000-line draft with one paste, zero submissions, and matching hash.

## Evidence and integrity

All 10 original manifest artifact references match before execution.
After the full suite, packaging regenerated the manifest in the owned copy, and all 10 copied manifest artifact references match.
The copy's final manifest is preserved with the independent logs.
Manifest validation rejects missing commands, missing artifacts, wrong hashes, false D14 readiness, duplicate gates, and missing optional records.
G4, G5, pixel, palette, keepalive, and modifyOtherKeys remain not-run with reasons.
Their six test skips are not capability passes.

RendererFixClasses is 0.
The manifest separately discloses output-owner insertion observation, confirmed-frontier/failure-latch behavior, and owned cleanup adaptations.
Each G1/G2/G3/G6 real-terminal result remains pending; D14Ready is false.
The packaging command does not run hardware checks or substitute automated output for real-terminal evidence.

The source freeze file evidence/phase5-checksums.txt verifies as a31c65689f378674741ed3f5c2fedf0462b4a184f542cb822798f4f730c2600e.
All 179 pre-tester Phase 5 archive entries match both archive files and original frozen scratch files.
All 24 frozen source, test, module, and README files match the archive.
Earlier immutable archives verify with 34, 79, 130, and 155 matching hashes.
All 27 protected product hashes match.
The scratch boundary check passes for root imports, replacements, and workspace isolation.
Owned PTY children are waited for, and reader/responder cleanup completes in the passing suite.
The controller owns the archive manifest update for added independent evidence.

## Remaining acceptance

Real iTerm2 3.7.3 observations remain pending because app access was denied.
The x/vt height-shrink limit and permanent-output byte-restoration limit remain explicit.
No complete blocking gate, T0, or D14 acceptance is claimed.
Unresolved questions: none for independent automated verification.
