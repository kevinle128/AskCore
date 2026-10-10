---
title: "T1a local TUI"
description: "Build the inline Ask TUI that tests the completed harness through the local leader over ACP, as specified by the approved design."
status: in-progress
priority: P1
effort: "9-12d"
branch: new-session
tags: [tui, bubbletea, acp, leader, tdd]
blockedBy: []
blocks: []
created: 2026-10-09
mode: deep
tdd: true
---

# T1a local TUI

## Outcome

`ask` with no arguments opens the inline TUI from the approved [design](design.md) and talks to the local leader through `ConnectOrSpawn`, the Unix socket and ACP.
The completed H4, H7a and lifecycle behavior works from the terminal ([roadmap T1a exit](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core)).
All 22 acceptance criteria in design section 10 pass, with a built `ask`, its real socket, and real providers for the final check.

## Authority and links

| Link | Role |
|---|---|
| [design.md](design.md) | Approved UI contract (2026-10-09). Any visible change sends it back to `draft` and needs a new approval ([skill](../../.claude/skills/ask-tui-design/SKILL.md)). |
| [artifacts/preview.html](artifacts/preview.html) | Visual form of the same sketches. |
| [docs/tui-style-guide.md](../../docs/tui-style-guide.md) | Visual authority: roles, palette (truecolor, 256, no color), glyphs, block patterns. Direction B4 (user, 2026-10-10). `theme.go` in phase 3 implements its palette. |
| [Roadmap T1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core) | Parent scope. T1a and T1b together own all T1 P0 rows. |
| [TUI E2E automation](../261007-1104-tui-e2e-automation/plan.md) | Related plan. Its runner `e2e/tui/run.py` and pinned tui-test `0.1.0-beta.2` are reused in phase 8, retargeted from the T0 archive to the built `ask`. Its own open items (G3 resize strict failures, iTerm2) stay in that plan. |
| [Resize fix](../261007-1156-inline-resize-fix/plan.md) | Source of the Bubble Tea renderer patch used in phase 1. |
| [D14 decision](../reports/pm-261007-1312-d14-tui-decision.md) | Bubble Tea v2.0.10 with the local patch; exact Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7`. |
| [docs/tui-architecture.md](../../docs/tui-architecture.md) | State ownership, input order, inline transcript rules. |
| [Research: test and render libraries](research/researcher-01-test-and-render-libs.md) | Version compatibility evidence. |

## Decisions in this plan (user, 2026-10-09)

1. Libraries: `charm.land/lipgloss/v2` **v2.0.5** (pinned; v2.0.6 raises the Ultraviolet pin), `charm.land/glamour/v2` v2.0.1, `github.com/alecthomas/chroma/v2`. Streaming markdown uses an own stable-prefix cache (Crush is FSL: read only).
2. T-LAY-09 (tool output expand), T-MD-10 (tool output truncation display) and T-CMD-08 (`/copy`) move to T1b with H5 builtin tools. The approved design stays unchanged.
3. Design decisions are recorded in design section 11 (Pi keys, Pi thinking toggle, Grok-style reconnect, five slash commands, sketch sizes, Grok-style startup fallback).
4. design.md was approved again on 2026-10-10 after the startup fallback change and the B4 visual direction.

## Constraints

- TDD: each phase writes its failing tests first, then code, then runs the phase verification.
- `internal/tui` imports only `pkg/protocol`, `pkg/askui`, `internal/tuiext` and terminal UI libraries (`.golangci.yml` tui-client-boundary). The client implementation lives outside it and is injected.
- Startup fallback (user, 2026-10-10, as Grok): when the leader connect fails at startup, `ask` runs an in-process agent and the footer shows `in-process`. After a successful start, a lost leader is reconnected, never replaced. Every leader-path acceptance test asserts the leader id in the footer, so a fallback cannot pass a leader test.
- Tests run the real code path: the built `ask`, the real leader and socket; the faux provider and cassettes replace only the model service. No test-only harness bypasses the `ask` entry point.
- No plan ids, phase numbers or finding codes in code, test names or commit messages.
- `ask connect` stays as a debugging client; it is not removed in T1a.
- Unix only (D8); inline only (D15).

## Phases

Deep mode: phase 1 is detailed. Each later phase gets a dedicated scout pass and detail update before it is cooked.

| # | Phase | Status | Depends on | Effort |
|---|---|---|---|---|
| 1 | [Root Bubble Tea v2 migration and dependencies](phase-01-root-migration.md) | completed | — | 1d |
| 2 | [Leader client for the TUI](phase-02-leader-client.md) | completed | 1 | 1.5d |
| 3 | [Inline render core and layout](phase-03-render-core.md) | completed | 1 | 1.5d |
| 4 | [Editor](phase-04-editor.md) | completed | 3 | 1.5d |
| 5 | [Transcript content and markdown streaming](phase-05-transcript.md) | completed | 3 | 1.5d |
| 6 | [Session view: status, queue, footer, selectors, connection states](phase-06-session-view.md) | completed | 2, 4, 5 | 1.5d |
| 7 | [Terminal lifecycle and `ask` entry](phase-07-terminal-lifecycle.md) | completed | 6 | 1d |
| 8 | [E2E acceptance and docs](phase-08-e2e-acceptance.md) | in-progress | 7 | 2d |

Phases 2 and 3 can run in parallel with this file ownership: phase 2 owns `internal/tui/client.go` (the consumer interface and its message types), `internal/tuiclient/**`, `cmd/tui/connect.go` and the depguard rules; phase 3 owns every other file in `internal/tui`. Phase 3 does not edit `client.go`.

## Acceptance

- Every design section 10 criterion has a named automated test (component golden, Go E2E against a real leader, or tui-test scenario), listed in phase 8.
- `go test ./...`, `go vet ./...` and `golangci-lint run ./...` pass.
- One real-provider run (API key or subscription) and one manual Terminal.app or iTerm2 run are recorded in phase 8 evidence.
- Roadmap T1a row links to this plan; `CLAUDE.md`, `internal/tui/README.md` and `docs/tui-architecture.md` describe the implemented state.

## Criterion to test map

Each design section 10 criterion has an owning test. "Go E2E" tests run the built `ask` in a PTY against a real leader started by it, with an isolated `ASK_HOME`; model replies come from the faux provider or from the provider fixture in `cmd/tui/acp_e2e_test.go:1460-1550`, which the started leader inherits through its environment (no test-only hook). "Golden" tests render the model into an `x/vt` emulator.

| # | Criterion (short) | Owning test | Phase |
|---|---|---|---|
| 1 | Starting leader, then header and editor | `TestInteractiveStartsLeaderAndShowsEditor` (Go E2E) | 7 |
| 2 | Leader unavailable: in-process agent answers | `TestClientInProcessAgent` + `TestInteractiveFallsBackInProcess` (Go E2E, unsafe socket) | 2, 7 |
| 3 | Sending, accepted, streaming | `TestClientPromptAcceptanceSignal` + `TestSendClearsDraftOnlyAfterAcceptance` + `TestInteractivePromptStreams` (Go E2E) | 2, 6, 8 |
| 4 | Refused prompt keeps draft | `TestClientRefusedPromptIsSendFailure` + `TestInteractiveRefusedPromptKeepsDraft` (Go E2E, observer client sends) | 2, 8 |
| 5 | Tool line running then committed | `TestBlockGoldens` + `TestInteractiveEchoToolLine` (Go E2E, faux `echo`) | 5, 8 |
| 6 | Steer, follow-up, alt+up | `TestAltUpDequeuesIntoDraft` + `TestInteractiveQueueRoundTrip` (Go E2E) | 6, 8 |
| 7 | 429 retry status and note | `TestInteractiveRetryVisible` (Go E2E, provider fixture answers 429 then 200) | 8 |
| 8 | Expired token error block | `TestInteractiveAuthErrorBlock` (Go E2E, provider fixture token endpoint fails) | 8 |
| 9 | Cancel marker after settle | `TestCancelMarkerAtSettleOnly` + `TestInteractiveCancelThenPrompt` (Go E2E) | 5, 8 |
| 10 | Model selector changes footer | `TestSelectorChangesModel` (golden) + `TestInteractiveModelSwitch` (Go E2E) | 6, 8 |
| 11 | Readiness failure keeps model | `TestClientSetModelReadinessFailure` + `TestSelectorReadinessFailureKeepsModel` | 2, 6 |
| 12 | Disconnected state while leader is down | `TestInteractiveLeaderDownShowsDisconnected` (Go E2E: stop the leader and keep it stopped while asserting; restart only after) | 8 |
| 13 | Socket-only drop restores session | `TestClientFollowResumeAfterSocketDrop` (real leader and socket; the test closes the client connection while the leader stays up) + `TestReconnectStatesGoldens` for S13a. Accepted as the evidence for this criterion (user, 2026-10-10); no leader control is added for tests | 2, 6 |
| 14 | Leader restart: session gone | `TestClientSessionGoneAfterLeaderRestart` + `TestInteractiveSessionGone` (Go E2E) | 2, 8 |
| 15 | Observer footer and editor text | `TestInteractiveSecondClientObserves` (Go E2E) | 8 |
| 16 | `/sessions` attaches as observer | `TestInteractiveSessionsAttach` (Go E2E) | 8 |
| 17 | ctrl+d detaches, run continues | `TestInteractiveQuitDetachesRunContinues` (Go E2E) | 7 |
| 18 | ctrl+t hides and shows thinking | `TestThinkingToggleRebuild` (golden) + `TestInteractiveThinkingToggle` (Go E2E, provider fixture streams thinking) | 5, 8 |
| 19 | 40×14 wide characters and short footer | `TestWidthWrapNeverExceedsColumns` + `TestFooterUsageThresholds` (golden at 40×14) | 3, 6 |
| 20 | 120×40 sketches S3, S6, S7, S11 | `TestScreens120Goldens` (golden) | 6 |
| 21 | Terminal restored on every exit path | `TestInteractiveSignalsRestoreTerminal` + `TestInteractiveErrorExitRestores` (Go E2E) | 7 |
| 22 | Resize while streaming | `TestResizeRebuildKeepsDraftAndFocus` (golden) + tui-test resize scenario | 3, 8 |

## Risks

| Risk | Mitigation |
|---|---|
| Prompt acceptance is not a separate ACP signal: `session/prompt` returns at settle; only steer and follow-up return an input id | Phase 2 proves which event marks acceptance (run start or the user message event) with a test before the S5a state is built |
| A prompt error is the same request error whether the leader refused the prompt or accepted it and the run failed (`internal/acp/agent.go:345-358`) | The client decides by the acceptance event: an error before acceptance is a send failure (S5b); after acceptance it is a run error (S9) |
| Bubble Tea handles SIGINT/SIGTERM itself and SIGTERM would exit 0; SIGHUP has no handler | `tea.WithoutSignalHandler()`; `ask` handles the three signals and restores the terminal (phase 7) |
| The renderer patch was verified headless only; Ghostty 52×15 keeps 1,096 of 2,000 lines | Keep the patch guard test; record the limit; manual terminal check in phase 8 |
| A later `go get` raises lipgloss or Ultraviolet | Pin test in phase 1 fails when the versions in the build differ from the pins |
| glamour renders partial markdown badly while streaming | Stable-prefix cache with tests for open fences, lists and tables (phase 5) |
| Reconnect to a restarted leader loses the session (no H8 storage) | Designed state S13b; test with a leader restart |

## Red-team questions (answered)

The [red-team report](reports/red-team-261009-t1a-plan.md) raised two questions:

1. Roadmap in-process fallback wording: decided (user, 2026-10-10) as Grok behavior; roadmap and design updated.
2. Evidence for criterion 13: the client-level test against a real leader is accepted (user, 2026-10-10).

## Decisions after the real-provider run (user, 2026-10-10)

- Refused prompt kind text is `not_driver`; retry sketch is `(1/5)`; footer cost keeps the sketch format (two decimals in the footer, three in the meta line); the quit and detach notes leave out "the run continues" when no run is active; the `/sessions` list has no `watching` column; a model that the ChatGPT sign-in cannot use gets its own error kind and text; the in-process client keeps the ACP host over an in-memory pipe; the demo `echo` tool and the missing Anthropic cost stay for the tools and usage work.
- The right-margin renderer patch in `third_party/bubbletea` is done together with the manual terminal check, not before.

## Unresolved questions

1. Manual Terminal.app or iTerm2 run of S3, S6, S7, S11 and resize is not done (needs a person at a terminal). Record date, terminal and result in [phase 8](phase-08-e2e-acceptance.md).
2. Right-margin renderer patch (see above) waits for that run.
