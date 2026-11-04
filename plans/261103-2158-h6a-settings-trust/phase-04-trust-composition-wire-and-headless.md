---
phase: 4
title: "Trust composition, wire and headless"
status: completed
effort: 8h
depends_on: [3]
---

# Phase 4: Trust composition, wire and headless

## Context links

- [plan.md](./plan.md): S8, S9, S12, S13, S15, S17, [Wire compatibility](./plan.md#wire-compatibility), [Data flow](./plan.md#data-flow)
- Composition: `cmd/tui/acp.go:125-161` (`acpParams`, used by `runACP` at `cmd/tui/acp.go:71`, `runLeader` at `cmd/tui/leader.go:250`, `openInProcessClient` at `cmd/tui/tui_client.go:104`), `internal/app/module_acp.go:81-196` (`NewACPRuntime`, `Factory` at line 135, `prefs` at 93, `startChoice` at 129, `DefaultModel`/`SaveDefault` at 155-156), `internal/app/default_model.go:39-55`
- Adapter: `internal/acp/host.go:41` (`Factory` type), `:258-271` (`Host.NewSession`), `internal/acp/agent.go:30-75` (`Config`), `:275-319` (`Adapter.NewSession`), `internal/acp/ask_methods.go:20-62` (`HandleExtensionMethod`: provider table first, then route check), `:128-145` (`state`), `:167-171` (models `default`), `:263-265` (`SaveDefault`)
- Wire: `pkg/protocol/acp.go:8-36,173-185,221-225`, `pkg/protocol/acp_providers.go:1-18`
- Router: `internal/leader/policy.go:34-82`, `internal/leader/router.go:251-275`, `internal/leader/rpc.go:158-175` (`injectRoute` keeps other `_meta` keys)
- Headless: `cmd/tui/headless.go:79-191`, `:273-283` (cwd from `os.Getwd`), `cmd/tui/args.go:28-61,116-202`, `cmd/tui/acp.go:41-50` (`parseACPArgs`)
- Client: `internal/tuiclient/client.go:447-466` (`NewSession`)

## Overview

Priority P1. Make the leader (and the two other compositions) decide trust for each session cwd, load the layered settings for that session, and expose the state and grant/revoke on the wire. Add `--approve`/`--no-approve` to `ask`, `ask -p` and `ask acp`. After this phase `ask -p`, `ask acp`, `ask connect` and an old TUI behave correctly with no prompt; phase 6 adds the prompt.

## Key insights

- All three agent hosts compose through `acpParams` and `NewACPRuntime`, so one `Factory` change covers the leader, `ask acp` and the in-process TUI. Headless `ask -p` does not use ACP (`internal/acp/README.md:128`); it calls the same app helper directly.
- The in-process TUI link has no route (`cmd/tui/tui_client.go:100-135`). The switch that serves `_ask/trust/*` is therefore an `ACPParams` field, not `RequireRoute`.
- `HandleExtensionMethod` checks the provider table before the session route check (`ask_methods.go:21-27`). The trust table goes in the same place, because trust calls carry no session.
- The router needs only two policy entries. An unlisted method is already `unsupported` (`router.go:251-254`); other `_meta` keys pass (`rpc.go:158-175`).
- `get_available_models` names the saved default through `Config.DefaultModel(ctx)` with no cwd (`ask_methods.go:167-171`). With a project layer the default depends on the session, so the callback takes the session cwd. The handler has the session (`ask_methods.go:153`).

## Requirements

### Wire (`pkg/protocol/acp_trust.go`, new)

1. Methods: `ACPTrustState = "_ask/trust/state"`, `ACPTrustSet = "_ask/trust/set"`.
2. Meta key: `ACPTrustMetaKey = "ask.dev/trust"` in `session/new` `_meta`, value `"approve"` or `"deny"`. Any other value is `invalid_params`.
3. `ACPTrustStateRequest{cwd}`. `ACPTrustStateResult{root, trusted, source, needsDecision, gated[], entryPath, warning}`. `source` uses the phase 1 words. `warning` is a fixed text plus the path (for example a corrupt store), never file content.
4. `ACPTrustSetRequest{cwd, target: "folder"|"parent", decision: "trust"|"distrust"|"clear"}`. The result is the new `ACPTrustStateResult`. "parent" with "trust" writes the parent entry and deletes the folder entry (Pi `trust-manager.ts:72-83`).
5. `ACPStateResult` gains optional `trust *ACPSessionTrust{trusted, source}` and `diagnostics []ACPDiagnostic{layer, path, key, text}`.
6. `ACPSetModelResult` gains optional `defaultNotSaved` with fixed words `shadowed`, `unsafe_file`, `failed`.

### Adapter (`internal/acp`)

7. `Factory` takes `SessionRequest{ID, Cwd, Trust}` (Trust = override word or empty). `Host.NewSession(ctx, cwd)` stays as a wrapper over a new `Host.NewSessionWith(ctx, SessionRequest)`, so the 19 existing test calls of `Host.NewSession` do not change.
8. `Adapter.NewSession` reads `_meta["ask.dev/trust"]` (any link) and passes it on.
9. `Config.StartTrust func(sessionID) (*protocol.ACPSessionTrust, []protocol.ACPDiagnostic)`: called once after the Factory, as `StartFallback` is (`agent.go:67-70`, `:297-299`). The session keeps the values; `state` reports them.
10. `Config.TrustSetup` interface with `State(ctx, cwd)` and `Set(ctx, req)`, in protocol shapes. Nil means both methods answer `unsupported`. The trust table is handled before the session route check; a present route must still be valid.
11. `Config.DefaultModel` takes the session cwd. `SaveDefault` returns a reason word; `set_model` puts it in `defaultNotSaved`.

### Composition (`internal/app/trust.go`, new; `module_acp.go`)

12. `app.ResolveTrust(home, userHome, cwd, override, ui bool) (TrustOutcome, error)`: `ProjectRoot` then `GatedFiles` then `TrustStore.Read` then `NewLayered(home, Project{}).UserLayer` (a `Layered` with no project is the user-only view; it reads the user file only, for `defaultProjectTrust`) then `NearestEntry` then `Decide`. It never opens a project file.
13. `ACPParams` gains `TrustOverride string` (process-wide, from `ask acp` flags) and `TrustRequests bool` (serve `_ask/trust/*`). `Factory`: per-session override wins over `TrustOverride`; `ui = false` (the client asked before `session/new` if it can ask); then `NewLayered(home, Project{root, trusted})`; `startChoice.saved` reads that session's `Effective.DefaultModel`; trust and diagnostics go to `StartTrust`.
14. `TrustSetup.State` runs `ResolveTrust` with `ui = true`, so `needsDecision` is true only for source `prompt`. `Set` canonicalizes the target and calls `TrustStore.Update`; a corrupt store gives an error kind `invalid_state` with the warning.
15. `SaveDefault(ctx, cwd, id)` writes user scope through `Layered.Update` with the session project; `ErrShadowed` maps to `shadowed`.
16. The leader writes each settings and trust diagnostic to its log once per session.

### Router (`internal/leader/policy.go`)

17. Add `protocol.ACPTrustState` and `protocol.ACPTrustSet` with kind `callAuthenticate` (forward with the client id, no session, `router.go:265-266`). Add a comment that this kind means "no session". No other router change.

### CLI (`cmd/tui`)

18. `args.go`: `--approve`, `-a`, `--no-approve`, `-na` set `options.trust`. Both given: error "use only one of --approve and --no-approve" (exit 1). Help text lists them.
19. `acp.go`: `parseACPArgs` accepts the same four flags and `--help`; anything else stays an error. `acpParams` takes the override; `runACP` sets `TrustOverride`; `TrustRequests` stays false.
20. `leader.go`: `TrustRequests = true`, no override. `tui_client.go` in-process: `TrustRequests = true` (wired here; the prompt that uses it is phase 6).
21. `headless.go` print mode: `ResolveTrust(..., ui=false)` with the flag; `newNativeAgentOn` receives the `Effective` value; one stderr line when the project has gated files and is untrusted: `Warning: project settings ignored: <root> is not trusted. Use --approve, or save a decision in the TUI with /trust.` Each diagnostic is one `Warning: settings: ...` line. In `--mode json` the same lines go to stderr.
22. `internal/tuiclient`: `TrustState(ctx, cwd)`, `SetTrust(ctx, req)` (file `trust.go`), and an unexported session-create helper that adds `_meta["ask.dev/trust"]` when it gets an override. The exported `NewSession(ctx, cwd)` keeps its signature in this phase and sends no override; phase 6 changes the `tui.Client` signature and uses the helper. A leader that answers `unsupported` gives a typed "not available" result, not an error screen.

### Optional (only when the user confirms S17)

23. `defaultThinkingLevel`: the start level of `newNativeAgentOn` when `--thinking` is empty (`headless.go:308-311`), through `acpParams` `NewAgent`.

## Architecture

```text
TUI/connect/editor --session/new{cwd,_meta trust?}--> router (policy) --> Adapter.NewSession
  --> Host.NewSessionWith(SessionRequest) --> Factory --> app.ResolveTrust --> NewLayered --> startChoice --> agent
  --> StartTrust(id) --> Session{trust, diagnostics} --> _ask/session/state
TUI --_ask/trust/state{cwd}--> router (no session) --> Adapter trust table --> app TrustSetup.State
TUI --_ask/trust/set{...}----> ... --> app TrustSetup.Set --> settings.TrustStore.Update
ask -p --> app.ResolveTrust(ui=false) --> NewLayered --> newNativeAgentOn
```

## Related code files

Create: `pkg/protocol/acp_trust.go` (+ `acp_trust_test.go`), `internal/acp/trust_methods.go` (+ test), `internal/app/trust.go` (+ `trust_test.go`), `internal/tuiclient/trust.go` (+ test).

Modify, with every caller of a changed signature:

| Change | Sites |
|---|---|
| `Factory` type takes `SessionRequest` | `internal/acp/host.go:41` (type), `:267` (call), `internal/acp/agent.go:32` (field), `internal/app/module_acp.go:135`, tests `internal/acp/adapter_peer_test.go:70`, `internal/acp/host_binding_test.go:22`, `internal/acp/host_test.go:22`, `internal/acp/host_test.go:145`, `internal/acp/stdio_test.go:121` (the `f` passed there) |
| `Host.NewSessionWith` added, `NewSession` wraps it | `internal/acp/host.go:258`; caller `internal/acp/agent.go:294` moves to `NewSessionWith` |
| `Config.DefaultModel` takes cwd | `internal/acp/ask_methods.go:169`, `internal/app/module_acp.go:155`, test doubles found by `rg -n 'DefaultModel:' internal/acp internal/app` |
| `Config.SaveDefault` returns a reason | `internal/acp/ask_methods.go:263-265`, `internal/app/module_acp.go:156` |
| `startChoice.saved` per session | `internal/app/default_model.go:42-43`, `internal/app/module_acp.go:129,136` |
| `ACPParams` fields | `cmd/tui/acp.go:144-160`; set in `cmd/tui/acp.go:71`, `cmd/tui/leader.go:250`, `cmd/tui/tui_client.go:104` |
| Policy entries | `internal/leader/policy.go:34` |
| Flags | `cmd/tui/args.go:28-61,116-202,204-226`, `cmd/tui/acp.go:25-50` |
| Print mode | `cmd/tui/headless.go:155-169,278-283` |

## Implementation steps

1. Write "Tests Before".
2. Protocol types and constants.
3. Adapter: `SessionRequest`, `NewSessionWith`, meta read, `StartTrust`, trust table, `DefaultModel` cwd, `SaveDefault` reason.
4. App: `ResolveTrust`, `TrustSetup`, `Factory`, `startChoice` per session.
5. Router policy entries.
6. CLI flags, `acpParams`, print mode lines.
7. `tuiclient` trust calls and session meta.
8. Optional step 23 only after user confirmation.
9. Regression gate.

## Tests Before (written first, fail first)

| Test | Package | Checks |
|---|---|---|
| `TestTrustWireRoundTrip` | protocol | JSON names of every new type; optional fields omitted when empty |
| `TestTrustMethodsUnsupportedWithoutSetup` | acp | nil `TrustSetup`: both methods `unsupported` |
| `TestTrustMethodsNeedNoSession` | acp | with a valid route and no session the call works; an invalid route is refused |
| `TestNewSessionPassesTrustMeta` | acp | `_meta["ask.dev/trust"]="approve"` reaches the Factory; `"yes"` is `invalid_params` |
| `TestStateReportsSessionTrust` | acp | `state` carries `trust` and `diagnostics` from `StartTrust` |
| `TestModelsDefaultUsesSessionCwd` | acp | two sessions in two cwds report two defaults |
| `TestDecideThroughFactory` | app | untrusted project: session default model from user; `--approve` meta: from project |
| `TestImplicitTrustIsNeverSaved` | app | plain folder session trusted; `trust.json` absent; create `.ask/settings.json`; next session untrusted `no_ui`, `trust.json` still absent |
| `TestSessionOverrideIsNotSaved` | app | approve meta trusts the session; `trust.json` unchanged |
| `TestTrustReadAtEachSession` | app | grant through `Set`, next `session/new` is trusted without restart |
| `TestTrustSetParentReplacesChild` | app | parent true written, child entry removed |
| `TestTrustSetRefusedWhenStoreCorrupt` | app | `invalid_state`; bytes unchanged |
| `TestSaveDefaultReportsShadowed` | app | trusted project sets `defaultModel`; `set_model` with save gives `defaultSaved:false`, `defaultNotSaved:"shadowed"`, switch kept |
| `TestLeaderRoutesTrustMethods` | leader | both methods forward with the client id and no session; unknown `_ask/trust/x` is `unsupported` |
| `TestParseApproveFlags` | cmd/tui | four spellings; both kinds given is an error |
| `TestACPArgsAcceptApprove` | cmd/tui | `ask acp --approve` accepted; `ask acp foo` still an error |
| `TestPrintModeUntrustedWithoutApprove` | cmd/tui | `runWithDependencies` with a temp project: stderr has the warning; `--approve` removes it; the project default model part is in `TestPrintModeProjectDefaultModelNeedsTrust` |
| `TestTUIClientTrustUnsupportedLeader` | tuiclient | `unsupported` answer gives the typed "not available" value |

Existing tests to keep green: `internal/app/leader_routing_test.go`, `internal/app/module_acp_test.go`, `internal/app/default_model_test.go`, `internal/acp/*`, `cmd/tui/acp_e2e_test.go`, `cmd/tui/connect_e2e_test.go`, `cmd/tui/headless_*_test.go`.

## Refactor

`Factory` signature and `DefaultModel`/`SaveDefault` callbacks. Protected by `go test ./internal/acp ./internal/app` before the change (record the pass) and after.

## Tests After

All "Tests Before" pass; existing suites pass; `go test ./...` passes.

## Regression gate

```text
go test -race ./internal/app ./internal/acp ./internal/leader ./pkg/protocol ./internal/tuiclient
go test ./cmd/tui
go test ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Todo

- [ ] Tests Before
- [ ] Protocol
- [ ] Adapter
- [ ] App composition
- [ ] Router policy
- [ ] CLI flags and print mode
- [ ] tuiclient
- [ ] (only if confirmed) `defaultThinkingLevel`
- [ ] Regression gate green

## Success criteria

- `ask -p` and `ask acp` follow the decision order with no UI; the four compat cells in plan.md behave as written (old client and new leader pinned by `TestDecideThroughFactory` with no meta).
- No leader protocol version change: `rg -n 'LeaderProtocolVersion|ProtocolVersion =' pkg/protocol/leader.go` shows the same value as before.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| A signature change misses a caller | H x L | Enumerated table above; compile gate |
| `--approve` in a leader-wide place grants trust to other clients | L x H | Leader takes no flag; override is per `session/new` |
| Trust state call slows startup | M x M | Two `Lstat` walks and one small file read; no network |

## Security considerations

Trust grant needs an initialized client on the owner-only socket (`internal/leader/README.md:77`) or the in-process pipe. `ask acp` serves no grant. Warnings carry fixed text and paths only.

## Rollback

Revert. Wire additions are optional fields and unknown methods; older peers ignore or refuse them.

## Next steps

Phase 6 calls `TrustState` before the first `session/new` and shows the prompt. Phase 7 documents the flags.
