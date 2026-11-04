# H6b: system prompt sections, context files, skills, tool selection

Date: 2026-11-04. Status: design agreed, implementation in waves. Inputs: [xia report](../reports/xia-261104-h6b-prompt-context-skills-tools.md) (decisions section), three architect sketches (one structure, merged below).

## Goal

`ask` builds the system prompt from named sections, reads `SYSTEM.md` and `APPEND_SYSTEM.md`, delivers `AGENTS.md`/`CLAUDE.md` and a skill list as tagged user-role history items, selects tools with `defaultTools` and `--tools`, and carries the per-session flags to TUI and leader sessions through ACP `_meta`. A project that is not trusted still loads `AGENTS.md` (H6a decision 1) but never its `.ask/` files or skills.

## Decisions (user, 2026-11-04)

D-a/D-h/D-d: context files and skill list are user-role seed items. D-b: cwd and ISO date are the last system sections. D-c: 1 MiB per file, 64 KiB total. D-e: flags reach TUI and leader through ACP `_meta`. D-f: Pi first-wins for skill names, with a diagnostic. D-g: project skill dirs come from the existing `workspace` gated list.

## Synthesis choices

| Choice | Picked | Why |
|---|---|---|
| Where sections live | fixed array in `internal/agent/systemprompt_sections.go` | The bootstrap README puts final assembly in `agent`; an array has no runtime registry to misuse. Rejected: new `internal/prompt` leaf (second owner of prompt layout). |
| Escape and frame | `protocol.ReminderText` with `Frame` and `Quote` in `pkg/protocol` | `skills` cannot import `bootstrap`; the frame already lives in `pkg/protocol`. Cannot forget to escape: bodies, paths, notices and skill fields only enter through `Quote`. |
| Seed | `agent.Config.Seed []protocol.CustomMessage`, `CustomType` `context-file` and `skill-list`, applied in `New` and Reset, skipped when the log already holds that type | Survives Reset, idempotent for H8. A seed-only log still counts as empty for `Continue`. |
| Session start | `app.PrepareSession` does all file I/O and precedence; `SessionStart.Render(snap)` is pure and returns prompt and seed; `AgentSpec` replaces positional `NewAgent` args | One path for print, ACP, leader, in-process TUI. `Render` runs after the tool snapshot because the skill list needs `read` or `bash`. |
| Tool selection | `tools.Selection` parse, `Over`, pure `Resolve` before `Register`; `PromptSection` split into `PromptList` and `PromptGuidelines` | No `Unregister` after build (keeps the "no tool change, no system message" baseline). Extras honor `-name` only. |
| Wire | one `_meta` key `ask.dev/session` with `ACPSessionOptions{SystemPrompt, AppendSystemPrompt, Tools, NoTools}`; `--system-prompt` path resolved to text on the client; cap 256 KiB total; result `_meta` ack so the TUI warns when an older leader dropped the flags | A dropped `--tools` would run with more tools than asked. |
| Fail or skip | unreadable `SYSTEM.md`/`APPEND_SYSTEM.md` fails the start; bad context file or skill is skipped with a diagnostic | Prompt files are explicit configuration; context files are ungated repo content. |

Not built: `-nc`/`noContextFiles`, `ask acp` process flags, skill `/reload`, `.gitignore` in the skill walk, `*.local.md`, `{{var}}` interpolation, configurable budgets, skill body cap.

## Data shapes

- `agent.SectionName` constants and `sections` array `{name, order, baseline}`: preamble 100, tools 200, rules 300, docs 400 (baseline sections dropped by a custom prompt), addendum 500, cwd 9000, date 9010. Orders unique, gaps >= 10, pinned by a test. `PromptParts` input; blank custom text ignored.
- `bootstrap.Context{Files, Skipped, Text}` with `SkipReason` enum; `bootstrap.PromptFiles{Custom, CustomFrom, Append}`.
- `skills.Result{Skills, Diagnostics}`, `skills.Root{Dir, Scope}`, `skills.Skill`.
- `tools.Selection` (private fields), `tools.SelectionError{Origin, Name, Known}`.
- `settings.Effective.DefaultTools *[]string` (nil = unset, empty = no built-ins).

## Waves

Each wave ends green: `go build ./...` and `go test` of the touched packages. Workers in one wave own disjoint packages and run only their own package tests.

| Wave | Unit | Owns | Depends on |
|---|---|---|---|
| 0 | W0 protocol | `pkg/protocol`: `ReminderText`, seed CustomType constants, `ACPSessionOptions`, key, caps, README row, round-trip test | none |
| 1 | W1a agent | `internal/agent`: sections array, `SystemPrompt(PromptParts)`, `Config.Seed`, `openSource`, `seedLog`, `Continue` rule, tests incl. two-environment prefix identity | W0 |
| 1 | W1b tools+settings | `internal/tools` selection and prompt split; `internal/settings` `defaultTools` key, merge rule, tests | none |
| 1 | W1c bootstrap | `internal/bootstrap`: root walk, discovery, dedup, budget `fit`, `render`, `LoadPromptFiles`; README | W0 |
| 1 | W1d skills | `internal/skills`: `Load`, frontmatter (yaml.v3 strict bool), `Listing`; README | W0 |
| 1 | W1e wire | `internal/acp` (`flagsFromMeta`, `SessionRequest.Flags`, `InvalidParams`, ack), `internal/tui`, `internal/tuiclient` (`NewSessionOptions`, `SessionInfo.FlagsIgnored`) | W0 |
| 2 | W2 compose | `internal/app` (`PrepareSession`, `SessionStart`, `Render`, `Warnings`, `AgentSpec`, `NewAgent` widening), `cmd/tui` (flags, `newNativeAgentOn`, `settings show`, print path, interactive path) | wave 1 |
| 3 | W3 docs+accept | `docs/cli.md`, READMEs, `CHANGELOG.md`, `docs/reference/tools.md` regeneration, roadmap row, real-entry-path acceptance with the built `ask` | W2 |

## Acceptance (roadmap H6 exit)

On a built `ask`: an `AGENTS.md` and a skill change the reply of a faux-provider run (request body shows the seed items); an untrusted project's `.ask/` config, `SYSTEM.md` and skills are ignored while its `AGENTS.md` still loads; `-t read` and `-nt` change the request tool list and the prompt tools section; `--system-prompt` over the leader and the in-process TUI reaches the request body; the system prompt prefix is byte-identical across two runs that differ in cwd and date; a `</system-reminder>` in a context file, path and skill description cannot close the frame.

## Divergences from Pi

Pi puts context files (`<project_instructions>`) and the skill list (`<available_skills>`) in the system prompt (`packages/coding-agent/test/system-prompt.test.ts:71`, `skills.test.ts:242`). Pi also keeps the first skill of a name and reports a collision (`core/skills.ts:419-436`); Ask keeps that rule and is not different there.

| Topic | Ask | Reason |
|---|---|---|
| Context files | One user-role seed item of the custom type `context-file`, in a `<system-reminder>` frame with escaped bodies and paths | Project `AGENTS.md` is not gated, so a cloned repository controls it. In the system prompt it would have system rank (injection surface). |
| Skill list | One user-role seed item of the custom type `skill-list`, only when `read` or `bash` is kept | Skill names and descriptions come from files, some from a project. The list has no use without a tool that reads a skill. |
| Duplicates | Per folder, a file with the same trimmed text as an earlier file is left out, with no warning | `AGENTS.md` and `CLAUDE.md` often hold the same text. |
| Size | 1 MiB per file, 64 KiB in total, counted after escaping. The broadest files go first, then the most specific file is cut, and the model sees a notice | A bounded prompt: one bad file cannot fill the context. |
| Diagnostics | First-wins skill names and skipped files give fixed texts with a path, never file content; one `Warning:` summary line in `ask -p`; detail in `ask settings show` and in the ACP session diagnostics | A diagnostic must not carry text that the model or the terminal could act on. |
| cwd and date | The last two sections (9000, 9010), ISO date | The prefix stays the same between sessions, so the provider cache holds. |
