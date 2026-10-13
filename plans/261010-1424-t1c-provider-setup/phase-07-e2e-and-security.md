---
phase: 7
title: "E2E and security acceptance"
status: completed
priority: P1
effort: "1.5d"
dependencies: [6]
---

# Phase 7: E2E and security acceptance

Scout record and results of the run of 2026-10-10 are below. Criterion numbers follow the re-approved design (1-60).

## Goal

Prove the leader-path criteria and the security criteria on the built `ask` in a PTY, with a real leader that the TUI starts, an isolated `ASK_HOME`, the faux provider (`ASK_FAUX=1`) or no provider, and the provider fixture for key probes and OAuth endpoints. Every leader-path test asserts the leader id in the footer, so an in-process fallback cannot pass.

## Evidence (re-scouted 2026-10-10 against the tree after phase 6)

- PTY helpers and env: `cmd/tui/interactive_test.go:61` (`interactiveEnv`). `ASK_FAUX=1` is in `leaderEnv` (`leader_e2e_test.go:26`); a first-run test appends `ASK_FAUX=0`. The process has no controlling terminal, so a resize needs the in-band report of the emulator and `SIGWINCH` (`askTerm.resize`).
- Provider fixture: `cmd/tui/acp_e2e_test.go` (HTTPS backend behind a CONNECT proxy with a temporary CA). It now has one host list for the certificate and the proxy, and a `route` hook for every destination the switch does not know. The routes of the provider flows are in `providers_flow_test.go` (`providerRoutes`): the three key checks, the device authorization and token poll of xAI, the token and key set of ChatGPT, and the Token Plan messages endpoint (401). A unit check (`TestProviderFixtureAnswersProviderRoutes`) drives them through the proxy.
- The leader inherits the fixture through the environment of the first `ask` that starts it; a second `ask` joins that leader.
- Terminal runner: `e2e/tui/run.py`. It records what it types in `commands.json` and in the input events of `output.cast`; the new case redacts its fake key there and adds the value to the scan, so the scan finds it only when the product wrote it.
- A single link of the built `ask` cannot be dropped from outside. `linkProxy` (`providers_reconnect_test.go`) forwards the leader socket of one home to the leader of another; the client runs with the proxy home. Dropping it ends only the links, and while stalled it never refuses, so the client never starts a leader of its own.

## Tests (all new files; failing first against the phase 6 build)

`cmd/tui/providers_flow_test.go`:

| Test | Criterion |
|---|---|
| `TestInteractiveFirstRunNoModel` | 1 (no env key, no `ASK_FAUX`) |
| `TestInteractiveFirstPromptNotSent` | 3 (no prompt reaches the leader: fixture and leader counters unchanged) |
| `TestInteractiveProvidersList` | 5 (saved Token Plan key, stored without a check, `ANTHROPIC_API_KEY` fake, saved ChatGPT record from `ask auth login` with the fixture) |
| `TestInteractiveAPIKeyRefused` | 11 (fixture answers 401 `invalid_api_key`) |
| `TestInteractiveTokenPlanKeySavedUnchecked` | 59 (paste a fake Token Plan key: P6b text, P9a note and rail, row `api key`; the fixture sees no request; `/model` lists the Token Plan models) |
| `TestInteractiveTokenPlanBadKeyAtFirstPrompt` | 60 (choose the Token Plan model; the fixture answers 401 on the Token Plan messages endpoint; the error block and `Replace the key with /providers` show) |
| `TestInteractiveAPIKeySaved` | 12 (fixture answers 200; footer still `no model` on first run) |
| `TestInteractiveProviderBusyTwoClients` | 16 (two `ask` clients on one leader; second start refused) |
| `TestInteractiveLoginLeaderLost` | 17 (leader stopped during a device login; box text; not reopened after reconnect) |
| `TestInteractiveOAuthSignedInNamesAccount` | 20 (device or copy-code login against the fixture; account from the fixture token answer) |
| `TestInteractiveRemoveProvider` | 25 |
| `TestInteractiveRemoveCurrentProvider` | 28 (`no model` footer on both clients; prompt refused) |
| `TestInteractiveModelListAfterSignIn` | 30 |
| `TestInteractiveRemovalCancelsOtherClientSignIn` | 23, D15 (two `ask` clients on one leader: client A waits in the device box, client B removes the provider; A's box shows the cancel with the removal reason, B's note shows the removal; the fixture sees no token request from A after the removal) |
| `TestInteractiveDefaultModelSurvivesNewSession` | 53, D12 (choose `gpt-5.5` in `/model`: `Default model:` status; quit, start `ask` again: the start line shows `openai/gpt-5.5`; `settings.json` is 0600) |
| `TestInteractiveFallbackNoticeWhenDefaultRemoved` | 54, D12 (saved default on a provider that is then removed; a new session starts on the next choice and the fallback notice names it) |
| `TestInteractiveProvidersChangedSecondClient` | 32 (observer client with the box open) |
| `TestInteractiveLoginResizeKeepsLogin` | 35 (PTY resize 120×40 to 80×24 with the device box open; the fixture sees one device-code request) |
| `TestInteractiveStartLineShowsPair` | 44 |
| `TestInteractiveModelSwitch`, `TestInteractiveSessionsAttach` | 48, 49 (the T1a tests gain the change line and the start line; edited after the T1a work is committed) |

`cmd/tui/providers_security_test.go` (one fake key and one fake code, made at run time):

| Test | Criterion | Scan target |
|---|---|---|
| `TestInteractiveSecretNeverOnScreen` | 36 | PTY screen after every read during paste, checking, refused and saved |
| `TestInteractiveSecretNotInLogsOrEvents` | 41 | Leader log under `ASK_HOME`, TUI stderr, the event stream of a second `ask connect` client that follows the session, the store file holds it once; the fixture sees one probe with the key |
| `TestInteractiveSecretNotInTranscript` | 37 | Screen scrollback dump after `/new` and resize rebuild |
| `TestInteractiveTraceWarningShown` | 57, D14 | With `TEA_TRACE` set to a temp file: the warning shows once at start and on the key box. The trace file is the stated exception and is not scanned; the test deletes it |
| `TestInteractiveNoTraceWarningWhenUnset` | 58, D14 | Without `TEA_TRACE`, no warning row |
| `TestConnectFollowUpWithoutModelRefused` | 3, 28 (leader side) | `ask connect` follow-up on an idle session with no model gets `no_model`; nothing is queued |

The security tests run without `TEA_TRACE` (the design's stated exception). The UI-only security criteria (38, 39, 40, 42, 43) stay with their phase 6 tests, which can read the history, the rings, the buffer and the clipboard command directly.

Notes on how the tests meet the map (2026-10-10):

- Criterion 5 and the draft: the command `/providers` is the whole editor line, so the editor holds no draft when the box opens. The test checks that esc brings the editor back (empty, placeholder). The design text "your draft is kept" can only hold for the empty editor (design gap, see the report).
- Criterion 20 uses the xAI device sign-in. The account comes from the `email` claim of the fixture id token. The ChatGPT account name stays with the auth unit tests; the ChatGPT sign-in is used here only to put a saved record in the home of criterion 5, with the production `ask auth login` against the fixture.
- Criterion 17: after the leader is stopped and a new one started, the box still shows the stopped text until esc, and the fixture sees no second device authorization.
- Criterion 41 also reads every frame between the client and the leader through `linkProxy`: the key is in the frames to the leader once and in none from it.
- Criterion 57: the test pastes no key, so the trace file holds no secret; it deletes the file at the end.
- Criterion 35: the resize test uses `askTerm.resize`.
- `TestConnectFollowUpWithoutModelRefused`: `ask connect` has no follow-up verb, so the follow-up and the steer go over the same leader link with the raw call helper, on the session that `ask connect` made.
- Reconnect with two missed model changes: `TestInteractiveReconnectDrawsNoMissedModelChanges` (built `ask`, link dropped through `linkProxy`), `TestClientResumedFollowNamesReplayHead` (tuiclient and leader), `TestRestoreMissedModelChangesDrawNoLine` (view, the failing case first), `TestFollowHeadNamesTheEndOfTheReplay` (agent).

`e2e/tui/run.py`: one structural case `ask-providers` (provider box at 80×24, draft kept, esc back) for a real terminal engine, plus the secret scan of its artifacts. The runner writes typed text into argv and `commands.json` (`run.py:145-158,208-209`), so a case that types a fake key adds that value to the scan set (`secret_values`, `run.py:633`) and never types a real one.

## Files owned

| File | Action |
|---|---|
| `cmd/tui/providers_flow_test.go`, `cmd/tui/providers_security_test.go`, `cmd/tui/providers_reconnect_test.go` | created |
| `cmd/tui/acp_e2e_test.go` | modified: one host list, `route` hook |
| `cmd/tui/interactive_flow_test.go` | modified: change line (48) and start line under the divider (49) |
| `e2e/tui/run.py`, `e2e/tui/test-assertions.py`, `e2e/tui/README.md` | modified: `ask-providers` case, redaction of the typed fake key, controls, environment text |
| Product fix for a replayed model change after a reconnect: `pkg/protocol/acp.go` (`ACPFollowResult.Head`), `internal/agent/follow.go` (`Follow.Head`), `internal/acp/updates.go`, `internal/tuiclient/events.go`, `internal/tui/client.go` (`SnapshotMsg.Head`), `internal/tui/update_session.go` (baseline), `internal/acp/README.md` | modified, additive |
| `internal/agent/follow_head_test.go`, `internal/tui/restore_missed_model_test.go` | created |

## Steps

1. Scout the fixture and the PTY helpers; add the fixture routes with their own unit check.
2. Write each test, see it fail where the product is missing, fix the product in the phase that owns the file (reopen that phase's checklist), never in a test-only hook.
3. Run the full suite and the terminal runner.

## Verification

```
go test ./cmd/tui/ -run 'TestInteractive|TestConnect|TestLeader'
go test ./...
python3 e2e/tui/run.py
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Rollback

Test-only files and fixture routes; revert the commit.

## Risks

| Risk | Mitigation |
|---|---|
| Browser flows need a real browser | Use the device and copy-code flows with the fixture; the ChatGPT browser flow stays with the auth unit tests and the phase 8 real run |
| PTY timing makes tests flaky | Wait for screen text with deadlines, as the T1a tests do; no sleeps as asserts |
| A test leaves a leader running | Each test stops its own leader with `ask leader stop` and checks the process table, as `e2e/tui/run.py` does |

## Todo

- [x] Fixture routes
- [x] Flow tests (incl. default model, removal cancels sign-in)
- [x] Security tests (incl. trace warning)
- [x] Runner case and scan
- [x] Full suite green

## Results (2026-10-10)

- `go test ./...` passes; `go test -race` passes for `internal/tui`, `internal/tuiclient`, `internal/agent`, `internal/acp`, `pkg/protocol`; `go vet ./...` and `golangci-lint run ./...` report no issue.
- The new E2E tests passed three times in a row (`-count=3`).
- `python3 e2e/tui/run.py` passes (T0 and ask drivers, both engines, with `ask-providers`); `python3 e2e/tui/test-assertions.py` passes.

### Reconnect with two missed model changes

Reproduced in the view: with the state answer first and a resumed follow whose cursor stays at the client cut, the events A to B and B to C drew two change lines (`TestRestoreMissedModelChangesDrawNoLine` failed with both lines before the fix). In the built `ask` the order is almost always events first (the replay is written right after the follow answer), so the E2E passes with and without the fix; it is the acceptance test, and the view test is the deterministic proof.

Fix (additive): the follow result carries `head`, the last event published when the follow was answered. The client keeps it as the baseline of the follow when it lies above the cursor, so a replayed model event at or below it draws no line and the state read shows the model. A leader without `head` behaves as before. No known limit remains: a change that the leader posted but had not published at that moment reaches the client above `head` and draws its line, or draws nothing when the state already shows the same model.
