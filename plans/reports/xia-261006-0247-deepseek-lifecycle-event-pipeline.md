# DeepSeek lifecycle and event pipeline: lessons and proposed Ask architecture

Date: 2026-10-06.
Mode: `ak:xia --improve`, restricted by the user to research and a high-level architecture report.
Status: discussion material; no design accepted, implementation plan produced, or runtime change made.
Audience: the Ask maintainer who wants to assess the value before deciding on a change.
This report uses simple English for the technical record.

The later [detailed architecture proposal](architecture-261006-ask-lifecycle-event-pipeline.md) records the current design discussion.
It supersedes this report's suggestions to retain old hooks through compatibility adapters.
The user requested direct replacement of the internal API.
Neither document is an accepted implementation design.

## 1. What Ask would gain compared with the current version

The main gain is a clear contract for each execution boundary.
The current Ask driver already has useful stages, typed hooks, ordered message results, and cancellation handling.
DeepSeek adds a distinction between a user work cycle, a model-and-tool round, and a model request attempt.
It also separates control hooks from observations and committed history.
These ideas fit Ask's existing packages; they do not require Cordis or a new plugin framework.

| Area | Ask now | Proposed behavior inspired by DeepSeek | Concrete gain | Requires runtime change? |
|---|---|---|---|---|
| Lifecycle | `Turn` means one model response and its tools | A semantic Turn contains Steps; each Step contains model Attempts and a tool batch | One user input that needs several tool rounds stays one understandable work cycle | Yes; names alone do not establish the boundary |
| Retry | No harness attempt/recovery loop in the reviewed agent driver | Retry an eligible model failure inside the open Step | A temporary failure does not add user input again or restart completed tool work | Yes |
| Hook timing | `PrepareRequest` and `FinishTurn` describe each current model/tool round | Separate Step admission, Attempt preparation, Step completion, and Turn stopping | Hook authors know which side effects repeat on retry and which decisions apply to the whole Turn | Yes |
| Observation | A `Subscribe` error or panic can stop the run; a slow listener stalls it | Passive observers have a separate failure contract from control and persistence | A failed monitor need not stop useful agent work; policy and storage failures still stop it where required | Yes; a new contract is needed, not a silent change to `Subscribe` |
| History | `ContextSource` stores completed messages, not all lifecycle or request facts | Session entries also explain accepted input, request changes, attempts, and outcomes | After a failure, inspection can explain what was accepted, attempted, and committed | Yes; durable storage is separate from naming |
| Streaming | Partial updates and final messages share the agent event surface | Transient frames identify an Attempt; terminal frames refer to committed outcomes | Clients can distinguish visible progress from saved results and failed attempts from final answers | Yes |
| Parallel tools | One sequential tool makes the whole batch sequential; otherwise all jobs start in their own goroutines | Optional bounded execution with barriers and ordered post-processing | Bound resource use, retain concurrency around exclusive work, and make stateful post hooks predictable | Yes; this is an independent behavior change |
| Failure outcomes | Failure handling creates an error assistant message and terminal events | Track actual open boundaries and distinguish tool-not-started from outcome-unknown | Avoid treating uncertain side effects as work that safely did not happen | Yes; no automatic tool retry follows from this |
| Tracing and usage | Events have `sessionId` and `runId`; no Step or Attempt identity | Correlate Turn, Step, model Attempt, and tool call separately | Show whether time and cost came from useful rounds, retry waits, failed requests, or tools | Identity alone helps; complete usage accounting needs additional work |

Current owners: [driver](../../internal/agent/loop_run.go), [stages](../../internal/agent/loop_stage.go), [stream](../../internal/agent/loop_stream.go), [dispatch](../../internal/agent/emit.go), [tool batch](../../internal/agent/loop_tools.go), and [event contract](../../pkg/protocol/events.go).

The largest functional gains would come from retry isolation, explicit hook scopes, and separate observation contracts.
The lifecycle vocabulary makes these gains understandable and testable.
Renaming the existing `Turn` alone would improve the explanation but would not add those behaviors.
This analysis does not prove better model answers, lower token cost, or faster completion.
Retry can increase cost, and ordered result commits can wait for an earlier slow tool.

## 2. Source manifest and evidence limits

| Item | Inspected state |
|---|---|
| Source repository | `deepseek-ai/deepseek-harness` |
| Local checkout | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` |
| Branch and commit | `master`, `5badb15009ae1756c3afe0ae0cef1faafc290ccc` |
| GitNexus | Index reports the same commit, indexed on 2026-10-06 at 02:47:08, up to date |
| Navigation | GitNexus queries and exact symbol context, followed by direct source and test reads |
| Main source scope | Architecture, lifecycle, tool pipeline, agent dispatch, inbox, request preparation, assistant stream, tool scheduler, Session recovery, retry policy, and compaction hooks |
| Ask baseline | HEAD `fc8dd22c20363c42a11f3125f0a4af19c0236a40`, branch `master-2`, plus the inspected uncommitted working tree |
| Verification | Source and test assertions read; no DeepSeek scripts, provider calls, or runtime tests executed |

The user updated the checkout during research.
The final source analysis uses the refreshed GitNexus index and checkout, not the earlier `0d1f500` revision.
The graph locates relevant symbols; the source establishes behavior and ordering.
The available skill catalog has no Repomix skill or sequential-thinking skill, so scoped GitNexus navigation and an explicit flow trace replace those workflow steps.
The user's report-only scope overrides the Xia plan and implementation handoff.

Pinned entry points: [architecture](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/architecture.md), [agent lifecycle](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/agent-lifecycle.md), and [tool pipeline](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/tool-execution-pipeline.md).

## 3. The concepts worth keeping

| Concept | Meaning | Boundary |
|---|---|---|
| Session | History and its projections | Survives separate periods of agent activity when persistence is enabled |
| Agent / Driver | Live execution owner | Owns input delivery, cancellation, hooks, and ordered work |
| Turn | A work cycle opened before the first input claim | Ends on completion, block, error, cancellation, or another recorded terminal reason |
| Step | One model-and-tool round within a Turn | Can contain several model Attempts under retry |
| Attempt | One loop-visible model request attempt | Settles as a successful message or a failed/log-only attempt |
| Stage | An ordered part of a Step's implementation | Does not mean a model round or a request attempt |

A Turn can contain zero Steps.
For example, the first proposed input can be rejected before `step/start`.
A Step can open and then fail during preparation before its first assistant stream starts.
The documentation's phrase “one model request plus tools” describes the normal Step; the retry loop makes the distinction between a logical round and its individual requests explicit.
See [Turn and Step driver](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L296).

The reviewed DeepSeek lifecycle does not introduce `Run` as another conversation-history tier.
Ask's existing `run` is an execution handle with its own ID, context, cancellation function, and completion channel.
It remains useful for `Prompt`, `Continue`, `Abort`, and `WaitForIdle`.
It is not the Session and need not sit between Session and Turn in the domain model.
See [Ask execution handle](../../internal/agent/agent.go).

```text
Conversation history                     Live execution

Session                                 Agent
  Turn 1                                  active run handle
    Step 1                                  cancellation and completion
      model Attempt 1                       drives work in the Session
      model Attempt 2
      tool batch
    Step 2
      model Attempt 1
  Turn 2
```

This is a proposed way to explain Ask, not an accepted event schema.
One active run can already process follow-up input through the loop's hook seam.
Whether a follow-up batch opens one semantic Turn or several Turns is still a local decision.
DeepSeek takes one queued next-turn message; Ask queue hooks can return a batch.

## 4. Event pipeline and Turn flow solve different problems

Turn flow owns the order of work and its transitions.
The event pipeline exposes defined places where policy or extensions can affect that work.
The driver remains the owner of ordering, even though DeepSeek implements the product as Cordis plugins.

There are two separate classifications.
An event's domain states what it concerns; its dispatch contract states how listeners run.

| Domain | Purpose | Examples |
|---|---|---|
| Session | Committed facts for history and replay | Turn/Step boundaries, users, assistant settlements, tool results |
| Agent | Live execution and control | Input claims, request construction, status, streaming, stopping |
| Capability | Policy near an operation | Tool execution, filesystem mutation, telemetry |

| Dispatch contract | Listener power | Failure consequence |
|---|---|---|
| Waterfall / around middleware | Wrap `next()`, change the returned decision, or stop delegation | Failure can terminate the operation; callers must distinguish it from a provider failure |
| Awaited serial checkpoint | Run ordered lifecycle work | Its failure can stop progress; the driver checks state again afterward |
| Notification | Observe a fact | Agent notifications contain each listener's throw or rejected promise; they cannot veto progress |

Source: [agent dispatcher](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent/src/dispatch.ts#L54).
Notification isolation does not make a synchronous slow callback free of latency.
It also does not prove that every `session/event` listener has the same failure contract.
Session append remains an authoritative boundary; a storage failure must not be treated like a failed monitor.

Ask's `pipeline.Compose` already has typed merge rules for transforms, decisions, keys, and tool policy.
It is a useful local equivalent, but it is not the same as Cordis around middleware.
Existing transforms can retain their current contract, but `Compose` alone does not provide DeepSeek's control composition.
Around middleware is useful for admission and recovery decisions as well as operation wrappers such as timeouts.
The deeper comparison in section 11 explains why the earlier recommendation to extend `Compose` needs this qualification.
See [Ask pipeline contract](../../internal/pipeline/README.md) and [composition](../../internal/pipeline/hooks_compose.go).

## 5. The important ordering and commit boundaries

The source flow teaches four boundaries that should not be merged.

### Input claim and admission

The driver opens a Turn, removes the proposed input from the inbox, assembles prompt and tools, and invokes `agent/pre-step`.
The returned admission decision is authoritative.
Rejected claimed input is not automatically requeued.
The original insertion remains in the event history, but a claim is not yet a committed user message.
This is a delivery policy, not an exactly-once transaction across every phase.
See [pre-step](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L267) and [inbox claim](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/inbox.ts).

### Step preparation and Attempt preparation

After admission, the Step retains its assembled prompt and tool schemas.
Each Attempt resolves the actual request route and adapter capabilities again.
Only then does the driver reconcile the system prompt, admit user messages on the first Attempt, and derive the request.
Cancellation during the asynchronous request hook or adapter preparation commits neither the system prompt nor the accepted users.
Earlier inbox and runtime-context facts can already have been logged.

Retry repeats Attempt preparation but does not repeat Step assembly, pre-step admission, or user-message insertion.
It need not send identical request bytes: recovery can change the projected history or resolved route.
See [Attempt loop](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L398).

### Stream visibility and settlement

Start and chunk frames are transient observations.
At settlement, the loop appends `assistant/message` or `assistant/attempt` before sending a committed end frame that cites the Session sequence.
An append failure produces an abandoned end frame.
Failed attempt content is retained for inspection without automatically joining model history.
Safe interrupted content can become an explicitly interrupted assistant message.

There are no generic durable `attempt/start` and `attempt/end` events in this loop.
The retry plugin separately records a scheduled retry and a retry that started after its wait.
Live Attempt IDs are unique within the attached Agent lifecycle, not across all future Session attachments.
Hard process loss before settlement can lose the current stream.
Logical append does not by itself establish a per-event disk flush guarantee.
See [assistant stream](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/assistant-stream.ts#L17) and [retry policy](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/llm/llm-retry/src/index.ts).

### Step completion and Turn stopping

Tool results or next-step input can require another Step within the open Turn.
The natural stopping checkpoint runs only when the Step has a terminal outcome and the next-step inbox is empty.
A stopping listener can enqueue next-step input, which the driver checks before it closes the Turn.
Follow-up messages target later Turns; steering targets the next Step; injected context does not wake the driver by itself.
When steering wakes an idle agent, it can start work rather than join a Turn that no longer exists.
See [input APIs](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L154) and [Turn stopping](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L358).

## 6. What the updated source adds to the lesson

### Failure recovery must say what is known

The current Step observes committed tool requests and results.
If the Step fails, `ToolCallRecovery` supplies conservative error results before `step/end`.
It distinguishes a request that never acquired a logged `tool/call` from a logged call with no committed outcome.
The call is logged before pre-policy, so that record does not prove that the tool body started.
The latter is outcome-unknown, not proof that the side effect failed.
If recovery recording also fails, the driver preserves both the original error and the recovery error.

This improves transcript validity without claiming exactly-once external effects.
Ask can learn the distinction without adding crash-resume or automatically re-executing a write tool.
See [Step recovery](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L331) and [recovery state](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/session/src/repair.ts#L105).

### Tool policy has an order that matters

The current tool pipeline separates pre-policy, monotonic guards, around-dispatch work, prepared content, post-policy, final content invariants, and final observation.
The new `projectContent` hook installs prepared content before post-policy, so projection does not overwrite a later policy replacement.
The definition's `finalizeContent` can still enforce a later content invariant; post-policy is not always the final content writer.
Guards can deny or abstain; a later allow decision cannot override another guard's denial.
Post-execute policy changes the model-facing result; it does not undo a file edit or other completed side effect.

The lesson for Ask is to define which layer may change which fields and when its answer becomes final.
It does not require copying DeepSeek approval prompts, filesystem policy, or its full tool registry.
See [tool pipeline](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/tool-execution-pipeline.md) and [result finalization](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/tools/src/index.ts#L1640).

### Request history explains capability changes

The updated request builder logs tool availability changes as developer messages and supplies Session tool history to the model adapter.
Adapter capabilities determine how system and tool updates are rendered.
The useful principle is that the request's model-visible state must be explainable from recorded inputs and defined projections.
The updated service also owns inbox and Turn-boundary projections, so readers can inspect pending input and boundaries without a live Agent.
Provider-specific wire rendering still belongs to the provider.
See [request construction](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L598).
See [shared projection ownership](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/index.ts#L354).

Ask already records tool declarations and owns provider rendering.
Its `TransformContext` result is deliberately not stored, so exact request reconstruction is not yet guaranteed by the message log alone.
Do not remove this valid extension behavior by claiming that every transform must become a user message.
A future replay contract must define recorded inputs, projection identity, and the effective request outcome.
See [context source](../../internal/agent/context_source.go), [tool declarations](../../internal/agent/loop_tool_changes.go), and [transform contract](../../internal/pipeline/hooks.go).

## 7. High-level architecture for Ask

The proposed shape keeps the current modular monolith and its import rules.
It gives each existing package a clearer responsibility instead of adding a second runtime.

```text
Go API / ACP / headless
          |
          v
internal/agent
  one execution owner; input delivery; Turn and Step transitions
  existing stages operate inside a Step
  Attempt loop wraps model work, not the whole tool round
          |
          +---- internal/pipeline: typed admission, request, recovery,
          |                       Step-completion and stopping contracts
          +---- internal/providers: bound model request, wire rendering,
          |                        failure classification, stream producer
          +---- internal/tools: registry snapshot, validation, tool bodies
          +---- internal/sessions: committed entries and context projection

Committed facts --> observation feed / bus --> tracing, clients, monitor
Control hooks   --> awaited by the owner at their defined boundary
```

This diagram describes runtime calls, not permission for reverse imports.
`pipeline`, `providers`, and `tools` still do not import `agent`.
The composition root wires dependencies; transport adapters wrap the runtime.
Scheduler remains the owner of lanes and limits rather than input queues or conversation transitions.
See [architecture authority](../../docs/ask-architecture-reference.md) and [package rules](../../internal/README.md).

| Proposed responsibility | Existing owner | Distillation |
|---|---|---|
| Session projection and committed outcomes | `sessions` | Extend the owning history contract when that scope is accepted; keep the entry-tree model |
| Run lifetime, Turn/Step transitions, queue admission | `agent` | Preserve one active execution owner and add explicit semantic boundaries |
| Step stages | `agent/loop_stage.go` | Keep steer, prepare, reason, act, observe, decide as internal implementation stages |
| Extension decisions and composition | `pipeline` | Use typed inputs/results and existing `Compose`; distinguish Step-level and Attempt-level effects |
| Recovery policy | `agent` with provider classification and pipeline hooks | Decide retry eligibility separately from executing another Attempt |
| Tool execution | `agent` and `tools` under current ownership | Keep the declared snapshot stable for a Step; assess bounded scheduling separately |
| Passive observation | Event producer plus existing `bus`, `hooks`, `tracing` surfaces | Publish committed outcomes and transient progress with explicit failure and backpressure contracts |
| External event mapping | `pkg/protocol` and adapters | Preserve or explicitly version existing JSONL and ACP meanings |

### Hook scopes to discuss

The following are conceptual responsibilities, not final API names.

| Scope | Responsibility | Runs again on model retry? |
|---|---|---|
| Before Step admission | Accept, rewrite, or reject proposed input; capture prompt/tool state | No |
| Before model Attempt | Resolve current route, credentials, request projection, and provider options | Yes, according to the hook's declared contract |
| After failed model Attempt | Classify failure and choose bounded recovery or termination | For each eligible failed Attempt |
| Tool pre/body/post | Validate and execute a tool from the admitted Step snapshot | No automatic repeat caused by model retry |
| After Step | Observe the assistant/tool round and request the next flow | Once per settled Step |
| Before Turn closes | Run final checks and explicitly request continuation | At natural stopping boundaries |

The current `FinishTurn` is a Step-level seam under this vocabulary.
Its `End` currently ends the whole Run, and its `Continue` can be satisfied by tool results, steering, or follow-up input.
Moving it to a new Turn-level checkpoint would change those semantics.
Retain the behavior through a defined compatibility contract unless the user accepts a change.
The current `NewMessages` is cumulative for the Run, not a per-Turn or per-Step slice.
See [decision types](../../internal/pipeline/hooks.go), [driver priority](../../internal/agent/loop_run.go), and [decision tests](../../internal/agent/loop_run_test.go).

### The retry boundary

A model retry belongs inside the reason/model portion of the Step.
It must not replay steer, input admission, tool execution, observe, or Step completion.
An SDK's internal HTTP retry is not automatically another harness Attempt unless the harness exposes it.
Attempt identity must cover the loop-visible stream and its terminal outcome; preparation failures need their own clear scope.

The existing Ask rule remains: mid-stream public output cannot be transparently replayed.
Attempt records improve diagnosis but do not grant permission to hide an emitted prefix and silently start again.
Ask should retain its existing retry eligibility and billing constraints, including no billed fallback after exhausted subscription allowance.
See [queue/retry constraints](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h9-queues-abort-retry-usage).

### The observation boundary

Policy decisions must remain awaited and able to stop execution when their contract requires it.
Session commit failures must remain visible failures.
Passive telemetry and display listeners should have a separate notification contract.
A bounded observation feed needs explicit overflow and replay behavior; moving callbacks into arbitrary goroutines is not enough.
Do not silently alter the current `Subscribe` contract, which is covered by listener-order and listener-failure tests.
See [dispatch](../../internal/agent/emit.go) and [listener tests](../../internal/agent/agent_test.go).

## 8. Dependency and fit matrix

| Source component | Ask equivalent | State | Recommendation |
|---|---|---|---|
| Driver and explicit flow | Loop and `flow` enum | EXISTS | Reuse; add semantic Turn boundary around rounds |
| Step implementation | Current `turnStages` and `turnState` | EXISTS, naming CONFLICT | Distinguish Stage from Step; assess event compatibility before renaming |
| Attempt settlement and recovery | Current model stream and failure path | NEW harness contract | Add only as part of an accepted retry/history scope |
| Typed extension decisions | `Hooks` and `Compose` | EXISTS | Extend local contracts rather than import Cordis |
| Session events and projections | `ContextSource`, `MemoryLog`, intended entry tree | PARTIAL | Preserve Session ownership and entry-tree direction |
| Durable inbox | External steering/follow-up hook sources; no queues on current Agent wrapper | PARTIAL | Preserve local queue modes; do not assume a source-equivalent inbox already exists |
| Passive notifications | Synchronous fail-stop `Subscribe` | CONFLICT | Provide an explicit observer contract without silently weakening control listeners |
| Tool scheduling | Snapshot plus sequential/parallel strategies | EXISTS, behavior CONFLICT | Bound execution if adopted; keep declaration/execution consistency |
| Final outcome repair | Provider replay normalization and current error handling | PARTIAL | Assess recording unknown/not-started outcomes without automatic re-execution |
| Plugin tree, YAML profiles, hot reload | fx and capability packages | CONFLICT | Keep Ask composition and package boundaries |

This adaptation does not require a new database, event broker, runtime framework, or package hierarchy.
Persisted lifecycle facts can affect the eventual Session schema, but this report does not choose a schema or migration.

## 9. Challenges before treating this as a design

| Question | DeepSeek answer | Ask answer today | Risk if copied without checking | Discussion recommendation |
|---|---|---|---|---|
| Is a Turn a model round or a whole input work cycle? | A work cycle with zero or more Steps | A model response and tool batch | Existing event readers count and close Turns differently | Adopt the distinction conceptually; decide wire compatibility separately |
| Does rejected input remain queued? | Claimed input is removed before admission | Queue hooks return input; the Agent wrapper has no owned inbox | Lost or duplicate delivery if rejection behavior is assumed | Define accepted, rejected, cancelled, and pending outcomes explicitly |
| Which hooks repeat during retry? | Request preparation repeats; Step assembly/admission does not | No Attempt contract; `PrepareRequest` can update later state | Repeated hook side effects or stale credentials/context | Give each hook a declared scope before adding retry |
| Can a monitor veto work? | Agent notification cannot; awaited control can | Any `Subscribe` error can end the run | Policy failures could be ignored, or display failures could stop tools | Separate contracts; preserve existing API behavior until accepted |
| Must a request be reconstructable? | Model-visible state comes from logged facts and defined projections | Transform output is intentionally not stored | Replay differs from the real request | Decide the reconstruction guarantee without removing valid transforms |
| Can pending tools use a changed registry? | Pending calls can be reclassified from the live registry | One declared/executable snapshot per current round | A model calls a tool definition that differs from what it saw | Keep Ask's snapshot invariant; adapt scheduling independently |
| Does tool failure prove no effect? | Recovery distinguishes not-started from unknown | Transcript normalization can repair unmatched requests | A repeated write can duplicate or corrupt an external effect | Record uncertainty; never infer safe tool retry from model retry |
| Does `Continue` mean reopening an old Turn? | Durable boundaries and attachment recovery are separate mechanisms | `Continue()` starts a new Run on valid context | Reopening closed history or changing End/follow-up priority | Do not infer Turn resume from the method name |
| Should compaction run before every Step? | Source supports pre-step pressure and overflow recovery | Existing roadmap excludes proactive mid-turn compaction | Lifecycle adoption silently expands product behavior | Reuse safe seams without changing accepted compaction policy |

Integration risk is substantial at public contracts: event meaning, hook timing, and input delivery can change user-visible behavior.
The fit at package boundaries is strong.
The appropriate architecture direction is selective adaptation, with each contract change still open for discussion.

Existing decisions remain authoritative: Pi semantics inside Ask's capability packages, no mid-turn crash-resume requirement, scheduler lanes rather than queue ownership, and current compaction exclusions.
See [decision record](../260930-2254-pi-feature-inventory-go-roadmap/plan.md) and [roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md).

## 10. Evidence that would make a future change convincing

These are useful properties from the reviewed tests, not a test implementation plan or a claim that tests passed.

| Property | Source evidence | Why it matters for Ask |
|---|---|---|
| Two retries keep one Turn and Step and admit users once | [request recovery](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/tests/request-error.spec.ts), [prompt admission](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/tests/system-prompt-admission.spec.ts) | Proves retry isolation |
| Cancellation wins over a returned retry action | Same request-recovery tests | Abort must stop recovery as well as streaming |
| Provider failure and middleware failure follow different paths | Same request-recovery tests | Prevents retry of arbitrary hook errors |
| Results and post-policy follow model order despite reverse body completion | [tool scheduler tests](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/tests/tool-calls.spec.ts) | Preserves deterministic context and hook effects |
| Started tools drain; failed Step records conservative results before closing | Same tool-scheduler tests and [repair tests](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/session/tests/repair.spec.ts) | Preserves transcript validity without claiming rollback |
| Failed stream settlement is distinct from final answer history | [cancellation tests](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/tests/cancel.spec.ts) | Makes clients and replay agree about visible work |
| Existing End and Continue priorities remain explicit | [Ask loop tests](../../internal/agent/loop_run_test.go), [composition tests](../../internal/pipeline/hooks_compose_test.go) | Avoids changing accepted extension semantics by renaming |

No speed or cost benchmark was performed.
Tool draining is cooperative; a body that ignores cancellation can still delay settlement.
DeepSeek's ordinary retry mode is bounded, but its explicit always mode can continue until cancellation; that is a source policy choice, not a recommendation for Ask.
A stopping checkpoint alone cannot enforce a budget on tool loops that have not reached natural stop.

Two documentation conflicts remain in the updated source.
The agent-loop README describes stream chunks as published after durable settlement and uses an aborted terminal outcome, while the stream implementation publishes transient chunks and uses committed or abandoned end frames.
The runtime-type comment about rejected steering remaining parked conflicts with claim-before-admission behavior and the later claimed-input contract.
This report follows the implementation and focused tests for those claims.
See [stream implementation](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/assistant-stream.ts), [README](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/README.md), and [runtime contracts](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent/src/runtime-types.ts).

## 11. Deeper Event Pipeline findings

This follow-up uses the same current GitNexus index and commit.
It traces real admission, post-tool, compaction, retry, stream, and scope handlers in addition to the dispatcher.
The findings change an earlier recommendation: adding hook fields to the current `Compose` does not by itself reproduce DeepSeek's Event Pipeline.

### A waterfall composes control, not just data

The caller supplies event arguments and an innermost default operation.
Listeners wrap that operation through `next()` in registration order, with optional prepend priority.
The first registered outer listener enters first and can inspect the final downstream decision on the return path.
A listener can return its own decision without delegating; the remaining inner listeners and the default do not run.
Already-entered outer listeners can still process that returned decision.

```text
Caller
  A: work before
    next -> B: work before
      next -> Default operation or decision
    B: inspect/transform returned result
  A: inspect/transform returned result
Caller receives final result
```

The result can be a decision, request configuration, normalized tool outcome, or a stream.
The default is event-specific: enter input, use seeded model configuration, allow a tool, accept its result, preserve a terminal request error, or invoke the resolved stream adapter.
Returning a replacement decision does not itself commit Session history or advance the driver.
The operation owner interprets and validates that decision.
See [waterfall tutorial](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/cordis-tutorial/04-events.md#waterfall-transform-or-short-circuit), [request preparation](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent-loop/src/agent.ts#L547), and [tool policy](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/tools/src/index.ts#L1496).

### Admission shows why this matters beyond timeout wrappers

The Codex hook bridge first runs its user-input hook.
If that hook rejects input, it returns reject without delegating.
If the hook only supplies context, it calls `next()` and receives the later admission decision.
It adds its context only when that decision is enter, preserving the downstream decision fields.
It does not turn a later rejection back into an acceptance or discard a later request-series declaration.

This means that context injection can cooperate with policy even when the injector is registered outside the policy listener.
Ask's forward transform chain cannot express this outer-after-inner behavior through its current function signature.
It can implement selected outcomes through special merge rules, but the extension cannot directly inspect the completed downstream decision.
See [Codex admission bridge](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/hooks/hooks-codex/src/index.ts#L205) and [Ask forward composition](../../internal/pipeline/hooks_compose.go).

### Recovery composes alternative owners

Compaction recovery checks for canonical context overflow and meaningful surface progress.
When it cannot recover, it delegates to the next recovery listener.
When it owns recovery, it returns retry without delegating.
The retry policy can also delegate unsupported errors or exhausted normal retries.
Its explicit always mode first asks downstream recovery and preserves a downstream retry decision before considering its fallback.

Thus handler order and return-path behavior are part of the contract.
There is no universal rule that the first handler always wins or that every handler must run.
The driver still opens the next Attempt and checks cancellation after receiving the final action.
See [compaction recovery](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/compaction/compaction-basic/src/index.ts#L190) and [retry recovery](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/llm/llm-retry/src/index.ts#L195).

### Middleware does not replace mandatory invariants

An extensible waterfall can short-circuit downstream listeners.
DeepSeek therefore runs mandatory tool guards outside the pre-policy waterfall.
An allow result from a middleware is still subject to those guards and caller cancellation checks.
Execution identity remains protected, and a wrapper cannot detach the original cancellation signal.
For streamed model calls, loop-built requests remain immutable; middleware can wrap or supply the stream but cannot rewrite the frozen request history.
See [guard boundary](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/tools/src/index.ts#L1519) and [stream contract](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/llm/llm/src/index.ts#L63).

### Event mode also defines return and failure behavior

The framework's serial mode can stop at a non-null, non-false, non-undefined return value.
The specific Turn-stopping contract returns void and expresses continuation by queued input, so it does not use that generic bail result as a stop/continue vote.
Agent notification wrappers separately contain per-listener failures; ordinary control waterfalls propagate failures to their operation owner.
Scope filtering selects which listeners participate before dispatch.
Agent dispatch couples the payload Agent to its routing scope, preventing a caller from routing one Agent while naming another in the payload.
Registrations have a lifetime and a disposer; retry teardown also cancels and drains active recovery work.
See [dispatch modes](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/docs/cordis-tutorial/04-events.md), [agent dispatch](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/agent/src/dispatch.ts), and [scope routing](https://github.com/deepseek-ai/deepseek-harness/blob/5badb15009ae1756c3afe0ae0cef1faafc290ccc/packages/core/scope/src/index.ts).

`llm/stream` returns an async iterable rather than a promise of the final response.
A wrapper that measures only the `next()` call measures stream construction, not the whole request.
It must wrap stream iteration and cleanup to cover completion, failure, and cancellation.
This distinction matters when translating the model into Go's stream producer and consumer contract.

### Revised architecture recommendation for Ask

Use typed around middleware for control seams where extensions need to delegate, own a decision, or inspect downstream results: Step admission, request configuration, request recovery, and tool pre/execute/post processing.
Keep the existing capability packages and let each operation owner supply its terminal default and enforce mandatory invariants.
Keep notification and awaited checkpoint contracts separate from middleware.
Keep plain forward transforms where their restricted behavior is the intended public contract.
Use existing hooks as compatibility adapters where needed; do not claim that adapting them makes every old merge rule equivalent to a waterfall.

The proposed improvement is a richer control-composition contract, not simply a generic event bus or additional hook names.
It does not require importing Cordis or replacing fx.
It does require explicit choices about ordering, short-circuit authority, mutable fields, return types, cancellation, scope, and registration lifetime.
Before choosing Go signatures, verify whether a control seam permits repeated delegation and how a stale handler is prevented from entering disposed work; this review does not establish a universal single-use `next()` guarantee in the underlying framework.
No implementation or accepted-contract change follows from this revised recommendation.

## 12. Open discussion points

1. Should Ask expose new Turn/Step meanings through a versioned event contract, or preserve the current wire events through an adapter?
2. What should one semantic Turn own when a follow-up hook returns several messages?
3. Which existing hooks must remain Step-level, and which can safely run for every Attempt?
4. Should passive observers get a separate API while `Subscribe` retains its current fail-stop behavior?
5. How much request and failed-stream detail should Session history retain for replay and inspection?
6. Should bounded tool execution be part of this lifecycle change or a separate proposal under the same architecture?

These points are deliberately unresolved.
The report records what we learned and a proposed architecture direction; it does not authorize implementation or change existing project decisions.
