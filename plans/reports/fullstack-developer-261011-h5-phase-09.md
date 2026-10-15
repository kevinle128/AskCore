# H5 phase 9 report: end-to-end acceptance

Status: completed with concerns. Every matrix row has its named test and it passes, except one row that skips (below). The six cassette tests replay on OpenAI.

## Verification

- `go test ./cmd/tui`: pass (about 231 s, the baseline was 237 s with `-race`). `go test -race ./cmd/tui`: pass (331 s), run twice; the first run found a real race (item 3 below).
- `go test ./...`: every package passes. `go vet ./...` clean. `golangci-lint run ./...`: 0 issues.
- `ps` after the runs: no `sleep 300`, no `tui.test`, no `ask-jobs-*` directory. Each job test checks `kill(-pgid, 0) == ESRCH` after its cleanup step.
- Key scan: `OPENAI_API_KEY` was read from the environment only. `TestCassettesHoldNoSecrets` now also compares every cassette with that variable and passes. No cassette has an auth header, a `bash` call (the `bash` name appears only in the tool declarations of each request) or a word-bounded `sk-` string.

## Files

- New: `cmd/tui/tools_e2e_test.go` (PM and PB tests, helpers, repo fixture), `cmd/tui/tools_leader_e2e_test.go` (LE and in-process tests).
- Changed: `cmd/tui/headless_record_test.go` (OpenAI provider, fixed session ID, `newCassetteAgent`), the six cassettes under `cmd/tui/testdata/cassettes/`, the phase file.

## Ownership exceptions (small, in place)

1. `cmd/tui/headless_cassette_test.go`: `useCassette` takes the provider and knows the `OPENAI_API_KEY` placeholder; `TestCassettesHoldNoSecrets` also checks `OPENAI_API_KEY`.
2. `cmd/tui/output.go` (race fix): `guardStdout` wrote `os.Stdout` on restore even when it had not changed it, so two `runWithDependencies` calls in one process raced (found by `-race`). It now restores only what it swapped. Regression test: `TestGuardStdoutKeepsOsStdoutForNonFileStderr`.
3. Job-table error line (the phase 7 concern), see below: `cmd/tui/args.go`, `headless.go`, `acp.go`, `tui_client.go`.

## N4: cassettes on OpenAI

- Provider `openai`, model `gpt-5.5`, API key from `OPENAI_API_KEY` in record mode, placeholder in replay. Five were re-recorded with `ASK_RECORD=1`; `TestCapturedSessionReplays` was re-captured with `ASK_CAPTURE` (`ask -p ... --provider openai --model gpt-5.5`, isolated `ASK_HOME`).
- The OpenAI Responses wire sends the session ID as `prompt_cache_key`. A random ID made every replay fail, so the cassette tests use one fixed ID (`newCassetteAgent` calls `newNativeAgent`, the same composition as `ask -p`). The captured file had its random ID replaced by the fixed one (same length); the test comment says to do that after a capture.
- `TestCassetteThinkingReply` keeps its assertion: at `--thinking medium` the OpenAI wire gives a `thinking` block. Event order and tool-call tests needed no change.
- `docs/testing-llm-cassettes.md` still describes Token Plan keys and the Token Plan model; phase 10 owns that edit. It should also mention the session ID rule.

## Job table `OnError` and the TUI (decided)

Reachable: the in-process TUI agent comes from `acpParams` and the job table wrote one `jobs:` line to `os.Stderr`, which is the terminal under the full-screen program. The leader writes to its log file (stderr is redirected), `ask acp` and print mode keep stderr.
Change: `options.onJobError` and `runDependencies.onJobError`; nil keeps the stderr line; `openInProcessClient` sets a no-op, so the TUI gets nothing on screen (the job stays readable with `job_list` and `job_output`). Tests: `TestJobTableErrorsReachTheConfiguredSink`, `TestJobTableErrorsDefaultToStderr`. The in-process wiring itself has no observable trigger from a test (a refused notice needs a session without a model, which cannot start a job), so it is covered by code review only.

## Deviations and gaps for the orchestrator

1. `TestLeaderSessionCloseKillsBackgroundJobs` skips. `session/close` is `callUnsupported` on the leader (`internal/leader/policy.go:75`) and in `internal/acp/agent.go:434`. The test sends the call and skips with that text, so it turns into a live check when the method lands. `Session.Close -> Dispose -> Release` is proven by the shutdown test (three sessions, jobs that trap TERM, stop in under 8 s) and the in-process close test.
2. `TestPrintReadImageForNonVisionModelAddsNote` cannot use `runWithDependencies`: no catalog model lacks `image` input. It builds the agent with `newNativeAgentOn` and a text-only row (the call that the ACP factory uses) and runs `runHeadless`; the model HTTP is the scripted server through the real adapter.
3. `TestPrintFindWithoutFd...` tests run serially: `find` and `grep` use `exec.LookPath` on the process `PATH`, so they use `t.Setenv`. Grep tests and the `rg` fallback skip when `rg` is not installed.
4. Leader tests use `--provider anthropic` through the fixture proxy; the Token Plan host has no fixture route.
5. After a built `ask -p` exits, a killed group can stay for a short time until init reaps it, so the two PB tests wait at most 10 s for ESRCH (`requireGroupGoneSoon`). In-process PM tests check at once after the run returns.
6. `TestPrintMonitorOverloadKillsProcess` needs about 33 s of real time (parallel). The overload kill also produces a completion notice (`[status: killed, reason: overload]`); the test asserts both the final event and that notice.
7. The matrix says the monitor flood guard delivers "first 10 events"; with a 200 ms batch the test asserts 10 to 14 delivered events and the suppression note, because the bucket refills one per 2 s during the 6 s run.
8. One early run of the bash/job batch hung for more than 120 s once and could not be reproduced in 8 further runs, three full package runs and two race runs. The cause is unknown; the likely place is a gate wait in one of the new tests (all waits are bounded by 30 s and a failure message, so a repeat would show a message). Flag it if CI shows it.

## Unresolved questions

- Should `session/close` be added to the leader wire (policy `callSession`, driver only)? This is a protocol decision outside H5.
- Should the cassette package normalize `prompt_cache_key` so captures from a real run need no manual ID edit?

Status: DONE_WITH_CONCERNS
