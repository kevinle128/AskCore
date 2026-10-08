# Claude–Codex cycle prompts

Use these templates as structure, not as literal boilerplate. Replace every bracketed field and remove irrelevant branches. Every delegation must remain self-contained because native DSH provider runs are fresh one-shot sessions.

## Shared context block

Include this block in every call:

```text
Role: [implementation | review | repair | re-review]
Workspace: [absolute workspace path]
Plan/task: [exact path or complete task]
Current phase: [exact path/name]
Reports path: [exact path]
Accepted earlier evidence: [exact paths or concise facts]
Dirty baseline: [changed file names and ownership warning]
Scope flags: [for example --tdd, --deep, no --yagni]
Authority: [what the user authorized]
Forbidden actions: reset, restore, checkout, clean, stash, rebase, commit, push, and unrelated deletion
Sensitive-data boundary: do not expose credentials, tokens, private data, or raw provider errors
```

End every prompt with:

```text
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Summary: one or two sentences
Concerns/Blockers: optional
```

## Claude implementation

```text
You are the implementation owner. The parent coordinates only, and independent review belongs to Codex.

[shared context]

Load and follow [owning implementation skill]. Read [project instructions, architecture, package docs, plan, phase, prior evidence, affected callers/tests]. Implement only [phase]. Preserve existing valid work and all unrelated dirty files.

Files you may modify:
- [exact owned paths]

Acceptance criteria:
1. [observable criterion]
2. [observable criterion]
...

Run the narrowest useful RED/GREEN checks, then affected regression, race, leak, lint, build, or integration checks required by risk. Do not weaken checks, fake behavior, review your own work, update acceptance status, or begin the next phase.

Return changed files and reasons, RED/GREEN evidence, exact checks and outcomes, preserved contracts, blockers, and a concise Codex handoff.
```

## Codex review

```text
You are the independent read-only reviewer. Never edit, format, generate, delete, rename, commit, or update plan state.

[shared context]
Claude handoff: [verbatim implementation result]

Review the actual cumulative workspace revision against [plan and phase]. Verify source and tests instead of trusting the handoff. Cover acceptance, affected callers, public contracts, security, regressions, repository conventions, and [phase-specific risk list]. Run only non-mutating checks.

Return one verdict:
- PASS: no actionable finding remains and acceptance evidence is sufficient.
- FIX_REQUIRED: include severity-ranked findings with file/line evidence, impact, and required fix.
- BLOCKED: state the concrete condition and missing evidence.

Distinguish checks you ran from checks Claude reported.
```

## Claude repair

```text
You are the implementation repair owner. The parent coordinates only; Codex will re-review.

[shared context]
Codex findings: [verbatim structured findings]

Re-read the actual source and acceptance contract. Repair every supported in-scope finding without expanding scope. If a finding is unsupported, provide source or executable evidence instead of silently dismissing it. Preserve unrelated and accepted work. Rerun checks affected by each repair and the phase gate. Do not perform independent review or begin the next phase.

Return a finding-by-finding disposition, changed files, exact checks and outcomes, blockers, and a concise re-review handoff.
```

## Codex re-review

```text
You are a fresh independent read-only re-reviewer. Never modify the workspace.

[shared context]
Original findings: [verbatim findings]
Claude repair handoff: [verbatim repair result]

Verify every disposition against source and checks, then re-evaluate all cumulative phase acceptance criteria. PASS only when no actionable finding remains. Otherwise return FIX_REQUIRED or BLOCKED with current file/line evidence.
```

## Final cumulative gate

Use the same templates, replacing the phase boundary with the complete accepted plan. Claude owns broad verification, docs impact, and plan reconciliation. Codex owns the final cumulative read-only verdict.
