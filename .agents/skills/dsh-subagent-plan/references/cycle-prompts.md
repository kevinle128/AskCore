# Claude–Codex planning prompts

Use these templates as structures, not literal boilerplate. Replace every bracketed field and remove irrelevant branches. Native provider calls are fresh sessions, so each prompt must be self-contained.

## Shared context

Include in every call:

```text
Role: [evidence | planner | synthesizer | reviewer | adjudicator | validator | reviser | re-reviewer | artifact writer]
Workspace: [absolute path]
Owning workflow: installed ak-plan skill
Selected route: [mode or subcommand plus modifiers]
Request or plan: [complete request or exact plan path]
Active plan directory: [exact path]
Reports path: [exact path]
Accepted evidence and decisions: [paths and concise facts]
Dirty baseline: [changed file names and ownership warning]
Authority: [authorized writes and external effects]
Product source: read-only; planning writes stay within [exact allowed paths]
Forbidden actions: reset, restore, checkout, clean, stash, rebase, commit, push, unrelated deletion, and product implementation
Sensitive-data boundary: do not expose credentials, tokens, private data, or raw provider errors
```

End each prompt with:

```text
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Summary: one or two sentences
Artifacts or evidence: exact paths
Concerns/Blockers: optional
```

## Claude evidence pass

```text
You own evidence gathering for an ak-plan route. The parent coordinates only.

[shared context]

Load ak-plan and only the references required by [selected route]. Read repository instructions and current source, tests, manifests, and owning documentation. Perform the scope, research, or scout work assigned by that route. Do not write plan files or product source during a read-only evidence pass. Write only [disjoint report path] when a persisted report is required.

Return evidence with file:line citations, unresolved assumptions, candidate user decisions, and an exact handoff to the plan writer.
```

## Claude plan author or synthesizer

```text
You are the sole plan writer for this stage. The parent coordinates only; independent review belongs to Codex.

[shared context]
Evidence inputs: [exact report paths]
Candidate inputs, when synthesizing: [exact paths]

Load ak-plan and the selected route references. Use its live CLI scaffolding and generated-file read pass. Write only the authorized plan directory. Produce every artifact, phase field, acceptance criterion, verification command, dependency, and handoff required by the selected route. Perform the planner self-verification required by ak-plan, verify cited paths and symbols from source, and mark unresolved claims exactly as ak-plan requires. Do not modify product source, implement the plan, issue an independent gate verdict on your own work, or advance beyond this planning stage.

Return created or changed plan files, evidence used, unresolved decisions, and a concise Codex handoff.
```

## Codex review or validation

```text
You are the independent read-only reviewer for an ak-plan route. Never edit, format, generate, delete, rename, publish, or update plan state.

[shared context]
Claude handoff: [verbatim result]
Assigned ak-plan lens or verification role: [exact live role]

Load ak-plan and the selected review or validation references. Review the actual plan directory and verify claims against the actual source, tests, callers, contracts, and repository rules rather than trusting the handoff. Follow the live evidence format, severity rules, question budget, and consistency-sweep contract.

Return one verdict:
- PASS: the assigned gate is satisfied with sufficient evidence.
- REVISION_REQUIRED: include structured findings with plan location, file:line evidence, impact, and required correction.
- BLOCKED: state the concrete missing capability or evidence.
- NEEDS_USER_DECISION: provide the exact question and bounded options required by ak-plan.

Distinguish checks you ran from evidence Claude reported.
```

## Codex red-team adjudicator

```text
You are a fresh independent read-only adjudicator. The parent coordinates and presents decisions; it does not judge findings.

[shared context]
Reviewer outputs: [verbatim outputs from every required persona]

Load ak-plan's red-team and verification contracts. Collect all findings, deduplicate overlaps, sort and cap them exactly as the live route requires, reject evidence-free findings, and propose Accept or Reject dispositions with concrete rationale. Do not edit files or replace the user's review gate.

Return the child-authored findings table, counts, proposed dispositions, and the exact bounded choices the controller must present to the user.
```

## Claude revision

```text
You are the plan revision owner. The parent coordinates only; Codex will re-review.

[shared context]
Codex findings or user decisions: [verbatim structured payload]

Load ak-plan and re-read the actual plan and source evidence. Apply every accepted in-scope finding or confirmed answer across all affected plan files. If a reviewer claim is unsupported, record source evidence for fresh Codex verification instead of silently dismissing it. Run the whole-plan reconciliation work assigned to the writer, preserve unrelated work, and do not implement product code.

Return finding-by-finding dispositions, changed plan files, unresolved contradictions, and a concise re-review handoff.
```

## Codex re-review

```text
You are a fresh independent read-only re-reviewer.

[shared context]
Original findings or decisions: [verbatim payload]
Claude revision handoff: [verbatim result]

Verify each disposition against the current plan and source, then re-run the assigned ak-plan gate and whole-plan consistency checks. Return PASS only when no required finding or contradiction remains; otherwise return the current structured verdict with evidence.
```

## Claude final artifact pass

Use the author template with the final accepted plan as input. Limit writes to the plan directory or explicitly authorized publication state. Generate only artifacts and state transitions required by the selected `ak-plan` route; do not change accepted technical decisions during presentation or publishing.
