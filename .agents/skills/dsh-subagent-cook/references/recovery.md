# Recovery rules

Open this reference only when normal dispatch cannot continue.

## Required provider tool missing

If `subagent_claude_code` or `subagent_codex` is absent from the live tool catalog:

1. Stop before source edits.
2. Report the exact missing tool and that the DSH profile has not exposed the required provider-specific delegation surface.
3. Do not fall back to generic `subagent`, `subagent_fork`, `workflow`, Paseo, or direct Claude/Codex CLI execution.
4. Ask the user to repair or reload the DSH profile through its owning configuration workflow. A changed profile requires a restarted session before the tool catalog can be trusted.

The implementation skill does not install providers, alter global profiles, grant version exemptions, or change authentication.

## Provider call fails before child start

- Record the tool error and whether a child/job ID was created.
- Treat the workspace as unchanged unless evidence shows otherwise.
- Retry once only after the evidenced configuration or transient cause changes.
- Do not retry the same failing prompt unchanged or switch runtimes silently.

## Claude stops after partial writes

- Preserve the dirty workspace; never reset or discard it.
- Dispatch a fresh Claude run with the original phase contract, the captured failure, and instructions to inspect, validate, and continue existing work.
- Codex review remains gated until Claude returns implementation and check evidence.

## Codex review mutates the workspace

- Compare changed-file names with the pre-review baseline.
- Invalidate the verdict even when it says PASS.
- Preserve the mutation for evidence. Do not let the controller edit it away.
- Dispatch Claude to inspect and reconcile the unexpected mutation within scope, then run a fresh Codex review.

## Background job loses contact

- Keep the job ID and collect its durable result before another writer starts.
- If the job is still running, wait for its completion notification rather than polling.
- If it is definitively failed or cancelled, follow the provider-failure or partial-write branch above.

## No progress

Stop when the same supported finding remains after two Claude repair cycles without new source or executable evidence. Report:

- the repeated finding;
- both repair dispositions;
- checks and outcomes;
- the smallest missing decision, contract, or runtime capability.

Do not weaken tests, broaden scope, approve the finding, or replace the required provider.

## User decision changes

If a review recommendation conflicts with an explicit user decision, present the original decision, review concern, trade-off, and concrete options. Wait for the user rather than silently reversing scope.
