# Independent H13a plan review, 2026-10-08

## Scope and authorization

Review the implementation against `plans/261007-0700-h13a-acp-stdio/plan.md` and its four phases.
Claude Code reported implementation and its own review complete before these repairs started.
No commit or push was requested or made.
Existing workspace changes were preserved.

## Verified findings and repairs

| Finding | Evidence before repair | Repair and regression check |
|---|---|---|
| EOF hangs after a null-ID extension notification | Production `ask` stayed alive after stdin closed; an independent Python peer timed out after 4 seconds | Match the SDK's null-ID notification semantics; `TestACPE2EShutdownNullID` |
| EOF hangs after an envelope that the SDK cannot decode | Production E2E failed for numeric `jsonrpc` and a string `error`; independent reviewer reproduced both | Decode the same complete typed envelope as the SDK before tracking a response; `TestACPE2EEOFMalformedEnvelope` |
| Request tracking misses leading whitespace, a final unterminated frame, and standard methods | Tracker inspection showed a first-byte check, newline-only registration, and method filtering | Track every decodable request and the final EOF frame; `TestACPE2EEOFDeliversUnterminatedRequest` |
| Prompt response can overtake followed lifecycle events | Production E2E failed with the prompt response before `turn_end`, `cycle_end`, and `agent_settled` | The session writer waits for active Follow physical-write frontiers, or completed detach/resync; `TestACPE2EPromptIncludesFollowCompletion` |
| Repeated Unfollow fails although the plan specifies idempotence | Production `TestACPE2EUnfollow` failed on the second call with `unknown_subscription` | Return the empty result for an ended subscription; keep the error for another session's subscription and for an unknown session |
| Output errors during EOF cleanup are discarded | Host failure latch ignored errors once close began | Preserve the first output error during disposal; `TestStdioEOFWithStalledStdoutPeerIsBounded` now checks `ErrOutputFailed` |
| Provider/OAuth evidence does not certify the built binary | Both rows re-executed the Go test binary and injected transport/clock/wait dependencies | Use the production binary with an external HTTPS fixture, CONNECT proxy, and temporary CA; keep real retry delays, production host login, and real credential expiry |
| Stalled-stdout test permits reads before it proves process exit | The test unlocked its reader immediately after SIGTERM | Own the stdout pipe independently and wait for actual process exit before unlocking the reader |

The Follow-release unit test now runs Prompt concurrently and proves that it cannot finish before the Follow result releases live frames.
A synchronous Prompt in that test was incompatible with the corrected physical-write contract and timed out before the fixture was corrected.
The repeated-Unfollow assertion was updated to the accepted idempotent contract.
No test was skipped or weakened to obtain a passing gate.

## Plan comparison

| Phase | Result |
|---|---|
| SDK and wire contract | The pinned SDK, safe DTOs, explicit unsupported capabilities, and precision-safe metadata remain in place |
| Independent session adapter | One host owns independent Agent sessions; execution and control use the existing Agent owners |
| Ordered events and controls | Prompt/Continue barriers now include active Follow writes; reset fencing, queues, resync, and unbounded ordinary tool drain remain protected |
| Stdio, auth, and E2E | Real production provider/OAuth coverage replaces the test-binary gap; malformed input and blocked output have direct regression evidence |

The mandatory event observer uses one bounded synchronous Agent listener rather than a mandatory ring follower.
The listener performs no transport write and fails the connection on queue overflow.
Optional Follow streams use the real Agent ring for replay, snapshot cuts, open-stream baselines, and explicit resync.
This keeps oversized ring events from losing mandatory prompt completion while preserving the verified external contracts.
The implementation does not duplicate standard updates or automatically replay prompts.
The independent review found and repaired the concrete missing cross-stream completion barrier rather than replacing those tested owners.

Started-tool drain, faulting-log no-start failure, and a prepared user-tail Continue remain the disclosed supplemental real-stdio composition proofs approved in the plan.
They are not production-binary certification of injected internal faults.
No production test flag, provider endpoint override, new dependency, or system trust-store change was added.
The CA and proxy exist only in the test fixture.

## Independent quality review

The code-reviewer found the malformed-envelope EOF hang and confirmed the complete envelope fix by source review.
It found no further concrete Follow-barrier or cleanup regression.
Final verification results are recorded below after the controller's gates finish.

## Final verification

All commands ran against the final source on 2026-10-08 and exited 0.

- Focused production EOF/Follow regressions and ACP Follow/Unfollow tests passed.
- `go test ./... -count=1 -timeout=8m` passed all packages; `cmd/tui` completed in 61.586 s.
- `go test -race ./internal/acp ./internal/agent ./internal/bus ./internal/app ./pkg/protocol ./cmd/tui -count=1 -timeout=5m` passed all selected packages; `cmd/tui` completed in 105.478 s.
- `go build ./...` and `go vet ./...` passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` reported `0 issues`.
- `gofmt -l internal/acp cmd/tui internal/app pkg/protocol` and `git diff --check` produced no findings.

Loopback fixtures and Go cache access required sandbox escalation on this host.
Those initial infrastructure failures were not counted as behavioral failures.
The final escalated gates passed without exclusions.
The reviewer and controller joined their reproduction children and removed their temporary review binaries.
