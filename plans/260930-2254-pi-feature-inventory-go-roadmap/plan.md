---
title: "Pi feature inventory and Go rewrite roadmap"
status: roadmap-ready-awaiting-decisions
priority: P1
created: 2026-09-30
---

# Pi feature inventory and Go rewrite roadmap

Note: the user CLAUDE.md asks for `tasks/todo.md`. The session hook allows markdown only in `plans/` and `docs/`, so the plan is here.

## Outcome

1. A complete inventory of Pi features: big features, settings, flags, commands, events, hooks, and edge cases. Each item has a source (doc file, source file, or changelog version).
2. The history of Pi from the changelogs: what was added, what was removed or deprecated, and what broke the extension API.
3. A survey of Go libraries for the TUI, and for the other hard parts (extension runtime, LLM SDKs, MCP).
4. A roadmap for the Go rewrite in AskCore. Priority: harness core, extension system, own TUI.

## Constraints

- Initial inventory reference: Pi at `/Users/dale/Desktop/workspace/opensources/pi`, commit `2bbfcca4`, version `0.99.1`.
  Provider follow-up uses `4c6fb7cfe` (1.0.1), as recorded in roadmap revision 5 and the source audits.
- Changelogs: `packages/coding-agent/CHANGELOG.md` (5979 lines, 281 releases, 0.10.0 on 2025-11-25 to 0.99.1), `packages/ai` (2203), `packages/tui` (1214), `packages/agent` (754), plus 8 small ones.
- Prior research to reuse (verify, do not copy): `/Users/dale/Desktop/workspace/opensources/ask/plans/reports/researcher-260930-*.md` (12 reports, including Pi agent core, Pi extension system, Goose vs Pi).
- Target stack: AskCore package map in `internal/README.md`; `go.mod` pins `bubbletea v1.3.10` + `lipgloss v1.1.0`, with `charm.land/lipgloss/v2` indirect (known conflict note in `go.mod`).
- TUI stack: the Charm stack. bubbletea (`github.com/charmbracelet/bubbletea`) is the framework, Lip Gloss does styling, Glamour renders markdown, Bubbles gives components. The user decided this on 2026-09-30. Charm's Go coding agent Crush uses the same stack, so it is the main design reference. Crush is licensed FSL-1.1-MIT, so read its code but do not copy it. Research asks how to build Pi's TUI on this stack, not which library to use.
- Reports: English, max 800 lines each. Chat with the user: Vietnamese.
- This plan produces documents only. No Go code.

## Non-goals

- Code walkthrough of Pi. The goal is a feature-level inventory, not an explanation of each file.
- Web UI for chat and control (out of scope). The read-only monitoring dashboard is in scope (see Confirmed decisions).
- Implementation of any roadmap phase.

## Method: spiral scouting

Each lane scans its area in rings. It goes to the next ring only after the current ring is complete. It stops when a full ring finds no new items (saturation).

| Ring | Question |
|---|---|
| 0. Core | What is the main job of this area? What is the main flow? |
| 1. Features | Which features does the user or the extension author see? |
| 2. Surface | For each feature: settings keys, CLI flags, env vars, slash commands, keybindings, events, API names. |
| 3. Interactions | How does each feature interact with others (session, compaction, extensions, TUI, RPC mode)? |
| 4. Edge cases | Failure modes, limits, platform quirks, and bug fixes in the changelog (`Fixed` entries). |
| 5. History | When was it added, changed, removed? Did it break the extension API? |

## Phases

| # | Phase | Status |
|---|---|---|
| 1 | [Research lanes (parallel)](./phase-01-research-lanes.md) | Done (10 reports in `plans/reports/researcher-260930-2254-*.md`) |
| 2 | [Synthesis: inventory, roadmap, Go library choice](./phase-02-synthesis-roadmap.md) | Done: `inventory-harness.md`, `inventory-extensions.md`, `inventory-tui.md`, `timeline.md`, `roadmap.md` (revision 2 after review) |

## Confirmed decisions (user)

| Date | Decision |
|---|---|
| 2026-09-30 | TUI uses the Charm stack: bubbletea, Lip Gloss, Glamour and Bubbles. Crush is the design reference. |
| 2026-09-30 | Run the 10 research lanes. The oh-my-pi comparison waits until after the inventory. |
| 2026-09-30 | Add one feature from waku-agent (`opensources/waku-agent`, MIT): "Watch it think. A local dashboard lights up every message as it flows through the harness." Distill it and place it in the roadmap. Report: `plans/reports/researcher-260930-2254-waku-watch-it-think.md`. |
| 2026-09-30 | D12 (roadmap numbering): build a web dashboard for monitoring only ("Web để phục vụ cho một số monitoring"). Scope is narrow: a read-only "Watch it think" dashboard, embedded static files served by the daemon, localhost + token, no chat and no control. The TUI inspector stays. This narrows the earlier "no web UI" rule. `CLAUDE.md`, `AGENTS.md` and `docs/ask-architecture-reference.md:9` are updated. |
| 2026-10-01 | D1 = A ("theo PI"): Pi runtime semantics inside the confirmed dewee package model. `agent` runs Pi's two-level loop, `pipeline` holds the turn's hook points, `sessions` is an entry tree, and the queues are steer and follow-up. |
| 2026-10-01 | D2 = B ("File như Pi"): credentials in `~/.ask/auth.json` (0600, file lock, read-merge-write) and settings in `~/.ask/settings.json` + project `.ask/settings.json`, all files as in Pi. The user confirmed B twice. |
| 2026-10-01 | **Superseded by the later D3 row (no built-in policy in M1).** D3 = A (the user first picked C, then changed to A: "tôi đổi ý rồi, A"). H5 to H10 run tools without approval, as in Pi. H11 adds a first-party `tool_call` policy handler (allow, deny, ask from settings) in `tools/policy.go`. |
| 2026-10-01 | D4 = A: a deadline only for out-of-process hooks (X1). On expiry `tool_call`/`user_bash` fail closed; other events are logged. Compiled-in Go handlers get no deadline. This is a documented departure from Pi 0.31.0. |
| 2026-10-01 | Extensions have two kinds ("vậy sẽ có 2 loại internal extension và external extension"). Internal: Go compiled into Ask, for Ask developers (the self-upgrade use was dropped later: level 2 = no). External: loaded at run time without rebuilding Ask, for end users and for Ask's own self-extension. Both share one event contract. No-code extension (skills, templates, MCP, shell hooks) stays. The runtime for external extensions is D13, asked again. This replaces the short-lived "plugins are internal only" answer from the same night. |
| 2026-10-01 | D13 = B only: external extensions are Go source that Ask builds on load into a separate binary (cached) and runs as a child process over stdio. Written with a public SDK in `pkg/`. No TypeScript or WASM runtime. Needs the Go toolchain on the machine. |
| 2026-10-01 | D5 (Grok model, "TUI có chế độ headless. khi mở TUI thì sẽ kết nối qua leader, còn headless thì call thẳng agent"): `cmd/tui` = `ask`. The interactive TUI connects to the leader (first recorded as `cmd/server`; **superseded** by the next row: the leader is `ask leader`). Headless (`ask -p`) runs the agent in process. The rule "`cmd/tui` imports no `internal/*`" is removed: the user never chose it ("ai bảo không được?"); Claude had inferred it from the word "client" in scaffold decision 2. Removed from `CLAUDE.md`, `internal/README.md`, `docs/ask-architecture-reference.md` and the depguard rule `tui-is-client`. |
| 2026-10-01 | Leader runs from the `ask` binary (`ask leader`, option A, Grok model). The TUI and the daemon are both clients of the leader; headless calls the agent directly. |
| 2026-10-01 | D16: ACP (JSON-RPC, JSON) + `_ask/*` is the only external protocol of the agent (local leader, remote agent, editors). Inside the process and headless: direct Go API. The user asked whether Grok uses protobuf; research showed protobuf only for OTLP telemetry and an unused tools proto, never on the agent path. Design in `docs/ask-architecture-reference.md` section 7.3. |
| 2026-10-01 | Self-improvement: level 1 yes (Ask writes skills, templates, external extensions in M1, and MCP config and shell hooks in M2, for itself, then `/reload`; its prompt points to its own docs). Level 2 no (Ask does not rebuild or swap its own binary). |
| 2026-10-01 | D6 = B: Anthropic + OpenAI-compatible + OpenAI Responses at launch; Google, Bedrock, Mistral later (H17). |
| 2026-10-01 | Self-extension level 1 includes Go code ("Có thể tự viết extension bằng Go và build"): Ask may write an external Go extension for itself; `/reload` builds and loads that extension. Ask itself is never rebuilt by Ask. |
| 2026-10-01 | D7: `ask` / `~/.ask` (`ASK_HOME`) / `.ask` / `ASK_*`; binaries `ask` and `ask-server`. |
| 2026-10-01 | Rule: must-have first, every nice-to-have later ("chúng ta tập chung làm must have trước, tất cả những cái nice to have để sau"). Applied to D8 (no Windows at launch), D9 (API keys only), D10 (no crash-resume), D11 (no jitter for now), D15 (inline TUI only). |
| 2026-10-01 | Milestone M1 (must-have): Phase 0, H1-H13, H14 session tree, H15, W1 dashboard, X1 external Go extensions, T0, T1. M2 (nice-to-have): H16 MCP, X2 shell hooks, H17, T2, X3, Windows. The user picked W1 and H14 as must-have; MCP and shell hooks were not picked. |
| 2026-10-01 | D3 changed again: no permission popups ("PI No permission popups, Chúng ta cũng sẽ chưa làm nhé") and no built-in policy handler in M1. To block a command, write an external extension. The policy handler moves to M2. |
| 2026-10-01 | Codex review blocker B1: keep M1 small. `/login` (API key and OAuth) and extension UI dialogs are M2. In M1 the API key comes from an env var or `~/.ask/auth.json`; X1's M1 exit drops the `confirm` dialog. |
| 2026-10-01 | `transformMessages` = option C: the pure function, H-PROV-27 and the replay part of H-TOOL-21 move from H4 to H3, because the H3 Anthropic adapter is the first caller (`AI:api/anthropic-messages.ts:1057`). H4 keeps the per-vendor tool-call id rules and the cross-API replay tests. Source: `plans/261001-0836-h1-messages-events-faux/plan.md` section 6. |
| 2026-10-05 | H4 port analysis (`plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md`), one question per turn. Targets A: Token Plan `/compatible-mode/v1` for Completions (live probe run), OpenAI `gpt-5.5` for Responses. Patch fantasy ("sửa fantasy, chúng ta có source mà"); D22 stays; fork through a `go.mod` `replace` to `github.com/kevinle128/fantasy`. `OPENAI_API_KEY` only. Thinking level as Pi (clamped level only). Package shape: "fantasy là ở tầng infrastructure, các adapter sử dụng cái gì là việc của adapter": one adapter for each wire API, `fantasykit` for shared plumbing, vendors as data. No id-collision guard, as Pi. H4 renders mid-conversation system messages. Cost "làm giống PI" in H4. Switch shown by Go tests only. Pi reference moves to `4c6fb7cfe`. Roadmap revision 5, inventory section 19, providers README and architecture reference updated. |
| 2026-10-06 | Distribute the provider interview across phases because H4 cannot deliver it all. H4 owns wire/replay; H5 Pi builtin contracts; H7 catalog/configuration and one-credential store; new H7a native Anthropic/ChatGPT/xAI auth, profiles and minimal headless login/logout after H7. H8/H9/H13 integrate persistence, retry and switching; H17/T2 retain breadth and full gateway/TUI auth. This supersedes Q1's H4 timing and the older API-key-only scope, not accepted behavior. |
| 2026-09-30 | The user's goal is to learn how to build a harness. The roadmap puts harness features first. Extension and TUI phases come after the harness core. Each harness phase must teach one harness concept and end with a working, testable result. |

## Verified findings (lock)

- The shipping session store is `SessionManager` JSONL v3. Verified by `pi/packages/coding-agent/src/core/session-manager.ts:41`. The v4 `AgentHarness` store is used only in `src/experimental`. That code is excluded from the build (`tsconfig.build.json:19`) and gated by `PI_EXPERIMENTAL=1` (`src/core/experimental.ts:1`).

## Current provider planning

[Roadmap revision 7](./roadmap.md) owns the current allocation: subscription delivery now precedes full H5-H7 because Alibaba Token Plan expires soon.
[H7a execution detail](./phase-h7a-subscription-auth.md), its [deep TDD implementation plan](../261006-0157-h7a-subscription-auth/plan.md), and the [provider design](../261005-2139-provider-auth-design/plan.md) define subscription delivery.
The H4 report is a cross-phase decision record, not an H4 implementation checklist.

## Acceptance criteria

- [ ] Every doc in `packages/coding-agent/docs/` (39 files) is covered by the inventory.
- [ ] Every release in the coding-agent changelog is in the timeline (grouped by version range).
- [ ] Removed or deprecated features are listed, so the Go rewrite does not rebuild them.
- [ ] Every extension hook/event, and every UI primitive given to extensions, is listed with its source.
- [ ] Edge cases are grouped by area, with the changelog version of the fix.
- [ ] Each of the 14 Pi packages is classified: core harness, extension-adjacent, or out of scope, with a reason.
- [ ] The bubbletea report answers: v1 or v2 to pin, and how to build inline scrollback mode + fixed bottom editor, overlays, and the custom editor. Each gap has a workaround or a custom component.
- [ ] The Go extension-runtime recommendation compares the realistic options, with trade-offs.
- [ ] The roadmap maps each phase to AskCore packages and lists the inventory items it delivers.

## Open questions

- None before approval. Questions found during research go at the end of each report.

## Review (2026-09-30)

- 2026-10-01: an independent Codex review (`plans/reports/codex-261001-0131-roadmap-review.md`) found 1 blocker, 10 major and 3 minor issues. The major and minor fixes are applied to `roadmap.md` and `docs/ask-architecture-reference.md` (contracts for lease, replay, sessions and drivers, spawn from two binaries, version mismatch, extension build and reload, ACP conformance; M1-only T0 gate; H13 split into three runnable steps). The blocker (M1 needed `/login` and extension dialogs from M2) was resolved by the user: both stay in M2.

- Outputs: 10 lane reports and 1 waku report (`plans/reports/researcher-260930-2254-*.md`), 3 inventories (harness about 200 rows, extensions with 41 events, TUI 177 rows), `timeline.md` (14 stages, 15 lessons), `roadmap.md`.
- An independent review of the roadmap (`plans/reports/code-reviewer-260930-2254-roadmap-review.md`) found 3 blockers and 9 major issues. All are fixed in roadmap revision 2 (roadmap section 8). The main fixes: gateway authentication (the scaffold defaults to `0.0.0.0` and CORS `*`, verified by `internal/config/config.go:40,45`), P0 coverage, phase order, and a new storage decision.
- A script checks the coverage: 110 of 110 P0 harness rows have an owner phase, and all 47 P1 rows are placed or deferred.
- The user decides the next step, D1 to D15 in roadmap section 1, one per turn. A narrow re-check of revision 2 (review report, last section) found no blocker. Its consistency findings (decision order, "Waits on" lines, H14 to H17 content, forward uses) are fixed in revision 3. No code was written.
- The acceptance criteria in this file are met. Exceptions: the "edge cases grouped by area" rows are keyword-based (lane C). The bubbletea inline behavior is not proven yet; the T0 gate proves it.
