# Code review: H13a ACP over stdio (261007)

Build, vet, race tests, goleak and lint were green. Findings, none Critical:

- M1 (Medium) `internal/acp/host.go` Reset (~511-558): the epoch boundary comes from `Follow(Cursor{})` after `Agent.Reset` returns, and `releaseHeld` drops `s.mu` before it enqueues held events. A concurrent `_ask/session/steer` or `follow_up` (which skips the `resetting` flag) can start a run on the new epoch; its events get the old epoch tag, `state.steering` goes stale, and frames can leave out of order. Fix: refuse Steer/FollowUp with busy while `resetting`, keep admission closed until held events are enqueued, and add a real-Agent test with a concurrent Steer.
- M2 (Medium) `internal/acp/stdio.go` ~119-123, 200-206: the in-flight slot is released when the handler returns, but the SDK writes the response afterwards. On EOF the output can close before the prompt response is written. Fix: release the slot when the response frame is written (CheckedWriter.Observe).
- L1 `stdio.go`/`agent.go`: on EOF with a peer that stops reading stdout, shutdown can hang; bound the drain, then force-close output.
- L2 `agent.go` ~331-339: `session/cancel` right after `session/prompt` can run before the run binds and be lost; cancel the bound run or record a pending cancel.
- L3 `host.go` ~417-458: Prompt can return spurious busy while a queue_update is dispatched on an idle Agent; retry after WaitForIdle.
- L4 `updates.go` ~191-193: Unfollow with unknown subscription returns `unknown_session`.

Verified OK: prompt barrier, no-start failure, lock ordering, bounded queue, error mapping, no credential on wire, capabilities not overclaimed, headless unchanged, host.go/host_test.go intact after lint --fix.
