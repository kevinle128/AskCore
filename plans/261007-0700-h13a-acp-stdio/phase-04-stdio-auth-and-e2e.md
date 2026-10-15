---
title: "Phase 4: Stdio, native auth, and full E2E"
status: completed
---

# Phase 4: Stdio, native auth, and full E2E

## Outcome and requirements

Register built ask acp, compose shared auth and session owners, and prove every promised operation at real stdio.
Preserve the complete H13a scope and public Agent semantics.
Use real existing owners; never implement a second execution loop or advertise deferred capabilities.
No implementation is done during planning.

## File inventory

| Action | Absolute path | Change |
|---|---|---|
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/stdio.go` | SDK connection and owned stdio shutdown |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/stdio_test.go` | SDK connection and owned stdio shutdown |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/acp.go` | CLI registration and real built subprocess acceptance |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/acp_test.go` | CLI registration and real built subprocess acceptance |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/acp_e2e_test.go` | CLI registration and real built subprocess acceptance |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/app/module_acp.go` | Editor fx composition and ValidateApp |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/app/module_acp_test.go` | Editor fx composition and ValidateApp |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/headless.go` | Dispatch acp before prompt/mode parsing; help/regressions |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/args.go` | Dispatch acp before prompt/mode parsing; help/regressions |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/cmd/tui/args_test.go` | Dispatch acp before prompt/mode parsing; help/regressions |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/README.md` | New command and no-listener composition guidance |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/app/README.md` | New command and no-listener composition guidance |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/README.md` | New command and no-listener composition guidance |
| MODIFY | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/AGENTS.md` | New command and no-listener composition guidance |

No existing file is deleted unless constructor code moves within its existing owner.
Generated SDK files are inspected, not manually edited.
Only create a helper when its responsibility requires a real boundary.

## Test baseline and caller protection

Counts are source declarations from the phase scout, not passing test results.
ACP has 0 existing tests; all ACP conformance, adapter, sender, and stdio checks below are missing.
Protocol has 114 tests; Agent 345; bus 13; app 13; auth 25; providers 94; sessions 11; CLI 81.
Read owning package tests before changes and reuse their real fixtures.

| Existing call surface | Production / test lexical sites | Protection |
|---|---:|---|
| Prompt / Continue / Abort | 1/271; 0/8; 1/58 | Preserve busy, state, cancellation and settled behavior |
| WaitForIdle / Dispose / Reset | 0/9; 1/52; 4/16 | Never dispose from sync listeners; preserve writer/epoch lifecycle |
| SetModel / SetThinkingLevel | 0/17; 0/4 | Preserve readiness atomicity, clamp and idle boundary |
| Steer / FollowUp / Remove / Follow | 0/9; 0/9; 2/12; 1/26 | Preserve queue claim, input IDs and consistent snapshot cut |
| NewNativeAuth / BindAuth / AuthWait | 2/0; 1/8; 2/0 | Preserve private auth transport and refresh drain |
| newHeadlessAgentWithAuth / runWithDependencies | 2/0; 1/2 | Move shared construction without changing direct headless behavior |
| parseArgs / runAuth / openProvider | 1/1; 1/0; 1/0 | Dispatch ACP explicitly; no raw auth stdin on protocol connection |

These counts are lexical navigation bounds, not type-resolved call graphs.
Before edits, read every typed caller in `cmd/tui`, `internal/app`, `internal/agent`, and their tests.
Do not alter Agent or bus public APIs unless a failing executable check proves the missing contract.

## Function protection checklist

- [x] Resolve typed callers and existing fixtures before changing a shared owner.
- [x] Protect Prompt busy and completion semantics, Abort versus Dispose, and unbounded started-tool drain.
- [x] Protect queue ID/claim behavior, Follow snapshot/open-stream baseline, and epoch identity.
- [x] Keep native auth/provider callbacks in app and preserve direct headless behavior.
- [x] Check cancellation and output errors at their actual physical transport seam.

## Dependency map

Phase 1 SDK → Phase 2 app/session host → Phase 3 sender/follow → cmd runWithDependencies dispatch → app.NewNativeAuth/BindAuth → ask acp stdio.
One framed reader owns stdin; one protocol writer owns stdout; diagnostics go only to stderr.
Auth HTTP is private and outside inference capture.
Authentication uses already configured credentials and host-side CLI login, as confirmed by the user.

## Shutdown contract

EOF or output failure closes owned I/O to unblock SDK writers/readers, closes admission, cancels the editor-owned host, and joins Dispose/follow/sender/auth tasks.
A blocked SDK write must be interrupted by closure of its owned pipe/file; cancellation alone is not proof of shutdown.
Native refresh uses existing AuthWait, separate from inference/tool drain.
Ordinary session/cancel has no forced process deadline and waits for real started-tool drain.
For SIGINT/SIGTERM/SIGHUP, first attempt graceful cleanup; a second signal can force the process exit with the conventional code and explicit stderr indication that cleanup did not drain.
Forced exit must never publish completed prompt work or claim graceful disposal.
Do not copy Grok's fixed EOF delay or headless two-second grace as normal cancellation semantics.
For future H13b, connection detach closes only client followers; the shared host is owned by leader composition.

## Confirmed user decisions

1. Question: “H13a nên dùng contract ACP nào? ACP v1 trả kết quả khi prompt hoàn tất, phù hợp roadmap hiện tại; v2 draft trả acknowledgement ngay rồi báo hoàn tất qua sự kiện.”
   Options: “ACP v1 cho H13a (Recommended)” / “Chuyển sang ACP v2 draft”.
   Answer: “ACP v1 cho H13a (Recommended)”.
2. Question: “Với nhiều session trong một process, bạn chọn cách nào? Hiện mỗi Go Agent của Ask giữ một conversation; một host có thể quản lý nhiều Agent, mỗi Agent thuộc một session.”
   Options: “Một host, Agent riêng mỗi session (Recommended)” / “H13a chỉ một session; mở rộng ở H13b”.
   Answer: “Một host, Agent riêng mỗi session (Recommended)”.
3. Question: “Auth ở H13a qua stdio nên hỗ trợ tới đâu? M1 đã để giao diện /login ở M2; ACP có thể dùng credential đã đăng nhập bằng CLI, hoặc thêm luồng login tương tác qua editor.”
   Options: “Dùng credential sẵn có; login bằng CLI (Recommended)” / “Thêm login tương tác qua ACP ở H13a”.
   Answer: “Dùng credential sẵn có; login bằng CLI (Recommended)”.

4. Review question: apply no-start completion, Reset observer replacement, configured authMethodId constraints, and supplemental stdio composition proof for started-tool drain.
   Options: apply all four / review each correction / keep the draft.
   Answer: apply all four (Recommended).

## Scenario matrix

| Scenario | Required assertion |
|---|---|
| Built command | bare acp dispatches before positional prompt; no terminal demo or headless mode selected |
| Every public operation | matrix in plan.md through built ask stdin/stdout, real Agent/app/auth/bus/protocol |
| API key/native saved auth | supported host login, readiness and expired-credential refresh; same H7a security/host checks |
| Auth interaction decision | configured method/readiness validation and CLI guidance; no interactive login |
| Private credentials | no AuthSnapshot/token in DTOs/errors/capture; raw stdin never used by runAuth inside ACP |
| Provider retry/failure | real adapters with external HTTP fixture; safe error and exact usage, no duplicate admission |
| EOF/output error | close admission, cancel owned host work, drain tools, close followers, join sender/SDK tasks |
| Signals | SIGINT=130, SIGTERM=143, SIGHUP=129; deterministic owned process wait |
| No listener | editor/headless fx validation and actual child inspection: no database/leader/server/listening socket |
| Headless compatibility | auth commands, -p text/json, direct API, exit codes and no database behavior preserved |

## Tests Before (RED)

Add parser dispatch tests before registration; current bare acp becomes a positional prompt.
Build the normal production ask binary and launch it on pipes from Go E2E tests.
Implement the whole runtime-flow matrix using actual standard and extension request bytes.
Use external provider/OAuth HTTP fixtures for production subprocess E2E; any endpoint seam must reuse supported configuration, not a test-only CLI flag.
<!-- Accepted red-team correction: supported drain fixture. -->
The shipped tool registry has Echo only, so held inference proves inference cancellation but not started-tool drain.
Retain production subprocess TestACPE2ECancel for inference cancellation.
Add TestACPStdioToolDrain in internal/acp/stdio_test.go through the same production stdio handler, SDK, host, app factory, Agent, and executor with real pipes.
Inject only a registered controlled tool through the existing composition seam; wait for body start, send session/cancel, assert no settled/result, release the body, then assert written updates precede the cancelled result.
This is supplemental stdio integration proof, not production-binary certification of a shipped blocking tool.
Likewise exercise a real faulting session writer through composition for the no-start execution failure; retain production binary Continue-rejection and later-prompt cases.
No internal owner is mocked and no production test flag or extra shipped tool is added.
Prepare credentials through supported native login/service flows in a temporary ASK_HOME, with file permissions and redaction checks.
Run narrow checks first and save the precise failing assertion and command.
A timeout from a broken fixture or inaccessible dependency is not behavioral RED.

## Refactor (GREEN)

Register acp before mode selection, like the existing auth command.
Compose session factory/model/auth callbacks through app; do not import raw config/auth transport into ACP.
Standard authenticate validates already configured authMethodId/readiness through app callbacks and returns actionable host-side CLI guidance.
Do not advertise interactive login or reuse runAuth input callbacks inside the stdio connection.
Reuse app.NewNativeAuth, BindAuth, and AuthWait; raw credential-bearing providers.AuthSnapshot never becomes a wire interaction object.
Editor connection EOF/output failure disposes its exclusively owned host; future leader detach only closes followers and never disposes shared Agents.
Use existing independent AuthWait 20-second auth drain; never apply headless two-second grace to ordinary session cancellation.
On output failure, latch failure and do not write a successful prompt response; clean owned tasks without treating cleanup bytes as content recovery.
Reuse standard library and selected SDK facilities before introducing abstractions.
One synchronous Agent listener must never block on wire output or disposal.

## Tests After

Run each production-binary plan.md E2E method and all reachable error/control variants against the built child.
Run the disclosed supplemental stdio composition tests for started-tool drain and failing-log no-start execution.
Run Reset immediately followed by Prompt, with and without optional follow, and reject both auth-method mismatch directions without credential mutation or inference.
Assert request IDs, sessions, stdout framing, stderr diagnostics and exact settled/write ordering.
Check malformed-input SDK diagnostics with a secret sentinel and own/redact SDK logging.
Check no tokens in stdout, stderr or inference captures; retained artifact redaction must preserve useful safe assertions.
Run actual native auth/inference fixtures, existing direct headless regressions, fx.ValidateApp, race, lint and build.
Record real-provider access limits instead of substituting a fake internal provider.
Wait for every started child; do not kill user-owned processes.
Every test has an owner, cancellation context, and bounded fixture cleanup.
Ordinary tool drain remains contractually unbounded; the test waits for its deliberate real release rather than inventing a product timeout.

## Regression commands

Commands name proposed tests and files; they do not claim those tests exist today.
Run from the primary worktree with a task-owned cache if the environment needs one.

```sh
go test ./cmd/tui ./internal/app ./internal/acp -run 'TestACP|TestHeadless|TestAuth|Test.*Validate' -count=1
go test -race ./internal/acp ./internal/app ./internal/auth ./internal/agent ./cmd/tui ./pkg/protocol -count=1
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
go build -o /private/tmp/ask-h13a ./cmd/tui
```

## Success criteria

- [x] All listed scenarios have runnable tests and saved results.
- [x] New behavior has valid RED-to-GREEN evidence; initially correct controls retain their PASS result.
- [x] Existing Agent/headless/auth public contracts remain intact.
- [x] Planned external behavior is wired through the real built ask acp boundary in final acceptance.
- [x] No silent lifecycle loss, precision loss, secret exposure, or capability overclaim remains.
- [x] Narrow tests, applicable race/leak checks, regression gates, and owned-process cleanup pass.

## Risk signal and response

Signal: stdout diagnostics, stdin auth competition, secrets in frames, a listener appears, or a hanging owned process.
Response: keep H13a incomplete, correct dispatch/composition/ownership, and rerun the full built-process matrix.

## Rollback

Cancel and join only phase-owned children/readers/SDK connections.
Restore only this phase's reviewed source/dependency diff after checking for user or other agent edits.
Keep failing frame traces and immutable schema/source identities for the next attempt.
Do not weaken existing checks, replay an ambiguously admitted prompt, or modify generated files to conceal a failure.

## Execution evidence (2026-10-07)

### RED commands and failing assertions

1. Dispatch (behavioral RED, before registration). Command: `go test ./cmd/tui -run 'TestACP|TestUsageNamesACP' -count=1`. Result: `TestACPDispatchHelp` failed because `ask acp --help` printed the main usage ("does not contain \"ask acp\""), and `TestACPDispatchRejectsArguments` failed because `ask acp extra` exited 0 and treated `extra` as a prompt (`expected: 1 actual: 0`). Saved output: scratchpad `red-args.txt` of the implementation session.
2. Stdio server. Command: `go test ./internal/acp -run 'TestACPStdio|TestStdio' -count=1` against a stub `ServeStdio` that returns at once. Result: FAIL after the 120 s test timeout, because no server answers on the pipes. This is a timeout against a stub, not a behavioral assertion; the green run below is the evidence for the behavior.
3. Race at start. `go test -race ./internal/acp -run TestStdioOversizeFrame` reported a data race between the SDK logger setup and its first read when the input fails at once. Fix: the server holds the first read until `Bind` ends (`recordReader.ready`).
4. Output pipe. `Close` on a blocked stdout write was suspected not to wake the write. On this platform (darwin, Go 1.27) the built binary exits 143 within 10 ms in both the plain and the experimental non-blocking variant, so no pollable-file code was added. The second-signal force path covers a platform where `Close` cannot interrupt a write. The stalled-output test stays as protection.

### GREEN results

- `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `gofmt -l` (only the older `tools.go`, not touched).
- `go test ./... -count=1`: all packages pass. `go test -race ./internal/acp ./internal/agent ./internal/bus ./pkg/protocol ./internal/app ./cmd/tui -count=1`: pass.
- `go test ./cmd/tui -run TestACP -count=3`: pass. `go test -race ./internal/acp -run 'TestStdio|TestACPStdio' -count=30`: pass.
- `go run ./cmd/tui -p hello` prints `hello`, exit 0; `--mode json` streams the unchanged event lines; no leftover child process or temporary binary directory.

### Boundaries and deviations

- Every row of the plan Runtime Flow Proof table has a `TestACPE2E*` test in [acp_e2e_test.go](../../cmd/tui/acp_e2e_test.go) that drives the built `ask acp` binary over real pipes with raw NDJSON. The child gets a minimal environment, a temporary `ASK_HOME` and a temporary working directory. Credentials come from the supported `ask auth login`.
- The production binary has no endpoint override (the base URLs are compiled into the catalog, and no existing environment variable replaces them). Rows that need a provider or OAuth fixture (`TestACPE2ERetryUsageProvider`, `TestACPE2EAuthRefresh`) therefore run the same `runWithDependencies` composition inside the re-executed test binary, on real pipes, with the injected transport that the existing auth command tests use. They are not built-binary proofs. The other rows run on the faux model of the built binary (`ASK_FAUX_TPS` holds a stream open).
- Started-tool drain, no-start failure and a continue from a user-tail log are supplemental stdio-composition tests in [stdio_test.go](../../internal/acp/stdio_test.go): the shipped tool set has Echo only, and the binary cannot preload a log.
- `TestACPE2ENoListener` lists the open files of the live child with `lsof` (no IPv4, IPv6 or Unix socket) and checks for no database or socket file.
- Sentinel checks: no API key, OAuth token, malformed-input text or provider error text that echoes a key appears on any stdout frame or on stderr. The failure text of a model request reaches follow events as the provider layer cleaned it (the key is removed). No extra redaction layer was needed.
- Stderr on end of input holds the SDK notice `connection closed` (level INFO) only.
- Real provider access was not used: no real provider key was available and none was requested.


## Independent review corrections, 2026-10-08

The initial provider/OAuth evidence above used the test binary and did not satisfy built-binary acceptance.
The corrected fixture uses an external HTTPS service, a CONNECT proxy, and a temporary CA with the standard Go trust override.
Both rows now run the production `ask` binary without injected product transports, clocks, retry waits, or test flags.
The OAuth credential comes from production `ask auth login` and expires with the real clock.
The retry test uses the normal production backoff.
The blocked-stdout test now waits for actual process exit before permitting the peer to read again.
See [independent review](../reports/code-review-261008-h13a-plan-compliance.md) for regressions, fixes, and final gates.
