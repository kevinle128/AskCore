# Recovery rules

Open this reference only when the normal delegated `ak-plan` route cannot continue.

## Required tool or owning skill missing

If `ak-plan`, `subagent_claude_code`, or `subagent_codex` is unavailable, or a selected special route cannot spawn its required named agent:

1. Stop before plan or source writes.
2. Name the missing skill or tool and explain that the provider-specific adapter cannot preserve its contract.
3. Keep existing plan and dirty workspace state unchanged.
4. Route profile or provider repair to its owning setup workflow; a changed tool catalog requires a fresh session before it is trusted.

Do not substitute a generic subagent, workflow fan-out, direct provider CLI, or another runtime for Claude Code or Codex. Invoke a named special agent such as Kongming only through the live delegation path required by `ak-plan`; if that path is missing, stop the special route.

## Child cannot load `ak-plan`

Treat the run as unusable because the child cannot verify the current planning contract. Preserve any report as diagnostic evidence, prevent it from becoming plan authority, and stop for runtime repair. Do not paste a stale local summary and claim equivalent behavior.

## Provider call fails before start

Record the error and whether a child or job ID exists. Treat the workspace as unchanged unless evidence shows otherwise. Retry only after the evidenced configuration or transient condition changes; never switch providers silently.

## Claude stops after partial plan writes

Preserve the plan directory. Dispatch a fresh Claude writer with the original route, partial-write evidence, allowed paths, and instructions to inspect and continue the current artifacts. Codex review remains gated until Claude returns a complete handoff.

## Codex changes the workspace

Compare the post-review content fingerprint with the baseline covering tracked, staged, and untracked contents. Any mutation invalidates the verdict, including PASS. Preserve the mutation as evidence. If every mutation is inside Claude's authorized plan paths, dispatch Claude to reconcile it and then run a fresh Codex review; if any mutation falls outside those paths, stop and report it rather than authorizing a product-source repair.

## Candidate or reviewer slot fails

Follow the selected `ak-plan` route's usable-candidate and retry rules exactly. Do not shrink a required pool, relabel a partial run, or let the controller simulate the missing role.

## Background job loses contact

Keep the job ID and collect its durable result before starting a conflicting writer. Wait for the completion notification rather than polling. If the job definitively failed or was cancelled, follow the relevant provider or partial-write branch.

## No progress

If the same supported finding survives two Claude revision cycles without new source or executable evidence, stop as no-progress. Report the repeated finding, both dispositions, current plan paths, and the smallest missing decision or capability. This bound prevents an unproductive writer-reviewer loop; it does not authorize weakening the gate.

## User decision conflicts with review

Present the original decision, reviewer concern, trade-off, and concrete options. Wait for the user. Neither the controller nor a child may silently reverse an explicit scope or architecture decision.
