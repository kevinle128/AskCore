---
phase: 4
title: "Headless resume and composition"
status: pending
priority: P1
effort: "8h"
dependencies: [2, 3]
---

# Phase 4: Headless resume and composition

## Goal

`internal/app` composes the JSONL store for every native agent, and `ask -p` saves by default, resumes with `-c` or `--session <id>`, and saves nothing with `--no-session`.

## Context

- Shared composition: `app.NativeAgentConfig` (`internal/app/agent_native.go:26-42`), `app.NewNativeAgent` (`:45`); `AgentSpec` (`internal/app/session_start.go:60-78`). Headless builds through `newPrintAgent` / `newNativeAgent` (`cmd/tui/headless.go:319,423`) and calls `app.NewNativeAgent` at `:555`.
- Ids today: `newSessionID` (`cmd/tui/headless.go:563-569`), used at `:195,416`.
- Start model: `printStartModel` (`cmd/tui/headless.go:235-250`) -> `app.ModelOptions` -> `startChoice.choose` (`internal/app/default_model.go:155`). Pi order (inventory H-PROV-19): `--provider/--model` > first of `--models` (not when resuming) > restored > saved default > first usable.
- Flags: `cmd/tui/args.go:113-153` (value flags), `:243-275` (switches). No `-c`, `--session`, `--no-session` today.
- JSON mode: `cmd/tui/headless_json.go:8-12` says there is no session header yet.
- Home isolation in tests: `cmd/tui/headless_test.go:44,84`.
- Auth readiness with a requested method: `nativeAuthReady(..., requestedMethod)` (`internal/app/agent_native.go:181,188`).

## Requirements

0. Export the canonical-path helper of `internal/workspace` (`canonicalPath`, `internal/workspace/project.go:37`) as `CanonicalPath`; `SessionLog` uses it for every cwd.
1. `internal/app/session_log.go`: `SessionLog` built once per process from the Ask home (`settings.ResolveHome`), lazy (the sessions folder is made only at the first write, so `ask settings show` still makes no file). Functions: `NewContext(spec)` returns the closure for `agent.Config.NewContext` (first call: create with `spec.SessionID` or return the resumed writer; later calls: `CreateNow` with `sessions.NewID()`); `Resume(cwd, id string)` opens a session (`id == ""` means the newest of the canonical cwd); `Latest(cwd)` returns the newest id or `ErrSessionNotFound`.
2. `AgentSpec` gains `Resume store.SessionWriter` (pre-opened log), `NoSession bool` and `CreateNow bool` (ACP `session/new` sets it in the next phase; headless keeps lazy create). `NewNativeAgent` sets `Config.NewContext` from `SessionLog` unless `NoSession`; with `NoSession` the `MemoryLog` default stays.
3. Restored selection: `app.ModelOptions` gains `Restored` (model ref, auth method, thinking level from `agent.LoggedSelection`). `choose` places it after `--model` and before the saved default, and ignores the `--models` start pick when resuming. The restored method is the requested method for readiness. When readiness refuses it, the start fails with a message that names the old method and tells the user to pass `--model`.
4. Flags: `-c` / `--continue`, `--session <id>`, `--no-session`. Conflicts: `-c` with `--session`, and either with `--no-session`, give a usage error (exit 1). `--session` of an unknown id: error "no session <id> in <cwd>". `-c` with no session: error "no session to continue in <cwd>". A held session: the store error text (names the lock file).
5. Ids: delete `newSessionID` in `cmd/tui/headless.go` and in `internal/acp/host.go:347-352`; both use `sessions.NewID()`. The ACP `NewAgent` factory (`internal/app/module_acp.go:182`) sets `CreateNow`. This lands here, not later, because saving starts for every native agent in this phase: a `sess_` file name would sort above every decimal id and always win `-c`. Test literals such as `sess_missing` stay valid (they name unknown sessions).
6. JSON mode: `ask -p --mode json` writes the session header record (`type: "session"`, `version`, `id`, `cwd`, `createdAt`) as its first line, before the first event. With `--no-session` the header still prints (the id is the run id). Update the golden JSON-mode tests (`cmd/tui/headless_test.go:321,381,431,444` and any other golden that `rg -n -- '--mode", "json"' cmd/tui` finds).
7. Store errors map to user text in `internal/app` (`internal/acp` does not import `store`): owned, not found, corrupt, newer format.
8. Durability end to end: the headless run disposes the Agent on every exit path so `Close` releases the flock (existing `Dispose` path; verify the signal path).

## Files owned

- Create: `internal/app/session_log.go`, `internal/app/session_log_test.go`
- Modify: `internal/app/agent_native.go`, `internal/app/session_start.go`, `internal/app/default_model.go`, `internal/app/module_acp.go` (`CreateNow` only), `internal/workspace/project.go` (export only)
- Modify: `internal/acp/host.go` (id generator only), `cmd/tui/headless_test.go` (JSON goldens)
- Modify: `cmd/tui/args.go`, `cmd/tui/headless.go`, `cmd/tui/headless_json.go`, `cmd/tui/native_agent_test.go`, `cmd/tui/tools_e2e_test.go` (replace `newSessionID` calls at `:821,1587`)
- Create: `cmd/tui/headless_session_test.go`

## Tests written first (built binary or real child processes)

- `TestPrintResumeAfterKillKeepsModelAndThinking` (exit test): run the built `ask --mode json` with a non-default model and `--thinking`; let turn 1 finish; SIGKILL the process on the `tool_execution_start` event of turn 2 (a `bash` call that sleeps), so the kill lands inside an open turn; then run `ask -p -c "next"` with no model flags. Assert that the new `RequestDelta` has the same `Model` and `Options.Reasoning`, and that the file holds the closing entries. Use the provider fixture of `builtAsk` (`cmd/tui/tools_e2e_test.go:1487`) or a second model from a user `models.json` [UNVERIFIED which fixture lists two models].
- `TestPrintSessionFlagPicksSession`, `TestPrintNoSessionWritesNoFile`, `TestPrintContinueWithoutSessionFails`, `TestPrintSessionFlagConflicts`.
- `TestPrintSecondWriterFailsAtOnce` (child holds the session; second `ask -p --session` exits 1 with the lock error and no wait).
- `TestPrintJSONModeStartsWithSessionHeader`.
- `TestSessionFileHoldsNoCredential` (run with an API key in the environment; the file bytes do not contain it).
- `TestResumeRefusesChangedAuthMethod` and `TestHeadlessCredentialReplacementReachesNextRequest` (headless replaces a saved key; the next request of a resumed session uses the new record).
- `TestExplicitModelBeatsRestoredModel`; `TestRestoredModelBeatsSavedDefault`.
- Existing guards stay green: `TestSettingsShowStartsNoLeader` (`cmd/tui/settings_command_test.go:206`), cassette tests (`cmd/tui/headless_cassette_test.go`).

## Steps

1. Write the tests; see them fail.
2. `SessionLog` and its unit tests on a temp home.
3. `AgentSpec` and `NewNativeAgent` wiring; restored selection in `choose`.
4. Flags, id swap, JSON header.
5. Run the whole `cmd/tui` suite; fix tests that assumed an empty home after `ask -p` by checking only the files they own.

## Validation

```
go test ./internal/app/... ./cmd/tui/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui   # only if a tool or provider reference changed
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| Tests in other packages write into the real home | M x H | run `rg -n "ASK_HOME" --type go internal cmd -g '*_test.go'`; every test that builds a native agent sets a temp home |
| Cassette bodies hold the old id form in prompt-cache keys | M x M | the id is random today, so matching must already ignore it; re-record only if a test fails (`ASK_RECORD=1`) |
| Resume picks a session of another cwd that encodes the same | L x M | header cwd check in the store (phase 2) |

## Rollback

`--no-session` gives today's in-memory behavior at run time. A code revert of this phase leaves the store unused.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
