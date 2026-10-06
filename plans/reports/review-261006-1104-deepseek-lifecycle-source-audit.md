# DeepSeek lifecycle source audit

Date: 2026-10-06.
Status: changes required before implementation.
Scope: independent review of the lifecycle redesign plan and architecture report.
This is a review record, not an accepted replacement design.

## Result

The main architecture direction is sound.
The plan does not yet prove behavioral conformance with DeepSeek.
Some source claims are false, some Ask assertions test a different contract, and several proposed safety rules lack an ownership contract.
Correct those issues before using the matrix as an implementation specification.

Keep the capability packages, one driver per Agent, typed control points, ordered tool commits, separate session writes, and bounded remote observation.
Do not copy the Cordis plugin framework into Go.
The most important changes concern the meaning of a recorded tool call, work that survives cancellation, request preparation, and observation cursors.

## Evidence boundary

- Ask HEAD: `e991b5cee5e9611c3843e2065ef63a749524028a`, plus the existing working tree.
- DeepSeek HEAD: `5badb15009ae1756c3afe0ae0cef1faafc290ccc`.
- DeepSeek checkout: `/Users/dale/Desktop/workspace/opensources/deepseek-harness`.
- Reviewed plan: [lifecycle redesign](../261006-0933-lifecycle-event-pipeline-redesign/plan.md).
- Reviewed report: [architecture proposal](architecture-261006-ask-lifecycle-event-pipeline.md).
- Primary evidence: executable source control flow.
- Supporting evidence: test bodies and assertions, not test names alone.

Three sub-agents checked lifecycle/retry, dispatch/tools/repair, and session/observation/Ask architecture.
The controller checked their findings against the source, checked matrix structure, and reviewed the remaining external boundaries.
Prior reports were review targets, not proof.

`gitnexus status` reported an up-to-date index at the pinned commit.
`gitnexus query` and `gitnexus context` located `ToolCallRecovery` and retry code.
The graph did not return a complete execution path, so source reads supplied the call order.
[DeepWiki](https://deepwiki.com/deepseek-ai/deepseek-harness) reported an older index, `4878cd`, dated 2026-09-29.
It was not used to prove behavior at `5badb15`.

Two focused upstream test commands started an automatic dependency install before test execution.
Both were stopped with SIGINT.
No upstream test result is claimed.
The commands may have added files under the upstream `node_modules` directory.
No tracked source was changed by this review.
No Ask implementation test was run because this task reviews a pending design.

In the references below, `DS:` means the pinned DeepSeek checkout.
Source references identify the implementation boundary, not a claim that all tests in that package passed.

Pinned source entry points:

- [Driver and attempt boundaries](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L398).
- [Tool coordinator and call recording](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/tool-calls.ts#L165).
- [Tool preparation and post-control routing](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/tools/src/index.ts#L1493).
- [Conservative tool recovery](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/session/src/repair.ts#L116).
- [Prepared provider registration and retry policy](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/llm/llm/src/index.ts#L930).
- [Follow snapshot and frame watermark](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/api/session-controller/src/history.ts#L179).

## High-priority findings

### 1. Bounded cancellation can break exclusivity across runs

Target: phase 09, lines 24 and 47; architecture report, lines 207 and 455–477; matrix N5.

The plan ends a run after five seconds, leaves an uncooperative tool body running, and drops its late result.
It does not retain that body's resource ownership across runs.
A later Prompt can start another exclusive tool while the previous exclusive tool still runs.
Repeated aborts can also leave more active bodies than `MaxParallelTools` permits.

DeepSeek waits for started work before settlement.
See `DS:packages/core/agent-loop/src/tool-calls.ts:220` and `DS:packages/core/tools/src/index.ts:1560`.
The five-second rule is an Ask difference, not an equivalent implementation of that behavior.

The roadmap's D19 requires started bodies to drain before the Step closes.
The five-second abandonment rule appears in this plan, but is not an explicit exception in the recorded D25 list.
Resolve that trade-off instead of treating the new bound as already approved.
If the bound is retained, define who owns unfinished bodies after run settlement.
An Agent-level outstanding-work gate can prevent new tool execution until the old bodies return.
Do not report full execution drain while such work remains.
Test an exclusive body that ignores cancellation, timeout, and a second Prompt before the old body exits.
Also test repeated cancellation for an Agent-wide resource bound.

### 2. The coordinator's timer cannot interrupt a blocked control handler

Target: phase 09, lines 23 and 46–47.

The coordinator runs `BeforeTool` and `AfterTool` itself.
While either handler blocks, it cannot return to its select loop to observe cancellation or a drain timer.
A bound on waiting for workers is not a bound on all run cleanup.
DeepSeek also awaits preparation and post-control in the coordinator.
See `DS:packages/core/agent-loop/src/tool-calls.ts:153` and `:170`.

State whether the bound starts after cooperative control returns.
If the bound must cover control too, specify ownership and late-result rejection for that work.
Do not claim that a context or select can forcibly stop a Go function.
Test cancellation during both pre-control and post-control, separately from body cancellation.

### 3. Repair confuses recorded intent with actual body invocation

Target: phase 09, lines 43–48; matrix B2, B5, 51b, 63.

DeepSeek appends `tool/call` before preparation.
See `DS:packages/core/agent-loop/src/tool-calls.ts:165`.
Repair uses the presence of that record to choose an unknown outcome.
See `DS:packages/core/session/src/repair.ts:131` and `:166`.
Normal cancellation uses a separate body-invocation fact.
See `DS:packages/core/tools/src/index.ts:1548`.

These facts are not interchangeable.
The cited scheduler-failure test expects an unknown outcome for `c3` even though its body did not run.
See `DS:packages/core/agent-loop/tests/tool-calls.spec.ts:808`.
The architecture report explains this distinction, but the phase tasks and assertion wording do not preserve it clearly.

Specify at least three separate facts: assistant request, recorded call intent, and body invocation.
Require the call record before pre-control.
Use recorded intent for conservative repair and body invocation for normal abort classification.
Test failure after the call record but before body entry.

### 4. The request log does not contain enough information for its stated equality

Target: phase 04, lines 42–50; matrix 25 and SB9.

DeepSeek materializes adapter defaults before it logs the request header.
See `DS:packages/llm/llm/src/index.ts:889` and `DS:packages/core/agent-loop/src/agent.ts:587` and `:609`.
Ask still computes effective values inside adapters.
For example, `internal/providers/anthropic/document.go:130` computes the transmitted token limit from options, model limits, and the input estimate.

The planned log can store `MaxTokens == 0` while the adapter sends a nonzero value.
Its `ModelRef` omits the model limits and other configuration required to reproduce that value.
It also drops all sampling parameters, endpoint paths, and header values, including values that need not be credentials.
Therefore, equality with the dispatched request cannot mean only “except credentials” as currently stated.
A faux-provider comparison can miss this defect.

Define the exact reconstruction boundary.
Materialize safe effective options through the provider preparation boundary before logging them.
Do not duplicate adapter calculations in `agent`.
Keep secrets excluded, and name every other deliberate omission.
Test reconstruction against an actual adapter request body with defaults and after a model switch.

### 5. Retry must use the policy of the registration that served the failed attempt

Target: matrix SL10, line 375; phase 08 provider-policy lookup.

SL10 says that replacing a provider during an attempt makes the next retry use the new policy delay.
The cited source does the opposite for recovery from that old attempt.
DeepSeek captures the registration and policy during preparation, then supplies that policy with the failure.
See `DS:packages/llm/llm/src/index.ts:930` and `DS:packages/core/agent-loop/src/agent.ts:500`.

The cited test first expects the old delay, 1 ms.
Only failure of a later attempt served by the replacement produces the new 3 ms delay and reset budget.
See `DS:packages/llm/llm-retry/tests/retry.spec.ts:649`.

Correct SL10 and capture the policy with the serving registration or stream.
A lookup by provider name after failure is insufficient if replacement is allowed.
If live provider replacement remains outside Ask scope, classify that part consistently with SB10 and SL32.

### 6. Attempt opening has incompatible definitions

Target: phase 01, line 52; phase 08, line 63; matrix SA5, line 219; report, line 114.

The phases open the attempt before the stream call.
SA5 requires no attempt event if stream construction throws.
DeepSeek constructs the stream, checks cancellation, and only then calls `live.start()`.
See `DS:packages/core/agent-loop/src/agent.ts:434`.
The catch at line 446 bypasses live settlement if start did not occur.

Choose one boundary for the live attempt events and apply it everywhere.
For DeepSeek equivalence, publish start only after stream construction succeeds and cancellation is checked.
A prepared-request identity may exist earlier, but it must not imply a started live attempt.
Test a construction throw and cancellation between construction and live start.

### 7. Follow needs one snapshot and cursor cut

Target: phase 05, lines 20 and 49–51; matrix 71.

DeepSeek takes the durable events and their cursor from one source observation.
It also captures an assistant-stream baseline and an ordinal watermark.
It filters already represented events and checks continuity.
See `DS:packages/api/session-controller/src/history.ts:179` and `:213`.

Ask separates session commit from event publication, but the plan does not define an atomic relation between the snapshot callback and the Agent event cursor.
If a message commits between those reads, a follower can receive it twice or miss it.
A follower that joins during streaming also needs a rule for the current partial assistant output.

Specify one Agent-owned operation that returns a consistent snapshot and cursor.
State whether partial frames have a baseline or require a reset.
Test a join between commit and publication, during a model stream, and during Reset.

### 8. The output deadline does not yet release a blocked write

Target: phase 05, lines 19 and 47–48; matrix W4 and W5.

`cmd/tui/output.go:53` holds a mutex while it writes to the target.
The flush path does the same at line 73.
A check that drops later writes does not release a write already blocked on a full pipe.
A deadline setter that needs the same mutex can also block.
Timer-driven flush errors do not necessarily pass through the JSON listener's proposed abort callback.

Specify how the supported output target interrupts an in-flight write.
Propagate the first failure from both Write and flush to the run owner.
Test a pipe that is already full before SIGINT and EPIPE from the timer flush.
Preserve the selected stdout backpressure behavior.

## Source and conformance corrections

| Target | Source result | Required correction |
|---|---|---|
| Row 74; SA31 | A busy DeepSeek Agent normally accepts queued or steering input; `agent.ts:154` and `:214` implement it. The cited test at `session-cold.host.spec.ts:900` uses an idle stub that throws “disposed”; `commands.ts:372` maps that exception to a transport error. | Keep Ask `Prompt -> ErrBusy` as an API difference, not `follow`. |
| Phase 09 line 20 | Deny and cancel use post-control, but pre-control exceptions return `final-result` and skip post-control; `tools/src/index.ts:1516` and `:1536`. Ordinary unknown tools can reach pre/post-control. | Replace “every call that reached BeforeTool” with a case table. Name retained Ask validation and unknown-tool differences. |
| ST27 | `tools/execute` is an around wrapper at `tools/src/index.ts:1601`. Phase 02 and phase 09 explicitly add Ask `ExecuteTool`. | Remove the false N/A reason. Cover short circuit, wrapper failure, cached success after cancellation, and preservation of the original cancellation signal. |
| ST11 | Direct tool execution materializes JSON arguments before checking pre-abort; `tools/src/index.ts:1497`. A pre-aborted batch skips preparation; `tool-calls.ts:139` and `:238`. The cited bad argument is a function value, not a schema mismatch. | Separate direct capability behavior from batch behavior and materialization from schema validation. |
| ST20 versus B1 and phase 03 | ST20 asks for raw start-event arguments; B1 and phase 03 require the validated arguments that run. | Make the start-event assertion use the selected effective-argument contract. Preserve D21 coercion. |
| ST21 | Input-schema rejection is not universally at Register. `tools.spec.ts:2067` registers a lossy schema, then expects `schemas()` to fail. `tools/src/index.ts:1063` validates output schema and timeout at registration. | Separate registration validation, input validation, and declaration projection. Label Ask's stronger early rejection as an Ask rule. |
| ST1 | `tools/src/index.ts:1282` can also emit `deferLoading: true`. | Include the optional upstream field; Ask can omit it under ST29. |
| SA18 versus row 20 | DeepSeek removal by ID is real; `inbox.ts:152`. Phase 06 exposes no removal operation and row 20 excludes editing. | Include the prerequisite API or split the removal case from the second-cancel case. Do not leave an impossible test. |
| SA26 versus rows 19 and 53 | `agent.ts:535` queues additional context without waking the Agent. Phase 06 has only waking sends and excludes the additional-context output. | Resolve this scope conflict explicitly; `Steer` is not a substitute for non-waking insertion. |
| SA19 and SA25 | DeepSeek disposal closes admission separately from ordinary cancellation. | Define a disposal gate or classify disposal-specific behavior outside scope; a cause string alone is not a gate. |
| SL14, phase 07 | Classification lives in `llm-deepseek/src/translate.ts:149`; retry lives in `llm-retry/src/index.ts:215`. | Keep classification in phase 07 and the second-request assertion in phase 08. |
| A6 | The cited `llm/src/index.ts:1018` handles file paths. | Cite adapter-only failure catching at `:1082` and the outer waterfall at `:1132`, plus driver throw handling at `agent.ts:445`. The claimed behavior is otherwise correct. |
| Phase 08 line 19 | An over-cap Retry-After declines normal retry, but does not synthesize reset-time text; `llm-retry/src/index.ts:227` and `llm-deepseek/src/transport.ts:25`. | Remove the text guarantee or label it Ask-new. |
| F1 and phase 07 | Unknown in-band provider errors become `SERVER`; unknown HTTP statuses become `HTTP_<status>` at `llm-deepseek/src/transport.ts:35`. | Do not label an unconditional `UNKNOWN` fallback as equivalent; an in-band fallback changes retry eligibility. |
| Row 56 | Source has emit, serial, and waterfall modes; `agent/src/dispatch.ts:120`. Its Ask assertions only check one-shot `next`. | Give the modes their own assertions; one-shot `next` is an Ask extension. |
| Row 22 | The cited `interception.spec.ts:76` checks default delegation, not an outer handler preserving an inner rejection. | Use the actual nested rejection assertion as evidence; the outer-handler behavior itself is supported by the bridge source. |
| W3 and phase 05 | DeepSeek invokes callbacks inline and does not await returned promises; `agent/src/dispatch.ts:127`. Synchronous work still delays it. | Replace the absolute “observers never stall” claim. Distinguish failure containment, invocation order, and completion/backpressure. |
| SS3 | DeepSeek guards Session append through publication; `session/src/index.ts:737`. Ask's test only checks `Prompt -> ErrBusy`. | Specify and test sole-writer ownership or narrow the claim. Agent admission is not writer reentry protection. |
| Phase 05 line 21 | Live delivery is whole but replay storage is truncated without an explicit marker. DeepSeek follow delivers full entries; `history.ts:232`. | An oversized event must cause resync or carry an explicit incomplete representation. Never return different complete-looking payloads at one cursor. |
| Row 77 | Spawn starts with no parent seed; `subagent-spawn-in-process/src/index.ts:49`. Fork seeding belongs to a separate provider. | Split depth and fork claims and cite the fork implementation, not only the spawn test directory. |
| Row 70 and SL18 | Their source descriptions include attempt/frame identity and final-usage fallback, respectively. Their proposed assertions check only commit order and repeated usage updates. | Add assertions for the missing parts or narrow the claimed coverage. |

The post-control findings do not require removal of Ask's accepted coercion or snapshot rules.
They require accurate attribution and tests for the selected Ask behavior.

## Architecture assessment

| Design choice | Assessment | Condition for a correct Go design |
|---|---|---|
| Modular monolith by capability | Keep. It matches Ask's architecture reference and import rules. | Wiring stays in `app`; runtime packages do not import transports or concrete storage. |
| One execution owner per Agent | Keep. It provides deterministic lifecycle transitions and history order. | Include late workers and disposal in the ownership model. |
| Typed middleware | Keep for admission, request preparation, execution, and recovery. | Cover the complete operation lifetime; preserve cancellation; validate short-circuit results. |
| Ordered completion decisions | Keep the accepted precedence. | Label it an Ask/Pi exception; it is not DeepSeek's serial first-bail contract. |
| Tool coordinator | Keep the rolling pool, barriers, and source-order commits. | Separate preparation, body completion, post-control, and commit; bound outstanding work across runs. |
| Session entry log and projection | Keep. | Define the safe reconstructable request and sole-writer contract; do not equate an in-memory append with disk durability. |
| Local observer plus remote follow | Keep the selected two paths. | Local handlers may delay the Agent; remote replay needs one cursor cut and explicit gaps. |
| Strategy for recovery and concurrency | Appropriate. | Policy belongs to the serving provider/tool, while the driver owns transitions and scheduling. |
| “Template Method” | Only a loose description of the fixed stage order. | No inheritance framework is needed; describe private orchestration and typed seams directly. |

The design supports adding providers, tools, and control handlers without changing the central loop for every feature.
That is the useful part to extract from DeepSeek's capability seams.
Copying its package names or event names would not establish equivalent architecture.

Scale has three distinct limits here.
One Agent serializes control and commits by design.
Many Agents can execute independently if they do not share a global driver lock.
Many remote readers require bounded buffers and a correct resync contract.
Multi-process writers additionally require the planned storage lease or generation check when persistence is introduced.
An in-process mutex is not that check.

Full history copies and full opening snapshots grow with history size.
Record that limit and measure it when implementing persistence and large-session support.
There is no source evidence here that requires a distributed broker, actor framework, or new workflow engine.

## Report precedence and plan readiness

The architecture report is explicitly an earlier proposal.
Phase 00 already plans to correct its vocabulary, retry policy, argument rewriting, and observation policy.
Those known differences alone are not new defects in the implementation plan.

However, the phase instructions and matrix still contradict each other after those planned corrections.
The statement “Remaining contradictions: none” in `plan.md` is false.
Also, phase 00 must not change the report status to “accepted design” merely because corrections were written.
Acceptance is a separate user decision.

Keep these accepted differences visible: Pi wire projection, no retry jitter, registry snapshot, completion precedence, all-results termination, D20 error projection, and D21 coercion.
Do not silently reverse them in response to this review.
Do not classify changed cancellation lifetime or a continuation limit as purely additive behavior if it changes what an upstream run can do.
Use `Ask-new` for added capability and `diverge-deliberate` for changed behavior, with a reason for either.

## Verification record

A read-only parser found 281 behavior rows: 143 `follow`, 22 `diverge-deliberate`, 103 `N/A`, and 13 `Ask-new`.
It found no duplicate row IDs.
All 178 non-N/A rows have a matching assertion-review checkbox, and all are still unchecked.
The 219 distinct full-path source citations parsed from the matrix exist and their cited first line is in range.
This proves reference structure only, not semantic accuracy.
It does not validate every shorthand citation or every claimed test title.
The planned `check-conformance-matrix.sh` does not exist yet, as expected for pending phase 00.

Test names, a passing name lookup, and a checked box are not independent evidence of conformance.
Each assertion must distinguish the claimed behavior from the counterexample.
For example, `ErrBusy` does not prove session append protection, and a faux request does not prove adapter-default materialization.

The following source groups were traced for the behavior comparison.
N/A rows were checked as scope claims where noted; they do not establish implemented Ask support.

| Matrix group | Primary source anchors | Result |
|---|---|---|
| Lifecycle, input, and admission: 1–4, 8–11, 15–18, 21–24b, A3/A5/A8/A9, B3, SA7–25, SB6–8/17/22/23 | `DS:packages/core/agent-loop/src/agent.ts:154,214,295,398`; `inbox.ts:97` | Core mechanisms match, subject to attempt timing, busy/disposal, and scope findings. |
| Interrupted output: 5–7, B9, SA23, SL23/26 | `DS:packages/core/agent-loop/src/agent.ts:445`; `DS:packages/llm/llm/src/assembler.ts:135` | Partial text/reasoning survive; tool calls do not; empty-output Ask wrapper is a declared difference. |
| Retry: 28–36, A1/A4/A6, SL9–16 | `DS:packages/llm/llm/src/index.ts:930`; `DS:packages/llm/llm-retry/src/index.ts:125,149,215`; `retry-policy.ts:14` | Defaults, in-step retry, cancellation, and per-provider policy are supported; replacement assertion is wrong. |
| Provider failure mapping: 33/38/38b/F1, SA2/4, SL14/30 | `DS:packages/llm/llm-deepseek/src/transport.ts:22`; `adapter.ts:51`; `translate.ts:143` | Typed distinctions are supported; Ask adapter equivalence needs actual adapter tests. |
| Tools: 43–54, B1–5, SB20, ST1–13/17–23/27 | `DS:packages/core/agent-loop/src/tool-calls.ts:85`; `DS:packages/core/tools/src/index.ts:1282,1441,1493,1601` | Pool and ordering are sound; stage routing, pre-abort, schema timing, and wrapper coverage need correction. |
| Repair: 51/63, SS9/10 | `DS:packages/core/session/src/repair.ts:113`; `DS:packages/core/agent-loop/src/agent.ts:331` | No re-execution, matching result removal, and closed-step discard are supported; recorded intent is the key fact. |
| Freeze and append: 25/26/66, SB3/5/9, SS2/3 | `DS:packages/core/agent-loop/src/agent.ts:609,669`; `DS:packages/core/session/src/index.ts:728` | Copy/freeze and commit-before-publish are supported; reconstruction and reentry assertions are incomplete. |
| Observation: 56/57/70/71, B6/B8, W3 | `DS:packages/core/agent/src/dispatch.ts:120`; `DS:packages/api/session-controller/src/history.ts:123,179,213` | Error containment is real; nonblocking and snapshot claims need the stated qualifications. |
| External hooks: 60–62, B11 | `DS:packages/hooks/hook-protocol/src/runner.ts:20,74`; `merge.ts:35`; `hooks-codex/src/index.ts:178,204,263` | Ten-minute default, merge precedence, downstream decision preservation, and missing Codex stop handling are supported; X1/X2 remain out of scope. |
| Gateway and archive: 72/73/75 | `DS:packages/api/session-controller/src/commands.ts:330,602`; `archived-session-gate.ts:24`; `DS:packages/api/gateway/src/stream-server.ts:105,334` | Prompt RPC deduplication, archive admission, heartbeat, and uplink byte bounds exist; do not generalize prompt deduplication to every command. |
| Subagents: 76/77 | `DS:packages/subagent/subagent-acp/src/run.ts:465`; `DS:packages/subagent/subagent-in-process-driver/src/index.ts:109,160`; `DS:packages/subagent/subagent-spawn-in-process/src/index.ts:49` | Parent signal propagation and fresh spawn are supported; fork citation needs separation. |

### Additional source coverage and limits

The following pass checked grouped capabilities that the plan marks N/A, plus remaining shared behavior.
N/A is a decision about Ask scope, not a reason to accept the upstream description without checking it.

| Rows or subject | Primary source | Result |
|---|---|---|
| Usage 41/42, SL18–22 | `packages/llm/token-meter/src/turn-usage.ts:185,226,246,252`; `usage-projection.ts:81,123` | Failed attempts count once; missing usage stays unknown; final usage replaces the latest sample. |
| Compaction 37/39/40, SB14 | `packages/compaction/compaction-basic/src/index.ts:190`; `compaction-image-offload/src/index.ts:25`; `compaction-tool-result-pruner/src/index.ts:136` | Recovery, progress checks, image offload, and pruning exist; H10 exclusion is consistent. |
| SA1/13 and durable inbox | `packages/core/agent-loop/src/inbox.ts:235`; `packages/core/session/src/index.ts:728` | Receipt precedes wake; saved inputs are detached and frozen. |
| SA30–34, SB2/15/24/25/29/30 | `packages/core/agent-loop/src/index.ts:334`; `runtime-context.ts:123`; `agent.ts:652`; `packages/core/agent/src/consumed-work.ts:83`; `model-selection.ts:47`; `packages/core/system-prompt/src/index.ts:197` | Config, runtime context, model notices, tool ordering, and consumed-work capabilities exist. |
| SB10–12, SL30/32 | `packages/llm/llm/src/index.ts:354,930,1132`; `packages/core/agent-loop/src/agent.ts:587` | Registration pinning and middleware-owned unregistered routes exist; SL10 cannot require replacement while SB10 excludes it. |
| SB13/16/19 and prompt history | `packages/core/agent-loop/src/agent.ts:409,615`; `runtime-context.ts:68`; `packages/llm/llm/src/content.ts:424` | Prompt/header reconciliation and provider-specific tool declaration updates exist; their exclusion is consistent. |
| SB26–28/31 | `packages/core/agent/src/index.ts:248,437,533,602`; `packages/core/scope/src/index.ts:137,170` | Scoped registry, initiator lifetime, ancestry routing, and factory teardown exist; Go can pass context explicitly. |
| SB32/33; rows 65/B10 | `packages/core/agent-loop/src/index.ts:374,807,839` | Configured IDs, write ownership, resume, and repair exist; crash-resume is out of scope. |
| SB34/35 | `packages/core/agent-loop/tests/request-cache.e2e.ts:73`; `scripts/verify-export-jsdoc.ts:567` | Live-key cache test and TypeScript JSDoc gate exist; these are test/tooling capabilities. |
| SL1/2/8/11 | `packages/llm/llm-retry/src/types.ts:6`; `src/index.ts:30,130,199,243` | Shared payload type, replayed budget, always mode, and recovery disposal exist; bounded normal retry is a distinct selection. |
| SL24–29/35/37 | `packages/llm/llm/src/assembler.ts:49,135`; `assistant-stream.ts:100,202,234`; `adapter-failure.ts`; `call-config.ts:48` | Assembly, replay metadata, stream compaction, timing, and failure normalization exist; most are provider internals. |
| SL31/33/34/36/38–40 | `packages/llm/llm/src/content.ts:151`; `api-key.ts:15`; `message.ts:122,212`; `attribution.ts:16`; `packages/llm/token-meter/src/route-pricing.ts:32`; `breakdown-projection.ts:48`; `index.ts:88` | Projection, key validation, attribution, pricing, and token-meter capabilities exist; this is not certification of every test branch. |
| Rows 12/64/68/69, SS12/18 | `packages/core/agent-loop/src/index.ts:526`; `packages/core/session/src/fork.ts:21`; `packages/session/session-persistence-jsonl/src/lease.ts:70`; `src/index.ts:776,1446`; `packages/core/agent-loop/src/agent.ts:134` | Teardown persistence, fork closers, lease, torn-tail handling, generation selection, and continued turn numbering exist. |
| SS6–8 | `packages/core/session/src/tool-history.ts:27`; `src/index.ts:815`; `packages/llm/llm/src/content.ts:381,424` | Tool-history folding, deferred declaration, and inherited fold exist. |
| SS9–11 | `packages/core/session/src/repair.ts:116,157`; `src/index.ts:856`; `src/surface.ts:119` | Closed-step debt is not repaired later; projection uses existing surface messages. No universal claim about every downstream adapter's rejection behavior is established. |
| SS13/14 | `packages/core/session/src/index.ts:201,555`; `packages/util/values/src/index.ts:31,72,210` | Seed validation, contiguous sequence checks, JSON-safe snapshots, cycles, finite values, and recursive freeze are implemented. |
| SS15/16/20/21 | `packages/core/session/src/index.ts:394,938,982,1080,1178` | Registration lifetime, callback collection before commit, scoped flush, and plugin projections exist. |
| SS17/19/22 | `packages/core/session/src/surface.ts:420,455,510,637`; `seq-ranges.ts:18`; `packages/session/session-format/src/json.ts:23`; `scripts/gen-persistence-catalog.ts:175`; `packages/core/session/src/index.ts:784,856` | Surface validation, codecs, catalog generation, and caches exist. The caches are not inherently dependent on durable storage. |
| ST15/19/20/25/28–34 | `packages/core/tools/src/schema.ts:175,554`; `json-schema.ts:476`; `index.ts:1096,1727,1831`; `ptc.ts:334,554`; `packages/guard/timeout-policy/src/index.ts:55` | Cooperative timeout, authoring validation, canonical output, approval, scoped restrictions, and code-mode tooling exist; the selected Go exclusions are reasonable. |
| ST22–24/26 | `packages/core/scope/src/store.ts:226`; `packages/core/tools/src/index.ts:1083,1230,1845`; `ptc.ts:640` | Disposer handoff, lookup, undo registration, and nested result handling are present; complete rollback depends on Cordis. |
| Row 55 | `packages/spill/spill-policy/src/index.ts:48,91` | Tool-output spill is a separate policy capability; its N/A classification is consistent. |
| Fork part of row 77 | `packages/subagent/subagent-fork-in-process/src/index.ts:48,76` | Fork uses the parent's completed-turn prefix; fresh spawn does not. |

The audit does not certify every individual title count stated by the original sweep.
Grouped rows often cover many tests, and existence of a capability is weaker than proof of every edge-case assertion.
Delegated reads of Cordis vendor code were blocked by the local `.ckignore` rule.
Full captured-chain lifetime and effect rollback semantics therefore remain source-unverified here; callers and tests were inspected.
The full generation-migration chain, every resume publication race, and every provider's response to an incomplete transcript were not proved.
All proposed Ask assertions remain unexecuted because the implementation is pending.
Ask-only rows N1–N8 and W4/W5 require their own contract validation; they cannot be certified by upstream source.

## Required next revision

1. Correct the source claims and split rows that combine different capability boundaries.
2. Define ownership of detached bodies, disposal, and in-flight output writes.
3. Define the prepared-request record and snapshot/cursor operation before their implementation phases.
4. Resolve the input-removal and additional-context scope conflicts without silently changing user decisions.
5. Move assertions to the phase where their prerequisites exist.
6. Add the counterexample tests named in this report, then review assertion meaning rather than only test existence.

## Unresolved questions

- Does the selected five-second settlement bound permit unfinished tool bodies after `agent_settled`, or must a separate execution-drain state remain visible?
- Are removal by input ID and non-waking additional context in scope, or should their dependent matrix cases remain N/A?
- Which prepared-request boundary will satisfy D23's decided exact-model-request requirement without recording credentials?
- Is disposal part of this redesign, including a closed admission gate, or only ordinary cancellation?

These questions arise from contradictory requirements in the reviewed text.
They are not permission requests for changes already authorized.
