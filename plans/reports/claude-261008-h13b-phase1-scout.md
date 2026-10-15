# H13b Phase 1 scout report

Date: 2026-10-08. Branch `design-tui-bubbletea` @ `213afbe`. Route: `ak plan --deep --tdd`, scout only. No plan exists. No product file changed.

## Outcome

H13b is feasible on the current tree. `internal/leader` holds only a README and `doc.go`. Every other piece (`ask leader`, version probe, ACP client in `cmd/tui`, app link, depguard deny rule) is missing. The shared `acp.Adapter`/`Host` cannot serve many sockets as is. The recommended design is one internal ACP link owned by `internal/app`, with the leader as a router outside the adapter (xia §6.1). Five contracts in xia §13 still need maintainer approval before the plan is final.

Baseline: `git status` shows only user-owned untracked paths (`.agents/`, the xia report). One extra untracked file, `plans/reports/claude-261008-h13b-requirements-evidence.md`, appeared during the scout. It is not mine. I did not read or touch it.

## Verified source map

| Area | Evidence (file:line) | Finding |
|---|---|---|
| Roadmap exit | roadmap.md:468-492 | Exit: two `ask` TUI stubs share one auto-started leader. Order T0, H13a, H13b, T1a (roadmap.md:107). H13c out of scope. |
| Leader package | internal/leader/README.md, doc.go | No code. Planned files: server.go, client.go, lock.go, spawn.go. Allowed imports: stdlib, pkg/protocol, logs. README:24 says the ACP stream arrives as `io.ReadWriteCloser` built by `internal/app`. |
| Depguard | .golangci.yml `tui-client-boundary` (~26-46), `core-no-adapters` (~109-127) | Deny `internal/leader` and `internal/acp` for tui and core. No rule denies `internal/agent` or `internal/acp` inside `internal/leader/**`. Confirmed change. |
| Link framing | internal/acp/stdio.go:19,69-178 | `defaultMaxFrame` 8 MiB. `ServeStdio` builds Adapter, `LineLimitReader`, `CheckedWriter`, SDK `AgentSideConnection`, `Bind`. Internal link is NDJSON. It stops on input EOF. |
| Adapter | internal/acp/agent.go:57-80, 90-137, 142, 163-180, 201-210 | Adapter owns its Host. `Bind` is single-shot. `Fail` and `Close` are host-wide. `Initialize` stores one `atomic.Bool` and ignores the body. |
| Host | internal/acp/host.go:67-84, 122-142, 205-222, 340-356 | `latch` cancels runs on every session. Queue overflow latches the host. Client loss must never call these. |
| Extension methods | internal/acp/ask_methods.go:19-58 | All `_ask/*` use the global `gate()`. Compact, fork and tree are unsupported. |
| Follow | internal/acp/updates.go:100-228, 274-352 | Subscription map is global, keyed by `sub_<hex>`. `unfollow` is not owner-scoped. The leader must route by subscriptionId, owner only. |
| Wire | internal/acp/wire.go:19-89 | `CheckedWriter`, `LineLimitReader`. Reusable by app, not by leader (depguard). |
| Protocol | pkg/protocol/acp.go:10-29, 228-256 | Methods, DTOs, kinds, codes -32010..-32014. Leader kinds and codes extend this file. |
| App | internal/app/module_acp.go:24-122 | `ACPParams`, `ACPRuntime{Config, Cleanup}`, `NewACPRuntime`. No listener. Sibling `module_leader.go` fits. `module_acp_test.go` uses `fx.ValidateApp`. |
| CLI | cmd/tui/acp.go (115 lines), headless.go:38-54, 60+, args.go:146-148 | `runACP` is the pattern for `runLeader`. Subcommands dispatch before `parseArgs`. Unknown `--flag` is an error. `Info.Version` is the literal `"dev"`, so no version probe exists. |
| Interactive | cmd/tui/main.go:125-132 | Still a demo menu. No ACP client. The "TUI stub" must be a new real client in `cmd/tui`. |
| E2E pattern | cmd/tui/acp_e2e_test.go (`acpBinary`, `e2ePeer`, `e2eOptions`) | Builds the real binary once, isolated `ASK_HOME`, real pipes. Reuse for two client processes. |
| Settings | internal/settings/lock_unix.go:13-28, paths.go:11-53, auth.go:67-84 | Raw `syscall.Flock` plus `O_NOFOLLOW`. Path checks are private. `ASK_HOME` must be absolute. |
| SDK ids | acp-go-sdk@v0.13.5 connection.go:26,141-194,390-403,516,542-555 | Ids are `*json.RawMessage`, canonicalised for lookup. String ids from a rewrite are accepted. `$/cancel_request` is handled in the reader and keyed by canonical id. Rewrite must cover `requestId`. |
| x/sys | go.mod:394 (`// indirect`), cmd/tui/auth_input.go imports `x/sys/unix` | Already imported in code but marked indirect. `go mod tidy` was not run. Phase 1 needs `unix.GetsockoptXucred` (darwin) and `GetsockoptUcred` (linux). Check whether go.mod needs the direct marker. |
| Test tools | go.mod:32 goleak v1.3.0; internal/testsupport; internal/logs/logger.go | goleak is available. Logger is allowed for leader. |
| H13a review | plans/reports/code-review-261008-h13a-plan-compliance.md | Not read in full. Treat as open. |

## Change inventory

C = confirmed, K = candidate.

| Action | Path | Size | Test impact | Status |
|---|---|---|---|---|
| Edit | .golangci.yml (deny agent/acp/config in `internal/leader/**`) | S | `golangci-lint run` | C |
| New | internal/leader/{frame,handshake,ids,paths,lock}.go | M-L | unit, race, goleak | C |
| New | internal/leader/{server,router,client,spawn}.go | L | unit, race, E2E | C |
| Edit | pkg/protocol/ (new leader.go or acp.go) | M | protocol tests | C |
| New | internal/app/module_leader.go (+test) | M | `fx.ValidateApp` | C |
| Edit | internal/acp: per-client driver context, detach, live attach/take (agent.go, ask_methods.go, updates.go) | M-L | acp conformance, race | C |
| Edit | cmd/tui/headless.go, args.go (leader dispatch, usage) | M | cmd/tui tests | C |
| New | cmd/tui/leader.go, leader_client stub | M | E2E | C |
| New | cmd/tui leader E2E tests | M | binary build | C |
| Edit | go.mod / go.sum | S | `go mod tidy` | K |
| Edit | internal/acp/stdio.go (share link code) | S | acp tests | K |
| Edit | docs: architecture §7.3, leader README, app README, CLAUDE.md commands, cmd/tui usage | S-M | none | C |

## Recommended phase split

Phase 1 (full detail in the plan) is the transport and security foundation. Phases 2+ are outlines. Each needs a scout pass at cook time.

1. **P1 Foundations.** Frame codec (4-byte length, 8 MiB bound), handshake with hard version gate, raw-id rewrite table, secure path rules (0700 dir, 0600 socket, no symlink), flock with stable inode, peer-UID check, depguard rule, protocol DTOs. No router, no spawn.
2. **P2 Router.** Per-client initialize snapshot, session ownership and driver rules, pending-route table, scoped cancel, Follow ownership, bounded per-client FIFO with detach and resync.
3. **P3 App link.** `module_leader.go`, one internal ACP link, driver context, live attach/take/detach in `internal/acp`. Client loss never calls `Fail` or `Close`.
4. **P4 Spawn and CLI.** `ConnectOrSpawn`, binary lookup, version probe, `Setsid`, `--spawned-by-client`, log rotation, `ask leader list|status|stop`, shutdown frame, pid check before signal.
5. **P5 TUI stubs and E2E.** Two real client processes share one auto-started leader (the roadmap exit).
6. **P6 Reverse requests, shutdown, docs.** Eligibility and generation checks, driver-disconnect question cancel, regression for `ask acp` and headless, docs.

## Dependency map

P1 -> P2 -> P3 -> P4 -> P5 -> P6. P2 and P3 share the driver-context contract. P4 needs the P1 paths and lock. P5 needs P3 and P4. Protocol DTOs (P1) unblock all others. The idle-authority callback (P4 replace) needs P3.

## Test scenario matrix

| Level | Scenario |
|---|---|
| Critical | Peer with another UID is rejected. Socket and dir modes are 0600 and 0700. Symlinked path is refused. Version mismatch is rejected before any session call. Raw ids with large numbers and strings round-trip. Client loss does not latch or close the host. Follow events go to the owner only. Lock inode survives stop and restart. |
| High | Two clients get distinct ids for equal request ids. Scoped `$/cancel_request` hits the right route. Switch to a busy foreign session is rejected. `/new` changes the caller's view only. Driver disconnect cancels the open question. Frame over 8 MiB closes the client. Overflow detaches with resync. Stop sends shutdown first, then SIGTERM only after pid check. |
| Medium | Spawn race between two clients starts one leader. `ASK` binary lookup next to the caller, then `PATH`. Log rotation. Stale pid file. Socket path length on macOS (about 104 bytes). Goroutine leaks after close. |

## TDD per phase (Phase 1 shown in full)

Phase 1:
- Tests Before: frame round-trip and bound; handshake accept/reject by version; id rewrite with raw JSON; path mode and symlink checks; lock contention in two goroutines and a child process; peer-UID unit test with a fake credential source.
- Protected seams: `internal/acp` public API, `internal/settings` path helpers (do not export or edit them; copy the rule or move only with its own tests), `ServeStdio` behaviour.
- Tests After: depguard run proves the leader imports no agent or acp package; `ask acp` and `-p` stay listener-free.

Phases 2-6 repeat the same shape. Seams: Adapter/Host from P3 on, `cmd/tui` dispatch from P4 on.

## Verification commands (narrow to broad)

```
go test ./internal/leader/... -count=1
go test -race ./internal/leader/... ./internal/acp/... ./internal/app/... -count=1
go test ./pkg/protocol/... -count=1
go test ./cmd/tui/... -count=1          # E2E builds the binary; slow
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
go build ./... && go vet ./...
go test ./...
```

Setup: set a short temp `ASK_HOME` (macOS sun_path limit, use `/tmp/...`, not `t.TempDir()` when the path is long). A second-UID fixture is not available in CI, so use a fake credential source in unit tests and mark the real cross-UID check as manual. macOS and Linux use different peer-credential calls, so test both build tags or document the gap.

## Risks

- Socket path length on macOS.
- Lock inode must stay stable. Never unlink the lock file.
- JSON ids must stay raw, never float64.
- `ask acp`, headless and direct paths must stay listener-free.
- Adapter `Fail` on client loss would kill every client.
- NDJSON on the internal link versus length frames on the socket needs a clear conversion point (app side or a leader raw-JSON forward).
- The prompt write barrier ends at the SDK pipe. The leader adds a second output hop and needs an ordering contract.

## Unresolved assumptions

1. The five xia §13 contracts need maintainer approval: one internal ACP link with typed driver context; explicit live attach/take/detach/discovery with durable `session/load` staying unsupported; absent driver after disconnect with question cancel; router acceptance plus bounded per-client FIFO with detach and resync; 4-byte framing with raw nested JSON and final field and method names.
2. Where NDJSON and length framing convert.
3. Whether go.mod needs the direct `x/sys` marker.
4. Version identity source, because `Info.Version` is `"dev"`.
5. The idle-authority callback from app/ACP for replacement.
6. Aggregate limit defaults.
7. The H13a plan-compliance review was not read in full.

## Handoff to the plan writer

- Dir: `plans/261008-h13b-leader-unix-socket/` (rule `{date}-{issue}-{slug}`), files `plan.md`, `phase-01-foundations.md` (full), `phase-02-router.md` through `phase-06-reverse-shutdown-docs.md` (outline).
- Mode: deep, TDD, HOLD SCOPE. Hydrate tasks. Keep the journal step on.
- Carry from xia: file manifest §10, verification table §11, High risk score, exit gate, rollback.
- Code comments, file names and tests must not name plan or phase codes.
- Do not start P2+ detail until a cook-time scout pass runs.
- Docs impact: major. Update architecture §7.3, leader README, app README, CLAUDE.md commands and `cmd/tui` usage after the behaviour exists.
- Read the H13a compliance review before the final plan.
