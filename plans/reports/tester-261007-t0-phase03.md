# T0 Phase 3 independent tests

Status: DONE_WITH_CONCERNS.
The frozen Phase 3 automated checks pass.
Real iTerm2 evidence remains pending, so complete G1 acceptance is not claimed.

## Commands and results

Run in `/private/tmp/askcore-t0-09kf6w9r` with `GOWORK=off`, `GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache`, and `GOMODCACHE=/private/tmp/askcore-t0-mod-cache`.

| Command | Exit | Result |
|---|---:|---|
| `go test -v -run 'TestTranscript\|TestCommit\|TestG1' -count=1 -timeout 90s ./...` | 0 | 14 top-level tests and 13 subtests pass |
| `go test -race -v -count=1 -timeout 90s ./...` | 0 | 27 top-level tests and 30 subtests pass |
| `go vet ./...` | 0 | No output |
| `go build -o evidence/tester-phase3/g1 .` | 0 | No output |
| `gofmt -l *.go` | 0 | No output |

The full suite includes TestManualTranscriptStart, which is outside the narrower name filter.
Logs and command metadata are in [independent evidence](../261006-1649-t0-inline-prototype-gate/artifacts/phase-03-source/evidence/tester-phase3/result.json).

## Terminal and writer checks

Stock insertion retains 2,000 numbered lines exactly once and in order across history and visible rows.
The oversized and live-region shrink cases preserve the editor and its exact cursor.
Out-of-order readiness waits for a contiguous prefix and produces the correct terminal order.
The streaming case retains input and cursor state at four checkpoints and verifies all 2,000 lines.
The pending-write case changes real PTY dimensions and sends SIGWINCH before releasing the insertion, then checks model size, terminal order, and cursor.
One-byte and randomized output fragmentation retain all 2,000 ordered lines.

Zero-byte, partial, short-nil, and permanent writer failures leave the confirmed frontier unchanged.
Failure metadata checks written-prefix length and hash against actual PTY capture.
The checks reject later transcript/frame output and automatic replay.
The transcript-owner synchronized-output failure case verifies enable and cleanup reset bytes without failed-frame replay.
Permanent failure reports terminal-byte restoration as unavailable, not restored.
Owned children are waited for, and fixture cleanup closes PTYs and joins readers and responders.
No detached process was started by this tester.

## Integrity and remaining evidence

All 123 pre-tester Phase 3 archive hashes match.
All 16 frozen source, test, module, and README files match the archive.
The immutable Phase 1 archive has 34 matching hashes; Phase 2 has 79 matching hashes.
All 27 protected product hashes match.
No source, root product, earlier archive, or plan file was changed by this tester.
The controller owns the manifest update for the added independent evidence.

Real iTerm2 observations remain pending.
No complete G1, G6, D14, or T0 pass is claimed.
No independent automated failure blocks continuation after source review.
Unresolved questions: none for this verification.
