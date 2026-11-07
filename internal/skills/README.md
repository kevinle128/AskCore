# `internal/skills`

Skill discovery. The loader finds skill folders with a `SKILL.md` file and returns their metadata (name and description). Only metadata goes into the skill list. The full skill text is read when the agent needs it. The loader runs once per session.

## What belongs here

- `Load`: walks the skill roots it is given (depth 6, at most 2000 folders and 20000 entries per root, hidden folders skipped, symbolic links followed, loops broken), reads the frontmatter of each `SKILL.md`, and keeps every skill. Same-name skills are all kept; the order is scope (project first), name, path
- `Diagnostic`: one fixed-text reason per skill that did not load
- `Budget`, `BudgetFor`: the token or character room of the catalog (the `skillsMaxContextTokens` setting capped at 10000 tokens, else 2% of the model window, else 8000 characters)
- `Result.Listing`: the skill catalog as one system reminder within a `Budget`, with a `Report` (`Total`, `Included`, `Omitted`, `TruncatedDescriptions`) and `Report.Warning`
- `Expand`: turns a prompt that starts with `/skill:<name>` into the skill block (see `protocol.SkillInput`) followed by the arguments. A skill with `disable-model-invocation` expands too. The body is the text after the frontmatter, cut at 100 KB on a rune boundary with a visible note. An unknown name, or a file that cannot be read, leaves the text unchanged

## What does not belong here

| Code | Put it in |
|---|---|
| Which folders are roots, and the project trust gate | `internal/app`, from the `workspace` trust decision |
| File watcher, `/reload` | Not built |
| `.gitignore` handling | Not built |
| BM25 and embedding search | Parked |
| The tools that read a skill | `internal/tools` (the model uses `read`) |

## Main interfaces

- `Load(roots []Root) Result`
- `BudgetFor(contextWindow, maxContextTokens int) Budget`
- `Result.Listing(b Budget) (string, Report)`
- `Expand(text string, r Result) (string, bool)`
- `Report.Warning() string`

## File names

`loader*.go`

## Imports

- Allowed: `pkg/protocol`, `gopkg.in/yaml.v3`, the standard library (enforced by the `skills-boundary` depguard rule)
- Denied: everything else, in particular `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- The name is the frontmatter name with white space collapsed, else the folder name, cut to 64 runes, with no charset rule. The description is required and has no length cap at load; `Listing` cuts it to 1021 runes and `...` above 1024.
- Frontmatter is read from the first 64 KiB. When strict YAML fails, top-level plain values that hold `: ` or start with `[`, `{`, `@` or a backtick are double-quoted and read once more. `disable-model-invocation` must be the YAML boolean `true` or `false`; `yes` is an error.
- `Listing` fits the catalog in three steps (full descriptions, shared descriptions round-robin by rune, names only with the rest left out). The omission line is charged to the budget.
- Every untrusted string in the listing goes through `protocol.ReminderText.Quote`.
- A diagnostic holds a path and a fixed text, never file content.
- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
