# Independent audit: lifecycle and Event Pipeline report

Date: 2026-10-06.
Target: [architecture-261006-ask-lifecycle-event-pipeline.md](architecture-261006-ask-lifecycle-event-pipeline.md) (called "the report" below).
Method: four independent verifiers checked the report against source, not against the report's own references.
DeepSeek evidence: local checkout at `5badb15009ae1756c3afe0ae0cef1faafc290ccc` (the same commit as the report and the GitNexus index).
Ask evidence: the `master-2` working tree, including uncommitted changes. The `internal/agent` and `internal/pipeline` tests pass.
The two sibling reports (`xia-261006-0247-*`, `research-261006-0836-*`) were not used as evidence.
DeepWiki was used only for orientation. A local `.ckignore` rule blocks `vendor/cordis`, so one detail (double `next()` in Cordis waterfall) rests on DeepWiki and DeepSeek docs, not on the Cordis source.

## 1. Verdict

1. **Facts about DeepSeek: accurate.** All 9 driver/dispatch claims and the main tool/session claims match the source. No DeepSeek fact in the report is wrong.
2. **Provenance is not labelled.** Several designs read as "learned from DeepSeek" but are Ask inventions or deliberate reversals of DeepSeek behavior. The report does not mark which is which (section 3).
3. **Facts about current Ask: mostly accurate, with four errors** of "current versus future" (section 4).
4. **Fit: the main risk is not technical.** The report replaces Pi-based decisions (D19, and implicitly the H9 queue and retry design) with DeepSeek semantics. I found no recorded user decision for this (section 5).
5. **Depth: equal or deeper on dispatch, tool coordination, repair, and replay. Shallower on how a model answer ends** (max-tokens, truncation, cancel with partial output, empty or reasoning-only output) and on failure rules per control point (section 6).

The report is a good base. It is not ready for acceptance until the provenance labels, the four current-state errors, the missing edge cases, and the D19/H9 decision are resolved.

## 2. DeepSeek fidelity: claim verdicts

| Report claim | DeepSeek source | Verdict |
|---|---|---|
| Turn / Step / Attempt; many rounds per input cycle; Attempt opens right before the model call | `packages/core/agent-loop/src/agent.ts:296-544`; `docs/agent-lifecycle.md:24-83` | Correct. DeepSeek has no attempt start event; an attempt is recorded only when it settles (`assistant/attempt`). |
| Around middleware with `next` for request, stream, and tool points | `packages/core/agent/src/dispatch.ts:54-147`; `runtime-types.ts:259-393` | Correct. |
| DeepSeek does not enforce at-most-once `next` (report line 330) | Cordis waterfall (DeepWiki); `packages/llm/llm/src/index.ts:946-960` | Correct but incomplete. A prepared LLM call throws `INVALID_PREPARED_CALL` on a second dispatch, so model dispatch is already one-shot. |
| Handler snapshot, nested scopes, HMR | `dispatch.ts:121-136`; `packages/core/scope/src/index.ts:33-37` | Correct. |
| Steering at next step boundary, follow-up opens a new turn, inbox cleared on cancel by default | `agent.ts:154-181, 359-395`; `inbox.ts:97-114` | Correct. |
| Retry loop inside the step; cancellation wins; mid-turn compaction as recovery | `agent.ts:406-510`; `llm-retry/src/index.ts:183-242`; `compaction-basic/src/index.ts:158-234` | Correct. |
| Stop aggregation differs from "all results terminate" | `tool-calls.ts:95, 158` | Correct, but the report never states the DeepSeek rule: **any** successful result with `concludesTurn` concludes the step, unless queued context or a turn-stopping steer continues it. |
| Ordered tool coordinator, bounded rolling pool, exclusive barrier | `tool-calls.ts:85-231`; `constants.ts:6` (default 10) | Correct. This design is DeepSeek's, not an Ask invention. |
| `tools/result` notification fires before the driver appends the result | `packages/core/tools/src/index.ts:1669-1683` | Correct. |
| Repair distinguishes not-started from unknown outcome; never re-executes | `packages/core/session/src/repair.ts:14-197` | Correct. |
| Cancel stops scheduling, drains started bodies, records abort outcomes | `tool-calls.ts:221-260`; `tools/src/index.ts:1548-1627` | Correct. |
| Observer failure is contained; live frames separate from committed events | `dispatch.ts:55-63`; `docs/agent-lifecycle.md:83, 89` | Correct for `emit`. `serial` and `waterfall` listeners do propagate throws. |
| Credentials per attempt | `agent.ts:547-596`; `llm-deepseek/src/adapter.ts:43-82` | Correct as a fact, but DeepSeek resolves credentials inside the adapter, not in the driver. |

## 3. Designs that are Ask's own, not DeepSeek's

The report sometimes marks these as Ask proposals and sometimes leaves them unmarked. Each needs an explicit "Ask choice, differs from DeepSeek because ..." line.

| Report design | What DeepSeek does | Evidence |
|---|---|---|
| "Ordered decision handlers" with a precedence rule (`CompleteStep`, `StopTurn`) | No decision rule. `agent/turn-stopping` is `serial` and its return value is ignored; a listener continues by calling `agent.steer()`. Source: "listener order cannot change the outcome". | `runtime-types.ts:364-381`; `agent.ts:316-363` |
| Tool definition snapshot per Step (report lines 439-440) | Reads the live registry on purpose ("registry changes affect unstarted calls") and re-checks each call's mode before start. | `tool-calls.ts:86, 201-205`; `tools/src/index.ts:1424-1435` |
| Re-validate arguments that a pre-tool hook rewrote (line 492) | **Forbids** rewriting. Pre-hook can only allow, deny, cancel, or ask, because "arguments are already logged and presented". Arguments are deep-frozen. | `tools/src/index.ts:598-611` |
| Bounded replay ring, `(epoch, seq)` cursor, bounded subscriber queue, detach on overflow (11.2) | None of these. `follow()` subscribes into an unbounded queue, sends a full snapshot at a per-session cursor, and throws on a gap; the client calls `resync()`. | `api/session-controller/src/history.ts:114-275`; `client/sessions/session.ts:486-497` |
| No subscriber code on the driver goroutine | Session observers and assistant-stream listeners run inline on the driver. | `core/session/src/index.ts:681-770` |
| Publish a committed tool result only after append (11.1) | Report flags this one correctly as an intentional difference. | `tools/src/index.ts:1669-1683` |
| Auth binding in the driver after request configuration | Adapter owns credentials; driver has no auth concept. | `llm/llm/src/index.ts:929-975` |
| Checkpoint before model dispatch and before tool body (line 558) | This **is** DeepSeek's `session-checkpoint-policy`, but the report does not credit it. | `session-checkpoint-policy/src/index.ts:63-83` |
| At-most-once `next` for all points | Only model dispatch is one-shot; Cordis `next` has no guard. | see section 2 |

Several of these Ask choices are reasonable (single owner, commit-then-publish, bounded subscriber queues). The problem is the label, not the choice: a reader cannot tell what was learned from a working system and what was designed on paper.

## 4. Ask reality: claim verdicts

| Report claim | Actual state | Verdict |
|---|---|---|
| Six ordered stages | steer, prepare, reason, act, observe, decide (`internal/agent/loop_stage.go:56-72`) | Correct. |
| `Hooks` typed fields plus `Compose` merge rules | 9 fields, per-field merge rules (`internal/pipeline/hooks.go:135-168`, `hooks_compose.go:11-282`) | Correct. |
| Listener failure can stop execution | Listener error or panic ends the run (`internal/agent/emit.go:15-64`) | Correct. |
| No model-attempt retry | No retry in agent or providers; fantasy SDK retries are 0 | Correct. |
| Batch serial if any tool is sequential, else one goroutine per call | Correct, but there is a third executor: a `StopLength` message gets error results for every call (`loop_tools.go:94-99`) | Simplified; the third executor is not restated in the redesign. |
| All results must terminate; `End > Continue > Proceed` | `loop_tools.go:432-442`; `hooks.go:21-33`; `loop_stage.go:186-199` | Correct. |
| Pre-tool hook replaces arguments without re-validation | `hooks.go:87-89`; `loop_tools.go:309-311` | Correct. |
| Per-Step tool snapshot | Snapshot once per turn (`loop_stage.go:96`) | Correct. |
| **Auth: "resolve after final request configuration" is a proposed change** (section 2 table) | Already current: auth resolves in the reason stage after `PrepareRequest` (`loop_stage.go:108-130`, `loop_stream.go:14-35`) | **Wrong framing**: current behavior shown as a change. |
| **"Existing external-hook failure policy" and "existing process-stop mechanism"** (lines 147, 164-165, 212) | `internal/hooks` has no code; the fail-closed seams exist only as decision D4 (`roadmap.md:40, 72`) | **Wrong**: design, not code. Line 165 ("no implicit hook deadlines") also conflicts with D4, which plans deadlines for out-of-process hooks. |
| **Queues are "dequeue-on-read hooks" that the Agent should own** (section 6) | No queue exists. Only two poll hooks, with no non-test implementation (`agent.go:15-18`) | **Overstated**: there is nothing to replace. Phase H9 already plans queues and retry with Pi semantics; the report never mentions H9. |
| bus, scheduler scaffolds; `(epoch, seq)` planned | bus/scheduler are doc-only; `sessions` has a working `MemoryLog`; cursor planned in H12; today `Seq` is per Agent (`emit.go:37`) | Mostly correct; moving sequence numbering to the bus publisher is not mentioned. |
| Public "Turn" means one round today | `pkg/protocol/events.go:54-62` | Correct. The report's "Step" is the code's "turn"; the report's "Turn" is new. This is a breaking wire change. |

## 5. Fit: is the "learn from DeepSeek" approach right?

**What is right.** Learning the mechanism from a working, tested system is the right method. The parts the report takes from DeepSeek are well chosen and correctly described: around middleware where an operation must be wrapped, retry inside the step without repeating admission or tools, the ordered coordinator with a bounded pool and exclusive barrier, drain on cancel, and not-started versus unknown repair. The deliberate exclusions (inbox clear on cancel, any-result stop rule, compaction, HMR, retry after visible output) each give a reason.

**What is wrong or unproven.**

1. **Project direction conflict without a recorded decision.** The project goal is to rebuild Pi in Go. The roadmap records these user decisions:
   - D19 (`roadmap.md:55`): **"Decided (user, 2026-10-01): B, as Pi."** The rejected option A was "every unstarted call gets Operation aborted", which is close to DeepSeek's model. The report replaces D19 with DeepSeek's model because "the user subsequently selected DeepSeek as the model" (lines 511, 654). No trace of that later selection exists in `plans/`, `docs/adr/`, `docs/CONTEXT.md`, `tasks/`, or memory.
   - D4 (`roadmap.md:40`): **"Decided (user, 2026-10-01)"**: out-of-process hooks get a deadline. Report line 165 ("do not add implicit hook deadlines") reads as a contradiction; it must say that D4's explicit deadline stays.
   - D20 (`roadmap.md:56`): "Decided (user)". The report keeps it (line 627). No conflict.
   - D11 (retry jitter, "copy Pi") is decided but not user-marked. H9 plans queues and retry with Pi rules and `auto_retry_*` events. The report's retry and queue sections overlap H9 without citing it.
   Under the repository's rule for user decisions, the D19 reversal must be confirmed by the user, not assumed.
2. **Unlabelled divergences** (section 3) make it hard to judge risk. Two of them go in the opposite direction from DeepSeek on purpose (argument rewrite: DeepSeek forbids, Ask re-validates; tool snapshot: DeepSeek is live, Ask is frozen). Both Ask choices are defensible, but the report must say why.
3. **Ordered decision handlers with precedence have no precedent.** DeepSeek avoids a precedence rule by making "continue" a data action (`steer()`). Ask's current `End > Continue > Proceed` is from Pi. Keeping it is fine; presenting it as part of the DeepSeek-informed design is not.
4. **Input commit timing differs.** DeepSeek saves user input only after request preparation (`docs/architecture.md:113`). The report's flowchart (lines 357-359) commits input before preparation and auth. A preparation or auth failure then leaves a saved user message with no model call. Section 12 does not cover this.
5. **"Model-visible means logged".** DeepSeek can rebuild every model request from the log (`docs/architecture.md:127`). The report says a transient request projection is not saved (line 291). This is a real choice with debugging and replay cost; it needs a decision.

## 6. Missing edge cases and features

Built from DeepSeek tests first (about 550 test titles), then compared with the report.

**Must-have for this design**

| Gap | DeepSeek evidence | Note for Ask |
|---|---|---|
| Max-tokens finish: a terminal reason that stays set for the turn | `core/agent-loop/tests/loop.spec.ts:1291, 1311, 1364` | No such reason in sections 3.1 or 12. |
| Tool calls from a max-tokens-truncated message are never run | `loop.spec.ts:1382`; `llm/llm/tests/assembler.spec.ts:242` | Ask already has `truncatedExecutor` (`loop_tools.go:97`); the redesign does not restate it and could drop it. |
| Cancel during a stream saves the partial answer as an interrupted message and drops a half-streamed tool call | `cancel.spec.ts:664`; `agent.ts:447-466` | Report covers failed attempts, not cancel. Tool pairing in the D19 replacement depends on this. |
| Failure rule for `CompleteStep` / `StopTurn` handlers | `contract-regressions.spec.ts:346` | Report promises a rule per point (line 250) but gives none for these two. |
| Steering that arrives after abort | `agent.ts:155-159` (DeepSeek turns it into follow-up) | Report says follow-up must not silently become steering, but is silent on the reverse. In current Ask, the decide stage can still poll steering after an abort during a tool batch (`loop_stage.go:191-195`); a test should confirm or rule out this path. |

**Should consider**

- Empty assistant answers kept out of history; reasoning-only output during abort (`docs/agent-lifecycle.md:83`; `cancel.spec.ts:640`).
- `inject()` context class and tool `additionalContexts` added after the batch (`loop.spec.ts:1007`; `interception.spec.ts:625`). Ask has no equivalent input class.
- Retry details: empty-response retry, `Retry-After`, retry budget per provider (`llm-retry/tests/retry.spec.ts:252, 392, 488`). These overlap H9.
- A slow post-hook delays later tool starts in DeepSeek. The report puts post-control on the driver; current Ask runs `AfterToolCall` on tool goroutines, so this is a behavior change to state.
- Command idempotency: a retried command with the same id is deduplicated (`commands-upload-file.host.spec.ts:345`).
- Cancel called from inside a handler; rollback when Agent creation fails (`cancel.spec.ts:793`; `core/agent/tests/agent.spec.ts:118`).
- Parent scopes receive child-agent events (`core/scope/src/index.ts:130-134`); this qualifies the report's sibling-isolation rule.

**Not relevant now**: subagent cancel propagation and depth limits (not in the Ask roadmap).

**Where the report is deeper than DeepSeek**: one-shot `next`, a bound on empty continuation (DeepSeek's own hooks-codex Stop hook has no loop guard), separate Turn/Step/Attempt identities, auth bound per Attempt, publish after commit. These are improvements, provided they are labelled as Ask design.

## 7. Recommended corrections to the report

1. Add a provenance column ("DeepSeek", "Pi / current Ask", "Ask new") to the tables in sections 7.3, 9, 11, and 13.
2. Fix the four current-state errors in section 4 (auth timing, external hooks not built, queues do not exist, H9 overlap).
3. Add the five must-have edge cases in section 6 to sections 8, 12, and 15.
4. State the input-commit timing and the "model-visible means logged" choice explicitly.
5. D19 direction is confirmed (question 1 below). Do not change H9 queue and retry semantics until question 3 is answered.

## Unresolved questions

1. Resolved (user, 2026-10-06): option B. D19 is revised to the DeepSeek tool-outcome model and recorded in the roadmap (Revision 8 and the D19 row). The design report now cites that record at lines 511 and 654.
2. Resolved (user, 2026-10-06): option A. Recorded as D23 in the roadmap; the design report sections 7.3 and 10 now log the hook changes for each Attempt.
3. Resolved (user, 2026-10-06): option A. One redesign phase before H8 (D24), with in-depth tests and a DeepSeek behavior conformance check.
