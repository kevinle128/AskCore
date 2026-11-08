# DeepSeek harness: config layering and project trust (input for Ask H6a)

Source: `/Users/dale/Desktop/workspace/opensources/deepseek-harness` at commit `5badb15`. All paths below are relative to that checkout unless they start with `internal/` (Ask). Date: 2026-11-03.

## Bottom line

1. DeepSeek has **no project config layer and no project trust gate**. All configuration lives in `$DSH_HOME` (profile patch + home patch + `--patch` overlays). The project contributes only a `.env`, `AGENTS.md`/`CLAUDE.md` instructions, and skill folders. The project is "trusted by default, without a prompt, with no stored trust record" — this is an explicit, recorded product decision, and a project-trust gate is named as deferred work.
2. Its safety model replaces a trust gate with two mechanisms: (a) a **deny-by-namespace list** of variables a discovered `.env` can never set (permission mode, `HOME`, `PATH`, proxies, base URLs, interpreter hooks), which fails the launch loudly; (b) **lowered authority** of accepted project input (instructions arrive as a user-role message, escaped, budgeted).
3. The write path is good and worth copying in part: lock, validate the full candidate before disk, refuse a write that a higher layer would shadow, atomic rename, reconcile, and roll back on reconcile failure. It is **weaker than Ask's AuthStore** on durability (no fsync).
4. Ask's own docs already decide the opposite trust stance (project `.ask/settings.json`, skills, `AGENTS.md` load only after trust). D25 does not cover H6a. So the trust stance is a **user decision**, not a copy target. The denylist and the writer discipline are safe to adopt under either stance.

## 1. Config

### Layers and precedence

Composition is a Cordis entry list built from patch layers applied to an empty root (`docs/architecture.md:27`):

```text
bundle layers (profile's bundle list order, e.g. dsh-base then dsh-headless)
  < $DSH_HOME/profiles/<name>/cordis.patch.yml      (profile user layer; the only write target)
  < $DSH_HOME/cordis.patch.yml                      (home layer; outranks the profile layer)
  < --patch <file> overlays (argv order)
  < telemetry switch
```

- Evidence: `apps/cli/src/profile-boot.ts:184-209` (compose order; "home-level ... outranks the per-profile layer", 188-189); `packages/boot/app-boot/README.md:61` (tweak layer semantics).
- A patch replaces the **whole** `config` of a row by id; there is no deep merge (`packages/boot/app-boot/README.md:204`; `packages/bundle/base/cordis.patch.yml:6-10`).
- Non-secret value ordering across all sources (still current for env and credentials; the `settings.yaml` tier was later removed): `.agents/notes/implemented/architecture/2026-08-04-configuration-source-ownership.md` "Decision": explicit run > settings > composition > shell env > discovered `.env` > defaults. The settings tier was superseded by "profile-owned live configuration" (`.agents/notes/implemented/architecture/2026-09-19-profile-owned-live-configuration.md`), so today "settings" are just volatile fields stored in the profile patch.
- Credentials have a separate, narrower ordering: inherited process env (read-only, wins) > `$DSH_HOME/.credentials.yaml` (writable) > `<cwd>/.env` > `$DSH_HOME/.env` (`packages/credentials/credentials-local/README.md:80`; same note, "Credentials keep a narrower, separate ordering"). Configuration carries only a *reference* (an env-var name such as `apiKeyEnv: DEEPSEEK_API_KEY`, `packages/bundle/base/cordis.patch.yml:478-481`); providers resolve the value per request (`docs/capability-seams.md:608`).

### Schema, validation, defaults

- Every plugin declares a typed `Config` with a schemastery schema; defaults live in the schema (example: `packages/context/agent-instructions/src/config.ts:11-13,41-45`). The doc catalog is generated and cross-checked against the runtime schema (`docs/config-catalog.md:1-8`).
- Repo rule: no hardcoded tunables; "Misconfiguration fails loud at load" (`AGENTS.md:141-142`). Validation happens at parser/config, file, wire boundaries, not at typed in-process calls (`AGENTS.md:144`).
- Startup and reload failure matrix: a missing/malformed root or required overlay stops startup; schema failure on an optional entry warns and continues; required entries stop startup (`packages/boot/app-boot/README.md:92-109`).
- A present but unreadable/unparsable/non-array patch file throws; only a missing file means "no layer" (`packages/boot/app-boot/src/index.ts:305-325`). An empty or comment-only patch fails boot; `[]` disables the layer (`packages/boot/app-boot/README.md:61`).

### Write policy

Owner: `ConfigEditor.edit` (`packages/boot/config-editor/src/index.ts:87-154`).

| Concern | Behavior | Evidence |
|---|---|---|
| Serialization | Runs inside `hmr.runExclusive` when HMR exists, so writes and file-watch reloads share one queue | `config-editor/src/index.ts:152-153`; `packages/boot/hmr/README.md:57` |
| Cross-process lock | `withFileLock` on `<profile>/package.json` (one lock for all profile operations, not the patch file itself) | `config-editor/src/index.ts:93` |
| Fresh read | Reconciles external changes first, re-checks the entry still exists | `config-editor/src/index.ts:94-97` |
| Validate before disk | Runs the plugin's config resolver on the full candidate | `config-editor/src/index.ts:103-104` |
| Preserve unknown content | Parses YAML as a document; replaces only the target row's `config`; comments and `!!js` elsewhere survive | `config-editor/src/index.ts:111-134`; `packages/boot/config-editor/README.md:41` |
| Reset | Equal-to-inherited value removes the override row | `config-editor/src/index.ts:120-126` |
| Shadow check | Recomposes with home + `--patch` layers; refuses if the effective value would differ | `config-editor/src/index.ts:136-141` |
| Atomic commit | Temp file `wx` 0600 + rename | `config-editor/src/index.ts:142`; `packages/util/atomic-write/src/index.ts:79-89` |
| Rollback | Reconcile failure rewrites the prior bytes and reconciles the prior patches | `config-editor/src/index.ts:143-148` |
| Durability | **No fsync** (open TODO) | `packages/util/atomic-write/src/index.ts:74,84` |
| Stale lock | Lock file records PID; a contender takes over only when the PID is gone (`ESRCH`), with a claim file to admit one taker | `packages/util/atomic-write/src/index.ts:140-178,212-235` |

Two different "unknown key" policies exist and must not be merged:

- Patch file: unknown content is preserved (above).
- Credentials file: unknown top-level keys, record fields, and tags are **rejected** at boot; on live reload the last good snapshot keeps serving with a warning (`packages/credentials/credentials-local/README.md:109,121,139`). Writes patch lines and keep comments under the same writer lock (`README.md:138`). A file readable by group/other is refused on POSIX (`README.md:115`). Same-reference concurrent writes are last-write-wins, no revision check (`README.md:198`).

Corrupt profile patch recovery: `sanitizeProfile` renames the patch to `.bak-<timestamp>` and restores the bundle list; only after the profile is stopped (`packages/boot/app-boot/README.md:69`).

Legacy migration: old `$DSH_HOME/settings.yaml` imported once into the profile; renamed to `settings.yaml.imported` before the first write; rejected sections stay in the renamed file (`packages/settings/settings/README.md:34`; test `packages/settings/settings/tests/configuration.spec.ts:315`).

### Hot reload

- HMR watches the profile `package.json` and both user patch files (`packages/boot/hmr/src/index.ts:328-349`). Enabled only in the web profile; headless, ACP, SDK disable it (`docs/architecture.md:29`; `packages/bundle/headless/cordis.patch.yml:33-34`).
- Invalid live patch: rejected, running config unchanged; next valid edit applies (`packages/boot/app-boot/README.md:96`; tests `packages/boot/app-boot/tests/config-reload.spec.ts:63,232`).
- Fields marked `.volatile()` update in place (no remount); consumers hold a reference and read it per operation; ordinary fields remount (`.agents/notes/implemented/architecture/2026-09-18-volatile-config-references.md` "Decision").
- Credentials file has its own watcher; reload replaces the snapshot wholesale (`credentials-local/README.md:46,155`).

### Env overrides

- `loadLayeredEnv` reads `<cwd>/.env` and `$DSH_HOME/.env` once at launch; inherited env wins; both files are parsed and checked before either is applied (`packages/boot/app-boot/src/index.ts:235-256`).
- Some config rows read env directly, e.g. `DSH_PERMISSION_MODE` (`packages/bundle/base/cordis.patch.yml:232,248`). Because `DSH_*` is denied in any `.env`, only the launching shell can set it.
- The snapshot keeps the source of each value (`process`, `project-env`, `user-env`) (`app-boot/src/index.ts:251-255`; `packages/util/launch-environment/tests/launch-environment.spec.ts:32`).

### Diagnostics

- `dsh --profile web --dump-config`: the composed tree with per-source comments and the patch layers that changed each row; unmatched patches reported (`docs/architecture.md:33-39`; `app-boot/README.md:73`).
- `--dump-config-schema`: JSON Schema of the composition; imports plugin modules, so "run it only against a profile whose plugins you already trust" (`app-boot/README.md:54,79-81`).
- Startup error lists failed plugins, required marks, nested causes; full diagnostics saved (`app-boot/README.md:92`).

### How consumers receive typed config

Each plugin receives its own validated `Config` from its row; there is no global settings object. Live fields are `Volatile<T>` references (example `packages/core/agent-default-model/src/index.ts:24`, shown in `docs/config-catalog.md:44-53`). `ctx.settings` only projects volatile fields into forms and delegates writes to `ctx.configEditor` (`docs/capability-seams.md:605-606`). Read API redacts secrets (`docs/capability-seams.md:596`; `packages/settings/settings/src/redact.ts`).

### Test strategy

Real profile on disk, real Loader, real consumer:

- `packages/settings/settings/tests/configuration.spec.ts`: persist + live consumer update + restart (9), redaction and invalid edits (22), refuse edit shadowed by home layer (39), concurrent writes and stale revisions after external change (48), preserve `!!js` (63), restore patch when activation fails after validation (151), legacy import (315).
- `packages/boot/app-boot/tests/user-patches.spec.ts:41-145`: missing = no layer; unreadable, unparsable, non-array all fail loud.
- `packages/boot/app-boot/tests/config-reload.spec.ts:63,232`: last good tree kept on invalid edit.
- `packages/util/atomic-write/tests/atomic-write.spec.ts:124-455`: mode narrowing, symlink target replaced not followed, lock takeover races, PID reuse.
- `packages/boot/app-boot/tests/app-boot.spec.ts:100-353`: `.env` denylist incl. lowercase `https_proxy` (144-154), home-only proxy exemption (167-232).

## 2. Trust and workspace safety

### Project-local inputs that change behavior

| Input | Loaded by default? | Gate |
|---|---|---|
| `<cwd>/.env` | Yes, at launch | Bootstrap denylist; launch fails if hit (`app-boot/src/index.ts:133-157,190-222`) |
| `AGENTS.md`, `CLAUDE.md`, `*.local.md` from `.git` root down to cwd | Yes (`base/cordis.patch.yml:289-292`) | None. Delivered as one user-role baseline message, budget `maxBytes`, `</system-reminder>` escaped; README calls it lower-authority guidance (`packages/context/agent-instructions/README.md:32,105,130,217`). Symlinks are followed off-tree (217) |
| `.dsh/skills`, `.agents/skills` under project root | Yes (`base/cordis.patch.yml:297-298`) | None (`packages/skill/skill-filesystem/README.md:50-56`) |
| Hooks (`hooks.json`) | No; plugin not in base | One process-level `configPath` set in a user patch; no project discovery (`docs/config-catalog.md` hooks-claude-code `configPath` TODO; `packages/hooks/hooks-claude-code/README.md:181`) |
| MCP servers | No | Each server is a row in a user patch; no `.mcp.json` discovery (grep found none in `packages/`, `apps/`) |
| Project config file | Does not exist | n/a |

### The denylist (the real gate)

`BOOTSTRAP_NAMES` and `BOOTSTRAP_PREFIXES` (`packages/boot/app-boot/src/index.ts:133-157`) cover: process launch (`PATH`, `HOME`, `SHELL`, `NODE_OPTIONS`, `LD_*`), interpreter start hooks (`BASH_ENV`, `PYTHONSTARTUP`, ...), VCS hooks and editors, network reach and trust (`DEEPSEEK_BASE_URL`, proxies, CA bundles, `NODE_TLS_REJECT_UNAUTHORIZED`), and the whole `DSH_`, `XDG_`, `DYLD_`, `BASH_FUNC_` prefixes. Case-insensitive (`:176`). Proxy names are allowed only from `$DSH_HOME/.env` (`:166,209-210`). No opt-out by design ("anything a discovered file could set is the hole itself", config-source-ownership note).

Rationale stated in the note: the project's code may run under the agent's policy, but the project may not rewrite that policy (`DSH_PERMISSION_MODE`), relocate instructions, or redirect where the user's key is sent.

### Decision storage

- Project trust: **none**. "Trusted by default, with no prompt and no stored trust record"; withholding routing/credentials until trust was "Rejected as the product's stance"; "a later project-trust gate is where that gets addressed" (config-source-ownership note, "Decision" and "Alternatives").
- Approvals: one-shot only (`allowed-once`); no allow-always, no grant store (`packages/interaction/user-approval/README.md:154`).
- The only persisted admission decision is plugin version exemptions in `<profile>/compatibility.json`; malformed file → valid records still apply, file becomes read-only, grants refused (`app-boot/README.md:56`).

### Non-interactive behavior

- Base: `approval.policy = ask` unless `DSH_PERMISSION_MODE=danger-full-access`, which maps to `never` (`base/cordis.patch.yml:245-248`). Headless, ACP, SDK bundles add no approval row, so they inherit this.
- `ask` with no answerer → `unavailable` → the action fails closed (`user-approval/README.md:12,77`). `never` **rejects** every approval request without prompting; it is not "allow all" — under `danger-full-access` the sandbox already allows the action, so no request is raised (`user-approval/README.md:36,121`).
- Sandbox default `workspace-write`, root `process.cwd()` (`base/cordis.patch.yml:229-233`).

### Failure modes and edge cases pinned by tests

- Project `.env` sets a denied name → launch fails with a remedy message (`app-boot.spec.ts:144-154,208-215`).
- Lowercase variant bypass blocked (`app-boot.spec.ts:145`).
- Instruction root discovery stops on permission/IO error instead of picking an ancestor (`agent-instructions/README.md:38`).
- Credentials file readable by others → refused at boot (`credentials-local/README.md:115,121`).
- Known residual risk, stated by the authors: a cloned repo `.env` carrying another key routes that session through it (config-source-ownership note, "Alternatives"); project vars reach child processes because layers are materialized into `process.env` ("Consequences").

## 3. Differences versus a Pi-style `settings.json` + `trust.json`

| Axis | Pi-style | DeepSeek |
|---|---|---|
| Project config file | `.pi/settings.json` deep-merged over user | None |
| Precedence direction | Project over user | Home over profile; overlays over both |
| Merge | Deep merge of keys | Whole-row replace by id |
| Trust | Stored decision in `trust.json`, gates project files | No stored decision; deny-by-namespace + lowered authority |
| Instructions | Usually gated with project files | Always loaded, as user-role message |
| Live edits | Rewrite file | Volatile fields update in place; shadowed writes refused |
| Secrets | In settings/auth file | References in config; values in a separate strict store |

## 4. What Ask should copy, adapt, or skip

Context checked: `internal/settings/README.md` (AuthStore: flock, fsync, dir-sync, revision CAS, keeps unknown fields; project `.ask/settings.json` "loads only after the project is trusted"), `internal/workspace/README.md:3,11,55` (planned `trust.go`; project settings, skills, `AGENTS.md` load only after trust).

**Decision for the user (not a copy target).** Ask's written design and DeepSeek's recorded stance conflict on trust-by-default. D25 covers lifecycle only. Options:
- (A) Keep Ask's design: trust gate for `.ask/settings.json`, project skills, project `AGENTS.md`. Under D3 it must be non-interactive in M1: e.g. `ask trust <dir>` / `ask trust list|revoke` plus a flag or env for CI. DeepSeek gives no model for this.
- (B) Follow DeepSeek: no gate, rely on the denylist below. Simpler, no M1 UI, but a cloned repo can change behavior the user did not approve.
- (C) Hybrid: gate only `.ask/settings.json` (it can change behavior); load `AGENTS.md` always, as lower-authority input. Skills need a separate judgment inside C: unlike instructions they are directories that can carry scripts and steer tool execution, and Ask's `internal/workspace/README.md:55` lists them as gated. DeepSeek loads them ungated (`skill-filesystem/README.md:50-56`). My ranking: **C, then A, then B**. C matches D3 (nothing to prompt for instructions), keeps the gate where it protects real settings, and matches how DeepSeek frames instructions. This ranking is advisory.

**Copy (fits any option):**
1. A denylist of keys that project `.ask/settings.json` (and a project `.env`, if Ask reads one) can never set, regardless of trust: permission/approval mode, `ASK_HOME`, provider base URLs and auth headers, hook commands, MCP command lines, proxies/CA. Fail loud with a remedy. Deny by prefix (`ASK_*`), not an allowlist. Evidence of value: `app-boot/src/index.ts:133-222` and its tests.
2. Writer discipline for `settings.json`: validate the complete candidate before disk; refuse a write that a higher layer would shadow; roll back if apply fails (`config-editor/src/index.ts:103-148`). Reuse AuthStore's lock + fsync + rename; do not adopt DeepSeek's weaker no-fsync write.
3. Diagnostics: an `ask settings show --sources` style dump that names the file each effective value came from (`--dump-config` model).
4. Credentials as references: settings name a credential, AuthStore holds the value (already Ask's split; keep it).
5. Corrupt-file stance per file: settings → fail loud with path at startup; on any live reload keep last good. Pick one unknown-key policy per file and write it down (Ask keeps unknown fields in `auth.json`; keep that for `settings.json` too so older/newer builds do not drop each other's keys).

**Adapt:**
- Instructions as a user-role, escaped, byte-budgeted message (`agent-instructions/README.md:105,130`). Orthogonal to trust; useful in options A–C.
- Volatile vs restart-required fields: mark which settings apply live (model, thinking fold) and which need a new session. Ask has no HMR; a read-per-operation accessor is enough.

**Skip:**
- Cordis patch layering, `!!js` expressions, profiles/bundles, whole-row replace: tied to a plugin loader Ask does not have. Ask's two-file merge is the stated design.
- PID-record lock takeover: Ask's `flock` releases on process death (`internal/settings/lock_unix.go:13-20`).
- File-watch hot reload: Ask leader reads on demand; watching adds lifecycle cost with no M1 need.

## Limitations

- `packages/credentials/credentials-local/src/index.ts` was blocked by a local privacy hook. Evidence for that store is its README plus a symbol grep run before the block (lines 6-14, 179-226). Not re-read by other means.
- `mcp__gitnexus__list_repos` was not available in this session; source was read directly as the task allowed. DeepWiki was not consulted.
- The Pi column in section 3 comes from the task's framing and general knowledge, not a reading of Pi source in this session.
- Web-profile UI flows (settings forms, plugin manager risk confirmation) were not traced; they are outside H6a.

## Unresolved questions

1. Trust stance for H6a: option A, B, or C above? Ask's READMEs currently say A. Under C, are project skills gated with settings or loaded with instructions?
2. Does Ask read a project `.env` at all? If yes, adopt the denylist there too.
3. Unknown-key policy for `settings.json`: keep (as `auth.json`) or reject (as DeepSeek credentials)?
4. Which settings keys are live vs restart-required in M1?
5. Under option A/C, what is the non-interactive trust command and does `ask -p` / `ask acp` refuse or ignore an untrusted `.ask/settings.json`?

Status: DONE_WITH_CONCERNS
