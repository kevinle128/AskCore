# CLI reference

This page lists the commands of the `ask` binary and of the `cmd/server` daemon. The code is the authority. Run `ask --help` or `ask <command> --help` for the text of the current build. The built-in tools and the providers are in [reference/](reference/) (generated).

## Build the binary

Build the binary with the name `ask`:

```sh
go build -o ask ./cmd/tui
```

The TUI, `ask connect`, `ask leader`, and `/providers` need that name. A client starts the leader from a binary named `ask` that is next to the caller or on the `PATH` (`internal/leader/spawn.go`). `go run ./cmd/tui` makes a binary named `tui`. With it the TUI falls back to the in-process agent. `ask -p`, `ask auth`, `ask version`, and `ask acp` need no leader and work with `go run ./cmd/tui`.

## Dispatch

`ask` checks the first argument against `auth`, `acp`, `connect`, `leader`, and `version`, in this order (`cmd/tui/headless.go`). Any other input goes to the root command.

All commands use these exit codes for signals: 130 for SIGINT, 143 for SIGTERM, and 129 for SIGHUP. A second signal ends `ask acp` and `ask leader` before the cleanup finishes.

## `ask` (root)

```
ask [options] [--] [@files...] [messages...]
```

The mode is chosen in this order:

1. `--mode json` selects JSON mode.
2. `-p` or `--print`, or a stdin or stdout that is not a terminal, selects print mode.
3. Otherwise the interactive TUI opens.

| Flag | Purpose |
|---|---|
| `-p`, `--print` | Run the prompt, print the reply, and exit. A token right after `-p` that does not start with `-` and is not an `@file` is the first message. |
| `--mode text\|json` | Output format. `json` writes one JSON line for each agent event. Another value is an error. |
| `--provider <name>` | `faux` (default), `alibaba-token-plan`, `anthropic`, `openai`, or `xai`. |
| `--model <id>` | Model id. Defaults: `faux-1` (faux), `deepseek-v4.1-flash` (alibaba-token-plan), `claude-sonnet-4-6` (anthropic), `gpt-5.5` (openai), `grok-4.7` (xai). The model list is in [reference/providers.md](reference/providers.md). |
| `--api-key <key>` | Key for this run. It needs `--model`, except for `alibaba-token-plan`. A key in the arguments is visible to other processes. Prefer the environment or `ask auth login`. |
| `--thinking <level>` | `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`. `alibaba-token-plan` defaults to `medium`. |
| `--max-turns <n>` | Print and JSON mode. End the run after `n` turns. `n` is a whole number above 0, or the command exits 1. See [Background work in print mode](#background-work-in-print-mode). |
| `--` | End of options. |
| `-h`, `--help` | Show the usage text. |

Input rules:

- An argument that starts with `@` is a file. Its text goes before the first message, wrapped in `<file name="...">`. `~` is expanded. An empty file is skipped. A missing file is an error.
- Piped stdin is read, trimmed, and put before the files and the first message.
- More messages run as follow-up prompts, in order.
- An unknown flag is an error.

Exit codes: 0 done; 1 for an argument or setup error, and in print mode for an assistant error; `--mode json` exits 0 on an assistant error. In print mode the code comes from the last reply that the run printed. SIGINT, SIGTERM, and SIGHUP end the run, also during the wait for background work, with 130, 143, and 129.

The demo provider `faux` is the default of `ask -p`. It runs a script: `echo <text>` calls the echo tool, `fail <text>` ends in an assistant error, and any other text is said back.

### Background work in print mode

A `bash` job or a `monitor` can still run when the last prompt ends. `ask -p` and `--mode json` then wait for it instead of stopping it:

- Print mode prints the reply of the last prompt at once. Then it waits by signal. Each completion notice or monitor event starts one more run, and print mode prints the reply of each of these runs. `--mode json` writes the events of these runs to the stream.
- The wait ends when no run, task, or job is left. A job that still runs at the exit (signal, idle cap, or `--max-turns`) is stopped.
- A background job stops after a time limit. The default is 10 minutes. A `bash` call with `run_in_background` can set the limit with `timeout`, up to 2 hours. Every `monitor` stops after at most 10 minutes, also a `persistent` one. The model gets a notice when a limit stops a job. The foreground wait of `bash` keeps its own budget (default 120 s, maximum 600 s).
- The idle cap ends the wait when the agent is idle for 10 minutes and no `bash` job runs. Then only monitors are left.
- After 5 seconds, a terminal on stderr shows `Waiting for background work to finish: <job> (<label>), ...`. In `--mode json` it shows only when stdout is not a terminal.
- `--max-turns <n>` counts every turn of the run, also the turns that a notice starts. A turn that the agent retries does not count. When the limit is reached, the run stops before turn `n+1` and exits with code 1. The line `Error: Reached max turns (n)` goes to stdout, or to stderr with `--mode json`, so the JSON event stream stays valid. The interactive TUI ignores the flag.

The TUI, the leader, and `ask acp` set no time limit on background jobs. The variables `ASK_PRINT_BG_LIMIT_MS`, `ASK_PRINT_BG_WAIT_CEILING_MS`, and `ASK_DISABLE_BACKGROUND_TASKS` are in [Environment variables](#environment-variables). The owner of the rules is [internal/jobs/README.md](../internal/jobs/README.md#background-limit-and-quiescence). The tool texts are in [reference/tools.md](reference/tools.md).

## `ask auth`

```
ask auth login --provider <id> --method <id> [--interaction browser|copy-code] [--new-account]
ask auth logout --provider <id> [--method <id>]
ask auth --help
```

- Providers: `anthropic`, `openai`, `xai`, `alibaba-token-plan`.
- Methods: `api-key`, `anthropic-oauth`, `openai-chatgpt`, `xai-oauth`.
- `--provider` is required.
- `--new-account` works only with `openai` and `openai-chatgpt`.
- `--interaction` works only with `anthropic-oauth`.
- `logout` takes no login options. No command takes a positional argument.
- Without `--method`, a login on a terminal asks for the method. A login without a terminal fails with "noninteractive login requires --method".
- Secrets come from private stdin only (limit 16 KiB).
- The command needs no database and no leader. It writes messages to stderr.
- Exit codes: 0 done, 1 error, 130 private input interrupted, 143, 129.

The sign-in flows are in [README.md](../README.md#native-auth-and-headless-prompts).

## `ask acp`

```
ask acp
ask acp --help
```

The command serves ACP v1 as JSON-RPC lines on stdin and stdout. Diagnostics go to stderr. It opens no listener and no database. Each session has its own agent. No credentials pass over the connection: sign in first with `ask auth login`. Any other argument is an error.

Exit codes: 0 input ended, 1 error or lost output, 130, 143, 129. The owner is [internal/acp/README.md](../internal/acp/README.md).

## `ask leader`

```
ask leader                      run the leader in the foreground
ask leader status [--json]      show the identity and counts of the running leader
ask leader list [--json]        list the leaders of this home (one)
ask leader stop                 stop the running leader
ask leader --help
```

- The leader listens on `<ASK_HOME>/leader.sock`. The socket has mode 0600 in a directory of mode 0700. Only the owner can connect.
- `status`, `list`, and `stop` never start a leader. `--json` works only with `status` and `list`.
- `stop` waits up to 15 s for each step. On Linux it falls back to SIGTERM through a verified process handle.
- A leader that a client started has the internal flag `--spawned-by-client` and writes `<ASK_HOME>/leader.log`. The log rotates at 8 MiB. Do not use this flag by hand.
- A client of a newer build replaces an older leader that a client started. The sessions and background jobs of the old leader end.
- Exit codes: `ask leader status` exits 1 with "no leader is running" when none runs. `list` and `stop` exit 0 in that case. A real error exits 1. Signals exit with 130, 143, or 129. The usage text of the command says "0 stopped or nothing to do" without this exception for `status`.

The owner is [internal/leader/README.md](../internal/leader/README.md).

## `ask connect`

```
ask connect [--cwd DIR | --attach SESSION]
```

`--cwd` and `--attach` exclude each other. The command starts the leader when none runs. It never falls back to its own agent. It exits 1 with the reason when the leader is unreachable.

It reads one line of stdin at a time:

| Input | Effect |
|---|---|
| `text` | Send the text as a prompt to the current session. |
| `//text` | Send a prompt that starts with `/`. |
| `/new [DIR]` | Create a session and make it current. |
| `/sessions` | List the live sessions of the leader. |
| `/attach ID` | Watch a session. |
| `/detach` | Leave the current session. Its run goes on. |
| `/take` | Become the driver of the current session. |
| `/cancel` | Cancel the run of the current session (driver only). |
| `/follow [EPOCH SEQ]` | Follow the events of the current session from a cursor. |
| `/unfollow` | Stop following. |
| `/answer ID OPTION` | Answer an open question. |
| `/quit` | Leave at once. |

Output is one line for each event. The first word is one of `connected`, `session`, `live`, `update`, `result`, `error`, `follow`, `event`, `resync`, `question`, `detached`, `bye`. At the end of stdin the client waits for the prompts in flight and leaves. Leaving only detaches: the leader and its runs go on.

Exit codes: 0 left, 1 error or lost leader, 130, 143, 129.

## `ask version`

`ask version` prints `ask <build>`. `ask version --json` prints one JSON line with `name`, `build`, `leaderProtocol`, `acpVersion`, and `commitTime` (a binary with a VCS stamp) or `devStamp` (a binary without one, for example from `go run`). The build is the first 12 characters of the VCS revision, with `+dirty` when the tree was modified. Without a revision it is the module version, or `dev`. An unknown argument exits 1.

## Interactive TUI

`ask` with no arguments on a terminal opens the TUI on the alternate screen. It connects to the local leader and starts one when none runs. Only when the first connect fails does it run an agent in process; the header row then shows `in-process`. The TUI never switches to an in-process agent later. On exit the terminal is restored and the line `ask: session <id>` stays in the shell.

| Command | Purpose |
|---|---|
| `/model` | Select a model. The list has only the models of usable providers. The choice is saved as `defaultModel` in `settings.json`. |
| `/providers` | Add or remove a provider sign-in. It needs the leader. |
| `/new` | Start a new session in this directory. |
| `/sessions` | Watch another live session. It needs the leader. |
| `/take` | Take control of this session. |
| `/copy` | Copy the last reply. It uses a native command first, then OSC 52 up to 75 KiB. |
| `/quit` | Detach and quit. |

With no usable provider the session has no model. The header shows `no model`, and a prompt is refused and stays in the editor. `/providers` opens the sign-in box. The leader checks an API key before it saves it (mode 0600 `auth.json`); it cannot check the Alibaba Token Plan key, so it saves that key as it is. The key is pasted into a masked input.

The key bindings, block folding, the viewer, and the mouse rules are in [internal/tui/README.md](../internal/tui/README.md). Terminal.app, iTerm2, and kitty were not checked by hand.

## Environment variables

| Variable | Used by | Effect |
|---|---|---|
| `ASK_HOME` | all `ask` commands | Home directory (default `~/.ask`). It holds `auth.json`, `settings.json`, `leader.sock`, and `leader.log`. It must be an absolute path. For the leader it must be short enough for a Unix socket path. |
| `ASK_FAUX` | leader, `ask acp`, in-process TUI | `1` lists the demo provider `faux`. `ask -p` has it by default and ignores the variable. |
| `ASK_FAUX_TPS` | `faux` provider | Pace in tokens per second. A negative value or a non-number is an error. |
| `ASK_COMMAND_GUARD` | `ask -p`, leader, `ask acp` | `1` blocks a short list of destructive commands. It is off by default and is not a security boundary. |
| `ASK_DISABLE_BACKGROUND_TASKS` | `ask -p`, leader, `ask acp`, in-process TUI | `1` turns background jobs off. A foreground `bash` command stops at its wait budget. A background start and `monitor` are refused. |
| `ASK_PRINT_BG_LIMIT_MS` | `ask -p` | The default background time limit, in milliseconds (default 600000). A value that is not a whole number above 0 is ignored. A larger value also raises the 2 hour maximum. |
| `ASK_PRINT_BG_WAIT_CEILING_MS` | `ask -p` | The idle cap of the background wait, in milliseconds (default 600000). `0` means no cap. An empty, negative, or invalid value gives the default. |
| `ASK_CAPTURE` | `ask -p` | A directory. The run records the provider HTTP traffic to a new cassette. Ignored for `faux`. |
| `TEA_TRACE` | interactive TUI | A file. It receives raw keyboard input, pasted keys included. The TUI warns on screen. Do not set it when you sign in. |
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `XAI_API_KEY`, `ASK_ALIBABA_TOKEN_PLAN_API_KEY` (or `ALIBABA_TOKEN_PLAN_API_KEY`) | providers | API key when no saved sign-in applies. |

Ask sets `ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, and `ASK_REASONING_LEVEL` in the environment of the processes that tools start. Do not set them.

Test variables (`ASK_RECORD`, `ASK_UPDATE_DOCS`, and others) are in [testing-llm-cassettes.md](testing-llm-cassettes.md) and [../CONTRIBUTING.md](../CONTRIBUTING.md).

## `cmd/server`

```
go run ./cmd/server [-migrate]
```

`-migrate` applies the pending migrations and exits. Without it the server applies the migrations, then starts HTTP and gRPC. It has no other flag.

The server reads `.env` if present, then the environment, and an optional `config.yaml` in the working directory:

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `8080` |
| `GRPC_PORT` | `50051` |
| `DATABASE_URL` | `app.db` (SQLite file) |
| `LOG_LEVEL` | empty (logger default) |
| `CORS_ORIGIN` | `*` |

Routes: `GET /`, `GET /health`, `/api/users`, `/api/users/:id`, `/api/posts`, `/api/posts/:id`. gRPC: `Greeter.SayHello` and `Greeter.SayHelloStream`.
