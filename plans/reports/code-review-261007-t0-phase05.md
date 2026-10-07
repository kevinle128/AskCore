# T0 Phase 5 code review

Status: DONE.
The frozen evidence implementation has no unresolved source finding in the accepted Phase 5 scope.
It is ready for independent tests.
This reviewer ran no tests and changed no source files.

## Scope and integrity

Reviewed the accepted Phase 5 contract, evidence validation, draft export, tests, README, and [T0 result report](t0-261007-inline-prototype-result.md).
The frozen [source archive](../261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source/README.md) matches the reviewed scratch source, tests, modules, and README.
The scratch source checksum file SHA-256 is `a31c65689f378674741ed3f5c2fedf0462b4a184f542cb822798f4f730c2600e`.
This hash identifies `evidence/phase5-checksums.txt`, not the archive manifest.
All 27 protected product hashes match.
No root source, dependency, workspace, roadmap decision, or public product contract changed.

## Evidence contract

The manifest records commands, environment, terminal dimensions, dependency graph, criteria, results, reasons, artifact paths, and hashes.
Validation rejects missing blocking records, duplicate or unknown gates, missing commands or artifacts, changed hashes, missing optional records, invalid dimensions, and inconsistent D14 readiness.
Local artifact paths reject absolute paths and parent traversal.
This is a local generated evidence contract, not an external JSON ingestion service.
All 10 artifact hash references match the frozen files after terminal captures stopped changing.
G1, G2, G3, and G6 have separate automated pass and real-terminal pending records.
The manifest has `D14Ready: false`.
Clean isolated rerun evidence is separate from the gate records.

G4, G5, pixel, palette, keepalive, and modifyOtherKeys have explicit not-run reasons.
Their tests skip with those reasons rather than reporting successful terminal behavior.
The result report keeps those checks outside the blocking gate score.
No hardware support or unsupported capability is inferred from denied app access.

## Review correction and checks

The evidence command test now writes to a temporary evidence copy.
It cannot overwrite the final manifest before later PTY tests rewrite captured output.
The documented workflow generates the final manifest after terminal tests and validates it without running terminal cases again.
Valid RED records cover missing evidence commands and artifacts, the missing packaging command, and the missing draft-report flag.
Infrastructure failures remain separate from behavioral RED claims.

The optional draft report is written after terminal cleanup with private file permissions.
File errors cause a nonzero exit.
A real PTY test checks a single 2,000-line paste against full normalized text, SHA-256, line count, paste count, and submit count in the exported JSON.
This supports the runnable manual checklist but does not claim that iTerm2 was tested.
The export adds no second terminal output owner or retained model mutation path.

The worker clean-copy race log passes 39 top-level tests, with explicit nonblocking capability skips.
Saved clean regular-suite, vet, build, and formatting checks pass.
Final evidence validation passes after manifest generation.
These are worker results, not an independent test run by this reviewer.
The module boundary checks reject root imports, replacements, scratch go.work, and active workspace coupling.
Prior terminal, output failure, and child cleanup checks remain in the full suite.

## Route and remaining limits

The report recommends stock Println as a measured candidate with explicit output-owner adaptations.
It records zero kiln-class renderer fixes and does not call the whole fixture unchanged stock behavior.
WriteString observation, complete-write progress, failure latching, and owned filtered cleanup are disclosed.
No fork, custom committer, fallback decision, or product dependency migration was made.

Real iTerm2 3.7.3 observations remain pending because app access was denied.
Native height-shrink history reflow and independent Unicode placement remain unproved.
The x/vt truncation limit and macOS pre-process-exit termios snapshot limit remain explicit.
Permanent output failure does not claim terminal-byte restoration.
Full T0, G1/G2/G3/G6, and D14 acceptance remain incomplete.
Independent tests follow this review under the user's gate waiver.
