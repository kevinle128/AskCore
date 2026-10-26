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

### Changed

- **Protocol:** An object with `"role": "custom"` now decodes to `protocol.CustomMessage`, not to `protocol.RawMessage`. Such an object without `customType` is a decode error.
- **Agent:** Task notices do not count toward `MaxQueuedInputs`. `Abort` keeps background tasks running and moves their queued notices to the context queue. `Reset` stops the tasks of the old conversation; `Dispose` stops and waits for every task.

### Fixed

- **Settings:** Two `ask` processes that use a new `ASK_HOME` at the same time no longer fail with `file exists`.

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
