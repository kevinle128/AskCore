# `internal/workspace`

The place where a session works. It owns the path rules of a session cwd: the canonical project root, the gated project files and the pure project trust decision. Every tool and every trust decision uses an explicit cwd from here.

## What belongs here

Implemented: path resolution for tools (`path.go`), the project root and the gated files (`project.go`), and the trust decision (`trust.go`). Planned: the resolvers below.

- Path resolution: `ResolvePath` and `PathVariants` (`path.go`)
- The canonical project root and the gated files: `ProjectRoot` and `GatedFiles` (`project.go`)
- The trust decision rule: `NearestEntry` and `Decide` (`trust.go`)
- Planned: the session cwd and a resolver for each run kind: agent default, subagent, cron (`resolver.go`, `workspace_context.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| File tools | `internal/tools` |
| Settings files and the bytes of `trust.json` | [internal/settings](../settings/README.md) (it receives the trust decision through `NewLayered`) |
| The composition: read the store, decide, load the settings | [internal/app/trust.go](../app/trust.go) |
| Role-based access | `internal/permissions` |

## Path resolution rules

`ResolvePath(cwd, p)` turns the path argument of a tool into a clean absolute path. Every file and search tool calls it.

1. One leading `@` is removed.
2. A `file://` URL becomes a local path. A URL with a host other than `localhost`, or without a path, is an error.
3. `~` and `~/` expand to the home directory.
4. A relative path joins the explicit `cwd`. A relative path with an empty `cwd` is an error.
5. The result is cleaned. The function does not check that the path exists and does not limit the path to `cwd`. Symbolic links are resolved by the callers that need it (`FileState` and `PathLock` in `internal/tools`).

`PathVariants(abs)` returns other spellings of a path that is missing on disk: the shell-unescaped form, a narrow no-break space before `AM` or `PM` (macOS screenshot names), decomposed Unicode (NFD) and a curly apostrophe, alone and combined.
File and search tools try the variants in order and use the first that exists.
`write` creates the given path when no existing variant is found.

## Project root and gated files

`ProjectRoot(cwd, askHome)` returns the canonical cwd: the absolute path with its symbolic links resolved, or the clean absolute path when it does not resolve. It does not search the parent folders for `.ask`. When `<root>/.ask` is the Ask home, the session has no project layer (`hasLayer` is false).

`GatedFiles(root, hasLayer, userHome)` lists the entries that need a trust decision. It uses `Lstat` only and never opens a file:

- in `<root>/.ask/` (only when `hasLayer`): `settings.json`, `SYSTEM.md`, `APPEND_SYSTEM.md`, `mcp.json`, `models.json`, `skills`, `prompts`, `extensions`;
- `.agents/skills` in the root and in each parent folder, except `<user home>/.agents/skills`.

An entry that `Lstat` cannot check counts as gated. `AGENTS.md`, `CLAUDE.md` and other context files are not gated.

## Trust decision

`NearestEntry` finds the saved entry of the root or of its nearest parent folder: a `false` entry on a child beats a `true` entry on a parent. `Decide` applies the first rule that matches:

1. An override (`--approve`, `--no-approve`, or a session choice of the trust box).
2. No gated files: trusted.
3. A corrupt or unsafe trust store: untrusted. The default does not apply.
4. The saved entry.
5. `defaultProjectTrust` `always` or `never`.
6. With a UI, the user must answer (`prompt`); without a UI, untrusted (`no_ui`).

`Decide` never saves a decision. The source words (`flag`, `no_gated_files`, `store_error`, `saved`, `saved_ancestor`, `default_always`, `default_never`, `prompt`, `no_ui`) are wire words; do not change them.

## Main interfaces

- `ResolvePath`, `PathVariants`
- `ProjectRoot`, `GatedFiles`, `NearestEntry`, `Decide`
- Resolver interface (planned)

## File names

`path.go`, `project.go`, `trust.go`; planned: `resolver.go`, `workspace_context.go`

## Imports

- Allowed: standard library, `golang.org/x/text/unicode/norm`, `store`
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader` (adapters wrap the core, never the reverse); `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- The gated project files (`.ask/settings.json`, project skills, `SYSTEM.md`, extensions) load only after the project is trusted.
- Project `AGENTS.md` and `CLAUDE.md` are not gated: they load also in a project that is not trusted. A cloned repository can put instructions there that steer the model. The prompt assembly must handle that surface; see the [hand-off note](../../plans/261103-2158-h6a-settings-trust/plan.md#hand-off-note-for-h6b-ungated-context-files).

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
