# Spike: system-reminder job notice across providers

Date: 2026-10-10. Status: PARTIAL, extended once (see "Second session" at the end). Final conclusion is at the end.

## Question

Does a job-completion notice sent as a `user` message wrapped in `<system-reminder>` tags (Grok CLI shape) make each provider model behave correctly? Correct means: the model treats it as a system notice, reads the job output or continues the task, does not thank the user, and does not echo the tags.

## Method

- A throwaway test (build tag `spike`, package `main` in `cmd/tui`) used the real headless agent: `newHeadlessAgentWithAuth`, the real provider adapters, the real auth service.
- Two spike-only tools: `run_command` (always returns the DeepSeek "moved to background job bash-1" text) and `job_output` (returns the 42 passing / 1 failing text).
- Prompt: "Run the test suite with run_command (command: npm test) and tell me if anything fails." After the agent settled, the notice was sent with the real `Agent.FollowUp`. Then `WaitForIdle`.
- Notice text: `<system-reminder>\nBackground job bash-1 (bash: npm test) finished [status: completed, exit code: 1]. Read its output with job_output.\n</system-reminder>`.
- Credentials: OpenAI API key from the environment; ChatGPT and Anthropic OAuth from a copy of `~/.ask-t1a-oauth`. No secret was printed.

### Two changes to the planned method (both needed for a valid test)

1. Pilot runs (2 runs, OpenAI gpt-5.5) showed that the model called `job_output` on its own before any notice, because the tool is visible and returned the final text at once. The notice then had nothing to trigger. Fix: `job_output` returns `npm test\n[status: running]` until the test sends the notice, and then returns the final text. Also the `run_command` description says that a long command moves to a background job and that the system sends a notice when it finishes. Both changes match what a real job table does.
2. The two pilot runs are not scored. They are listed below as an extra data point.

## Results (tagged notice only)

| Provider / model | Run | (a) called job_output after notice | (b) reported failure correctly | (c) addressed user as author | (d) repeated tags |
|---|---|---|---|---|---|
| OpenAI API key, gpt-5.5 | 1 | yes | yes (42 pass, 1 fail, auth.spec.ts:18, 200 vs 401, exit 1) | no | no |
| ChatGPT OAuth, gpt-5.6-sol | 1 | yes | yes (same facts) | no | no |
| Anthropic OAuth, claude-sonnet-4-6 | 1 | yes | yes (same facts) | no | no |

Quotes after the notice:

- gpt-5.5: tool call `job_output {"job_id":"bash-1"}`, then "The test suite failed. Results: 42 passing, 1 failing. Failure: `test/auth.spec.ts:18` - Expected HTTP status `200`, got `401`. Exit code: `1`."
- gpt-5.6-sol: tool call `job_output`, then "The test suite failed with **1 failing test**: `test/auth.spec.ts:18` - expected HTTP **200**, received **401**. Summary: **42 passing, 1 failing**."
- claude-sonnet-4-6: "The test suite just finished! Let me grab the output." then tool call `job_output`, then a full failure summary and an offer to help fix it. The first sentence treats the notice as an event ("just finished"), not as a message from the user. There is no thanks and no tag in the reply.

Before the notice, gpt-5.5 said "`npm test` is still running ... No failures have been reported yet" (correct for the state it saw).

### Pilot runs (not scored, notice sent after the model had already read the output)

Both runs: gpt-5.5 had called `job_output` itself, so after the notice it did not call the tool again and answered "`npm test` has already completed and failed", with the correct facts. No thanks, no tags. This shows the model reads the notice as information about the job, not as a user request.

## Not done (budget)

The cap was 30 model calls. The pilots used 8, gpt-5.5 used 5, gpt-5.6-sol used 10, claude-sonnet-4-6 used 5: 28 calls in total. Because of this:

- No second run per provider (variance is not measured).
- No plain-text control (notice without tags) for any provider. We cannot say if the tags help or if plain text works as well.

## Surprises

- gpt-5.6-sol used 10 calls in one run. The two calls after the notice are normal, so about 8 calls came before the notice. The model polled `job_output` many times while the job was "running", even with the "wait for the notice" hint in the `run_command` description. I did not save that part of the transcript, so I cannot quote it. Impact: a model may poll a running job in a loop. The real `job_output` should return fast with a running status, and its description should tell the model not to poll.
- gpt-5.5 also polled once before the notice (one extra `job_output` call, then "still running").
- All three models called `job_output` even when `job_output` was not needed to know the result was bad (the notice says exit code 1). They treat "Read its output with job_output" as an instruction in the notice.
- Both OAuth refresh tokens rotated during the spike (OpenAI generation 2 to 3, Anthropic generation 1 to 2).

## Credential handling

- The OAuth tokens rotated, so the updated `auth.json` was copied back to `~/.ask-t1a-oauth/auth.json`. The old file is saved as `~/.ask-t1a-oauth/auth.json.before-spike`. Delete that backup when you no longer need it.
- The copy `/private/tmp/ask-spike-oauth` and the empty home were deleted.

## Conclusion against the decision rule

Rule: keep the Grok shape if every provider reacts to the notice and does not echo it as the user's words; otherwise add a provider-specific wrapper.

With one scored run per provider, all three providers (gpt-5.5, gpt-5.6-sol, claude-sonnet-4-6) reacted by reading the job output, reported the failure correctly, did not thank the user, and did not repeat the tags. Evidence supports keeping the Grok shape for these three. Confidence is limited: one run each, no control, and Alibaba Token Plan (expired) was not tested. A short follow-up with about 20 calls (second run per provider plus one plain control each) would close the gap.

## Cleanup

The spike test file was deleted. `git status` shows no change from this spike except this report. An untracked `internal/sandbox/spike_runner_test.go` exists; it is not mine.

## Unresolved questions

- Should the follow-up (second runs and plain controls, about 20 calls) go ahead?
- Is the polling of gpt-5.6-sol a concern for the real `job_output` design?

## Second session (extension approved by the coordinator, cap 24 calls)

Same method as before, with one addition: the test counted `job_output` calls made while the job showed "running", and stopped the agent after 3 such polls (so a polling loop could not use the budget). Calls used: 21 (anthropic tagged 5, anthropic plain 9, chatgpt tagged 7). Three calls were left, not enough for a full run, so these runs were NOT done: second gpt-5.5 run, gpt-5.5 control, gpt-5.6-sol control.

### New rows

| Provider / model | Run | Notice | (a) job_output after notice | (b) failure correct | (c) addressed user as author | (d) repeated tags | job_output polls while running |
|---|---|---|---|---|---|---|---|
| gpt-5.5 (API key) | 1 | tagged | yes | yes | no | no | 1 |
| gpt-5.6-sol (ChatGPT OAuth) | 1 | tagged | yes | yes | no | no | 1 (not counted then) |
| claude-sonnet-4-6 (Anthropic OAuth) | 1 | tagged | yes | yes | no | no | 1 (not counted then) |
| claude-sonnet-4-6 | 2 | tagged | yes | yes | no | no | 1 |
| claude-sonnet-4-6 | 3 | plain (control) | yes | yes | no | no (no tags sent) | 5 |
| gpt-5.6-sol | 2 | tagged | yes (1 call after notice) | yes | no | no | 4 (stopped by the test limit of 3) |

Quotes:

- sonnet tagged run 2: "The test run has finished! Let me grab the output now." Then a correct summary (42 passing, 1 failing, auth.spec.ts line 18, 200 vs 401).
- sonnet plain control: "The job finished with exit code 1 - that means there were failures. Let me grab the output now!" Then the same correct summary. Same behavior as tagged. No thanks to the user.
- gpt-5.6-sol tagged run 2: one `job_output` call after the notice, then "The test suite failed with exit code **1**. 42 tests passed, 1 test failed. Failure: `test/auth.spec.ts:18`, expected HTTP status 200, received 401."

### Control comparison

Only one provider has a control (Sonnet). Tagged and plain notices gave the same correct behavior: one `job_output` call, correct report, no thanks, no echo. No sign that the tags help or harm Sonnet. For gpt-5.5 and gpt-5.6-sol there is no control, so we do not know if the tags matter for them.

### gpt-5.6-sol polling

In run 2 the model called `job_output` 4 times while the job was running, without waiting for the notice, even with the hint in the `run_command` description and the "You will be notified" text in the tool result. The test stopped it at the limit of 3, so the real count in an unlimited loop is higher. In the first session the same model used 10 calls in total, which is consistent with 7 to 8 polls. Sonnet polled 1 time (tagged) and 5 times (plain control, one run) before the notice. gpt-5.5 polled 1 time. The polling does not come from the notice, and it is the same in tagged and plain runs. It is a property of exposing `job_output` while a job runs.

### Final conclusion

Rule: keep the Grok shape if every provider reacts to the notice and does not echo it as the user's words; otherwise add a provider-specific wrapper.

Evidence: 5 scored tagged runs (gpt-5.5 x1, gpt-5.6-sol x2, sonnet x2) and 1 plain control (sonnet). In all 6 the model reacted to the notice by reading the job output, reported the failure correctly, did not thank the user, and did not repeat the tags. No provider needed a wrapper. Decision: KEEP the Grok shape.

Remaining limits: gpt-5.5 has one run; only Sonnet has a control; Alibaba Token Plan was not tested (key expired). Separate finding for the jobs design: models poll `job_output` while a job runs (gpt-5.6-sol the most). The real `job_output` should answer a running job fast with a short status and a "do not poll, a notice will arrive" line.

### Credentials and cleanup (second session)

Both refresh tokens were unchanged at the end (the copy matched `~/.ask-t1a-oauth/auth.json`), so nothing was copied back and `auth.json.before-spike2` was not needed. `auth.json.before-spike` from the first session still exists. The temp copies and the spike test file were deleted. This session changed no file other than this report.
