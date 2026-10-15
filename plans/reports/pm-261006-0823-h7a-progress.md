# H7a implementation progress

Status: COMPLETED.
The active plan is [H7a subscription auth](../261006-0157-h7a-subscription-auth/plan.md).
The live plan pointer is `AskCore/261005-1858` for `master-2`.
All three ordinary production live routes passed.
The exact repeated local-commit actor, final review, and tagged lint gates are closed.
The full `--auto --advisor --tdd` scope remains active; no `--yagni` scope cut was requested.

## Final evidence

The [controller implementation report](./cook-261006-0849-h7a-implementation.md) explains the delivered architecture and maintenance limits.
The [live acceptance record](./tester-261006-0835-h7a-live-acceptance.md) records all three real provider routes and local logout.
The [final command proof](./tester-261006-0835-h7a-final-proof.md) records real replacement-write failure, SIGKILL, lost response, and explicit signed ChatGPT model selection.
The [final independent review](./reviewer-261006-0843-h7a-final-review.md) scores the result at 9.5/10 and reports no confirmed open critical code defect.
It approves the implementation and closes the exact local-commit signal actor gate.
The [exact filesystem actor report](./tester-261006-0855-h7a-fsync-actor.md) records the corrected first pass, resource-limited repeat, and final clean ten-case race pass.
The earlier [baseline](./tester-261006-0801-h7a-baseline.md), [shared](./debugger-261006-0801-h7a-shared-hardening.md), [native](./worker-261006-0801-h7a-native-flows.md), [wire](./worker-261006-0801-h7a-wire-profiles.md), [command](./tester-261006-0801-h7a-cli-acceptance.md), and [first review](./reviewer-261006-0823-h7a-implementation.md) reports remain historical records.
Their initial gaps are resolved by the final proof and review.
The [documentation report](./docs-261006-0823-h7a-runtime.md) records owning documentation and local link checks.

The controller reports passing full `go test ./...`, `go vet ./...`, compilation, lint with zero issues, required races, and `git diff --check`.
The latest full run reports CLI success in 32.725 seconds.
Final changed app/provider races and the signed ChatGPT explicit Sol command race also pass.
The strengthened faux named-choice check passed with the race detector in 1.561 seconds.
An earlier test/live callback port collision was followed by a clean full rerun.
It was a resource scheduling conflict, not an observed product regression.

## Full-plan reconciliation

All six phase files and all 22 phase success criteria were audited against the source and reports.
Every phase execution body now points to the current exact actor evidence and preserves its accepted creation-time source inventory.
No live task-management tool is available; the durable plan and CLI status remain the tracking authority.
All six phases were checked through the installed CLI.
The CLI-derived progress is completed, 6/6 phases and 22/22 criteria, or 100%.
Phases 2 and 6 were checked only after the final repeated actor and reviewer closure.
The final tagged lint check also passed with zero issues.
The [completion journal](../journals/2026-10-06-h7a-native-auth-and-exact-commit-proof-complete.md) records the filesystem observer fix, memory and disk limits, cache recovery, final pass, and cleanup.

| Phase and criterion | Evidence and result | State |
| --- | --- | --- |
| 1: Legacy keys and unknown data. | Legacy reader, selected-provider unknown fields, immutable optional snapshot, protected key tests. | Passed. |
| 1: Processes, absence revisions, durable failures. | Real writer/reader processes, absent logout revision, pre-rename preservation, post-rename uncertainty, independent commit failure. | Passed. |
| 1: Redaction and acyclic imports. | Snapshot redaction, capture separation, full import/lint gates. | Passed. |
| 2: One shared resolver/readiness seam. | Ordinary app binding, native SetModel, no agent vendor switch, import gate. | Passed. |
| 2: Precedence, refresh races, cancellation, endpoints. | Real owner tests, three restart faults, external signal drain, final HTTP guards. | Passed: final ten-case race run and review closure. |
| 2: Key hooks, model switching, one settlement. | Protected hooks, native model/thinking preservation, early and late settlement, full suites. | Passed. |
| 3: Commands and two Anthropic interactions. | Compiled real key/browser/copy-code commands, explicit selection, local logout. | Passed. |
| 3: Subscription prompt and tool result. | Ordinary live Sonnet 4.6 prompt and canonical echo result without key fallback. | Passed. |
| 3: Profiles, names, callbacks, capture. | Final Messages request guards, historical codec, cleanup/bounds, private-client separation. | Passed. |
| 4: Identity, client reuse, one account. | Signed OIDC command login, returning client, new-account replacement, live sign-in. | Passed. |
| 4: Discovery and stale generations. | Unknown/denied/listed/replaced/deleted native SetModel and model/thinking preservation. | Passed. |
| 4: Responses and key compatibility. | Actual SDK body/header restrictions, grouped tools, named choice, replay, protected key suites. | Passed. |
| 4: Live ChatGPT route/tool/logout. | Explicit listed GPT-5.6 Sol live echo loop; record removed by logout. | Passed. |
| 5: Bounded device states. | Native device timing and terminal states, real command timing/denials, bounded cancellation and cleanup. | Passed. |
| 5: Refresh retention and no fallback. | Method-specific omitted/replacement token checks and shared fenced resolver. | Passed. |
| 5: xAI profiles and canonical tools. | Actual key/OAuth request checks, conditional reasoning, canonical echo turn. | Passed. |
| 5: Live xAI route/tool/logout. | Grok 4.7 device login and live echo loop; record removed by logout. | Passed. |
| 6: Every stated actor row. | Main source/test inventory and final reviewer reconciliation. | Passed: final ten-case race run and review closure. |
| 6: Three live routes and logout. | All three ordinary production routes and absent records. | Passed. |
| 6: Existing contracts and quality gates. | Full test, vet, compilation, lint, required races, final changed-package races. | Passed. |
| 6: Docs, links, secrets, cleanup. | Owning docs, local links, all test records absent, owned resources removed. | Passed. |
| 6: CLI status after acceptance. | Four satisfied phases checked; H7a remains in progress. | Passed: CLI reports completed, 6/6, 22/22, 100%. |

## Live results

The ordinary production binary used isolated owner-only homes without ambient provider keys or auth capture.
No test HTTP transport, custom endpoint, TLS exception, or signature bypass was used.

| Provider and method | Explicit live model | Login and saved state | Prompt/tool/settlement | Local logout |
| --- | --- | --- | --- | --- |
| Anthropic, `anthropic-oauth`. | `claude-sonnet-4-6`. | Exit 0, generation 1, expiry, no fence. | Exit 0, one echo result, `isError=false`, two assistant turns, usage, one settlement. | Exit 0, record absent. |
| OpenAI, `openai-chatgpt`. | `gpt-5.6-sol`. | Exit 0, generation 1, expiry, no fence. | Exit 0, one echo result, `isError=false`, two assistant turns, usage, one settlement. | Exit 0, record absent. |
| xAI, `xai-oauth`. | `grok-4.7`. | Exit 0, generation 1, expiry, no fence. | Exit 0, one echo result, `isError=false`, two assistant turns, usage, one settlement. | Exit 0, record absent. |

Each pair of assistant turns ended with `toolUse` and `stop`.
The live ChatGPT account hid GPT-5.5 and listed GPT-5.6 Sol.
Readiness correctly refused the hidden model before inference.
The static Sol row was selected explicitly; the existing GPT-5.5 default was preserved.
All three private test records were absent after logout.
The controller confirms that owned live processes/listeners ended and private test homes, logs, bridge, and binary were removed.
No credential, callback code, identity, email, or private artifact path is recorded here.

## Final proof disposition

Native SetModel now preserves the prior model and thinking level on denied, replaced, or deleted discovery responses.
The reviewer and controller record passing focused and race checks.
The command restart matrix now covers actual SIGKILL, lost external response, and a real filesystem replacement-write failure.
The write-failure case confirms an OS permission error, returns a validated rotating response, restores normal mode, and starts process B.
Process B sends no additional refresh or inference request and leaves the pending file bytes unchanged.
Faux now checks named-choice script delivery, recorded value, and immutable returned-record copies in its existing test.
That strengthened test passes with the race detector.

The external token-exchange signal actor and private real-store fault remain supporting checks.
The new tagged compiled CLI actor now blocks the real second regular-file sync on an isolated Linux FUSE mount after token validation and temporary-file write.
The first sync and its directory sync durably publish the fence.
For ordinary signals, the actor holds the replacement beyond two seconds after first and second signals, then releases it before the five-second budget.
The fixture performs real backing directory sync before a separate compiled observer reads the complete new credential while the CLI is alive.
The expected signal exits remain 130, 143, and 129.
Budget expiry and actual SIGKILL start a second prompt process that sends no extra auth or inference request and preserves the fence byte-for-byte.
The observer uses the owned backing files to avoid Linux mounted-inode serialization behind active fsync.
The product still uses the real mount for writes, locks, rename, and sync.
No production hook or timing race was added.

The corrected five-case matrix passed once with race instrumentation in 19.50 seconds.
The earlier repeat did not complete after host disk exhaustion, so that failed `-count=2` invocation is not reported as passed.
A later cached link failure showed cache corruption; removing the owned Linux cache and rebuilding resolved it.
The controller recovered space and started a fresh complete repeated race run.
The fresh Linux run passed all five cases twice with race instrumentation in 38.186 seconds and returned exit code 0.
There were no race diagnostics or cleanup failures.
The reviewer closed the exact actor gate with score 9.5/10.
Final tagged lint passed with zero issues.
The owned test VM and its cache were removed, and no owned test process remains.
The [fixture setup](../../internal/testsupport/README.md#filesystem-fault-tests) owns the Linux requirements and points to the executable runner.
The [fixture implementation report](./worker-261006-0855-h7a-fsync-fixture.md) records setup, strict C compilation, resource failures, and cleanup boundaries.

## Architecture and maintenance

The [detailed architecture study](./xia-261006-0143-h7a-subscription-auth-architecture.md) remains the stateful source analysis.
Settings owns the stdlib file transaction; auth owns protocols, identity, discovery, and resolution; app owns composition.
Providers receive immutable snapshots and own final HTTP guards; agent owns canonical history and public settlement.
One shared command/app path supports all three methods and future callers without another inference stack.
The stable lock and fresh merge coordinate processes on one authoritative reliable local filesystem.
The durable fence prevents uncertain rotating-grant reuse after restart.
The store does not claim distributed replica safety or multi-account coordination.

## CLI record and limitation

Read live help for `ak plan`, `check`, `status`, `phase update`, and `update` before mutations.
Ran `ak plan check plans/261006-0157-h7a-subscription-auth/phase-01-start.md --json`.
Ran `ak plan check plans/261006-0157-h7a-subscription-auth/phase-03-anthropic-and-headless-login.md --json`.
Ran `ak plan check plans/261006-0157-h7a-subscription-auth/phase-04-chatgpt-identity-and-responses.md --json`.
Ran `ak plan check plans/261006-0157-h7a-subscription-auth/phase-05-xai-device-authorization.md --json`.
Ran `ak plan update AskCore/261005-1858 --status in-progress --current-phase 2 --json`.
The earlier `ak plan status` result was in-progress, 4/6, 14/22, 63%.
After final proof and review, ran `ak plan check` for phase 2 and phase 6, then `ak plan update AskCore/261005-1858 --status completed --current-phase 6 --json`.
The final `ak plan status plans/261006-0157-h7a-subscription-auth --json` result is completed, 6/6 phases, 22/22 criteria, 100%.
Phase evidence and notes use `ak plan phase update AskCore/261005-1858 N --notes <evidence> --evidence <report link> --json` for N=1 through 6.
The installed `check` changes only checkboxes.
`status` is read-only and accepts a plan directory.
`update` accepts a plan-store ID and writes plan frontmatter, not phase status.
`phase update` explicitly rejects file-owned status changes.
Thus phase frontmatter and the main phase table retain creation-time metadata; the current execution records and CLI checkbox progress are authoritative.
No manual status cell/frontmatter edit or repo-wide reindex was used.
All six phase index notes and evidence were synchronized after the final pass.
`ak plan validate` and the completion journal validation passed.
AgentWiki publish skipped; the journal remains local.
Only the owning parent H7a phase prose was reconciled with the completed execution record.
The whole parent roadmap and other phases were not marked complete.

## Unresolved acceptance

None.
The repeated exact actor, all three live routes, review, tagged lint, plan synchronization, and owned-resource cleanup passed.
Single-host file coordination and the accepted one-account/local-only logout scope remain documented product limits.
