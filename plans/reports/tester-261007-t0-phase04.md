# T0 Phase 4 independent tests

Status: DONE_WITH_CONCERNS.
All independent automated checks pass.
Real iTerm2 evidence remains pending.

## Commands and results

Run in `/private/tmp/askcore-t0-09kf6w9r` with `GOWORK=off`, `GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache`, and `GOMODCACHE=/private/tmp/askcore-t0-mod-cache`.

| Command | Exit | Result |
|---|---:|---|
| `go test -v -run 'TestLayout\|TestSelector\|TestG2\|TestG3' -count=1 -timeout 90s ./...` | 0 | 6 top-level tests pass |
| `go test -race -v -count=1 -timeout 90s ./...` | 0 | 33 top-level tests and 30 subtests pass |
| `go vet ./...` | 0 | No output |
| `go build -o evidence/tester-phase4/g2 .` | 0 | No output |
| `gofmt -l *.go` | 0 | No output |

Logs and command metadata are in [independent evidence](../261006-1649-t0-inline-prototype-gate/artifacts/phase-04-source/evidence/tester-phase4/result.json).

## Scenario assertions

The selector check asserts the exact visible window of items 10, 11, and 12, selected cursor X/Y, and selection retention after width changes.
Closing restores the draft and its moved cursor, rejects selector-only typed and pasted content, and removes stale selector rows.
Stable-height checks retain all ten committed lines in order.
Height shrink checks valid live state and exactly one emission of each committed token; native history preservation is not inferred from x/vt.

Real PTY ioctl and SIGWINCH drive resize messages.
Hand-written Unicode rows are checked at widths 13 and 12; widths 2 and 1 with height 1 keep a bounded cursor.
Repeated width and height changes preserve draft bytes and one editor.
Resize during streaming retains all 2,000 numbered lines in order and checks that each token was emitted once.
Layout checks bound tail, active slot, and help rows; selector boundary checks cover widths 1, 2, 10, and 40.
Owned children are waited for, and fixture cleanup closes PTYs and joins reader and responder goroutines.
No detached process was started by this tester.

## Integrity and limits

All 148 pre-tester Phase 4 archive hashes match.
All 20 frozen source, test, module, and README files match the archive.
Earlier immutable archives verify: Phase 1 has 34 hashes, Phase 2 has 79, and Phase 3 has 130.
All 27 protected product hashes match.
No source, root product, earlier archive, or plan file was changed by this tester.
The controller owns the manifest update for the added independent evidence.

The source freeze file evidence/phase4-checksums.txt verifies as `0757f201a34add8b09db37517dfc937086584176a41350fe34c2495a9180883d`.
The separate archive-sha256.json verifies as `18f3497a109efb823e1406ecbf2932edb5be012c7bf8c9282fc828bca90d4b78`.
These are different files, so their different digests are expected; no integrity finding remains.

The saved x/vt height-truncation limit remains a limit, not a native-history PASS.
Real iTerm2 selector, Unicode, resize, and history observations remain pending.
No complete G2, G3, T0, or D14 acceptance is claimed.
Unresolved questions: none for this verification.
