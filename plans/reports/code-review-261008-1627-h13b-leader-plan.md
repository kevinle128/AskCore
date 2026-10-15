# H13b plan review

## Verdict

H13b is not implemented on this tree.
All six phase files have `status: pending`.
`internal/leader` contains only `README.md` and `doc.go`, and `go test` reports no test files.
The command dispatcher in `cmd/tui/headless.go` has no leader or connect command.
The corrected plan covers the H13b roadmap scope, but it does not certify its runnable exit.

The user requested review and repairs to the plan, tests and solution.
This review repairs the plan and supplies a runnable SDK prototype check.
It does not treat this request as an instruction to build the entire pending feature.

Scope authority: [roadmap H13](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h13-leader-acp-adapter-and-gateway-pis-rpc-mode-multi-client).
Reviewed plan: [H13b](../261008-1033-h13b-leader-unix-socket/plan.md).

## Findings and repairs

| Severity | Finding and evidence | Repair |
|---|---|---|
| Critical | The installed SDK `connection.go` `receive` uses a fixed 10 MiB scanner maximum. Phase 1 and the plan promised 64 MiB with only a reader wrapper. A large client message could end the common agent connection. | Keep the user's 64 MiB decision. Add a parser patch gate, 65 MiB internal line budget for route expansion, and tests in both directions. A temporary-copy buffer patch is verified by the runnable probe. |
| High | The mismatch handshake closed the connection, but Phase 4 required a shutdown handshake on that incompatible connection. | Keep ACP rejected. Define a bounded, version-stable status and conditional idle-shutdown control path, with expected instance identity. |
| High | Generation checks did not specify atomic mutation admission or concurrent take result ordering. Existing `Session` admission and Agent calls are separate operations in `internal/acp/host.go`. | Use the mutation admission lock for generation checks and commits. Serialize takes until router state reflects the host result. Add mutation, auth, factory and shutdown race tests. |
| High | `DropClient` discarded routes before late Follow, new-session or take results could be processed. `internal/acp/updates.go` makes a successful Follow a live host resource. | Retain cleanup records, release orphan follows, and prevent an absent caller from becoming a live driver. Preserve idempotent Unfollow with owner checks. |
| High | Implicit subscribe included detach and cancellation traffic. The plan also had no rule for old queued events after reattach. | Subscribe only after validated session operations. Exclude detach and transport-control paths. Add membership generations and a detach acknowledgment fence. |
| High | Shared-question replay did not add an attaching client to the eligible response set. Driver-loss cancellation mentioned only driver-only questions. | Use one reverse table for replay and completion. Add eligibility with replay atomically, validate answers, and cancel questions for the lost driver generation. |
| High | The NDJSON bridge could forward a multiline raw JSON message, and one shared writer was not explicit. The outgoing envelope bound was also unspecified. | Compact JSON to one line. Serialize complete writes to the common link. Enforce 64 MiB on the fully encoded socket envelope before writing bytes; isolate an outgoing size failure to its recipient. |
| High | The PID fallback specified a UID and command snapshot only, which cannot by itself prevent PID reuse between check and signal. | Require verifiable process identity; use a stable handle when available. Fail closed when a safe target cannot be established. Keep control shutdown as the normal path. |
| Medium | `driverGen` was numeric inside SDK `_meta`, which decodes numbers as float64. `internal/acp/README.md` already requires decimal text for Ask counters. | Use decimal text and test values above 2^53 and malformed metadata. |
| Medium | Binary probes, losing-child log rotation, private-open side effects, terminal handle ownership and client input during a prompt had no precise tests. | Add explicit deadlines and reaping, owner-only rotation, safe type checks, terminal ownership, and interactive cancel tests in the owning phases. |
| Medium | Phase 5 tests depended on reverse and shutdown behavior deferred to Phase 6. | Move ownership of pending-question and tool-skew acceptance evidence to Phase 6; Phase 5 lists them only as final requirements. |
| Medium | The plan still referred to a full queue closing a client and a live-session bound. The journal still said slow clients close. | Remove stale claims and preserve the accepted unbounded socket FIFO and aggregate policy. |

## Maintenance assessment

The package boundary is sound: the leader routes bytes, ACP owns Agent mapping, and app owns composition and lifetime.
Keep one host, one generation authority, one reverse table and one method-policy table.
Keep client writes outside router state transitions.
These rules reduce duplicate state and make races testable without a new service layer.

The SDK patch must have reproducible source, a license record and an update procedure before production integration.
Do not edit the module cache or use a machine-local replacement in the product.
The SDK upstream main branch checked during this review also has the fixed 10 MiB maximum: [connection.go](https://github.com/coder/acp-go-sdk/blob/main/connection.go).
Changing to an unverified newer source is not a demonstrated solution.

The socket FIFO remains unbounded because the user selected it.
A client that remains connected but does not read can retain unbounded memory.
Sessions also remain allocated until leader stop because disposal is outside H13b.
These are accepted limitations, not defects silently removed by this review.
The existing H13a host event-queue safeguards remain in place.

## Verification performed

Go commands used `GOCACHE=/private/tmp/ask-h13b-review-cache` because the default cache is outside the writable sandbox.
The initial default-cache command failed with `operation not permitted`; the rerun used the writable cache and passed.

| Check | Result |
|---|---|
| `go test ./internal/leader/... ./pkg/protocol/... ./internal/acp/... ./internal/app/... -count=1` | Protocol, ACP and app passed. Leader has no test files. |
| `go test -race ./internal/acp/... ./internal/app/... ./pkg/protocol/... -count=1` | Passed. |
| `go vet ./internal/leader/... ./internal/acp/... ./internal/app/... ./pkg/protocol/...` | Passed. |
| `go build -o /private/tmp/ask-h13b-review ./cmd/tui` | Passed. |
| Built `ask leader --help` and `ask connect --help` with an isolated home | Both show general help; neither has its own command dispatch. No home artifact was created. This is baseline evidence, not leader acceptance. |
| SDK parser probe | Installed SDK accepts 9 MiB and rejects 11 MiB. The buffer-only temporary patch accepts 11 MiB, 64 MiB and 65 MiB lines, then rejects 65 MiB plus one byte. |
| Independent read-only plan review | Found the missing outgoing envelope bound. Added it and the recipient-isolation tests. |

Reproduce the parser experiment from the repository root:

```sh
python3 plans/261008-1033-h13b-leader-unix-socket/artifacts/verify-sdk-frame-limit.py
```

The probe uses the actual installed SDK connection with pipes and a method handler.
It edits only `connection.go` in a temporary copy and deletes that copy afterward.
It does not change generated source, installed dependencies or production code.
It proves the parser change, not a 64 MiB prompt through the future leader.

## Coverage conclusion and remaining gates

Existing ACP tests do not establish leader routing, socket security, spawn or shared process lifetime.
The plan now maps each roadmap leader requirement to its owner and required evidence.
No H13b coverage percentage or completion claim is valid before implementation.

1. Select and pin the production SDK patch, then rerun conformance against it.
2. Execute the route-meta spike and the six phases.
3. Prove the two-built-client exit and all roadmap leader rows, including stale-event and cursor behavior.
4. Record Linux runtime peer and filesystem checks; cross-compilation alone is insufficient.
5. Keep version-skew and reverse-tool transport fixtures separate from production tool evidence.

Unresolved questions: none for this plan review.
Production SDK provenance and unexecuted acceptance tests remain implementation gates.
