# T0 inline prototype result

Automated evidence supports stock Bubble Tea v2.0.10 Println insertion with an ordered output observer and explicit failure cleanup.
D14 is not ready.
Real iTerm2 3.7.3 observations remain pending because computer-use app access was denied.
No product dependency migration, roadmap decision, fork, or custom committer was made.

| Gate | Automated result | Real-terminal result |
|---|---|---|
| G1 | PASS: 2,000 ordered lines, oversized insertion, live shrink/cursor, ready order, partial failure/no replay, pending resize | Pending |
| G2 | PASS: bounded selector window, exact cursor, draft restoration, stable-height history, actual resize | Pending |
| G3 | PASS: hand-written Unicode cells, boundary sizes, repeated actual resize, exact-once stream output | Pending; native height-shrink history reflow unproved |
| G6 | PASS: actual negotiated/fallback keys, one full 2,000-line PasteMsg, integrity hash, exit routes and cleanup | Pending |
| G4 | Not run: no image-capability responder or selected-terminal image observation | Not run |
| G5 | Not run: M2 alternate transcript reconstruction is not exercised by the inline fixture | Not run |
| Optional pixel/palette/keepalive/modifyOtherKeys | Not run, with explicit reasons | Not run |

## Evidence and reproduction

The [scratch README](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/README.md) contains exact commands and the iTerm2 checklist.
The [machine manifest](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/manifest.json) separates automated and real-terminal results and reports `D14Ready: false`.
The [final hash check](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/phase5-final-manifest-check.txt) verifies all 10 artifact references after terminal captures stopped changing.
The [source manifest](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/phase5-checksums.txt) identifies the final scratch source.
The controller retained source, module files, README, and evidence in the portable archive.
Prior phase archives remain immutable.

A separate module copy at `/private/tmp/askcore-t0-clean-lmppemhc` passes the regular suite and the final race run.
The final race run passes 39 top-level tests; nonblocking capability cases are explicit skips.
The final source passes vet, build, and formatting.
All Go source and module files in the clean copy match scratch.
The boundary check rejects root imports, replacements, scratch go.work, and active workspace coupling.
The pinned graph uses Bubble Tea v2.0.10, creack/pty v1.1.24, and x/vt v0.0.0-20261004011457-ad85c59fdf4e.
The worker runs use Darwin 25.6.0 arm64, Go 1.27.0, C.UTF-8, and GOWORK=off.
The product baseline commit is d9279ab5adc341312550430d7579f18a05e6b17d; scratch source checksums are separate.

The packaging scenario runs after tests and validates hashes; it does not execute terminal cases or hardware checks.
Tests for the packaging command use temporary evidence copies, so later capture writes cannot silently change the final manifest during a suite.
The missing-command and missing-artifact RED assertions are preserved.
The optional final-draft report is checked with a real PTY 2,000-line paste and full text/hash/count assertions.
The manual checklist uses public flags and a fixed payload, without requiring control/status descriptors.
No manual result is claimed until that checklist is executed in iTerm2.

## Route and limits

Zero kiln-class renderer fixes were used.
Stock Println placement passed before renderer changes were considered.
The required output-owner adaptations are explicit WriteString insertion observation preserving term.File, complete-write frontier/error latch, and post-renderer owned mode 2026 reset with filtered restoration bytes.
These adaptations are not a claim that the whole fixture is unchanged stock behavior.
A tea.Cmd return does not confirm terminal output.
Partial or short writes stop content output and do not advance the frontier or replay the block.
Permanent output failure reports terminal-byte restoration as unavailable.

Termios is compared after tea.Run returns while the child is alive.
On macOS, session-leader exit revokes the retained slave, so no post-process termios guarantee is claimed.
Final ANSI mode checks occur after child exit and master drain.
Pinned x/vt Screen.Resize truncates visible rows on height shrink without moving them into native history.
The saved failure is an emulator limit, not a renderer defect.
Stable-height history checks remain strict; height-shrink checks prove live geometry and no application replay or global erase.
Real-terminal native reflow and Unicode placement remain required.

All owned children were closed and waited; all tracked tool sessions completed.
The [process ledger](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/phase5-owned-processes.json) retains child PIDs, temporary command paths, test context, and cleanup status.
The shared fixture log labels scenarios g6; actual scenario arguments are defined by the test call sites.
System-wide process inspection was sandbox-denied; no detached process was started and no known owned process remains.
The controller must verify protected product hashes and final archive integrity during independent verification.
Full T0 and D14 acceptance stay incomplete until the blocking real-terminal evidence is recorded.

## Independent verification

Independent tests ran in a fresh isolated copy and passed all 39 top-level tests and 44 subtests under race.
Six nonblocking capability cases skipped with explicit reasons.
Focused checks, vet, build, formatting, post-suite packaging, and final artifact validation pass.
The original frozen captures were not changed.
See the [independent report](tester-261007-t0-phase05.md) and [archived logs](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/evidence/tester-phase5/).
All 27 protected product hashes and earlier archives match.
The final archive manifest includes 189 file hashes.
Real iTerm2 evidence remains pending; D14 is not ready.
