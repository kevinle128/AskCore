# Xia analysis: H5 built-in tools from omp and Grok CLI (mode: improve)

Date: 2026-10-10. Status: analysis and decision matrix; all four decisions made by the user on 2026-10-10. Plan next.

## Source manifest

| Source | Ref | Read through | License |
|---|---|---|---|
| Pi (contract reference, roadmap H5) | as recorded in `inventory-harness.md` rows H-TOOL-01..23 | inventory rows | MIT |
| omp `oh-my-pi`, `packages/coding-agent/src/tools`, `src/edit`, `src/exec`, `src/session/streaming-output.ts` | `989271449953` | GitNexus plus direct reads | MIT |
| Grok CLI `grok-build`, `crates/codegen/xai-grok-tools`, `xai-grok-shell`, `xai-grok-workspace`, `xai-grok-permission-rules` | `2bdd1d6a` | GitNexus (definitions only) plus direct reads | Apache-2.0 (keep the notice if code is ported; this plan ports behavior only) |

## Scope authority (roadmap H5, decided)

- Tools: `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls` (H-TOOL-01..07), prompt snippets (H-TOOL-11), paths (H-TOOL-19), shell and child env (H-TOOL-22), process-tree kill (H-TOOL-23), no-approval stance (H-SEC-03, D3).
- Contract (2026-10-06): Pi's input, behavior and output; Claude Code names where semantics match; Pi contract parity for names, fields, units, defaults, errors and output limits; no `find -> Glob` alias.
- D3: no permission prompts and no built-in policy in M1; blocking is an extension (X1); allow/deny/ask policy is M2.
- D7: env prefix `ASK_*` (so Pi's `PI_SESSION_ID` becomes `ASK_SESSION_ID`). D8: Unix only.
- Output reuses H2's result contract (`content`, `details`, `structuredContent`, `isError`); hard-coded defaults, H6 wires settings.
- Exit: in print mode the agent reads a repo, edits a file and runs its tests.

## Ask today (scout)

- `internal/tools`: `Tool`, `ConcurrencySafe`, `ArgumentPreparer`, snapshot registry, only `Echo` (`internal/tools/types.go:15-31`, `cmd/tui/headless.go:211`).
- Pipeline `BeforeTool` / `AfterTool`, partial updates through `Context.Update`, drain rule D19 (started tool bodies finish).
- `internal/sandbox` and `internal/workspace` are scaffolds.
- `rg` installed through Homebrew; `fd` only inside another tool's directory, so `fd` cannot be assumed on user machines.

## Dependency matrix

| Component | Ask | Class |
|---|---|---|
| Tool interface, registry, result contract, partial updates | exists | EXISTS |
| Argument coercion table (D21) | none | NEW |
| Seven tools | none | NEW |
| Per-path mutation queue, atomic write | none | NEW |
| Local process executor (process group, two-wave kill, env) | sandbox scaffold | NEW |
| Session cwd resolver | workspace scaffold | NEW |
| Output truncation and spill files | none | NEW |
| Prompt snippets per tool | none | NEW |
| ACP tool mapping (kind, locations, diff) | partial in `internal/acp` | CONFLICT (diff shape, locations) |

## What to take (no conflict with the contract)

| Area | Take | From |
|---|---|---|
| read | numbers on every line `N\|text`; binary check (NUL or over 30% control bytes in the first 8 KB); long-line cap only on model view (768); past-end-of-file hint instead of error; explicit `Use offset=N to continue`; image re-encode | omp `read.ts`, Grok `binary.rs`, `image.rs` |
| write | atomic temp file and rename (both sources write in place; Ask does better); shebang `chmod +x`; refuse to overwrite generated files (`@generated`, protoc, sqlc) | omp `write.ts:413-427`, `auto-generated-guard.ts` |
| edit | match ladder exact, then rstrip/trim/Unicode normalize, then fuzzy per-line score 0.95 (retry 0.8 without indent), unique-match rule; re-indent replacement; error text `Closest match (N% similar) at line L` with the first differing line, up to 5 previews for multiple matches; reject no-op edits, hard error after 3 identical no-ops; CRLF and BOM round trip; overlap check with excluded ranges | omp `replace.ts`, `normalize.ts`; Grok `search_replace`, Codex `seek_sequence.rs` |
| edit freshness | record mtime and size (or hash) at read; reject an edit when the file changed since this session last read it (new: neither source enforces it) | new |
| bash | non-interactive env (`TERM=dumb`, `NO_COLOR`, `CI=true`, pagers `cat`, `GIT_TERMINAL_PROMPT=0`, editors `true`, `SSH_ASKPASS` off) plus `ASK_*` session vars; own process group, SIGTERM to the group, wait, re-walk, SIGKILL; full output to a spill file; `exit: N` header; ANSI stripped; `\r` progress collapsed; partial updates at most one per 50 ms; leading `cd X &&` noted | omp `non-interactive-env.ts`, `streaming-output.ts`; Grok `terminal.rs`, `bash/mod.rs` |
| grep | `rg` with `--json --hidden` per Pi; per-file match cap, early kill when the budget is full, `Found at least N` wording, 20 s timeout, 500-char line cap | Grok `grep`, omp `grep.ts` |
| find / ls | `fd` per Pi when present, else `rg --files`, else pure Go walk with gitignore; 5 s timeout | Grok opencode `glob`, omp `glob.ts` |
| paths | `@`, `~`, `file://`, macOS screenshot variants (U+202F, NFD, curly quotes), shell-escaped names | omp `path-utils.ts` |
| result | model text separate from UI details (truncation range, next offset, spill path, exit code); ACP diff with whole-file old and new text; `locations` on read and edit | omp `output-meta.ts`, Grok `acp_conversion.rs` |
| prompts | one description per tool with tool names filled at render time | omp prompts, Grok templates |
| tests | scenario checklist: CRLF, BOM, multi-match, confusable quotes, binary by content, head and tail truncation, kill of a process tree, no-op loop, stale edit | both test suites |

## Skip

omp read selectors (`:50-200`) and hashline anchors; persistent in-process shell (brush) and Grok shell-state replay; PTY mode; LLM permission classifier; OS sandbox (neither source turns it on; matches Pi); Grok's `list_dir` tree (Pi's `ls` is flat); apply_patch (later, for GPT models); sparse line markers.

## Decisions (user, 2026-10-10)

1. Edit schema. Decided (user, 2026-10-10): keep Pi `edits[{oldText,newText}]` (contract of 2026-10-06); the omp and Grok matching and error improvements apply to it.
2. Bash output truncation. Decided (user, 2026-10-10): keep the head 20 KB and the tail 30 KB inside Pi's 50 KB and 2000-line limits (as omp), with an elision marker and the spill file path; `read` keeps Pi's behavior.
3. Critical commands. Decided (user, 2026-10-10): D3 stays (no built-in policy). H5 adds a compiled-in extension `command-guard` registered through `pipeline.Registry.OnBeforeTool` (`internal/pipeline/registry.go:215,334`), OFF by default and enabled with `ASK_COMMAND_GUARD=1`. It blocks a short list of catastrophic patterns and returns a tool error `blocked by command-guard: <reason>`; the run continues. Purpose: exercise the extension and denied-tool path end to end before X1; it is not a security boundary. Tests cover on and off.
4. Long-running commands. Decided (user, 2026-10-10): option C, scope extended to work like Grok CLI. A command that is still running after a wait budget moves to the background instead of being killed (Grok `block_until_ms`, default 30 s; `0` starts it in the background at once; background cap 10 h; a trailing `&` is rejected with a hint) (`xai-grok-tools/src/implementations/grok_build/bash/mod.rs:427-428,898-904,1221-1257,1556-1577`; `computer/local/terminal.rs:46`). Add Grok's companion tools for background tasks: read task output, kill a task, and the monitor tool that turns each output line of a script into an event (`grok_build/monitor/tool.rs:30`). The leader owns background processes: they are killed when the session ends or the leader exits (H-TOOL-23), and they never block D19's drain of the tool call that started them.

## Risks

- Fuzzy edits can change the wrong place: mitigated by the unique-match rule, the score margin and the overlap check, and by tests with near-duplicate blocks.
- Process-tree kill races with fast-forking children: two-wave kill and a test with a grandchild that ignores SIGTERM.
- `fd` missing on user machines: fallback chain and a test with `fd` hidden from `PATH`.
- Spill files leak secrets printed by commands: store under the session directory with mode 0600 and remove with the session.
- Changing the edit schema or truncation shape breaks Pi parity tests and cassettes recorded against the current contract.

## Risk score

Medium: the tools are new code with no consumer yet except the TUI (T1b) and print mode; the main risk is edit correctness and process control, both testable without real providers.
