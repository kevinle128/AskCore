# Changelog

All notable changes to this project are written in this file.

The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/). This project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Agent:** `internal/tasks` is a per-Agent registry for background work. Each task ends in exactly one terminal state and produces exactly one notice. No tool uses it yet ([design](docs/async-task-runtime.md), [ADR 0004](docs/adr/0004-agent-owned-async-task-runtime.md)).
- **Agent:** `Agent.Notify` delivers task notices. A notice joins the next turn of a running agent or starts a run on an idle agent. Nothing sleeps or polls to detect completion.
- **Agent:** `Agent.Idle` and `Agent.WaitForQuiescence` wait by signal for the end of runs and tasks.
- **Protocol:** `protocol.CustomMessage` (role `custom`) is an application message that the model gets as a user message. Task notices use it with the custom type `task-notification`. The events `task_start`, `task_update`, and `task_end` report tasks.
- **Settings:** Task output files are written under `<ASK_HOME>/tasks/<session>/` with mode 0600. Session directories older than 7 days are removed once when a process starts.
- **CLI:** `ask -p --max-turns <n>` ends the run after `n` turns, also the turns that a background notice starts. It prints `Error: Reached max turns (n)` and exits with code 1.
- **CLI:** `ASK_PRINT_BG_LIMIT_MS` sets the default background time limit of `ask -p`, and `ASK_PRINT_BG_WAIT_CEILING_MS` sets the idle cap of its wait for background work.
- **Tools:** In `ask -p`, a background job stops after 10 minutes by default and after 2 hours at most. `bash` with `run_in_background` sets the limit with `timeout`. Every `monitor` stops after at most 10 minutes. The model gets a notice when a limit stops a job.
- **Tools:** `ASK_DISABLE_BACKGROUND_TASKS=1` makes a foreground `bash` command stop at its wait budget and refuses background jobs and `monitor`.
- **Models:** `/model` lists a generated model catalog. When Ask can read the model list of the signed-in account, `/model` shows only the models that the account can use.
- **Models:** Ask refreshes the account model lists and the models.dev metadata at start, after a sign-in or a sign-out, and on `ask models refresh`. It keeps them in `<ASK_HOME>/model-cache.json` for 2 hours. A failed refresh keeps the last good list.
- **CLI:** `ask models refresh [--provider <id>]` refreshes the model lists now. It needs no leader and no database. It prints one line for each provider and one metadata line.
- **CLI:** `ASK_DISABLE_MODEL_REFRESH=1` turns the automatic model list refresh off. `ask models refresh` still refreshes.
- **Protocol:** `ACPModel` has the fields `costUnknown` and `notInAccount`. A session start that cannot use the saved model because the account list does not have it gives the fallback reason `not_in_account`.
- **TUI:** `/model` shows `?` in the window column of a model with an unknown context window. The footer row shows `?` in place of the context bar for such a model. The running model that the account list no longer has shows the note `not in account list` (the tag `unlisted` under 60 columns). A saved default that the account list does not have gives a start notice with the same reason.

### Changed

- **Protocol:** An object with `"role": "custom"` now decodes to `protocol.CustomMessage`, not to `protocol.RawMessage`. Such an object without `customType` is a decode error.
- **Agent:** Task notices do not count toward `MaxQueuedInputs`. `Abort` keeps background tasks running and moves their queued notices to the context queue. A notice that arrives after `Abort` of an active run waits there until the next human input. `Reset` stops the tasks of the old conversation; `Dispose` stops and waits for every task.
- **CLI:** `ask -p` no longer stops background jobs after the last reply. It prints that reply, waits for the jobs, prints the reply of each run that a notice starts, and stops the jobs that still run when it exits.
- **Tools:** A background job belongs to the conversation that started it. After a conversation reset the old jobs run on until the session closes, but their notices are dropped, and `job_list`, `job_output`, and `job_kill` do not see them. The limit of 10 jobs still counts them.
- **TUI:** The status data (working directory, model, thinking level, context use) moved from the header row at the top to a footer row above the shortcut bar. The header row stays and shows only the logo `◢◤ ask`, with a rule under it. The footer row names the model as `<provider>/<model>`, and the working directory stays from 60 columns up.
- **TUI:** The footer row no longer shows the link (the leader id, `in-process`, an observer, `starting` or `lost`) or the session cost. A lost link still shows in the status row and in the notices. The reply meta line keeps its cost.
- **TUI:** The block pane no longer shows the model line (`model  <provider>/<model>` at the start and the `· was` line after a change). The footer row names the model. The fallback note and the `TEA_TRACE` warning are notices of their own at the start of the transcript.
- **CLI:** `ask -p --model` accepts the model ids of the generated catalog with no network request.
- **CLI:** `ask auth login` reads the model list of that provider in its own process, and `ask auth logout` deletes the cached list of that provider.
- **Providers:** `gpt-5.5` gets the thinking level `xhigh` and now sends the reasoning effort. `claude-sonnet-4-6` gets the thinking level `max`. On both models a saved `minimal` level becomes `off`.
- **Providers:** On `grok-4.7`, thinking `off` now sends the lowest reasoning effort.
- **Providers:** An Anthropic model without reasoning support always sends thinking as disabled.
- **Providers:** In `ask -p`, `--thinking off` on a model that cannot turn thinking off (`grok-4.7`, `gpt-6-astra`, `gpt-6.1-sol`) sends no reasoning effort, so the vendor default applies. The `/model` selector and the thinking key of the TUI use the lowest effort instead.
- **Providers:** The ids `gpt-6-terra` and `gpt-6.1-luna` no longer show in `/model`, because the model metadata has no row for them.
- **TUI:** The header row and its rule take one row from a box in the prompt pane slot. On a terminal of 14 rows, the browser sign-in box shows two rows of the address and not three.
- **Dependencies:** `charm.land/fantasy` uses the fork commit `e894229`. A reasoning effort that the caller sets now reaches the request for model ids that the library does not know, such as `gpt-6` and `grok-4.7` ([upstream pull request](https://github.com/charmbracelet/fantasy/pull/443)).

### Fixed

- **Providers:** The `gpt-6` models (astra, luna, sol, terra) now send their thinking level and show it in the `/model` selector. `gpt-6-astra` and `gpt-6.1-sol` refuse "none", so thinking off in the selector uses their lowest effort.
- **Providers:** `grok-4.7` now sends its thinking level (`low`, `medium`, `high`, `xhigh`). It cannot turn thinking off, so the selector uses `low` for off.
- **Settings:** Two `ask` processes that use a new `ASK_HOME` at the same time no longer fail with `file exists`.
- **Editors:** A prompt over ACP binds only to its own run. A run that a background notice started is no longer taken for the prompt.
- **Tools:** Output lines of a `monitor` are escaped, so command output cannot close the `monitor-event` element or the `<system-reminder>` around it.

## [0.1.0] - 2026-10-15

### Added

- **Project:** Ask is released under the MIT License.
- **CLI:** `ask -p "<prompt>"` runs one prompt without a terminal UI and prints the reply. `--mode json` writes every agent event as one JSON line.
- **CLI:** `ask -p` accepts piped stdin, `@file` arguments, several messages, `--thinking`, `--model`, and `--api-key`.
- **CLI:** `ask version [--json]` prints the build and the protocol versions.
- **Providers:** Alibaba Token Plan, Anthropic, OpenAI, and xAI work through `--provider`.
- **Providers:** The demo provider `faux` answers with a script, so you can try Ask without an account. It is the default of `ask -p`.
- **Auth:** `ask auth login` and `ask auth logout` manage API keys and the sign-ins for Claude Pro and Max, ChatGPT Plus and Pro, and xAI Grok. Ask refreshes saved sign-ins.
- **Agent:** The agent retries transient provider failures, can be cancelled, and recovers from failed tool calls.
- **Tools:** Every agent has the tools `read`, `write`, `edit`, `ls`, `grep`, `find`, `bash`, `job_output`, `job_kill`, `job_list`, and `monitor`, and a demo tool `echo`.
- **Tools:** A `bash` call that runs past its wait budget (120 s by default, 600 s at most) continues as a background job. A finished job tells the model through a `<system-reminder>` notice.
- **Tools:** `ASK_COMMAND_GUARD=1` blocks a short list of destructive shell commands. It is not a security boundary.
- **Editors:** `ask acp` serves the Agent Client Protocol v1 over standard input and output.
- **Leader:** `ask leader` runs a local router on a Unix socket (`<ASK_HOME>/leader.sock`, owner only). It holds one agent for all local clients. `ask leader status|list|stop` manage it.
- **Leader:** `ask connect` is a line-oriented client of the leader.
- **TUI:** `ask` with no arguments on a terminal opens a full-screen TUI on the alternate screen. It connects to the leader and starts one when none runs. It runs an agent in the same process only when the first connect fails.
- **TUI:** Tool calls, thinking, and hooks show as foldable blocks. A full-screen viewer shows one block. The mouse wheel scrolls, a click selects a block, and a drag copies text.
- **TUI:** The commands `/model`, `/providers`, `/new`, `/sessions`, `/take`, `/copy`, and `/quit` are available.
- **TUI:** `/providers` signs in to a provider (API key, browser URL, copy-code, or device code) and removes a saved sign-in. Without a usable provider the session starts with no model and says so. `/model` lists only the models of usable providers and saves your choice.
- **Testing:** `ASK_CAPTURE=<dir>` records the provider traffic of an `ask -p` run to a cassette. Tests replay recorded model replies.
- **Testing:** `python3 e2e/tui/run.py` drives the built `ask` on real terminal engines (Alacritty and Ghostty).
- **Docs:** `docs/reference/` lists the built-in tools and the providers. The test suite generates it from code and fails when it is out of date.
- **Server:** `cmd/server` is a demo daemon. It serves HTTP (`/health`, users, posts) and gRPC (`Greeter`) on SQLite with embedded migrations. `-migrate` applies the migrations and exits.

### Changed

- **TUI:** The interactive TUI moved from inline output to the alternate screen.
- **Leader:** A client of a newer build now replaces an older leader that a client started. The sessions and background jobs of the old leader end. Clients retry a failed leader start.
- **Build:** Ask now needs Go 1.27 or later.

### Fixed

- **Leader:** On Linux, a file that is not a socket at the leader socket path now stops the client with an unsafe-endpoint error. Before, the client tried to start a leader three times and then failed.
- **Providers:** A model that the account cannot use is reported as its own error, not as a generic failure.
- **TUI:** Rows use the full terminal width. A reconnect to a session no longer races on the session state.

[Unreleased]: https://github.com/kevinle128/AskCore/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/kevinle128/AskCore/releases/tag/v0.1.0
