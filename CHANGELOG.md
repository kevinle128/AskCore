# Changelog

All notable changes to Ask are recorded in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Ask has no release yet, so every entry is under `Unreleased`.
Details of each change are in `docs/`, in the plans under `plans/`, and in the git history.

## [Unreleased]

### Added

#### Project structure

- One Go package for each capability (the dewee package model) with the Ask import rules, enforced by depguard lint rules. Every package has a `README.md` that states what belongs there and which imports it may use ([architecture reference](docs/ask-architecture-reference.md)).
- Composition root with `fx`; HTTP server (echo) and gRPC server; zap logging; viper configuration.
- SQLite storage through GORM with embedded SQL migrations. `go run ./cmd/server` applies pending migrations at start; `-migrate` applies them and exits.
- Reference example for a new entity: users and posts with REST endpoints, and a gRPC greeter service.

#### Messages and events

- Shared wire contract in `pkg/protocol`: system, user, assistant, and tool result messages; text, thinking, image, and tool call blocks; usage, stop reasons, and diagnostics.
- Thinking levels `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`.
- Agent events with a sequencing envelope (sequence number, time, session ID, run ID) and an exact JSON and JSONL codec. The public JSON projection keeps Pi's event names.
- Lifecycle events for cycles, turns, and attempts (`cycle_start`, `turn_start`, `attempt_start`, and their end events), queue updates, retry events, `agent_settled`, and `agent_disposed`.

#### Agent loop

- Two-level agent loop as in Pi: a cycle for each admitted input batch and a turn for each model answer with its tool calls.
- Streaming of assistant messages from the provider.
- Tool calls run in parallel; a tool can declare that a call must run alone. Results keep the order of the calls. A tool body that started always finishes before the run ends; abort records an outcome for every call.
- Typed control points (`pipeline`) for admission, request preparation, model execution and recovery, before and after a tool, and the end of a turn. A handler can change a decision but does not own the loop.
- Input queues: `Prompt`, `Continue`, `Steer` (taken at the next turn), `FollowUp` (taken when the run would stop), and `Remove`. `Abort` stops the current work; `Reset` starts a new conversation; `Dispose` ends the Agent.
- Model retry with a captured serving policy; a retry keeps the selected credential and billing class. Failed attempts are recorded without putting their content into the model history.
- Repair of an uncertain open turn after a crash, without running a tool again.
- In-memory session log with typed entries and an exact rebuild of each model request without credentials.
- Local event listeners and a bounded replay ring with followers, cursors, and explicit resync for slow observers (`Agent.Follow`).

#### Tools

- Tool registry with JSON Schema declarations, argument coercion and validation, and one immutable snapshot of the tools for each turn.
- Built-in `echo` tool.

#### Providers and models

- Wire APIs: Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses.
- Providers: `anthropic`, `openai`, `xai`, and `alibaba-token-plan`, with a built-in model catalog, cost data, and request profiles.
- `faux` provider: a scripted provider for tests and demos. In `ask -p`, `echo <text>` calls the echo tool, `fail <text>` ends with an assistant error, and other text is said back; `ASK_FAUX_TPS` sets the speed in tokens per second.

#### Authentication

- One saved credential for each provider in `~/.ask/auth.json` (override the home with `ASK_HOME`), with mode 0600 and a file lock.
- `ask auth login --provider <name> --method <method>`: API key (read from private input), Anthropic subscription (`anthropic-oauth`), ChatGPT subscription (`openai-chatgpt`), and xAI (`xai-oauth`, device authorization). `ask auth logout` removes the saved credential.
- Automatic refresh of a subscription token before a request, and a check of account access for the selected model.

#### Command line

- `ask -p "<prompt>"` (print mode): runs one prompt in process and prints the answer. Flags include `--provider`, `--model`, `--thinking`, `--api-key`, and `--mode json`, which writes every agent event as one JSON line.
- Exit codes: 0 when done, 1 on an error, 130 on SIGINT, 143 on SIGTERM, 129 on SIGHUP.
- `ask acp`: serves the Agent Client Protocol (ACP) v1 over standard input and output for editors. Each session has its own Agent. Ask methods under `_ask/session/*` add continue, state, reset, steer, follow-up, remove, follow, unfollow, resync, usage, model and thinking selection, and the model list. `compact`, `fork`, and `tree` return "unsupported".

#### Testing

- LLM cassettes: tests replay recorded provider traffic. `ASK_RECORD=1` records the cassette of one test again; `ASK_CAPTURE=<dir> ask -p ...` records a real run ([guide](docs/testing-llm-cassettes.md)).
- Terminal end-to-end checks in `e2e/tui` with `tui-test`, and goroutine leak checks with goleak.

#### Async task runtime (first phase)

See the [design](docs/async-task-runtime.md) and [ADR 0004](docs/adr/0004-agent-owned-async-task-runtime.md).

- `internal/tasks`: a per-Agent task registry. Each task runs on a goroutine that the registry owns, ends in exactly one terminal state, and produces exactly one notice. Timer tasks hold no goroutine while they wait.
- `Agent.Notify`: the only entry for task notices. A notice joins the next turn of a running Agent or starts a run on an idle Agent. No component sleeps or polls to detect completion.
- `Agent.Idle` and `Agent.WaitForQuiescence`: wait for the end of runs and tasks by signal.
- `Agent.Tasks` and `tools.Context.Tasks`: tools start background work through the registry.
- `protocol.CustomMessage` (role `custom`): an application message that the model receives as a user message. Task notices use it with the custom type `task-notification`.
- Events `task_start`, `task_update`, and `task_end`.
- Task output files under `<ASK_HOME>/tasks/<session>/`, created at the first output with mode 0600 in private directories. Session directories older than 7 days are removed once when a process starts.
- `settings.EnsurePrivateDir`, `settings.CheckPrivateDir`, and `settings.ResolveHome`.

### Changed

- An object with `"role": "custom"` now decodes to `protocol.CustomMessage`, not to `protocol.RawMessage`. Such an object without `customType` is a decode error.
- `providers.ConvertToLLM` keeps a custom message and gives it to the model as a user message. Raw messages are still dropped.
- `queue_update` shows a task notice as a preview of at most 200 bytes.
- Task notices do not count toward `MaxQueuedInputs`, so they never take the queue slots of human input.
- `Abort` keeps background tasks running and moves queued task notices to the context queue, where they ride with the next input. Human input is dropped as before.
- `Reset` stops the tasks of the old conversation and refuses their late notices and events. `Dispose` stops and waits for every task.

### Not yet available

- The interactive `ask` terminal UI is a placeholder; the TUI packages are scaffolds ([TUI architecture](docs/tui-architecture.md)).
- The leader over a Unix socket, the read-only monitoring dashboard, persistent sessions, and the remote model catalog overlay ([ADR 0003](docs/adr/0003-remote-model-catalog-overlay.md)) are designed but not implemented.
