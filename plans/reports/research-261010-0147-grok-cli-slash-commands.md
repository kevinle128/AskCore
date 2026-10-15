# Grok CLI and Pi: how slash commands work

Source repos (read-only): Grok `/Users/dale/Desktop/workspace/opensources/grok-build` (commit `2bdd1d6a`, 2026-09-29), Pi `/Users/dale/Desktop/workspace/opensources/pi` (commit `4c6fb7cf`). Paths for Grok are under `crates/codegen/xai-grok-pager/src/` (pager) and `crates/codegen/xai-grok-shell/src/` (shell). Pi paths are under `packages/`.

Status of each claim: "read" means a person read the cited lines; "reported" means a research agent reported it and it was not re-read; "user-tested" means the user ran it in the real program.

## Result for Ask (user decision, 2026-10-10)

Slash command names are case-insensitive, like Grok. Only the command token is folded; arguments keep their case. Unknown commands stay normal prompts. In T1a no command takes arguments, so a draft with arguments is a normal prompt.

## Grok: path of a typed command

1. **Menu opens** when `/` starts a token (also mid-text after whitespace). An empty query lists all commands in registration order (`slash/matcher.rs:67-69`, reported).
2. **Menu matching is smart-case** (read): an all-lowercase query matches without regard to case; any uppercase letter makes it an exact-case prefix (`slash/mod.rs:162-170`, `slash/matcher.rs:73`). So `/MEMORY` shows no row for `memory`.
3. **Enter with the menu open** (read, `app/agent_view/prompt.rs:213-235`, `slash/mod.rs:1493-1519`):
   - if the typed command is complete and the highlighted row is that command (or an alias), the text is sent unchanged;
   - otherwise the highlighted row is accepted (text replaced by its `insert_text`) and sent, unless `insert_text` ends with a space (the command takes arguments); then the menu stays open for arguments.
4. **Esc** closes the menu and reverts any live preview (`prompt.rs:207-210`, reported).
5. **Dispatch in the pager** (read): `parse_invocation` splits the token and arguments (`slash/mod.rs:1427-1450`). `CommandRegistry::get_for_dispatch(token)` is an exact-case `HashMap` lookup (`slash/registry.rs:195-202`; keys are inserted as written at `:541` and `:556`). `/MEMORY` does not match here.
6. **Unknown commands pass through** (read): `is_command_complete` treats a command that is not in the registry as complete, and it goes to the agent shell as text (`slash/mod.rs:1452-1463`).
7. **The shell folds the case** (read): `slash_key(name) = name.to_lowercase()` (`xai-grok-shell/src/session/slash_commands.rs:566`). `resolve_human_intent` compares builtins, aliases and prompt commands with `slash_key` (`:1568-1603`). This is why `/MEMORY` runs (user-tested).
8. **Visibility tiers** (reported, `slash/registry.rs`): `hidden` (not shown, not runnable), `menu_hidden` (not offered, still runnable when typed in full), `restricted` (tier gate, upsell instead), `tools_satisfied` (needs advertised tools).
9. **Arguments** (reported): `takes_args` and `args_required` decide completeness; `/model` requires an argument, so Enter on a bare `/model` accepts the row and opens the argument menu.
10. **During a run** (reported, `app/dispatch/queue.rs:156-162`): `/compact`, `/imagine` and `/loop` are queued at the tail of the local queue; most other commands run at once.

## Pi: path of a typed command

- **Menu** (read, `tui/src/fuzzy.ts:13-14`): query and text are both lowercased, so `/MODEL` lists `model` (user-tested with a screenshot).
- **Enter with the menu open** (read, `tui/src/components/editor.ts:789-810`): the selected item is applied (text replaced by the canonical name), then the editor falls through to submit. So `/MODEL` + Enter runs `/model`.
- **Submit** (read, `coding-agent/src/modes/interactive/interactive-mode.ts:3153-3293`): text is trimmed, then compared with `text === "/settings"`, `text === "/model" || text.startsWith("/model ")` and so on. Exact case. After Esc has closed the menu, `/MODEL` is not a command in Pi (read, not user-tested).

## Comparison

| Situation | Pi | Grok | Ask (decided) |
|---|---|---|---|
| Menu filter with uppercase query | matches (case-insensitive) | no row (smart-case) | matches (case-insensitive) |
| `/MEMORY` + Enter, menu open | accepts the row, runs | sent as text, shell folds case, runs | runs |
| `/MEMORY` + Enter after Esc | prompt (exact compare) | runs (shell folds case) | runs (lookup lowercases) |
| Command with arguments | exact `startsWith("/cmd ")` | argument menu, chained Enter | not in T1a (draft with arguments is a prompt) |

## Grok behavior the Ask editor does not have (kept out of T1a scope on purpose)

Fuzzy ranking with nucleo, MRU decay (7-day half-life), tiebreak order, ghost text, mid-text `/token` hoist, live preview while arrowing arguments, chained argument menus, `menu_hidden` tier, queue-versus-run split. The five T1a commands take no arguments, so none of these is needed. Revisit when argument commands arrive (resume, tree, settings).

## Unverified

- MRU, tiebreak, ghost text, mid-text hoist and queue claims come from the research agent and were not re-read.
- `/MEMORY` after Esc in Pi was not run.
