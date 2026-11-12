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
| `--provider <name>` | `faux`, `alibaba-token-plan`, `anthropic`, `openai`, `xai`, or a provider of the user `models.json`. Without `--provider` and `--model`, the start model comes from the scope or the saved `defaultModel`; see [Start model](#start-model). |
| `--model <pattern>` | Model pattern `[provider/]id[@api][:level]`, for example `anthropic/claude-sonnet-4-6:high`. See [Model patterns](#model-patterns). With `--provider P`, the pattern is `P/<pattern>`. `--provider` alone uses the default model of the provider: `faux-1` (faux), `deepseek-v4.1-flash` (alibaba-token-plan), `claude-sonnet-4-6` (anthropic), `gpt-5.5` (openai), `grok-4.7` (xai). The model list is in [reference/providers.md](reference/providers.md). |
| `--models <patterns>` | The scope: the models to start on and to cycle through with `ctrl+p` in the TUI. Give a comma list; repeat the flag to add more. Each entry is a pattern or a glob. It replaces the `enabledModels` setting. See [Model scope](#model-scope). |
| `--api-key <key>` | Key for this run. It needs a model (`--model`, with or without a provider in the pattern), except for `alibaba-token-plan`. A key in the arguments is visible to other processes. Prefer the environment or `ask auth login`. |
| `--thinking <level>` | `default`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`. It wins over a `:level` of the pattern. `default` removes the level that a pattern or a scope entry chose. `alibaba-token-plan` defaults to `medium`. |
| `--offline` | Do not refresh the model list from the network. It is the same as `ASK_DISABLE_MODEL_REFRESH=1` for this process and for a leader that this command starts. A leader that already runs keeps its own setting; stop it with `ask leader stop` first. Print mode makes no refresh request, so the flag changes nothing there. |
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

### Start model

`ask -p`, the TUI, the leader, and `ask acp` choose the start model of a session in this order:

1. `--provider` and `--model`. A pattern that does not resolve ends the run with exit 1 (in the TUI the session start fails with the same text).
2. The scope: `--models`, else the `enabledModels` setting. When the saved `defaultModel` is in the scope and its provider is usable, it is the start model. Else the first scope entry of a usable provider is the start model.
3. The saved `defaultModel` (the value that `/model` saves). The project layer counts only when the project is trusted.
4. `ask -p` runs on `faux`. The TUI, the leader, and `ask acp` take the first model of a usable provider.

When the saved default is not in the model list, or its provider has no credential, `ask -p` writes one line to stderr, for example `Warning: default model <id> is not usable (no credential); using faux/faux-1@faux.`, and runs on `faux`.

The level is `--thinking`, else the `:level` of the pattern or of the scope entry. With neither, no level is chosen, and Ask shows the level as `default`. When the start model has no thinking, a requested level is dropped (`default` and `off` are never dropped). `ask -p` then writes `Warning: <provider>/<id> has no thinking; level <level> is not used.`, and the TUI shows the same text as a session notice. The run continues with thinking off.

A requested start level then fits the levels of the model, with the same rule as `/model` and the thinking key of the TUI. A level that the model does not have becomes the nearest lower level that it has. Some models cannot turn thinking off (`grok-4.7`, `gpt-6-astra`, `gpt-6.1-sol`). For these models, off becomes the lowest effort of the model, in `ask -p`, the TUI, the leader, and `ask acp`.

With no chosen level (`default`), Ask sends no level, and the provider path picks one. The OpenAI Responses adapter (`openai`, `xai`) sends no reasoning effort, so the vendor default applies. The Anthropic Messages adapter (`anthropic`, `alibaba-token-plan`) and the OpenAI Completions adapter (`alibaba-token-plan`, custom providers) use `medium`. `_ask/session/state`, the footer row of the TUI, `ASK_REASONING_LEVEL`, and the `thinkingLevel` of an assistant message say `default`.

A model change (`/model`, `ctrl+p`, or the `setModel` method of a client) keeps the level that you chose. A level, off included, fits the new model with the same rule. With no level, the level stays empty after a model change, so the request still has no reasoning effort. A `PrepareRequest` hook sees `default` when no level is chosen. A hook that sets a level uses the same rule with the model of the request: off stays off, and on a model that cannot turn thinking off it becomes the lowest effort. A hook that sets `default` removes the chosen level. A hook level applies to the requests of its run only: the session keeps its level, and the footer row does not change. A `PrepareRequest` hook that changes the model and sets no level uses the rule of a model change: a chosen level fits the new model for the rest of that run, and an empty level stays empty.

### Model patterns

`--model`, `--models`, `enabledModels`, and the session options of a client take a model pattern: `[provider/]id[@api][:level]`.

- `provider/id` selects one model. `@api` selects the wire when a provider has the model on two APIs (for example `alibaba-token-plan/deepseek-v4.1-flash@openai-completions`).
- An id without a provider resolves across all providers. An exact id, an alias, a dated id, and then a part of an id match. When two providers have a match, the pattern is ambiguous: the error lists the choices, and you add the provider.
- `:level` sets the thinking level. The level words are `default`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`. `:default` chooses no level.
- Ask first tries the whole pattern as a model id. Only when no model has that id does Ask read the text after the last colon as a level. Thus `local/qwen3-coder:30b` is the model `qwen3-coder:30b`, and a model with the id `x:max` is that model, not `x` with level `max`. `kimi:high` is the model `kimi` with level `high`, not the model `kimi-highspeed`.
- A word after the colon that is not a level word makes the pattern an unknown model: `Error: unknown model "openai/gpt-5.5:hgih"`. When the text before the colon is a model, the error lists the level words.

The saved `defaultModel` and the model id of `_ask/session/set_model` are always the exact name `provider/id@api`. They do not take a pattern.

### Model scope

`--models` and the `enabledModels` setting give the scope. The scope chooses the start model and the cycle list of `ctrl+p` in the TUI. `/model` still lists every model.

- Each entry is a pattern, or a glob with `*`, `?`, or `[...]`. A glob matches `provider/id` or `id` in lower case, and it can have a `:level` suffix.
- A glob `*` does not match `/`. So `openai/*` matches `openai/gpt-5.5`, but not `openrouter/qwen/qwen3`. Use `openrouter/*/*` for that.
- The scope keeps the order of the entries, and each model is in it once.
- An entry that matches nothing gives one warning, for example `Warning: --models: unknown model "<pattern>"`. For a settings entry the warning names the position (`enabledModels[0]`), not the text. The other entries still apply.

### Background work in print mode

A `bash` job or a `monitor` can still run when the last prompt ends. `ask -p` and `--mode json` then wait for it instead of stopping it:

- Print mode prints the reply of the last prompt at once. Then it waits by signal. Each completion notice or monitor event starts one more run, and print mode prints the reply of each of these runs. `--mode json` writes the events of these runs to the stream.
- The wait ends when no run, task, or job is left. A job that still runs at the exit (signal, idle cap, or `--max-turns`) is stopped.
- A background job stops after a time limit. The default is 10 minutes. A `bash` call with `run_in_background` can set the limit with `timeout`, up to 2 hours. Every `monitor` stops after at most 10 minutes, also a `persistent` one. The model gets a notice when a limit stops a job. The foreground wait of `bash` keeps its own budget (default 120 s, maximum 600 s).
- The idle cap ends the wait when the agent is idle for 10 minutes and no `bash` job runs. Then only monitors are left.
- After 5 seconds, a terminal on stderr shows `Waiting for background work to finish: <job> (<label>), ...`. In `--mode json` it shows only when stdout is not a terminal.
- `--max-turns <n>` counts every turn of the run, also the turns that a notice starts. A turn that the agent retries does not count. When the limit is reached, the run stops before turn `n+1` and exits with code 1. The line `Error: Reached max turns (n)` goes to stdout, or to stderr with `--mode json`, so the JSON event stream stays valid. The interactive TUI ignores the flag.

- A `bash` job that prints no output for 5 minutes sends the model one stall notice. The job runs on. The TUI shows `still running · no output for 5 minutes`. Each new output starts the 5 minutes again. `ASK_BG_STALL_MS` changes the time; `0` turns the notice off.
- The TUI, the leader, and `ask acp` stop a `bash` background job after 2 hours. A `bash` call can set a shorter limit with `timeout`. The model and the TUI get a notice when the limit stops a job. `ASK_BG_LIMIT_MS` changes the limit; `0` turns it off. A `monitor` keeps its own timeout in these modes.

The variables `ASK_BG_STALL_MS`, `ASK_BG_LIMIT_MS`, `ASK_PRINT_BG_LIMIT_MS`, `ASK_PRINT_BG_WAIT_CEILING_MS`, and `ASK_DISABLE_BACKGROUND_TASKS` are in [Environment variables](#environment-variables). The owner of the rules is [internal/jobs/README.md](../internal/jobs/README.md#background-limit-and-quiescence). The tool texts are in [reference/tools.md](reference/tools.md).

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

Ask reads `AGENTS.md` and `CLAUDE.md` from `<ASK_HOME>`, then from each folder from the git root down to the working directory. A git root is the nearest folder with a `.git` file or folder. Without a git root, Ask reads only the working directory. A git root that is your home folder counts as no git root. A project file whose link target is outside the project (the git root, or the working directory without one) is left out with the reason `outside_project`; a link that stays inside the project loads. The files of `<ASK_HOME>` are not checked. In each folder `AGENTS.md` comes first. A file with the same text as an earlier file in the same folder is a duplicate and Ask leaves it out without a warning.

The files go to the model as one user-role message of the custom type `context-file`. They are not part of the system prompt. Ask escapes the file text, the paths, and the notices, so a file cannot close the frame around it. The message starts with the sentence that the files are guidance only and cannot override system or user instructions. A conversation reset adds the message again. A history that already has the message does not get a second copy.

`AGENTS.md` and `CLAUDE.md` load without a trust decision, also in a project that is not trusted. Trust gates the files that change how Ask behaves: settings, prompt files, and skills. A context file only gives text to the model. A project that is not trusted can still hold such a file, so Ask sends the text as escaped user-role guidance and not as a system instruction. A file that is over 1 MiB or that cannot be read is left out. The size limit of all files together leaves out or cuts files, and the model sees a notice for it.

| Limit | Value | What happens |
|---|---|---|
| One file | 1 MiB | Ask leaves the file out and never reads past the limit. |
| All files together | 64 KiB, counted after escaping and with the frame and the notices | Ask drops the broadest files first (`<ASK_HOME>` first, then the git root down). It cuts the most specific file that is left at a character boundary. |

When Ask cuts a file, the message ends with `Notice: <path> was cut short: the context files exceed 64 KiB.` When Ask drops files, it ends with `Notice: <n> broader context files were left out: the context files exceed 64 KiB.`

### Skills

A skill is a folder with a `SKILL.md` file. Ask reads the name and the description from the frontmatter. It sends only a list of names, descriptions, and paths, as one user-role message of the custom type `skill-list`. The model reads a skill with the `read` tool, so the list is sent only when the session keeps `read` or `bash`. A skill with `disable-model-invocation: true` is not in the list. Ask follows the rules of Codex here, so a skill folder that works there loads in Ask.

Ask looks in these folders, in this order, to a depth of 6, in at most 2000 folders and 20000 entries for each folder of this list (a walk that reaches a limit stops without an error). It skips hidden folders below a root and follows symbolic links to folders:

1. `<ASK_HOME>/skills`.
2. `~/.agents/skills`.
3. In a trusted project: `<project>/.ask/skills` and `<project>/.agents/skills`, then the `.agents/skills` of each parent folder, nearest first.

A project skill folder is a gated entry, so an untrusted project never gives skills.

**Name.** The `name` is the frontmatter value with every run of white space turned into one space. A missing or empty name falls back to the folder name (`skill` if that is empty too). A name over 64 characters is cut to 64 characters. There is no rule on the characters, so `name: kk:agent-browser` loads.

**Description.** The `description` is required: a missing or blank one is the problem `missing description`. White space is collapsed to one line. The loader has no length cap. The list cuts a description over 1024 characters to its first 1021 characters and `...`.

**Broken YAML.** When the frontmatter is not valid YAML, Ask double-quotes each top-level value that holds `: ` or starts with `[`, `{`, `@`, or a backtick, and reads it once more. So `description: Build for AWS: ECS` loads. The problem `bad frontmatter` is left only when the second read fails too. `disable-model-invocation` stays strict: it must be the YAML boolean `true` or `false`, so `yes` and `"true"` are the problem `bad boolean disable-model-invocation`. Ask reads at most 64 KiB of frontmatter (`frontmatter over 65536 bytes`).

**Same name.** Skills of the same name are all kept, and the list shows the path of each. The order is the scope (project before user), the name, and the path. A `SKILL.md` that two folders reach is listed once. Nothing is reported for a name that appears twice.

**Problems.** The reasons that remain are `no frontmatter`, `bad frontmatter`, `missing description`, `bad boolean disable-model-invocation`, `frontmatter over 65536 bytes`, and `unreadable`. A skill with a problem is not loaded, and the other skills are.

**Budget.** The list takes a part of the context window of the session model. A line is one skill entry; it costs its text and a line break. In tokens, a line costs `ceil(bytes / 4)` plus one. The header and the footer are free. The budget is:

| Condition | Budget |
|---|---|
| `skillsMaxContextTokens` is set | That many tokens, at most 10000 |
| The model window is known | 2% of the window in tokens (100000 gives 2000, 400000 gives 8000) |
| The window is not known | 8000 characters (runes, not bytes) |

Ask fits the skills in three steps:

1. Every description (cut to 1024 characters) fits: the list has them all.
2. Otherwise, if every skill fits with an empty description, the room that is left goes to the descriptions one character at a time, round-robin, so one skill cannot take it all. No description gets longer than its own text.
3. Otherwise, the skills keep their order with no description until the budget ends, and the rest are left out.

When skills are left out, the list ends with `- <n> additional skill(s) omitted from this bounded skills list.`, and that line is charged to the budget: Ask drops entries from the end until it fits. It also writes one warning, `Exceeded skills context budget. All skill descriptions were removed and <n> additional skill(s) was/were not included in the model-visible skills list.` Shortened descriptions alone give no warning. The list is made once at the session start, so a later `/model` does not change it.

**Load a skill by name.** A prompt that starts with `/skill:<name>` loads that skill. The name ends at the first white space. The rest of the text, without the white space around it, is the arguments. The text that goes to the model is:

```
<skill name="<name>" location="<path of SKILL.md>">
References are relative to <folder of SKILL.md>.

<the text of SKILL.md after its frontmatter>
</skill>

<the arguments>
```

The empty line and the arguments are absent when there are no arguments. The skill text is at most 100 KB. When it is longer, Ask cuts it on a character boundary and adds the note `[The skill text is cut here at 100 KB. Read the SKILL.md file at the location above for the rest.]`. A skill with `disable-model-invocation: true` loads too: loading it by name is its purpose. The first skill of that name in the order above wins. A name that no skill has leaves the text as you typed it. A project skill is available only in a trusted project.

The agent of the session does this, so it works the same in `ask -p '/skill:<name> text'`, in the TUI with a leader or an in-process agent, and over `ask acp`. Only the first block of a prompt, a steer, or a follow-up is checked. The log of the session keeps the text after the change. The TUI shows what you typed (`/skill:<name> text`) in the transcript and in the queue rows, not the skill text.

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

- `Warning: <n> context file(s) was/were skipped; run ask settings show for details`. It counts the files that Ask left out or cut (too large, not readable, outside the project, or over the 64 KiB limit). A duplicate is not counted.
- `Warning: skills: <path>: <reason>`. One line for each skill that did not load, at most 4 lines of at most 256 bytes each. When more skills have a problem, `Warning: skills: <n> more skill problem(s); run ask settings show for details` follows. The leader and `ask acp` send the same rows, with the same cap, as session diagnostics.
- `Warning: Exceeded skills context budget. ...`. The skill list left skills out (see [Skills](#skills)). It comes when the agent is built, after the lines above.

The context file line is a summary, and its detail is in `ask settings show`. `ask settings show` also keeps the full list of skill problems. A line never contains file content.

### Flags in the TUI and with a leader

The TUI sends the prompt and tool flags to its host with `session/new`, in `_meta["ask.dev/session"]` (`systemPrompt`, `appendSystemPrompt`, `tools`, `noTools`). The prompt flags hold text, not paths. The flags apply to every session that this TUI process opens, also after `/new`. They are not saved.

The TUI sends `--model`, `--thinking`, and `--models` for its first session in the same object (`model`, `thinking`, `models`). The host resolves them against its own model list and echoes the options that it applied in the result `_meta["ask.dev/appliedOptions"]`. When the TUI sent one of these fields and the echo does not have it, the session start fails with `the leader did not apply --model; restart it with this build` (or `--thinking`, `--models`). Thus a leader that is older than this build never drops `--model` without a message. Stop it with `ask leader stop`, or use `--no-leader`.

A leader that is older than this build ignores the key without an error. The TUI then shows `Warning: the leader ignored the prompt and tool flags of this command line (it is older than this build). They are not applied in this session.` The session runs with more tools and with the default prompt. Stop the old leader with `ask leader stop`, or use `--no-leader`.

`ask acp` does not take these flags. An ACP client that starts a session can send `_meta["ask.dev/session"]` with the object `{"systemPrompt": "...", "appendSystemPrompt": ["..."], "tools": ["read"], "noTools": false, "model": "anthropic/claude-sonnet-4-6:high", "thinking": "low", "models": ["anthropic/*"]}`. The host applies it to that session only. A value that is not an object, a field of the wrong type, prompt text over 256 KiB in total, a tool name that is not valid, both `tools` and `noTools`, a `model` or a `models` entry over 256 bytes, more than 64 `models` entries, or a `thinking` that is not a level word answers `invalid_params`. When the host applied non-empty flags, the result `_meta` has `ask.dev/session: true`, and `ask.dev/appliedOptions` echoes the model options that it applied.

`_ask/session/get_available_models` returns `scope` (the cycle list in order: the exact model id and the level of each entry) and `diagnostics` (the problems of the user `models.json` that the list read). `_ask/session/set_model` takes an optional `thinkingLevel` and sets the model and the level in one call.

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
ask acp [--approve | --no-approve] [--offline]
ask acp --help
```

The command serves ACP v1 as JSON-RPC lines on stdin and stdout. Diagnostics go to stderr. It opens no listener and no database. Each session has its own agent. No credentials pass over the connection: sign in first with `ask auth login`.

`--approve` (`-a`) trusts the project of every session of this process, and `--no-approve` (`-na`) trusts none. Without a flag, each session decides with no UI (see [Project trust](#project-trust)). `--offline` turns the model refresh off, as `ASK_DISABLE_MODEL_REFRESH=1`. The two trust flags together, or any other argument, are an error. The command takes none of the prompt, tool, and model flags (`--system-prompt`, `--append-system-prompt`, `-t`, `-nt`, `--model`, `--models`, `--thinking`). A client sends them for one session in `_meta["ask.dev/session"]` (see [Flags in the TUI and with a leader](#flags-in-the-tui-and-with-a-leader)). The command answers `unsupported` for `_ask/trust/state` and `_ask/trust/set`, because an editor link has no trust question.

Exit codes: 0 input ended, 1 error or lost output, 130, 143, 129. The owner is [internal/acp/README.md](../internal/acp/README.md).

## `ask settings`

```
ask settings show [--json] [--approve | --no-approve]
ask settings --help
```

The command prints the settings of the current folder. It reads files only. It writes nothing, and it starts no leader and no database. It decides the project trust as `ask -p` does (with no UI), and the trust flags change that decision for this run only. It does not read the project settings file of a project that is not trusted. It prints only the shipped keys, never other keys, file content, or credentials.

The text form shows, in this order:

1. The Ask home, the user settings file, the project root, the trust decision with its source (and the saved entry that applies), the gated files, the state of the project settings: `loaded`, `not read (untrusted)`, or `none` (the `.ask` folder of the cwd is the Ask home), and the row `User models:` with the path of the user `models.json` and its state (`loaded`, `problem`, or `missing`).
2. `Settings:` one row for each key, with the value and the layer that sets it (`default`, `user`, or `project`). `(none)` means that no `defaultModel` is set.
3. `Key sources:` one row for each provider with the source of the key that a request would use (see [Key order](#key-order)). The row names the source, never the key. A `problem` row adds the problem text, which names a variable or a label, never a value.
4. A `Warning:` line for a trust store that cannot be read.
5. `Problems:` one row for each settings diagnostic: the layer, the path, the key, and a fixed text.
6. `Context files:` one row for each `AGENTS.md` or `CLAUDE.md` that the session reads, with its path and size in bytes, then the files that Ask left out. A row has the word `too_large`, `unreadable`, `duplicate`, `outside_project`, `budget`, or `truncated` when Ask did not deliver the file whole. `(none)` means that no context file exists.
7. `Skill problems:` one row for each skill that did not load: the layer, the path, and a fixed text. The list is complete: the 4-row cap of `ask -p` and the TUI does not apply here. The block is absent when every skill loaded.

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
    {"key": "defaultTools", "value": "read,grep", "source": "user"},
    {"key": "skillsMaxContextTokens", "value": "", "source": "default"},
    {"key": "enabledModels", "value": "anthropic/*,gpt-5.5:low", "source": "user"}
  ],
  "diagnostics": [
    {"layer": "project", "path": "/work/repo/.ask/settings.json", "key": "env", "text": "a project file cannot set this key; it is ignored"}
  ],
  "context": [
    {"path": "/work/repo/AGENTS.md", "bytes": 1204, "skipped": ""}
  ],
  "skillDiagnostics": [
    {"layer": "project", "path": "/work/repo/.agents/skills/old/SKILL.md", "key": "skills", "text": "a fixed reason"}
  ],
  "modelsFile": {"path": "/home/u/.ask/models.json", "state": "loaded"},
  "keySources": [
    {"provider": "anthropic", "source": "saved:api-key"},
    {"provider": "local", "source": "config:$LOCAL_KEY"},
    {"provider": "other", "source": "problem", "problem": "other apiKey: variable LOCAL_UNSET_KEY is not set"}
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
| `settings[].value` | The effective value. An empty `defaultModel` means that no default model is set. `defaultTools` is the array joined with commas; an empty value is shown for an unset key and for an empty array, so read `source` to tell them apart (`default` means unset). `skillsMaxContextTokens` is the number, or empty when unset. |
| `settings[].source` | `default`, `user`, or `project`. |
| `diagnostics[].key` | The key, or empty for a problem with the whole file. |
| `context[].bytes` | The size of the file in bytes. `0` when Ask cannot read it. |
| `context[].skipped` | Empty for a file that the session gets whole, else `too_large`, `unreadable`, `duplicate`, `outside_project`, `budget`, or `truncated`. |
| `skillDiagnostics[]` | Every skill that did not load. `key` is `skills`. |
| `settings[].value` of `enabledModels` | The array joined with commas, or empty when unset. |
| `modelsFile.state` | `loaded`, `problem` (the file has a problem; see `diagnostics`), or `missing`. |
| `keySources[].source` | `override`, `saved:<method>`, `config:$NAME` (a template with more variables names all of them, for example `config:$A,$B`), `config:command`, `config:literal`, `env:<NAME>`, `none`, or `problem` (a `models.json` value cannot give a key). The command never runs a `!command` to find it, and never refreshes a token. |
| `keySources[].problem` | The problem text of a `problem` row, else absent. |

A bad value of a known key gives the default and a diagnostic. A corrupt user file or trust store is reported and keeps its bytes.

Exit codes: 0 done, also with diagnostics or a trust store warning; 1 for a usage error, a cwd that cannot be read, or an output that cannot be written; 130, 143, 129.

## `ask leader`

```
ask leader [--offline]          run the leader in the foreground
ask leader status [--json]      show the identity and counts of the running leader
ask leader list [--json]        list the leaders of this home (one)
ask leader stop                 stop the running leader
ask leader --help
```

- `--offline` turns the model refresh of the leader off, as `ASK_DISABLE_MODEL_REFRESH=1`. A TUI started with `--offline` passes the variable to a leader that it starts.
- The leader listens on `<ASK_HOME>/leader.sock`. The socket has mode 0600 in a directory of mode 0700. Only the owner can connect.
- `status`, `list`, and `stop` never start a leader. `--json` works only with `status` and `list`; it exits 1 when the output cannot be written.
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

Exit codes: 0 left, 1 error or lost leader (also when the input ends after a prompt that ended because the leader closed), 130, 143, 129. A signal during setup, before the session starts, also exits with 130, 143, or 129.

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
| `/skill:<name>` | Load a skill of this session (see [Skills](#skills)). One row for each skill, after the commands above, with its description. Enter puts `/skill:<name> ` in the editor and sends nothing, so you can add arguments and send. Typing `/sk` or `/skill:kk` shows only the rows that match. The rows come from the session and change with a new session. A leader that is older than this build gives no skill rows and no error. |

While the command list is open, `Tab` completes the draft like a shell. When the rows share a longer prefix than the draft, `Tab` extends the draft to that prefix, and the list stays open on the rows that are left. Otherwise `Tab` fills the draft with the selected row: a `/skill:<name>` row gets a trailing space for arguments, and a built-in command gets none, so `Enter` then runs it. `↑` and `↓` move the selection, and the selection goes back to the first row after each completion. After `Esc` the list stays closed until the next edit, and `Tab` with the list closed moves focus to the block list. `Shift+Tab` still cycles the thinking level.

With no usable provider the session has no model. The footer row shows `no model`, and a prompt is refused and stays in the editor. `/providers` opens the sign-in box. The leader checks an API key before it saves it (mode 0600 `auth.json`); it cannot check the Alibaba Token Plan key, so it saves that key as it is. The key is pasted into a masked input.

The key `thinkingFold` in the settings sets the fold mode that a thinking step takes when it ends: `"expanded"` (all lines, the default), `"truncated"` (the last lines), or `"collapsed"` (one row). A missing key, an unknown value, or a file that cannot be read gives `"expanded"`. The TUI reads `<ASK_HOME>/settings.json` once when it starts, for the first frame. Then it uses the value of the session state, so a trusted project file can override the user value. The TUI never writes the key, so a change takes effect at the next start. A streaming thinking step always shows its last lines.

`ctrl+t` opens or folds all thinking steps and also sets the mode for the thinking steps that end later in the same TUI process. It does not change `settings.json`.

`shift+tab` changes the thinking level of a model that has thinking. The order is `default`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and then `default` again. `default` means no chosen level (see [Start model](#start-model)). A level that the model does not have fits the model, and the next press goes on from the level that you asked. The footer row shows the level. While a run works, the key gives `Thinking level can change when the run ends.`. A leader of an older build does not know `default`: the status row then shows `Could not change the thinking level: the leader does not know default.` and `Restart it with this build: ask leader stop`, and the next press asks `off`.

`ctrl+p` changes the session model to the next model of the cycle list, and `ctrl+shift+p` changes it to the previous one. The keys work in the prompt pane, in the block list, and with the command list open. In `/model` and in the provider box, `ctrl+p` moves the list selection up. The cycle list is the scope (`--models` or `enabledModels`); with no scope it is every model that `/model` shows. The list wraps at each end. The footer row names the new model, and the status row shows `✓ Model: <provider>/<model> · <level> · <n> of <total>`. The key does not save the default. While a run works, the key gives `Model can change when the run ends.`; with one model in the list, it gives `No other model to cycle to.`. `ctrl+shift+p` needs a terminal with the kitty keyboard protocol or `modifyOtherKeys`, for example Ghostty, Alacritty, kitty, WezTerm, or xterm. Terminal.app sends `ctrl+shift+p` as `ctrl+p`, so there the key cycles forward and no key cycles back. The shortcut bar does not show these keys.

When `/model` opens, the leader (or the in-process agent) reads the user `models.json` again. When the file has problems, `/model` shows one summary row (`models.json has <n> problems; the list is in the transcript.`) and the transcript gets the block `Problems in models.json, read again when /model opened`. When an edit of the file removed or changed the row of the session model, its row shows ` · not in models.json` or the tag `changed`. Choose that row again to use the new row. Until then a prompt fails with `Error (config): model configuration changed; choose the model again.` and sends nothing.

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
| `skillsMaxContextTokens` | A positive integer: the token budget of the skill list, at most 10000 (a larger value counts as 10000). Default: unset, which uses 2% of the model window. A zero, a negative number, or a value that is not an integer is ignored with a diagnostic. See [Skills](#skills). | user, project |
| `enabledModels` | An array of at most 64 model patterns or globs of 1 to 256 bytes each, for example `["anthropic/*", "gpt-5.5:low"]`: the scope of a session when `--models` is not given. See [Model scope](#model-scope). A value with a bad shape is ignored with a diagnostic. | user, project |

Rules of `defaultTools`:

- Plain names are the allowed set. `+name` and `-name` change the full set: `-bash` keeps every tool except `bash`. A `-name` always wins.
- An unset key keeps every tool. An empty array `[]` keeps no built-in tool. A `null` is the same as an unset key.
- Arrays replace across layers. They never merge. A trusted project array replaces the user array, and an empty project array replaces a user array with names. A project `null` keeps the user value.
- A project array can keep more tools than the user array, because it replaces the user array. A project that is not trusted gives no value.
- A value with a bad shape (not an array of strings, a bad entry, or plain names mixed with `+`/`-` entries) is ignored and gives a diagnostic. The key then counts as unset, also when the other layer has a valid value. A name that is well-formed but is not a built-in tool is not a bad shape: it ends the session start with `defaultTools: unknown tool "<name>"; known tools: <list>`.
- `-t` and `-nt` replace the setting for one run (see [Tools](#tools)).

Ask keeps unknown keys when it writes a file. A write reads the file again under a lock, changes only its own key, and replaces the file atomically. Ask never overwrites a corrupt file: it refuses the write and names the path. A file that cannot be read, or a bad value of a known key, gives the default and a diagnostic. `ask -p` writes each diagnostic as one `Warning: settings:` line to stderr; `ask settings show` lists them. A diagnostic never contains file content.

## models.json

`<ASK_HOME>/models.json` adds providers and models, and changes the fields of built-in models. You need no rebuild. Ask never writes this file.

Ask reads only the user file. It does not read `<project>/.ask/models.json`, also in a trusted project. That name is still a gated entry (see [Project trust](#project-trust)), so a project with the file needs a trust decision, but the file has no effect.

### File rules

- The file has mode 0600 in a home of mode 0700, as `settings.json`. A file with another mode, a link, a FIFO, or a file over 1 MiB is a file problem.
- The file is JSONC: `//` and `/* */` comments and trailing commas are allowed.
- A provider id has lower-case letters, digits, and `-`.

```jsonc
{
  "providers": {
    "local": {
      "baseUrl": "http://127.0.0.1:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama",                       // a literal; "$OLLAMA_KEY" reads the environment
      "models": [{ "id": "qwen3-coder:30b" }]   // the defaults fill the other fields
    },
    "openai": {
      "modelOverrides": { "gpt-5.5": { "name": "Five", "maxTokens": 32000 } }
    }
  }
}
```

### Fields

| Field | Meaning |
|---|---|
| `providers.<id>.baseUrl` | The URL of the API. It must be an `http` or `https` URL with a host and no user name or password; another value is a problem and drops the provider. Ask removes one trailing `/`. On a built-in provider it moves every model of the provider to this URL. |
| `providers.<id>.api` | The wire: `anthropic-messages`, `openai-completions`, or `openai-responses`. A model can set its own `api`. |
| `providers.<id>.apiKey` | The key of the provider, as a value (see [Values](#values)). |
| `providers.<id>.headers` | Extra request headers, as an object of values. A model can set its own `headers`. |
| `providers.<id>.compat` | Compatibility switches of the wire, for every model of the provider. An unknown key for the API gives a problem and is ignored. For `openai-completions`, `supportsReasoningEffort` is true by default; `false` means Ask does not send `reasoning_effort`. |
| `providers.<id>.models[]` | New models. `id` is required. The other fields are `name`, `api`, `reasoning`, `thinkingLevelMap`, `input` (`text`, `image`), `cost` (`input`, `output`, `cacheRead`, `cacheWrite`, in USD for each million tokens), `contextWindow`, `maxTokens`, `headers`, and `compat`. |
| `providers.<id>.modelOverrides.<model id>` | Changes of one existing model, with the same fields as a model. |

A provider with models that is not built in needs `baseUrl` and `apiKey`. A new model takes the URL of its provider. A new model without these fields gets the defaults of Pi: a context window of 128000, an output limit of 16384, text input, no thinking, and a price of 0. A new model replaces a built-in model with the same provider, id, and API.

An override on a built-in model can set the name, the limits, the price, and the input types, and it can turn thinking levels off. An override cannot change the `api` of a model, and on a built-in model it cannot change the compat either. On a new model of the file, an override can change the compat and add thinking levels. An override price sets only the fields that it names; the other prices stay. An override that names no model of a provider that the file defines is a problem. For a built-in provider it is not a problem, because an account list can hide the model until the next refresh.

When the file changes the `baseUrl` of a built-in provider, Ask shows every model of that provider, because the account list tells the access at the default host only.

### Values

`apiKey` and each header value is a value:

| Value | Result |
|---|---|
| `text` | The literal text. |
| `$NAME` or `${NAME}` | The environment variable `NAME`, also inside other text. An unset or blank variable is an error that names the variable, never a value. |
| `!command` | The whole value is a shell command. Ask uses its trimmed stdout. The command runs in your home folder with the whole environment of the process, for at most 10 s. Stdout larger than 64 KiB stops the command, and the value fails. Ask keeps the output for the life of the process and runs the command again after a 401 answer or an edit of the file. After a failure it waits 30 s before the next run. Ask never logs the output. |
| `$$` | A literal `$`. |
| `$!` at the start | A literal `!`. |

Another `$` form is an error.

### Key order

A request takes the key from the first source that applies:

1. `--api-key` of this run.
2. The saved credential of `ask auth login` or `/providers` in `auth.json`, when it is for the same provider, API, and URL as the model. Ask never sends a saved key to a URL that the file changed.
3. The `apiKey` of the provider in `models.json`. It applies to every model of that provider, also to a built-in provider whose URL the file does not change (as in Pi).
4. The environment variable of the provider, when no credential is saved.

On the OpenAI wires, an `Authorization` header that the model or its provider sets in `models.json` wins over a static API key (a saved key, `apiKey`, the environment, or `--api-key`). A saved OAuth sign-in wins over the header. Ask removes the other credential headers (`X-Api-Key`, `Openai-Organization`, `Openai-Project`, `Chatgpt-Account-Id`, `Cookie`).

For a provider whose `baseUrl` the file changes, Ask uses `--api-key` only when the file entry of that provider also sets `apiKey`. Otherwise the request fails with `auth: unsupported credential method: <provider>/api-key`.

A provider with only a `models.json` key is usable: it can be the start model, the saved default, and a `/model` choice. `ask settings show` names the source that wins for each provider; it uses the same function as a request.

### When Ask reads the file

The leader, `ask acp`, and the in-process TUI read the file again at three points: at a session start, when `/model` opens (the model list), and when a session changes its model. Ask does not watch the file, and a request never reads it. `ask -p` and `ask settings show` read the file once.

A process that loaded a valid file keeps that copy when an edit breaks the file. The model list and the session start then show the problem and `the last valid copy is used`. A process that never loaded a valid copy (for example `ask -p`, `ask settings show`, or a new leader that replaced an older one) shows the problem and no models of the file. A saved default on such a model then falls back with the reason `not in the catalog`. `ask -p` writes each problem as one `Warning: settings: <path>: ...` line.

A bad model or override drops only that entry, and a bad provider drops only that provider. An unknown key loads and gives one problem. A problem names the file position (for example `providers[2].models[0].contextWindow`), never a provider id, a model id, or a value.

### Remove a custom default

A saved `defaultModel` can point at a model of `models.json`, for example `local/qwen3-coder:30b@openai-completions`. When the model is gone (an edit of the file, or a build that does not read `models.json`), a new session falls back with the note `not in the catalog`. To fix it, choose a model with `/model` and save it as the default, or remove the `defaultModel` key from `<ASK_HOME>/settings.json`.

## Project trust

A project needs a trust decision when one of these entries exists (Ask uses `Lstat` and opens no file):

- In `<project>/.ask/`: `settings.json`, `SYSTEM.md`, `APPEND_SYSTEM.md`, `mcp.json`, `models.json`, `skills`, `prompts`, `extensions`. Ask does not read the project `models.json` (see [models.json](#modelsjson)).
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
| `ASK_HOME` | all `ask` commands | Home directory (default `~/.ask`). It holds `auth.json`, `settings.json`, `models.json`, `trust.json`, `model-cache.json`, `leader.sock`, and `leader.log`. It must be an absolute path. For the leader it must be short enough for a Unix socket path. |
| `ASK_FAUX` | leader, `ask acp`, in-process TUI | `1` lists the demo provider `faux`. `ask -p` has it by default and ignores the variable. |
| `ASK_FAUX_TPS` | `faux` provider | Pace in tokens per second. A negative value or a non-number is an error. |
| `ASK_COMMAND_GUARD` | `ask -p`, leader, `ask acp` | `1` blocks a short list of destructive commands. It is off by default and is not a security boundary. |
| `ASK_DISABLE_BACKGROUND_TASKS` | `ask -p`, leader, `ask acp`, in-process TUI | `1` turns background jobs off. A foreground `bash` command stops at its wait budget. A background start and `monitor` are refused. |
| `ASK_DISABLE_MODEL_REFRESH` | leader, `ask acp`, in-process TUI, `ask auth login` | `1` turns the automatic network refresh of the model list off: no models.dev request and no account model list request. The list then comes from `<ASK_HOME>/model-cache.json` and the built-in catalog. `ask models refresh` still refreshes the list. `--offline` on `ask`, `ask acp`, and `ask leader` has the same effect. `ask auth login` has no `--offline` flag; use the variable. |
| `ASK_BG_STALL_MS` | `ask -p`, leader, `ask acp`, in-process TUI | The time without output after which a background `bash` job sends a stall notice, in milliseconds (default 300000). `0` turns the notice off. An empty, negative, or invalid value gives the default. |
| `ASK_BG_LIMIT_MS` | leader, `ask acp`, in-process TUI | The background time limit of `bash` jobs, in milliseconds (default 7200000). `0` turns the limit off. An empty, negative, or invalid value gives the default. A larger value also raises the 2 hour maximum. `ask -p` reads `ASK_PRINT_BG_LIMIT_MS` instead. |
| `ASK_PRINT_BG_LIMIT_MS` | `ask -p` | The default background time limit, in milliseconds (default 600000). A value that is not a whole number above 0 is ignored. A larger value also raises the 2 hour maximum. |
| `ASK_PRINT_BG_WAIT_CEILING_MS` | `ask -p` | The idle cap of the background wait, in milliseconds (default 600000). `0` means no cap. An empty, negative, or invalid value gives the default. |
| `ASK_CAPTURE` | `ask -p` | A directory. The run records the provider HTTP traffic to a new cassette, also when only `--model` selects the provider. Ask makes the folder with mode 0700 and writes the cassette with mode 0600; a folder that exists keeps its mode. Ignored for `faux`. |
| `TEA_TRACE` | interactive TUI | A file. It receives raw keyboard input, pasted keys included. The TUI warns on screen. Do not set it when you sign in. |
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `XAI_API_KEY`, `ASK_ALIBABA_TOKEN_PLAN_API_KEY` (or `ALIBABA_TOKEN_PLAN_API_KEY`) | providers | API key when no saved sign-in applies. A saved sign-in whose last refresh has no known result also blocks the key: `ask settings show` shows `saved:<method>` with the problem `refresh result uncertain`, and a request fails with `refresh result uncertain; sign in again`. Run `ask auth login --provider <id> --method <method>` to sign in again, or `ask auth logout --provider <id>` to use the key. |

Ask sets `ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, and `ASK_REASONING_LEVEL` in the environment of the processes that tools start. `ASK_REASONING_LEVEL` is `default` when no thinking level is chosen. Do not set them.

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
