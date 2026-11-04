# CLI reference

This page lists the commands of the `ask` binary and of the `cmd/server` daemon. The code is the authority. Run `ask --help` or `ask <command> --help` for the text of the current build. The built-in tools and the providers are in [reference/](reference/) (generated).

## Build the binary

Build the binary with the name `ask`:

```sh
go build -o ask ./cmd/tui
```

The TUI, `ask connect`, `ask leader`, and `/providers` need that name. A client starts the leader from a binary named `ask` that is next to the caller or on the `PATH` (`internal/leader/spawn.go`). `go run ./cmd/tui` makes a binary named `tui`. With it the TUI falls back to the in-process agent. `ask -p`, `ask auth`, `ask models`, `ask settings`, `ask version`, and `ask acp` need no leader and work with `go run ./cmd/tui`.

## Dispatch

`ask` checks the first argument against `auth`, `models`, `settings`, `acp`, `connect`, `leader`, and `version`, in this order (`cmd/tui/headless.go`). Any other input goes to the root command.

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
| `--provider <name>` | `faux` (default), `alibaba-token-plan`, `anthropic`, `openai`, or `xai`. Without `--provider` and `--model`, print mode uses the saved `defaultModel`; see below. |
| `--model <id>` | Model id. Defaults: `faux-1` (faux), `deepseek-v4.1-flash` (alibaba-token-plan), `claude-sonnet-4-6` (anthropic), `gpt-5.5` (openai), `grok-4.7` (xai). The model list is in [reference/providers.md](reference/providers.md). |
| `--api-key <key>` | Key for this run. It needs `--model`, except for `alibaba-token-plan`. A key in the arguments is visible to other processes. Prefer the environment or `ask auth login`. |
| `--thinking <level>` | `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`. `alibaba-token-plan` defaults to `medium`. |
| `--max-turns <n>` | Print and JSON mode. End the run after `n` turns. `n` is a whole number above 0, or the command exits 1. See [Background work in print mode](#background-work-in-print-mode). |
| `--system-prompt <text\|file>` | Replace the default system prompt for this run. See [Prompt, context files, skills, and tools](#prompt-context-files-skills-and-tools). |
| `--append-system-prompt <text\|file>` | Add text at the end of the system prompt. Repeat the flag to add more. |
| `-t`, `--tools <names>` | Keep only these built-in tools. Give a comma list, for example `read,grep`. Repeat the flag to add more names. |
| `-nt`, `--no-tools` | Keep no built-in tool. Use it alone: with `-t` it is an error. |
| `--no-leader` | TUI only. Run the agent in this process. Do not connect to a leader and do not start one. Use it to debug the agent, because the tools then run in this process. Print mode already runs in process and ignores the flag. |
| `-a`, `--approve` | Trust the project of the current folder for this run. The TUI then shows no trust question. Nothing is saved. See [Project trust](#project-trust). |
| `-na`, `--no-approve` | Do not trust the project of the current folder for this run. Nothing is saved. The two flags together are an error (exit 1). |
| `--` | End of options. |
| `-h`, `--help` | Show the usage text. |

Input rules:

- An argument that starts with `@` is a file. Its text goes before the first message, wrapped in `<file name="...">`. `~` is expanded. An empty file is skipped. A missing file is an error.
- Piped stdin is read, trimmed, and put before the files and the first message.
- More messages run as follow-up prompts, in order.
- An unknown flag is an error.

Exit codes: 0 done; 1 for an argument or setup error, and in print mode for an assistant error; `--mode json` exits 0 on an assistant error. In print mode the code comes from the last reply that the run printed. SIGINT, SIGTERM, and SIGHUP end the run, also during the wait for background work, with 130, 143, and 129.

The demo provider `faux` is the default of `ask -p`. It runs a script: `echo <text>` calls the echo tool, `fail <text>` ends in an assistant error, and any other text is said back.

When you give no `--provider` and no `--model`, `ask -p` uses the `defaultModel` of the settings (the value that `/model` saves). The project layer counts only when the project is trusted. The rules are the same as for a new TUI session. When the model is not in the model list, or its provider has no credential, `ask -p` writes one line to stderr, for example `Warning: default model <id> is not usable (no credential); using faux/faux-1@faux.`, and runs on `faux`. When no `defaultModel` is set, `ask -p` runs on `faux`. `--provider` or `--model` always wins.

### Background work in print mode

A `bash` job or a `monitor` can still run when the last prompt ends. `ask -p` and `--mode json` then wait for it instead of stopping it:

- Print mode prints the reply of the last prompt at once. Then it waits by signal. Each completion notice or monitor event starts one more run, and print mode prints the reply of each of these runs. `--mode json` writes the events of these runs to the stream.
- The wait ends when no run, task, or job is left. A job that still runs at the exit (signal, idle cap, or `--max-turns`) is stopped.
- A background job stops after a time limit. The default is 10 minutes. A `bash` call with `run_in_background` can set the limit with `timeout`, up to 2 hours. Every `monitor` stops after at most 10 minutes, also a `persistent` one. The model gets a notice when a limit stops a job. The foreground wait of `bash` keeps its own budget (default 120 s, maximum 600 s).
- The idle cap ends the wait when the agent is idle for 10 minutes and no `bash` job runs. Then only monitors are left.
- After 5 seconds, a terminal on stderr shows `Waiting for background work to finish: <job> (<label>), ...`. In `--mode json` it shows only when stdout is not a terminal.
- `--max-turns <n>` counts every turn of the run, also the turns that a notice starts. A turn that the agent retries does not count. When the limit is reached, the run stops before turn `n+1` and exits with code 1. The line `Error: Reached max turns (n)` goes to stdout, or to stderr with `--mode json`, so the JSON event stream stays valid. The interactive TUI ignores the flag.

The TUI, the leader, and `ask acp` set no time limit on background jobs. The variables `ASK_PRINT_BG_LIMIT_MS`, `ASK_PRINT_BG_WAIT_CEILING_MS`, and `ASK_DISABLE_BACKGROUND_TASKS` are in [Environment variables](#environment-variables). The owner of the rules is [internal/jobs/README.md](../internal/jobs/README.md#background-limit-and-quiescence). The tool texts are in [reference/tools.md](reference/tools.md).

## Prompt, context files, skills, and tools

Each session gets its system prompt, its context files, its skill list, and its tools from the files below and from the flags above. `ask -p`, the TUI, and `ask acp` use the same rules.

### System prompt

The system prompt has these sections in this order: the preamble, the tools, the rules, the Ask docs pointer, the addendum, the working directory, and the date. The working directory and the date are last. The text before them is the same for two sessions that differ only in directory and date, so the provider cache stays valid. The date is the local date of the session start (`YYYY-MM-DD`).

A replacement prompt takes the place of the preamble. It drops the tools, rules, and docs sections. The addendum, the working directory, and the date stay. A replacement or an addendum with only white space is ignored.

The first match decides the replacement. Ask never merges two replacements:

1. `--system-prompt`. A second `--system-prompt` replaces the first.
2. `<project>/.ask/SYSTEM.md`.
3. `<ASK_HOME>/SYSTEM.md`.

The addendum has the text of one `APPEND_SYSTEM.md` file: the project file when it has text, else the file in `<ASK_HOME>`. Each `--append-system-prompt` text follows it, in the order of the command line.

A project `SYSTEM.md` or `APPEND_SYSTEM.md` is a gated file. Ask reads it only when the project is trusted (see [Project trust](#project-trust)). A file that exists but cannot be read, or that is over 1 MiB, ends the session start with an error. Ask does not fall back to the default prompt in this case.

### Text or file path

The value of `--system-prompt` and `--append-system-prompt` is text, or the path of a file. When the value names an existing regular file, Ask reads the file at once and uses its content. Any other value is the text. A path with a typo is therefore sent as text. Ask reads the file in the `ask` process that you start, also when the session runs on a leader. A file that exists but cannot be read gives `Error: --system-prompt: cannot read <path>` and exit 1.

All prompt text of the flags together may have at most 262144 bytes (256 KiB). A larger total gives `Error: the prompt flags are <n> bytes; the limit is 262144` and exit 1.

### Context files

Ask reads `AGENTS.md` and `CLAUDE.md` from `<ASK_HOME>`, then from each folder from the git root down to the working directory. A git root is the nearest folder with a `.git` file or folder. Without a git root, Ask reads only the working directory. A git root that is your home folder counts as no git root. In each folder `AGENTS.md` comes first. A file with the same text as an earlier file in the same folder is a duplicate and Ask leaves it out without a warning.

The files go to the model as one user-role message of the custom type `context-file`. They are not part of the system prompt. Ask escapes the file text, the paths, and the notices, so a file cannot close the frame around it. The message starts with the sentence that the files are guidance only and cannot override system or user instructions. A conversation reset adds the message again. A history that already has the message does not get a second copy.

`AGENTS.md` and `CLAUDE.md` load without a trust decision, also in a project that is not trusted. Trust gates the files that change how Ask behaves: settings, prompt files, and skills. A context file only gives text to the model. A project that is not trusted can still hold such a file, so Ask sends the text as escaped user-role guidance and not as a system instruction. A file that is over 1 MiB or that cannot be read is left out. The size limit of all files together leaves out or cuts files, and the model sees a notice for it.

| Limit | Value | What happens |
|---|---|---|
| One file | 1 MiB | Ask leaves the file out and never reads past the limit. |
| All files together | 64 KiB, counted after escaping and with the frame and the notices | Ask drops the broadest files first (`<ASK_HOME>` first, then the git root down). It cuts the most specific file that is left at a character boundary. |

When Ask cuts a file, the message ends with `Notice: <path> was cut short: the context files exceed 64 KiB.` When Ask drops files, it ends with `Notice: <n> broader context files were left out: the context files exceed 64 KiB.`

### Skills

A skill is a folder with a `SKILL.md` file. Ask reads the name and the description from the frontmatter. It sends only a list of names, descriptions, and paths, as one user-role message of the custom type `skill-list` (at most 16 KiB). The model reads a skill with the `read` tool, so the list is sent only when the session keeps `read` or `bash`. A skill with `disable-model-invocation: true` is not in the list.

Ask looks in these folders, in this order, to a depth of 5:

1. `<ASK_HOME>/skills`.
2. `~/.agents/skills`.
3. In a trusted project: `<project>/.ask/skills` and `<project>/.agents/skills`, then the `.agents/skills` of each parent folder, nearest first.

The first skill of a name wins. A later skill of the same name is not loaded, and Ask reports it as a problem. A project skill folder is a gated entry, so an untrusted project never gives skills.

### Tools

Ask keeps all built-in tools unless you select. Select with these, from the strongest to the weakest:

1. `-nt` or `--no-tools` keeps no built-in tool.
2. `-t` or `--tools <names>` keeps only the named tools. The names are lower-case letters, digits, and `_`.
3. The `defaultTools` setting (see [Settings files](#settings-files)).

A flag replaces the setting. A `-name` entry of the `defaultTools` setting still removes its tool when you use `-t`. The prompt tool list and the rules follow the kept tools, and Ask does not register the tools that it does not keep. A name that is not a built-in tool ends the session start with `--tools: unknown tool "<name>"; known tools: <list>` (a bad `defaultTools` entry names `defaultTools` in place of `--tools`). Over ACP the same text answers `invalid_params`. The tool list of this build is in [reference/tools.md](reference/tools.md).

### Usage errors

These errors write one line to stderr and exit with code 1 before any session starts:

| Cause | Line |
|---|---|
| A flag with no value | `Error: --system-prompt requires a value` (also for `--append-system-prompt`, `-t`, `--tools`) |
| `-t` or `--tools` together with `-nt` or `--no-tools` | `Error: --tools and --no-tools cannot be used together` |
| A tool name that is not valid | `Error: --tools: invalid tool name "<name>"` |
| A prompt file that cannot be read | `Error: --system-prompt: cannot read <path>` |
| Prompt text over 256 KiB | `Error: the prompt flags are <n> bytes; the limit is 262144` |
| A tool name that is not a built-in tool, an unreadable `SYSTEM.md`, or an unreadable `APPEND_SYSTEM.md` | `Error: ...` (at the session start) |

### Warnings

`ask -p` writes these lines to stderr after the trust and settings lines, and before the run. The leader and `ask acp` send the same problems to the client as session diagnostics.

- `Warning: <n> context file(s) was/were skipped; run ask settings show for details`. It counts the files that Ask left out or cut (too large, not readable, or over the 64 KiB limit). A duplicate is not counted.
- `Warning: <n> skill(s) was/were not loaded; run ask settings show for details`. It counts the skills that did not load, a skill that lost to an earlier name included.

Each line is a summary. The path and the reason of each problem are in `ask settings show`. A line never contains file content.

### Flags in the TUI and with a leader

The TUI sends the four flags to its host with `session/new`, in `_meta["ask.dev/session"]` (`systemPrompt`, `appendSystemPrompt`, `tools`, `noTools`). The prompt flags hold text, not paths. The flags apply to every session that this TUI process opens, also after `/new`. They are not saved.

A leader that is older than this build ignores the key without an error. The TUI then shows `Warning: the leader ignored the prompt and tool flags of this command line (it is older than this build). They are not applied in this session.` The session runs with more tools and with the default prompt. Stop the old leader with `ask leader stop`, or use `--no-leader`.

`ask acp` does not take these flags. An ACP client that starts a session can send `_meta["ask.dev/session"]` with the object `{"systemPrompt": "...", "appendSystemPrompt": ["..."], "tools": ["read"], "noTools": false}`. The host applies it to that session only. A value that is not an object, a field of the wrong type, prompt text over 256 KiB in total, a tool name that is not valid, or both `tools` and `noTools` answers `invalid_params`. When the host applied non-empty flags, the result `_meta` has `ask.dev/session: true`.

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

## `ask models`

```
ask models refresh [--provider <id>]
ask models --help
```

The command reads the model lists of the signed-in accounts and the models.dev metadata now. It writes them to `<ASK_HOME>/model-cache.json`. It does not wait for the 2 hour cache age.

- Providers: `alibaba-token-plan`, `anthropic`, `openai`, `xai`. Without `--provider`, the command refreshes all providers. The metadata is always refreshed.
- The command needs no database and no leader. A running leader reads the new cache file at its next model list.
- `ASK_DISABLE_MODEL_REFRESH` does not stop this command.
- The command writes one line for each provider and one metadata line to stdout. It does not write keys, tokens, or the message text of a provider.

```
openai    12 models  known
metadata  updated
```

| Provider state | Meaning |
|---|---|
| `known` | The command read the model list of the account. |
| `unknown` | The command could not read the list, or the sign-in method has no list (Alibaba Token Plan, Anthropic subscription). The last good list stays. Without a list, all catalog models of the provider stay. |
| `no credential` | The provider has no sign-in and no environment key. The command sent no list request. |

The count is the number of models of the provider in the model list after the refresh. `/model` shows these models when the provider has a credential.

The metadata line is `updated`, `not modified` (the data did not change), or `failed` (the last good data stays).

Exit codes: 0 done, also when a provider is `unknown` or the metadata `failed`; 1 for a usage error or a local error, for example a cache file that the command cannot write; 130, 143, 129.

## `ask acp`

```
ask acp [--approve | --no-approve]
ask acp --help
```

The command serves ACP v1 as JSON-RPC lines on stdin and stdout. Diagnostics go to stderr. It opens no listener and no database. Each session has its own agent. No credentials pass over the connection: sign in first with `ask auth login`.

`--approve` (`-a`) trusts the project of every session of this process, and `--no-approve` (`-na`) trusts none. Without a flag, each session decides with no UI (see [Project trust](#project-trust)). The two flags together, or any other argument, are an error. The command takes none of the prompt and tool flags (`--system-prompt`, `--append-system-prompt`, `-t`, `-nt`). A client sends them for one session in `_meta["ask.dev/session"]` (see [Flags in the TUI and with a leader](#flags-in-the-tui-and-with-a-leader)). The command answers `unsupported` for `_ask/trust/state` and `_ask/trust/set`, because an editor link has no trust question.

Exit codes: 0 input ended, 1 error or lost output, 130, 143, 129. The owner is [internal/acp/README.md](../internal/acp/README.md).

## `ask settings`

```
ask settings show [--json] [--approve | --no-approve]
ask settings --help
```

The command prints the settings of the current folder. It reads files only. It writes nothing, and it starts no leader and no database. It decides the project trust as `ask -p` does (with no UI), and the trust flags change that decision for this run only. It does not read the project settings file of a project that is not trusted. It prints only the shipped keys, never other keys, file content, or credentials.

The text form shows, in this order:

1. The Ask home, the user settings file, the project root, the trust decision with its source (and the saved entry that applies), the gated files, and the state of the project settings: `loaded`, `not read (untrusted)`, or `none` (the `.ask` folder of the cwd is the Ask home).
2. `Settings:` one row for each key, with the value and the layer that sets it (`default`, `user`, or `project`). `(none)` means that no `defaultModel` is set.
3. A `Warning:` line for a trust store that cannot be read.
4. `Problems:` one row for each settings diagnostic: the layer, the path, the key, and a fixed text.
5. `Context files:` one row for each `AGENTS.md` or `CLAUDE.md` that the session reads, with its path and size in bytes, then the files that Ask left out. A row has the word `too_large`, `unreadable`, `duplicate`, `budget`, or `truncated` when Ask did not deliver the file whole. `(none)` means that no context file exists.
6. `Skill problems:` one row for each skill that did not load: the layer, the path, and a fixed text. The block is absent when every skill loaded.

`--json` prints one JSON object with this shape. Arrays are always present, also when they are empty.

```json
{
  "home": "/home/u/.ask",
  "userFile": "/home/u/.ask/settings.json",
  "project": {
    "root": "/work/repo",
    "layer": "loaded",
    "trusted": true,
    "source": "saved",
    "entry": "/work/repo",
    "gated": [".ask/settings.json"],
    "warning": ""
  },
  "settings": [
    {"key": "defaultModel", "value": "openai/gpt-5.5@responses", "source": "project"},
    {"key": "thinkingFold", "value": "collapsed", "source": "user"},
    {"key": "defaultProjectTrust", "value": "ask", "source": "default"},
    {"key": "defaultTools", "value": "read,grep", "source": "user"}
  ],
  "diagnostics": [
    {"layer": "project", "path": "/work/repo/.ask/settings.json", "key": "env", "text": "a project file cannot set this key; it is ignored"}
  ],
  "context": [
    {"path": "/work/repo/AGENTS.md", "bytes": 1204, "skipped": ""}
  ],
  "skillDiagnostics": [
    {"layer": "project", "path": "/work/repo/.agents/skills/old/SKILL.md", "key": "skills", "text": "a fixed reason"}
  ]
}
```

| Field | Values |
|---|---|
| `project.layer` | `loaded`, `untrusted` (the file is not read), or `none`. |
| `project.source` | `flag`, `no_gated_files`, `store_error`, `saved`, `saved_ancestor`, `default_always`, `default_never`, or `no_ui`. |
| `project.entry` | The folder of the saved trust entry that applies, or empty. |
| `project.gated` | The gated entries that exist: relative to the root in the root, absolute in a parent folder. |
| `project.warning` | A fixed text with the path of a trust store that cannot be read, or empty. |
| `settings[].value` | The effective value. An empty `defaultModel` means that no default model is set. `defaultTools` is the array joined with commas; an empty value is shown for an unset key and for an empty array, so read `source` to tell them apart (`default` means unset). |
| `settings[].source` | `default`, `user`, or `project`. |
| `diagnostics[].key` | The key, or empty for a problem with the whole file. |
| `context[].bytes` | The size of the file in bytes. `0` when Ask cannot read it. |
| `context[].skipped` | Empty for a file that the session gets whole, else `too_large`, `unreadable`, `duplicate`, `budget`, or `truncated`. |
| `skillDiagnostics[]` | The skills that did not load. `key` is `skills`. |

A bad value of a known key gives the default and a diagnostic. A corrupt user file or trust store is reported and keeps its bytes.

Exit codes: 0 done, also with diagnostics or a trust store warning; 1 for a usage error, a cwd that cannot be read, or an output that cannot be written; 130, 143, 129.

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

`ask` with no arguments on a terminal opens the TUI on the alternate screen. It connects to the local leader and starts one when none runs. Only when the first connect fails does it run an agent in process. With `--no-leader` it always runs the agent in process and starts no leader. The screen does not show the difference: `/providers` and `/sessions` answer `Needs the leader; this session runs in process.`, and `ask leader status` reports `no leader is running`. The TUI never switches to an in-process agent later. On exit the terminal is restored and the line `ask: session <id>` stays in the shell.

| Command | Purpose |
|---|---|
| `/model` | Select a model. The list has only the models of usable providers. The choice is saved as `defaultModel` in the user `settings.json`. The save is refused, and the file does not change, when the trusted project settings set another `defaultModel`, or when the user file is not safe or has a known key with a bad value. The model switch still applies. |
| `/providers` | Add or remove a provider sign-in. It needs the leader. |
| `/new` | Start a new session in this directory. |
| `/sessions` | Watch another live session. It needs the leader. |
| `/trust` | Show the saved trust decision of this folder and change it: trust this folder, trust the parent folder, do not trust, or forget the saved choice. The change applies to the next session. An older leader answers `/trust is not available`. |
| `/take` | Take control of this session. |
| `/copy` | Copy the last reply. It uses a native command first, then OSC 52 up to 75 KiB. |
| `/quit` | Detach and quit. |

With no usable provider the session has no model. The footer row shows `no model`, and a prompt is refused and stays in the editor. `/providers` opens the sign-in box. The leader checks an API key before it saves it (mode 0600 `auth.json`); it cannot check the Alibaba Token Plan key, so it saves that key as it is. The key is pasted into a masked input.

The key `thinkingFold` in the settings sets the fold mode that a thinking step takes when it ends: `"expanded"` (all lines, the default), `"truncated"` (the last lines), or `"collapsed"` (one row). A missing key, an unknown value, or a file that cannot be read gives `"expanded"`. The TUI reads `<ASK_HOME>/settings.json` once when it starts, for the first frame. Then it uses the value of the session state, so a trusted project file can override the user value. The TUI never writes the key, so a change takes effect at the next start. A streaming thinking step always shows its last lines.

`ctrl+t` opens or folds all thinking steps and also sets the mode for the thinking steps that end later in the same TUI process. It does not change `settings.json`.

At the start of a session (also after `/new`), a folder that needs a trust decision opens the box `Trust this project folder?` in the prompt pane slot. The options are **Not now** (the first and highlighted option: this session is untrusted and nothing is saved), **Trust this folder**, **Trust parent folder**, and **Do not trust**. Esc and ctrl+c act as Not now. `--approve` or `--no-approve` skips the question. The leader saves the choice in `trust.json`.

The key bindings, block folding, the viewer, and the mouse rules are in [internal/tui/README.md](../internal/tui/README.md). Terminal.app, iTerm2, and kitty were not checked by hand.

## Settings files

Ask reads two settings layers for each session:

| Layer | File | Rules |
|---|---|---|
| user | `<ASK_HOME>/settings.json` | Mode 0600 in a home of mode 0700. |
| project | `<project>/.ask/settings.json` | Read only when the project is trusted. The project is the canonical cwd (symbolic links resolved). Ask does not search parent folders for `.ask`. When `<cwd>/.ask` is the Ask home, the session has no project layer. The file must be a regular file of at most 1 MiB. |

The project file overrides the user file. JSON objects merge key by key. A project array, string, number, or bool replaces the user value. A project `null` is ignored. Two kinds of keys are an exception: a project file cannot set `defaultProjectTrust`, and it cannot set a key of the deny list (`trust`, `httpProxy`, `env`, `shellPath`, `shellCommandPrefix`, `npmCommand`, `providers`, `hooks`, `cacheWarming`, `deviceId`). The match ignores letter case. Ask drops such a key with one diagnostic, and the rest of the project file still applies.

| Key | Values | Layers |
|---|---|---|
| `defaultModel` | A model id `provider/id@api`. `/model` saves it. | user, project |
| `thinkingFold` | `"expanded"` (default), `"truncated"`, `"collapsed"` | user, project |
| `defaultProjectTrust` | `"ask"` (default), `"always"`, `"never"`: the trust of a project with gated files and no saved decision | user only |
| `defaultTools` | An array of built-in tool names, for example `["read", "grep"]`, or an array of `"+name"` and `"-name"` entries, for example `["-bash"]`. Plain names and `+`/`-` entries in one array are a bad value. Default: unset, which keeps every built-in tool. | user, project |

Rules of `defaultTools`:

- Plain names are the allowed set. `+name` and `-name` change the full set: `-bash` keeps every tool except `bash`. A `-name` always wins.
- An unset key keeps every tool. An empty array `[]` keeps no built-in tool. A `null` is the same as an unset key.
- Arrays replace across layers. They never merge. A trusted project array replaces the user array, and an empty project array replaces a user array with names. A project `null` keeps the user value.
- A project array can keep more tools than the user array, because it replaces the user array. A project that is not trusted gives no value.
- A value with a bad shape (not an array of strings, a bad entry, or plain names mixed with `+`/`-` entries) is ignored and gives a diagnostic. The key then counts as unset, also when the other layer has a valid value. A name that is well-formed but is not a built-in tool is not a bad shape: it ends the session start with `defaultTools: unknown tool "<name>"; known tools: <list>`.
- `-t` and `-nt` replace the setting for one run (see [Tools](#tools)).

Ask keeps unknown keys when it writes a file. A write reads the file again under a lock, changes only its own key, and replaces the file atomically. Ask never overwrites a corrupt file: it refuses the write and names the path. A file that cannot be read, or a bad value of a known key, gives the default and a diagnostic. `ask -p` writes each diagnostic as one `Warning: settings:` line to stderr; `ask settings show` lists them. A diagnostic never contains file content.

## Project trust

A project needs a trust decision when one of these entries exists (Ask uses `Lstat` and opens no file):

- In `<project>/.ask/`: `settings.json`, `SYSTEM.md`, `APPEND_SYSTEM.md`, `mcp.json`, `models.json`, `skills`, `prompts`, `extensions`.
- `.agents/skills` in the project folder and in each parent folder, except `~/.agents/skills`.

Context files such as `AGENTS.md` and `CLAUDE.md` are not gated. They always load, also in a project that is not trusted (see [Context files](#context-files)). Ask reads no gated file of a project before the decision. These need trust: the project `.ask/settings.json`, the project `.ask/SYSTEM.md` and `.ask/APPEND_SYSTEM.md`, and the project skill folders. The user files in `<ASK_HOME>` and `~/.agents/skills` need no trust.

The first rule that matches decides:

1. `--approve` or `--no-approve` (or the session choice of the trust box).
2. A folder with no gated files is trusted. Ask saves nothing for it.
3. A `trust.json` that cannot be read or is not safe gives untrusted, with one warning that names the path. `defaultProjectTrust` does not apply then. Ask refuses each grant and keeps the bytes of the file.
4. The saved entry of the folder, or else of the nearest parent folder.
5. `defaultProjectTrust: "always"` or `"never"` in the user settings.
6. The TUI asks. `ask -p`, `ask acp`, `ask settings show`, `ask connect`, and older clients have no question: the project is untrusted.

`<ASK_HOME>/trust.json` is one JSON object: a canonical folder path to `true` (trusted) or `false` (not trusted). It has mode 0600. The leader (or the in-process TUI) writes it; a client asks the leader with `_ask/trust/state` and `_ask/trust/set`. The leader reads the file at each session start, so a change applies to the next session. `ask -p` prints `Warning: project settings ignored: <root> is not trusted. Use --approve, or save a decision in the TUI with /trust.` on stderr when it does not trust a project with gated files.

Trust is not a security boundary against the tools of the agent. A `bash` command can change `trust.json` or the project files.

## Environment variables

| Variable | Used by | Effect |
|---|---|---|
| `ASK_HOME` | all `ask` commands | Home directory (default `~/.ask`). It holds `auth.json`, `settings.json`, `trust.json`, `model-cache.json`, `leader.sock`, and `leader.log`. It must be an absolute path. For the leader it must be short enough for a Unix socket path. |
| `ASK_FAUX` | leader, `ask acp`, in-process TUI | `1` lists the demo provider `faux`. `ask -p` has it by default and ignores the variable. |
| `ASK_FAUX_TPS` | `faux` provider | Pace in tokens per second. A negative value or a non-number is an error. |
| `ASK_COMMAND_GUARD` | `ask -p`, leader, `ask acp` | `1` blocks a short list of destructive commands. It is off by default and is not a security boundary. |
| `ASK_DISABLE_BACKGROUND_TASKS` | `ask -p`, leader, `ask acp`, in-process TUI | `1` turns background jobs off. A foreground `bash` command stops at its wait budget. A background start and `monitor` are refused. |
| `ASK_DISABLE_MODEL_REFRESH` | leader, `ask acp`, in-process TUI, `ask auth login` | `1` turns the automatic network refresh of the model list off: no models.dev request and no account model list request. The list then comes from `<ASK_HOME>/model-cache.json` and the built-in catalog. `ask models refresh` still refreshes the list. |
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
