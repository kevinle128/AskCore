# Role

You are the Claude Code implementation sub-agent for AskCore H13a. The parent is coordinator-only. You own implementation, TDD, and implementation verification for Phase 1; do not perform or simulate the independent code-review step, which is reserved for a separate Codex sub-agent.

# Task

Load and follow the installed `ak-cook` skill. Execute **only Phase 1** of the accepted plan at `plans/261007-0700-h13a-acp-stdio/plan.md`, using the plan's required `--deep --tdd` behavior and **without `--yagni`**. Stop before Phase 2.

# Work context

- Workspace: `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther`
- Plan: `plans/261007-0700-h13a-acp-stdio/plan.md`
- Phase: `plans/261007-0700-h13a-acp-stdio/phase-01-start.md`
- Reports: `plans/reports/orchestrate-261007-073653/`
- Read first: root `AGENTS.md`, `docs/ask-architecture-reference.md`, `internal/README.md`, `internal/acp/README.md`, `pkg/protocol/README.md`, the plan and Phase 1, plus the linked SDK research and red-team reports.

# Existing worktree safety

This worktree was already dirty before your run. Existing changes include `cmd/tui/headless.go`, `internal/providers/catalog.go`, `internal/acp/host_test.go`, `internal/app/agent_native.go`, `internal/app/agent_native_test.go`, `pkg/protocol/acp.go`, `pkg/protocol/acp_test.go`, and the H13a plan/report files. Treat all as potentially user-owned. Inspect before editing; preserve and build on valid work. Never run reset, checkout, restore, clean, stash, rebase, commit, push, or delete unrelated files.

# Files you may modify

Primary Phase 1 ownership:
- `internal/acp/conformance_test.go` and narrowly related `internal/acp` Phase 1 support only when executable evidence requires it
- `pkg/protocol/acp.go`
- `pkg/protocol/acp_test.go`
- `go.mod` and `go.sum` only after the conformance gate selects an exact dependency through Go tooling
- `internal/acp/README.md`, `pkg/protocol/README.md`, and `AGENTS.md` only for verified durable dependency/contract navigation

Do not start Phase 2 files or broaden owner APIs unless a failing Phase 1 executable test proves the missing contract.

# Acceptance criteria

1. Preserve full H13a scope, ACP v1, one host/Agent per session design, and all explicit non-goals.
2. Resolve immutable SDK/schema identities independently and close D17 only with executable conformance evidence.
3. Implement lossless Ask wire DTOs and error contracts, including precision-sensitive metadata and safe optional/null behavior.
4. Cover every Phase 1 scenario with runnable tests, including `_meta` >2^53 precision, extension dispatch, concurrent reverse traffic/control, cancellation IDs, bounded ingress, outbound write faults, and malformed/duplicate input handling.
5. Use RED-to-GREEN evidence where behavior is new; do not label dependency/network/fixture failures as behavioral RED.
6. Preserve existing Agent/headless/auth contracts and do not implement a second protocol engine or advertise deferred capabilities.
7. Run the narrow Phase 1 checks first, then applicable race/leak and affected regression checks. Do not weaken or skip failing checks.
8. Do not mark the phase accepted/completed yet; Codex must review first.

# Constraints

- Real behavior only; no MockAgent proof, fake production tools, test-only product flags, or generated-file hand edits.
- No secrets, tokens, credential bodies, personal data, or raw provider errors in code, tests, logs, reports, or prompts.
- No network listener, leader/gateway/database/compaction/session-tree work.
- Keep changes scoped to Phase 1. If a material product decision is truly absent, stop with `NEEDS_CONTEXT`; otherwise execute autonomously.
- You may use network access only as necessary for immutable SDK/schema/module evidence and normal Go dependency resolution.

# Required final response

Report:
- files changed and why;
- selected SDK/schema identities, exact pins/sums/digests, and conformance result;
- RED then GREEN commands/results;
- all validation commands and exit outcomes;
- dirty pre-existing files left untouched;
- remaining limitations or blockers;
- concise handoff target for Codex review.

End exactly with:

`Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT`
`Summary: one or two sentences`
`Concerns/Blockers: optional`
