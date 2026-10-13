# Real-provider acceptance: H5 built-in tools

Date: 2026-10-11. Branch `new-session`, uncommitted H5 state (phases 1 to 9 done).

## Result

| Run | Provider and model | Credential | Outcome |
|---|---|---|---|
| A | `openai` / `gpt-5.5` | API key from the environment | PASS. `read`, `edit`, `bash` seen; file fixed; `go test ./...` passes; exit 0; no process left. |
| B | `anthropic` / `claude-sonnet-4-6` | Anthropic OAuth, copy of `~/.ask-t1a-oauth` | PASS. Same criteria. The refresh token rotated during the run. |
| C | `openai` / `gpt-5.6-sol` | ChatGPT OAuth (`openai-chatgpt`), same copy | NOT VERIFIED. The refresh of the stored credential was rejected by the provider ("provider rejected exchange"); no request reached the model. The credential needs a new sign-in. |
| D | background check, same session | A and B | PASS on the mechanism; see "Background check" for the print-mode limit. |

The roadmap exit criterion (print mode reads a repository, edits a file, runs its tests, with a real provider) holds for OpenAI (API key) and Anthropic (OAuth). The ChatGPT OAuth run is unverified, not failed in the tools.

## Setup

- Binary: `go install ./cmd/tui` with `GOBIN` set to a temp directory (the `ask` binary, named `tui` by its package directory).
- Repository: a new temp directory per run with `go.mod` (`module calc`, `go 1.21`), `calc.go` (`Add` returns `a - b`), `calc_test.go` (`TestAdd` wants 5 from `Add(2, 3)`) and `git init`. The same files as the phase 9 fixture, without the extra files.
- Environment of the child: every name that looks like a credential and every `ASK_*` name was unset first. `OPENAI_API_KEY` was put back for run A only. `ASK_HOME` was `/private/tmp/ask-h5-home` (empty, mode 0700) for run A and `/private/tmp/ask-h5-oauth` (copy, mode 0700) for B and C. Standard input was `/dev/null`.
- Commands (the key is read from the environment and is never an argument):

```sh
# run A (OPENAI_API_KEY=<from env>)
ASK_HOME=/private/tmp/ask-h5-home ask -p --mode json --provider openai --model gpt-5.5 -- \
  "The test in this repo fails. Find out why, fix the code and run the tests." \
  'Run "sleep 5; go test ./..." in the background and tell me the result when it finishes.'
# run B (no OPENAI_API_KEY)
ASK_HOME=/private/tmp/ask-h5-oauth ask -p --mode json --provider anthropic --model claude-sonnet-4-6 -- <same two prompts>
# run C (no OPENAI_API_KEY)
ASK_HOME=/private/tmp/ask-h5-oauth ask -p --mode json --provider openai --model gpt-5.6-sol -- <same two prompts>
```

## Exit criterion runs (first prompt)

| Run | Tool calls in order | `go test ./...` after the run | Exit code | Wall time (both prompts) |
|---|---|---|---|---|
| A | `ls`, `read` x3 (parallel), `edit`, `bash` (`go test ./...`, exit 0) | pass | 0 | 13 s |
| B | `ls` and `bash` (`find`, parallel), `read` x2 (parallel), `edit`, `bash` (`cd . && go test ./...`, exit 0) | pass | 0 | 30 s |
| C | none | fail (unchanged file) | 0 (see observation 2) | under 1 s |

In both passing runs the `edit` used Pi's `edits[{oldText,newText}]` shape and the tool result said `Successfully replaced 1 block(s) in calc.go.` The `read` results had the `N|text` line numbers. Parallel `read` calls ran together.

## Background check

Prompt 2 of each run asks for `sleep 5; go test ./...` in the background and its result when it finishes.

- Run A: the model used `bash` with `run_in_background: true`; the tool answered `Started background job bash-3. A notice will arrive when it finishes; do not poll job_output.` The model said it would report, and `ask -p` ended. The job was still running, so print mode ended it (the designed behavior: print mode waits for the Agent to be idle, not for jobs). No notice could arrive in this flow. Poll count: 0.
- Run B: same start. The model then called `job_output` without `wait` (answered at once with `[status: running]` and the do-not-poll line), then with `wait: true` (returned `ok calc (cached)`, `[status: completed, exit code: 0]`). Poll count: 2. The model also wrote the words "job has finished" before that call, which the log shows to be wrong at that time; it is a model habit, not a tool fault.
- Because print mode cannot show a notice after a single background prompt, a third prompt was added in extra runs (A2 on `gpt-5.5`, B2 on `claude-sonnet-4-6`): `Run "sleep 7" in the foreground with bash (timeout 20), then tell me the result of the background job.`
  - A2: the background job `bash-3` finished during the foreground `sleep 7`. The next request held the `<system-reminder>` notice (`Background job bash-3 (bash: sleep 5; go test ./...) finished [status: completed, exit code: 0]. Read its output with job_output.`). The model made one `job_output` call, got `ok calc (cached)`, and reported the result. The notice is in the JSON stream as an ordinary `user` message. Poll count: 1. Wall time 29 s, exit 0.
  - B2: the model polled twice (once without `wait`, once with `wait: true`) before the job ended, so the job was awaited and no notice was sent. Poll count: 2. Wall time 33 s, exit 0.
- After every run no `sleep` or `ask` process of the run was left (checked with `ps`).

## Credentials and rotation

- Fingerprints are the first 10 hex characters of a SHA-256 of the stored value; no token value was printed or saved.
- Before: Anthropic generation 2, refresh fingerprint `c931a86b34`, access token expired; ChatGPT generation 3, refresh fingerprint `cf5c5e0a`, access token expired; revision 8.
- After run B: Anthropic generation 3, new refresh fingerprint (`b16caeb83d`). The refresh token rotated, so the stored one in `~/.ask-t1a-oauth` was no longer valid.
- After run C: ChatGPT unchanged (generation 3, same fingerprint) with a `refreshState` of `pending`; the service reports `refresh result uncertain; sign in again`. Revision 11.
- Action (plan unresolved question 2): `~/.ask-t1a-oauth/auth.json` was backed up as `auth.json.before-h5` (mode 0600) and the refreshed `auth.json` from the copy was copied back (mode 0600). Only the Anthropic credential rotated. The ChatGPT entry in the home now holds the `pending` refresh state, which has the same effect as the rejected refresh: it needs a new `ask auth login`.
- The copy under `/private/tmp/ask-h5-oauth` was deleted.

## Deviations and observations

1. Run C could not run. The ChatGPT refresh token was rejected by the provider, so the run ended in an assistant error before any tool call. Recorded as unverified. A new `ask auth login` for ChatGPT, then one more run of the same command, closes it.
2. `ask -p --mode json` exits with code 0 when the run ends in an assistant error (run C); print mode (text) exits 1 in that case (`printReply`). This is the behavior since before H5 (`cmd/tui/headless.go`, the `modePrint` branch). `CLAUDE.md` now says it. It is not changed here.
3. When standard input is an open pipe that never closes, `ask -p` waits for it (`readPipedStdin`, `cmd/tui/input.go`). One early run hung for more than 8 minutes because the harness left stdin open; it was stopped with SIGQUIT (stack in `readPipedStdin`) and run again with `< /dev/null`. This is the documented piped-input behavior, not a tool defect. It differs from the one-time hang that the phase 9 report could not reproduce.
4. In run A2 the `message_end` event of the notice appears after the assistant message that already reacted to it (the model text said `NOTICE: bash-3 finished in background.` before the event). The order of events in the JSON stream for a queued notice should be checked in a later phase; the model request itself held the notice once.
5. The prompt of the plan says the model "reports the result when it finishes". In a one-prompt `ask -p` session this cannot happen after the last reply (N2 = A). The third-prompt runs above show the notice path.

## Cleanup and checks

- Temp directories, copies, scripts, transcripts and the temp binary under `/private/tmp` (`ask-h5-*`) were deleted. Transcripts held model output only, no credential.
- No process of the runs is left. One `ask leader --spawned-by-client` that is 14 hours old belongs to another session's test (`ask-acp-e2e-*` in the system temp directory) and was not touched.
- Secret scan: this report and the files changed in phase 10 were searched for `sk-`, `Bearer `, `refresh_token`, `api_key=` and long token-like strings. No match.

## Unresolved questions

- Should `--mode json` exit 1 on an assistant error, as print mode does? Outside H5.
- Should the ChatGPT OAuth credential be signed in again so the third run can be closed?
- Should the JSON event order of a notice (observation 4) be investigated?
