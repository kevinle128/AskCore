# H13b implementation and plan review

Date: 2026-10-09.
Scope: the current tracked changes and new files in this worktree.
This review changes no product code, plan status, roadmap status, or dependency.

## Verdict

H13b implements the main runnable outcome: two built `ask connect` clients share one leader and one session update stream.
It does not yet meet all plan requirements and exit gates.
Eight findings remain: four P1 findings and four P2 findings.
Six added checks fail on the current implementation, and the existing SIGTERM E2E test fails under the race detector.
The forced-exit finding is based on the complete call path; it is not certified by a new production-binary test.
Do not use the current `completed` state as proof that every contract passed.

P1 means a required runtime contract fails and should block completion.
P2 means a required safety, cleanup, or test contract remains incomplete.

## Sources and scope boundary

Read the [H13b plan](../261008-1033-h13b-leader-unix-socket/plan.md), all six phase files, its requirements evidence, implementation journal, and prior review record.
Read the [roadmap H13](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h13-leader-acp-adapter-and-gateway-pis-rpc-mode-multi-client), architecture section 7.3, root command guide, package contracts, and [the accepted leader ADR](../../docs/adr/0004-leader-drivers-output-and-sdk-copy.md).
Read the H13a plan and [independent H13a review](code-review-261008-h13a-plan-compliance.md) to check the protected stdio and Follow contracts.
Trace the router, reverse table, socket transport, process start and stop, ACP mutations, app composition, CLI, and their tests.

The following are accepted decisions, not findings: unbounded socket queues, no aggregate session limit, no automatic driver promotion, take of a busy session with no live driver, the outer-protocol-only version gate, and the line client in place of a rendered TUI.
The SDK source copy has a local relative `replace`, provenance and license records, and a runnable parser limit check.
The injected-owner UID test is an accepted proof method; a literal second-user connection remains a disclosed manual gap.
H13c gateway conversion, remote tokens and Origin checks, durable sessions, compaction, and T1a rendering are outside H13b.
Their absence does not fail this review.

## Findings

### P1: An old driver's model change can commit after a successful take

Owner: [Session.guard and Session.Take](../../internal/acp/host.go), near lines 412 and 451; [setModel](../../internal/acp/ask_methods.go), near line 145.

`guard` checks the generation, then releases both admission locks before the mutation runs.
It counts the mutation in `muts`, but `Take` ignores that count when `LiveDriver` is false.
A model/auth readiness call from the old driver can therefore wait, another client can take the session, and the old call can then change the new driver's model.
The cancel path also checks `currentDriver` and calls `Abort` after releasing the generation lock; it needs the same commit-boundary review.

Evidence: `TestReviewTakeFencesInFlightModelChange` drives a real ACP SDK connection and adapter, holds the old driver's model auth callback, commits generation 2 with the no-live-driver take rule, and releases the old call.
The old call succeeds and changes the model after take returns.
This is a controlled adapter proof of the driver-loss ordering, not a built-client E2E test.

Required repair: check the generation at the actual mutation commit boundary.
Keep the accepted ability to take an already running session after driver loss.
Do not solve this by making take wait for the full prompt or tool drain.
Add controlled races for every driver-only mutation, including cancel and model/auth preparation.

### P1: The line client can drop the first live Follow events

Owner: [lineClient.follow and notification](../../cmd/tui/connect.go), near lines 538 and 364.

The socket reader delivers the Follow result to a waiting command goroutine.
That goroutine later adds the subscription to `c.subs`.
The reader can process the next event or resync frame before that registration, and `following` then treats the live subscription as ended and drops the frame.
The server's FIFO cannot prevent this client scheduling gap.

Evidence: `TestReviewFollowKeepsEventsImmediatelyAfterResult` executes the real line-client response and notification handlers in that order while the command waiter is not scheduled.
The client prints the Follow result but drops sequence 2, an `agent_settled` event.
This is a deterministic client interleaving check, not a built-binary certification.

Required repair: commit Follow ownership on the reader path before it releases the result waiter, or hold early frames until that commit.
Keep the existing fence for ended subscriptions.
Add a built-client Follow test with events sent immediately after the result and check the full sequence.

### P1: A second signal can still wait forever in the command's deferred Stop

Owner: [serveLeader](../../cmd/tui/leader.go), near line 272; `runUntilSignal`, near line 319; [LeaderRuntime.Stop](../../internal/app/module_leader.go), near line 139.

The first signal makes `Serve` enter `rt.Stop` and drain a started tool body without a time limit.
The second signal makes `runUntilSignal` return the signal exit code.
Before `serveLeader` can return to the process entry point, its deferred `rt.Stop` runs again.
`sync.Once.Do` waits for the first Stop call to finish, so this defer restores the unbounded wait that the second signal was meant to bypass.
The command can print `forced exit` while remaining alive.

Evidence: the source path above, the existing controlled started-tool drain test, and the standalone second-signal unit test.
The standalone test calls only `runUntilSignal`; it does not include the command defer and cannot certify the process exit.

Required repair: let the forced path reach the process exit without joining the active drain again.
Keep cleanup for failures before Serve starts.
Add an integration test of the command lifecycle with a started tool body that does not finish after cancellation and two signals.

### P1: A detached non-member can become the driver when its pending take returns

Owner: [removeMember and finishTake](../../internal/leader/router.go), near lines 433 and 745.

A client may take a session without first attaching.
Its take tag then records `memberGen == 0`.
A detach while that take waits does nothing because the client has no membership yet.
`finishTake` skips its detach fence for generation zero and installs that client as the driver after the detach acknowledgment.

Evidence: `TestReviewNonMemberDetachFencesPendingTake` uses real Unix socket clients and the existing disclosed NDJSON transport peer.
A delayed take result arrives after detach; the take succeeds and the detached client becomes the driver.

Required repair: track the request or session membership epoch even before membership exists.
A detach must fence a pending take independently of the old membership map.
Keep the committed host generation but leave no live driver when the result recipient has detached.

### P2: Idle replacement does not count an authenticate operation in flight

Owner: [Adapter.Authenticate](../../internal/acp/agent.go), near line 230; [Host.QuiesceIfIdle](../../internal/acp/host.go); [router.quiesce](../../internal/leader/router.go).

Authenticate invokes the native auth callback without registering work with host admission.
The router checks open reverse calls and host idleness, but does not cover this pending request.
It can accept idle shutdown while an auth readiness operation is still running.
This fails the Phase 3 requirement to count admitted auth operations.

Evidence: `TestReviewIdleShutdownWaitsForAuthenticate` holds a real adapter auth callback and observes `QuiesceIfIdle() == true` before the callback finishes.

Required repair: register auth readiness work under the same admission contract as session creation and mutations.
Also test a request already accepted by the router but not yet admitted by the SDK handler, so the link queue cannot bypass the idle barrier.

### P2: Closing the old Unix listener removes a replacement socket

Owner: [listenPrivate](../../cmd/tui/leader.go), near line 342; [Server.Close](../../internal/leader/server.go).

`net.Listen("unix", path)` returns a Unix listener with automatic unlink on close enabled.
Closing it removes the path before the later `removeOwnSocket` inode check runs.
If that path now holds a replacement socket, the old listener removes the replacement.
The explicit inode check therefore does not protect the full cleanup path.

Evidence: `TestReviewListenerClosePreservesReplacementSocket` opens the production private listener, replaces its path with a second live socket, then closes the old listener.
The replacement path disappears.

Required repair: disable automatic unlink on the owned Unix listener and use only the inode-checked cleanup while the flock is held.
Keep this check as a direct regression test.
The race requires access by the same local user; it is not a cross-UID access claim.

### P2: Native auth cleanup waits behind the complete Agent drain

Owner: [LeaderRuntime.Stop](../../internal/app/module_leader.go), near lines 141 to 143.

Stop calls `Adapter.Close` first and calls native cleanup only after it returns.
A started tool body can keep that first call open forever.
`StopRefresh` and auth drain then never start, contrary to the Phase 6 requirement that native auth cleanup run in parallel with Agent drain, as editor stdio does.

Evidence: `TestReviewNativeCleanupStartsWhileToolDrains` uses the real socket, server, host and controlled started tool from the app test fixture.
After the client is disconnected, the auth cleanup callback does not start until the tool body is released.

Required repair: start native cleanup when shutdown starts, run it alongside Agent drain, and join both on a graceful stop.
Keep the forced exit path separate from that join.

### P2: The required race gate fails in the foreground SIGTERM E2E test

Owner: [TestLeaderE2EForegroundLeaderSIGTERM](../../cmd/tui/leader_e2e_test.go), near line 337.

`os/exec` writes child stderr to `firstErr` while the test evaluates `firstErr.String()` for the Eventually failure message.
`bytes.Buffer` is not safe for that concurrent read and write.

Evidence: the complete selected race command fails here, and the isolated test reproduces the race.
The race is in test output collection; this evidence does not establish a production transport race.

Required repair: use the project's synchronized output collector, or read the buffer only after the command and its copy goroutine finish.
Re-run the narrow test with `-race`, then the full required race gate.

## Plan and roadmap coverage

| Area | Current evidence | Remaining work |
|---|---|---|
| Phase 1: frames, IDs, handshake, paths, flock, peer UID | Current leader/protocol tests pass with race and goleak; SDK parser boundary check passes | Socket replacement cleanup fails; private directory descriptor is explicitly omitted in the phase record |
| Phase 2: routing, driver rules, private Follow, ordered socket output, reverse table | Current router tests pass; the FIFO and observer rules are implemented | Pending non-member take is not fenced by detach |
| Phase 3: one shared adapter/host, route validation, generation, admission | Current real-host routing and editor regression tests pass | Old mutation commit after take, auth admission gap, and full large-frame link evidence |
| Phase 4: two-binary discovery, spawn race, version management, stop and log | Current built-binary cases and controlled tool/version test pass without race | Socket cleanup and stable signal target requirements; SIGTERM test race |
| Phase 5: two built line clients, independent sessions, disconnect, reconnect and slow reader | Whole repository suite passes, including these built-client cases | First Follow event can be lost; add a complete-sequence client check |
| Phase 6: reverse lifecycle, stop order, docs, final gates | Router reverse fixture and real-host tool drain checks pass | Second signal can block; auth cleanup is serial; required race gate fails |
| H13b runnable exit | Two actual `ask connect` processes share the leader and updates | This exit passes, but does not close the other plan gates |
| Full H13 exit | Includes H13c, durable owners and the T1 TUI | Outside this plan; do not mark all of H13 complete |

### Required evidence still missing or weaker than the plan

- Phase 3 requires frames above 10 MiB in both directions through the real server and real adapter, plus transformed metadata at the 65 MiB boundary and continued service for a second client after a size error.
  `TestSDKAcceptsLeaderLineLimit` proves the SDK parser only; codec tests and scripted router tests prove different boundaries.
  No current app integration test joins those proofs across the full shared path.
- Phase 3 asks for controlled races of two takes and take against every mutation.
  The existing stale-generation tests send an already stale call after take, which cannot detect a mutation that passed its first check before take.
- The final roadmap coverage map asks for a driver-loss question fixture together with a real host run.
  Current question cancellation evidence uses the scripted agent, and the real-host run-survival evidence uses a separate scenario.
  The transport fixture is an accepted substitute for missing product tool owners; it still does not prove the combined question-and-run lifecycle required by that row.
- Phase 4 requires a stable process target for PID fallback, with a verified Linux process handle where supported and refusal when a safe target cannot be established.
  `Stop` uses `processInfo` followed by `syscall.Kill(pid)` on both platforms.
  It does not use a Linux pidfd or verify identity changes between the check and signal.
  The macOS check-to-signal limitation is disclosed in the phase record, but this disclosure does not prove the stronger safety requirement.
  Resolve that contract explicitly before claiming the reused-PID gate is complete.
- Phase 1 explicitly records that private directory descriptor operations were not implemented.
  This review does not claim a cross-user exploit from that omission.
  Record an accepted change to the requirement or implement it; an implementation note alone does not make the original requirement pass.

### Documentation corrections needed

The plan header says completed, but its opening text still says the leader has no implementation or tests.
Its runtime table still says PLANNED, and its final coverage-map preface says no row has passed.
The Phase 3 review still describes the old flag-first idle check, although the current implementation checks first under the admission write lock.
These records need one consistent final state after the remaining code and test gates close.
Do not change the accepted queue, driver-loss take, UID-test or version decisions to hide these failures.

## Verification executed

All commands ran against the current worktree on macOS arm64.
No Linux container run or literal second-user connection was executed in this review.

| Command | Result |
|---|---|
| `go test -race ./internal/leader ./internal/acp ./internal/app ./pkg/protocol ./cmd/tui -count=1` | FAIL: four library packages pass; cmd/tui fails with the SIGTERM test data race |
| `go test -race ./cmd/tui -run '^TestLeaderE2EForegroundLeaderSIGTERM$' -count=3` | FAIL: the same buffer race reproduces |
| `go test ./... -count=1 -timeout=10m` | PASS; cmd/tui completes in 82.117 s |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | PASS, 0 issues |
| `git diff --check` and scoped `gofmt -l` | PASS, no output |
| Added overlay checks | FAIL as expected: six assertions expose the six implementation defects described above |

Logs and runnable regression inputs are in [h13b-review-artifacts](h13b-review-artifacts/).
The Go inputs have a `.go.txt` suffix so they do not change normal package discovery or the production build.
The overlay adds virtual test files to the four owning packages and does not modify their source files.
Run the check from this worktree root:

```sh
go test -overlay=plans/reports/h13b-review-artifacts/overlay.json \
  ./internal/acp ./internal/leader ./internal/app ./cmd/tui \
  -run '^TestReview' -count=1 -v
```

The overlay records absolute paths for this reviewed worktree.
Update those paths if the worktree moves.
The added checks use the existing controlled fixtures; they do not add a product test flag or call the maintainer's leader.
The source fixtures are supplemental evidence and are labelled as such above.

## Recommended completion order

1. Repair generation commit checks, Follow registration, pending-take detach fencing and the forced command exit.
2. Repair socket unlink ownership, auth admission, parallel auth cleanup and the test race.
3. Add the missing shared-link size and lifecycle race evidence.
4. Resolve the stable PID-target and directory-descriptor requirement gaps.
5. Re-run all exit gates, then reconcile the plan and roadmap completion records.

## Unresolved questions

- Is the disclosed macOS signal race an accepted change to the original fail-closed requirement, or must fallback be refused when no stable process target is available?
- Was the private directory descriptor requirement formally removed, or is it still required before H13b completion?
