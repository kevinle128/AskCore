---
phase: 9
title: "End-to-end acceptance"
status: completed
priority: P1
effort: "3d"
dependencies: [8]
---

# Phase 9: End-to-end acceptance

## Context

- Architecture: [Other](../architectures/h5-builtin-tools-architecture.md#other) (scenario checklist from DeepSeek and Grok; ESRCH after each test), [Problem & goals](../architectures/h5-builtin-tools-architecture.md#problem--goals) (roadmap exit).
- Decisions: all of Q1-Q15 ([decision record](../architectures/h5-builtin-tools.md)); each Runtime Flow Proof row in `plan.md` names its decision.
- Spike 1: [Method](../reports/spike-261010-system-reminder-notice.md#method) (gated job output, notice through the real `Agent.FollowUp`). Spike 2: [Facts the plan must keep](../reports/spike-261010-output-buffer.md#facts-the-plan-must-keep) (ESRCH after cleanup).
- Roadmap: [H5](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools) tests E§24#12, #13, #15-#18 and Pi contract parity; [LLM cassette testing](../../docs/testing-llm-cassettes.md) (why `bash` runs never become cassettes; re-record rules).

## Overview

Prove every promised feature through the real entry points: print mode (`run` / `runWithDependencies`, and the built `ask` where a signal or a process exit must be observed) and the built `ask leader` over its Unix socket. Only the model HTTP service is replaced. Also restore the cassette suite that the new request body breaks (N4).

## Requirements

- Functional:
  - Print-mode E2E (PM): `runWithDependencies([]string{"-p", "--provider", "alibaba-token-plan", "--model", ..., prompt}, ...)` with `deps.inferenceHTTP` set to a scripted Anthropic Messages SSE server (same `anthropic-messages` wire as the real adapter; pattern `cmd/tui/headless_lifecycle_test.go:82,136-160`, `sseResponse` at `cmd/tui/headless_cassette_test.go:71`), `deps.getenv` with a fake key and the env of the case, and `deps.environ` with the case's base env. The script answers request N with a tool call or text and asserts the tool result in the body of request N+1. Not the `newHeadlessAgent` shortcut (rule: tests run the real entry point).
  - Built-binary print E2E (PB): the `ask` binary from `acpBinary(t)` (`cmd/tui/acp_e2e_test.go:46-60`) run as `ask -p ...` with the provider fixture (`newProviderFixture`, `cmd/tui/acp_e2e_test.go:1493`, env at `:1574-1575`); used where the test must send a signal or observe process exit.
  - Leader E2E (LE): the built `ask leader` with an isolated `ASK_HOME`, the provider fixture, and a client on the Unix socket: `session/new` → `session/prompt` → `session/update` frames → `session/close` or leader stop.
  - Every test that starts a job records the pgid (the script writes `$$` and `ps -o pgid= $$` to a file) and asserts `kill(-pgid, 0) == ESRCH` after the cleanup step, not at command end.
  - Prepared repo: a temp git repo with a Go module, `calc.go`, `calc_test.go` with one failing test, a `.gitignore`, a CRLF file with BOM, a generated file, a 3000-line file, a binary file, a small PNG, a 4000x3000 PNG.
  - The test names, entry points and observable results are those of the Runtime Flow Proof matrix in `plan.md`; this phase owns all of them.
  - N4 (option A, decided by the user on 2026-10-11): move the six cassette tests (`TestCassetteTextReply`, `TestCassetteOneToolCall`, `TestCassetteParallelToolCalls`, `TestCassetteJSONEventOrder`, `TestCassetteThinkingReply`, `TestCapturedSessionReplays`, `cmd/tui/headless_record_test.go:97-182`) to the OpenAI provider with an API key and re-record them with `ASK_RECORD=1` and `OPENAI_API_KEY` from the environment (never printed). Replay uses a fake key: the API-key path sends the same request body at record and replay. Native Anthropic OAuth is not used for cassettes because the OAuth profile adds the Claude Code system block and beta header (`internal/providers/anthropic/document.go:35`, `profile.go:18-19,72`), so a replay would need a stored OAuth credential. Assertions that depend on the Anthropic event shape (thinking blocks, event order) are adjusted to the OpenAI wire, or the test keeps its name and asserts the same user-visible behavior. Prompts that call `echo`, `add`, `mul` stay (no `bash` in any cassette). `TestCassettesHoldNoSecrets` (`headless_cassette_test.go:181`) must pass.
- Non-functional: E2E tests use `t.Parallel()` only when they do not set process env; PB and LE tests share the built binary; the whole `cmd/tui` package stays under the current CI time plus 60 s.

## Architecture

```
PM:  test → runWithDependencies → newNativeAgentOn → Agent → coordinator → tool → jobs/sandbox
                                     ↘ scripted SSE (deps.inferenceHTTP) ↙ asserts request bodies
PB:  test → exec ask -p (built) → … same path … → fixture (HTTPS proxy) ; test sends SIGINT / waits exit
LE:  test → ask leader (built) ← Unix socket client (session/new, prompt, close) ; fixture serves SSE
```

## Runtime Flow

- Actor and trigger: the model (scripted) and the user (signals, session close).
- Registered entry point: `runWithDependencies` (`cmd/tui/headless.go:60`); the `ask` binary; `ask leader`.
- Internal path: as the plan matrix lists per row.
- Prepared data and external mocks: the prepared repo above; only the model HTTP service is replaced.
- Observable result: printed reply and exit code, request bodies, files on disk, ACP frames, process groups gone.
- End-to-end proof: the tests below.

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `cmd/tui/tools_e2e_test.go` | create (PM and PB tests, scripted SSE helper, repo fixture) | ~700 | new |
| `cmd/tui/tools_leader_e2e_test.go` | create (LE tests) | ~300 | new |
| `cmd/tui/headless_record_test.go` | modify (provider of the six cassette tests, N4) | ±30 | itself |
| `cmd/tui/testdata/cassettes/*.yaml` | re-record six files (N4) | regenerated | cassette tests |

## Tests Before

`go test ./...` on the phase 8 tree: everything passes except the six N4 cassette tests; record the list.

## Tests After (write first; each maps to one matrix row)

| Test | Entry | Row |
|---|---|---|
| `TestPrintReadsFileWithLineNumbers`, `TestPrintReadPagesWithOffset` | PM | `read` |
| `TestPrintReadImageReturnsImageBlock` | PM (`--provider anthropic` with a fake key, vision model; the next request body holds the image block) | `read` image |
| `TestPrintReadLargeImageIsResized` | PM (4000x3000 PNG) | image resize |
| `TestPrintReadImageForNonVisionModelAddsNote` | PM (a catalog model whose `Input` has no `image`) | non-vision note |
| `TestPrintWritesFileAtomically`, `TestPrintWriteRefusesGeneratedFile` | PM | `write` |
| `TestPrintEditsFileAfterRead`, `TestPrintEditRejectsStaleFile` | PM | `edit` |
| `TestPrintListsDirectory` | PM | `ls` |
| `TestPrintGrepFindsMatches` | PM | `grep` |
| `TestPrintFindWithoutFdUsesFallback` | PM (`deps.environ` and `PATH` without `fd`) | `find` via `rg --files` |
| `TestPrintFindWithoutFdAndRgUsesWalk` | PM (`PATH` without `fd` and `rg`) | `find` via Go walk |
| `TestPrintBashRunsTestsAndReportsExit` | PM | `bash` foreground |
| `TestPrintBashBigOutputKeepsHeadAndTail` | PM | head and tail view, spill 0600 |
| `TestPrintBashSpillStopsAtCap` | PM (70 MB output) | spill cap 64 MB, job not killed |
| `TestPrintJSONStreamCarriesBashPartialUpdates` | PM `--mode json` | live partial updates |
| `TestPrintBashMovesToBackgroundAfterTimeout` | PM | move to background |
| `TestPrintBashRunInBackgroundReturnsJobID` | PM | `run_in_background` |
| `TestPrintBashRejectsTrailingAmpersand` | PM | trailing `&` |
| `TestPrintSIGINTDuringBashKillsProcessGroup` | PB (child `trap '' TERM` plus a grandchild) | cancelled `bash` |
| `TestPrintJobOutputAnswersRunningJobAtOnce`, `TestPrintJobOutputWaitReturnsFinalStatus` | PM | `job_output` |
| `TestPrintJobKillStopsJobWithoutNotice` | PM | `job_kill` |
| `TestPrintJobListShowsJobs` | PM | `job_list` |
| `TestPrintEleventhJobIsRefused` | PM (9 bash jobs and 1 monitor) | cap, monitors count |
| `TestPrintCompletionNoticeStartsFollowUpCycle` | PM (two prompts; job finishes during the second) | completion notice |
| `TestPrintJSONStreamCarriesNotice` | PM `--mode json` | notice kept in the log and stream |
| `TestPrintNoticeAfterLastPromptIsAnswered` | PM (job ends during the last prompt; reply to the notice is printed) | print end order (N2) |
| `TestPrintMonitorDeliversEveryStdoutLine` | PM | monitor events (every stdout line) |
| `TestPrintMonitorFloodGuardSuppressesEvents` | PM (script prints one line every 100 ms for 6 s) | flood guard |
| `TestPrintMonitorOverloadKillsProcess` | PM (script prints without end; about 35 s, runs in parallel) | overload kill of the group |
| `TestPrintMonitorStopsAtTimeout` | PM (`timeout_ms: 500`) | monitor timeout |
| `TestPrintMonitorRejectsTimeoutAboveMax` | PM | timeout validation |
| `TestPrintMonitorPersistentRunsUntilJobKill` | PM (`persistent: true`, then `job_kill`) | persistent monitor |
| `TestPrintCommandGuardBlocksWhenEnabled`, `TestPrintCommandGuardOffByDefault` | PM | command-guard |
| `TestPrintExitKillsBackgroundJobs` | PB (background `trap '' TERM; sleep 300`) | exit cleanup (N2) |
| `TestPrintBashEnvironmentHidesSecrets` | PM (`deps.environ` with `FAKE_API_KEY`, `GH_TOKEN`) | env filter |
| `TestPrintRequestCarriesToolPromptSection` | PM | prompt snippets |
| `TestPrintCoercesStringArguments`, `TestPrintEditAcceptsLegacyShape` | PM | coercion |
| `TestLeaderSessionCloseKillsBackgroundJobs` | LE | `session/close` cleanup |
| `TestLeaderShutdownKillsBackgroundJobs` | LE (three sessions, `trap '' TERM` jobs; stop < 8 s) | leader shutdown cleanup |
| `TestInProcessClientCloseKillsBackgroundJobs` | `openInProcessClient` (`cmd/tui/tui_client.go:88`), the TUI fallback entry | in-process client close cleanup |
| `TestLeaderToolUpdatesCarryKindLocationsAndDiff` | LE | ACP mapping |
| `TestLeaderNoticeIsNotSentAsUserFrame` | LE | notice hidden from standard ACP frames |

## Function and interface checklist

- [x] scripted SSE helper `scriptModel(t, steps ...step) rtFunc` with per-request body assertions
- [x] repo fixture `newToolRepo(t)`
- [x] `requireGroupGone(t, pgidFile)`
- [x] N4 provider switch and re-record

## Dependency map

Needs phase 8 (and so all earlier phases). Blocks phase 10. Runs alone.

## Related Code Files

- Create: `cmd/tui/tools_e2e_test.go`, `cmd/tui/tools_leader_e2e_test.go`.
- Modify: `cmd/tui/headless_record_test.go`, `cmd/tui/testdata/cassettes/*.yaml`.

## Implementation Steps

1. Write the helpers and the tests; run them; fix product code only through the owning phase's files (report a gap; do not add a test-only path).
2. N4: re-record the six tests one by one with `ASK_RECORD=1 go test -run '^TestX$' ./cmd/tui` and `OPENAI_API_KEY` from the environment; run `TestCassettesHoldNoSecrets`; check that no cassette holds the key or a `bash` call.
3. Run `go test ./... -race` twice.

## Regression Gate

`go test ./... -race && go vet ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`

## Success Criteria

- [x] Every matrix row has its named test, and it passes.
- [x] `go test ./...` passes with the re-recorded cassettes; no cassette contains a `bash` call or a secret.
- [x] After the full `cmd/tui` run, `pgrep -f 'sleep 300'` started by the tests finds nothing.

Exceptions: `TestLeaderSessionCloseKillsBackgroundJobs` skips because `session/close` is unsupported on the leader wire (`internal/leader/policy.go:75`); `TestPrintReadImageForNonVisionModelAddsNote` builds the agent with `newNativeAgentOn` and a text-only model row, because the catalog has no model without image input. See the phase 9 report.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Timing flakes in background and notice tests | M x M | File gates instead of sleeps; generous bounds; `-count=2` in step 3; signal: a flake in CI; response: add a gate, never a sleep. |
| OpenAI cassettes differ in event shape from the Token Plan ones (thinking, event order) | M x L | Adjust the assertions to the same user-visible behavior; signal: a cassette test cannot express its old intent; response: keep that one test on a scripted SSE server instead of a cassette. |
| The recording leaks the key | L x H | The recorder keeps only an allow-list of headers (`internal/providers/cassette/cassette.go:41-45,247`); `TestCassettesHoldNoSecrets` fails on a leak. |
| Built-binary tests leave a leader or child behind | L x M | `t.Cleanup` stops the leader and checks the pgid files (process-management rule). |

## Security Considerations

- Fake secrets only in env tests; the real `OPENAI_API_KEY` is read from the environment for recording only and never printed.
- Cassettes are checked by `TestCassettesHoldNoSecrets`.
