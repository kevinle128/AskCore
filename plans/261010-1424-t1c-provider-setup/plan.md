---
title: "T1c provider setup: /providers, /model and first run"
description: "Add, check and remove providers from the TUI through the local leader, list only usable models in /model, and start with no model when no provider is usable, as the approved design specifies."
status: in_progress
priority: P1
effort: "12-14d"
branch: new-session
tags: [feature, tui, acp, leader, auth, security, tdd]
blockedBy: []
blocks: []
created: 2026-10-10
mode: deep
tdd: true
---

# T1c provider setup: `/providers`, `/model` and first run

## Outcome

In `ask` (leader path), `/providers` lists the seven login options, signs in by API key, browser URL, copy-code or device code, and removes a saved sign-in after a confirmation.
The leader runs every sign-in on its host, checks an API key before it saves it (mode 0600 store), and never logs or echoes a secret.
`/model` lists only the models of usable providers. With no usable provider, a session has no model, the footer shows `no model` and a prompt is not sent.
Every session shows its `<provider>/<model>` on a start line under the header and on a change line when the leader reports a new model.
The tests below cover [design section 10](../261010-1424-t1c-provider-setup-design/design.md#10-acceptance-criteria) with the built `ask`, a real leader, and the faux provider or the provider fixture.
Phase 8 has a recorded real-provider run; a new OAuth sign-in remains unverified.

## Authority and links

| Link | Role |
|---|---|
| [Approved design](../261010-1424-t1c-provider-setup-design/design.md) | **UI authority** (approved by Kevin Le on 2026-10-10, and approved again after the confirmed amendments). Section 10 owns criteria 1-60, section 12 owns the T1a amendments, and section 3 owns the leader contract. A visible change sends it back to `draft` ([ask-tui-design skill](../../.claude/skills/ask-tui-design/SKILL.md)). Read only in this plan. |
| [Design preview](../261010-1424-t1c-provider-setup-design/artifacts/preview.html) | Visual form of the sketches. |
| [Flow analysis](../reports/xia-261010-1416-provider-model-flow.md) | omp, Pi and Grok behavior; dependency matrix; risks. Its claim "default model deepseek" is stale: the leader default is faux (see decision D10). |
| [T1a plan](../261009-2239-t1a-local-tui/plan.md) and [T1a design](../261009-2239-t1a-local-tui/design.md) | Layout and quality model. Section 12 of the new design amends the T1a design and its goldens (phases 6 and 8). |
| [Architecture reference](../../docs/ask-architecture-reference.md), [internal/README.md](../../internal/README.md) | Package model and import rules (depguard in `.golangci.yml`). |
| [TUI architecture](../../docs/tui-architecture.md), [style guide](../../docs/tui-style-guide.md) | Input order, inline rules, roles and glyphs. |
| [Roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md) | T1c delivers a local part of M2 rows H17 (`H-SLASH-10`, `H-AUTH-10` transport) and T2 (login/logout dialogs) early; phase 8 records the overlap. |

## Decisions

User decisions of 2026-10-10. Do not reopen.

| # | Decision |
|---|---|
| U1 | Command `/providers`. Flat list, one row for each login option. Slash command names are case-insensitive. |
| U2 | `/model` lists only usable models; empty list shows `Use /providers to add providers.` Locked providers are hidden in `/model`, visible in `/providers`. |
| U3 | API key in a masked input shown as `(hidden)`, sent once over the owner-only socket to the leader, which checks and stores it (0600). Never logged, never in transcript, events, prompt history or editor undo. |
| U4 | OAuth by browser URL (wrapped rows, ctrl+y, hyperlink, never in scrollback), copy-code, or device code. Removal asks for confirmation. |
| U5 | First-run notice on the live rail and footer only. No default model on first run; footer `no model`. |
| U6 | Start line `  model  <provider>/<model>` under the header and a change line in scrollback. Reply meta line and footer keep the bare model id. |
| U7 | `/providers` and login boxes replace the editor slot and keep the draft. |
| U8 | ACP: keep standard `initialize.authMethods` and `authenticate` for readiness; add `_ask` extension methods (providers list, login start/input/cancel, api-key save, logout) and a providers-changed notification; clients re-fetch models. The leader resets only that provider's cache. Custom endpoints are out of scope. |

Plan decisions (planner, from the code). D10 to D15 were confirmed by the user on 2026-10-10.

| # | Decision | Evidence |
|---|---|---|
| D1 | Method names: `_ask/providers/list`, `_ask/providers/api_key`, `_ask/providers/login/start`, `_ask/providers/login/input`, `_ask/providers/login/wait`, `_ask/providers/login/cancel`, `_ask/providers/logout`; notification `_ask/providers/changed`. `login/wait` is the one addition to U8: it carries the outcome of a browser or device login, which has no input call. A request routes back by id, so the router needs no per-client notification path. The key save carries a client-made login id, so `login/cancel` can stop a key check before the store write; the cancel answer says whether anything was saved. | `router.go:637-679` routes answers by id; `router.go:778-804` routes notifications only by session or follow |
| D2 | Provider methods are served only on the leader link (`RequireRoute`) and only where the leader runtime sets the accounts; `ask acp` and the in-process link answer `unsupported` even with a forged `_meta` route. A gone client cancels its logins: the router sends an internal `_ask/providers/release_client` to the host from `removeClient`. Clients cannot call it (not in the policy table). | `route.go:27-39` (route parsed on every link), `module_leader.go:73-79` (only the leader requires it), `router.go:470-485`, `ids.go:206-218` (host is never told), `router.go:574-585,603-617`, `router.go:250-253` |
| D3 | `providers_changed` is broadcast by the router to every initialized client. | `router.go:802-803` drops unknown agent notifications today |
| D4 | The login state machine (`auth.Broker`) lives in `internal/auth`: one active login or key check per provider (`provider_busy` for a second sign-in); a removal cancels a pending sign-in or key check of that provider (reason `removed`, D15) and removals are serialized per provider; a goroutine per login over `Service.Login` with a context from the broker root (not the request), input by channel, cancel by context. Shutdown closes the broker before the refresh drain. | `login.go:62-113` (10 min, revision read at `:80`, replace at `:111`); `connection.go:415-421` of the SDK copy cancels a request context at handler return; `module_acp.go:101-104` |
| D5 | API keys are checked by a read-only `GET <base>/models` probe per provider before `Store.Replace`. A refusal returns `{httpStatus, providerCode}`; the code comes only from JSON `error.code` or `error.type` matching `^[a-z0-9_]{1,64}$`, never from message text. | `login.go:93-98` stores without a check; `http.go:132-139` hides provider text |
| D6 | Usable status is offline and synchronous: a saved record, or an env key when no saved record exists. It never refreshes a token. | `service.go:124-138` precedence; `service.go:152-194` may refresh (not reused) |
| D7 | `_ask/session/get_available_models` gains a request flag `usableOnly`; a new leader answers with only usable rows and echoes `usableOnly: true`. Old clients and `ask connect` get the full list. A new TUI on an old leader sees no echo and shows all rows. | `ask_methods.go:127-142`, `pkg/protocol/acp.go:191-194` |
| D8 | New agent event `model_update` (provider, id, api; empty when no model) is posted on every model change, as `queue_update` is. It reaches followers through `_ask/session/event`, so the TUI draws a change line only for a leader event with the current epoch and sequence. The start line comes from `_ask/session/state`. | `agent.go:327` (model set), `emit.go:79-87` (`postLocked`), `agent.go:199,246,374` |
| D9 | A session can have no model: `ACPStateResult.ModelID` and `ACPModelsResult.Current` are empty, prompt, continue, steer and follow-up answer the new kind `no_model` before any run starts (`queue.go:180-195` starts a run from an idle enqueue today). The logout result names the caller's cleared model. After a logout, the host clears the model of each live session whose provider became unusable; a busy session clears when its run ends (the run copies its config unlocked at `agent.go:478-482`). | `agent.go:121-141` accepts an empty `Config.Model`; `agent.go:282-284` |
| D10 | **(user, 2026-10-10)** The faux demo provider is opt-in for the leader, `ask acp` and the in-process TUI agent (they share `acpParams`): only with `ASK_FAUX=1` is it listed, usable, and the initial model. Without it a new session starts by the D12 order (saved default, provider default, first usable row, none). `ask -p` keeps faux as its default. | `cmd/tui/args.go:58-59`, `cmd/tui/acp.go:104`, `internal/app/module_acp.go:71-73` prepend faux to every list today, so P1 is unreachable |
| D11 | **(user, 2026-10-10)** Anthropic row: the leader starts the browser flow (callback on the host) and accepts a pasted callback address in the same box; if the callback port is busy it starts the copy-code flow (pasted code). One authorize URL cannot serve both, because the redirect is in the URL. P15 box text needs a small amendment. | `anthropic.go:22-28`, `callback.go:89-94,133-153` (`bare` only when not browser) |
| D12 | **(user, 2026-10-10: option B3, as omp)** Default model. Choosing a model in `/model` switches the session and saves it as the default (leader side, in ASK_HOME settings; no new key). A new session starts with: the saved default if its provider is still usable; else the default model of a usable provider (one default per provider in the catalog); else the first usable model in catalog order; else no model. When the saved default is not usable, the session starts with the next choice and a notice names the model in use. Adding a provider with `/providers` does not set the default. A session-only switch (omp `/switch`) is later work. Visible text is in the re-approved design (P25a, P28c, P29). | omp `selector-controller.ts:922-926`, `config/model-resolver.ts:61-80`; Pi `core/model-resolver.ts:620-710` for contrast |
| D13 | **(user, 2026-10-10)** Criterion 42 and P28 show deepseek without `(anthropic-messages)`, but the compiled catalog always has deepseek on two formats, so P28a applies in the product. Goldens use fixture lists; the real run in phase 8 shows the format. | `catalog.go:17-51`, design `design.md:1731-1734` |
| D14 | **(user, 2026-10-10: option B)** `TEA_TRACE` stays usable. When it is set, the TUI shows a warning that keyboard input, pasted keys included, is written to the trace file: on the key box and the code input (P6, P15) and once at start. The security criteria of the design get this one stated exception. The text is in the re-approved design. | `third_party/bubbletea/tea.go:641-646`, `tty.go:75` |
| D15 | **(user, 2026-10-10: option B)** A removal while another client signs in to the same provider is allowed and wins: the leader cancels that sign-in, and the other client's box shows the sign-in as cancelled with the reason that another client removed the provider. The text is in the re-approved design. | `login.go:80,111,122-136` (a removal changes the revision and fails the sign-in) |
| D16 | **(user, 2026-10-10: option A)** The leader saves an Alibaba Token Plan API key without a live check (no free read-only check is known; the user's Token Plan has expired). The TUI shows `Saved. This provider cannot be checked before use.` The other three providers keep the probe (D5). The design has the variant (P6b, P9a, P9b; criteria 59, 60). | Phase 3 `KeyCheckable`; `internal/providers/api.go:23-24` |

## Constraints

- TDD: each phase writes its failing tests first, then code, then runs its verification commands.
- Boundaries: `internal/tui` imports only `pkg/protocol`, `pkg/askui`, `internal/tuiext` and UI libraries; the client implementation stays in `internal/tuiclient` (`.golangci.yml:28-74`). `internal/acp` never imports `auth` or `app` (`internal/acp/README.md:55`). `internal/leader` never imports `acp` or `agent` (`internal/leader/README.md:36-37`).
- Tests run the real code path: the built `ask`, its real leader and socket; faux and the provider fixture replace only the model and auth services (`cmd/tui/acp_e2e_test.go:1464-1560`). Leader-path E2E tests assert the leader id in the footer.
- No plan ids, phase numbers or finding codes in code, test names or commit messages.
- No secret value in any plan, log, artifact or commit. Tests use fake keys; the real run reads `OPENAI_API_KEY` from the environment only.
- Precondition: the uncommitted T1a work in `internal/tui`, `internal/tuiclient`, `cmd/tui/interactive_flow_test.go` and `third_party/bubbletea` (git status 2026-10-10) is committed before phase 4 starts. Citations into those files are against the working tree of 2026-10-10.
- Unix only, inline only, as T1a.

## Phases

Deep mode: phase 2 is detailed; every later phase starts with a scout pass that re-checks its evidence and updates its file before cooking.

| # | Phase | Status | Depends on | Effort |
|---|---|---|---|---|
| 1 | [TUI design](phase-01-tui-design.md) | completed (approved, then approved again after the D11, D12, D14, D15 amendments, 2026-10-10) and the D16 variant (P6b, P9a, P9b), 2026-10-10 | — | done |
| 2 | [Protocol shapes, model event and no-model contract](phase-02-protocol-and-model-contract.md) | completed | 1 | 1d |
| 3 | [Auth broker, key check, provider status and default-model store](phase-03-auth-broker.md) | completed | 1 | 2.5d |
| 4 | [Leader host and router](phase-04-leader-host-and-router.md) | completed | 2, 3 | 2.5d |
| 5 | [TUI client contract and tuiclient](phase-05-tui-client.md) | completed | 4 | 1d |
| 6 | [TUI screens and design amendments](phase-06-tui-screens.md) | completed | 1, 5 | 3d |
| 7 | [E2E and security acceptance](phase-07-e2e-and-security.md) | completed | 6 | 1.5d |
| 8 | [Real-provider acceptance and docs](phase-08-real-provider-and-docs.md) | in progress: new OAuth sign-in not run | 7 | 1d |

Phases 2 and 3 can run in parallel: phase 2 owns `pkg/protocol/**` and `internal/agent/**`; phase 3 owns `internal/auth/**`, `internal/settings/settings.go` (with its test and README) and `internal/providers/keys.go` and `catalog.go` (disjoint). No other phase pair runs in parallel. A later phase may edit a file that an earlier phase changed; each phase file lists the files it edits. Shared across sequential phases: `internal/tui/session_rig_test.go` (5, 6), `cmd/tui/tui_client_e2e_test.go` (4, 5), `cmd/tui/acp_e2e_test.go` (4, 7), `e2e/tui/run.py` (4, 7), `cmd/tui/interactive_flow_test.go` (7 only, after the T1a work is committed).

## Acceptance

- Every design criterion 1-60 has a named passing test (table below); security criteria 36-43 are checked without `TEA_TRACE` (stated exception).
- `go test ./...`, `go vet ./...` and `golangci-lint run ./...` pass; `python3 e2e/tui/run.py` passes with the new cases.
- `ask connect` E2E tests pass unchanged (old client on the new leader).
- Real-provider run (phase 8): OpenAI API key saved, refused and removed; one OAuth sign-in from a copy of the T1a OAuth home; the artifact scan finds no key value.
- Roadmap, `CLAUDE.md`, `internal/tui/README.md`, `docs/tui-architecture.md`, the skill gallery and the T1a design amendments describe the implemented state.

## Criterion to test map

Synced on 2026-10-10 with the final criteria 1-60 (59 and 60 added for D16; 1-58 unchanged) of the [re-approved design](../261010-1424-t1c-provider-setup-design/design.md#10-acceptance-criteria) (it adds P6a, P12c, P15a, P25a, P28b, P28c and renumbers from 21 on). Security criteria 36-43 hold when `TEA_TRACE` is not set: that is the design's stated exception (D14), so the security tests run without `TEA_TRACE`, and criteria 57-58 cover the warning.

"Golden" renders the model into the `x/vt` emulator (`internal/tui`). "Go E2E" runs the built `ask` in a PTY with a real leader it started, an isolated `ASK_HOME`, and the faux provider or the provider fixture inherited through the environment. Unit tests name their package.

| # | Criterion (short) | Owning test | Phase |
|---|---|---|---|
| 1 | P1 80×24 rail notice, `no model`, no bar | `TestFirstRunGoldens/no_model_80x24` + `TestInteractiveFirstRunNoModel` | 6, 7 |
| 2 | P1 40×14 | `TestFirstRunGoldens/no_model_40x14` | 6 |
| 3 | P2 enter with no model keeps draft | `TestNoModelSendRefusedKeepsDraft` + `TestInteractiveFirstPromptNotSent` | 6, 7 |
| 4 | `/p` lists `/providers`; `/PROVIDERS` opens it | `TestCompletionListsProviders` + `TestProvidersCommandCaseInsensitive` | 6 |
| 5 | Seven rows, search row last, draft kept | `TestProviderBoxGoldens/list_80x24` + `TestInteractiveProvidersList` | 6, 7 |
| 6 | Search `key` filters, first match selected | `TestProviderBoxSearchFilters` | 6 |
| 7 | 40×14 window, `… 3 more`, name cut, suffix whole | `TestProviderBoxGoldens/narrow_40x14` | 6 |
| 8 | Row menu on signed-in row; key box at once otherwise | `TestProviderRowMenu` + `TestProviderEnterStartsFlowAtOnce` | 6 |
| 9 | Pasted key shows `(hidden)` only | `TestKeyBoxPasteShowsHidden` | 6 |
| 10 | Checking: rail line, input locked | `TestKeyBoxCheckingLocksInput` | 6 |
| 11 | Refusal text, input empty | `TestKeyBoxRefusedClearsInput` + `TestBrokerKeyRefusalCarriesCodeOnly` + `TestInteractiveAPIKeyRefused` | 3, 6, 7 |
| 12 | Saved: note, rail, draft back | `TestKeyBoxSavedNoteAndRail` + `TestInteractiveAPIKeySaved` | 6, 7 |
| 13 | Browser box: wrapped URL, rail, hints | `TestBrowserBoxGoldens/waiting_80x24` | 6 |
| 14 | 40×14: four URL rows, cap row, esc | `TestBrowserBoxGoldens/narrow_40x14` | 6 |
| 15 | `access_denied`, enter tries again | `TestLoginFailedTryAgain` + `TestBrokerBrowserDenied` | 3, 6 |
| 16 | Another client is signing in | `TestBrokerOneLoginPerProvider` + `TestLoginCannotStartBusy` + `TestInteractiveProviderBusyTwoClients` | 3, 6, 7 |
| 17 | Leader lost during sign-in | `TestLoginBoxLeaderLost` + `TestRouterReleasesLoginsOfGoneClient` + `TestInteractiveLoginLeaderLost` | 4, 6, 7 |
| 18 | esc cancels; late success ignored | `TestLoginCancelNoteIgnoresLateResult` + `TestEscDuringKeyCheckNoteFollowsCancelAnswer` + `TestBrokerCancelDropsLateResult` + `TestBrokerKeyCancelBeforeCommitSavesNothing` | 3, 6 |
| 19 | ctrl+c cancels, never quits | `TestLoginCtrlCCancelsAndDoesNotQuit` | 6 |
| 20 | Account name in note | `TestLoginSuccessNamesAccount` + `TestInteractiveOAuthSignedInNamesAccount` | 6, 7 |
| 21 | P15, P16 address input hidden, checking | `TestAnthropicAddressAndCodeBoxes` + `TestAddressBoxPasteShowsHidden` + `TestBrokerAnthropicBrowserAcceptsPastedAddress` | 3, 6 |
| 22 | P15a busy port: code input hidden | `TestAnthropicAddressAndCodeBoxes` + `TestCodeBoxPasteShowsHidden` + `TestBrokerAnthropicFallsBackToCopyCode` + `TestBrokerCopyCodeInput` | 3, 6 |
| 23 | P12c removal by another client cancels this sign-in | `TestLoginCancelledByRemovalShowsReason` + `TestBrokerLogoutCancelsPendingSignIn` + `TestHostLogoutCancelsOtherClientSignIn` + `TestClientWaitLoginCancelledByRemoval` + `TestInteractiveRemovalCancelsOtherClientSignIn` | 3, 4, 5, 6, 7 |
| 24 | Device code, expiry, new code | `TestDeviceBoxGoldens` + `TestBrokerDeviceExpired` | 3, 6 |
| 25 | Remove confirm, esc keeps, enter removes | `TestRemoveConfirmFlow` + `TestInteractiveRemoveProvider` | 6, 7 |
| 26 | Env still applies note | `TestRemoveNoteEnvStillSet` + `TestAccountsLogoutReportsEnv` | 4, 6 |
| 27 | Env-only row: nothing sent | `TestRemoveEnvOnlyRowSendsNothing` | 6 |
| 28 | Current model removed, then prompt refused | `TestCurrentModelRemovedShowsNoModel` + `TestHostLogoutClearsUnusableModels` + `TestLogoutReportsClearedModelOfCallerSession` + `TestInteractiveRemoveCurrentProvider` | 4, 6, 7 |
| 29 | In-process: needs the leader | `TestProvidersNeedsLeaderInProcess` + `TestClientProvidersNeedLeaderInProcess` | 5, 6 |
| 30 | `/model` groups usable providers only | `TestModelSelectorGroupsUsableProviders` + `TestModelsUsableOnly` + `TestInteractiveModelListAfterSignIn` | 4, 6, 7 |
| 31 | `/model` empty hint, no filter row | `TestModelSelectorEmptyHint` | 6 |
| 32 | Second client: providers changed in place | `TestProvidersChangedUpdatesOpenBox` + `TestRouterBroadcastsProvidersChanged` + `TestInteractiveProvidersChangedSecondClient` | 4, 6, 7 |
| 33 | Stale login id, request id, generation | `TestStaleProviderResultsIgnored` + `TestClientDropsStaleProvidersChanged` | 5, 6 |
| 34 | Key box ignores up; draft kept | `TestKeyBoxIgnoresHistoryKeepsDraft` | 6 |
| 35 | Resize with login box open | `TestLoginBoxResizeKeepsLogin` + `TestAddressBufferSurvivesResizeAndIsSentOnce` + `TestKeyBufferSurvivesResizeAndIsSentOnce` + `TestInteractiveLoginResizeKeepsLogin` (PTY resize; fixture sees one start) | 6, 7 |
| 36 | Key never on the screen grid | `TestSecretNeverOnScreenGrid` + `TestInteractiveSecretNeverOnScreen` | 6, 7 |
| 37 | Key not in scrollback, blocks, notes | `TestSecretNotInTranscriptBlocks` + `TestInteractiveSecretNotInTranscript` | 6, 7 |
| 38 | Key not in prompt history | `TestSecretNotInPromptHistory` + `TestInteractiveSecretNotInTranscript` (Up in the real editor) | 6, 7 |
| 39 | Key not in undo or kill ring; draft same | `TestSecretNotInUndoOrKillRing` + `TestSecretFlowKeepsDraftAndUndo` (box handlers with a saved nonempty draft) | 6 |
| 40 | Buffer cleared on every exit | `TestSecretBufferClearedOnEveryExit` + `TestSecretExitOverwritesFormerBuffer` (old bytes after close, save, refusal and disconnect) | 6 |
| 41 | No key or code in logs or event stream; leader gets it once | `TestHostNeverLogsSecrets` + `TestInteractiveSecretNotInLogsOrEvents` + `TestInteractiveCopyCodeNotInLogsOrEvents` (successful code flow, proxy frames and one exchange) | 4, 7 |
| 42 | Not in title, bell, clipboard | `TestClipboardCopiesOnlyURLOrCode` + `TestNoTitleOrBellInOutput` | 6 |
| 43 | Bad paste refused without echo | `TestSecretPasteRefusedWithoutEcho` | 6 |
| 44 | Start line 80×24, footer bare id | `TestModelLineGoldens/start_80x24` + `TestInteractiveStartLineShowsPair` | 6, 7 |
| 45 | Start line 120×40 | `TestModelLineGoldens/start_120x40` | 6 |
| 46 | Start line 40×14, no label | `TestModelLineGoldens/start_40x14` | 6 |
| 47 | Two formats, own row at 40×14 | `TestModelLineGoldens/two_formats_80x24` and `two_formats_40x14` | 6 |
| 48 | Change line after `/model` | `TestModelChangeLine` + `TestAgentSetModelPublishesModelUpdate` + `TestInteractiveModelSwitch` (updated) | 2, 6, 7 |
| 49 | Observer attach: divider, line, history | `TestObserverAttachStartLine` + `TestInteractiveSessionsAttach` (updated) | 6, 7 |
| 50 | Reconnect note then line | `TestReconnectStatesGoldens/restored` (updated) | 6 |
| 51 | No model: no line, notice stays | `TestNoModelHasNoModelLine` + `TestLiveModelClearKeepsRemovalNoticeWithoutLine` (live clear, footer, notice and resize; prior history stays) | 6 |
| 52 | Stale model event adds no row | `TestStaleModelEventAddsNoRow` | 6 |
| 53 | `/model` saves the default; rail; `/new` uses it | `TestDefaultModelSavedOnSelect` + `TestModelSelectShowsDefaultSavedStatus` + `TestSessionStartUsesSavedDefault` + `TestInteractiveDefaultModelSurvivesNewSession` + `TestLeaderDefaultModelSurvivesRestart` | 4, 6, 7 |
| 54 | P28c saved default not usable; fallback row | `TestSessionStartFallsBackWhenDefaultUnusable` + `TestSessionStartUsesProviderDefault` + `TestFallbackNoticeNamesModelInUse` + `TestInteractiveFallbackNoticeWhenDefaultRemoved` | 4, 6, 7 |
| 55 | Sign-in does not change default or current model | `TestDefaultNotSetBySignIn` + `TestProvidersSignInDoesNotChangeDefault` | 4, 6 |
| 56 | P25a `/model` tags the saved default | `TestModelsReportsSavedDefault` + `TestModelSelectorTagsDefault` | 4, 6 |
| 57 | P28b, P6a `TEA_TRACE` warning with the file name | `TestTraceWarningOnceAtStart` + `TestTraceWarningOnKeyBox` + `TestTraceWarningOnCodeInput` + `TestInteractiveTraceWarningShown` | 6, 7 |
| 58 | No `TEA_TRACE` row without the variable | `TestNoTraceWarningWhenUnset` + `TestInteractiveNoTraceWarningWhenUnset` | 6, 7 |
| 59 | Token Plan key box says it cannot be checked; no `Checking` row; saved note; row reads `api key` (P6b, P9a, D16) | `TestAlibabaKeyBoxSaysNotChecked` + `TestKeySavedNotCheckedNote` + `TestInteractiveTokenPlanKeySavedUnchecked` | 6, 7 |
| 60 | A wrong Token Plan key saved without a check ends the first prompt with the auth error block and the `/providers` hint (P9b) | `TestUncheckedKeyRefusedAtFirstPrompt` + `TestInteractiveTokenPlanBadKeyAtFirstPrompt` | 6, 7 |

## Risks

| Risk | L×I | Mitigation |
|---|---|---|
| A secret reaches a log, frame record, transcript or artifact | M×H | Secret only in request params; fixed error texts; scans of leader log, client frames, screen grid, history, rings (phases 4, 6, 7); real-run artifact scan (phase 8) |
| Removing faux as default breaks T1a, `ask acp` and runner tests, or sends runner prompts to a billed provider | H×H | `ASK_FAUX=1` in every env helper, the `ask acp` E2E env and `e2e/tui/run.py`; credential variables removed from runner children; same commit as the default change (phase 4) |
| A cancelled key check still stores the key | M×H | Key save has a login id; cancel checked right before the store write; note follows the cancel answer (phases 3, 6) |
| Provider methods reachable from an editor over `ask acp` | M×H | Served only on the leader link with leader-only accounts (D2); forged-route test (phase 4) |
| A login goroutine outlives its client | M×M | Release on client gone (D2), 10 min auth timeout (`login.go:85`), goleak in broker tests |
| Version skew (new TUI, old leader or the reverse) | M×M | Additive fields; `usableOnly` echo (D7); unknown method gives `unsupported`, which the TUI shows as P3 load failure |
| A bad Alibaba Token Plan key is found only at the first prompt (D16) | M×L | Accepted by the user; the save note says the key was not checked; the first prompt shows the auth error block; no real Token Plan run in phase 8 |
| `TEA_TRACE` writes raw input, a pasted key included, to the trace file (D14, documented residual risk) | L×H | Stated exception to the security criteria; the TUI warns on the key box, the code input and once at start; docs name it (phases 6, 7, 8). The user sets the variable on purpose |
| A removal cancels another client's sign-in in the middle (D15) | M×M | Cancel with reason `removed`, wait for the goroutine to end before the removal, drop a late result; the other client's box says why; two-client E2E (phases 3, 4, 6, 7) |
| Default model in `settings.json` is lost or corrupt under two writers | L×M | Lock sidecar, read-merge-write, 0600, as `auth.json`; a failed save never undoes the switch (`defaultSaved: false`) (phases 3, 4) |
| Golden churn from section 12 amendments hides a regression | M×M | Regenerate per golden and review each diff: only the model line row may change |

## Red-team and validation record

### Red-team review (2026-10-10)

One hostile reviewer (code-reviewer agent) applied the four lenses (Assumption Destroyer, Failure Mode Analyst, Scope and Complexity Critic, Security Adversary) to all eight files. 12 findings, all with `file:line` evidence; the evidence filter rejected none. Spot checks by the planner: `route.go:27-39`, `module_leader.go:73-79`, `acp_e2e_test.go:709-712`, `queue.go:180-195`.

| # | Sev | Lens | Finding | Evidence | Disposition | Applied in |
|---|---|---|---|---|---|---|
| 1 | Critical | Security | An editor on `ask acp` can forge a `_meta` route and reach the provider methods | `route.go:27-39,53-76`, `module_leader.go:73-79` | Accept: serve only with `RequireRoute` and leader-only accounts; forged-route test | D2, phase 4 |
| 2 | High | Assumption | Faux opt-in also changes `ask acp` and `e2e/tui/run.py`; runner could bill a real provider | `cmd/tui/acp.go:100-102`, `acp_e2e_test.go:711`, `run.py:506,545,659-665` | Accept: env in ACP E2E and runner, strip credentials, same commit | D10, phase 4 |
| 3 | High | Failure/Security | esc during a key check says "Nothing was saved" while the leader saves | design P7, `rpc/conn.go:241`, `session.go:33` | Accept: key save gets a login id; cancel before commit; note follows the cancel answer | D1, phases 2, 3, 5, 6 |
| 4 | High | Failure | A model clear during a run races with the unlocked config copy | `agent.go:478-482,554-562` | Accept (planner had found it): clear deferred to `end` under `a.mu` | D9, phase 2 |
| 5 | High | Failure | Steer and follow-up skip the no-model refusal | `queue.go:180-195`, `update_input.go:108-109` | Accept: refuse in `enqueue`; map `no_model`; UI check before the run branch | D9, phases 2, 4, 6, 7 |
| 6 | High | Security/Scope | Real run: leader inherits `OPENAI_API_KEY`; scan not wired in that runner | `real_provider.py:37-41`, `spawn.go:272-276`, `run.py:633-639` | Accept: ordered env handling and scan | phases 7, 8 |
| 7 | High | Failure | Providers generation restarts with a new leader; client drops valid notices | `internal/tuiclient/client.go:127,266-290` | Accept: last seen value per leader instance | phase 5 |
| 8 | Medium | Security | `TEA_TRACE` logs raw input, a pasted key included | `third_party/bubbletea/tea.go:641-646`, `tty.go:75` | Accept; user chose D14 option B: keep `TEA_TRACE`, warn on screen, stated exception | phases 6, 7 |
| 9 | Medium | Scope | Logout result does not say the caller's model was cleared | design `design.md:144`, `host.go:556-572` | Accept: `clearedModel`, `clearPending` | phases 2, 4, 6 |
| 10 | Medium | Failure | Reconnect can draw a change line and a start line | `reconnect.go:19-27`, design P31 | Accept: change lines only after the restore baseline | phase 6 |
| 11 | Medium | Failure | Pending login stalls shutdown 20 s; login context tied to the request | `module_acp.go:101-104`, `module_auth.go:53-58`, SDK `connection.go:415-421` | Accept: broker root context; close before drain | D4, phases 3, 4 |
| 12 | Medium | Failure | A removal during another client's sign-in fails that sign-in | `login.go:80,111,122-136` | Accept; user chose D15 option B: removal wins and cancels the other sign-in with a reason | D4, phases 3, 4, 6, 7 |

### Validation (2026-10-10)

The planner could not interview the user from this run; the user answered the decisions D10-D15 through the coordinator on 2026-10-10 (section below).

- Tier: Full (8 phases): Fact Checker, Flow Tracer, Scope Auditor, Contract Verifier.
- Fact Checker: 100 `path:line` citations checked mechanically (file exists, line in range). 98 passed. The 2 failures were short names (`acp.go:191-194` means `pkg/protocol/acp.go`), now qualified.
- Flow Tracer: traced the run config copy (`agent.go:478-482`; found the race, D9), the route check on the leader link (`route.go:64-75`; provider methods need their own dispatch, phase 4), the router's internal route form (`router.go:574-585`), and the secret paths. Frame tap keeps ids only (`stdio.go:300-321`), capture runs only in headless mode (`headless.go:147`), the router logs no params, and history lives in memory.
- Scope Auditor: the criterion map had rows 1-50 with none missing (first pass; resynced to 1-58 later, see the sweep below). Every non-UI test in the map is named in its owning phase file. Every section 12 amendment has an owner (phase 6 for goldens and tests, phase 8 for the T1a design text).
- Contract Verifier: protocol changes are additive (`usableOnly` request flag with echo, new kinds -32018 and -32019, new methods unknown to old routers give `unsupported`, `router.go:250-253`). `ask connect` E2E is a regression gate in phases 2, 4 and 5.
- Verified: 102; Failed: 0 after the fixes; Unverified: 2 at that time; the Token Plan probe was closed by D16, the account-name item stays a phase 3 scout step.
- `ak plan validate`: valid. Link and anchor check: 0 broken.

### Whole-Plan Consistency Sweep

Decision delta: key save has a login id; provider methods are leader-only; faux opt-in covers `ask acp` and the runner; deferred model clear; no-model refusal in `enqueue`; generation per leader instance; logout reports the cleared model; broker root context and shutdown order; removal takes the provider slot (superseded by D15 option B, see the sweep below); `TEA_TRACE` handling (superseded by D14 option B); no `modelCount` in the outcome.

Checked across `plan.md` and all phase files: D1, D2, D4, D9, D10 and D14-D15 match the phase designs. The criterion map rows 18, 26 and 39 name the new tests. The shared-file list includes `acp_e2e_test.go` and `run.py`. The phase 2 shapes (`ACPAPIKeyRequest.loginId`, `ACPLoginCancelResult`, `ACPLogoutResult.clearedModel`) match the phase 3 broker, the phase 4 host and the phase 5 client. No stale "allowed when busy" or "route proves leader" text remains. No contradiction is open.

### Whole-Plan Consistency Sweep after the confirmed decisions (2026-10-10)

Decision delta: D12 option B3 (default saved on `/model` choice in `settings.json`, start order, fallback in the state, `/providers` does not set it); D14 option B (`TEA_TRACE` kept, warning, stated exception; the earlier "drop `TEA_TRACE`" step is removed); D15 option B (removal cancels a pending sign-in or key check, reason `removed`, removals serialized per provider; the earlier `provider_busy` for removal is removed); phase 1 back to `draft`, then approved again the same day.

Planner choice to note: the fallback notice is carried in `_ask/session/state` (`modelFallback`), not as a stream event, because the fallback happens when the session is created, before any client follows it.

Checked: phases 2-8 match D10-D15. The phase 3 broker, phase 4 host, phase 5 client and phase 6 UI agree on `defaultSaved`, `modelFallback` and reason `removed`. The criterion map was then synced to the re-approved design's criteria 1-58 (no provisional rows remain). Ownership: `internal/settings/settings.go` and `internal/providers/catalog.go` belong to phase 3 only, and `internal/app/default_model.go` to phase 4 only. No stale text remains for the removed options (search below).

### Sweep after the design re-approval (2026-10-10)

The design was approved again with criteria 1-58 and new states P6a, P12c, P15a, P25a, P28b, P28c. Changes applied in this plan:
- Phase 1 is completed again, and both approvals are recorded.
- Phase 6 is no longer blocked.
- The criterion map is synced to 1-58; the phases 6, 7 and 8 criterion numbers are renumbered.
- `set_model` gains `saveDefault`: only a choice in `/model` saves, per design section 3.
- The models result names the saved `default` (P25a).
- The fallback reasons are `not_signed_in` and `not_in_catalog` (P28c).
- The Claude Pro/Max login kind is now `browser_address` (P15).
- The UI gets the `TEA_TRACE` file name, because the warning shows it (P28b, P6a).

The mechanical checks (validate, links, citations, map-to-phase test ownership) were run after these edits.

### Sweep after D16 (2026-10-10)

The Token Plan key is saved without a live check: phase 2 adds `notChecked` to the outcome, phase 3 adds `KeyCheckable` and replaces the stop-and-ask step, phase 6 adds the not-checked note, phase 7 adds an E2E with no fixture request, phase 8 never signs in to Token Plan. The old question 1 and the Token Plan probe risk are removed. The design added P6b, P9a, P9b and criteria 59-60; the map names their tests (no `TODO` row).

## Decisions the user confirmed (2026-10-10)

The phase files follow these confirmed options (updated 2026-10-10).

1. **D10 faux opt-in.** Decided (user, 2026-10-10): as recommended. faux only with `ASK_FAUX=1` (leader, `ask acp`, in-process TUI); `ask -p` unchanged. Without this, P1 cannot happen, because faux is always usable. Blocks phase 4; shapes phases 2 and 7.
2. **D11 Anthropic box.** Decided (user, 2026-10-10): as recommended; the design was amended and approved again (P15, P15a, P16). Browser flow plus a pasted callback address, with fallback to copy-code when the port is busy. Shapes the Anthropic path of phase 3 and the P15, P15a goldens of phase 6.
3. **D12 default model.** Decided (user, 2026-10-10): option B3, as omp (see the D12 row). The design now has these texts (P25a, P28c, P29). Shapes phase 3 (default storage on the leader), phase 4 (session start order) and phase 6 (status text).
4. **D13 criterion 42 vs. catalog.** Decided (user, 2026-10-10): as recommended. accept that the product start line for deepseek shows `(anthropic-messages)`; goldens use fixture lists. Blocks acceptance of the criterion 42 golden (phase 6) and the phase 8 record.
5. **D14 `TEA_TRACE`.** Decided (user, 2026-10-10): option B, keep it and warn on screen (see the D14 row). The design now has the warning (P6a, P28b) and the stated exception. Shapes phase 6 (warning) and phase 7 (test that the warning shows when the variable is set).
6. **D15 removal while another client signs in.** Decided (user, 2026-10-10): option B, removal wins and the other sign-in is cancelled with a reason (see the D15 row). The design now has that text (P12c). Shapes phases 3, 6 and 7.

## Unresolved questions

No open question remains.

Resolved (2026-10-10): the account name source per provider follows each provider's own CLI (Codex, Claude Code, Grok CLI; details in phase 3). The ChatGPT ID token carries `email` and `name` claims (checked on the stored sign-in: claim names only, no values read). `identity.go:77-86` already verifies the ID token; phase 3 adds `email` and `name` to its claims struct. The T1a work of the other session is committed (`f62059b`).
