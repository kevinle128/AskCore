# Xia analysis: H6b prompt, context files, skills, tool selection from DeepSeek and Grok CLI (mode: improve)

Date: 2026-11-04. Status: analysis and decision matrix. No decision made yet. Plan next.

## Source manifest

| Source | Ref | Read through | License |
|---|---|---|---|
| Pi (contract reference) | inventory rows H-PROMPT-01..10, H-TOOL-10, H-TOOL-11 | `inventory-harness.md:60,80-89` | MIT |
| DeepSeek harness | `5badb15009` (`/Users/dale/Desktop/workspace/opensources/deepseek-harness`) | direct reads by scout; extends `researcher-261103-2153-deepseek-config-trust.md` | see repo `LICENSE` |
| Grok Build | `2bdd1d6a` (`/Users/dale/Desktop/workspace/opensources/grok-build`) | direct reads by scout (static; nothing run) | Apache-2.0 (this plan ports behavior only) |
| Ask | branch `develop`, `de61a4d9` | direct reads | n/a |

Scope authority: roadmap H6 (`roadmap.md:306-330`) minus the part done by H6a. H6b = H-PROMPT-01, 02, 04, 05, 09, 10, H-TOOL-10, plus the H6a hand-off note (ungated `AGENTS.md`/`CLAUDE.md`, `plans/261103-2158-h6a-settings-trust/plan.md:263-265`). H-PROMPT-08 stays a stub until H11.

## Ask today

- The prompt is one string: `tools.PromptSection(reg.Snapshot())` set at `cmd/tui/headless.go:459-460` (`internal/tools/prompt.go:20-63`). No preamble, date, cwd, context files or skills. `internal/agent/systemprompt.go:16-24` wraps the string into one `SystemMessage`; `request_log.go:47` logs a changed prompt as a new snapshot.
- `internal/bootstrap` and `internal/skills` hold only README and `doc.go`.
- `workspace` already lists `SYSTEM.md`, `APPEND_SYSTEM.md`, `skills`, `.agents/skills` as gated (`internal/workspace/project.go:20-32`). `AGENTS.md` and `CLAUDE.md` are not gated (pinned by `cmd/tui/trust_e2e_test.go:425-428`).
- Plumbing gap: the ACP Factory computes the trust decision and `Effective` (`internal/app/module_acp.go:140-163`) but `NewAgent(service, id, cwd, start)` receives neither (`module_acp.go:54,153`). Print mode drops `eff` after reading `DefaultModel` (`headless.go:231-232,275-277`) and uses `os.Getwd()`.
- `settings.Effective` has no `defaultTools`. A project `defaultTools` would be allowed unless `merge.go:16-30` denies it.
- Flags `--system-prompt`, `--append-system-prompt`, `-t`, `-nt`, `-nc` do not exist (`cmd/tui/args.go`; an unknown `-x` is an error).
- No test pins byte-stability of the prompt.

## Comparison

| Topic | DeepSeek | Grok | Pi (contract) |
|---|---|---|---|
| Sections | Registry of `{name, order, text\|fn, complete?}`; sparse integer orders, gap >= 10, name tiebreak by code unit; volatile facts at order 10000+ (`system-prompt/src/index.ts:44-67,117-146,230-237`) | One MiniJinja template with tool-kind `if` blocks; static per session; 16384-byte soft ceiling test (`template.rs:531`) | Ordered named sections, patch by name (H-PROMPT-01) |
| Stability | Test renders two environments, asserts identical prefix (`system-prompt.spec.ts:44-75`) | Optional block is a pure suffix, tested (`context.rs:345-358`); date frozen, rollover by reminder (`reminders.rs:173-177`) | ISO date only (H-PROMPT-09) |
| Variables | Strict `{{x}}`: unknown or malformed throws; no rescan; `hasOwn` guard (`index.ts:408-440`) | Custom `${{ }}` delimiters so literal braces pass | none |
| Replace/append | No SYSTEM.md or flags. `complete: true` section = whole prompt; more than one throws (`index.ts:459-461`) | `--system-prompt` replaces; `--rules` appends in `<human_rules>`; blank override ignored; `rules` only at creation, override re-synced on resume (`mvp_agent/mod.rs:976-1052`) | `SYSTEM.md`, `APPEND_SYSTEM.md`, two flags (H-PROMPT-02) |
| Context files: where | One user-role `<system-reminder>` message (`render.ts:12-14`) | One user-role `<system-reminder>` item at conversation index 1, tagged, idempotent across resume, fork, compaction (`spawn.rs:1422-1430`, `prompt_build.rs:292-298`) | System prompt, `<project_instructions path>` |
| Names and order | `AGENTS.md`, `CLAUDE.md`, then `*.local.md`; every file loads; user-global first, then root to cwd; root = nearest `.git` (file or dir); marker probe error throws (`files.ts`, `config.ts:12-13`) | Six filenames, every match loads; Home, Configured, Project; root-first; worktree root only; git root equal to `$HOME` dropped (`permission-rules/src/repo.rs:10-43`) | First hit per dir of 5 names, agent dir, then ancestors |
| Escaping | Only `</system-reminder>` to `<\/system-reminder>`, body, paths and notices (`render.ts:81-83`) | Case-insensitive `(?i)<(\s*/?\s*system[-_]reminder)` to `&lt;$1` (`agents_md.rs:372-388`); the `name="path"` attribute in `<rules>` is not escaped | none (Pi wraps, does not escape) |
| Size | Per file 1 MiB silently ignored; whole message `maxBytes` (base 65536); drop broadest files first, truncate most specific at UTF-8 boundary, visible notice (`render.ts:206-215,272-340`) | No cap, by design (`12-project-rules.md`) | none stated |
| Dedup | Trimmed-content SHA-1 per dir; user-global equal to project file deduped (`digest.ts:26`) | Canonical path (case-insensitive FS) | none |
| Trust of context files | none | Gated; gate fires on marker presence, also gitignored files; headless fails closed (`folder_trust.rs`) | loads without trust (Ask decision 1 copies this) |
| Skills: dirs | `.dsh/skills`, `.agents/skills`, custom, home, `~/.agents/skills`, bundled; depth 1 only | `.grok`, `.agents`, compat dirs per chain dir, cwd-first; depth max 5; sorted walk (`discovery.rs:58-130`) | `<agent-dir>/skills`, `.pi/skills`, `~/.agents/skills`, ancestor `.agents/skills`; recursive; .gitignore |
| Skills: validation | Name `^[a-z0-9]+(-[a-z0-9]+)*$`; bad frontmatter or bool drops skill with a log warning only | `[a-z0-9-]` max 64, slug fallback to dir name; description max 1024; frontmatter reader 4096 bytes; 3-stage YAML recovery (`discovery.rs:14-20,470-570`) | name max 64, description max 1024; bad skills not loaded |
| Skills: collision | First by rank; loser logged | First-seen; cross-scope shadow intended; same-scope sibling re-keyed to dir name (`skills.rs:382-470`) | First wins with warning |
| Skills: prompt | User-role `<available_skills>` list, name and description (cap 500, whitespace-normalized, XML-escaped); republish by digest of entries | User-role `<agent_skill fullPath>`; 400-byte entry cap, 3 budget tiers, overflow line (`listing.rs`) | `<available_skills>` in system prompt, only if `read` or `bash` active |
| Skills: trust | none | Gated on presence of skills dir, even empty | gated (H-SEC) |
| Skill body | `skill` tool, no cap | `Skill` tool, 25k tokens cap, `$ARGUMENTS` | `read` the file |
| Tool selection | `ctx.tools.restrict({allow\|deny})` per agent scope; unknown name throws; `{}` throws; `toolOrder` (`tools/src/index.ts:1099-1124`) | `--tools`/`--disallowed-tools`, headless only, final intersection clamp, denylist wins, inherited by subagents (`config.rs:766-773,1316-1329`) | `defaultTools` with `+`/`-` deltas, `-t`, `-xt`, `-nbt`, `-nt` |
| Tool prompt text | Semantics in tool description; sections only for cross-call habits; guidance resolves tool visibility at assembly | Tool description templated from the final toolset | `promptSnippet` and `promptGuidelines`, tools without snippet unlisted |

Neither reference has `SYSTEM.md`, `APPEND_SYSTEM.md`, or `defaultTools`. Those stay Pi-owned. Neither one gates skills and context files in the way Ask needs: DeepSeek does not gate at all; Grok gates both, and Ask has decided not to gate context files.

## What to take

1. **Named sections with sparse orders and a test** (DeepSeek). Section = `{Name, Order, Text}`; orders unique and >= 10 apart; name regex `^[a-z][a-z0-9_-]*$` from Pi; tiebreak by name. Test: two environments give an identical prefix. Volatile sections (cwd, date) sort last.
2. **Replace and append are different operations** (DeepSeek `complete`, Grok override vs rules, Pi files). One `complete` baseline; blank override ignored (Grok). Pi precedence: project `SYSTEM.md` beats user; custom prompt drops tools, rules and docs sections.
3. **Context files as one user-role message, not system text** (both). Idempotent across resume, fork and compaction (Grok tag). This answers the H6a hand-off: the repo cannot write the system prompt, and the framing text says the content cannot override system or user instructions (DeepSeek `render.ts:12-14`).
4. **Escaping**: Grok's case-insensitive pattern accepting `-` and `_`, applied to body, path and notices (DeepSeek's coverage). Escape attribute values too (Grok gap). Pin with tests.
5. **Byte budget with a visible notice** (DeepSeek): per-file source cap, total cap, drop broad first, UTF-8-safe truncation of the specific file. Grok's "no cap" is unsafe for Ask because context files are ungated.
6. **Dedup** by trimmed content per directory (DeepSeek), so `AGENTS.md` copied to `CLAUDE.md` loads once.
7. **Root detection**: `.git` as file or dir marks the root (worktree-safe); a probe error other than not-exist fails instead of climbing (DeepSeek). Drop a git root equal to `$HOME` (Grok).
8. **Skill parsing**: Pi limits (64/1024), Grok's 4096-byte frontmatter reader, strict booleans (DeepSeek), sorted deterministic walk, same-scope collision re-key (Grok) or Pi's first-wins with a warning.
9. **Skill diagnostics**: both references skip bad skills silently or log only. Ask returns a diagnostics list (path, reason) the TUI and `ask settings show` can print. This is the improvement over both.
10. **Skill listing**: user-role message, XML-escaped, description whitespace-normalized and capped, overflow line, republish only when the entry digest changes (DeepSeek). Needs a byte budget (Grok `listing.rs`).
11. **Skill body cap** (Grok 100 KB with a truncation note). DeepSeek has none.
12. **Tool selection**: one resolver before `Registry.Snapshot`, so snippets follow the active set by construction. Unknown names are an error that lists known tools (DeepSeek). Denylist wins (Grok). Empty `defaultTools` disables built-ins only (Pi).
13. **Guidance follows the toolset** (both): sections check tool presence at assembly. Ask already derives snippets from the registry; keep this rule for new sections.
14. **Date outside the system prompt** with a rollover reminder (Grok), or ISO date as a last section (Pi). See decision D-b.

## What to skip

XOR template obfuscation; MiniJinja and custom delimiters (Go `text/template` is not needed, plain section strings suffice); Claude, Cursor and vendor-default compat cells; workspace-user overlay; ACP `_meta` carriers for override and rules in this phase (see D-e); touch-driven nested-instruction refresh and SHA-1 reconcile; file watchers (Pi has none, E-LD-11); `{{var}}` interpolation in user-authored `SYSTEM.md` (DeepSeek documents it has no escape syntax); model-specific prompt templates (neither source has per-model branching); Grok's unescaped path attribute.

## Ask-specific risks the references do not solve

| Risk | Evidence | Proposed handling |
|---|---|---|
| Ungated context files steer the model | H6a decision 1; Grok gates, DeepSeek does not | Deliver in a user-role frame, escape, cap, label with source path; `ask settings show` lists loaded context files with their size; the TUI start screen shows the count |
| Frame breakout through attributes | Grok `<rules name="path">` is unescaped | Render paths as text lines, not attributes, or escape `"` and `<` |
| Cache break when the prompt changes mid-session | DeepSeek `agent-loop/README.md:162` | Prompt fixed at session start; skill reload appends a user-role catalog message; `request_log` already records changes |
| `Config.SystemPrompt` is a fixed string | `agent/types.go:49` | Keep the field; add the assembly package, called from `NewAgent`. Context and skills go as initial user-role history items, so this needs a way to seed non-user-typed history (see D-d) |
| `NewAgent` has no trust or settings input | `module_acp.go:54,153` | Widen the signature to carry `Project{Root, Trusted}` and `Effective`; the H6a plan names this consumer |
| Project `defaultTools` | `merge.go:16-30` has no entry | Allowed for trusted projects. Arrays replace across layers, so a trusted project array can re-enable a built-in that the user layer removed (accepted: trust gates the project layer; the tool set is still limited to built-ins). Pinned by `TestDefaultToolsProjectLayerReplacesUserArray`. |
| Sibling tools over the leader | Flags are print-only today (`headless.go:~138`) | `--tools`, `--system-prompt` reach the leader session through a documented carrier (D-e) |

## Decisions needed

| ID | Question | Options | Recommendation |
|---|---|---|---|
| D-a | Where do context files go? | A: system prompt `<project_instructions>` (Pi). B: first user-role `<system-reminder>` message, escaped, capped (both references). | B. It answers the hand-off and keeps the system prompt trust-independent. Deliberate divergence from H-PROMPT-04, recorded as a row. |
| D-b | Where do cwd and date go? | A: last system sections (DeepSeek, Pi ISO date). B: user-role message plus rollover reminder (Grok). | A for cwd and ISO date at the tail, as Pi H-PROMPT-09. Revisit B at H8 when resumed sessions cross midnight. |
| D-c | Byte budget values | Per-file source cap, total cap; DeepSeek uses 1 MiB and 64 KiB. | 1 MiB source cap, 64 KiB total, setting-free in H6b; visible truncation notice. |
| D-d | How do context files and the skill list enter history? | A: initial user-role history items before the first user prompt. B: prepend to the first prompt only. | A, tagged with a synthetic reason, idempotent on resume (Grok). Needs an `Agent` seed API; check against H8 projection. |
| D-e | Reaching the leader | A: `ACPParams`/`_meta` for `--tools` and `--system-prompt`. B: print mode only now, interactive later. | B for flags in H6b; `defaultTools`, `SYSTEM.md`, context files and skills work in every mode because they come from files. |
| D-f | Skill collisions | A: Pi first-wins with a diagnostic. B: Grok re-key within a scope. | A (Pi contract), plus diagnostics. Re-key is extra scope. |
| D-g | Skill trust gate | Gate on presence of a skills dir (Grok) or on trust only (Ask `workspace` already lists the paths). | Reuse the existing `workspace` gated list; no new gate code (H6a extension trace). |
| D-h | Skills in prompt | A: system prompt `<available_skills>`, only if `read` or `bash` active (Pi). B: user-role catalog (both references). | B for the same reason as D-a. Keep Pi's "only if read or bash active" condition. |

## Test checklist (from both suites)

- Prefix identical across two environments; sections unique and ordered; optional section is a pure suffix.
- Blank `--system-prompt` is ignored; custom prompt drops tools, rules and docs sections.
- `</system-reminder>` in a context file, in its path and in a skill description cannot close the frame (case, `_`, spaces).
- Budget: multi-byte cut at a UTF-8 boundary; notice present; total never exceeds the cap; a 2 MiB file is ignored with a diagnostic.
- Dedup of identical `AGENTS.md` and `CLAUDE.md`; linked worktree; git root equal to `$HOME`; `.git` file as marker; probe error does not climb.
- No project skill loads before trust; an untrusted project with `AGENTS.md` still loads it (pins decision 1).
- Skill: bad name, long description, CRLF frontmatter, bad boolean, duplicate name across scopes; diagnostics returned for each.
- Injection is idempotent on resume (needs H8; test the seed API with a replayed history).
- `defaultTools`: unknown name errors, empty disables built-ins only, `+`/`-` deltas, project value cannot add a tool.
- Real entry path: built `ask` on a trusted project with `AGENTS.md` and a skill changes the reply; an untrusted project's `.ask/` is ignored (roadmap H6 exit).

## Not verified

Static reading only; nothing was run in either reference. Grok `os_name`, `shell_path` and `current_date` use in the primary template is unconfirmed. Grok trust store format and prompt UI were not read. DeepSeek worktree behavior beyond the `.git` marker was not found.

## Decisions (user, 2026-11-04)

- D-a, D-h, D-d: B. Context files and the skill list go in user-role messages (escaped, capped), seeded as tagged initial history items. Deliberate divergence from Pi H-PROMPT-04/05; needs a row in the divergence table.
- D-b: A. cwd and ISO date are the last system-prompt sections.
- D-c: 1 MiB per file, 64 KiB total, fixed in H6b.
- D-e: B reversed by the user: `--tools` and `--system-prompt` reach TUI and leader sessions through ACP `_meta`, so H6b adds a wire carrier (`pkg/protocol`, `internal/acp`, `internal/leader`, `tuiclient`).
- D-f, D-g: not asked; the recommendations stand (Pi first-wins with a diagnostic; reuse the `workspace` gated list).
