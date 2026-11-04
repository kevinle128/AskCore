# `internal/skills`

Skill discovery. The loader finds skill folders with a `SKILL.md` file and returns their metadata (name and description). Only metadata goes into the skill list. The full skill text is read when the agent needs it. The loader runs once per session.

## What belongs here

- `Load`: walks the skill roots it is given (precedence order, depth 5, symbolic links followed, loops broken), reads the frontmatter of each `SKILL.md`, and keeps the first skill of each name
- `Diagnostic`: one fixed-text reason per skill that did not load or lost to an earlier name
- `Result.Listing`: the skill catalog as one system reminder, capped at 16 KiB

## What does not belong here

| Code | Put it in |
|---|---|
| Which folders are roots, and the project trust gate | `internal/app`, from the `workspace` trust decision |
| File watcher, `/reload` | Not built |
| Skill body cap, `.gitignore` handling | Not built |
| BM25 and embedding search | Parked |
| The tools that read a skill | `internal/tools` (the model uses `read`) |

## Main interfaces

- `Load(roots []Root) Result`
- `Result.Listing() string`

## File names

`loader*.go`

## Imports

- Allowed: `pkg/protocol`, `gopkg.in/yaml.v3`, the standard library (enforced by the `skills-boundary` depguard rule)
- Denied: everything else, in particular `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- Frontmatter is read from the first 4096 bytes. `disable-model-invocation` must be the YAML boolean `true` or `false`; `yes` is an error.
- Every untrusted string in the listing goes through `protocol.ReminderText.Quote`.
- A diagnostic holds a path and a fixed text, never file content.
- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
