---
phase: 6
title: "TUI trust prompt and /trust"
status: completed
effort: 6h
depends_on: [4, 5]
---

# Phase 6: TUI trust prompt and /trust

## Context links

- Approved `plans/261103-2158-h6a-settings-trust/design.md` (the contract; do not start without `design-status: approved`)
- `internal/tui/client.go:90-140` (`tui.Client` interface; `NewSession` at line 98), `internal/tui/session_rig_test.go:159` (fake client)
- `internal/tui/update_input.go:155`, `internal/tui/update_session.go:242` (the two `NewSession` calls: first session and `/new`)
- `internal/tuiclient/trust.go` (phase 4)
- `cmd/tui/interactive.go:52-79` (`runInteractive`), `cmd/tui/tui_client.go:24-55,58-98` (`tuiClientOptions`, `openTUIClient`)
- `docs/tui-architecture.md`, `internal/tui/README.md`

## Overview

Priority P1. Implement the approved design: ask the trust state before the first session and before `/new`, show the prompt when `needsDecision`, send the choice, then create the session with the right override. Add `/trust`, the untrusted notice and the diagnostics notice. Pass `--approve`/`--no-approve` from the command line to the client.

## Key insights

- `internal/tui` reads no file (`plans/261102-1800-thinking-fold-setting/plan.md`, architecture rules). All trust data comes through `tui.Client`.
- In process, the TUI uses the same `tuiclient` code over a pipe (`cmd/tui/tui_client.go:100-135`); phase 4 set `TrustRequests` there, so the prompt works with no leader.
- A flag on the command line skips the prompt: the TUI does not ask the state for the decision, and sends the override on every `session/new` of this process.
- An old leader answers "not available": no prompt, no notice.
- A trusted project may override `thinkingFold`. The first frame uses the user file (`cmd/tui/thinking_fold.go`). After the session exists, the TUI applies the effective `thinkingFold` from `_ask/session/state` through `tui.Client`. Test: `TestThinkingFoldFromSessionState` (a project value wins in a trusted project; an untrusted project keeps the user value).

## Requirements

1. `tui.Client` gains `TrustState(ctx, cwd)` and `SetTrust(ctx, req)` in `pkg/protocol` shapes. The fake client in `internal/tui/session_rig_test.go` gains both.
2. Before each `NewSession` (first session and `/new`): when no command-line override exists, call `TrustState`; when `needsDecision`, open the trust dialog in the prompt pane slot; the choice either calls `SetTrust` (saved choices) or sets a session override (session-only choices); then `NewSession` with the override.
3. `NewSession` in the interface takes the override (the two call sites at `update_input.go:155` and `update_session.go:242` pass it).
4. `/trust` opens the selector from the design; a change calls `SetTrust` and shows that it applies to the next session.
5. After the session starts, `State` gives `trust` and `diagnostics`; the TUI shows the notices from the design.
6. `cmd/tui`: `runInteractive` receives `options.trust`; `tuiClientOptions` carries it to `tuiclient.Config`.
7. `/trust` in docs and in the TUI help list (phase 7 writes the docs).

## Related code files

Modify: `internal/tui/client.go`, `internal/tuiclient/client.go` (`NewSession` takes the override and uses the phase 4 helper), `internal/tui/update_input.go`, `internal/tui/update_session.go`, `internal/tui/session_rig_test.go`, the dialog and selector files named by the approved design (new file `internal/tui/trust_dialog.go` if the design adds a component), `internal/tui/README.md` (component owner row), `cmd/tui/interactive.go`, `cmd/tui/tui_client.go`, `cmd/tui/headless.go` (pass `o.trust` to `runInteractive`).

Create: `internal/tui/trust_dialog_test.go` and goldens under `internal/tui/testdata/` as the design needs.

## Implementation steps

1. Confirm `design-status: approved`.
2. Write goldens and unit tests from the design acceptance criteria (Tests Before).
3. Interface and fake client.
4. Dialog, selector, notices.
5. Flag plumbing.
6. Refresh goldens only after a diff against the design sketch.
7. Regression gate.

## Tests Before (written first, fail first)

| Test | Checks |
|---|---|
| `TestTrustPromptShownForUndecidedProject` | fake client `needsDecision`: the dialog renders in the prompt pane slot; golden at 80x24 |
| `TestTrustPromptChoiceSavesAndStartsSession` | "Trust" calls `SetTrust` then `NewSession` with no override |
| `TestTrustPromptSessionOnlyChoiceSendsOverride` | session-only choice: no `SetTrust`; `NewSession` override `approve` or `deny` |
| `TestTrustPromptCancelIsUntrustedSession` | Escape: no `SetTrust`; override `deny` |
| `TestTrustPromptSkippedWithFlag` | override set: no `TrustState` call |
| `TestTrustPromptSkippedForOldLeader` | "not available": no dialog, no notice |
| `TestNewCommandAsksTrustAgain` | `/new` calls `TrustState` |
| `TestTrustCommandAppliesToNextSession` | `/trust` change calls `SetTrust` and shows the next-session text |
| `TestUntrustedNoticeShowsSource` | each untrusted source gives the design text |
| `TestSettingsDiagnosticsNotice` | a denied-key diagnostic shows as designed |
| Long path and CJK path goldens | the 40-column case from the design |

## Refactor

`tui.Client.NewSession` signature. Protected by the existing `internal/tui` session tests and goldens (run before and after).

## Tests After

All tests above and existing `internal/tui` and `cmd/tui` tests pass; goldens match the design sketches.

## Regression gate

```text
go test ./internal/tui ./internal/tuiclient ./cmd/tui
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

Golden refresh (only after a diff against the design): `go test ./internal/tui -run '^TestTrustPromptShownForUndecidedProject$' -update`.

## Todo

- [ ] Design approved
- [ ] Tests Before
- [ ] Interface and fake
- [ ] Dialog, selector, notices
- [ ] Flag plumbing
- [ ] Regression gate green

## Success criteria

Every acceptance criterion of `design.md` has a test or a golden that passes.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| The design cannot work in code | L x M | Stop, set the design to `draft`, ask the user (skill hard rule) |
| `NewSession` signature change breaks the fake client and goldens | M x L | Change both in one commit; goldens diffed |

## Security considerations

The TUI never writes `trust.json`; it only sends `SetTrust`. Cancel never grants.

## Rollback

Revert. Phase 4 behavior (no prompt, untrusted unless decided) stays.

## Next steps

Phase 7 documents `/trust` and the flags; phase 8 runs the prompt on real terminals.
