---
phase: 6
title: "Reverse requests, shutdown, regressions, docs"
status: completed
priority: P1
effort: "1d"
dependencies: [2, 3, 5]
---


## Current repair state (2026-10-09)

[module_leader.go](../../internal/app/module_leader.go) starts native auth cleanup in parallel with Agent drain after the router and link close.
Graceful stop joins both operations.
[leader.go](../../cmd/tui/leader.go) keeps startup-failure cleanup but lets a second signal return without joining the active runtime drain again.
The forced path reports incomplete cleanup and the signal exit code.
[leader_lifecycle_test.go](../../internal/app/leader_lifecycle_test.go) checks parallel cleanup and driver-loss question cancellation while a real host run remains active.
A late answer from the old generation cannot resolve a new generation question.
The question is issued through the real SDK connection with a controlled fixture; no product tool owns this reverse call yet.
[leader_regression_test.go](../../cmd/tui/leader_regression_test.go) checks the complete command lifecycle with a held started tool and two signals.
The focused app race checks passed in 36.182 s.
The final exit gates passed; the [repair report](../reports/pm-261009-h13b-repairs.md) records their evidence and accepted limits.

# Phase 6: Reverse requests, shutdown, regressions, docs

Outline.
Scout at cook time.

## Goal

Eligible reverse-request routing with first-valid-answer completion and driver-loss cancellation, ordered leader shutdown, full regression of editor and headless modes, and docs that match the shipped behavior.

## Files to Create / Modify (candidate)

- Modify: `internal/leader/router.go` (pending reverse table), `server.go` (shutdown order)
- Create: reverse-request tests with a disclosed transport peer fixture
- Modify: `internal/app/module_leader.go` (stop order), tests
- Modify docs: `internal/leader/README.md`, `internal/acp/README.md`, `internal/app/README.md`, `pkg/protocol/README.md`, `docs/ask-architecture-reference.md` 7.3, `CLAUDE.md` commands, roadmap H13b status

## Contracts

- Reverse table: reverse id, session, driver generation, method, eligible clients, state.
  - `fs/*`, `terminal/*`: driver only, and only with the needed capability; no eligible driver gives a safe error to the agent.
  - `session/request_permission` and question-type requests: all eligible subscribers; first valid answer completes once; ineligible senders rejected; late or duplicate answers ignored. **Eligible** means: currently subscribed, initialized, and advertising the capability the request needs.
  A client that attaches while a shared question is pending is added to the eligible set atomically with replay.
  Permission handling is a core ACP client method; do not invent a standard permission capability that the SDK schema does not have.
  Observers are eligible on purpose: architecture 7.3 item 4 and the roadmap rule "driver, or all subscribers for shared questions" accept that any subscriber may answer a shared question.
  This is not a driver-only mutation.
  - Agent-to-client `$/cancel_request` (the SDK sends it when the adapter cancels its own outbound request): rewrite `requestId` through the reverse table, deliver it to every client the request went to, and close the pending entry.
  - Driver loss during any pending question for that driver generation, including a shared question: cancel it (error response to the agent), no later answer accepted, run not aborted.
  - Generation check stops a former driver's answer completing a new question.
- Shutdown: close admission, notify clients, close blocked socket writes, dispose owned Agents (started tool bodies drain), drain native auth cleanup, remove owned socket, clear PID, release flock.
  A second signal reports incomplete cleanup.
- Docs impact: major.
  Write only what exists; do not claim H13c behavior.

## TDD (RED list)

- `TestReverseDriverOnlyWithCapability`, `TestReverseNoEligibleDriverSafeError`
- `TestReverseFirstAnswerWins`, `TestReverseIneligibleSenderRejected`, `TestReverseLateAnswerIgnored`
- `TestReverseDriverLossCancelsQuestion` (run continues)
- `TestReverseStaleGenerationAnswerRejected`
- `TestReverseAgentCancelFannedOutAndRewritten`
- `TestLeaderShutdownOrder`, `TestLeaderSecondSignalReportsIncomplete`
- `TestHeadlessNoLeader` (no socket, no leader process after `ask -p`), existing `TestACPE2ENoListener`, all H13a E2E

## Verification

```sh
go build ./... && go vet ./...
go test -race ./internal/leader/... ./internal/acp/... ./internal/app/... ./cmd/tui/... -count=1
go test ./... -count=1 -timeout=10m
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

- Reverse fixture mistaken for product tool E2E: label tests and plan rows as transport fixture.

## Reverse lifecycle and shutdown evidence

Give every recipient a router-generated reverse id and keep the original agent id only in the reverse table.
Validate the response shape and selected permission option before committing the first answer.
An invalid answer cannot resolve the question; a detach removes that client from the eligible set.
Resolution, replay-on-attach, cancellation and driver loss run through the same router transition.
Cancel other recipients when a valid answer wins and clear the table once.
A duplicate answer or cancellation cannot be delivered to a later question with a reused agent id.
Do not create a second independent cache for replay.

For terminal calls, retain the owning client and driver generation for each terminal handle.
A new driver cannot use or release an old driver's terminal id on another client.
Driver loss must end or fail those resources safely without cancelling the shared Agent run.
Use a disclosed transport fixture until the product owner of these tools exists.

This phase owns `TestLeaderE2EAttachSeesPendingQuestion` and `TestLeaderE2EVersionMismatchWhileToolRuns`; Phase 5 lists them as final exit requirements only.
The skew test must use a real host with a controlled started tool and a skewed connecting peer, not a fake agent stream as proof that the tool survives.
Add tests for replay racing first answer, invalid permission option then valid answer, detach during question, driver loss during a shared question, reverse-id reuse, terminal ownership after take, and close with a blocked client writer.

Close admission and cancel pending reverse calls before disposing Agents, so disposal cannot wait forever for a detached client answer.
Native auth cleanup runs in parallel with Agent drain as in editor stdio.
Stop must close every owned pipe/socket and join its reader, writer and liveness goroutines.
Artifact cleanup checks instance and inode while the flock is still held.
On a second signal, report incomplete cleanup and return the signal exit code; never claim a drained host.
Update the existing command guide and `AGENTS.md` or its owning source only after behavior exists; do not change auto-generated files.

## Historical review (2026-10-08)

### What was built

- **`internal/leader/reverse.go`** (moved out of `router.go`, reworked). The agent's calls to clients go through one table. Each recipient gets an id that the router made and never uses twice; the agent's own id stays in the table, so an answer can never reach a later question that reuses an agent id. An answer counts only from the client that got the id, for an open call. A permission answer must select an offered option or say cancelled; a result with an error, an unknown outcome, a missing outcome or both result and error does not resolve. When a valid answer wins, the other recipients get `$/cancel_request` with their own ids. Late, repeated, foreign, invalid and detached answers are ignored. Replay on attach, cancel, driver loss and take share the same transitions, and there is no second cache.
- **Terminals.** A terminal belongs to the client that made it (`terminal/create` answer), in the driver generation of that call. Later calls about it go to that client only. A take, a detach or a loss of the driver ends the terminals of that generation, so a new driver cannot use or release an old terminal and the agent gets a safe error. Releasing a terminal removes it.
- **Shutdown.** `runUntilSignal` in `cmd/tui/leader.go` carries the signal rules: the first signal cancels the context so the stop runs in order; a second signal reports "cleanup did not drain; forced exit" with the exit code of the signal and never success. `LeaderRuntime.Stop` and the command's deferred cleanup give this order: router stops and tells the clients, link closes, host disposes and waits for started tool bodies, credential refresh drains, the owned socket is removed (only if it is still ours), the owner record is cleared and the lock is released.
- **Docs** (impact: major): `internal/leader/README.md` (rewritten), `internal/acp/README.md` (route context, SDK copy, leader link), `internal/app/README.md`, `pkg/protocol/README.md`, architecture section 7.3, README, CLAUDE.md, AGENTS.md, roadmap revision 17 and the D17 note, and `docs/adr/0004-leader-drivers-output-and-sdk-copy.md`.

### Evidence

| Check | Result |
|---|---|
| `internal/leader` reverse tests (shared question, first answer wins, per-recipient ids, guessed or foreign ids, invalid answers, replay, eviction, reused agent id, driver loss, detach, take, agent cancel fan-out, files, no-driver errors, terminals) | pass with `-race` |
| `TestRunUntilSignal*` (clean end, error, first signal, second signal) | pass |
| `TestLeaderShutdownOrder` (real host, a tool body that ignores the cancel: the listener closes and the client is disconnected first, `Serve` returns only after the body ends) | pass |
| `TestLeaderE2EForegroundLeaderSIGTERM` (built binary, exit 143, socket removed, lock free, owner cleared) | pass |
| `TestLeaderE2EHeadlessStartsNoLeader`, the existing `TestACPE2ENoListener` and all H13a E2E tests | pass |
| `golangci-lint run ./...`, `go vet ./...` | clean |
| Documentation links (all relative links of the edited files) | no broken link |

### Changes from the outline

- The reverse work was built in Phase 2 and finished here, so Phase 6 is the re-proof against the real parts plus the items above.
- `TestReverse*` names became `TestRouter*` in `router_reverse_test.go`, next to the other router tests.
- No product part issues a reverse call yet, so the reverse tests use a scripted agent (a disclosed transport fixture, not tool E2E). A question in the built binary cannot be produced.

### Known limits

- The reverse paths are proven at the transport; the real owners (file tools, terminal tool, permission policy) do not exist yet.
- The terminal table keeps an entry until a release, a take, a detach or a loss.
