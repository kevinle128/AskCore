# DeepSeek tool outcomes, cancellation, and recovery

Date: 2026-10-06.
Status: source research and proposed Ask direction; no runtime implementation.
Source: DeepSeek commit `5badb15009ae1756c3afe0ae0cef1faafc290ccc`.
Method: three scoped sub-agents used GitNexus and read source and tests.
The controller checked related call sites and consumers.
Tests were read, not executed.

## 1. Main correction

DeepSeek has two different mechanisms.
Normal cancellation produces an authoritative cancellation result through the tool pipeline.
Missing-outcome recovery adds conservative error results only when a committed assistant request has no matching recorded result.
Do not reduce both mechanisms to “Stop means outcome unknown.”

The evidence levels are also different.
A recorded `tool/call` means the call reached the logged preparation boundary.
It does not prove that policy allowed the call or that the tool body ran.
A private in-memory `bodyInvoked` flag distinguishes normal cancellation before and after invocation.
That flag is not the same as the recorded call event used by crash repair.

| Outcome | Evidence | What it does not prove |
|---|---|---|
| Successful or failed ordinary result | The tool pipeline finalized that result | That a successful result reached disk unless the persistence guarantee applies |
| `ABORTED_BEFORE_DISPATCH` | Cancellation prevented body invocation on that execution path | That a fork's parent never executed the same inherited request |
| `ABORTED` | Body invocation occurred; cancellation superseded success before finalization | That the operation had no side effects or rolled them back |
| `TOOL_NOT_STARTED` | The repair input contains an assistant request but no recorded call | That a fork parent never ran the request after the cut |
| `TOOL_OUTCOME_UNKNOWN` | The repair input contains a call record but no matching result | That the body ran, failed, or did nothing |

Normal cancellation codes live in tool error information.
Synthetic repair codes live in the Session event's error metadata.
The model mainly receives the tool message's text and error flag, not all internal event metadata.

Source: [cancellation state](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/tools/src/index.ts:1549) and [repair algorithm](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/session/src/repair.ts:105).

## 2. Normal tool execution order

```text
Commit assistant/message with tool requests
  Classify execution mode
  Append tool/call
  Prepare execution and run pre-policy
  Run around-execution middleware
  Invoke body if allowed and not cancelled
  Await body and middleware completion
  Finalize in assistant request order
    Project content
    Run post-policy where applicable
    Apply final output invariants
    Notify tool-capability observers
  Append tool/result to Session
```

The call record precedes preparation and pre-policy.
The capability notification precedes the Session result append.
Neither a call notification nor a result notification alone proves a saved result.
A content projector runs before post-policy; final definition-owned output constraints run after it.

Parallel-safe bodies use a capped rolling pool.
Exclusive bodies are barriers.
Preparation is ordered; post-policy, result append, and additional context admission follow request order.
After a scheduler failure, the owner stops replenishment and drains every started dispatch before surfacing the failure.
A body may have completed successfully but receive an unknown synthetic result if its outcome never committed.

Sources: [coordinator](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/src/tool-calls.ts:147) and [finalization](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/tools/src/index.ts:1641).

## 3. Cancellation edge cases

| Timing | DeepSeek behavior | Evidence |
|---|---|---|
| Group already cancelled | Start no body; append call/result pairs for skipped requests with `ABORTED_BEFORE_DISPATCH` | Coordinator lines 238–259; scheduler tests 459–487 |
| Cancel during pre-policy | Prevent body dispatch and later siblings | Tools lines 1500–1535; scheduler tests 490–518 |
| Around handler delays or replaces signal | Fuse original caller signal with replacement before body invocation | Tools lines 1568–1581; tools tests 1299 and 1337 |
| Wrapper returns success without invoking body after cancellation | Convert success to `ABORTED_BEFORE_DISPATCH` | Tools lines 1624–1626; tools tests 1368–1409 |
| Cancel while body runs | Await the body; late success becomes `ABORTED` | Tools lines 1578–1585; tools tests 1652–1684 |
| Cancel while post-policy runs | Late success becomes the appropriate cancellation result | Tools lines 1652–1654; tools tests 1449 |
| Body or policy returns an error while cancellation also occurs | Preserve the error facts rather than overwrite every error as aborted | Tools success-only guards; tools tests 1235, 1267, 1495, 1527, 1557 |
| Cancel after final result checks, during notification or append | A committed success can remain; stop later scheduling; Turn still ends aborted | Source-derived from tools 1669–1683, coordinator 229, Agent 367–371 |
| Body or awaited policy never settles | Drain and idle can wait indefinitely | Awaited body and pool drain; no termination deadline in reviewed paths |

Cancellation preserves additional context in canonical cancellation results.
It does not preserve late success content as a success outcome.
Body failure still goes through post-policy; execution middleware failure can become a final error that skips post-policy.
The body restores the wrapper signal on exit; the wrapper is responsible for restoring its own upstream signal.
Signal relay listeners are disposed after abort or settlement.

Normal cancellation finishes the tool batch with results.
The driver closes the Step, then checks cancellation outside the Step's recovery catch.
Therefore it does not convert all ordinary cancelled calls into unknown-outcome repairs.

Sources: [tool cancellation and signal fusion](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/tools/src/index.ts:1549), [scheduler cancellation tests](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/tests/tool-calls.spec.ts:459), and [registry tests](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/tools/tests/tools.spec.ts:1177).

## 4. Missing-result tracking

`ToolCallRecovery` tracks committed assistant tool requests in a Map keyed by call ID.
It stores Turn, Step, and an optional sequence of the recorded call.
It does not keep the complete event history or execute tools.

An assistant `tool-call` block adds a pending request.
A matching `tool/call` supplies its recorded call sequence.
An appended `tool/result` removes the pending request only when call ID, Turn, and Step match.
A replacement result does not acknowledge the current request.
A raw call event without a committed assistant request does not create pending debt.
Turn start, Turn end, and Step end clear pending state.

`results()` returns proposed error results without clearing state.
The owner must append them and let the tracker observe accepted appends.
Repeated generation before a commit returns the same proposals.
After each accepted result, only the remaining debt is proposed.
This is commit acknowledgement, not a blanket exactly-once guarantee.

Synthetic results:

- Follow assistant request order.
- Are tool messages with `isError: true`.
- Use explicit not-started or unknown error codes.
- Cite the call event sequence only when that event exists.
- Use deterministic message identities when generated from the same prefix.
- Reuse the last observed event's time and continue its sequence.

Source and tests: [tracker](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/session/src/repair.ts:105) and [tracker tests](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/session/tests/repair.spec.ts:54).

## 5. Three recovery contexts, plus read-only viewing

### 5.1 Failed live Step

The driver installs a tracker after Step start.
Its observer filters by the exact owning Session object.
When the Step throws, the catch appends proposed missing results before Step end.
The finally block disposes the observer and appends Step end.
Turn closing retains the original failure or cancellation reason.
A child Agent or another Session cannot satisfy the owning Step's pending request merely by reusing a call ID.

Classification failure before a call record yields not-started.
Preparation failure after a call record yields unknown, even when the body never ran.
Finalization failure can yield unknown after body completion.
Already committed successful results remain unchanged.

The next model request receives the committed repair messages.
The tracker does not re-execute calls.

Evidence: [driver](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/src/agent.ts:329) and [failure tests](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/tests/tool-calls.spec.ts:634).

### 5.2 Crash resume

Resume obtains write ownership, reads the validated persisted prefix, computes interrupted closers, and appends them before preparing and publishing the Session.
The closer order is missing results, open Step end, then interrupted Turn end.
It balances recorded history; it does not resume a suspended tool body.
A remote side effect may have continued after the harness process died.
Recovery cannot discover that effect from the log alone.

A live Session append commits to memory and schedules persistence; it does not wait for disk I/O.
The generic persistence handle distinguishes append acceptance from flush crash survival.
The JSONL backend fsyncs its append path, but that is a backend guarantee rather than a universal append guarantee.
Do not treat all uses of the word “recorded” as equivalent to fsync.

Sources: [resume](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/src/index.ts:840) and [persistence handle](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-persistence/src/handle.ts:85).

### 5.2.1 Checkpoints before side effects

The mounted checkpoint policy flushes before model dispatch and before tool body entry.
This gives recorded intent a persistence boundary before the operation can have effects.
The crash E2E tests kill a child process after failpoints and check that the request prefix and tool intent survived.
An interrupted call with saved intent but no result becomes unknown after repair.
The checkpoint does not prove that an external effect never happened after intent was saved.
These tests construct balanced views in memory; they are not proof of every production resume-publication path.

Sources: [checkpoint policy](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-checkpoint-policy/src/index.ts:35) and [crash E2E assertions](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-checkpoint-policy/tests/crash-recovery.e2e.ts:92).

### 5.2.2 Physical log recovery

JSONL ignores a final record without a newline.
Malformed JSON or a sequence gap can delimit a recoverable suffix when no later Turn end seals that corruption.
Corruption followed by a later Turn end is rejected.
Some native message-shape validation failures remain fatal before suffix recovery applies.
Recovery retains the valid prefix; it does not discard every event in an incomplete Turn.

The compressed backend can retain complete records from a torn final frame, even if its append was not acknowledged.
A later write repairs the physical tail before adding new records.
Read-only viewing does not perform that durable rewrite.
Thus an unacknowledged operation can still leave recoverable log records.

JSONL batch write/fsync failure attempts rollback to the prior file size and syncs the truncate.
If rollback also fails, it reports both errors and storage is uncertain.
Do not assume blind retry is safe after a failed rollback.

Sources: [physical prefix scanner](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-persistence-jsonl/src/format.ts:461), [compressed recovery](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-persistence-jsonl/src/index.ts:992), and [append rollback](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session/session-persistence-jsonl/src/index.ts:1324).

### 5.3 Fork seed

Fork copies an exact inclusive prefix.
The child gets an inherited seed marker, then its own missing results and forked boundary closers.
The parent is not repaired or modified by these child closers.
Closed Steps and historical missing results in the copied prefix remain unchanged.
The low-level Session fork defaults to the latest event.
The controller and subagent entry points use completed-turn defaults instead; an explicit controller cut can still select a midturn event.
Do not assume all fork entry points have the same default boundary.

Fork guidance is more cautious than ordinary interrupted not-started guidance.
A child may have no call record, while its parent ran the call immediately after the selected fork point.
For both fork repair codes, the message warns about possible parent execution.
Only read-only or idempotent work is suggested for direct retry; side effects require external verification or user input.
This guidance is model-visible text, not a mechanical enforcement of idempotency.

Source: [fork construction](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/session/src/fork.ts:21).

### 5.4 Cold history read

A read-only query also balances an interrupted tail for viewing.
It adds synthetic closers to the returned in-memory array and writes nothing back.
A UI can therefore show a balanced transcript even while stored bytes still contain an open tail.
The consumer must know whether it is seeing persisted entries or a derived balanced view.

Source: [cold read](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/session-query/session-query/src/cold-read.ts:31).

## 6. Related feature: questions that remain answerable

Timed user questions use the structured unknown code as a distinct state signal.
A pending result or `TOOL_OUTCOME_UNKNOWN` keeps the question answerable as continued.
Ordinary cancellation or failure drops the active question.
A late reply settles it only after the reply is admitted as a committed user message.
Adding a reply to the inbox is insufficient because that input can still be discarded.
Legacy blocking questions are not tracked by the timed-question projection.

This is a useful reason to retain structured outcome codes rather than only an error string.
It does not mean every unknown tool should remain executable or automatically run again.

Source and tests: [question projection](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/interaction/user-questions/src/projection.ts:218) and [admission test](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/interaction/user-questions/tests/projection.spec.ts:138).

## 7. Limits that Ask must not copy blindly

| Limit | Actual behavior | Ask design implication |
|---|---|---|
| Recovery append fails | Aggregate original and recovery errors; finally still closes Step and Turn | Do not claim history is complete merely because boundaries closed |
| Closed missing result | Tail recovery does not repair it later | Track unresolved debt separately or explicitly block incomplete projection |
| Partial recovery append failure | Accepted repair entries remain; later debt can remain unanswered | Retry generation must acknowledge committed results and avoid duplicates |
| Duplicate call IDs | Map can overwrite pending entries; provider serializer later rejects duplicates | Validate identity before execution and preserve occurrence identity if needed |
| Late result after synthetic repair | Tracker has no synthetic-to-real reconciliation mechanism | Drain local work and define any external late-result rule explicitly |
| Tool call event | Logged before preparation and also synthesized for skipped work | Name and document it as a recorded call boundary, not proof of side effects |
| Never-settling body | No bounded drain guarantee | Use execution isolation if a hard stop guarantee is required |
| Boundary append also fails | A finally exception can mask an earlier error | Preserve root cause and cleanup failures explicitly |
| Repair guidance | Model receives warnings, not an enforced retry policy | No automatic tool retry solely because a result is unknown |

The closed-missing-result limit is directly demonstrated by the recovery-failure test.
Partial append failure, late result handling, and final boundary exception masking are source-derived limits without dedicated inspected tests.
The provider serializer enforces immediate pairing and rejects duplicated or unresolved call history.
Repair improves pairing on handled paths; it does not prove that every history is always valid.

Evidence: [recovery append failure](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/agent-loop/tests/tool-calls.spec.ts:725), [closed Step preserved](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/core/session/tests/repair.spec.ts:209), and [provider pairing](/Users/dale/Desktop/workspace/opensources/deepseek-harness/packages/llm/llm-deepseek/src/serialize.ts:118).

## 8. Proposed Ask direction

The user selected DeepSeek as the model for this part of the design.
Replace the proposed reliance on D19's replay-only placeholder with recorded tool outcomes and shared missing-result recovery.
This changes the proposed architecture; it does not change the current runtime or silently rewrite the old roadmap record.

Use two paths:

1. Normal cancellation returns typed before-dispatch or after-invocation outcomes, drains started work, and records results for the assistant requests.
2. Unexpected failure or interrupted history computes missing results from committed request/call/result evidence and records conservative not-started or unknown outcomes.

Keep an actual successful or failed committed result.
Never replace known facts with a generic unknown placeholder.
Do not infer that unknown or aborted means no external effect.
Do not automatically re-execute a tool in recovery.
Record recovery origin: live failure, crash interruption, or fork.
Preserve source order, Session ownership, immutable committed snapshots, and original failure causality.
For persistent execution, require an explicit checkpoint before model dispatch and tool body entry.
A failed checkpoint prevents that dispatch; successful intent persistence does not prove external completion.

For Ask, improve on the observed recovery-write failure limit.
If required results cannot commit, expose unresolved tool debt and a failed Run rather than presenting boundary closure as complete history.
A future model request must have an explicit pairing/repair contract; it must not silently invent that the side effect failed.
The precise store transaction and incomplete-history policy belong to the implementation design.

[Detailed architecture proposal](architecture-261006-ask-lifecycle-event-pipeline.md) owns the overall package and pipeline design.

## 9. Verification status and unresolved questions

All three sub-agents finished source and test review.
No runtime code was changed.
No DeepSeek tests or live provider calls were executed.
A local dependency inventory command was blocked by the tool access hook; the investigation used source and test assertions without changing access configuration.

Open implementation decisions:

- What persistent transaction and flush guarantees will Ask require for recovery batches?
- How will Ask expose and prevent model use of unresolved tool debt after a failed repair write?
- Which tool execution boundary facts will be stored, beyond the assistant request and recorded call?
- Will any external tool support late-result reconciliation after crash, and what is its operation identity?
