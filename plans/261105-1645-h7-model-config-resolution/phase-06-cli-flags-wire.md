---
phase: 6
title: "CLI flags, enabledModels and wire"
status: pending
priority: P1
effort: "2.5d"
dependencies: [1, 4, 5]
---

# Phase 6: CLI flags, enabledModels and wire

## Goal

`--model` takes the full pattern with `:level`, `--models` and the `enabledModels` setting give a scope, `--offline` turns the model refresh off, the initial model follows the Pi priority, a dropped thinking level gives a notice, and the TUI passes these flags to its host, which echoes what it applied.

## Context

- Decisions: D11 (initial priority H-PROV-19), D12, D13, M8, M9, M10, M13, M15, M16 ([decisions.md](./decisions.md)).
- Flags: `valueFlags` (`cmd/tui/args.go:103-139`); `--provider` default `faux` (`cmd/tui/args.go:92-95,174`); model defaults per provider (`cmd/tui/args.go:257-273`); usage text (`cmd/tui/args.go:298-330`). `TestArgs` calls `parseArgs` with no model rows (`cmd/tui/args_test.go:16`).
- The TUI does not pass `--model` to its host today: `runInteractive` gets only `noLeader`, trust and session flags (`cmd/tui/headless.go:150-151`, `cmd/tui/interactive.go:61`).
- Session flags on the wire: `ACPSessionOptions` (`pkg/protocol/acp_session.go:19-39`). The host decodes them with plain `json.Unmarshal`, so an older host drops unknown fields with no error (`internal/acp/session_options.go:24-50`), and reads them once (`internal/acp/agent.go:309-316`).
- `ACPModelRequest` has no level; `ACPThinkingRequest` is a second call (`pkg/protocol/acp.go:58-70`). `setModel` holds the session guard for one call (`internal/acp/ask_methods.go:235-239`).
- Models result: `ACPModelsResult` (`pkg/protocol/acp.go:221-226`).
- Start choice: `startChoice.pick` (`internal/app/default_model.go:104-131`); print-mode saved default (`cmd/tui/headless.go:192-205`).
- Offline env: `modelRefreshOn` (`cmd/tui/acp.go:120-127`); `ask acp` accepts only trust flags (`cmd/tui/acp.go:47-61`); `ask leader` args (`cmd/tui/leader.go:63`); spawned leader env (`internal/leader/spawn.go:239-279`).
- Settings keys: `shippedKeys`, `decodeEffective`, `Effective.set` (`internal/settings/settings.go:211-212,410-474`); settings show order (`cmd/tui/settings_command.go:243-257`). Phase 1 changed the reader in the same file before this phase.

## Requirements

1. **`--model` pattern.** `--model` keeps the raw string. `--provider P --model M` means the pattern `P/M`. `--model p/id` with no `--provider` sets the provider from the resolved row. The `faux` default applies only when neither flag and no saved default exist. `modelGiven` keeps its meaning (`cmd/tui/args.go:68-70`). The provider defaults of `args.go:260-273` stay for `--provider` alone.
<!-- Red Team 2026-11-06: Finding 7 -->
2. **Level after resolution.** `parseArgs` does not check the level of a pattern, because it has no rows. The resolver (phase 4, `AllowInvalidLevel` false) runs after the model list exists; a pattern that does not resolve is an exit-1 error that names the pattern and, when the text before the last colon resolves, lists the valid level words. A `:level` from the pattern sets the thinking level unless `--thinking` is given (M8). `--thinking` itself stays a flag check in `parseArgs`.
<!-- Red Team 2026-11-06: Finding 13 -->
3. **`--api-key`.** The rule "requires a model" (`cmd/tui/args.go:257-259`) also accepts a provider from the pattern. Phase 2 decides where the key may go.
4. **`--models a,b,...`** (repeatable, comma list). Not valid with `ask acp` (its flags come per session).
5. **`enabledModels`** setting: an array of patterns or globs in `settings.json`, user or project layer of `settings.json`. Add it to `shippedKeys`; a bad value is a diagnostic and the key is ignored; `ask settings show` prints it.
6. **Initial model priority** (H-PROV-19, D11), in `startChoice.pick` and print mode:
   1. `--provider`/`--model` (resolve error ends the run, exit 1; in the TUI the session start fails with the message).
   2. First model of the scope from `--models`, else from `enabledModels`. When the saved default is in the scope, the saved default wins (M15; user QD 2026-11-05). Session restore is H8 and is not in this plan.
   3. Saved default (`provider/id@api`, exact, M13).
   4. First usable model (existing `firstUsable`, with the usable rule of phase 5).
   A scope entry that matches nothing is a warning line, as other settings problems are.
<!-- Red Team 2026-11-06: reasoning notice (merged) -->
7. **Dropped level notice (M16).** When the clamp of the chosen row drops a requested level (for example `local/x:high` on a row with `Reasoning false`), print mode writes one warning line (`Warning: <provider/id> has no thinking; level high is not used.`) and the TUI host adds the same text as a session notice. The level then follows the row.
<!-- Red Team 2026-11-06: Finding 11 -->
8. **Wire, all `omitempty`.**
   - `ACPSessionOptions` gets `Model string`, `Thinking ThinkingLevel`, `Models []string`; `IsZero` checks them (`pkg/protocol/acp_session.go:36-39`).
   - `flagsFromMeta` bounds them: `Model` at most 256 bytes; `Models` at most 64 entries of at most 256 bytes; `Thinking` one of the seven level words. A bad value is `invalid_params` with a fixed text.
   - The new-session result `_meta` echoes the options that the host applied (`appliedOptions`: `model`, `thinking`, `models`). The TUI client fails the session start with `the leader did not apply --model; restart it with this build` when it sent one of these fields and the echo lacks it. An older host sends no echo, so an older leader never drops `--model` in silence.
   - `ACPModelsResult` gets `Scope []ACPScopedModel{ModelID, ThinkingLevel}` (the cycle list in order; `ModelID` is the resolved `provider/id@api`, never the raw pattern) and `Diagnostics []ACPDiagnostic` (model file problems found when the list was read).
   - `ACPModelRequest` gets `ThinkingLevel ThinkingLevel` (`omitempty`). `setModel` applies the model and the level under one guard and echoes the applied level in `ACPSetModelResult` (it already carries `ThinkingLevel`).
   An older peer ignores every new field.
9. **Host side.** The Factory resolves `Model`/`Models` against the snapshot (phase 5). The session keeps the raw scope patterns next to its flags; `models()` expands them against the snapshot of that call and fills `Scope`, else uses `enabledModels`, and fills `Diagnostics` from the snapshot. `setModel` stays exact for the model id (M13).
10. **TUI client side.** `runInteractive` puts `--model`, `--thinking` and `--models` into `ACPSessionOptions` for the first session and checks the echo. No TUI rendering change here.
11. **`--offline`** on `ask`, `ask acp` and `ask leader`: the process acts as with `ASK_DISABLE_MODEL_REFRESH=1`. A TUI that starts a leader passes `ASK_DISABLE_MODEL_REFRESH=1` in the child environment. A leader that already runs keeps its setting (M10). Print mode makes no refresh request today, so the flag changes nothing there; it is accepted for Pi compatibility. `ask auth login` keeps the environment variable only (`docs/cli.md:490` lists it); `docs/cli.md` says so.

## Files

| Action | Path | Notes |
|---|---|---|
| Modify | `cmd/tui/args.go`, `cmd/tui/args_test.go` | Flags, usage; no level check of a pattern |
| Modify | `cmd/tui/headless.go`, `cmd/tui/headless_faux.go` | Print-mode priority, resolve after rows, notice |
| Modify | `cmd/tui/interactive.go` | Session options from flags; echo check |
| Modify | `cmd/tui/acp.go`, `cmd/tui/leader.go` | `--offline` |
| Modify | `pkg/protocol/acp_session.go`, `pkg/protocol/acp.go` | Wire fields |
| Modify | `internal/acp/session_options.go` | Bounds and level check <!-- Red Team 2026-11-06: Finding 11 --> |
| Modify | `internal/acp/agent.go`, `internal/acp/ask_methods.go` | Echo, scope, diagnostics, model and level under one guard |
| Modify | `internal/app/default_model.go`, `internal/app/module_acp.go` | Priority with scope; notice |
| Modify | `internal/settings/settings.go`, `internal/settings/settings_test.go` | `enabledModels` key |
| Modify | `cmd/tui/settings_command.go` | Print `enabledModels` |
| Modify | `internal/leader/spawn.go` only if the env cannot pass through `Env` | Prefer the existing `Env` field |

## Steps

1. Add the wire fields, the bounds in `flagsFromMeta`, the echo and their `IsZero` check with a round-trip test.
2. Change `parseArgs` and the usage text; extend the `TestArgs` table with flag rows only.
3. Add `enabledModels` to the settings keys.
4. Put the priority into `startChoice.pick` and the print path; resolve patterns with phase 4 against the snapshot of phase 5; add the dropped-level notice.
5. Fill `Scope` and `Diagnostics` in `models()`; apply the level in `setModel` under the same guard.
6. Pass the flags from `runInteractive` into the first session options and check the echo.
7. Add `--offline` to `ask`, `ask acp` and `ask leader`, and the child environment of a spawned leader.

## Tests

- `TestArgs` table rows (flags only): `--model anthropic/claude-x:high` (raw string kept), `--provider openai --model gpt-5.5`, `--thinking low --model a/b:high`, `--models 'anthropic/*,gpt-5.5:low'`, `--offline`, `--api-key k --model anthropic/x`.
- `TestPrintModelPatternUnknownLevel`: real `runWithDependencies` with `-p --model openai/gpt-5.5:hgih` exits 1 and lists the level words. <!-- Red Team 2026-11-06: Finding 7 -->
- `TestPrintColonTaggedModelID`: `-p --model local/qwen3-coder:30b` with a user models.json row of that id runs with no level. <!-- Red Team 2026-11-06: Finding 7 -->
- `TestPrintTokenPlanPatternWithLevel`: `-p --model alibaba-token-plan/<id>@openai-completions:high` through the model set path. <!-- Red Team 2026-11-06: Finding 12 -->
- `TestPrintDroppedLevelNotice`: `-p --model local/x:high` on a row without reasoning prints the warning line. <!-- Red Team 2026-11-06: reasoning notice -->
- `TestACPCommandAcceptsOffline`, `TestLeaderAcceptsOffline`.
- `TestEnabledModelsSetting` (settings package, stdlib test).
- `TestStartModelPriority` (app): flag beats scope beats saved beats first usable; saved inside scope wins over the first scope entry.
- `TestSessionOptionsBounds` (acp): too many `Models`, a long `Model`, a bad `Thinking`. <!-- Red Team 2026-11-06: Finding 11 -->
- `TestSessionStartFailsWhenHostDoesNotEchoModel` (tuiclient against a host that drops the field). <!-- Red Team 2026-11-06: Finding 11 -->
- `TestSetModelAppliesLevelUnderOneGuard` (acp). <!-- Red Team 2026-11-06: Finding 11 -->
- `TestModelsResultScopeOrder` (acp).
- `TestPrintModelPatternSelectsCatalogModel`: real `ask -p --model openai/gpt-5.5:low` with an injected transport; the request carries the low effort.

```
go test ./cmd/tui -run 'TestArgs|TestACPCommand|TestLeaderAccepts|TestPrintModelPattern|TestPrintColonTagged|TestPrintTokenPlan|TestPrintDroppedLevel' -count=1
go test ./internal/settings -run 'TestEnabledModels' -count=1
go test ./internal/app -run 'TestStartModelPriority' -count=1
go test ./internal/acp ./internal/tuiclient ./pkg/protocol -count=1
go vet ./...
```

## Validation

- `ask --help`, `ask acp --help` and `ask leader --help` list the new flags.
- An older TUI against this leader still works (fields are `omitempty`; the existing wire tests pass). A new TUI against an older leader fails the start with the echo message instead of a silent default.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| `--model` without `--provider` changes meaning for a bare id | Med x Med | A bare id resolves over all providers and fails as ambiguous instead of picking; the old form `--provider p --model id` keeps working |
| `--offline` expected to stop a running leader | Med x Low | Docs and help text state the scope (M10) |
| An older leader drops the new fields | Med x Med | Echo check in the client (test above) |

## Rollback

Revert the commit. The wire fields are `omitempty`, so peers built before the revert still work; `enabledModels` in a user file becomes an unknown key, which `settings` ignores.
