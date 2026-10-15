# Independent inline resize replay verification

Status: DONE_WITH_CONCERNS.
The accepted purge/replay candidate passes all 18 independent headless cases on Alacritty and Ghostty.
Candidate Go checks, race checks, vet, build, and the authored renderer regression pass.
Real iTerm2 and extreme-size acceptance remain unproved.

## Frozen source and test context

Read the candidate README and PROVENANCE.md; the task's PROVENANCE.json path is absent.
The source manifest digest is 788dee6e7aadb89d076ec4c4f9348f27f067539f0e3efae16df4125c6a1f2aa2.
All 987 source-manifest entries match before and after independent checks.
Tests run in the fresh owned copy `/private/tmp/askcore-replay-independent-srhgy0lc/source`.
The original frozen candidate is unchanged, and all copied Go source files still match it.
Durable logs, results, boundaries, recordings, and cleanup evidence are in [independent verification](../261007-1156-inline-resize-fix/artifacts/replay-verification/independent/native/results.json).

Use the exact Go 1.27 binary `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.darwin-arm64/bin/go` with GOTOOLCHAIN=local, GOWORK=off, GOMODCACHE=/private/tmp/askcore-resize-mod-cache, and the owned context's cache directory.

| Command | Exit | Result |
|---|---:|---|
| `go test -v -count=1 -timeout=120s ./...` in copied candidate | 0 | 43 top-level tests pass |
| `go test -race -v -count=1 -timeout=120s ./...` | 0 | 43 top-level tests pass |
| `go vet ./...` | 0 | Pass |
| `go build -o ../candidate .` | 0 | Pass |
| `go test -mod=mod -v -run TestInlineRepair -count=1 -timeout=60s ./...` in copied bubbletea | 0 | Authored renderer regression passes |
| `python3 e2e/tui/test-assertions.py` | 0 | Five oracle controls pass |
| `python3 e2e/tui/run.py --target /private/tmp/askcore-replay-independent-srhgy0lc/candidate --artifacts /private/tmp/askcore-replay-independent-srhgy0lc/native` | 0 | 18 headless cases pass |

The native command uses narrow sandbox escalation for unique CLI session locks under ~/.tui-test.
It also independently runs the 39 retained historical Go checks, which pass.

## Native results and accepted contract

Both engines pass G1, G2, G3, and G6.
Both pass the four draft variants: cursor in the middle, explicit hard lines, long wrapped text, and trailing spaces.
Both pass pending-write resize with 2,000 ordinary committed markers and complete ordered replay snapshots of 1 and 2,000 markers.
The default native results retain exact case records and raw recording artifacts.

The candidate follows the accepted resize-only purge contract.
Ordinary commits remain exactly once; each resize reconstruction is a complete ordered generation.
The oracle controls reject missing, duplicate, reordered, and partial generations and stale editor rows.
Alternate-screen use remains forbidden.
The candidate Go suite also passes superseded repair generation, acknowledged-prefix waiting, content-stop on replay failure, and real partial-write terminal restoration checks.
The renderer regression proves one synchronized BEGIN/END pair per successful repair.
The application transcript and commit frontier remain separate from replay output.

## Provenance, boundaries, and cleanup

The candidate identifies Bubble Tea v2.0.10 and UV f5a850f9c2b7.
Provenance identifies the four modified Bubble Tea files, the new replay API/test files, retained MIT licenses, and local patches.
Ultraviolet remains unmodified under the documented candidate contract.
No Grok source body is copied.
All source and notice files covered by the frozen manifest match.

The native runner's before/after immutable archive and protected product hashes match.
The current protected before/after product snapshot covers 426 files and is the active plan authority.
No removed historical temporary baseline is claimed.
No production source, root dependency, or historical archive was changed by this tester.
The owned module copy is retained as reviewable test evidence rather than detached execution.
All 18 sessions close, and a scoped process inspection finds none of their 36 recorded daemon/child PIDs alive.
No global daemon-stop command is used.

## Remaining limits

The earlier no-purge candidate remains failed evidence and is not accepted by this report.
The user-approved route can remove terminal history from before application startup.
The default pending-write check covers 13x6 to 40x12.
Ghostty 52x15 capacity remains explicitly unsupported in the candidate evidence; the optional capacity probe was not rerun independently.
No result here certifies width-1 extreme-size behavior, Terminal.app, iTerm2, or D14 readiness.
Unresolved questions: none for this verification.
