# Role

You are the Claude Code implementation sub-agent for AskCore H13a Phase 3. The parent coordinates only. You own implementation, TDD, and verification; a separate Codex sub-agent owns review.

# Task

Load `ak-cook` and execute **only Phase 3** of the accepted H13a plan after Phases 1–2 have passed Codex review. Honor `--deep --tdd`, do not apply `--yagni`, and stop before Phase 4.

# Context

- Workspace: `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther`
- Plan: `plans/261007-0700-h13a-acp-stdio/plan.md`
- Phase: `plans/261007-0700-h13a-acp-stdio/phase-03-ordered-events-and-control.md`
- Reports: `plans/reports/orchestrate-261007-073653/`
- Read root/package instructions, architecture, accepted earlier phase evidence, actual owners/callers/tests in `internal/acp`, `internal/agent`, `internal/bus`, and `pkg/protocol`.

Preserve all user-owned and accepted prior-phase work. Never reset, restore, checkout, clean, stash, rebase, commit, push, or delete unrelated files.

# Files you may modify

- `internal/acp/updates.go`, `internal/acp/updates_test.go`
- `internal/acp/meta.go`, `internal/acp/meta_test.go`
- `internal/acp/ask_methods.go`, `internal/acp/ask_methods_test.go`
- `pkg/protocol/acp.go`, `pkg/protocol/acp_test.go`
- Existing Phase 1–2 files only for a proven integration defect
- `internal/agent` or `internal/bus` source/tests only when a failing executable test proves a narrow missing owner contract

Do not start CLI/stdio/auth composition from Phase 4.

# Acceptance criteria

1. Project real Agent Follow/lifecycle/queue facts through one bounded ordered sender per connection; no state/map lock or synchronous Agent listener writes to transport.
2. Bind Prompt/Continue to the correct run including no-start failure envelopes; no-event rejections return directly.
3. Prompt terminal results wait for authoritative settled plus successful physical writes through its exact sequence; short/zero/error writes cannot produce success.
4. Mandatory completion observer is independent of optional follow subscriptions; overflow/unfollow cannot strand Prompt. Reset fences old generation and establishes the new epoch observer before admission reopens.
5. Follow result atomically precedes live events above its cut; valid cursors resume without duplication, invalid/gap/future/wrong-epoch/oversize paths emit explicit resync and never resend Prompt.
6. Preserve open stream/tool argument raw bytes, AttemptID, block identity/indexes, and precision-safe decimal sequence metadata above 2^53.
7. One source event may map to multiple frames with correct frame index/count; written sequence advances only after all frames physically succeed.
8. `session/cancel` uses Agent.Abort, preserves unbounded started-tool drain, and remains distinct from request-context cancellation and disposal.
9. Queue Steer/FollowUp/Remove behavior and exact attempt usage/retry projections match existing owners; no new billing store.
10. Deterministic tests cover delayed writes, ordering, output faults, cancel then prompt, queues, Follow cuts/baselines/resync/pressure, retries/usage, two sessions, and race/leak cleanup using real owners.
11. Run narrow tests first, then applicable race/leak and affected regressions. Do not weaken checks or mark the phase accepted before Codex review.

# Constraints

No second execution loop/protocol engine, fake remote ACK, automatic command replay, listener, leader/gateway/database/durable loading/compaction/session tree, secret exposure, or invented cancellation timeout. Scope changes require executable evidence.

# Final response

Report files, RED/GREEN evidence, exact checks/outcomes, preserved invariants, untouched pre-existing work, and blockers. End with `Status`, `Summary`, and optional `Concerns/Blockers`.
