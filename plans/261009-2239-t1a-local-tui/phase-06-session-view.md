---
phase: 6
title: "Session view: status, queue, footer, selectors, connection states"
status: completed
priority: P1
effort: "1.5d"
dependencies: [2, 4, 5]
---

# Phase 6: Session view

Scout pass done and implemented; see "Scout result" and "Done" below.

## Goal

Connect the components to the client interface and show every session and connection state of the design.

## Scope

- Send states S5a and S5b: draft kept while sending; cleared on acceptance only if the draft revision did not change; refusal shows `✗ Not sent:` with the kind.
- Run states: `Working… Ns`, retry countdown, steer on enter and follow-up on alt+enter while running, pending rows with `↳ steer:` and `↳ follow-up:`, alt+up moves the queue back into the draft (remove by input id).
- Footer: row 1 place, session, role, leader; row 2 usage (`↑ ↓ $ ctx%/window`, warning at 70%, error at 90%) and model with thinking level; short form under 60 columns.
- Model selector (ctrl+l, `/model`; idle only, notice when running), filter, readiness error keeps the previous model (S11, S12). shift+tab cycles thinking when the model supports reasoning.
- Session selector `/sessions` (S12a), observer view (S14), `/take`, `/new`.
- Connection states S13, S13a, S13b from client status messages; enter during reconnect shows `Reconnecting, please wait…`.
- Stale results ignored by generation.

## Tests first

Model tests with a fake implementation of the `internal/tui` client interface (the interface boundary, not a bypass of the product path; the real path is covered in phases 2 and 8):

| Test | Proves |
|---|---|
| `TestSendClearsDraftOnlyAfterAcceptance` | S5a: edit during sending keeps the new text |
| `TestEnterWhileRunningSteers` / `TestAltEnterWhileRunningFollowsUp` | Queue intents by run state |
| `TestAltUpDequeuesIntoDraft` | Queue rows removed and texts joined into the draft |
| `TestSelectorReadinessFailureKeepsModel` | S12 |
| `TestStaleSetModelIgnored` | Older generation result changes nothing |
| `TestReconnectStatesGoldens` | S13, S13a, S13b match the design |
| `TestFooterUsageThresholds` | 70% and 90% roles; short form at 40 columns (S17) |

## Verification

`go test ./internal/tui/...`

## Scout result

- Client contract (phase 2): `ConnectedMsg` is the first message on `Events()`; `NewSession` follows the session before it returns, so the snapshot can reach `Update` before the command result does. The view therefore tracks the followed session id from the snapshot (`followed`) and the session as the commands know it (`sess`) separately.
- `PromptAcceptedMsg` can arrive before the `Prompt` call result (the call and the event queue are separate goroutines). The view keeps early prompt messages and replays them when the id is known.
- `QueueUpdate` carries texts without input ids. Ids come from the results of this client's own `Steer` and `FollowUp` calls and are matched by kind and text when alt+up runs. An input without a known id (queued by another client, or before a reconnect) stays in the queue and the user gets a notice.
- Status needs two rows for a lost link (S13). Layout now accepts a `Status` with a line feed and gives it at most two rows (`layout.go`).
- Sketches S6, S7, S14 and S15 show an empty row between a live reply and the status row. The model adds it as the last row of the tail when a session view is active.
- A block that waits for its commit would show in the first frame and make it taller than the frames after the commit. Until the first frame had time to reach the screen (`firstFrameDelay`), the tail is empty and nothing is committed.
- Footer numbers are not derivable from the sketch turn (meta line `$0.004`, footer `$0.04`, `18%`). The footer uses the session total from `Client.Usage` for the cost (two decimals) and the token total of the last reply over the context window of the current model for the percent; the bar fills the nearest of eight cells.
- `ACPLiveSession` has no "driver busy" field. The selector maps `Role == driver` to `this client drives`, `HasDriver` to `driver busy`, else `no driver`; `Subscribers` is shown as `N watching`.
- Quit paths (ctrl+c on an empty draft, ctrl+d on an empty draft, `/quit`) call one function, `requestQuit`, which only returns `tea.Quit` now. The confirmation, the detach note, the exit codes and the in-process wording belong to the next phase.
- Login next action: the selector error reuses `loginCommand` (shared with the auth error block), so the text `ask auth login <provider>` has one owner and stays as the design text.

## Done

- [x] Tests first: `TestSendClearsDraftOnlyAfterAcceptance`, `TestRefusedPromptKeepsDraft`, `TestEnterWhileRunningSteers`, `TestAltEnterWhileRunningFollowsUp`, `TestAltUpDequeuesIntoDraft`, `TestSelectorChangesModel`, `TestSelectorReadinessFailureKeepsModel`, `TestStaleSetModelIgnored`, `TestReconnectStatesGoldens`, `TestFooterUsageThresholds`, `TestScreens120Goldens`, and tests for the session selector, observer, take, new, thinking keys, retry countdown, startup, resync, quit keys and animation clock.
- [x] Send states: draft kept while sending, cleared on acceptance only for an unchanged revision, `✗ Not sent:` with the kind, outcome-unknown wording after a lost link.
- [x] Run states: `Working… Ns`, retry countdown, cancelling until settle, steer on enter, follow-up on alt+enter, pending rows, alt+up by input id.
- [x] Footer: link, directory, model with level, usage thresholds at 70% and 90%, short form under 60 columns, directory and model name cut when the row is too wide.
- [x] Model selector (idle only), readiness failure inside the box, shift+tab cycling, ctrl+t toggle with rebuild.
- [x] `/sessions` selector (observer view), `/take`, `/new`, in-process wording.
- [x] Connection states S13, S13a, S13b; `Reconnecting, please wait…` on enter; stale results ignored by generation.
- [x] Verification: `go test -race ./internal/tui/...`, `go vet ./...`, golangci-lint on `internal/tui`, `go test ./...`.
