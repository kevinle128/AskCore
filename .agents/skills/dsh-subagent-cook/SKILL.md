---
name: dsh-subagent-cook
description: "Coordinate phased implementation through native DSH Claude Code and Codex sub-agent tools. Use when the controller must delegate implementation to Claude and alternate independent Codex review without editing or reviewing product code itself; not for generic parallel delegation or provider setup."
version: "1.0.0"
---

# DSH Sub-agent Cook

Run an accepted implementation plan through a serial Claude–Codex delivery loop. The controller owns routing, state, and user communication only; Claude owns implementation and repairs, while Codex owns independent review and re-review.

## When to use

- The user asks to implement a feature or plan with Claude Code and review it with Codex through native DSH sub-agent plugins.
- The user explicitly requires the parent agent to coordinate only.
- A multi-phase change needs an implementation → review → repair → re-review gate before each next phase.
- Not for provider installation or DSH profile repair; use the DSH configuration guidance for that setup.
- Not for generic parallel research, same-model delegation, or work without an accepted implementation outcome.

## Required runtime contract

Before reading source or dispatching work:

1. Load the implementation skill that owns the requested work, normally `ak-cook`, and retain its flags, acceptance criteria, review cycle, and finalization rules.
2. Confirm the live tool catalog exposes both `subagent_claude_code` and `subagent_codex`.
3. If either tool is absent, stop before implementation. Do not substitute generic `subagent`, `subagent_fork`, `workflow`, Paseo, or a direct CLI invocation. Open [`references/recovery.md`](references/recovery.md) and report the missing tool.
4. Confirm the workspace, plan or task, current phase, reports path, dirty baseline, authorized write scope, and destructive/external boundaries.

Provider configuration owns exact models, reasoning settings, permission modes, authentication, and executable versions. Do not guess or override those values from the skill.

## Controller-only boundary

The controller may:

- resolve plan state and phase dependencies;
- construct self-contained delegation prompts;
- dispatch one implementation or review sub-agent at a time;
- track job IDs, results, phase gates, and no-progress evidence;
- compare changed-file names before and after a read-only review;
- communicate blockers and decisions to the user.

The controller must not:

- edit, format, generate, or delete product code, tests, documentation, dependencies, or plan state;
- run implementation tests as a substitute for the implementation agent;
- inspect code to produce its own review verdict;
- rewrite or silently dismiss a Codex finding;
- switch to another runtime when a native DSH provider fails.

Delegate plan-state and documentation writes to Claude when they are part of finalization. Coordination records may be maintained by the controller only when they contain no product changes or review conclusions.

## Serial delivery loop

Open [`references/cycle-prompts.md`](references/cycle-prompts.md) before the first dispatch. Fill every placeholder with exact paths and accepted requirements.

### 1. Establish the phase gate

- Reuse the accepted outcome, constraints, non-goals, dependencies, and acceptance criteria.
- Resolve the earliest phase that lacks an accepted Codex verdict.
- Preserve existing dirty files as potentially user-owned. Never instruct an agent to reset, restore, checkout, clean, stash, rebase, commit, push, or discard unrelated changes.
- Keep one writer active in the workspace. Do not overlap Claude implementation or repair jobs.

### 2. Delegate implementation to Claude

Call `subagent_claude_code` with the implementation template. Require Claude to:

- load the owning implementation skill;
- inspect current source and prior accepted evidence;
- implement only the authorized phase;
- perform its own TDD and verification obligations;
- stop before independent review;
- return changed files, test evidence, blockers, and the status protocol.

A `BLOCKED` or `NEEDS_CONTEXT` result stops the phase. Ask the user only when the missing decision cannot be resolved from repository evidence.

### 3. Delegate independent review to Codex

After Claude settles, record the changed-file baseline and call `subagent_codex` with the review template. Require read-only review of the actual workspace revision, accepted plan, affected callers, public contracts, security, regressions, and executed checks.

Compare changed-file names after Codex returns. Any review-time workspace mutation invalidates that review; use the recovery procedure instead of accepting its verdict.

### 4. Reconcile findings through Claude

- `PASS`: record the acceptance evidence and advance to the next dependency-ready phase.
- `FIX_REQUIRED`: send the exact findings and evidence to Claude with the repair template. Claude decides code changes from source evidence, fixes supported findings, explains unsupported findings, and reruns affected checks.
- `BLOCKED`: stop and report the concrete condition.

After every repair, call a fresh Codex sub-agent with the re-review template. Codex must verify each finding disposition and the cumulative phase acceptance criteria from source, not from Claude's summary.

If the same supported finding survives two repair cycles with no new evidence, stop as no-progress. Report the repeated finding and exact checks rather than silently changing scope or runtime.

### 5. Finalize the complete plan

After every phase passes:

1. Delegate final broad verification, docs impact, and plan-state reconciliation to Claude.
2. Delegate one final cumulative read-only review to Codex.
3. Route any supported final findings through the same Claude repair → Codex re-review loop.
4. Finish only when all required checks have evidence, no required finding remains, and the accepted outcome is satisfied.
5. Commit, push, publish, or merge only when separately authorized.

## Job handling

- Prefer foreground calls when the next action depends immediately on the result.
- For long jobs, use the provider tool's background mode, record every returned job ID, and wait for completion notifications; do not poll.
- Collect every relevant job result before reporting completion and cancel jobs that have become irrelevant.
- Never start the next phase while its prerequisite review is unresolved.

## Completion report

Report:

- phases accepted and their Codex verdicts;
- Claude implementation and repair handoffs;
- checks actually run by each sub-agent;
- findings fixed, rejected with evidence, or still blocked;
- remaining setup, verification, or authorization gaps;
- confirmation that the controller made no product-code or review changes.

## Resources

- [`references/cycle-prompts.md`](references/cycle-prompts.md): fill-in templates for implementation, review, repair, and re-review calls.
- [`references/recovery.md`](references/recovery.md): open when tools are missing, a child fails, a review mutates files, or progress stalls.
- [`assets/eval-cases.json`](assets/eval-cases.json): routing and behavior cases for validating future changes to this skill.
