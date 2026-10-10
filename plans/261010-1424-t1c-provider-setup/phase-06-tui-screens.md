---
phase: 6
title: "TUI screens and design amendments"
status: pending
priority: P1
effort: "3d"
dependencies: [1, 5]
---

# Phase 6: TUI screens and design amendments

Outline with scouted evidence. Run a scout pass and update this file before cooking. Citations into `internal/tui` are against the working tree of 2026-10-10, which has uncommitted T1a edits.

## Goal

Every state of the [re-approved design](../261010-1424-t1c-provider-setup-design/design.md) (P1 to P31 and the new states) renders as sketched at 80×24, 40×14 and 120×40, with the keys of design section 6, and the section 12 amendments are applied to the existing T1a goldens and tests. `internal/tui` keeps its import boundary.

## Evidence (scouted 2026-10-10)

- Command list and case-insensitive lookup: `completion.go:21-29` (`DefaultCommands`), `:47-74` (`matches` lowers the draft, `lookup` lowers the name). `/providers` goes after `/model` (design P2a).
- Input order: `onKey` sends ctrl+c to the host, then the selector, then the editor, then shortcuts (`update_input.go:27-53`). Paste goes to the editor unless a selector is open (`:55-61`). ctrl+c closes a selector before it clears or quits (`:66-80`).
- Send path: `submit` (`update_input.go:96-113`) is where a send with no model is refused before any call (P2, P23). The check goes before the `s.run.running` branch (`:108-109`), so no steer or follow-up is sent either.
- `TEA_TRACE` makes Bubble Tea log raw input, including a bracketed paste, to a file (`third_party/bubbletea/tea.go:641-646`, `tty.go:75`). That would put a pasted key in a file. D14 (option B): `TEA_TRACE` stays usable and the TUI warns instead. `cmd/tui/interactive.go` passes the value of `TEA_TRACE` (the file name) in the UI options; `internal/tui` does not read the environment. The warning names the file (design P28b, P6a).
- `needsLeaderText` exists (`update_input.go:22`) for P24.
- Selector: one component for model and session lists (`selector.go:11-73`), model rows built in `newModelSelector` (`:288-327`) with the `(<api>)` label rule; filter row at the top; `… N more` window. The provider box differs (search row at the bottom, status suffix at the right, row menu), so it is its own component that reuses the row and window helpers.
- Dialog scaffold: `dialog.go` (comment only) and `FocusDialog` (`focus.go:13-17`) exist for this purpose.
- Footer: `footerText` and `modelPart` (`footer.go:109-159`); `no model` replaces model, bar and cost.
- Rail: `statusText` and notices (`status.go:13-99`; `notice` field `session.go:147`). Design adds a priority: send refusal, pending wait, 3 s result, no-model notice (`design.md:1445-1446`).
- Model state: `s.modelID` is set from state on refresh (`update_session.go:706-716`) and from `SetModel` (`update_input.go:401-404`).
- Scrollback blocks: startup header (`blocks.go:131-139`), header commit (`update_session.go:295`), session divider (`blocks.go:501`), restore note (`reconnect.go:26`).
- Clipboard: `tea.SetClipboard` returns a command (`third_party/bubbletea/clipboard.go:28-30`), so a test can read the copied text from the command. Hyperlinks: `lipgloss` v2.0.5 `Style.Hyperlink` (module `style.go`). The TUI sets no window title and no bell today (no match in `internal/tui`).
- Prompt history is in memory only (`editor_history.go:90-130`); undo and kill ring `:25-88`. No file write exists in `internal/tui`.

## Design

| Part | Files | Notes |
|---|---|---|
| Command | `completion.go` | `/providers  Add or remove providers` after `/model` |
| Provider box | `providers_box.go` (new) | Rows from `ACPProvidersResult`; window `clamp(height − 11, 3, 8)`; `… N more`; search row always last; name cut with `…` before the suffix; row menu (P5); P21 error rows; loading and load-failure rows; in-place update on providers changed (keep selection by provider and method id, else nearest; keep filter) |
| Dialogs | `dialog.go`, `login_box.go` (new) | Key box, browser box, copy-code box, device box, failure, cannot start, leader lost, removal confirm (with the active-run row). Title carries `esc cancel` under 6 rows. URL rows: cut by columns at `width − 5`, at most `height − 8` rows, then `… ctrl+y copies the full URL`; every URL row is one OSC 8 link to the full URL |
| Secret input | `secret_input.go` (new) | Own byte buffer; shows `(hidden)` only; paste trims trailing CR, LF, space; refuses inner line break or space and over 16 KiB without echo; ctrl+u clears; arrows, home, end ignored; buffer zeroed and dropped on send, cancel, refusal, close |
| Login flow | `update_providers.go` (new) | Calls through `Client`; each login has its login id and a request counter; a result with an old id, request or generation changes nothing; esc or ctrl+c sends the cancel, and the note follows the cancel answer: `⊘ … cancelled. Nothing was saved.` when `cancelled`, the P9 or P14 note when the key or sign-in was already saved; ctrl+c never counts toward quit; ctrl+d ignored while a box is open; detach and signals cancel an open login first. P22 note rows come from the logout result (`clearedModel`), not from event order |
| Notes | `blocks.go` | Success note (`✓`, second muted row at four spaces), cancel note (`⊘`); client-local blocks, drawn again on resize |
| Model line | `model_line.go` (new), `update_session.go`, `reconnect.go` | Start line after the header, at the end of the transcript after `/new` (the header is not drawn again), after the session divider, after the restore note (drawn once, from the state read after the follow baseline); change line on a `model_update` event of the current epoch whose sequence is after that baseline, so a missed change that a resumed follow delivers draws no second line (P31); `· was` only from 60 columns and when it fits; no label under 60 columns; `(<api>)` when two rows share provider and id, on its own row when the row is too narrow; nothing when no model |
| `/model` | `selector.go`, `update_input.go` | Rows only from a filtered answer (`usableOnly` echo); group header per provider (dot, id, muted bold), skipped by the cursor, shown only when a row matches; P26 empty box with no filter row; an old leader (no echo) shows all rows as in T1a |
| First run and no model | `footer.go`, `status.go`, `update_input.go` | `no model` footer; persistent rail notice until a model exists; send refused with draft kept; P24 rows in process |
| Rail priority | `status.go` | One notice at a time in the design order; the no-model notice comes back when a higher one ends |

## Files owned

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/tui/providers_box.go` | create | ~260 | `providers_box_test.go` |
| `internal/tui/login_box.go` | create | ~320 | `login_box_test.go` |
| `internal/tui/secret_input.go` | create | ~110 | `secret_input_test.go` |
| `internal/tui/model_line.go` | create | ~90 | `model_line_test.go` |
| `internal/tui/update_providers.go` | create | ~280 | flow tests |
| `internal/tui/dialog.go`, `completion.go`, `selector.go`, `footer.go`, `status.go`, `blocks.go`, `session.go`, `update_input.go`, `update_session.go`, `reconnect.go`, `translate.go` | modify | +250 total | existing tests updated |
| `internal/tui/providers_box_test.go`, `login_box_test.go`, `secret_input_test.go`, `model_line_test.go`, `first_run_test.go` | create | ~1,200 total | — |
| `internal/tui/session_rig_test.go` | modify: scripted provider answers (after phase 5 added the methods) | +80 | — |
| `internal/tui/screen_test.go`, `session_test.go`, `blocks_screen_test.go`, `model_test.go` | modify: amendments | +60 | — |
| `cmd/tui/interactive.go` | modify: pass `traceFile` (the value of `TEA_TRACE`) into the UI options; `TEA_TRACE` itself is kept (D14) | +5 | E2E in phase 7 |
| `internal/tui/testdata/**` | new goldens; amended goldens of section 12 | ~40 files | reviewed diff per file |

## Tests first

### Tests before (regression)

`go test ./internal/tui/...` green before any change (after the T1a work is committed).

### Tests after (new, failing first)

Every test named in the [criterion map](plan.md#criterion-to-test-map) with phase 6, plus:

| Test | Proves |
|---|---|
| `TestRailNoticePriority` | Refusal over wait over result over no-model notice; the notice returns |
| `TestProviderBoxHintDropsFromRight` | The hint row drops `your draft is kept` first, then `delete remove` or `ctrl+y …`; `enter` and `esc` stay |
| `TestLoginBoxTitleCarriesCancelWhenLow` | Under 6 rows the title reads `Sign in to openai · esc cancel` |
| `TestURLRowsAreOneHyperlink` | Each URL row carries the same OSC 8 target, the full URL |
| `TestCtrlDIgnoredWhileBoxOpen` | ctrl+d does nothing with a provider or login box open |
| `TestProvidersOpenDuringRunModelWaits` | `/providers` opens during a run; `/model` still waits |
| `TestProvidersConnectionLostKeepsList` | With the link down the box keeps its rows and actions show `Reconnecting, please wait…` |
| `TestSignalCancelsOpenLogin` | A detach by signal sends one cancel for an open login |
| `TestEscDuringKeyCheckNoteFollowsCancelAnswer` | esc in P7: `⊘` note when the leader answers `cancelled`; P9 note when it answers already saved |
| `TestNoModelRefusesSteerAndFollowUp` | With no model, enter sends no prompt, steer or follow-up, also while the view thinks a run is active |
| `TestRestoreDrawsOneModelLine` | A resumed `model_update` at or below the restore baseline draws no change line; one start line follows the restore note |
| `TestAlibabaKeyBoxSaysNotChecked` | The Alibaba key box shows `This provider cannot be checked before use; the key is saved as it is.` (P6b) |
| `TestKeySavedNotCheckedNote` | An outcome with `notChecked`: scrollback `✓ alibaba-token-plan: Saved. This provider cannot be checked before use.` then `Choose a model with /model.`; rail `✓ alibaba-token-plan key saved · 1 model · choose one with /model`; the row then reads `api key` (P9a) |
| `TestUncheckedKeyRefusedAtFirstPrompt` | An auth failure (HTTP 401) of a provider that cannot be checked shows `✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).` and `Replace the key with /providers. Then send again.` (P9b) |
| `TestTraceWarningOnKeyBox` | With `traceFile` set, the key box shows the P6a warning above the `key` input, with the file name |
| `TestTraceWarningOnCodeInput` | With `traceFile` set, the address box (P15) and the code box (P15a) show the warning |
| `TestTraceWarningOnceAtStart` | With `traceFile` set, the P28b rows go to scrollback once, under the start line (or the header with no model), and not again after a resize rebuild |
| `TestNoTraceWarningWhenUnset` | Without `traceFile`, no row contains `TEA_TRACE` |
| `TestModelSelectShowsDefaultSavedStatus` | `/model` sends `saveDefault`; with `defaultSaved` the rail shows `✓ Default model: <pair>` for 3 s (P29); without it, no default status |
| `TestModelSelectorTagsDefault` | A saved default that is not current shows `default` (muted); the current row keeps `current`; one tag when they are the same (P25a) |
| `TestFallbackNoticeNamesModelInUse` | A start state with `modelFallback` draws the start line of the model in use and under it `Default model <pair> is not usable (not signed in).`; at 40×14 the reason wraps with a two-column indent (P28c) |
| `TestAnthropicAddressAndCodeBoxes` | Kind `browser_address` shows the URL and the masked `address` input (P15); kind `copy_code` shows the busy-port text and the masked `code` input (P15a); the checking rail names address or code (P16) |
| `TestProvidersSignInDoesNotChangeDefault` | A sign-in success shows no default status |
| `TestLoginCancelledByRemovalShowsReason` | An outcome `cancelled/removed` closes the login box with the design's reason text (another client removed the provider); no `Nothing was saved` from esc |
| `TestOwnChangeShowsNoChangedRail` | The broadcast of a change that this client made (same generation as its own outcome) shows no `Providers changed in the leader` rail and no second note (P27 is for other clients) |
| `TestSignInCountComesFromRefreshedList` | `N models` comes from the model list fetched after the outcome; a failed fetch shows `model list not refreshed` (design P9, P14 text) |

Amended T1a tests and goldens (design section 12): `TestScreenGoldens`, `TestScreens120Goldens`, `TestReconnectStatesGoldens/restored`, `TestSessionsSelectorAttachesAsObserver`, `TestSelectorChangesModel` (change line), `TestSelectorReadinessFailureKeepsModel` (usable provider only). Each golden diff may change only the model line rows; review every file before it is accepted.

### Refactor

Extract the row and window helpers of `selector.go` (`itemRow`, `rowBudget`, `formatWindow`) so the provider box reuses them; the model and session selectors keep their goldens.

## Steps

1. Scout. The design has the D16 states (P6b, P9a, P9b; criteria 59, 60). The design is approved again and the [criterion map](plan.md#criterion-to-test-map) is synced to its criteria 1-58 (2026-10-10); re-check both before cooking.
2. Secret input and its security tests first (criteria 9, 21, 22, 36-40, 43).
3. Provider box, row menu, removal; then login boxes and the flow handlers.
4. `/model` grouping and empty state; footer and rail; first run.
5. Model line and the amendments to the T1a goldens.
6. Width checks at 40×14, 80×24, 120×40; resize test.

## Verification

```
go test -race ./internal/tui/...
go vet ./internal/tui/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/tui/...
```

Golden updates use the package's update flag per test, never for the whole package at once, and each diff is read before commit.

## Rollback

Revert the phase commit. The leader side stays compatible; the T1a goldens come back with the revert.

## Risks

| Risk | Mitigation |
|---|---|
| A secret reaches the editor through a paste path | Paste is routed to the active dialog first; `TestSecretNotInUndoOrKillRing`, `TestSecretNotInPromptHistory` |
| A hyperlink sequence breaks width math | Width is measured on visible text; `TestURLRowsAreOneHyperlink` with the emulator grid |
| The provider box and the selector diverge in behavior | Shared helpers; separate goldens |

## Security

The secret buffer is the only place a key or code lives in the UI. It never reaches the editor, history, rings, blocks, notes, notices, the clipboard command or a log. Tests scan the emulator grid after every frame of the key flow for the fake value.

Stated exception (D14): when the user sets `TEA_TRACE`, Bubble Tea writes raw input, a pasted key included, to the trace file. The TUI cannot prevent this; it warns on the key box, the code input and once at start. The design states this exception in its security criteria (36-43 hold when `TEA_TRACE` is not set).

## Todo

- [x] Design re-approved; criterion map synced in plan.md (2026-10-10)
- [ ] Scout
- [ ] Default status, fallback notice, trace warning, removal reason (D12, D14, D15)
- [ ] Secret input and security tests
- [ ] Provider box, row menu, removal
- [ ] Login boxes and flow
- [ ] `/model`, footer, rail, first run
- [ ] Model line and T1a amendments
- [ ] All phase 6 criterion tests green
