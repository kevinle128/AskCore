---
phase: 8
title: "Real entry path acceptance"
status: completed
effort: 4h
depends_on: [1, 2, 3, 4, 5, 6, 7]
---

# Phase 8: Real entry path acceptance

## Context links

- [plan.md](./plan.md) acceptance criteria 1 to 10
- Project rule: tests run the real code path; no test-only harness that bypasses `ask -p`, `ask acp` or the leader (user memory "Tests must run the real code path")
- Built-binary helpers: `cmd/tui/acp_e2e_test.go:47-69` (`acpBinary` builds `ask` once, `removeACPBinary`), `cmd/tui/headless_test.go:36-52` (`TestMain`, `isolateHome` gives each test run a private `ASK_HOME` and turns model refresh off), `cmd/tui/leader_e2e_test.go:226` (`runCaller`), `cmd/tui/connect_e2e_test.go:190-300` (two clients on one leader)
- Terminal runner: `e2e/tui/run.py`, `e2e/tui/tui-test.toml`, `e2e/tui/README.md` (Alacritty and Ghostty backends)

## Overview

Priority P1. Prove the whole feature through the built `ask` with a temporary `ASK_HOME` and a temporary project folder. Cover `ask -p`, `ask acp`, the leader with `ask connect` (old-client behavior: no trust calls), the leader with the TUI (terminal runner), and the in-process TUI (`--no-leader`).

## Key insights

- `acpBinary(t)` already builds one `ask` for the package tests; reuse it. Each test sets `ASK_HOME` to its own `t.TempDir()` (mode 0700) and runs in its own project folder.
- `ask connect` sends no trust call and no override, so it is the real "old client" case for the compat table.
- The faux provider makes runs deterministic. With `ASK_FAUX=1` the leader, `ask acp` and the in-process TUI list it.
- The model default from the project layer is observable: `_ask/session/state.modelId` or the print-mode JSON `model` field.

## Requirements

Test file `cmd/tui/trust_e2e_test.go` (built binary, real processes):

| Test | Entry | Setup | Expected |
|---|---|---|---|
| `TestTrustE2EPrintUntrustedWarns` | `ask -p` | project `.ask/settings.json` with `defaultModel` | stderr has the untrusted warning once; reply uses the user default |
| `TestTrustE2EPrintApproveLoadsProject` | `ask -p --approve` | same | no warning; project default used; `trust.json` absent after the run |
| `TestTrustE2EPrintSavedDecision` | `ask -p` | `trust.json` with the parent folder `true` | trusted `saved_ancestor` |
| `TestTrustE2EPrintCorruptTrustStore` | `ask -p` | `trust.json` = `{` and `defaultProjectTrust: always` | untrusted warning names `trust.json`; exit 0; file bytes unchanged |
| `TestTrustE2EPlainFolder` | `ask -p` | no gated files | no warning; `trust.json` absent |
| `TestTrustE2EAskHomeAsCwd` | `ask -p` | cwd = parent of `ASK_HOME` with `settings.json` in it | no project layer, no prompt source, no warning |
| `TestTrustE2EACPApprove` | `ask acp --approve` | project default model | `session/new` then `_ask/session/state`: `trust.source = "flag"`, project model |
| `TestTrustE2EACPRefusesTrustMethods` | `ask acp` | none | `_ask/trust/set` answers `unsupported` |
| `TestTrustE2ELeaderOldClientUntrusted` | leader + `ask connect` | gated project, no decision | session state `trusted: false`, `source: "no_ui"`; no `trust.json` |
| `TestTrustE2ELeaderGrantThroughWire` | leader + raw leader client (the `connect_e2e_test.go` client) | send `_ask/trust/set` trust, then `session/new` | `trust.json` holds the canonical path; session trusted `saved` |
| `TestTrustE2EImplicitTrustNeverSaved` | leader + `ask connect` | plain folder, then create `.ask/settings.json`, then a new session | first trusted `no_gated_files`; second untrusted; `trust.json` absent |
| `TestTrustE2EShadowedDefault` | leader + client | trusted project with `defaultModel` | `_ask/session/set_model` with `saveDefault` gives `defaultNotSaved: "shadowed"`; user `settings.json` unchanged |
| `TestTrustE2ESettingsShow` | `ask settings show --json` | trusted and untrusted runs | sources and the untrusted text as phase 7 |

Terminal runner (real terminal engines):

- Add one case to `e2e/tui/tui-test.toml` that starts the built `ask` in a gated project with a temp `ASK_HOME`, waits for the trust prompt text from `design.md`, presses the "Trust" key, and checks the session start text and that `trust.json` holds the path. Add a second case with `--no-leader` (in-process) that checks the same prompt.
- Document the cases in `e2e/tui/README.md`.

## Related code files

Create: `cmd/tui/trust_e2e_test.go`.
Modify: `e2e/tui/tui-test.toml`, `e2e/tui/README.md`, and `e2e/tui/run.py` only if the case needs a new step kind.

## Implementation steps

1. Write the tests; run them; they pass only when phases 1 to 7 are in.
2. Add the terminal runner cases; run them on Alacritty and Ghostty.
3. Stop every leader that a test started: tests use their own `ASK_HOME`, and cleanup runs `ask leader stop` against that home and checks that no process and no socket remain (project memory: stop the leader before removing the binary).
4. Record the run commands and results in the phase result (not in code).

## Tests Before / Refactor / Tests After

Tests Before: the table above, written before any fix found in this phase. Refactor: none planned; a failure found here is fixed in the owning phase's files with a focused test there. Tests After: the table and the two terminal cases pass.

## Regression gate

```text
go test ./cmd/tui -run '^TestTrustE2E'
go test ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
python3 e2e/tui/run.py --case trust-prompt-leader
python3 e2e/tui/run.py --case trust-prompt-in-process
```

After the runs: `ps -axo pid,command | grep '[a]sk leader'` shows no leader that a test started, and no `leader.sock` remains in the test homes.

## Todo

- [ ] `cmd/tui/trust_e2e_test.go`
- [ ] Two terminal cases
- [ ] Leader cleanup checked
- [ ] Regression gate green

## Success criteria

All plan acceptance criteria have a passing test on the built binary. Terminal.app, iTerm2 and kitty stay unverified (stated in `e2e/tui/README.md`).

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| A leaked leader from a failed test | M x M | Per-test `ASK_HOME`; cleanup stops the leader and checks process and socket |
| Timing flakes in the terminal runner | M x L | Wait on screen text, not on sleeps; rerun alone before any threshold change |

## Security considerations

Tests use only the faux provider and temporary homes; no real credential is read.

## Rollback

Delete the test file and the two cases.

## Next steps

Close H6a. Hand the H6b note (plan.md) to the H6b plan.
