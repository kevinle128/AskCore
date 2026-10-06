# H7a failure-mode and flow review

This review checks the six-phase plan against current source, the H7a parent requirements, the provider design, the architecture report and both scouts.
The scope is plan review only, with `--deep --tdd` and without `--yagni`.
No Go command, product test, OAuth request, credential read or live inference was executed.
The controller reports that the 131 planned proof rows passed its design-only gate.
That result is not runtime evidence.
The fixed choices remain one saved account per provider, local-only logout that removes issued-client metadata, verified ChatGPT identity and all three providers.
H4 remains a wire dependency only where the plan states it.

## Material findings

### High: uncertain rotation has no durable restart guard

Plan location: `phase-01-start.md:30–42`, `phase-02-auth-resolution-and-request-binding.md:39–42`, and `phase-06-cumulative-acceptance-and-documentation.md:80`.
The plan prohibits blind rotation retries and requires crash checks, but its persisted contract names tokens, expiry, metadata and revisions without a durable state for an exchange that can have reached the token server.
If the server rotates the token and the response is lost, or the process dies before replacement, the old expired record remains on disk.
A second process then sees the same expired record and follows the normal refresh path, which resubmits the old rotating token.
An error or reauthentication message in the first process cannot protect the second process.
Evidence: Pi `packages/ai/src/auth/resolve.ts:122–142` refreshes an expired current record inside `credentials.modify`, and its guards at lines 129–130 cover deletion and expiry only.
Evidence: Ask `internal/settings/doc.go:1–4` is a package scaffold, so no existing persisted recovery mechanism can satisfy the proposed restart behavior.
These Pi paths are under `/Users/dale/Desktop/workspace/opensources/pi` at the revision pinned by the scout.
Suggested fix: require a durable per-generation in-flight or uncertain marker before a rotating request can leave the process, refuse automatic refresh after an unresolved marker, and clear it only with a verified replacement or explicit recovery.
The marker can be part of the one provider record and does not require multiple saved accounts or remote revocation.
Add a two-process case that kills the first process after server rotation, then starts a fresh prompt and asserts that it sends no second refresh request.
Also cover a lost response and a failed pre-rename save, because those leave the same stale bytes without a process crash.

### High: signal shutdown can end a validated rotation commit

Plan location: `phase-02-auth-resolution-and-request-binding.md:41`, `phase-03-anthropic-and-headless-login.md:34`, and `phase-06-cumulative-acceptance-and-documentation.md:32,69`.
The independent bounded commit is required after token validation, but the plan also preserves existing signal behavior without defining how process shutdown waits for that commit.
The current headless path exits its wait after two seconds or a second signal.
A valid rotated response followed by a delayed file sync can therefore lose its only replacement token when the process exits, even though the commit context ignores caller cancellation.
Evidence: `cmd/tui/headless.go:43–44` sets `abortGrace` to two seconds, and lines 242–255 cancel the run and return on a second signal or that timer.
Evidence: `cmd/tui/main.go:121–122` invokes the process exit from the return value of `run`, so returning does not retain its background commit goroutine.
Suggested fix: specify the commit deadline and the owned shutdown wait together, including the second-signal policy, so a normal signal cannot return before a validated commit completes or reaches its own explicit failure bound.
Add a built-command check with a file-sync barrier held beyond the existing two-second grace, then release it and assert that replacement bytes exist before signal exit.
Keep signal exit codes while distinguishing completed commit, bounded storage failure and forced process death.

### High: built-command offline OAuth checks have no defined transport boundary

Plan location: `phase-03-anthropic-and-headless-login.md:68–69,116–118`, `phase-05-xai-device-authorization.md:73`, and `plan.md:132–135`.
The plan requires an ordinary built ask binary, fixed production HTTPS origins, real internal owners and external test HTTP/JWKS servers.
It does not define how the binary connects those fixed origins to the test servers while retaining the final origin checks.
Phase 5 names a phase 3 external test transport seam, but phase 3 does not specify that seam or its command-boundary wiring.
Without that wiring, implementation must weaken the actor boundary, contact real endpoints, or add an unreviewed endpoint bypass to make the planned checks run.
Evidence: `cmd/tui/args.go:33–35` has an in-process `http.RoundTripper` field, but `cmd/tui/headless.go:47–48,72–76` parses normal arguments and uses ordinary environment setup without a command-boundary transport injector.
Evidence: `cmd/tui/headless_test.go:458–465` builds the ordinary binary, while lines 484–485 launch that binary as a subprocess.
Evidence: `cmd/tui/headless.go:194–198` and `headless_faux.go:91–95` can inject clients only when the in-process options field is already set.
Suggested fix: name a test-only executable composition or transport interception mechanism that invokes the real `run` dispatch and ordinary constructors, preserves logical production URLs and issuer/JWKS checks, and routes only the external socket boundary to controlled servers.
Specify separate auth and inference transports so capture cannot receive OAuth bodies.
Name the clock/wait and process-barrier wiring needed for built-command xAI timing and rotation faults without adding a production endpoint override.
Prove early auth dispatch installs and releases its own signal handling, because the existing signal setup at `headless.go:87–90` occurs after the proposed auth return point.

## Flow sample

Each phase has four sampled claims, for a total of 24.
VERIFIED means the named current source segment supports the plan claim, not that the future feature or its test passed.
UNVERIFIED means the segment is proposed work without a current implementation or sufficient executable design detail.
FAILED means the current source and the stated preservation requirement conflict.
All source paths below are relative to the work context unless the row explicitly names Pi.

| Phase and claim | Entry, guards, wiring and result trace | Status |
| --- | --- | --- |
| 1: Owner-only transaction. | Planned settings entry must validate the owner path before lock/read/replace; `internal/settings/doc.go:1–4` contains no implementation, and phase 1 explicitly proposes the transaction files and failure checks. | UNVERIFIED, proposed new work. |
| 1: Absent logout prevents late login. | Planned login captures the store revision and final commit compares it after locked fresh read; no current settings implementation exists at `internal/settings/doc.go:1–4`, and phase 1 explicitly requires an absence-changing revision. | UNVERIFIED, proposed new work. |
| 1: Cancelled full buffer still has one result. | `internal/providers/stream.go:42–55` creates one producer, lines 89–93 settle with `sync.Once`, lines 97–102 close after settlement, and lines 106–117 drop an undeliverable terminal on cancellation. | VERIFIED, existing stream segment. |
| 1: Pre/post-rename errors differ. | Planned transaction checks write/sync/close/rename before directory sync and returns indeterminate after replacement; `internal/settings/doc.go:1–4` supplies no current writer or error classes, so phase 1 must implement both paths. | UNVERIFIED, proposed new work. |
| 2: Resolve follows final-model preparation on every turn. | `internal/agent/loop_stage.go:113–123` orders prepare before reason, `loop_stream.go:22–35` normalizes, resolves the legacy key and calls the configured stream, and `loop_stream.go:97–98` uses the current model provider. | VERIFIED, existing insertion point. |
| 2: Model readiness keeps the second idle check. | `internal/agent/agent.go:129–147` validates and checks idle before key resolution, then lines 150–157 check idle again before storing model/thinking; replacing key readiness remains explicit phase 2 work. | VERIFIED, existing guard and commit point. |
| 2: Early auth failure has one public lifecycle. | Planned composed runner returns `NewStream` plus `Assembler.Fail`; `loop_stream.go:39–74` drains, reads the result and emits one message end, while `agent.go:243–245` adds a failure sequence only if the loop returns a Go error. | VERIFIED, existing settlement contract; new runner must obey it. |
| 2: Validated rotation survives cancellation and restart. | Planned resolver locks/rechecks/exchanges/commits; Pi `packages/ai/src/auth/resolve.ts:122–145` supplies expiry-only refresh precedent, and Ask shutdown at `cmd/tui/headless.go:242–255` can end the process before an independent commit finishes. | FAILED, the plan lacks restart state and a shutdown budget contract. |
| 3: Auth dispatch avoids prompt/capture. | `cmd/tui/headless.go:47–48` parses inference arguments, lines 72–76 start capture/create the agent, and lines 81–90 read prompts/install signals; phase 3 explicitly proposes an earlier auth branch. | VERIFIED, current ordering identifies the required insertion point. |
| 3: Browser/manual loser and spare sockets close. | Planned callback/input lifecycle must cancel blocked stdin and close all connections; Pi `packages/ai/src/auth/oauth/openai-chatgpt.ts:271–296` races manual/callback results then aborts the loser and closes spare sockets, but Ask has no callback owner yet. | UNVERIFIED, proposed new Go lifecycle. |
| 3: Tool names are canonical before public start. | `internal/providers/fantasykit/fold.go:153` passes `part.ToolCallName` directly to `Assembler.ToolStart`; phase 3 step 6 explicitly adds adapter-local stream mapping before Fold and keeps arguments/IDs separate. | VERIFIED, current publication point and planned remedy align. |
| 3: Built binary reaches isolated token/inference servers. | `cmd/tui/headless_test.go:458–465` builds the ordinary executable; `args.go:33–35` offers only an in-process transport value, and `headless.go:72–76` uses production setup at the real command entry. | UNVERIFIED, the required command-boundary test wiring is unspecified. |
| 4: Returning identity uses verified saved client. | Pi `packages/ai/src/auth/oauth/openai-chatgpt.ts:250–263` always requests dynamic registration; phase 4 explicitly adds saved-client reuse plus a maintained verifier and wrong-subject/client rejection before revision commit. | UNVERIFIED, acknowledged Ask addition rather than missing baseline behavior. |
| 4: Old discovery cannot publish after replacement. | Planned discovery resolves one account, keys the cache by identity/generation and checks it before publication; `internal/agent/agent.go:142–155` presently has key-only readiness, so no existing discovery cache or stale-generation guard supplies this behavior. | UNVERIFIED, proposed new discovery path. |
| 4: Final profile cannot be replaced by model headers. | `internal/providers/openai/responses.go:102–110` constructs the outgoing client, then `isolate.go:60–64` applies model headers last; phase 4 explicitly replaces this unsafe typed-auth behavior with final binding checks. | VERIFIED, existing flaw and proposed owner align. |
| 4: Full local input and store=false reuse current builder. | `internal/providers/openai/responses_prompt.go:17–30` encodes local messages and instructions and sets Store=false; lines 40–54 still add key-profile fields, which phase 4 explicitly narrows for ChatGPT after serialization. | VERIFIED, existing reusable builder and restriction gap. |
| 5: Poll waits first and never speeds up after slow-down. | Pi `packages/ai/src/auth/oauth/device-code.ts:57–69` waits then checks expiry/cancel before poll, but lines 81–86 can replace the interval with a lower value; phase 5 explicitly requires a nondecreasing interval. | VERIFIED, reference flow and planned correction align. |
| 5: Hung poll ends within remaining lifetime. | Pi `packages/ai/src/auth/oauth/xai.ts:64–75,167–175` passes only flow cancellation to HTTP; phase 5 explicitly adds the minimum of operation deadline and remaining lifetime to each request. | UNVERIFIED, proposed Go timeout behavior that corrects a proven reference gap. |
| 5: xAI never uses an OpenAI ambient key. | `internal/providers/openai/responses.go:73–80` falls back to `ProviderOpenAI`; phase 5 explicitly registers xAI bindings and reuses typed-auth final checks instead of that branch. | VERIFIED, current fallback gap and proposed owner align. |
| 5: Reasoning inclusion is conditional. | `internal/providers/openai/responses_prompt.go:32–34` adds encrypted reasoning only when Model.Reasoning is true; phase 5 reuses this gate with verified compiled xAI capability data. | VERIFIED, existing wire gate. |
| 6: Signal exit preserves rotated-token commit. | `cmd/tui/headless.go:242–255` aborts and returns after two seconds or a second signal; phase 6 preserves this path while phase 2 permits an independently bounded commit with no linked shutdown budget. | FAILED, shared plan contracts conflict. |
| 6: Cumulative stream result settles once. | `internal/providers/stream.go:89–102` owns one settlement and channel close, and `internal/agent/loop_stream.go:62–74` reads the settled result and emits message end; phase 6 explicitly repeats early/late/full-buffer cases through consumers. | VERIFIED, existing owners support the planned checks. |
| 6: OAuth traffic cannot enter capture. | `cmd/tui/capture.go:25–46` assigns the inference recording transport, and `internal/providers/cassette/cassette.go:107–115` records through its real transport; phase 3 separates auth HTTP and phase 6 requires canary scans. | UNVERIFIED, auth transport separation is proposed and must be wired at the real command boundary. |
| 6: Three live routes are required for completion. | `cmd/tui/headless_faux.go:71–79` currently accepts only faux and Token Plan, so all three native registrations remain proposed; phase 6 explicitly requires real command/login/prompt/tool/logout evidence after the partial H4 gate. | UNVERIFIED, live acceptance is correctly deferred rather than claimed. |

## Concerns that did not become findings

The plan expressly covers wrong-state callbacks without killing a valid attempt, exact callback URIs, fixed-port errors and spare-connection cleanup.
The Go stdin cancellation mechanism still needs implementation detail, but the report does not call it a separate defect because phase 3 explicitly owns cancellable input cleanup and its leak checks.
The plan expressly covers post-rename uncertainty, fresh locked reads, absent-record revision changes and stale discovery publication.
Those rules are not rejected merely because their symbols do not exist before implementation.
The plan expressly covers bounded xAI poll calls and nondecreasing slow-down intervals, which address the concrete Pi gaps traced above.
The plan expressly preserves the distinction between stream-result failure and loop-error failure, so no duplicate public settlement is inferred without additional evidence.
The open verifier and SDK bearer choices are implementation gates, not proof that the plan omits identity or credential isolation.

Status: DONE_WITH_CONCERNS.
Summary: Reviewed all six phases and sampled 24 source flows without running tests or reading credentials.
Concerns: Three material gaps need explicit design and connected checks before the failure-mode proof can cover process restart, signal exit and offline built-command OAuth.
