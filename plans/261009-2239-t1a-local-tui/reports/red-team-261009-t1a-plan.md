# Red-team review: T1a local TUI plan

2026-10-09. All findings checked against the repo. No UI change proposed.

## Findings

**1. High: the client cannot tell a refused prompt from a failed run.** `Adapter.Prompt` returns a JSON-RPC error both for admission refusal and for a run that ends in error after acceptance (`internal/acp/agent.go:353-358`, `runError` at `internal/acp/errors.go:99-107` maps AUTH to `ErrAuth` and others to `internal`). If phase 2 maps every prompt error to a "send failure", criterion 8 (S9) shows `✗ Not sent:` instead, keeps the draft, and the user sends the prompt again, which breaks the never-resend rule. Fix: in phase 2, the acceptance event decides. An error after acceptance is a run error, and an error before acceptance is a refusal. Add `TestClientRunErrorAfterAcceptanceIsNotRefusal` that uses the provider fixture from finding 4.

**2. High: Bubble Tea's own signal handler breaks the phase 7 exit codes.** The patched copy catches SIGINT/SIGTERM itself (`source/bubbletea/tea.go:665-683`). SIGTERM becomes `QuitMsg`, `Run` returns nil, and the exit code is 0, not 143. SIGHUP has no handler, so the default action kills the process before the terminal is restored. That fails criterion 21. Fix: phase 7 uses `tea.WithoutSignalHandler()` and its own handler for SIGINT/TERM/HUP. The handler detaches, quits the program, waits for the restore, and then exits through `signalExitCodes` (`cmd/tui/headless.go:38-42`).

**3. High: many acceptance criteria have no named test.** plan.md leaves the criterion→test table to phase 8, which is only an outline. No test: 4 (S5b UI; client-level only), 10 (S11), 16 (S12a) and 20 (120×40 goldens). Half a test: 3 (`> ` in scrollback, `Working… Ns`), 9 (next prompt runs), 12 (enter sends nothing), 14 (new session), 15 (observer footer), 19 (40×14), 21 (normal quit, error exit) and 22 (while streaming). Fix: move the 22-row table into plan.md now, with an owning phase and test name per row. A phase scout pass may not end with a row it owns still empty.

**4. High: the planned test sources cannot produce criteria 7, 8 and 18 through the real leader.** The faux demo only says text, calls echo, or fails (`cmd/tui/headless_faux.go:24-44`). It has no retryable 429, no expired token and no thinking blocks. Cassettes reach only `ask -p` (`cmd/tui/capture.go`), and a spawned leader takes its transport from the production `deps` (`cmd/tui/acp.go:104-106`). Fix: name the existing `providerFixture` (`cmd/tui/acp_e2e_test.go:1460-1550`). It sets `HTTPS_PROXY`/`SSL_CERT_FILE`, and the spawned leader inherits the client env (`internal/leader/spawn.go:272-279`). The real code path stays, with no test-only hook.

**5. Medium: phase 1 adds libraries that `go mod tidy` then removes.** In phase 1 nothing imports glamour, teatest or golden, so step 3's tidy drops them. `debug.ReadBuildInfo` lists only the modules that are linked into the binary (checked: a scratch test showed `deps=0`). So `TestDependencyPins` cannot see lipgloss in phase 1. Fix: phase 1 pins Bubble Tea, Ultraviolet and x/vt. Phases 3 and 5 add lipgloss, glamour and teatest when they first import them, and extend the pin test at the same time.

**6. Medium: phase 1 step 5 cannot pass.** golangci-lint pulls `github.com/MirrexOne/unqueryvet@v1.5.4`, which requires bubbletea v1.3.10 and lipgloss v1.1.0 (`go mod graph`). So `go list -m all | grep … v1` always prints them. Fix: check with `go list -deps ./... | grep -E 'charmbracelet/(bubbletea|lipgloss)$'`, which must print nothing. After the tidy, check whether the cellbuf floor comment (`go.mod:139-141`) is still true.

**7. Medium: the patch regression test never runs in the gate.** `third_party/bubbletea` is a nested module. `go test ./third_party/acp-go-sdk/...` fails from the root with "main module does not contain package", and `go test ./...` skips it. Fix: phases 1 and 8 run `go -C third_party/bubbletea test ./...`; keep the copy's `go.sum`.

**8. Medium: the phase 8 runner is tied to T0.** `e2e/tui/run.py` waits for `editor>` (`:99`), asserts `T0 startup` (`:61`), passes `--scenario`/`--draft-report` (`:91-94`) and builds from the archive (`:408-412`). Retargeting means a new target driver, not 1d of work. Fix: add a separate `ask` target with its own markers, keep the T0 cases, and re-estimate phase 8.

**9. Medium: secrets can leak into the runner's artifacts.** `Session.call` writes every command, including the `--env` values, to `commands.json` (`run.py:133-143`), and the values also show in `ps`. Real-provider keys passed this way go to disk. The isolated `ASK_HOME` for the subscription run holds OAuth tokens. Fix: pass keys through the inherited environment or redact `--env` values. Delete the isolated home after the run. Copy only summaries into `plans/`. Keep `ASK_HOME` under the 103-byte socket limit (`internal/leader/paths.go:108-116`).

**10. Medium: criteria 12 and 13 have no deterministic setup.** Leader controls are only status and shutdown (`cmd/tui/leader.go:262`), so E2E cannot drop only the socket (S13a). For S13, reconnect respawns a leader after about 1 s, so polling for `Disconnected` is flaky. Fix: evidence for 13 is the phase 2 in-process connection-close test plus the phase 6 golden. For 12, hold the leader lock without answering (`cmd/tui/leader.go:213-220`) for at least 2 attempts.

**11. Medium: a panic in a client goroutine leaves the terminal in raw mode.** Bubble Tea recovers panics only in its own goroutines (`tea.go:921-965`). A panic in a `tuiclient` reader or reconnect goroutine kills the process with echo off. Phase 7 claims "restoration on panic" but has no test for it. Fix: each `tuiclient` goroutine recovers and sends a fatal message to the program. Add a test with a panicking fake client goroutine.

**12. Medium: phases 2 and 3 do share files.** The plan says they share no files. But phase 2 declares the consumer interface inside `internal/tui`, and the README maps client I/O to `commands.go` (`internal/tui/README.md`), which phase 3 also lists. Fix: phase 2 owns only `internal/tui/client.go` (the interface and client message types), and phase 3 must not edit it.

**13. Medium (needs user): the roadmap contradicts the no-fallback rule.** The roadmap constraint "AgentClient … `direct` (the fallback when no leader is reachable)" (`roadmap.md:716`) conflicts with the T1a no-silent-fallback rule (`roadmap.md:690-691`) and the plan constraints. Fix: ask the user to change the roadmap wording. Keep leader-only operations (attach, take, list_live) behind optional parts of the interface so that a later `direct` implementation stays possible.

**14. Low: `TestInlineReplayPatchPresent` asserts something the patch does not do.** The 120 ms settle is application code (T0 `source/model.go:77`, `tea.Tick`). The patch only rejects requests with a stale generation or size, and it needs the cursed renderer (`tea.go:773-785`). Fix: phase 1 asserts stale-generation rejection and one purge/sync pair; the settle moves to phase 3.

**15. Low: depguard does not enforce the import rule the plan states.** `tui-client-boundary` is a lax deny list of 7 prefixes (`.golangci.yml:28-46`), so imports of `internal/sessions`, `internal/auth` or `internal/app` pass the linter. Fix: make it a strict allow list when phase 2 adds the `tuiclient` rule.

## Rejected after checking

- lipgloss raised by golangci-lint: the graph has v2.0.3 and UV 20251205. lipgloss v2.0.5 needs the same UV, and only v2.0.6 raises it.
- Replace directive: the copy is `module charm.land/bubbletea/v2`, go 1.26. Feasible.
- teatest v2: compiled against v2.0.10 in the research, and the patch only adds API.
- Phase 1 breaks interactive `ask`: only a demo list is lost (`cmd/tui/main.go:37-118`).
- SIGHUP kills the spawned leader: `Setsid` (`internal/leader/spawn.go:281`).

## Unresolved questions

- Finding 13: change the roadmap wording about the `direct` fallback?
- Finding 10: is the in-process connection-close test acceptable as the evidence for criterion 13?
