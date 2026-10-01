# Phase 1: Research lanes (parallel)

Status: Pending approval

## Context

Pi is about 200k lines of TypeScript in 14 packages. One agent cannot hold it. Each lane below is one subagent with one area and one report. Lanes do not edit code. They only write their report.

Common inputs for every lane:
- Pi repo: `/Users/dale/Desktop/workspace/opensources/pi` (GitNexus repo `pi`; use `query` / `context` for call graphs).
- Method: spiral scouting, rings 0 to 5 (see `plan.md`).
- Prior reports to verify and extend: `/Users/dale/Desktop/workspace/opensources/ask/plans/reports/researcher-260930-*.md`.
- Output: `/Users/dale/Desktop/workspace/opensources/AskCore/plans/reports/researcher-260930-2254-<lane-slug>.md`, English, max 800 lines.
- Each item row: feature | ring | detail | source (`path:line`, doc file, or changelog version) | Go note (optional).
- End with: unresolved questions, then `Status / Summary / Concerns`.

## Lanes

| Lane | Area | Main inputs | Report slug |
|---|---|---|---|
| A | Docs inventory: every setting key, CLI flag, env var, slash command, keybinding, mode (interactive, print, JSON, RPC), session, compaction, models, providers, packages, themes, security | `packages/coding-agent/docs/*.md` (39 files), `README.md` | `pi-docs-inventory` |
| B1 | Changelog history, 0.10.0 to about 0.50: added, changed, removed/deprecated, extension API breaks | `packages/coding-agent/CHANGELOG.md`, second half of the file | `pi-changelog-early` |
| B2 | Changelog history, about 0.50 to 0.99.1: same columns | `packages/coding-agent/CHANGELOG.md`, first half of the file | `pi-changelog-late` |
| C | Edge cases: every `Fixed` entry in all changelogs, grouped by area (tool execution, bash, edit/diff, streaming, retry, session, compaction, TUI rendering, input, providers, auth, Windows/Termux/tmux, extensions) | all 12 `CHANGELOG.md` files | `pi-edge-cases` |
| D | TUI: components, differential render, main-screen vs alt-screen, overlays, editor, autocomplete, images, themes, keybinding system, terminal quirks; direction from `tui-plan.md` | `packages/tui/src`, `packages/tui/CHANGELOG.md`, `tui-plan.md`, `docs/tui.md`, `docs/themes.md`, `docs/keybindings.md`, `src/modes/interactive` | `pi-tui` |
| E | Extension system: every hook/event name, tool registration, commands, UI primitives for extensions, RPC extension UI, lifecycle, loading, packages and package manager, skills, prompt templates, MCP bridge | `src/extensions`, `examples/extensions`, `examples/plugins`, `package-manager-cli.ts`, `docs/extensions.md`, `docs/rpc-extension-ui.md`, `docs/packages.md`, `docs/skills.md`, `docs/prompt-templates.md`, `docs/mcp.md`, `packages/mcp` | `pi-extensions` |
| F | Harness core: agent loop, tool calling, built-in tools, streaming, abort, retry and backoff, compaction, session tree and format, branching, context files, system prompt build, provider abstraction, models registry, virtual models, auth/OAuth, cost/usage | `packages/agent`, `packages/ai`, `packages/coding-agent/src/core`, their changelogs | `pi-harness-core` |
| G | Package triage: chord, durable, codemode, server, client, protocol, session-backends, telemetry, evals. Classify each as core harness, extension-adjacent, or out of scope, with a one-line reason and the main features | those package folders and changelogs | `pi-package-triage` |
| H | Pi TUI on bubbletea (library fixed by the user, see `plan.md`). Questions: bubbletea v1 vs v2 status and which one to pin (the `go.mod` v1/v2 conflict); the Charm companion stack (lipgloss, bubbles, glamour, huh, ultraviolet, x/ansi); how bubbletea does each need from lane D: inline scrollback with a fixed bottom area (vs alt-screen), differential render, overlays, multi-line editor with autocomplete, images (kitty/iTerm2), mouse, bracketed paste, theming. For each gap: workaround or custom component | web + context7 + bubbletea source/examples; AskCore `go.mod` | `go-tui-bubbletea` |
| I | Go ecosystem for the other hard parts: extension runtime (goja, wazero/WASM, hashicorp go-plugin, yaegi, subprocess JSON-RPC), LLM SDKs (anthropic-sdk-go, openai-go, google genai), MCP (go-sdk), diff/patch, fuzzy match | web + context7 | `go-agent-libraries` |

Lanes B1 and B2 split by line number, so each agent reads about 3000 lines. The changelog is newest first. B2 reads lines 1 to 3016 (Unreleased, 0.99.1 down to 0.56.3). B1 reads lines 3017 to 5979 (0.56.2 on 2026-03-05 down to 0.10.0).

Status: approved by the user on 2026-09-30 (10 lanes; oh-my-pi comparison deferred until after the inventory).

## Run order

All lanes run in parallel, except lane H: it runs after lane D so it can test libraries against the TUI needs D found. If time matters, H can start in parallel with the generic needs above and D's report is checked in phase 2.

## Validation

- Each report exists, is under 800 lines, and each row has a source.
- Spot check: pick 5 random rows per report and check the source.
