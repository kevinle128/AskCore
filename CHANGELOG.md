# Changelog

All notable changes to this project are written in this file.

The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/). This project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **CLI:** `--system-prompt <text|file>` replaces the default system prompt, and `--append-system-prompt <text|file>` adds text after it. A value that names an existing file is read as a file. All prompt text of the flags is at most 256 KiB.
- **CLI:** `-t` / `--tools <names>` keeps only the named built-in tools, and `-nt` / `--no-tools` keeps none. A name that is not a built-in tool, or `-t` together with `-nt`, exits 1.
- **CLI:** The TUI and `ask acp` sessions take the same four flags: the TUI sends them to the leader or the in-process agent for each session it opens. `ask acp` has no flags, but a client can send `_meta["ask.dev/session"]` with `session/new`. A leader of an older build ignores the flags, and the TUI then shows a warning.
- **Prompt:** The system prompt now has named sections in a fixed order, and the working directory and the ISO date come last, so the start of the prompt stays the same between sessions.
- **Prompt:** `SYSTEM.md` replaces the default prompt, and `APPEND_SYSTEM.md` adds text to it. Ask reads them from `<project>/.ask/` (trusted projects only) and from `<ASK_HOME>`. The project file wins over the user file.
- **Prompt:** `AGENTS.md` and `CLAUDE.md` of `<ASK_HOME>` and of each folder from the git root to the working directory reach the model as an escaped user-role message, also in a project that is not trusted. A file is at most 1 MiB and all files together at most 64 KiB. A duplicate in one folder is left out.
- **Skills:** Ask lists the skills of `<ASK_HOME>/skills`, `~/.agents/skills`, and, in a trusted project, `.ask/skills` and `.agents/skills` as one user-role message. The model reads a skill with `read`. Skills follow the rules of Codex: a name is any text (white space collapsed, folder name when missing, cut to 64 characters), a description has no limit at load (the list cuts it to 1024 characters), a frontmatter that is not valid YAML is repaired once, and skills of the same name are all kept with their paths.
- **Skills:** The skill list takes `skillsMaxContextTokens` tokens (at most 10000), else 2% of the model context window, else 8000 characters. When it does not fit, descriptions are shortened, then skills are left out with one `Exceeded skills context budget` warning.
- **Settings:** The key `skillsMaxContextTokens` (a positive integer) sets the token budget of the skill list.
- **Settings:** The key `defaultTools` (an array of tool names, or of `+name` and `-name` entries) sets the built-in tools. An unset key keeps all tools, and `[]` keeps none. A project array replaces the user array.
- **CLI:** `ask -p` writes one `Warning:` line for the context files that Ask left out or cut, and up to four `Warning: skills:` lines (256 bytes each) for the skills that did not load, then a count of the rest. `ask settings show` lists the context files with their size and skip reason, and every skill that did not load, and its settings rows now include `defaultTools` and `skillsMaxContextTokens`.
- **CLI:** `ask -p` uses the saved `defaultModel` when you give no `--provider` and no `--model`. The project value counts only in a trusted project. A default that is not usable gives one stderr warning, and the run uses `faux`.
- **Settings:** Ask reads the project settings file `<project>/.ask/settings.json` on top of `<ASK_HOME>/settings.json`. The project file overrides the user file: objects merge key by key, and other values replace. A trusted project can set `defaultModel` and `thinkingFold`.
- **Settings:** A project file cannot set `defaultProjectTrust` or a key of the deny list (`trust`, `httpProxy`, `env`, `shellPath`, `shellCommandPrefix`, `npmCommand`, `providers`, `hooks`, `cacheWarming`, `deviceId`). Ask drops such a key with one diagnostic, and the rest of the file applies.
- **Settings:** Ask reads the project settings only after a trust decision for the folder. A folder with gated files (for example `.ask/settings.json` or `.agents/skills`) needs the decision, and a folder with none is trusted with no question. `AGENTS.md` and `CLAUDE.md` are not gated and always load.
- **Settings:** `<ASK_HOME>/trust.json` keeps the trust decision of each folder, and the nearest saved parent folder applies. The leader writes it. A corrupt or unsafe `trust.json` makes every project untrusted with one warning, and Ask never overwrites it.
- **Settings:** The user key `defaultProjectTrust` (`"ask"`, `"always"`, `"never"`) sets the trust of a project with gated files and no saved decision.
- **TUI:** At a session start in a folder that needs a trust decision, the TUI asks `Trust this project folder?` with the options Not now, Trust this folder, Trust parent folder, and Do not trust. Not now saves nothing.
- **TUI:** `/trust` shows the saved trust decision of the folder and changes it for the next session.
- **TUI:** When `/model` cannot save the choice as the default, the status row gives the reason: a project setting overrides it, the settings file is not safe, or the write failed. The session model changes all the same.
- **CLI:** `ask`, `ask -p`, and `ask acp` accept `--approve` (`-a`) and `--no-approve` (`-na`) to trust or not trust the project for this run only. The two flags together exit 1.
- **CLI:** `ask -p` writes one stderr warning when it does not trust a project with gated files, and one `Warning: settings:` line for each settings problem.
- **CLI:** `ask settings show [--json]` prints the trust decision of the current folder, each settings key with its value and its layer (`default`, `user`, or `project`), and each settings problem. It does not read the settings of an untrusted project, and it starts no leader.
- **Protocol:** The methods `_ask/trust/state` and `_ask/trust/set` read and save the trust of a folder, and `session/new` takes the override `_meta["ask.dev/trust"]`. `_ask/session/state` has the optional fields `trust`, `diagnostics`, and `thinkingFold`, and `set_model` has `defaultNotSaved`.
- **CLI:** `ask --no-leader` opens the TUI with an agent in this process. It does not connect to a leader and does not start one, so a debugger on the process sees the tool calls.
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
- **TUI:** The key `thinkingFold` in `<ASK_HOME>/settings.json` sets the fold mode of an ended thinking step: `"expanded"` (the default), `"truncated"` or `"collapsed"`. An unknown value gives `"expanded"`. The TUI reads the key when it starts and does not write it. A trusted project `.ask/settings.json` can override it.
- **TUI:** The first row of a reply shows the 24h time of the reply at the right, as the prompt band does. A reply that starts with a table or a code block shows no time.

### Changed

- **TUI:** `/model` does not save the default model when the trusted project settings set another `defaultModel`, or when the user settings file is not safe or has a known key with a bad value. The file keeps its bytes, and the model switch still applies.
- **Settings:** A `defaultModel` that is not a valid model id is now a settings problem with the path of the file. The session starts as with no default model.
- **Protocol:** An object with `"role": "custom"` now decodes to `protocol.CustomMessage`, not to `protocol.RawMessage`. Such an object without `customType` is a decode error.
- **Agent:** Task notices do not count toward `MaxQueuedInputs`. `Abort` keeps background tasks running and moves their queued notices to the context queue. A notice that arrives after `Abort` of an active run waits there until the next human input. `Reset` stops the tasks of the old conversation; `Dispose` stops and waits for every task.
- **CLI:** `ask -p` no longer stops background jobs after the last reply. It prints that reply, waits for the jobs, prints the reply of each run that a notice starts, and stops the jobs that still run when it exits.
- **Tools:** A background job belongs to the conversation that started it. After a conversation reset the old jobs run on until the session closes, but their notices are dropped, and `job_list`, `job_output`, and `job_kill` do not see them. The limit of 10 jobs still counts them.
- **TUI:** The status data (working directory, model, thinking level, context use) moved from the header row at the top to a footer row above the shortcut bar. The header row stays and shows only the logo `◢◤ ask`, with a rule under it. The footer row names the model as `<provider>/<model>`, and the working directory stays from 60 columns up.
- **TUI:** The footer row no longer shows the link (the leader id, `in-process`, an observer, `starting` or `lost`) or the session cost. A lost link still shows in the status row and in the notices. The reply meta line keeps its cost.
- **TUI:** The block pane no longer shows the model line (`model  <provider>/<model>` at the start and the `· was` line after a change). The footer row names the model. The fallback note and the `TEA_TRACE` warning are notices of their own at the start of the transcript.
- **TUI:** An ended thinking step now shows all its lines by default, below its header row and one blank row, at column 4 and without a rail. Its text is no longer italic, except without color. A step that is open or selected shows `›` in place of its own mark.
- **TUI:** Exactly one blank row separates any two blocks of the block pane. A selected block gets a thin box (`┌ ┐`, `│`, `└ ┘`) in place of the accent `┃` gutter and the shade of a selected step. A selection does not move any row.
- **TUI:** The reply meta line (model, time, tokens, cost) now has one blank row above it, so it stays apart from the reply text.
- **TUI:** Replies show markdown without raw syntax: headings without `#` in a color for each level, inline code in blue bold without backticks, `•` bullets, tables in a box, and code blocks on the code panel without a language row.
- **TUI:** `ctrl+t` now also sets the fold mode of the thinking steps that end later in the same TUI process.
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

- **Providers:** Anthropic models now show their thinking. Ask asks for a summary of the thinking (`display: "summarized"`). From Claude Opus 4.7 on, the thinking text was empty without it.
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
