# H13b repair verification

Date: 2026-10-09.
Scope: the repairs approved after the [independent review](code-review-261009-1030-h13b-leader-unix-socket.md).
The earlier report remains the record of the defects before repair.

## Repairs

| Contract | Repair and evidence |
|---|---|
| Driver generation | A separate session commit lock orders take against mutations. Agent prompt admission and model commit check ownership after waits and readiness callbacks. Controlled races cover every driver mutation and the cached run status before take. |
| Follow order | The socket reader registers Follow ownership before it wakes the request waiter. A built client receives an event and resync immediately after the Follow response. |
| Forced exit | The command joins runtime cleanup only before Serve starts. A second signal bypasses a started tool drain through the same command lifecycle helper. |
| Detach and take | Detach fences a pending take even when the caller had no membership. A late result updates the host generation but installs no driver. |
| Idle admission | Authenticate counts as host work. The router counts accepted requests until the common link returns a result, including requests from a client that has left. |
| Socket ownership | Unix listeners have automatic unlink disabled. The lock holder removes only the recorded socket inode through its held private directory. |
| Auth cleanup | Native auth cleanup starts alongside Agent disposal and tool drain. A graceful stop joins both. |
| Process safety | Linux uses a pidfd and checks the lock owner again before signaling. macOS refuses PID fallback because it has no stable process handle. Normal socket control stop remains available. |
| File safety | Lock, log, rotation, and socket cleanup use validated directory descriptors. Concurrent private log creation and absent-home management have regression checks. |
| Race gate | The foreground SIGTERM test reads child stderr only after Wait joins the output writer. |

## Added acceptance evidence

- A real socket, server, SDK adapter, and Agent carry an 11 MiB prompt and update in both directions.
- A socket envelope of exactly 64 MiB crosses the real adapter with maximum legal driver capabilities.
- An oversized client is closed while another client continues through the same host and link.
- Direct router transformation checks accept 65 MiB exactly and refuse one extra byte after route and ID rewriting.
  A legal 64 MiB socket envelope plus the 64 KiB capabilities cannot reach that internal boundary; the direct check is supplemental evidence.
- A real SDK permission request and a real host run share one link.
  Driver loss cancels the pending question and keeps the run active until the provider finishes.
  Product permission tool owners remain outside this plan.
- The built Follow burst and the composed second-signal check fail with the old implementation and pass with the repairs.
- The old cached-status take check fails under a controlled interleaving and passes after status is read under the commit lock.

## Verification

| Check | Result |
|---|---|
| `go test ./... -count=1 -timeout=10m` | PASS; CLI 84.035 s |
| `go test -race ./internal/leader ./internal/acp ./internal/app ./internal/agent ./pkg/protocol ./cmd/tui -count=1` | PASS; CLI 123.769 s, leader 56.298 s, app 38.204 s |
| Six original review checks with the overlay | PASS in all four owning packages |
| Real-host question and late-answer check, `-race -count=10` | PASS |
| Hung probe startup/cancellation check, `-race -count=20` | PASS |
| Linux arm64 leader test binary in `alpine:3` | PASS, full leader suite |
| `go build ./...`, `go vet ./...` | PASS |
| `golangci-lint run ./...` | PASS, 0 issues |
| `git diff --check` | PASS |

The full race gate ran before the last two test-only improvements.
Those tests then passed their focused repeated race checks, and the full repository suite passed with the updated probe test.
The final question assertion selects different options for the old and current request IDs, so it proves which request resolved the SDK call.
The probe test waits for a positive child PID before cancellation and joins the child on failure.
The first full-suite retries exposed a missing ENOENT error cause and a startup timing assumption; both are fixed with regression checks.

The independent repair reviewer found no remaining concrete production defect.
See the [ACP repair record](worker-261009-1057-acp-commit-fencing.md) and [router safety record](worker-261009-1057-router-safety.md).
Logs are in [h13b-repair-artifacts](h13b-repair-artifacts/).
The original report artifacts retain the before-repair logs and the red checks for Follow and forced exit.

## Scope and remaining manual evidence

The accepted queue, orphan busy take, version gate, and UID test decisions remain in place.
H13c, durable sessions, and T1a rendering remain outside this repair.
The literal second-user connection remains a manual check under the accepted UID evidence decision.
Linux tests use an arm64 test binary in an Alpine container; they do not claim a Linux race-detector run.
No commit or publication was requested.
