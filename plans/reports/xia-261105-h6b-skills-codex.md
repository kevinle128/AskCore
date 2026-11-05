# Xia analysis: skills validation, catalog budget and warnings from Codex (mode: improve)

Date: 2026-11-05. Status: analysis, decisions made by the user ("làm giống codex"), plan in `plans/261104-1500-h6b-prompt-context-skills-tools/plan.md` (Phase 2 section). Extends [xia-261104-h6b-prompt-context-skills-tools.md](./xia-261104-h6b-prompt-context-skills-tools.md).

## Trigger

After PR #8, a real `~/.agents/skills` folder made the TUI show 36 "Settings problems": `description over 1024 bytes` (2) and `bad name` (34, for example `name: kk:agent-browser`). Ask copied Pi (name regex, 1024-byte reject) and Grok (re-key, strict). The user chose to follow Codex instead.

## Source manifest

| Source | Ref | Read through | License |
|---|---|---|---|
| Codex | `695961ee88`, `/Users/dale/Desktop/workspace/opensources/codex`, GitNexus index `codex` | `gitnexus query` and `context`, then direct reads by three scouts; static only, nothing run | Apache-2.0 (behavior ported, no code copied) |
| DeepSeek, Grok | see the earlier xia report | | |
| Ask | branch `feat/h6b-prompt-context-skills-tools` | direct reads | n/a |

Current Codex crates: `codex-rs/skills` (parser), `codex-rs/ext/skills` (loader, render, budget), `codex-rs/config/src/skills_config.rs`. There is no `core-skills` crate at this commit. `CORE_COMPATIBLE` in `ext/skills/src/render.rs:48-52` is a render style kept for parity.

## What Codex does

Paths are relative to `codex-rs/`.

**Validation is lenient** (`skills/src/parser.rs`).
- `name`: whitespace collapsed; missing or empty falls back to the folder name (`ext/skills/src/loader/host.rs:384-394`); only a 64-char cap (`parser.rs:4,139-154`). No charset rule. `kk:agent-browser` loads. Test `parses_repairs_and_sanitizes_frontmatter` (`parser_tests.rs:7`).
- `description`: required and non-empty (`missing field description`, `parser_tests.rs:25`); no length cap at parse time. Test `preserves_overlong_descriptions_and_short_descriptions` (`parser_tests.rs:100`) feeds 1025 chars and asserts it parses.
- YAML repair: when strict YAML fails, scalars that contain `: ` or start with `[ { @` or a backtick are quoted and the parse is retried (`parser.rs:50-62,92-165`; tests `parser_tests.rs:46,64,82`).
- Unknown frontmatter fields are ignored.

**Invalid skill outcome.** Only the bad skill is dropped. It becomes `SkillError{path, message}` in a `LoadedSkills{skills, errors}` result (`skills/src/loading.rs:13-31`). System-scope errors are discarded (`loader/host.rs:320-324`). Optional `agents/openai.yaml` fails open (`loader/metadata.rs:25-80`).

**Who sees it.** The TUI keeps a set of (path, message). It shows each error once, again only if the error clears and returns or the message changes (`tui/src/app/startup_prompts.rs:31-67`). Text: `Skipped loading {n} skill(s) due to invalid SKILL.md files.` and one `{path}: {message}` line each. Before startup completes the summary line is dropped. Provider warnings are capped at 4 messages of 256 bytes (`ext/skills/src/warnings.rs:3-4`).

**Too many skills** (`ext/skills/src/render.rs`).
- Description cap at render time: more than 1,024 chars becomes 1,021 chars plus `...` (`:1186-1204`). No warning for truncation alone.
- Budget (`:23-33,169-196`): config `skills.max_context_tokens`, capped at 10,000; else 2% of the model context window in tokens; else 8,000 characters when the window is unknown. Tokens are `ceil(bytes/4)`; each line costs its text plus `\n`; characters mode counts chars. Pinned by `catalog_budget_uses_context_percentage_or_character_fallback` (`render_tests.rs:312`): 100k window gives 2,000, 400k gives 8,000, no window gives 8,000 chars, override 50,000 gives 10,000.
- `allocate_skill_lines` (`:380-421`, `469-508`) has three steps. (a) Everything fits: full descriptions. (b) Names alone fit: share the leftover budget over descriptions round-robin, one char at a time, so no skill takes it all. (c) Even names do not fit: keep lines in order with empty descriptions until the budget ends and mark the rest omitted.
- The omission line costs budget. If it does not fit, trailing lines are popped until it does (`:985-1003`). Text: `- {n} additional skill omitted from this bounded skills list.` or `skills`. Test `omission_notice_follows_render_policy_and_is_charged_to_catalog_budget` (`render_tests.rs:1057`).
- Warning only when skills are omitted (`:127-148`): `Exceeded skills context budget. All skill descriptions were removed and {n} additional skill(s) was/were not included in the model-visible skills list.` Pinned at `render_tests.rs:1188`.
- Order for dropping under the old style: System, Admin, Repo, User, then name, then path (`:84-98`).
- The catalog is one `developer` message in `<skills_instructions>` (`fragments.rs:14-80`), re-sent only when the body changes (`world_state.rs:70-76`). A hidden skill (`policy.allow_implicit_invocation: false`) is not listed but still reachable by `$name`, resolved against the full catalog (`ext/skills/src/selection.rs:21-71`).

**Collisions.** Same name in two roots keeps both. Dedupe is by canonical SKILL.md path; order is scope, name, path (`loader/host_merge.rs:150-222`; test `resolved_config_and_repo_roots_preserve_order_and_dedupe_paths_not_names`).

**Discovery.** Scan depth 6, 2,000 dirs and 20,000 entries per root, hidden directories skipped, directory symlinks followed except for bundled skills (`loader/mod.rs:26-27`, `discovery.rs:18,119-190`). No `.gitignore` handling found. Walk errors go to the log only.

**Trust.** Codex loads repo skills even in an untrusted project (`tui/src/app/startup_prompts.rs:74-93` says "skills still load"). It gates project docs instead (`core/src/agents_md.rs:65`), the opposite of Ask.

## Decision matrix

| Decision | Codex | Ask today (PR #8) | Chosen |
|---|---|---|---|
| Name rule | collapse spaces, folder fallback, max 64 chars, no charset | regex `^[a-z0-9]+(-[a-z0-9]+)*$` | Codex. `kk:agent-browser` loads. |
| Long description | parse OK, cut to 1,021 + `...` at render | reject at 1024 bytes | Codex. |
| Broken YAML (`a: b: c`) | quote and retry | "bad frontmatter" | Codex repair. |
| Frontmatter read cap | none | 4,096 bytes | Raise to 64 KiB (safety against a huge file, still loads real skills). |
| Same name twice | keep both, order scope, name, path | first wins plus `shadowed` diagnostic | Codex: keep both, no diagnostic; the model sees the path in each line. |
| Walk limits | depth 6, 2,000 dirs, 20,000 entries, hidden skipped | depth 5 | Codex numbers. |
| Many skills | 2% of window, 8,000-char fallback, 3 steps, omission line, one warning | 16 KiB flat cap, one "N more" line | Codex algorithm. Window comes from the session model. |
| Budget override | `skills.max_context_tokens`, max 10,000 | none | Add the setting `skillsMaxContextTokens`. |
| Warning volume | each error once; startup summary hidden; provider warnings 4 x 256 B | one line per category plus full list in TUI | Cap the TUI list at 4 rows plus "N more"; keep full list in `ask settings show`. |
| Untrusted project skills | load | gated | Keep Ask's gate. Codex's choice is weaker and does not fit decision D-g. |
| Explicit `$name`, `[[skills.config]]`, bundled toggle, plugins, delta mode, aliases | yes | no | Skip. Ask has no skill tool; the model reads SKILL.md. |

## Risks

| Risk | Handling |
|---|---|
| Loose names put odd characters in the prompt | Names go through `ReminderText.Quote` and XML escaping already; add a test for `<`, newline and a 10,000-char name (cut to 64 chars like Codex). |
| Budget depends on the model, but `PrepareSession` runs before the model is known | Catalog is rendered in `Render` (already after model choice); pass `ContextWindow` from `AgentSpec.Start`. |
| Model switch changes the budget mid-session | The seed is fixed at session start, as before; document it. |
| Same-name skills confuse the model | Each line carries the path. |

## Not verified

Static reading only. The TUI wording of the budget warning in `tui/src/chatwidget/tests/app_server.rs:1071` differs from the source string; the source string is used here. How `ExtensionWarning` reaches the UI was not traced.
