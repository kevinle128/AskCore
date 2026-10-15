# Xia analysis: T1b TUI surfaces from Grok CLI and omp (mode: port)

Date: 2026-10-10. Status: analysis and decision matrix, waiting for the user's scope choice. No plan or code yet.

## Source manifest

| Source | Ref | Read through | License |
|---|---|---|---|
| Grok CLI `grok-build`, crates `xai-grok-pager`, `xai-grok-pager-minimal`, `xai-grok-shell` | `2bdd1d6a` | GitNexus plus direct reads | not checked |
| omp `oh-my-pi`, `packages/coding-agent/src`, `packages/tui/src` | `989271449953` | GitNexus plus direct reads (four sub-agents) | MIT |

Behavior is ported; no code is copied (Rust and TypeScript sources).

## Key finding

Grok CLI has an inline mode (`grok --minimal`, `xai-grok-pager-minimal/src/lib.rs:1-6`) built like Ask's TUI: finished blocks print once into the terminal scrollback and a small live region stays at the bottom. Its rules for what to print, how to cut it and how to see more transfer almost one to one. omp is mostly a full-screen TUI; its limits and wording are useful, its expand-in-place toggle is not (Ask cannot change printed scrollback).

## Ask today (scout, verified where marked)

- Tool calls are one line with name, arguments, state and duration (`internal/tui/blocks.go:355-426`, verified). The result content already arrives in `tool_execution_end` (`pkg/protocol/events.go`), and the TUI drops it.
- The only real tool is the demo `echo` (`cmd/tui/headless.go:211`); H5 builtin tools have no plan.
- Backend phases H5 to H15 and X1 have no plans, so most T1b rows are blocked (roadmap rows 1 to 11).
- The scout listed `/compact`, `/resume`, `/login` and a `SetModel(..., saveDefault)` as delivered; they are not (T1a has `/model`, `/new`, `/sessions`, `/take`, `/quit`; `saveDefault` is planned in T1c).

## Surface by surface

| # | Surface | Grok CLI (inline mode) | omp | Ask status | Verdict |
|---|---|---|---|---|---|
| 1 | Tool output | Commit-time fold policy: edits full diff, read/search quiet, others first 2 + last 3 lines with `… +N lines` (`xai-grok-pager-minimal/src/commit.rs:71-89`); block cap 2000 rows with `… N more lines · /transcript to view` (`commit.rs:268-302`); `/expand` or `ctrl+e` re-prints the last folded block in full (`commit.rs:401-443`); consecutive same-kind tools group into one row (`Read 3 files`) | ctrl+o expands in place; limits collapsed 3, expanded 10-12, bash 20, diff 8 hunks/40 lines; `… N earlier lines` | one line, output dropped | Port Grok's inline policy; use omp limits where Grok has none |
| 2 | Full view | `/transcript` renders the whole conversation expanded in `$PAGER` | none (full-screen) | none | Port |
| 3 | Copy | `/copy [N] [file]`: native clipboard, tmux buffer, OSC 52 (tmux passthrough), always a fallback file; message names the route and size | `/copy` tree picker (replies, code blocks); OSC 52 first, native tools with 2 s timeout | none | Port Grok's command and routes, with omp's timeout |
| 4 | Errors and retry | Error type table with headline and next step; `Retrying (attempt N)…`; separate blocks for context too large, credits, re-auth, disk full | thresholds and countdown timer | partly (`translate_errors.go`, `retry_cause_test.go`) | Port the wording table |
| 5 | Usage meter | `8.5K / 1.0M (42%)` in the info row | thresholds by percent or tokens (50/70/90%) | footer meter exists | Small adapt (token count beside percent) |
| 6 | Activity line | phase label (`Thinking…`, `Responding…`, tool title), phase timer hidden under 60 columns, `N queued, Enter to send now` | similar | `Working… Ns` | Adapt |
| 7 | Notifications | OSC 9 / 99 / 777 / BEL on turn end, approval needed, error, only when unfocused (focus events); OSC 9;4 progress | none found | none | Port (high value for a daemon agent) |
| 8 | Terminal | resize reprint after 120 ms (Ask has it) | ctrl+z suspend in Pi/omp | no suspend, no focus reports, no tmux rules | Port suspend (T-TRM-08), focus (T-TRM-18), tmux rules (T-TRM-07) |
| 9 | Todo panel | above the prompt from ACP `Plan` updates, 8 rows max, hides when done | pinned todo reminder | none; agent sends no plan | Wait for an agent feature |
| 10 | Trust | y/N gate only when the repo has code-running config | none | H6 not planned | Wait for H6 |
| 11 | Compaction markers | `Context N% full. Compacting…`, `Context compacted: A → B tokens (Xs)`, failed, cancelled | divider `── method · 256K→20K · ctrl+o ──` | H10 not planned | Wait for H10; take Grok's text |
| 12 | Hooks and MCP | `` `tool` blocked by X: detail ``; `Turn blocked by a hook`; MCP status badges | blocked labels sanitized of ANSI | H11 not planned | Wait for H11 |
| 13 | Skills and slash merge | built-in wins, clashes renamed `/acme:login`, `[tag]` column, MRU | `/skill:name`, `$1 $@ {{arguments}}` | H15 not planned | Wait for H15 |
| 14 | Resume picker | summary + time ago, expandable details, content search; `-c`; replay de-dup by event id | search, status per session, `/tree`, `/fork` | `/sessions` live only | Wait for H8 / H14 |
| 15 | Esc semantics | Esc never cancels a turn (ctrl+c does) | Esc cancels | decided: esc cancels (Pi keys, user 2026-10-09) | Keep Ask's decision |

## What can be built now without a new backend phase

- Tool output display from the existing `tool_execution_end` result (rows 1 and 2) — but only the demo `echo` tool exists, and real renderers (diff, bash) come with H5. Note: the user moved T-LAY-09, T-MD-10 and T-CMD-08 to T1b **with H5** on 2026-10-09; doing them before H5 changes that timing and needs the user's confirmation.
- `/copy` (row 3), error wording (row 4), meter and activity line (rows 5 and 6), notifications and focus (row 7), suspend and tmux rules (row 8).

Every row changes what the user sees, so each slice needs an approved TUI design first (the `ask-tui-design` rule).

## Risks

- Tool output before H5: renderers designed against the demo `echo` may not fit real tools (diff, bash streams). Mitigation: design the fold policy now with fixture tool results, add per-tool renderers with H5.
- OSC 52 and notifications behave differently per terminal and under tmux; needs the manual terminal check and tmux passthrough rules.
- Printed scrollback cannot change; any expand must re-print (Grok) rather than toggle (omp).
- A full-view `/transcript` in `$PAGER` must restore the terminal and the inline frame after the pager exits (suspend and resume path).

## Unverified

- Grok's model catalog `_meta` source file (`xai-grok-shell/src/cli_models.rs`) was not opened.
- omp's `onGenerationChanged` and model cache TTL (from the earlier report) remain unverified; not needed here.
