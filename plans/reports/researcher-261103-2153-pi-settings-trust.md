# Pi settings and project trust: research for Ask H6a

Date: 2026-11-03. Source: Pi at `/Users/dale/Desktop/workspace/opensources/pi`, commit `4c6fb7cfe` (checked with `git rev-parse`, so the line numbers below hold for that commit). Paths are relative to `packages/coding-agent/` unless stated.

## Outcome

Use Pi's semantics and keep Ask's file mechanics. Pi's settings and trust logic is small and easy to copy: two JSON layers, a deep merge, merge-on-write of only the modified keys, a corrupt file that is never overwritten, a trust gate with an existence check, a nearest-ancestor `trust.json`, and an order of decisions (flag, event, saved entry, default). Pi's file writes are weaker than what Ask already has. Pi writes in place with no temp file and no rename, takes the lock only in some cases, and busy-waits for at most 200 ms. Ask's `AuthStore` and `SettingsStore` pattern (lock sidecar, 0600 temp file, rename, directory sync) is better. Do not port Pi's I/O.

Pi does not fully close the trust gate. It reads project settings before trust, and it writes to the project before trust (a migration). It does not gate context files. It also saves trust implicitly on `/reload`. Ask should not copy these four behaviors. One of them, context files, conflicts with an existing Ask rule and needs a decision from the user (see Unresolved questions).

## 1. Settings

### Files and layering
- Global file: `<agentDir>/settings.json`. Project file: `<cwd>/.pi/settings.json` (`src/core/settings-manager.ts:297-302`). The project file is found only in the cwd, with no ancestor walk.
- The effective settings are `deepMergeSettings(global, project)`, and the project layer wins (`settings-manager.ts:250-254`, built at `:413`).
- Merge rules (`settings-manager.ts:191-212`): plain objects merge recursively. Arrays and scalars from the override replace the base. An `undefined` override is skipped.
  - Changelog `CHANGELOG.md:818` records a fix: "project-level nested provider retry settings replacing unmodified global provider retry settings". The recursive object merge is the fix.
- Array exception, `defaultTools` (`settings-manager.ts:217-247`): if the project list contains only `+name`/`-name` entries, Pi appends it to the inherited list. A list with any plain name replaces the inherited list. A malformed value replaces the inherited value and does not throw (`:227-228`).
- Resource arrays (`packages`, `extensions`, `skills`, `prompts`, `themes`) are not unioned by the merge. The merge replaces them. The union happens at the consumer. `PackageManager.resolve()` reads `getGlobalSettings()` and `getProjectSettings()` separately and resolves both lists, project first, so that project entries win name collisions (`src/core/package-manager.ts:920-960`). Packages are deduplicated with project scope first (`:926-935`). So the statement "Resource lists are combined" (`docs/settings.md:3,158`) is true only through this consumer path. `getSettings().skills` returns only the project list.
- `applyOverrides()` merges runtime overrides, such as a CLI `--use-theme`, on top of the effective settings only. These overrides are never persisted (`settings-manager.ts:636-638`; used at `src/main.ts:680`).
- Global-only keys. The getter reads `globalSettings` directly, so a project file cannot set these keys:
  - `defaultProjectTrust` (`:1100-1103`; test `test/settings-manager.test.ts:293`)
  - `cacheWarming` (`:1022-1026`, because warming costs money)
  - `deviceId` (`:1170-1182`, so that a committed file cannot give every clone the same ID)
  - `httpProxy` (read from `getGlobalSettings()` at `src/main.ts:594,871`)

### Write policy
- Storage interface: `withLock(scope, fn(current) => next|undefined)` (`settings-manager.ts:273-275`). There are two implementations, file and in-memory (`:293-377`).
- Lock: `proper-lockfile`, with 10 attempts and a 20 ms synchronous busy-wait between them, so it gives up after about 200 ms (`:304-329`). When it only reads, Pi takes the lock only if the file exists (`:338-341`). Pi creates the directory only when it writes (`:344-351`). Changelog `:3284` records a fix: ".pi folder created unnecessarily when only reading". Changelog `:3125` records another: "parallel pi processes failing ... immediate lockfile contention on auth.json and settings.json". The retry loop is the fix.
- The write is not atomic. Pi calls `writeFileSync(path, next)` in place, with no temp file, rename, or fsync (`:352`). Trust uses the same method (`src/core/trust-manager.ts:126-136`).
- Queued setters: each setter changes `globalSettings` in memory, marks the field (and nested key) as modified, and calls `save()` (for example `:810-814`, `:918-925`). `save()` takes a snapshot and adds a task to a promise chain, `writeQueue` (`:683-695`, `:736-750`). Errors go to `errors[]` and are not thrown (`:692-694`). `flush()` waits for the queue (`:777-779`). `reload()` also waits for the queue first (`:607-608`).
- Merge-on-write that keeps external edits (`:705-734`): inside the lock, Pi reads the current file again and migrates it. It copies the fresh file and overwrites only the fields that were modified in this session. For nested fields, it overwrites only the modified nested keys. Unknown keys and keys that another process changed stay. Changelog `:3326` describes this. Tests: `test/settings-manager-bug.test.ts:37,74,102,126` and `test/settings-manager.test.ts:29,60,88`.
- A corrupt file is never overwritten. If the file failed to load, `save()` returns before it adds a write to the queue (`:739-741`, and `:757-759` for the project file). Pitfall: the in-memory change stays active for the session, but Pi drops it silently and records no error at that point.
- Project writes when the project is not trusted: `assertProjectTrustedForWrite()` throws both at the call and inside the queued task (`:662-666`, `:686-688`, `:753`, `:770`). Test: `test/settings-manager.test.ts:279`.
- Pitfall: every generic setter writes the global scope (`settings.json`). If a project file sets the same key, `/settings` changes nothing that the user can see, because the project layer still wins at `:737`.

### Migrations on load
`migrateSettings()` (`settings-manager.ts:504-563`) runs on every load and inside every merge-on-write (`:713`):
- `queueMode` becomes `steeringMode`.
- `websockets: bool` becomes `transport`.
- The old `skills: {enableSkillCommands, customDirectories}` object becomes an array.
- `retry.maxDelayMs` becomes `retry.provider.maxRetryDelayMs`.

Pi migrates only in memory. The migrated shape reaches the disk only when some later field is persisted, because the write starts from the migrated `currentFileSettings`.

### Errors and diagnostics
- `tryLoadFromStorage` catches parse errors. The scope then loads as `{}`, and Pi keeps the error with its scope and path (`:491-501`, `:437-463`).
- If `reload()` finds the file corrupt, it keeps the previous settings that loaded correctly (`:609-631`). Test: `test/settings-manager.test.ts:205`.
- `drainErrors()` (`:781-785`) passes the errors to `collectSettingsDiagnostics`, which makes them warnings of the form `Invalid settings file <path>: <msg>` (`src/core/settings-diagnostics.ts:4-9`). Pi deduplicates them because the startup manager and the runtime manager can report the same file (`:15-25`). Changelog `:579` records that these warnings now show in the TUI with the file path.
- Pi validates a value when it reads it, not when it loads the file. Each getter clamps, falls back, or throws:
  - `getCompactionTokenSetting` throws on an invalid value (`:927-953`).
  - `parseTimeoutSetting` throws (`:256-265`).
  - `getTuiMode` falls back to the default (`:1348-1350`).
  - `getFullscreenWheelScrollLines` clamps the value (`:1389-1394`).

### Environment and CLI overrides
- The agent directory comes from `PI_CODING_AGENT_DIR` (`src/config.ts:546,567`).
- `sessionDir` uses this order: `--session-dir`, then `PI_CODING_AGENT_SESSION_DIR`, then settings (`src/main.ts:688-692`).
- Some getters fall back to an environment variable after the settings value:
  - `externalEditor`: setting, then `VISUAL`/`EDITOR`, then the platform default (`settings-manager.ts:1054-1064`).
  - `PI_CLEAR_ON_SHRINK` (`:1318-1324`).
  - `PI_HARDWARE_CURSOR` (`:1468-1470`).
- CLI flags such as `--models`, `--tools`, and `--theme` take precedence at the call site (`main.ts:809`, `:680`). They do not go through the settings layer.

### How consumers read values
Consumers call typed getters, one getter for each key, that apply the default, normalize the value (`normalizePath` for `sessionDir` and `shellPath`, `:797-800`, `:1078-1081`), and return a copy (`getSettings()` uses `structuredClone`, `:566-576`). Scope-aware consumers, such as packages and the config selector, read `getGlobalSettings()` and `getProjectSettings()` directly.

### Test strategy
- Real files in a temp directory for the file storage. `SettingsManager.inMemory()` for unit tests (`:466-471`). Changelog `:2220` records a fix: "inMemory() initial settings being lost after reloads".
- Topics covered: external-edit preservation, the in-memory value winning for the same key, invalid-JSON reporting with the path, project trust on and off, the write refusal when the project is not trusted, the global-only `defaultProjectTrust`, no `.pi` folder after a read only (`test/settings-manager.test.ts:312`), and `defaultTools` layering (`:634-690`).

## 2. Project trust

### What trust protects
`hasTrustRequiringProjectResources(cwd)` (`src/core/trust-manager.ts:186-208`) returns true when one of these exists:
- In `cwd/.pi`: `settings.json`, `mcp.json`, `extensions`, `skills`, `prompts`, `themes`, `SYSTEM.md`, or `APPEND_SYSTEM.md` (`:30-39`).
- A `.agents/skills` directory in the cwd or in any ancestor, except `~/.agents/skills` (`:196-207`). Changelog `:1536` records the fix that ignores global `~/.pi/agent` state when the cwd is `$HOME`.

A bare `.pi` directory needs no decision (`docs/security.md:45`). When this check returns false, the project is trusted implicitly (`src/core/project-trust.ts:50-52`).

Where Pi uses the decision:

| Consumer | Gate |
|---|---|
| Project `settings.json` | `loadFromStorage` returns `{}` when the project is not trusted (`settings-manager.ts:474-476`) |
| `.pi/extensions`, `.pi/skills`, `.pi/prompts`, `.pi/themes`, and ancestor `.agents/skills` | `package-manager.ts:2459-2530` |
| Project package storage and install | `package-manager.ts:1775-1779` |
| `.pi/SYSTEM.md` and `.pi/APPEND_SYSTEM.md` | `resource-loader.ts:1201-1227` (falls back to the global file) |
| `.pi/mcp.json` | `extensions/mcp/config.ts:145-148` (runtime). The `pi mcp` CLI accepts only a saved `true` and ignores flags and the default (`extensions/mcp/cli.ts:200,383`) |
| Extensions | `ctx.isProjectTrusted()` (`core/extensions/runner.ts:922-924`) |

Not protected: context files. `AGENTS.override.md`, `AGENTS.md`, and `CLAUDE.md` load whatever the decision is (`docs/security.md:57`). The first release said trust covered "instructions" (`CHANGELOG.md:1596,1604`). The example extension still says so (`examples/extensions/project-trust.ts:12`), so the docs drifted.

What a trusted project can set: project `settings.json` can set `shellPath`, `shellCommandPrefix`, `npmCommand`, `packages`, and `extensions`, because these keys are not global-only (`settings-manager.ts:149-160`). Test `test/package-command-paths.test.ts:334-370` sets `npmCommand` from project settings. So trust lets the project run code.

### Order of decisions
`resolveProjectTrusted` (`src/core/project-trust.ts:46-96`) uses this order:
1. `--approve`/`-a` or `--no-approve`/`-na` (`:47-49`; parsed at `src/cli/args.ts:232-235`).
2. No trust-requiring resources: trusted (`:50-52`).
3. `project_trust` event. Only user-level extensions and CLI `-e` extensions can handle it, because Pi loads them first with project trust forced to false (`resource-loader.ts:497-517`). The first handler that returns `yes` or `no` decides. `undecided` passes to the next handler. A handler that throws is recorded and skipped (`core/extensions/runner.ts:295-322`). `remember: true` saves the decision (`project-trust.ts:63-68`). Types: `core/extensions/types.ts:677-694`.
4. Saved nearest-ancestor entry in `trust.json` (`:72-75`).
5. `defaultProjectTrust`, read from the global settings only: `always` trusts, `never` does not trust, and `ask` goes to step 6 (`:77-84`).
6. No UI: not trusted (`:86-88`). With a UI: a prompt, where cancel means not trusted (`:90-95`).

### Paths, nearest ancestor, and storage
- The key is `canonicalizePath(resolvePath(cwd))`, which is `realpathSync`. If `realpathSync` fails, Pi uses the raw path (`trust-manager.ts:41-43`; `src/utils/paths.ts:28-34`).
- `findNearestTrustEntry` walks from the cwd up to the root. The first `true` or `false` entry wins, so a `false` on a child overrides a `true` on its parent (`trust-manager.ts:45-59`). Test: `test/trust-manager.test.ts:24-37`.
- File: `<agentDir>/trust.json`. It is a flat JSON object that maps a canonical path to `true`, `false`, or `null`. Keys are sorted and the file ends with a newline (`:126-136`). Setting `null` deletes the key (`:236-241`).
- A read that is not valid throws an error and does not return an empty store (`:99-124`). Reads and writes both take the lock `trust.json.lock` (`:138-177`, `:221-245`).
- "Trust parent folder" writes `parent: true` and deletes the child entry (`:72-83`).

### Prompt UX
- Text: "Trust project folder? <cwd> This allows Pi to load .pi settings and resources, install missing project packages, and execute project extensions" (`project-trust.ts:24-26`).
- Options: Trust, Trust parent folder, Trust (this session only), Do not trust, and Do not trust (this session only) (`trust-manager.ts:67-97`). The session-only options save nothing.
- `/trust` opens a selector that shows the saved entry and whether it is inherited (`modes/interactive/components/trust-selector.ts`). A change applies only after a restart (`interactive-mode.ts:5250-5274`).
- When the project is not trusted, the TUI shows a warning (`interactive-mode.ts:4157-4170`).

### Non-interactive modes
- In print, JSON, or RPC mode, `createProjectTrustContext` returns `undefined` or `false` for every UI call (`src/cli/project-trust.ts:18-52`), and `hasUI` is false only at startup (`main.ts:770`). With no flag, no event decision, and no saved entry, `always` trusts and `ask` or `never` does not trust (`docs/security.md:75-82`).
- `pi update` uses only the saved decision or a flag. It never prompts and never asks extensions (`package-manager-cli.ts:753-757`; test `test/package-command-paths.test.ts:334`; changelog `:1536`).

### Reads and writes before the trust decision
1. `startupSettingsManager = SettingsManager.create(cwd, agentDir)` (`main.ts:669`) has no `projectTrusted: false`, so it loads project settings. Pi then uses that manager for `sessionDir` (`:692`), session selection (`:693`), first-time setup (`:675`), the missing-cwd prompt (`:697`), and the trust prompt's UI settings (`:769`). `docs/security.md:31` admits the `sessionDir` case. The Ask roadmap already says do not rebuild it (`AskCore/plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:328`).
2. A write before trust: `runMigrations(cwd)` (`main.ts:666`) calls `migrateExtensionSystem`, which renames project `.pi/commands` to `prompts` before any trust decision (`src/migrations.ts:257-263`).
3. Pi reads the global settings before trust. `httpProxy` and `defaultProjectTrust` come from this read, which is safe (`main.ts:593-594`).
4. User-level extensions run before trust so that they can decide. This is by design (`resource-loader.ts:497-503`).

### Failure modes and fixes
- Implicit trust saved on `/reload` (`interactive-mode.ts:5225-5248`; changelog `:1614`): if a session started with no trust-requiring resources and such resources appear later, the reload saves `trusted: true`. The agent itself can create `.pi/settings.json`, so the model can trust itself.
- A runtime cwd switch, such as `--session` for another project, decides again for each cwd. Pi caches the decision in `projectTrustByCwd` (`main.ts:724-776`), but a prompt is possible only for the first runtime (`:768-770`).
- Fixed bugs from the changelog:
  - Parent traversal was not portable (`:1615`).
  - The `$HOME` global state was taken as project state (`:1536`).
  - The subagent example prompted again in trusted repositories (`:580`; regression test `test/suite/regressions/8261-subagent-project-trust.test.ts`).
- Lock failure on a busy trust store: after 10 attempts, `acquireTrustLockSync` throws (`trust-manager.ts:145-167`). The `/reload` save catches the error and shows a warning (`interactive-mode.ts:5240-5245`).

## 3. Edge cases and pitfalls Pi hit

| Pitfall | Evidence | Lesson |
|---|---|---|
| A nested project object replaced the whole global object | `CHANGELOG.md:818` | Use a recursive merge for objects |
| A save overwrote external edits | `:3326`; `settings-manager-bug.test.ts` | Merge-on-write with the modified fields only |
| Lock contention between parallel processes | `:3125` | Retry the lock and do not fail at once |
| A read created `.pi/` | `:3284`; `settings-manager.test.ts:312` | A read must not create files |
| Invalid settings were hard to see | `:579` | Show diagnostics with the path |
| `/reload` ignored changes to the global settings | `:3558` | Reload both scopes |
| `inMemory` settings were lost on reload | `:2220` | The test storage must keep its state |
| `pi config` treated `~/.agents/skills` as a project directory under `$HOME` | `:3100`, `:1536` | Exclude the user directory from the project walk |
| Trust prompt during `pi update` | `:1536` | Commands without a UI use only saved decisions |
| A `sessionDir` read before trust | `docs/security.md:31` | Decide trust before the first project read |
| Pre-trust `.pi/commands` rename | `migrations.ts:257-263` | No project write before trust |
| Global setter vs project override | `settings-manager.ts:737` | Show which layer owns the value |
| Silent drop when the file is corrupt | `:739-741` | Report the skipped persist |

## 4. Recommendations for Ask (ranked)

Ask facts used here: `internal/settings` has `SettingsStore`, which uses the `settings.json.lock` sidecar, reads fresh data, keeps unknown keys, writes a 0600 temp file, renames it, and syncs the directory. It refuses to overwrite a file that is not an object (`AskCore/internal/settings/README.md:55-62`). The trust store belongs to `internal/workspace` (`AskCore/internal/workspace/README.md:3,11,19`). D3 forbids tool-approval popups (`roadmap.md:53`).

### Trade-off matrix: write mechanics

| Option | Safety on crash | Concurrency | Complexity | Fit |
|---|---|---|---|---|
| A. Keep Ask's lock, temp file, rename, and directory sync, and add Pi's merge semantics | High | flock with a 5 s timeout (`lock.go:11`) | Low, because it exists already | Best |
| B. Port Pi's in-place write with a busy-wait lock | Low (a torn file is possible) | 200 ms, then an error | Low | Weaker than the current Ask code |
| C. SQLite settings | High | High | High | Rejected by D2 |

Rank: A, then B, then C.

### Copy (same semantics)
1. Two layers: `~/.ask/settings.json` and `<cwd>/.ask/settings.json`. Deep merge for objects, and replace for scalars and arrays (`settings-manager.ts:191-212`).
2. Union resource lists in the consumer and not in the merge. Keep `Global()` and `Project()` accessors for scope-aware code (`package-manager.ts:920-960`).
3. A write changes only the keys that the user modified, inside the lock and after a fresh read (`:705-734`). Ask already keeps unknown keys. Generalize this to nested keys.
4. Never overwrite a file that failed to parse, and record the skipped persist as an error. Pi drops it silently; Ask should not.
5. Refuse a project write when the project is not trusted. Check at the call and inside the queued task.
6. Load errors become diagnostics with scope and path. Deduplicate them. A failed reload keeps the last settings that loaded correctly.
7. Global-only keys: `defaultProjectTrust`, and any key that affects cost, identity, or network (proxy). Enforce this in the getter that reads the global layer, and test it.
8. Typed getters with the default and validation at read time. An unknown value falls back to the default. A numeric value that is not valid gives an error.
9. The trust store: `~/.ask/trust.json`, a flat map from canonical path to `true` or `false`, with the nearest ancestor winning and `null` deleting the key. A file that is not valid is an error and not an empty store. Use Ask's lock and atomic replace, not Pi's.
10. The existence check (`hasTrustRequiringProjectResources`): with no gated resources, the project is trusted implicitly. Without this check, every directory would need a decision.
11. Decision order: flag, then a saved entry, then `defaultProjectTrust` (global only), then the mode-specific fallback. The extension event step waits for H11 (extensions). Leave a no-op hook for it.
12. `--approve` and `--no-approve` for `ask -p`, `ask acp`, and the leader.

### Adapt
- Lock: keep Ask's flock sidecar with a context deadline instead of Pi's synchronous busy-wait.
- Canonical path: Go `filepath.EvalSymlinks(filepath.Abs(cwd))`. Decide the behavior when the path does not exist; Pi uses the raw path. On macOS case-insensitive APFS, `EvalSymlinks` does not normalize case. This is not verified in Pi.
- Prompt vs D3: the trust prompt is a gate at startup or on a cwd change, not a tool approval. Pi already resolves trust without a prompt when there is no UI. See Unresolved questions.
- The leader and ACP: Pi decides trust for each runtime cwd and caches it (`main.ts:724-776`). Ask's leader serves many clients and many cwds, so it needs the same per-cwd cache, and the cache must be cleared when `trust.json` changes.
- Queued setters: Pi needs a promise chain because its setters are synchronous. In Go, use a synchronous `Update(ctx, fn)` under the lock, as `ModelCache.Update` does. Ask needs no write queue.

### Skip
- Any read of project settings before trust, including `sessionDir`. Decide trust before the first project read (`roadmap.md:328`).
- Any write to the project before trust, as Pi's `.pi/commands` migration does.
- Implicit trust saved on `/reload` (`interactive-mode.ts:5225-5248`). The agent can create `.ask/settings.json` itself, so this lets the model trust itself.
- The "this session only" options while D3 holds.
- In-place `writeFileSync` and the 200 ms busy-wait lock.
- Pi's legacy migrations (`queueMode`, `websockets`, and so on). Ask has no legacy format. Add a migration hook only when the first rename of a key happens.

## Limitations
- I did not use GitNexus. The GitNexus MCP tools were not in this subagent's tool list, so all tracing used `rg` and Read on commit `4c6fb7cfe`. I did not check call-graph completeness, for example every reader of `getSettings()` for an array key.
- I did not run Pi's code or tests. All behavior comes from reading the source.
- I did not read `system-prompt.ts`, `skills.ts`, or how context files load beyond the gates cited above. They belong to the H6 prompt work, not H6a.
- I did not check Windows path behavior.

## Unresolved questions
1. Context files: Pi loads project `AGENTS.md` and `CLAUDE.md` whether or not the project is trusted (`docs/security.md:57`). Ask's rule says project `AGENTS.md` loads only after trust (`AskCore/internal/workspace/README.md:55`). Should Ask keep its stricter rule, which diverges from Pi, or follow Pi? The user must decide.
2. Trust prompt under D3: with `defaultProjectTrust: "ask"`, should Ask's TUI show a startup trust prompt (as Pi does), or only accept flags, `trust.json`, and a `/trust` command, so that an unknown project with gated resources is not trusted?
3. Leader scope: is the trust decision made by each client at connect time, or by the leader for each session cwd? This affects where `--approve` goes on the wire.
4. Not verified: Pi's `ProjectTrustStore.get` throws when `trust.json` is not valid (`trust-manager.ts:109`). I found no catch on the startup path at `main.ts:747`. Pi may then fail at startup instead of falling back to untrusted. Ask must choose: fail closed with a diagnostic (recommended), or stop.
5. Should the existence check include `.ask/AGENTS.md` and other files that Ask plans to gate, so that the gated list and the check stay the same? Pi's list (`trust-manager.ts:30-39`) is the single source for both the check and the docs. Ask should keep one list too.

Status: DONE_WITH_CONCERNS
Summary: I traced Pi's settings layering, write policy, and trust gate with file:line evidence, and I ranked what Ask should copy, adapt, and skip. The concerns: I could not use GitNexus, and the context-file and trust-prompt decisions need the user.
