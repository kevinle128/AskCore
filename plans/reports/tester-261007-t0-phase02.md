# T0 Phase 2 independent tests

Status: DONE_WITH_CONCERNS.
The frozen Phase 2 automated checks pass.
Real iTerm2 evidence remains pending, so complete G6 acceptance is not claimed.

## Commands and results

Run in `/private/tmp/askcore-t0-09kf6w9r` with `GOWORK=off`, `GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache`, and `GOMODCACHE=/private/tmp/askcore-t0-mod-cache`.

| Command | Exit | Result |
|---|---:|---|
| `go test -v -run 'TestEditor\|TestInput\|TestG6\|TestRestore' -count=1 -timeout 90s ./...` | 0 | 6 top-level tests and 12 subtests pass |
| `go test -race -v -count=1 -timeout 90s ./...` | 0 | 12 top-level tests and 17 subtests pass |
| `go vet ./...` | 0 | No output |
| `go build -o evidence/tester-phase2/g6 .` | 0 | No output |
| `gofmt -l *.go` | 0 | No output |

Logs and command metadata are in [independent evidence](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/tester-phase2/result.json).

## Verified boundaries and behavior

All 72 pre-tester archive hashes match.
All 12 frozen source, test, module, and README files match the archive.
All 27 protected product snapshot hashes match.
No root import, replacement directive, or scratch go.work exists.
No source or plan file was changed by this tester.
The controller owns the archive checksum update for the added independent evidence.

The two input-capability cases pass with actual encoded keys and fragmented 2,000-line paste.
The checks assert full draft hash, byte count, line count, one semantic paste, and distinct submission.
All nine restoration child cases pass, including signals, context cancellation, both panic paths, and partial and permanent output failure.
The permanent-output case passes its expected-unavailable assertion; it does not prove terminal-byte restoration.
The fixture closes its owned pipes and PTYs, joins reader and responder goroutines, and waits for each child.
No detached process was started by this tester.

Worker RED records were inspected through the frozen README and review report.
The initial input failure and stock partial-cleanup failure remain separate from infrastructure and expectation errors.
No artificial RED run was created.

## Remaining evidence

Real iTerm2 3.7.3 observations remain pending because app access was denied.
No G1, G2, G3, D14, or complete G6 pass is claimed.
There is no independent automated test failure to block Phase 3 continuation.
