---
phase: 7
title: "E2E and security acceptance"
status: pending
priority: P1
effort: "1.5d"
dependencies: [6]
---

# Phase 7: E2E and security acceptance

Outline with scouted evidence. Run a scout pass and update this file before cooking.

## Goal

Criterion numbers follow the re-approved design (1-58). Prove the leader-path criteria and the security criteria on the built `ask` in a PTY, with a real leader that the TUI starts, an isolated `ASK_HOME`, the faux provider (`ASK_FAUX=1`) or no provider, and the provider fixture for key probes and OAuth endpoints. Every leader-path test asserts the leader id in the footer, so an in-process fallback cannot pass.

## Evidence (scouted 2026-10-10)

- PTY helpers and env: `cmd/tui/interactive_test.go:61` (`interactiveEnv` removes credential variables of the developer shell; phase 4 adds `ASK_FAUX=1`).
- Existing flow tests: `cmd/tui/interactive_flow_test.go` (uncommitted T1a edits; not edited here, new tests go in new files).
- Provider fixture: `cmd/tui/acp_e2e_test.go:1464-1560`, an HTTPS backend behind a CONNECT proxy with a temporary CA; it already answers `https://api.anthropic.com/v1/messages` and `https://platform.claude.com/v1/oauth/token` (`:1490-1505`). The leader inherits it through the environment (`f.env()`, `:1554`). It needs routes for the key probes (`/v1/models` of the four providers) and the xAI device endpoints (`auth.x.ai/oauth2/device/code`, `/oauth2/token`).
- Terminal runner: `e2e/tui/run.py` with secret scanning (`:516-517`, `secret_values` `:633`, `scan_secrets` `:639`).

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

`e2e/tui/run.py`: one structural case `ask-providers` (provider box at 80×24, draft kept, esc back) for a real terminal engine, plus the secret scan of its artifacts. The runner writes typed text into argv and `commands.json` (`run.py:145-158,208-209`), so a case that types a fake key adds that value to the scan set (`secret_values`, `run.py:633`) and never types a real one.

## Files owned

| File | Action |
|---|---|
| `cmd/tui/providers_flow_test.go`, `cmd/tui/providers_security_test.go` | create |
| `cmd/tui/acp_e2e_test.go` | modify: fixture routes for probes, device flow and the Token Plan messages endpoint (401) |
| `cmd/tui/interactive_flow_test.go` | modify: criteria 48, 49 assertions (only after the T1a work is committed) |
| `e2e/tui/run.py`, `e2e/tui/README.md` | modify: `ask-providers` case |

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

- [ ] Fixture routes
- [ ] Flow tests (incl. default model, removal cancels sign-in)
- [ ] Security tests (incl. trace warning)
- [ ] Runner case and scan
- [ ] Full suite green
