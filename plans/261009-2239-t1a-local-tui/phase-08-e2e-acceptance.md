---
phase: 8
title: "E2E acceptance and docs"
status: in-progress
priority: P1
effort: "2d"
dependencies: [7]
---

# Phase 8: E2E acceptance and docs

Automated acceptance and the real-provider run are recorded below.
Native terminal acceptance remains open.

## Goal

Prove all 22 design acceptance criteria on the built `ask`, with the real leader and real providers, and leave the docs, roadmap and the related E2E plan in a consistent state.

## Scope

- Retarget the runner of the [TUI E2E automation plan](../261007-1104-tui-e2e-automation/plan.md) (`e2e/tui/run.py`, tui-test `0.1.0-beta.2`) from the T0 archive to the built `ask`: isolated `ASK_HOME`, leader started by the TUI, owned sessions and processes cleaned up. Keep the T0 gate scenarios in that plan unchanged.
- The runner drives the T0 build directly, so T1a needs a new driver for the built `ask` in the same runner, not a configuration change.
- Secrets: provider keys reach `ask` only through the inherited environment, never through command arguments or `--env`; the runner's `commands.json` records environment variable names, not values. Check the artifacts for key values before keeping them.
- tui-test scenarios cover what needs a real terminal engine (resize while streaming, criterion 22); the [criterion to test map](plan.md#criterion-to-test-map) in plan.md owns the rest.
- Real-provider acceptance: one API-key provider and one subscription provider; repeated prompts, cancel then new prompt, visible retry, provider error, model switch across providers (roadmap T1a test list).
- Manual Terminal.app or iTerm2 run of S3, S6, S7, S11 and resize; record date, terminal, and result.
- Docs: `CLAUDE.md` commands (`ask` opens the TUI), `internal/tui/README.md` status and owners, `docs/tui-architecture.md` validation section, `docs/testing-llm-cassettes.md` if a TUI cassette is added.
- Links: roadmap T1a row points to this plan; the E2E automation plan gets a line that phase 8 here reuses and extends its runner; record T-LAY-09, T-MD-10, T-CMD-08 as T1b in the roadmap T1b list.
- Update the `ask-tui-design` skill with the verified gallery and golden steps (they become defined here).

## Verification

`python3 e2e/tui/run.py`, `go test ./...`, `go vet ./...`, `golangci-lint run ./...`, the criterion table complete with evidence paths.

## Runner and docs result

Terminal-runner and docs part, done 2026-10-10. The Go end-to-end tests, the real-provider run, the manual terminal run and the criterion table are not part of this section.

- **Runner.** `e2e/tui/run.py` has a second driver for the built `ask` (`--driver ask`). The T0 driver is the same code moved into `run_t0`; its 18 cases pass with the same assertions. Default run: both drivers. Details and the secret rules are in [e2e/tui/README.md](../../e2e/tui/README.md#built-ask-driver).
- **Cases (Alacritty and Ghostty, faux provider, leader started by the TUI).** `ask-idle` (80×24, S3 structure), `ask-selector` (80×24, S11 structure, draft kept), `ask-resize` (criterion 22: 120×40 to 80×24 while a reply streams; draft and cursor kept; one purge and rebuild; header, prompt and reply once and in order). The checks are structural: the sketches use `~/src/askcore` and a real model, so exact rows stay with the golden tests in `internal/tui`.
- **Result.** `python3 e2e/tui/run.py`: 24 of 24 cases pass (18 T0, 6 `ask`) and the process exits 0. The product-file hash check now ignores tests, testdata and markdown, because editing them does not change the binary under test. Evidence of the `ask` driver: `e2e/tui/artifacts/` (not tracked by git).
- **Secrets.** Keys reach `ask` only through the inherited environment. `commands.json` records `environment_names`; no `--env` is used for the `ask` driver. The artifact scan was proven with a made-up variable (the run failed, the files were removed, only the variable name was printed). Controls: `python3 e2e/tui/test-assertions.py` (13 tests pass).
- **Cleanup.** Each case stops its own leader with `ask leader stop` and checks the process table for leaders of the run's own binary path. None were left. Leader 94970 and the other leaders from before the run were not touched.
- **Docs.** `CLAUDE.md` (commands), `internal/tui/README.md` (status and owners), `docs/tui-architecture.md` (validation), `e2e/tui/README.md`, and `.claude/skills/ask-tui-design/SKILL.md` (gallery and golden steps). The roadmap T1b list now records T-LAY-09, T-MD-10 and T-CMD-08; the T1a roadmap row and the E2E automation plan already linked to this plan. `docs/testing-llm-cassettes.md` is unchanged because no TUI cassette was added.
- **Resolved observations.** Duplicate model rows are handled by `selector.go` and `selector_labels_test.go`.
  The headless guidance now names the configured providers and keeps `faux` as the default.

## Scout result (Go E2E part)

- Go E2E tests live in `cmd/tui/interactive_flow_test.go`. They reuse the PTY helpers of `cmd/tui/interactive_test.go`, the real leader started by the built `ask`, an isolated `ASK_HOME`, faux provider, and `providerFixture` (`cmd/tui/acp_e2e_test.go`) for 429, thinking, and a failing token endpoint. The leader gets the fixture through the environment of `ask`.
- Test environment: `interactiveEnv` now removes credential variables of the developer shell. `providerFixture` has a new `retryAfter` field (Retry-After header of a 429).
- Frozen leader: some tests send SIGSTOP to the leader they started (for `Sending…` and `Cancelling…`) and resume it; the cleanup always resumes it.
- Limit: the echo tool returns at once, so the `◇ echo … running` line lasts less than one frame. The E2E test checks the committed `● echo … ✓` line; the running state stays with the golden test `TestBlockGoldens`.
- Wire fact: a refused prompt shows the kind `not_driver` (the router sends `ACPErrNotDriver`). The constant `LeaderErrNotDriver` ("leader_not_driver") is not used on the wire. The design text `leader_not_driver` in S5b and criterion 4 differs from the product. The test checks `✗ Not sent: not_driver · draft kept · /take to drive`.
- Retry count: the retry policy is 5 retries, so the status shows `Retrying (1/5)`, not `(1/3)`.
- Criterion 12: the test moves the home away before it stops the leader (SIGTERM), so reconnect attempts fail and no new leader starts while the screen is checked; the home is restored after.
- Criterion 8: login by the real `ask auth login` with the fixture token service; each refresh gives a 60 s token (inside the 10 minute lead), the refresh succeeds for the model selection and is refused after.

### Product edits made while testing

| File | Change | Why |
|---|---|---|
| `internal/tui/translate_errors.go`, `translate.go`, `update_session.go` | `retryCause` turns `HTTP 429: <body>` into `rate limit (HTTP 429)` | The retry note and the status row showed the raw provider body (three rows of JSON) |
| `internal/tui/update_session.go` (`onSnapshot`) | Read the state again after the first snapshot of a newly followed session | A snapshot reset the run state, so an observer that joined a running session had no `Working…` status |
| `internal/tui/selector.go` | Model rows with the same provider and id on two wire formats get `(<api>)` in the label; rows with the same model id are listed once | `alibaba-token-plan/deepseek-v4.1-flash` showed twice with the same label (catalog has an Anthropic-messages and an OpenAI-completions row) |
| new unit tests | `internal/tui/selector_labels_test.go`, `internal/tui/retry_cause_test.go` | Failing first, then fixed |

### Evidence

| # | Criterion | Test | File | Result |
|---|---|---|---|---|
| 1 | Starting leader, header, editor | `TestInteractiveStartsLeaderAndShowsEditor` | `cmd/tui/interactive_test.go` | pass |
| 2 | Leader unavailable, in-process | `TestInteractiveFallsBackInProcess` | `cmd/tui/interactive_test.go` | pass |
| 3 | Sending, accepted, streaming | `TestInteractivePromptStreams` | `cmd/tui/interactive_flow_test.go` | pass |
| 4 | Refused prompt keeps draft | `TestInteractiveRefusedPromptKeepsDraft` | `cmd/tui/interactive_flow_test.go` | pass |
| 5 | Tool line | `TestInteractiveEchoToolLine` (committed line only) | `cmd/tui/interactive_flow_test.go` | pass |
| 6 | Steer, follow-up, alt+up | `TestInteractiveQueueRoundTrip` | `cmd/tui/interactive_flow_test.go` | pass |
| 7 | 429 retry | `TestInteractiveRetryVisible` | `cmd/tui/interactive_flow_test.go` | pass |
| 8 | Auth error block | `TestInteractiveAuthErrorBlock` | `cmd/tui/interactive_flow_test.go` | pass |
| 9 | Cancel marker after settle | `TestInteractiveCancelThenPrompt` | `cmd/tui/interactive_flow_test.go` | pass |
| 10 | Model switch | `TestInteractiveModelSwitch` | `cmd/tui/interactive_flow_test.go` | pass |
| 12 | Leader down | `TestInteractiveLeaderDownShowsDisconnected` | `cmd/tui/interactive_flow_test.go` | pass |
| 14 | Session gone | `TestInteractiveSessionGone` | `cmd/tui/interactive_flow_test.go` | pass |
| 15 | Observer | `TestInteractiveSecondClientObserves` | `cmd/tui/interactive_flow_test.go` | pass |
| 16 | `/sessions` attach | `TestInteractiveSessionsAttach` | `cmd/tui/interactive_flow_test.go` | pass |
| 17 | ctrl+d detaches | `TestInteractiveQuitDetachesRunContinues` | `cmd/tui/interactive_test.go` | pass |
| 18 | Thinking toggle | `TestInteractiveThinkingToggle` | `cmd/tui/interactive_flow_test.go` | pass |
| 21 | Terminal restored | `TestInteractiveSignalsRestoreTerminal`, `TestInteractiveErrorExitRestores` | `cmd/tui/interactive_test.go` | pass |

Criteria 11, 13, 19, 20, 22 stay with their phase 2, 3, 6 tests and the tui-test resize scenario. Not done in this part: tui-test runner, real-provider run, manual terminal run, docs.

## Real-provider result (2026-10-10)

Report: [real-provider-acceptance](../reports/real-provider-acceptance-261010-t1a-tui.md). 21 model calls: OpenAI API key (gpt-5.5, 14 calls and 1 call with a dummy key), ChatGPT OAuth (gpt-5.6-sol, 2), Anthropic OAuth (claude-sonnet-4-6, 4). Scenarios that passed: three prompts in one session at 80x24 and 120x40, cancel then new prompt, invalid-key error block then good prompt, model switch ChatGPT OAuth to Anthropic OAuth and back, thinking toggle, ctrl+d during a run and `/sessions` observer. The visible 429 retry is covered by `TestInteractiveRetryVisible` because a real provider cannot be forced to answer 429.

Defects from that run:
- Error block printed the raw provider body (including masked key text): fixed; the block shows one sentence with the HTTP status (`internal/tui/error_text_test.go`).
- Empty `∴ Thinking...` row for an empty reasoning summary: fixed (`translate_test.go`).
- `openai/gpt-5.5` is listed under a ChatGPT sign-in and then rejected with "not signed in": cause is the per-model access check of the ChatGPT method (`internal/auth/discovery.go:80-82`); the misleading message is fixed with `ACPErrModelAccess` and `TestSelectorModelAccessFailureKeepsModel`.
- Demo `echo` tool is offered to real models (`cmd/tui/headless.go:211`) and the Anthropic reply meta line has no cost (`metaText` prints cost only when above zero). Left for the tools and usage work.

## Open items at the end of this phase

- Manual Terminal.app or iTerm2 run of S3, S6, S7, S11 and resize: not done (needs a person at a terminal); record date, terminal, result here.
- The wire kind `not_driver`, retry count `(1/5)`, and model-access wording are decided in design section 11.
- The right-margin renderer patch is present in the current workspace; native terminal acceptance of it remains open.
- Early prompt acceptance uses an uncorrelated `AgentStart`.
  A correlated refusal now restores the draft, including subsequent edits, but the draft can disappear briefly before that refusal arrives.
  This does not prove the strict continuous draft-retention contract under concurrent clients.
- Follow-up audit and regression evidence: [origin-purpose audit](../reports/audit-261010-1334-t1a-origin-purpose.md).
