---
phase: 2
title: "Protocol shapes, model event and no-model contract"
status: completed
priority: P1
effort: "1d"
dependencies: [1]
---

# Phase 2: Protocol shapes, model event and no-model contract

Detailed phase (deep mode). Can run in parallel with phase 3.

## Goal

Add the wire types of the provider methods, the `model_update` event, the `no_model` and `provider_busy` error kinds, and an agent that can hold no model, all additive, so `ask connect` and old clients keep working.

## Evidence (scouted 2026-10-10)

- Method names and request types live in `pkg/protocol/acp.go:9-36` and `:52-231`. `ACPModelsResult` is `{current, models}` (`pkg/protocol/acp.go:191-194`); the models handler takes `ACPSessionRequest` (`internal/acp/ask_methods.go:127-142`).
- Error kinds and codes: `pkg/protocol/acp.go:234-261`, `:265-277`, `ACPErrorCode` `:296-329`, `ACPErrorMessage` `:335-374`. Ask codes use -32010 to -32017; the next free codes are -32018 and -32019.
- Agent events: `pkg/protocol/events.go:13` (type names), `:88-92` (`QueueUpdate`), `:205-224` (`EventType`). The codec of events is in `pkg/protocol/codec.go` and `codec_fast.go`. Resolved 2026-10-10: the registry is the two type switches of `EncodeEvent` and `DecodeEvent` in `codec.go` (`:22`, `:251`); `codec_fast.go` only holds the `message_update` delta fast path. An unregistered type decodes to `RawEvent`, so `ModelUpdate` needs both cases.
- The agent posts idle events with `postLocked` (`internal/agent/emit.go:79-87`), for example the queue update (`agent.go:199,246,374`).
- `SetModel` validates, checks readiness, then sets `a.cfg.Model` (`internal/agent/agent.go:282-329`). `New` accepts an empty `Config.Model` (`agent.go:121-141`). `State()` returns `a.cfg.Model` (`agent.go:398-408`).
- A run copies `a.cfg.LoopConfig` by value at its start in `execute` (`agent.go:478-482`), **without** holding `a.mu`. `SetModel` is idle-only, so no write races with that read today. A model clear while busy would race with it (verified 2026-10-10), so a busy clear must be deferred.
- `end` holds `a.mu` while it decides whether a queued run starts next (`agent.go:444-458`).

## Shapes

New file `pkg/protocol/acp_providers.go` (keeps `acp.go` focused on session methods):

| Type | Fields |
|---|---|
| Method constants | `ACPProvidersList`, `ACPProvidersAPIKey`, `ACPLoginStart`, `ACPLoginInput`, `ACPLoginWait`, `ACPLoginCancel`, `ACPProvidersLogout`, `ACPProvidersChanged` (notification), `ACPProvidersReleaseClient` (router only) |
| `ACPLoginOption` | `providerId`, `methodId`, `name`, `state` (`signed_in`, `api_key`, `env`, `none`), `envVar` |
| `ACPProviderStatus` | `providerId`, `usable`, `modelCount` |
| `ACPProvidersResult` | `generation` (decimal text), `options []ACPLoginOption`, `providers []ACPProviderStatus` |
| `ACPAPIKeyRequest` | `loginId` (made by the client, unique for that client), `providerId`, `key`. The id lets `login/cancel` stop a key check before the key is stored |
| `ACPLoginStartRequest` | `providerId`, `methodId` |
| `ACPLoginStartResult` | `loginId`, `kind` (`browser`, `browser_address`, `copy_code`, `device`), `url`, `userCode`, `expiresAt` (RFC 3339, device only) |
| `ACPLoginInputRequest` | `loginId`, `code` (a code, or a pasted callback address for `browser_address`; design P15, P15a) |
| `ACPLoginIDRequest` | `loginId` (wait, cancel) |
| `ACPLoginCancelResult` | `cancelled`: true only when the leader stopped the attempt before it stored anything; false when the credential was already saved |
| `ACPLoginOutcome` | `loginId`, `status` (`signed_in`, `refused`, `failed`, `expired`, `cancelled`), `reason` (fixed token), `httpStatus`, `providerCode`, `notChecked` (true when a key was saved without a live check, D16; omitted otherwise, so an old outcome decodes as checked), `account`, `generation`. The model count is not here: the client reads it from the model list that it fetches after the outcome (design P9) |
| `ACPLogoutRequest` / `ACPLogoutResult` | `providerId`, `methodId`, `sessionId` (optional: the caller's session) / `removed`, `envVar` (set when an env key still applies), `clearedModel` (the model id of the caller's session when the removal made it unusable), `clearPending` (true when that session is running and clears at run end), `generation` |
| `ACPProvidersChangedNotification` | `generation`, `providerId`, `change` (`signed_in`, `removed`) |
| `ACPModelsRequest` | `sessionId`, `usableOnly` (new; same JSON key as `ACPSessionRequest` for `sessionId`) |
| `ACPModelsResult` | adds `usableOnly bool` (echo; omitted by old leaders) |
| `ACPModelRequest` | adds `saveDefault bool`: the TUI sets it for a choice in `/model` only (design section 3: the default is saved when the model is set from `/model`); `ask connect` and old clients leave it off |
| `ACPSetModelResult` | adds `defaultSaved bool`: the choice was saved as the leader's default model (D12) |
| `ACPModelsResult` (default) | adds `default` (omitted when none): the saved default model id, so `/model` can tag it (design P25a) |
| `ACPStateResult` | adds `modelFallback` (omitted when empty): `{savedDefault, reason}` when the session started on another model because the saved default was not usable (D12; design P28c). `reason` is a fixed token: `not_signed_in` (shown as `not signed in`) or `not_in_catalog`. It stays until the model of the session changes. It is state, not a stream event: the fallback happens when the session is created, before any client follows it, and the state is read at every start, attach and restore |

In `pkg/protocol/acp.go`: kinds `ACPErrNoModel` (`no_model`, -32018, "the session has no model; choose one first") and `ACPErrProviderBusy` (`provider_busy`, -32019, "another client is signing in to this provider").
In `events.go`: `TypeModelUpdate = "model_update"`, `ModelUpdate{Envelope; Provider, ID, API string}`; all empty means no model.

Every `generation` field (list result, outcome, logout result, changed notification) is the same leader counter in decimal text, as other Ask counters are (`internal/acp/README.md:50`).

No field holds a secret except `ACPAPIKeyRequest.key` and `ACPLoginInputRequest.code`; their doc comments say they are never logged or echoed. The outcome has no free text: `reason` is one of a fixed set (`invalid_key`, `access_denied`, `expired`, `network`, `cancelled`, `removed` (another client removed the provider, D15), `busy`, `internal`).

Agent (`internal/agent/agent.go`):

- `SetModel` posts `ModelUpdate` after the change, with `postLocked` under `a.mu`.
- New `ClearModel()` under `a.mu`: when idle, it sets `a.cfg.Model` to the zero model and posts `ModelUpdate{}` at once. When busy, it sets a pending flag; `end` applies the clear under `a.mu` before it starts any queued run, then posts `ModelUpdate{}`. The active run keeps its model; a queued run that follows gets no model. A `SetModel` after a pending clear drops the flag. Returns false when there was no model.
- `begin` and `drive` refuse with new `ErrNoModel` when `a.cfg.Model.ID == ""`, before the run starts, so no `agent_start` is published.
- `enqueue` on an idle agent with no model refuses with `ErrNoModel` before it starts a run (`internal/agent/queue.go:180-195` starts one today); during a run, input is queued as before and uses the run's model.

## Files owned

| File | Action | Size | Test impact |
|---|---|---|---|
| `pkg/protocol/acp_providers.go` | create | ~170 lines | new round-trip tests |
| `pkg/protocol/acp_providers_test.go` | create | ~120 | — |
| `pkg/protocol/acp.go` | modify: two kinds, two codes, two texts | +20 | `acp_test.go` code table |
| `pkg/protocol/acp_test.go` | modify | +20 | — |
| `pkg/protocol/events.go`, codec files | modify: `ModelUpdate` | +15 | `events_test.go` |
| `pkg/protocol/events_test.go` | modify | +30 | — |
| `pkg/protocol/README.md` | modify: new file and event | +5 | — |
| `internal/agent/agent.go` | modify: emit, `ClearModel`, no-model refusal | +40 | agent tests |
| `internal/agent/queue.go` | modify: no-model refusal in `enqueue` | +6 | queue tests |
| `internal/agent/model_update_test.go` | create | ~150 | — |

## Tests first

### Tests before (regression, must stay green)

- `go test ./pkg/protocol/... ./internal/agent/... ./internal/acp/...` as is.
- `TestConnectE2E*` in `cmd/tui/connect_e2e_test.go` (an old client must not notice).

### Tests after (new, failing first)

| Test | Proves |
|---|---|
| `TestProviderShapesRoundTrip` | Every new request and result encodes and decodes; counters are decimal text |
| `TestModelsRequestKeepsSessionKey` | `ACPModelsRequest` decodes an old `{sessionId}` body; `usableOnly` defaults to false |
| `TestModelsResultOmitsEchoForOldLeader` | `usableOnly` is omitted when false, so an old result decodes the same |
| `TestNewErrorKindsHaveFixedCodesAndTexts` | `no_model` is -32018 and `provider_busy` is -32019; texts hold no provider text |
| `TestSetModelResultReportsDefaultSaved` | `saveDefault` and `defaultSaved` round-trip; both are omitted when false, so old bodies decode the same |
| `TestModelsResultCarriesSavedDefault` | `default` round-trips and is omitted when empty |
| `TestStateCarriesModelFallback` | `modelFallback` round-trips; an old state body decodes with no fallback |
| `TestOutcomeReasonRemoved` | `removed` is a valid fixed reason |
| `TestOutcomeNotCheckedFlag` | `notChecked` round-trips and is omitted when false |
| `TestModelUpdateEventCodec` | `model_update` round-trips through the event codec, empty fields included |
| `TestAgentSetModelPublishesModelUpdate` | A listener gets one `model_update` with provider, id and api after `SetModel`; a refused `SetModel` posts nothing |
| `TestAgentClearModelPublishesEmptyUpdate` | `ClearModel` posts `model_update` with empty fields; `State().Model` is empty |
| `TestAgentClearModelKeepsActiveRun` | A run that is active when `ClearModel` is called ends with its model; `model_update` comes after its `agent_settled`; the next prompt gets `ErrNoModel`. Run with `-race` |
| `TestAgentPromptWithoutModelRefusedBeforeStart` | `Prompt` and `Continue` return `ErrNoModel` and publish no `agent_start` |
| `TestAgentEnqueueWithoutModelRefusedWhenIdle` | Steer and follow-up on an idle agent with no model return `ErrNoModel`; nothing is queued and no run starts |
| `TestAgentQueuedInputWithoutModelEndsCleanly` | A queued follow-up after `ClearModel` ends without a hang and with a no-model error |

### Refactor

None outside the files above. `ACPSessionRequest` stays for every other method.

## Steps

1. Re-check the event codec registry; remove its scout note or adapt the steps.
2. Write the protocol tests, see them fail, add `acp_providers.go`, kinds and `ModelUpdate`.
3. Write the agent tests, see them fail, add the emit, `ClearModel` and the refusal.
4. Run the regression set.

## Verification

```
go test ./pkg/protocol/... ./internal/agent/...
go test ./internal/acp/... ./cmd/tui/ -run 'TestConnectE2E|TestACP'
go vet ./pkg/protocol/... ./internal/agent/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./pkg/protocol/... ./internal/agent/...
```

## Rollback

All changes are additive. Revert the phase commit; no stored data changes.

## Risks

| Risk | Mitigation |
|---|---|
| A follower that resumes from a cursor misses an idle `model_update` | Test resume over `model_update` in phase 4 (`TestFollowResumeCarriesModelUpdate`); `queue_update` uses the same path |
| `ClearModel` while busy races with the unlocked config copy in `execute` | Busy clear is deferred to `end` under `a.mu`; `TestAgentClearModelKeepsActiveRun` with `-race` |

## Todo

- [x] Scout: event codec registry (config copy in `execute` verified: deferred clear)
- [x] Protocol tests and shapes
- [x] Agent tests and changes
- [x] Regression set green
