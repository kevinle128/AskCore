# Triage: ocr m5 (tui, tuiclient) and m6 (cmd/tui, e2e, testsupport)

Date: 2026-11-10. Branch: develop. Code read at current HEAD, no build or test run. A = m5 findings, B = m6 findings (same order as the input files).

## m5: internal/tui

| # | location | claim (short) | verdict | severity | evidence | fix |
|---|----------|---------------|---------|----------|----------|-----|
| A1 | internal/tui/blocks.go:37 | Exported `SpinnerFrames` could be cleared, then modulo by zero | FALSE | n/a | Only blocks.go:41-42,93 read it; nothing in non-test code writes it. A caller must deliberately empty a package variable. | none |
| A2 | internal/tui/editor_paste.go:25-32 | Paste store never evicts, unbounded memory | DESIGN | Low | Doc comment at editor_paste.go:18-19: the store keeps every large paste "so that undo and history can bring a marker back". Growth needs repeated multi-line pastes by the local user in one session. | none |
| A3 | internal/tui/markdown_table.go:91-98 | A prose line right after a table is taken as a table row | DESIGN | Low | The scan ends at a blank line or `tableBreak` (markdown_table.go:91). This is the GFM rule: the table ends at the first blank line or the start of another block; a line without pipes is a one-cell row. The proposed fix would diverge from GFM and glamour. | none |
| A4 | internal/tui/footer.go:62 | `ctxTokens*100` overflows int64 | FALSE | n/a | Overflow needs more than 9.2e16 tokens. No real context window or usage value is near that. | none |
| A5 | internal/tui/markdown_split.go:95-99 | Unterminated final closing fence flushed as open | DESIGN | Low | Comment at markdown_split.go:90-92: the chunk is closed "only when the line is complete". While streaming, a bare ``` at the end may still grow (more backticks or an info text), so it must stay open. Cost of the other choice is a wrong stable cache entry. | none |
| A6 | internal/tui/model_cycle.go:100-102 | Stale `Models` reply after generation change leaves cycling locked | FALSE | n/a | model_cycle.go:97-99 clears `cycling` when `msg.token == s.cycleFetch`, before the `msg.gen != s.gen` return. A newer fetch sets `cycleFetch` again (model_cycle.go:84), so a stale token cannot clear the new state, and the new request has its own reply. The call ends by `callTimeout` anyway. | none |
| A7 | internal/tui/selection.go:172-178 | `strings.Fields` folding corrupts copied text with multiple spaces | FALSE | n/a | `softWraps` only returns a `wrapJoin` kind per row (selection.go:150-160, 215+). The copied text comes from `cellSlice`. At most the single separator at a wrap boundary becomes one space, and a wrap already consumes that whitespace. | none |
| A8 | internal/tui/session.go:328-331 | Session commands have no owner cancellation | DESIGN | Low | `callContext` (session.go:328) gives `callTimeout`; generation checks (model_cycle.go:100) drop stale results. The process ends on quit. 24 call sites; no leak beyond the timeout. | none |

## m6: cmd/tui, e2e, testsupport

| # | location | claim (short) | verdict | severity | evidence | fix |
|---|----------|---------------|---------|----------|----------|-----|
| B1 | e2e/tui/real_provider.py:102-103 | `os.write` may be partial | DESIGN | Low | Dev runner on a blocking PTY master; writes are single keys or one paste of a short key. Partial write needs a signal mid-write. README.md:90-91 describes it as a by-hand tool. | none |
| B2 | e2e/tui/real_provider.py:33-36 | Hard-coded `/private/tmp` fails on Linux | DESIGN | Low | Intentional macOS paths: real_provider_scenarios.py:11 ("/tmp is a symlink on macOS"), e2e/tui/README.md:90,91,105,111. AGENTS.md says the provider run is "By hand only". `ASK_BIN` and `RP_OUT` already override. | none |
| B3 | e2e/tui/real_provider_scenarios.py:33 | Same hard-coded `/private/tmp` for `mkdtemp` | DESIGN | Low | Same as B2; README.md:105 "Each scenario run owns a unique home under `/private/tmp`" (a real directory, since the leader refuses a symlinked home). | none |
| B4 | e2e/tui/real_provider.py:176-181 | `save` is unused and ignores `tail` | REAL | Low | `rg "save("` finds only the definition (real_provider.py:176). `tail` is never read. Dead code. | delete `save` |
| B5 | cmd/tui/connect.go:221, 568-570 | `initialize` and `session/new` use `context.Background()` and ignore signals | REAL | Low | `callForSession` passes `context.Background()` (connect.go:221). The setup calls (connect.go:568-585) run before the signal `select` (connect.go:600). With a signal handler registered, SIGINT/SIGTERM during a wedged leader is swallowed. Needs a hung local leader. | give `callForSession` a context cancelled when `sigs` fires, or a setup deadline |
| B6 | cmd/tui/auth_input.go:38-40 | Non-file reader blocks past cancel | DESIGN | Low | Real input is `os.Stdin` (an `*os.File`), which is polled every 100 ms (auth_input.go:38-48). Non-file readers occur only in tests. Doc comment on line 16 states the contract for files. | none |
| B7 | cmd/tui/capture.go:31, headless.go:163 | `ASK_CAPTURE` is ignored when only `--model` selects the provider | REAL | Low | `startCapture` runs at headless.go:163 before `printStartModel` (headless.go:177). `o.provider` is `defaultProvider` (args.go:214) and only `--provider` changes it (args.go:114). With `--model anthropic/x` the user gets the "ignored: provider faux makes no HTTP requests" message although the run uses HTTP. Docs examples always pass `--provider` (docs/testing-llm-cassettes.md:67). | call `startCapture` after `printStartModel` using `start.Provider`, or document that `--provider` is required |
| B8 | cmd/tui/headless_wait.go:248-249 | Stale `AfterFunc` callback can cancel a later run | FALSE | n/a | `fire` re-checks `c.ag.Idle()` (headless_wait.go:249); after `AgentStart` the Agent is not idle, so a late callback only re-arms through the goroutine at 251-257. It cannot cancel. | none |
| B9 | cmd/tui/headless_wait.go:56-57 | `ms * time.Millisecond` overflow disables the limit | DESIGN | Low | Needs a value above 9.2e12 ms (about 292 years) in the user's own env var. Both outcomes (wrapped negative, 292 years) are effectively "no limit"; the user chose it. | none |
| B10 | cmd/tui/interactive.go:372-375 | Signal during close treated as success, no escalation | DESIGN | Low | Doc comment at interactive.go:366: wait "until a signal comes". The caller exits right after; the signal ends the wait, which is the escape the user asked for. | none |
| B11 | cmd/tui/leader.go:150-153 | `leader status --json` ignores marshal and write errors | REAL | Low | `data, _ := json.Marshal(st)` and `_, _ = fmt.Fprintf(...)` (leader.go:152-153). A broken pipe returns 0. Marshal of a plain struct cannot fail, so only the write matters. | check the write error and return 1 |
| B12 | cmd/tui/leader.go:179-182 | Same for `leader list --json` | REAL | Low | leader.go:181-182, same pattern. | same as B11 |
| B13 | cmd/tui/headless_wait.go:86-87 | `envMillis` overflow | DESIGN | Low | Same as B9; `0` already means "off" by design (headless_wait.go:76-77). | none |
| B14 | cmd/tui/tui_client.go:126-128 | In-process `ServeStdio` uses `context.Background()` | DESIGN | Low | The stop path is the pipe close: `OnClose` waits for `exited` up to `inProcessStopWait` and reports a failure (tui_client.go:146-152). The agent shares the process, which ends after close. | none |
| B15 | cmd/tui/settings_command.go:108-112 | Signal path returns without joining the worker | DESIGN | Low | `abortGrace` is the intended bound (settings_command.go:108-111, doc comment "A signal cancels the read and ends the command"). The process exits right after; the worker only reads files. Joining unboundedly would defeat the signal. | none |
| B16 | cmd/tui/version.go:30 | `acpWireVersion` duplicates `internal/acp.wireVersion` | REAL | Low | version.go:30 `const acpWireVersion = 1` and internal/acp/agent.go:20 `const wireVersion = 1` (unexported). No test ties them (`rg` in `*_test.go` finds none). `cmd/tui` already imports `internal/acp`. | export `acp.WireVersion` and use it in version.go |
| B17 | cmd/tui/tui_client.go:146-147 | `stopAgent()` must be called in `OnClose` | FALSE | n/a | Depends on the B14 fix, which is not needed; as written no `stopAgent` exists. | none |
| B18 | cmd/tui/connect.go:636-642 | `finish` exits 0 when the leader closes after stdin EOF | REAL | Low | The main loop returns 1 with "the leader closed the connection" (connect.go:603-605). `finish` returns 0 and prints "bye" when `c.done` wins (connect.go:636-642), though a prompt was in flight. Inconsistent exit code for a debug client. | in the `c.done` case of `finish`, report the same message and return 1 |
| B19 | cmd/tui/headless.go:264 | `printStartModel` parses `ASK_FAUX_TPS` for real-provider runs | REAL | Low | `fauxStream(defaultModel, getenv)` runs unconditionally (headless.go:264) and errors on an invalid value (headless_faux.go:51-55). docs/cli.md:631 says the variable is for the `faux` provider only. A bad faux-only variable breaks `ask -p --provider openai`. | pass a getenv that returns "" for the demo model lookup |
| B20 | internal/testsupport/oauth-ports.go:15 | `syscall.Flock` does not build on Windows | DESIGN | Low | The project is Unix only: `golang.org/x/sys/unix` in cmd/tui/auth_input.go:9, leader unix socket, FUSE tests. AGENTS.md lists no Windows target. | none |
| B21 | internal/testsupport/fsfault/fsync-gate.c:80 | `(int)pread` truncates above `INT_MAX` | FALSE | n/a | FUSE caps a read at `max_read`, far below `INT_MAX`; `size` never exceeds that. Test-only mount (fsync-gate.c:20 comment). | none |
| B22 | internal/testsupport/fsfault/fsync-gate.c:33-35 | `stopping` read without the mutex | FALSE | n/a | `gate_stopping` is called only from `wait_gate` (fsync-gate.c:45,53), which calls `pthread_cond_timedwait(&changed, &mutex, ...)` at line 50, so every caller holds `mutex`. The `control` thread reads `stopping` under the lock (lines 190-193). | none |

## Counts

| verdict | A (m5) | B (m6) | total |
|---------|--------|--------|-------|
| REAL | 0 | 8 | 8 |
| FALSE | 4 | 4 | 8 |
| FIXED | 0 | 0 | 0 |
| DESIGN | 4 | 10 | 14 |

All REAL rows are Low severity.

## Unresolved questions

- B7: decide between moving `startCapture` after model resolution and documenting `--provider` as required.
- B16: no test or build check was run; the claim that no test links the two constants comes from `rg` only.
