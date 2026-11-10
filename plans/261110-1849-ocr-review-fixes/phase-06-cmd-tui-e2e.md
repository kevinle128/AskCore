---
phase: 6
title: "CLI commands, capture and e2e"
status: pending
priority: P2
effort: 5h
dependencies: [3, 4]
---

# Phase 6: CLI commands, capture and e2e

## Goal

Fix R11, N28, N29, N30, N31, N32, N33 (use half), N34, N35, and the `ask settings show` word of R2.

Steps 1 to 4 and 6 to 9 can start at once. Step 5 (N33) waits for the commit of phase 3 step 5 (`acp.WireVersion`). Step 10 (R2 word) waits for the first commit of phase 4 (`bootstrap.SkipOutsideProject`). Neither waits for the whole phase.

## File ownership

- Modify: `cmd/tui/capture.go`, `cmd/tui/headless.go`, `cmd/tui/connect.go`, `cmd/tui/leader.go`, `cmd/tui/version.go`, `cmd/tui/settings_command.go`
- Modify: `internal/providers/cassette/cassette.go` (file mode only; phase 2 does not touch this package)
- Modify: `e2e/tui/real_provider.py`
- Tests: `cmd/tui/headless_record_test.go` or a new `cmd/tui/capture_test.go`, `internal/providers/cassette/cassette_test.go`, `cmd/tui/connect_test.go`, `cmd/tui/connect_e2e_test.go`, `cmd/tui/leader_test.go`, `cmd/tui/version_test.go`, `cmd/tui/settings_command_test.go`, a headless test file for N30 and N35

## Constraints

- Tests call the real entry points (`runConnect`, the headless run, `startCapture`, the leader subcommands). The e2e tests use the built `ask` from `newE2EHome` (`cmd/tui/connect_e2e_test.go:161`).
- No network: capture tests use a fake real transport.

## Steps

### 1. R11: private capture folder and cassette

Evidence: `os.MkdirAll(dir, 0o755)` (`cmd/tui/capture.go:35`). The recorder removes the file in record mode (`internal/providers/cassette/cassette.go:94-97`) and go-vcr writes it again with `os.Create` at each save (`cassette.go:154-157`), so the file is `0666 &^ umask`, normally 0644. A cassette can hold prompts and tool results (`docs/testing-llm-cassettes.md:92`).

Where to set the mode: `capture.go` cannot do it, because the file is created inside the recorder after each request. The recorder sets it: after each successful save in record mode, `os.Chmod(r.path, 0o600)`. `os.Create` keeps the mode of an existing file, so later saves stay 0600. The folder is 0700, so no other user can open the file in the short time before the first chmod. This also makes test recordings under `testdata/` 0600; git keeps only the executable bit, so the repository does not change.

Tests first:

1. `internal/providers/cassette/cassette_test.go`: a record-mode recorder with a fake real transport; after one request the file mode is 0600.
2. `cmd/tui`: `startCapture` with `ASK_CAPTURE` set to a folder that does not exist and a fake real transport; one request through `o.transport`; the folder mode is 0700 and the cassette 0600.

Fix: `os.MkdirAll(dir, 0o700)` in `capture.go:35`, and the chmod in `cassette.go` after `r.vcr.Stop()`. An existing folder keeps its mode (it belongs to the user).

Command: `go test -run 'Capture|Record' ./cmd/tui ./internal/providers/cassette`

### 2. N30: `ASK_CAPTURE` works when only `--model` picks the provider

Evidence: `startCapture` runs at `cmd/tui/headless.go:162` with `o.provider` still the default `faux` (`args.go:214`); `printStartModel` sets the real provider later (`headless.go:186`, `:285-292`). With `--model anthropic/x` the run prints "ignored: provider faux makes no HTTP requests" and records nothing. `printStartModel` sends no inference request (the model view uses the private auth client, which stays outside capture by rule).

Test first: a headless run with `ASK_CAPTURE` set, `--model anthropic/<id>` (no `--provider`), and a fake inference transport in `deps.inferenceHTTP`; expect one cassette file and no "ignored" line.

Fix: call `startCapture` after `printStartModel` and before `newPrintAgent` (`headless.go:186-192`), so it reads the resolved `o.provider`. The stderr order changes: the line `ask: capturing provider HTTP to ...` now comes after the start-model warnings. Update a test that asserts the old order on purpose.

Command: `go test -run 'Capture' ./cmd/tui`

### 3. N35: `ASK_FAUX_TPS` is read only for a `faux` run

Evidence: `printStartModel` calls `fauxStream(defaultModel, getenv)` for every run to learn the demo model (`headless.go:264`); `fauxStream` fails on a bad `ASK_FAUX_TPS` (`headless_faux.go:51-55`). The faux run builds its own stream with the real `getenv` (`headless_faux.go:80`). `docs/cli.md:631` says the variable is for `faux` only.

Test first: `ASK_FAUX_TPS=bad` with `--provider openai` (fake transport) does not fail at start; `ASK_FAUX_TPS=bad` with `faux` still exits 1 with the existing message.

Fix: at `headless.go:264`, pass a `getenv` that returns `""` for `fauxPaceEnv`; the demo model does not depend on the pace.

Command: `go test -run 'Faux' ./cmd/tui`

### 4. N31 and N32: `leader status --json` and `leader list --json` report a write error

Evidence: `data, _ := json.Marshal(...)` and `_, _ = fmt.Fprintf(...)` (`cmd/tui/leader.go:151-152`, `:180-181`); a closed pipe exits 0.

Test first in `leader_test.go`: `leader list --json` with no leader and a writer that fails gives exit 1 and an `Error:` line (extend `TestLeaderManagementWithoutLeader`, `leader_test.go:92-94`).

Fix: one helper `writeJSONLine(stdout, stderr io.Writer, v any) int` used by both commands: report and return 1 on a marshal or write error, else 0.

Command: `go test -run 'TestLeaderManagement' ./cmd/tui`

### 5. N33 (use half): one ACP wire version (after phase 3)

Evidence: `const acpWireVersion = 1` (`cmd/tui/version.go:30-31`), used at `:144`.

Test first in `version_test.go`: `ask version --json` gives `acpVersion` equal to `acp.WireVersion`.

Fix: delete the constant and use `acp.WireVersion` (`cmd/tui` already imports `internal/acp`).

Command: `go test -run 'Version' ./cmd/tui`

### 6. N29: `ask connect` handles a signal during setup

Evidence: `ConnectOrSpawn(context.Background(), ...)` (`cmd/tui/connect.go:554`) and `CallCommit(context.Background(), ...)` (`:221`) run `initialize` and `session/new` (`:569-584`) before the first `select` on `sigs` (`:601-607`). A signal while a wedged leader holds `initialize` is lost.

Accepted rule: install the signal handling before setup; exit 130, 143 or 129.

Tests first:

1. In `connect_test.go`: a `lineClient` on an `rpc.Conn` over `net.Pipe()` whose peer reads and never answers; send `syscall.SIGTERM` on the signal channel; `call("initialize", ...)` returns, and the exit code that `runConnect` maps is 143.
2. If the in-process form needs a socket: a unix listener at the leader socket of a temp `ASK_HOME` that accepts and never writes; `runConnect` with a signal sent on the channel returns 143. `Register` waits at most `registerTimeout` (`internal/leader/client.go:37`, `:348`); the test asserts the code, not a time.

Fix:

1. At the start of `runConnect`, make a context that is cancelled on the first signal, with the signal as the cause, from one goroutine that reads `sigs` once. Use it for `ConnectOrSpawn`, and store it in `lineClient` for `callForSession` (`:221`).
2. The main loop (`:601-607`) and `finish` (`:631-643`) select on `ctx.Done()` and map `context.Cause(ctx)` to `signalExitCodes`, so one place reads the signal channel.
3. When a setup step fails and the context was cancelled by a signal, return the signal code instead of 1.

Command: `go test -race -run 'Connect.*Signal|TestConnect' ./cmd/tui`

### 7. N34: `ask connect` exits 1 when the leader is lost with a prompt in flight

Evidence: the main loop exits 1 with "the leader closed the connection" (`connect.go:603-605`), but `finish` treats `c.done` as a normal end and exits 0 with "bye" (`:631-643`).

Test first in `connect_e2e_test.go` (next to `TestLeaderE2EEndOfInputWaitsForThePrompt`, `:346`): `ASK_FAUX_TPS` slow, one prompt, close stdin, stop the leader process (PID from `ask leader status --json`); expect exit 1 and the message on stderr. Regression: the existing test still exits 0.

Fix: in the `c.done` case of `finish`, if `settled` is also closed, exit 0 with "bye" (all prompts ended first); else report "ask connect: the leader closed the connection" and return 1.

Command: `go test -run 'TestLeaderE2EEndOfInput|TestLeaderE2ELeaderLost' ./cmd/tui`

### 8. N28: delete the unused `save` helper

Evidence: `def save(name, sess, tail=24)` (`e2e/tui/real_provider.py:176-181`) has no caller, and `tail` is not read.

Fix: delete the function.

Check: `rg -n 'save\(' e2e/tui` finds no call; `python3 -m py_compile e2e/tui/real_provider.py` passes.

### 9. Step order inside `headless.go`

Steps 2 and 3 edit the same function area of `headless.go`. Do them in one commit or in sequence; do not split them across parallel workers.

### 10. R2 word in `ask settings show` (after phase 4)

Fix: add `case bootstrap.SkipOutsideProject: return "outside_project"` to `contextSkipWord` (`cmd/tui/settings_command.go:342-356`). The `--json` row then has `"skipped": "outside_project"`.

Test first in `settings_command_test.go` (next to the `too_large` row at `:266-275`): a project `AGENTS.md` linked outside the git root shows `outside_project` in the text and in `--json`. The golden `testdata/TestSettingsShowNamesSourceLayer.golden` has no skipped file and does not change.

Command: `go test -run 'Settings' ./cmd/tui`

## Verification

- `go test -race ./cmd/tui ./internal/providers/cassette`
- `go vet ./cmd/tui`
- `go test -run '^TestCassette|^TestCapturedSessionReplays' ./cmd/tui` (replays unchanged)

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| N29 changes which goroutine reads `sigs` and loses a signal | Low | Medium | One reader goroutine; the existing signal tests run with `-race`. |
| N34 test is slow or flaky because of process stop timing | Medium | Low | Wait on process exit, no sleep; the prompt is paced long enough that the stop comes first. |

## Rollback

Revert the phase commits. Step 5 must revert before phase 3 step 5.

## Docs impact

Phase 7: `docs/cli.md:418` (`--json` write error exits 1), `:449` (already "1 error or lost leader"; add "a signal during setup also exits 130, 143, or 129"), `:639` (`ASK_CAPTURE` with `--model`, modes 0700 and 0600), `:336` and `:391` (`outside_project`), `docs/testing-llm-cassettes.md:67`, `:92`.
