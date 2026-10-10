# Real-provider acceptance: provider setup in the TUI

Date: 2026-10-10. Build: working tree of branch `new-session` (compiled by the runner from `./cmd/tui`; the header showed `vcbad18b6cfad+dirty`).
Plan: [t1c-provider-setup](../261010-1424-t1c-provider-setup/plan.md), [phase 8](../261010-1424-t1c-provider-setup/phase-08-real-provider-and-docs.md).
No secret value is in this report. Only variable names and counts are recorded.

## Result

Scenarios 1 to 5 passed in the final run (41 recorded steps, 0 failed). The artifact scan found no key value in the output directory or in either leader log. Scenario 6 was not run. No product defect was found.

## Method

- Runner: `e2e/tui/real_provider_scenarios.py acceptance` (driver `e2e/tui/real_provider.py`). Terminal size 80×24. Output directory `/private/tmp/ask-t1c-out`.
- Homes: `/private/tmp/ask-t1c-key` (empty, created for the run) and `/private/tmp/ask-t1c-oauth` (a copy of `~/.ask-t1a-oauth`; the original was never used). The runner deleted both homes at the end. The copy is gone.
- Secret handling, in the order of the phase file: (1) the runner read `OPENAI_API_KEY` and every other credential variable of its environment; (2) it built the scan set from those values (23 values: the environment values, the made-up key, and the long string fields of the stored sign-ins of the OAuth copy); (3) it removed every `ASK_*` variable and every credential-like variable from the child environment, so the leader inherited none; (4) it started `ask`; (5) it sent the made-up key and then the real key as a bracketed paste through the PTY descriptor, never as an argument and never in a file; (6) it scanned the output directory and a copy of each leader log.
- Leader log sizes at the scan: 186 bytes (key home) and 64 bytes (OAuth copy).
- Model calls: one short prompt (`reply with the single word ok`) on `openai/gpt-5.5` with the API key. Across the earlier first run and the final run that is two prompts in total. The OAuth scenario made no model call (it checks lists, start line and change lines).
- Alibaba Token Plan: not used and no sign-in was tried (user decision).

## Scenarios

| # | Scenario | Result | Evidence (steps) |
|---|---|---|---|
| 1 | `providers_first_run` | pass | Footer `no model`; rail `Add a provider with /providers · then choose a model with /model`; no model line; `/model` shows `Use /providers to add providers.` |
| 2 | `key_refused` | pass | Made-up key shown as `(hidden)`; refusal `openai refused the key: <provider code> (HTTP 401). Nothing was saved.`; no `Checking` row after the answer; esc gave `Sign-in to openai cancelled. Nothing was saved.` |
| 3 | `key_saved_then_model` | pass | `openai: API key saved. 2 models ready.` and `Choose a model with /model.`; footer still `no model`; `/model` has the group `openai` only; choice of `gpt-5.5` gave the change line `model  openai/gpt-5.5` and `Default model: openai/gpt-5.5`; the prompt was answered (`ok`, 1.1 s, `↑53 ↓5`) |
| 4 | `remove_then_no_model` | pass | Confirmation `Remove OpenAI API key?`; `Removed the saved credential for openai.`; footer `no model`; a prompt gave `Not sent: no model` and the draft stayed |
| 5 | `oauth_rows_and_switch` | pass | Start line `model  openai/gpt-5.5` (saved default of the copy); `/providers` shows two `signed in` rows (Claude Pro/Max account, ChatGPT Plus/Pro subscription); `/model` has the groups `openai` and `anthropic`; three switches gave change lines with the right `was` pair and the footer model each time |
| 6 | Copy-code or device sign-in | not run | Manual step; the user was not present to approve it |
| - | `default_model_persists` | pass | After `leader stop` and a new `ask` in the same home the start line showed `model  openai/gpt-5.5` and the footer `gpt-5.5`; after removal a new session showed `no model` (no other provider was usable, so no fallback row) |
| - | `artifact_scan` | pass | 0 files with a value; 23 values scanned |

## Observations

- One earlier run had two failed steps in scenario 5: the second and third model switches did not happen. The check was the first version of the runner (a fixed sleep after `ctrl+l`, and a change-line pattern that also matched the start line, which made the first switch pass for the wrong reason). I rewrote the runner step to wait for the filter row, to check the footer model and the exact change line with its `was` pair, and the next two runs passed all three switches. The failure did not repeat (two full passes, one OAuth-only pass). It was not shown to be a product fault: the selector opens only after the model list arrives (`internal/tui/update_input.go:306`).
- The refusal line carries the provider code and the status as the design says. The test asserts the shape (`refused the key: ... (HTTP nnn)`), not the code text.
- The runner no longer inherits credentials into the leader. The earlier single-purpose scenarios (`repeat`, `cancel-*`, `ctrlc-running`, `switch`, `thinking`, `detach`) now save the key through `/providers` first. They were not run in this acceptance; only `providers`, `oauth` and `acceptance` were.
- Environment: the Bash hook of the session did not reject the compile step, but the runner compiles through Python (`ensure_ask`), as `e2e/tui/run.py` does.

## Cleanup

- `/private/tmp/ask-t1c-key` and `/private/tmp/ask-t1c-oauth`: deleted by the runner.
- Leader processes of the run: stopped with `ask leader stop` (rc 0 each time); no process of the run is left.
- Output directory `/private/tmp/ask-t1c-out` holds screens (plain text), `results.json` and the leader log copies; it has no secret value.
