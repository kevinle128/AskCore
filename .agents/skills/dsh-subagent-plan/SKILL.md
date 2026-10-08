---
name: dsh-subagent-plan
description: "Coordinate ak-plan through native DSH Claude Code and Codex sub-agent tools while the controller only routes state and user decisions. Use when the user explicitly asks to create, validate, red-team, or archive an ak-plan workflow with Claude/Codex delegation. Not for implementation, provider setup, ordinary ak-plan runs without provider-specific delegation, or generic multi-agent work."
version: 1.0.0
---

# DSH Sub-agent Plan

Run the installed `ak-plan` workflow through provider-specific DSH sub-agents. `ak-plan` remains the authority for modes, flags, artifacts, gates, publishing, task hydration, handoff, and journal behavior; this adapter changes only who performs planning work.

## Required runtime contract

Before dispatching planning work:

1. Load `ak-plan`, resolve its selected mode and modifiers, and load only the references that route requires. Do not copy its mode rules into this skill.
2. Confirm the live tool catalog exposes both `subagent_claude_code` and `subagent_codex`. If either is absent, open [`references/recovery.md`](references/recovery.md) and stop before plan or source writes. When the selected route requires Kongming, also resolve the live named-agent delegation capability; fail closed for that route if it is unavailable.
3. Resolve the workspace, request or existing plan path, active plan state, reports path, dirty baseline, requested flags, user authority, and external or destructive boundaries.
4. Preserve every `ak-plan` user gate and completion condition. A provider handoff cannot waive a scope choice, validation interview, publishing authorization, or special-mode requirement.

Provider configuration owns models, reasoning settings, permissions, authentication, and executable versions. Never guess or override them here.

## Controller-only boundary

The controller may:

- load `ak-plan` and mechanically reject incompatible flags;
- resolve paths, plan state, dependencies, jobs, and provider availability;
- capture and compare non-mutating workspace content fingerprints around read-only reviews;
- dispatch the provider mapped to each live `ak-plan` role;
- present child-authored scope, approach, red-team, and validation choices to the user;
- forward user answers and reviewer findings without technical rewriting;
- hydrate runtime tasks mechanically from an accepted plan when the live task surface exists;
- report artifacts, verdicts, blockers, and the next `ak-plan` step.

The controller does not author or edit planning artifacts, inspect source to form planning conclusions, synthesize candidate plans, adjudicate findings, or modify product code. Delegate plan/report/artifact writes to Claude and independent review to Codex. Keep user decisions authoritative.

## Provider ownership

- **Claude Code** owns evidence gathering, repository scouting, architecture work, plan drafting, plan revisions, consistency repairs, and requested plan-side artifacts or publishing operations.
- **Codex** owns independent read-only red-team, fact checking, contract verification, validation, re-review, and a fresh adjudication pass that deduplicates findings, applies the evidence filter, and proposes dispositions before user review.
- **Kongming and other roles explicitly required by `ak-plan`** retain their original identity. In particular, do not replace `--ultra` or `--advice` Kongming duties with Codex.
- **The user** decides unresolved scope and architecture forks and reviews findings where `ak-plan` requires a decision.

Open [`references/provider-routing.md`](references/provider-routing.md) after mode resolution for mode-specific ownership. Open [`references/cycle-prompts.md`](references/cycle-prompts.md) before the first provider dispatch.

## Delegated planning cycle

1. **Establish the route.** Apply the live `ak-plan` mode, modifiers, scope rules, output location, and active-plan contract. For auto detection or a nontrivial scope challenge, ask Claude for evidence-backed analysis; the controller applies the route or presents the required user choice.
2. **Build evidence.** Dispatch the Claude research, scout, or planner roles required by the selected route. Read-only jobs may run in parallel when `ak-plan` calls for fan-out; give each job a distinct report path.
3. **Author the plan.** Dispatch one Claude writer to scaffold and fill the plan through the live `ak-plan` CLI contract. Its write scope is the active plan directory; product source is read-only.
4. **Run selected gates.** Dispatch Codex reviewers only where the live route requires red-team or validation. For red-team, use a fresh Codex adjudicator to collect, deduplicate, severity-sort, cap, evidence-filter, and propose Accept/Reject dispositions before the controller presents the required user choices. Preserve reviewer count, lenses, evidence rules, question budgets, and user review gates from `ak-plan`.
5. **Revise through Claude.** Forward accepted findings or confirmed answers to one Claude writer. After any revision, dispatch a fresh Codex re-review when the selected route requires it. The controller never resolves the finding itself.
6. **Finish the route.** Delegate HTML, GitHub/Wiki, archive, plan-state, and journal writes according to `ak-plan`; retain their authorization checks. Hydrate tasks only from the accepted durable plan.
7. **Handoff.** Report the exact plan paths, selected mode, provider passes, unresolved items, publication results, and the next step prescribed by `ak-plan`.

## Workspace integrity

Keep one plan writer active at a time. Parallel Claude candidates or researchers must be read-only or write to disjoint report files until a designated Claude writer materializes the accepted result. Codex is always read-only. Capture a content fingerprint that covers tracked, staged, and untracked workspace contents before each Codex call, compare it afterward, and invalidate the verdict on any mutation. Route plan-scope mutations to Claude for reconciliation; stop and report mutations outside Claude's authorized plan paths.

Do not instruct any child to reset, restore, checkout, clean, stash, rebase, commit, push, discard unrelated work, or edit product source. Commit, push, issue updates, public publishing, and merge remain separately authorized effects.

## Completion

Finish only when the selected `ak-plan` route is complete, every required user decision is recorded, every required reviewer gate has an accepted verdict, the whole-plan consistency sweep has no unresolved contradiction, requested artifacts exist, and no product source was modified. State explicitly that the controller performed orchestration only.

## Resources

- [`references/provider-routing.md`](references/provider-routing.md) — map live `ak-plan` branches to Claude, Codex, retained special roles, and user gates.
- [`references/cycle-prompts.md`](references/cycle-prompts.md) — self-contained templates for evidence, authoring, review, revision, and re-review calls.
- [`references/recovery.md`](references/recovery.md) — use when a tool or skill is missing, a child fails, review mutates files, or the cycle stops progressing.
- [`assets/eval-cases.json`](assets/eval-cases.json) — activation, preservation, and recovery cases for future evaluation.
