---
phase: 6
title: "Reverse requests, shutdown, regressions, docs"
status: pending
priority: P1
effort: "1d"
dependencies: [2, 3, 5]
---

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
