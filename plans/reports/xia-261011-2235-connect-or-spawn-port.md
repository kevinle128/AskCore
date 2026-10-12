# Xia: Grok `connect_or_spawn` into Ask (`--port`)

Date: 2026-10-11. Mode: `--port`, stopped at the challenge gate (one scope decision open).

## Source manifest

| Item | Value |
|---|---|
| Source | `/Users/dale/Desktop/workspace/opensources/grok-build`, commit `2bdd1d6a`, Apache-2.0 (port behavior only; keep the notice if code is copied) |
| Scope | `crates/codegen/xai-grok-shell/src/leader/mod.rs:1376-1722` (`connect_or_spawn` and helpers), `leader/lock.rs`, `leader/transport.rs` |
| Source anatomy | [xia-261011-grok-connect-or-spawn-source.md](xia-261011-grok-connect-or-spawn-source.md) |
| Local anatomy | [xia-261011-ask-connect-or-spawn-local.md](xia-261011-ask-connect-or-spawn-local.md) (`internal/leader/client.go`, `spawn.go`, `lock.go`, `cmd/tui/leader.go`) |
| Local authority | [H13b leader plan](../261008-1033-h13b-leader-unix-socket/plan.md): user decision "Version gate: A" (line 65), non-goals (line 54) |

## Head-to-head

| # | Behavior | Grok | Ask today | Status |
|---|---|---|---|---|
| 1 | Binary for a new leader | `current_exe()`; managed symlink for a managed install (`mod.rs:1581-1601`) | Caller's own binary when the caller is ask (`CallerIsAsk`), else `ask` next to the caller or on PATH (`client.go` `leaderExecutable`) | **Ported 2026-10-11** (tests `TestConnectOrSpawnStartsTheCallerWhenItIsAsk`, `TestTUIStartsLeaderFromABinaryWithAnotherName`). Managed symlink: not applicable, Ask has no managed install or auto-update. |
| 2 | Election | `flock` on a spawn lock; spawner releases it before spawn (`mod.rs:1421-1463`) | Lifetime `flock` held by the leader; spawned leaders race for it, the loser exits (`TestLeaderE2ESpawnRace`) | Equivalent outcome: one leader. No port. |
| 3 | Leader identity | PID in lock file; `/proc/locks` and a name check (Linux only) | Owner record (PID, start time, instance, exe path); process check by command line and start time | Ask is stricter. No port. |
| 4 | Fast path with a dead PID | Skip connect when the PID is dead (`mod.rs:1390-1401`) | Dial fails, socket is treated as absent | Equivalent. No port. |
| 5 | Readiness wait | Poll 100 ms, 10 s budget (`mod.rs:1699-1722`) | Poll 25 ms inside a 20 s deadline; dial 1 s, register 3 s, ready 5 s | Equivalent. No port. |
| 6 | New leader dies early | Retry spawn up to 3 times (`MAX_SELF_SPAWN_ATTEMPTS`) | No retry; error shows the log tail (`TestConnectOrSpawnStartupFailureShowsTheLog`) | Gap (small). Option below. |
| 7 | Older leader still running | Semver floor: client newer than leader, then `RelaunchForUpdate` or SIGTERM, wait 8 s (`mod.rs:1099-1179`) | Replace only on a protocol-integer mismatch, only an idle leader that a client started; a different build gives a hint only | **Conflicts with user decision H13b "Version gate: A".** Ask build identity is a VCS revision, which has no order, so a semver floor cannot be ported as is. |
| 8 | Zombie leader (holds the lock, socket not connectable) | After 30 s: SIGTERM, 8 s, SIGKILL, 8 s; 3 tries per PID (`mod.rs:1195-1371`); needs `/proc/locks`, so Linux only | Waits to the deadline and returns `ErrLeaderStarting` every time (`TestConnectOrSpawnLiveLockNoSocketIsBounded`) | Gap. Listed as a non-goal in H13b (line 54). Linux only in both; on macOS Ask refuses signals without a stable process handle (H13b risk table, line 205). |
| 9 | Hung leader (accepts, never registers) | No recovery; documented gap (`mod.rs:2034`) | Register timeout, then error | Same gap in both. No port. |
| 10 | Connect failure at startup | Error to the user (`xai-grok-pager-bin/src/main.rs:729-731`) | In-process agent fallback (T1a decision) | Different by decision. Separate question, not part of this port. |

## Challenge

- Necessity: the user problem (`go run` could not start a leader) came from row 1, which is now ported and proven. Rows 2-5 already have equivalents.
- Conflict: row 7 reverses a user decision and does not fit Ask's unordered build identity.
- Platform: row 8 helps on Linux only; the user works on macOS, where both Grok and Ask cannot evict safely.
- Dev workflow caveat (both projects): after a code change, `go run` adopts the leader that still runs the old code, because both report the same protocol and an unordered dev build. Today the fix is `ask leader stop`.

## Decision matrix

| # | Decision | Source's way | Local way | Risk if wrong | Recommendation |
|---|---|---|---|---|---|
| D1 | Leader binary | current exe | ported | low | Done |
| D2 | Version floor eviction | semver, kills busy leader | protocol gate, idle only | critical (kills background jobs; reverses a user decision) | Keep Ask; do not port |
| D3 | Zombie eviction | SIGTERM/SIGKILL after 30 s, Linux | none | critical (signals a process) | Ask the user; Linux only |
| D4 | Spawn retry | 3 tries | log tail, no retry | low | Optional with D3 |

Risk score: 2 critical assumptions (D2, D3), low if D2 stays out.

## Unresolved

1. Scope after D1: stop here, or add D3 (Linux zombie eviction) and D4 (spawn retry)? Needs the user, because D3 was an H13b non-goal.
