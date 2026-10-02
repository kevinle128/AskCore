---
title: "H2. The agent loop, plus print and JSON mode (in memory)"
status: ready
priority: P1
created: 2026-10-02
parent: plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md (section 4, H2)
analysis: plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md
---

# H2 agent loop, print mode and JSON mode plan

H2 gives Ask its agent loop. It calls the model, runs the tool calls, feeds the results back, and stops when the model stops.
The consumer is a developer who runs `ask -p "hello"` or `ask --mode json` and gets the right output and exit code, all in one process.
The maintainer inherits a loop that is a 1:1 port of Pi's `runLoop`, with typed hook fields that H8, H9 and H11 fill without a rewrite.
The rule the program enforces is Pi parity on event order, plus the four recorded departures (D18, D21, SIGINT, unknown flags).
H2 runs on the faux provider only. The fantasy adapter is H3 (D22, option A).
PRs in order are H2-A (tools), H2-B (loop), H2-C (agent wrapper), H2-D (print mode), H2-E (JSON mode and output guard).

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`, because the five PRs are sequenced and each one builds on the one below it. No owner merges. The root appends each verified PR to one linear stack on `master`, and the operator lands it bottom-up. H2-D and H2-E change the CLI interaction, so they stop at merge-ready for the operator's review.

**Local mode (operator, 2026-10-02).** Nothing is pushed and no forge PR is opened. The H1 base is commit `b0b1f5e` on the local branch `master-2`. Each PR id is a local branch (`h2-a` to `h2-e`) in its own worktree, stacked on its parent. The forge, Bugbot and CI boxes are skipped with the reason "local mode". Every verify box still applies, run locally.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] Confirm the base. H1 (`pkg/protocol`, `internal/providers`) and the Go 1.27.0 bump are committed and merged on `origin/master`. Today both sit uncommitted in the `master-2` worktree. Evidence is `git show origin/master:go.mod` printing `go 1.27.0` and `git ls-tree origin/master internal/providers/faux`.
- [ ] State the protocol and this plan to the operator, then stop. Start execution only on the operator's explicit go.
- [ ] On the operator's go, arm a `/goal` with this exact text. "Run plans/261002-1419-h2-agent-loop-print-json/plan.md under autopilot-stack. Build H2-A, H2-B, H2-C, H2-D, H2-E in that order as one linear stack on master. A PR is verified only when its unit, live and perf boxes are checked. No owner merges. The operator reviews H2-D and H2-E with screenshots and video and lands the stack. Done when all five PRs carry a clean root verdict at their head SHA and sit in the stack."
- [ ] Read these at program start and at every tick. pstack is not in this repo, so read them from the plugin cache instead of `git show origin/main:` paths.
  - [ ] `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `pstack/skills/swarm/SKILL.md`
  - [ ] `cursor-team-kit/skills/control-cli/SKILL.md`
  - [ ] `pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `pstack/skills/show-me-your-work/SKILL.md` and `pstack/skills/no-comments/SKILL.md`
- [ ] Arm the 30-minute audit tick as a real terminal `/loop` in this local session. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then post a short status message to the operator in chat only when the audit found a tracked change that no earlier status message reported, such as a PR opened, a code-ready head, a round launched or closed, a verdict, a merge, a stuck agent and the action taken, a blocker added or cleared, or a decision only the operator can make. Name every such change and nothing else. Do not repeat a table, the merged list, or an unchanged blocker. If the audit found none, end the turn with no reply text. Either way, log this tick's row in your decision trail. The row names the items reported, or none."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle of `autopilot-stack.md` step 1. Code owners run on the `feature, refactoring` model. H2-B runs on the `hardest tasks` model, because its ordering and abort rules are the subtle part.
- [ ] Follow this dependency graph. Each child is based on its parent branch.
  - [ ] H2-A is first and branches from `master`.
  - [ ] H2-B after H2-A.
  - [ ] H2-C after H2-B.
  - [ ] H2-D after H2-C.
  - [ ] H2-E after H2-D.
- [ ] Hold the file boundaries.
  - [ ] H2-A touches only `internal/tools/**`, `go.mod`, `go.sum`.
  - [ ] H2-B touches only `internal/pipeline/**`, `internal/agent/loop_*.go`, `internal/agent/doc.go`, `internal/agent/README.md`, and the H-LOOP-08 row of `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md`.
  - [ ] H2-C touches only `internal/agent/{agent,types,emit,context_source,systemprompt}*.go`, `internal/sessions/memory*.go`, `internal/sessions/README.md`, `pkg/protocol/events*.go`, `pkg/protocol/codec*.go`, `pkg/protocol/README.md`.
  - [ ] H2-D touches only `cmd/tui/**` except `cmd/tui/headless_json*.go` and `cmd/tui/output*.go`.
  - [ ] H2-E touches only `cmd/tui/headless_json*.go`, `cmd/tui/output*.go`, `cmd/tui/headless_test.go`, `AGENTS.md`, `CLAUDE.md`.
- [ ] Hold the review gate. H2-D and H2-E change an interaction. They wait for the operator's review in chat with screenshots and a video before they enter the stack.

### PR mechanics, for every PR

- [ ] Resolve the forge once. Default to `gh`, because the remote is `github.com/kevinle128/AskCore`. If `command -v origin` succeeds and Origin can resolve the repository, use `origin pr` for every PR operation. Record any fallback to `gh`. Never require `gt`.
- [ ] Open the PR ready, never draft, with `gh pr create --base <parent-branch>`. H2-A targets `master`. Each child targets its parent branch.
- [ ] Run `go build ./...`, `go vet ./...` and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` once before the PR-facing push. Push with hooks on.
- [ ] Run `/deslop` before each commit and `/no-comments` before review.
- [ ] Triage every Bugbot and security-reviewer comment per `../references/bugbot-triage.md`.
- [ ] Rebase onto current trunk before the code-ready report and babysit. Keep that merge base in fix rounds. Rebase again only at merge prep, on a `git merge-tree` conflict with trunk, or on a CI failure that comes from a change on trunk.

### Verdict and merge, for every PR

- [ ] At the code-ready head SHA and at each later push that changes the patch, run the swarm per `pstack/skills/swarm/SKILL.md`. One gates lane runs `go test -race -count=3 ./...` plus the lint. The ten live lanes come from the PR's **Verify, live** block. The perf lane comes from its **Verify, perf** block. Two audit lanes read the diff and the receipts and distrust the PR body. One audits Pi parity against the `file:line` evidence in the analysis report. One audits goroutine ownership and cancellation. The root audits the receipts in the stack-ready report before the verdict.
- [ ] Clean only when every lane is `PASS`. Findings go back to the owner, including a defect that a lane filed as a note. A new head gets a fresh swarm and a fresh verdict, except for results that stay valid under the patch-id rule in `playbooks/shipping.md`.
- [ ] On a clean verdict the root appends the PR to the linear stack per `autopilot-stack.md` step 5 and step 6. A rebase that changes the patch-id voids the verdict. The operator lands the stack bottom-up.

### Boot recipe, for every live lane

Each live lane runs in its own local git worktree at the PR head. Drive through `control-cli` from `cursor-team-kit`, with tmux for interactive and signal lanes.

- [ ] `git fetch origin <head-branch> && git worktree add /tmp/h2-<pr-id>-<n> <head SHA>`.
- [ ] For H2-A to H2-C there is no CLI yet. The lane writes a throwaway probe `main` under `/tmp/h2probe-<pr-id>-<n>/` with a `go.work` that points at the worktree, runs it with `go run`, and never commits it. For H2-D and H2-E the lane builds `go build -o /tmp/h2-<pr-id>-<n>/ask ./cmd/tui` and drives that binary.
- [ ] Deliver input only through the control skill's commands (`tmux send-keys`, pipes, `kill -s`). Read-only diagnostics are `tmux capture-pane -p`, `echo $?`, `lsof -p <pid> -i`, and `GODEBUG=gctrace=0` stderr.
- [ ] Save every capture to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.txt` and return the paths with the report. A CLI surface has no pixels, so a captured pane or transcript is the screenshot.

## Build the tool contract, registry and argument validation (H2-A)

**Depends on.** None. H2-A is the stack root on `master`, after the H1 base is merged.

**Files.**

- [ ] Create `internal/tools/types.go` with `Tool`, the optional `Sequential` and `ArgumentPreparer` interfaces, and `Context`.
- [ ] Create `internal/tools/registry.go` and `internal/tools/source.go` (`SourceInfo`).
- [ ] Create `internal/tools/validate.go` and `internal/tools/coerce.go`.
- [ ] Create `internal/tools/echo.go`.
- [ ] Create `internal/tools/registry_test.go`, `internal/tools/coerce_test.go`, `internal/tools/validate_test.go`, `internal/tools/echo_test.go`.
- [ ] Edit `internal/tools/README.md` (file names, the registry rules).
- [ ] Edit `go.mod` and `go.sum` to make `github.com/santhosh-tekuri/jsonschema/v6` a direct dependency.

**Build.**

- [ ] `tools.Tool` exposes `Decl() protocol.ToolDecl` and `Execute(ctx, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error)`. `tools.Context` holds the call id, the cwd and the `Update` callback. The `ctx` argument is the abort signal (timeline lesson 5). See Appendix E.
- [ ] `Registry.Register(tool, SourceInfo)` rejects a tool without a parameters schema, a schema that does not compile, and a duplicate name. `Lookup` is exact and case-sensitive. `Decls` returns declarations in registration order.
- [ ] `Registry.Prepare(name, raw)` returns the arguments that `Execute` will see. A missing or `null` input becomes `{}`. Coercion follows D21 with one table for all schemas. An `anyOf` or `oneOf` arm that already validates is kept. `"5.7"` is not coerced to an integer.
- [ ] The validation error text follows Pi's format (`AI:utils/validation.ts`) and echoes the raw arguments capped at 2 KiB with a `... (truncated)` marker.
- [ ] `echo` declares `{text: string}` as required and returns one text block with the same text.

**You see.**

- [ ] `go test ./internal/tools/...` prints `ok` and the coercion table test lists one subtest per row of Appendix E's coercion table.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `coerce_test.go` covers `"5"` to 5 for an integer, `"5.7"` rejected for an integer, `"true"` and `1` to true, null kept in `anyOf`, an optional null removed, missing input to `{}`, and a valid arm kept. Run `go test -race ./internal/tools/ -run Coerce`.
- [ ] `validate_test.go` asserts the exact error text for a missing required field and for a wrong type, and that a 10 KiB argument is echoed at 2 KiB. Run `go test ./internal/tools/ -run Validate`.
- [ ] `registry_test.go` covers a schema-less tool, a duplicate name, a bad schema, case-sensitive lookup and `SourceInfo` round trip. Run `go test ./internal/tools/ -run Registry`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run a probe that registers `echo` and calls `Prepare("echo", {"text":"hi"})` at trunk and head. Trunk lacks `internal/tools` code, so record the compile failure at trunk and gate the head result. Save `regression.txt`. Pass when trunk fails to build the probe and head prints `{"text":"hi"}`.
- [ ] Lane 2. Probe registers a tool whose declaration has no parameters schema. Save `schemaless.txt`. Pass when `Register` returns an error naming the tool.
- [ ] Lane 3. Probe registers `echo` twice. Save `duplicate.txt`. Pass when the second `Register` fails and `Decls` still has one entry.
- [ ] Lane 4. Probe sends `{"text":5}` to `echo`. Save `coerce-string.txt`. Pass when `Prepare` returns `{"text":"5"}`.
- [ ] Lane 5. Probe with an integer schema sends `"5.7"`. Save `no-truncation.txt`. Pass when `Prepare` returns a validation error, not 5.
- [ ] Lane 6. Probe sends a 1 MiB invalid argument. Save `error-cap.txt`. Pass when the error text is under 3 KiB and ends with the truncation marker.
- [ ] Lane 7. Probe sends `null` and no bytes to a tool with no required fields. Save `missing-input.txt`. Pass when both give `{}`.
- [ ] Lane 8. Probe with an `anyOf [integer, null]` field sends `null`. Save `anyof-null.txt`. Pass when `null` is kept.
- [ ] Lane 9. Probe calls `echo.Execute` with a cancelled ctx. Save `echo-cancel.txt`. Pass when it returns promptly with a ctx error or a result, and never blocks.
- [ ] Lane 10. Probe calls `Prepare` from 64 goroutines on one registry under `-race`. Save `race.txt`. Pass when the race detector reports nothing.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Time and allocations per `Registry.Prepare` call for `echo` with a 200-byte argument. Trunk lacks the feature, so the diff-added work is the whole call.
- [ ] Probe. `go test -bench Prepare -benchmem -count=10 ./internal/tools/` at head, and the same probe at trunk recorded as absent.
- [ ] Baseline. Record trunk as "no tools package" first.
- [ ] Rule. Fail when the head median is above 50 microseconds or above 200 allocations per call. A schema compiles once at `Register`, never per call.

**Review gate.** None. H2-A is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends H2-A to the base of the stack. The operator lands it bottom-up.

## Port the two-level loop and the tool batch (H2-B)

**Depends on.** H2-A.

**Files.**

- [ ] Create `internal/pipeline/hooks.go` with the typed `Hooks` struct and its argument and result types. Delete the `Step`, `TurnState` and `PipelineDeps` text from `internal/pipeline/README.md` and describe the hook fields instead.
- [ ] Create `internal/agent/loop_run.go` (`Run`, `Continue`, the outer and inner loops).
- [ ] Create `internal/agent/loop_stream.go` (one assistant response).
- [ ] Create `internal/agent/loop_tools.go` (batch, pipeline, length guard).
- [ ] Create `internal/agent/loop_run_test.go`, `internal/agent/loop_tools_test.go`, `internal/agent/loop_stream_test.go`, `internal/agent/loop_helpers_test.go` (with `goleak.VerifyTestMain`).
- [ ] Edit `internal/agent/README.md` (file list matches the files above).
- [ ] Edit `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md` row H-LOOP-08 to match D19.

**Build.**

- [ ] `pipeline.Hooks` has one func field per hook point. `TransformContext`, `ConvertToLLM`, `GetAPIKey`, `PrepareRequest`, `FinishTurn`, `BeforeToolCall`, `AfterToolCall`, `GetSteeringMessages`, `GetFollowUpMessages`. A nil field means Pi's default. `FinishTurn` returns the typed `pipeline.TurnDecision` (`Proceed`, `Continue`, `End`), not a string.
- [ ] `agent.Run` and `agent.Continue` port `runLoop` (`A:agent-loop.ts:163-321`) 1:1, with `lastCompletedTurn` and `explicitContinuation`. `Continue` with an empty context or an assistant tail returns an error before any event.
- [ ] Steering is polled at run start, after each normal `turn_end`, and after `prepareNextTurn` only when the earlier poll was empty. It is never polled after an error or aborted message, or when `FinishTurn` returns `End`. `Continue` gives exactly one more request.
- [ ] `loop_stream.go` follows `A:agent-loop.ts:381-469`. `TransformContext`, then `ConvertToLLM`, then `providers.NormalizeRequest`, then `GetAPIKey` with fallback to the config key, then `StreamFn`. The partial message is replaced, never appended. After the event channel closes, the final message comes from `Stream.Result(ctx)`, which never hangs.
- [ ] `PrepareRequest` runs before every request. A returned request replaces the context for this request and all later ones.
- [ ] `loop_tools.go` follows `A:agent-loop.ts:508-935`. One `Sequential` tool makes the batch sequential. Parallel preflight runs in source order. `tool_execution_end` fires in completion order. Result messages are emitted after the batch in source order, and enter the context only after the whole batch.
- [ ] The pipeline per call is lookup (`Tool X not found`), `PrepareArguments`, `Registry.Prepare`, `BeforeToolCall` (block text is the reason or `Tool execution was blocked`), `Execute`, `AfterToolCall` (each non-nil field overrides). Events and hooks see raw arguments. `Execute` sees the prepared ones. `AfterToolCall` runs only for calls that executed. An `Update` after settle is dropped. A duplicate call id gets an error result (G9 item 2).
- [ ] Per D20, each tool goroutine recovers a panic from `PrepareArguments`, validation, `BeforeToolCall`, `Execute` and `AfterToolCall` into an error result. An error from `TransformContext`, `ConvertToLLM`, `GetAPIKey`, `PrepareRequest`, `FinishTurn` or the stream function is returned from `Run` unchanged.
- [ ] The length guard (`A:agent-loop.ts:478-503`) gives every tool call of a `length` message start and end events and the byte-exact error text. No hook runs and the loop continues.
- [ ] Abort follows D19. Preflight stops at the break point. Prepared calls get `Operation aborted`. Later calls get no events and no result. The loop makes one more stream call with the cancelled ctx, which returns an aborted message.
- [ ] Each emitted event goes through one `emit func(protocol.Event) error` passed in by the caller. An emit error ends `Run` with that error.

**You see.**

- [ ] `go test -race ./internal/agent/ -run TestTwoTurnEventOrder -v` prints the full event list of Appendix E and passes.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `loop_run_test.go` asserts the exact event type sequence of a two-turn run with tools against a literal list. Run `go test -race ./internal/agent/ -run TwoTurn`.
- [ ] `loop_run_test.go` covers `FinishTurn` `Continue` with no tools (faux `Calls()` grows by exactly one) and `End` (poll hooks called zero times after it). Run `go test ./internal/agent/ -run FinishTurn`.
- [ ] `loop_run_test.go` covers an error and an aborted assistant message. `FinishTurn` is called once, then `turn_end` with zero results, then `agent_end`, and no tool executes. Run `go test ./internal/agent/ -run ErrorTail`.
- [ ] `loop_run_test.go` covers `Continue` with an empty context and with an assistant tail. Run `go test ./internal/agent/ -run Continue`.
- [ ] `loop_stream_test.go` uses `faux.Step.Truncate` and asserts an error message with `ErrStreamIncomplete`, inside a 2 second deadline. Run `go test ./internal/agent/ -run Truncated`.
- [ ] `loop_tools_test.go` covers completion order versus source order, the sequential switch, unknown tool, validation text, block with and without reason, hook-mutated arguments, update after settle, `AfterToolCall` skipped for blocked and invalid calls, a duplicate id, and panics in a tool, `BeforeToolCall` and `AfterToolCall`. Run `go test -race ./internal/agent/ -run Tool`.
- [ ] `loop_tools_test.go` covers the length guard with two calls and the exact text, and abort in the middle of a parallel batch per D19. Run `go test ./internal/agent/ -run 'Length|Abort'`.
- [ ] `loop_helpers_test.go` runs goleak after a normal end, after abort and after a returned hook error. Run `go test -race -count=5 ./internal/agent/`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run a probe that calls `agent.Run` with faux replying `ToolCall(echo)` then `Say("done")`, at trunk and head. Trunk lacks the loop, so record the build failure and gate the head result. Save `regression.txt`. Pass when trunk fails to build and head prints two `turn_end` events and a final text `done`.
- [ ] Lane 2. Probe with two parallel tools where the second sleeps less. Save `order.txt`. Pass when `tool_execution_end` order is second, first and result message order is first, second.
- [ ] Lane 3. Probe with one tool marked sequential among three. Save `sequential.txt`. Pass when no two `Execute` spans overlap in the printed timestamps.
- [ ] Lane 4. Probe with faux `Stop(length)` and two tool calls. Save `length.txt`. Pass when both results carry the exact Pi text and the next request happens.
- [ ] Lane 5. Probe cancels ctx while the second of three parallel tools runs. Save `abort-batch.txt`. Pass when prepared calls show `Operation aborted`, later calls show no events, and faux `Calls()` grew by one more aborted request.
- [ ] Lane 6. Probe cancels ctx during a paced faux stream. Save `abort-stream.txt`. Pass when the last assistant message has `stopReason` aborted and the probe exits inside 1 second.
- [ ] Lane 7. Probe whose tool panics. Save `tool-panic.txt`. Pass when the process survives and the result has `isError` true with the panic text.
- [ ] Lane 8. Probe whose `PrepareRequest` returns an error. Save `hook-error.txt`. Pass when `Run` returns that error and no `agent_end` is emitted by the loop (D20).
- [ ] Lane 9. Probe with `FinishTurn` returning `Continue` once. Save `continue.txt`. Pass when faux served exactly one extra request with no new user message.
- [ ] Lane 10. Probe runs 200 sequential runs and prints `runtime.NumGoroutine()` before and after. Save `goroutines.txt`. Pass when the count after equals the count before.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Loop overhead per turn, measured as wall time and allocations of a one-tool turn on faux with `WithChunk` at its maximum and no pacing. Trunk lacks the loop, so this is the diff-added work.
- [ ] Probe. `go test -bench LoopTurn -benchmem -count=10 ./internal/agent/` at head, with trunk recorded as absent.
- [ ] Baseline. Record trunk as "no loop" first.
- [ ] Rule. Fail when the head median is above 500 microseconds per turn or above 2,000 allocations per turn.

**Review gate.** None. H2-B is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends H2-B on top of H2-A in the stack. The operator lands it bottom-up.

## Wrap the loop in the Agent API with state and listeners (H2-C)

**Depends on.** H2-B.

**Files.**

- [ ] Create `internal/agent/types.go` (`Config`, `State`, `Status`, `ContextSource`, errors).
- [ ] Create `internal/agent/agent.go` (`Agent`, `New`, `Prompt`, `Continue`, `Abort`, `WaitForIdle`, `Reset`, `Subscribe`, `State`).
- [ ] Create `internal/agent/emit.go` (envelope fill and ordered listener dispatch).
- [ ] Create `internal/agent/systemprompt.go` (the initial system message).
- [ ] Create `internal/sessions/memory.go` and `internal/sessions/memory_test.go` (`MemoryLog`).
- [ ] Create `internal/agent/agent_test.go`.
- [ ] Edit `pkg/protocol/events.go`, `pkg/protocol/codec.go`, `pkg/protocol/events_test.go` to add `agent_settled`.
- [ ] Edit `pkg/protocol/README.md`, `internal/sessions/README.md`.

**Build.**

- [ ] `protocol.AgentSettled` is an envelope-only event with type `agent_settled`, encoded and decoded like `AgentStart` (D18).
- [ ] `agent.Status` is a two-state machine, `Idle` and `Running`, behind one mutex. `Prompt` and `Continue` while `Running` return `ErrBusy`. `Reset` while `Running` returns `ErrBusy`. There is no second boolean.
- [ ] `emit.go` fills `Seq` (monotonic per agent), `RunID` (new per run), `TS` (injected clock) and `SessionID` (from `Config`). Listeners run in subscribe order, synchronously, on the loop goroutine. A slow listener stalls the loop (backpressure, as in Pi).
- [ ] The wrapper owns the run-failure path of D20 (`A:agent.ts:523-548`). When `Run` returns an error or a listener returns one, it emits an error assistant message with `message_start`, `message_end`, `turn_end` and `agent_end`, then `agent_settled`.
- [ ] On a normal end the wrapper emits `agent_settled` right after `agent_end`.
- [ ] `agent.ContextSource` has `Append(protocol.Message) error` and `Messages() []protocol.Message`. `sessions.MemoryLog` implements it. The wrapper appends each message on `message_end` only, and installs a `PrepareRequest` that projects `ContextSource.Messages()` into each request. H8 swaps in the session projection at this one seam.
- [ ] `systemprompt.go` builds the leading system message from the system prompt and all registry declarations with timestamp 0, as `createInitialSystemMessage` does. The tool set is fixed for the run.
- [ ] `WaitForIdle(ctx)` returns when the status is `Idle` or ctx ends. `Abort` cancels the run ctx and is a no-op when idle.

**You see.**

- [ ] `go test -race ./internal/agent/ -run TestAgentPromptSettles -v` shows `agent_end` followed by `agent_settled`, with `seq` values 1..N and no gap.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `agent_test.go` asserts `ErrBusy` for `Prompt` and `Reset` during a paced run. Run `go test -race ./internal/agent/ -run Busy`.
- [ ] `agent_test.go` asserts listener order and that a listener error yields the synthesized error message, `agent_end` and `agent_settled`. Run `go test ./internal/agent/ -run Listener`.
- [ ] `agent_test.go` asserts that a `PrepareRequest` error and a stream setup error each yield the wrapper's error message path (D20). Run `go test ./internal/agent/ -run RunFailure`.
- [ ] `agent_test.go` asserts that faux `Requests()` of the second prompt contains the first prompt's messages from `MemoryLog`, and that a partial message is never appended. Run `go test ./internal/agent/ -run ContextSource`.
- [ ] `events_test.go` round-trips `agent_settled` through JSONL. Run `go test ./pkg/protocol/ -run Settled`.
- [ ] `memory_test.go` covers append order and copy-on-read. Run `go test -race ./internal/sessions/`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run a probe that creates an `Agent`, prompts "hi" on faux and prints every event, at trunk and head. Trunk lacks `Agent`, so record the build failure and gate the head result. Save `regression.txt`. Pass when trunk fails to build and head ends with `agent_end` then `agent_settled`.
- [ ] Lane 2. Probe prompts twice while the first run is paced. Save `busy.txt`. Pass when the second call returns `ErrBusy` and the first run settles.
- [ ] Lane 3. Probe subscribes three listeners that print their index. Save `listener-order.txt`. Pass when every event prints 1, 2, 3 in order.
- [ ] Lane 4. Probe whose second listener returns an error on `turn_start`. Save `listener-error.txt`. Pass when the run ends with an error assistant message, `agent_end` and `agent_settled`.
- [ ] Lane 5. Probe prints `seq`, `runId` across two prompts. Save `envelope.txt`. Pass when `seq` is gap-free across both and `runId` differs between them.
- [ ] Lane 6. Probe calls `Abort` mid-stream then `WaitForIdle`. Save `abort-idle.txt`. Pass when `WaitForIdle` returns inside 1 second and the status is `Idle`.
- [ ] Lane 7. Probe calls `Reset` while running, then after idle. Save `reset.txt`. Pass when the first gives `ErrBusy` and the second empties `State().Messages`.
- [ ] Lane 8. Probe prints the first transcript message faux received. Save `system-message.txt`. Pass when it is a system message with timestamp 0 and `echo` in `toolsAdded`.
- [ ] Lane 9. Probe with a listener that sleeps 200 ms per event. Save `backpressure.txt`. Pass when the run takes at least the summed sleep, which shows the loop waits for the listener.
- [ ] Lane 10. Probe encodes every event to JSONL and decodes it back. Save `jsonl.txt`. Pass when every line decodes, including `agent_settled`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wrapper overhead, the wall time of `Agent.Prompt` minus `agent.Run` on the same one-tool faux turn, with one no-op listener.
- [ ] Probe. `go test -bench 'AgentPrompt|LoopTurn' -benchmem -count=10 ./internal/agent/` at head, interleaved with the H2-B parent as trunk for `LoopTurn`.
- [ ] Baseline. Record the H2-B parent `LoopTurn` median first.
- [ ] Rule. Fail when `AgentPrompt` is more than 20 percent or 100 microseconds slower than `LoopTurn`, whichever is larger, or when `LoopTurn` regresses more than 10 percent against the parent.

**Review gate.** None. H2-C is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends H2-C on top of H2-B in the stack. The operator lands it bottom-up.

## Run one prompt headless with print mode (H2-D)

**Depends on.** H2-C.

**Files.**

- [ ] Create `cmd/tui/args.go` and `cmd/tui/args_test.go` (the hand-written parser).
- [ ] Create `cmd/tui/input.go` and `cmd/tui/input_test.go` (stdin, `@file`, messages).
- [ ] Create `cmd/tui/headless.go` (mode selection, agent build, signals, exit codes).
- [ ] Create `cmd/tui/headless_print.go`.
- [ ] Create `cmd/tui/headless_faux.go` (the H2 demo script).
- [ ] Create `cmd/tui/headless_test.go`.
- [ ] Edit `cmd/tui/main.go` so `main` calls `os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))` and keeps the bubbletea scaffold for interactive mode.
- [ ] Edit `go.mod` to make `golang.org/x/term` a direct dependency.

**Build.**

- [ ] `args.go` parses `-p`, `--mode`, `--provider`, `--model`, `--api-key`, `--thinking`, `--`, positional messages and `@file`. `-p` consumes the next token unless it starts with `@` or `-`. `--mode` with a missing or invalid value, `--provider`, `--model` or `--api-key` with no value, an unknown `--flag`, and `--api-key` without a model are errors with exit 1. `--thinking` with a bad value warns on stderr and keeps the default.
- [ ] `headless.go` selects the mode in Pi's order. `--mode json`, then print when `-p` is set or stdin or stdout is not a TTY (`term.IsTerminal`), then interactive. `--mode text` does not force print.
- [ ] `input.go` joins trimmed piped stdin, `@file` text wrapped in `<file name="...">` and `messages[0]` with no separator. The other messages run as further prompts in order. A missing or unreadable file is exit 1. An empty file is skipped. A leading `/` stays literal text. Stdin is read until EOF.
- [ ] `headless_faux.go` is the only provider in H2 (`--provider faux`, model `faux-1`, the defaults). Its `faux.Func` replies with the result text when the last message is a tool result, calls `echo` with the rest of the prompt when it starts with `echo `, fails with the rest of the prompt when it starts with `fail `, and otherwise says the prompt back. `ASK_FAUX_TPS` sets pacing for the signal lanes. Any other provider is exit 1 with `provider "<name>" is not available until H3`.
- [ ] `headless_print.go` writes the text blocks of the last assistant message, each followed by `\n`. An error or aborted message prints its error on stderr and gives exit 1. A returned Go error gives exit 1 and stops the remaining prompts. An assistant error does not stop them.
- [ ] Signals cancel the run ctx, wait for idle, then exit 130 for SIGINT (deviation), 143 for SIGTERM, 129 for SIGHUP.
- [ ] Logs go to stderr only. Print mode opens no listener and no socket.

**You see.**

- [ ] `go run ./cmd/tui -p "hello"` prints `hello` and `echo $?` prints `0`. `go run ./cmd/tui -p "echo hi"` prints `hi`. `go run ./cmd/tui -p "fail boom"` prints `boom` on stderr and exits 1.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `args_test.go` is a table of argv to parsed result or error, covering every rule in the Build block. Run `go test ./cmd/tui/ -run Args`.
- [ ] `input_test.go` asserts the exact joined string for stdin plus `@file` plus message, the `<file>` wrapper, and the empty-file skip. Run `go test ./cmd/tui/ -run Input`.
- [ ] `headless_test.go` drives `run(argv, stdin, stdout, stderr)` in process and asserts stdout, stderr and the exit code for `hello`, `echo hi`, `fail boom`, two messages where the first fails as an assistant error, and an unknown provider. Run `go test -race ./cmd/tui/ -run Print`.
- [ ] `headless_test.go` builds the binary, sends SIGINT and SIGTERM to a paced run, and asserts 130 and 143. Run `go test ./cmd/tui/ -run Signal`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run `./ask -p "hello"; echo $?` at trunk and head. Trunk starts the bubbletea menu or fails without a TTY, so record that and gate the head result. Save `regression.txt`. Pass when head prints `hello` then `0`.
- [ ] Lane 2. `./ask -p "echo hi"; echo $?`. Save `echo-tool.txt`. Pass when the output is `hi` then `0`.
- [ ] Lane 3. `./ask -p "fail boom"; echo $?`. Save `assistant-error.txt`. Pass when stdout is empty, stderr has `boom`, and the code is `1`.
- [ ] Lane 4. `printf 'a' | ./ask -p b; echo $?`. Save `stdin-join.txt`. Pass when the output is `ab`.
- [ ] Lane 5. In tmux, `ASK_FAUX_TPS=2 ./ask -p "<200 words>"`, then `tmux send-keys C-c`. Save `sigint.txt`. Pass when the process exits inside 1 second with `130`.
- [ ] Lane 6. `ASK_FAUX_TPS=2 ./ask -p "<200 words>" & sleep 1; kill -TERM $!; wait $!; echo $?`. Save `sigterm.txt`. Pass when the code is `143`.
- [ ] Lane 7. `./ask --mode x -p hi; ./ask --model; ./ask --nope -p hi` with `echo $?` after each. Save `bad-flags.txt`. Pass when each prints an error on stderr and exits `1`.
- [ ] Lane 8. `./ask -p @missing.txt; echo $?` and `./ask -p @empty.txt hi`. Save `file-args.txt`. Pass when the first exits `1` and the second prints `hi`.
- [ ] Lane 9. Run `ASK_FAUX_TPS=2 ./ask -p "<200 words>"` and `lsof -nP -p <pid> -i` during the stream. Save `no-listener.txt`. Pass when `lsof` lists no network socket.
- [ ] Lane 10. `./ask -p "/help"`. Save `slash-literal.txt`. Pass when the output is `/help`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./ask -p hello` from exec to exit, and the binary size of `ask`. The end state the user waits for is the printed reply and the exit.
- [ ] Probe. `hyperfine -w 3 -r 30 './ask -p hello'` at head. Trunk has no `-p`, so its side of the probe is `ls -l ask` only, run interleaved with the head build.
- [ ] Baseline. Record the trunk binary size first.
- [ ] Rule. Fail when the head median of `./ask -p hello` is above 100 ms, or the binary grows by more than 8 MiB over trunk.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1, 2, 3 and 5 screenshots (pane captures) into `plans/261002-1419-h2-agent-loop-print-json/media/H2-D-review-<slug>.txt`.
- [ ] Record a 30 to 60 second video with `vhs` of lanes 1, 2, 3 and 5 in sequence. Save it as `plans/261002-1419-h2-agent-loop-print-json/media/H2-D-review.mp4`.
- [ ] Post the screenshots and the video in chat for the operator. Stop at stack-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] After the operator's review, the root appends H2-D on top of H2-C in the stack. The operator lands it bottom-up.

## Stream JSON mode through one guarded stdout (H2-E)

**Depends on.** H2-D.

**Files.**

- [ ] Create `cmd/tui/output.go` and `cmd/tui/output_test.go` (the stdout guard).
- [ ] Create `cmd/tui/headless_json.go`.
- [ ] Edit `cmd/tui/headless.go` to route both modes through the guard.
- [ ] Edit `cmd/tui/headless_test.go` with the JSON and EPIPE cases.
- [ ] Edit `AGENTS.md` and `CLAUDE.md` (the `ask -p` and `--mode json` commands, Go 1.27.0, fantasy planned for H3).

**Build.**

- [ ] `output.go` keeps the real stdout for protocol output and points `os.Stdout` at stderr for the rest of the process in print and JSON mode, so a stray write never corrupts the stream. It calls `signal.Notify` for SIGPIPE (not `signal.Ignore`, which children inherit), so a closed pipe gives an EPIPE error. A write error on the protocol writer cancels the run and gives exit 1. The writer is flushed before exit.
- [ ] `headless_json.go` subscribes one listener that writes each event with `protocol.NewJSONLWriter` on the guarded stdout. Each record ends with LF. Each prompt ends with `agent_settled`. There is no session header until H8.
- [ ] JSON mode exits 0 when the assistant ends in error, and exits 1 only on a returned Go error or a stdout write error.
- [ ] A slow reader stalls the loop, because the listener blocks on the write.

**You see.**

- [ ] `go run ./cmd/tui --mode json "echo hi" | tail -1` prints a line whose `type` is `agent_settled`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `headless_test.go` decodes every stdout line of `--mode json "echo hi"` and asserts the literal event type list from Appendix E, ending with `agent_settled`. Run `go test -race ./cmd/tui/ -run JSON`.
- [ ] `headless_test.go` asserts LF framing with a 100 KiB reply, U+2028 kept inside a string, exit 0 on `fail boom`, and one `agent_settled` per prompt for two messages. Run `go test ./cmd/tui/ -run JSON`.
- [ ] `output_test.go` asserts that a `fmt.Println` from library code lands on stderr in JSON mode. Run `go test ./cmd/tui/ -run Guard`.
- [ ] `headless_test.go` runs the built binary into a pipe closed after one line and asserts exit 1 and no leaked goroutine via goleak in the in-process variant. Run `go test -race ./cmd/tui/ -run EPIPE`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run `./ask --mode json hi | jq -c .type` at trunk (the H2-D parent) and head. The parent rejects or ignores JSON mode, so record that and gate the head result. Save `regression.txt`. Pass when head prints `agent_start` first and `agent_settled` last.
- [ ] Lane 2. `./ask --mode json "echo hi" | jq -c '{type,seq}'`. Save `json-tools.txt`. Pass when the tool events appear and `seq` is gap-free.
- [ ] Lane 3. `./ask --mode json "fail boom"; echo $?`. Save `json-error.txt`. Pass when the stream ends with `agent_settled` and the code is `0`.
- [ ] Lane 4. `./ask --mode json "<120 KiB text>" | awk '{print length}' | sort -n | tail -1`. Save `long-line.txt`. Pass when the longest line exceeds 64 KiB and every line parses with `jq`.
- [ ] Lane 5. `./ask --mode json "$(printf 'a\u2028b')" | jq -r 'select(.type=="message_end").message.content[0].text' | od -c`. Save `u2028.txt`. Pass when the bytes of U+2028 survive in one record.
- [ ] Lane 6. `bash -c './ask --mode json "<long>" | head -n1; echo ${PIPESTATUS[0]}'`. Save `epipe.txt`. Pass when the code is `1`, not `141`.
- [ ] Lane 7. `ASK_FAUX_TPS=50 ./ask --mode json "<long>" | (sleep 3; cat) | wc -l` with timestamps. Save `backpressure.txt`. Pass when the run takes at least 3 seconds and no line is lost.
- [ ] Lane 8. `./ask --mode json a b | jq -c 'select(.type=="agent_settled")' | wc -l`. Save `two-prompts.txt`. Pass when the count is `2`.
- [ ] Lane 9. In tmux, `ASK_FAUX_TPS=2 ./ask --mode json "<long>" > out.jsonl`, then `C-c`. Save `json-sigint.txt`. Pass when the code is `130` and the last line of `out.jsonl` parses.
- [ ] Lane 10. `./ask --mode json hi 2>err.txt | jq -c .type > /dev/null; cat err.txt`. Save `stderr-only-logs.txt`. Pass when every stdout line is JSON and any log text is only in `err.txt`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Events per second written by `./ask --mode json` for a 1 MiB faux reply with no pacing, and the wall time to `agent_settled`.
- [ ] Probe. `hyperfine -w 3 -r 20 './ask --mode json "<1 MiB>" > /dev/null'` at head, interleaved with `./ask -p "<1 MiB>" > /dev/null` at the H2-D parent as the trunk side of the same reply.
- [ ] Baseline. Record the H2-D parent print-mode median for the 1 MiB reply first.
- [ ] Rule. Fail when JSON mode is more than 3 times slower than print mode for the same reply, or above 1 second absolute.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1, 3, 6 and 7 screenshots (pane captures) into `plans/261002-1419-h2-agent-loop-print-json/media/H2-E-review-<slug>.txt`.
- [ ] Record a 30 to 60 second video with `vhs` of lanes 1, 3, 6 and 7. Save it as `plans/261002-1419-h2-agent-loop-print-json/media/H2-E-review.mp4`.
- [ ] Post the screenshots and the video in chat for the operator. Stop at stack-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] After the operator's review, the root appends H2-E on top of H2-D. The operator lands the whole stack bottom-up.

## Close the program

- [ ] Every box above is checked with its evidence. Only partly done. The unit, live and perf boxes are verified (Appendix F). The review-gate media, interrogate passes and forge boxes are parked below.
- [x] The roadmap H2 exit holds on the stack tip. `ask -p "hello"` and `ask --mode json` run in process against faux plus `echo`, with exit codes 0, 1, 130 and 143 shown in the lane captures.
- [x] Add a review section (Appendix F) to this plan with the five PR links, their verdict SHAs and the departures added during the build.
- [ ] Reply to the operator with the report `autopilot-stack.md` names. Links to the stack root and tip, a one-line verdict per link, and anything parked with the reason.

## Appendix A. Prototype evidence

**Closed stdout in a Go binary (settled, 2026-10-01).** A scratch program wrote 100,000 lines into `| head -c1`. With no handler it died by SIGPIPE, exit 141. With `signal.Notify(ch, SIGPIPE)` or `signal.Ignore(SIGPIPE)` the write returned `broken pipe` and the program exited 1. H2-E uses `Notify`, because `Ignore` sets SIG_IGN, which a child process inherits across exec and would break shell pipelines in H5 tools. The scratch code was in `/tmp/epipe-proto` and is deleted. No branch or SHA.

**Go 1.27.0 and fantasy v0.45.2 (settled, 2026-10-01).** A copy of the worktree with `go 1.27.0` and `charm.land/fantasy v0.45.2` imported passed `go build ./...`, `go test -race ./...` and `golangci-lint` with 0 issues. The worktree itself now has `go 1.27.0`, and its build, tests and lint pass. Fantasy is not imported by H2.

**Unproven.** The perf budgets in each PR are estimates, not measurements. The first owner run of each perf probe may show a budget is wrong. The owner then records the measured value and raises the budget question to the root, never silently.

## Appendix B. Alternatives rejected

- **The fantasy adapter in H2.** It would give H2 a real model, but it makes the loop phase depend on the network and doubles its concepts. The operator chose H3 (D22, option A).
- **Fantasy's agent loop.** It replaces Pi's two-level loop, hook order, steering and the D19 and D20 contracts that the roadmap ports. Ask uses only fantasy's wire layer.
- **cobra or pflag for the CLI.** Pi's rules (`-p` eats the next token unless it starts with `@` or `-`, unknown flags rejected, `--` handling) do not map onto pflag's grammar. A table-tested parser of about 150 lines is smaller than the workarounds.
- **The dewee `Step` and `TurnState` pipeline.** It adds ordered multi-handler steps that only H11 needs. H2 uses one typed func field per hook point, so a reader traces a hook in one hop.
- **An fx module for headless mode.** Only one composition exists in H2. H13 adds four and their `fx.ValidateApp` tests. Plain constructors in `cmd/tui` now, fx when there is a second composition.
- **Recovering loop hook errors inside the loop.** The operator chose Pi's contract (D20). The wrapper builds the error message.
- **Repairing orphan tool calls after abort inside the loop.** The operator chose Pi's behavior (D19). H3's `transformMessages` repairs them.

## Appendix C. Risks

- **H1 is not on trunk (all PRs).** The stack cannot start until H1 and the Go bump merge. The first Arm box gates this.
- **Event order under parallel tools (H2-B).** Completion order and source order differ by design. The owner watches `-race -count=5` and the order test.
- **Goroutine leaks on abort (H2-B, H2-E).** Every tool goroutine and the stream consumer must exit on cancel. goleak runs in `internal/agent` and in the in-process CLI tests.
- **Listener backpressure deadlock (H2-C, H2-E).** Listeners run on the loop goroutine. A listener that calls back into `Agent` would deadlock. The owner documents that rule on `Subscribe` and tests that `Abort` from another goroutine still ends a stalled run.
- **The faux demo prefixes (`echo `, `fail `) leak into real use (H2-D).** They exist only inside `headless_faux.go` and only when the provider is faux. H3 keeps faux as an explicit `--provider faux` choice.
- **TTY detection in CI (H2-D).** CI has no TTY, so every run picks print mode. Tests pass explicit argv and never rely on the TTY branch except one test that fakes it.
- **Fantasy retries (H3, recorded here).** Fantasy's Anthropic provider leaves the SDK's default retries on. H3 must turn them off so H9 owns retry.

## Appendix D. Links and reading list

- Read before editing. `plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md`, the three H2 researcher reports it links, the roadmap H2 section and decisions D5 and D18 to D22, `docs/ask-architecture-reference.md` section 7.3, and the READMEs of `internal/agent`, `internal/pipeline`, `internal/tools`, `internal/sessions`.
- Pi sources, at `/Users/dale/Desktop/workspace/opensources/pi` commit `2bbfcca4`. `packages/agent/src/agent-loop.ts`, `types.ts`, `agent.ts`, `packages/ai/src/utils/validation.ts`, `packages/coding-agent/src/modes/print-mode.ts`, `json-event.ts`, `core/output-guard.ts`, `cli/args.ts`, `cli/initial-message.ts`, `cli/file-processor.ts`.
- H2-B gets `pstack/skills/how/SKILL.md` before code and `pstack/skills/interrogate/SKILL.md` on its loop and abort design before the code-ready report. H2-C gets `interrogate` on the listener and failure path.
- Each owner keeps a `decisions.tsv` trail per `pstack/skills/show-me-your-work/SKILL.md` and returns it with its report. The root keeps the program trail.

## Appendix E. Data shapes and the reference event order

These are contract sketches, not generated code.

```go
// internal/tools
type Tool interface {
	Decl() protocol.ToolDecl
	Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error)
}
type Sequential interface{ Sequential() bool }
type ArgumentPreparer interface {
	PrepareArguments(raw json.RawMessage) (json.RawMessage, error)
}
type Context struct {
	CallID string
	Cwd    string
	Update func(partial protocol.ToolExecutionResult)
}
type SourceInfo struct{ Kind, Name, Path string } // Kind: builtin, extension, mcp
func (r *Registry) Register(t Tool, src SourceInfo) error
func (r *Registry) Lookup(name string) (Tool, SourceInfo, bool)
func (r *Registry) Prepare(name string, raw json.RawMessage) (json.RawMessage, error)
func (r *Registry) Decls() []protocol.ToolDecl

// internal/pipeline
type TurnDecision uint8 // Proceed, Continue, End
type Hooks struct {
	TransformContext    func(ctx context.Context, msgs []protocol.Message) ([]protocol.Message, error)
	ConvertToLLM        func(msgs []protocol.Message) ([]protocol.Message, error)
	GetAPIKey           func(ctx context.Context, provider string) (string, error)
	PrepareRequest      func(ctx context.Context, req providers.Request) (*providers.Request, error)
	FinishTurn          func(ctx context.Context, t TurnResult) (TurnDecision, error)
	BeforeToolCall      func(ctx context.Context, c ToolCallInfo) (*Block, error)
	AfterToolCall       func(ctx context.Context, c ToolCallInfo, r protocol.ToolExecutionResult) (*ResultPatch, error)
	GetSteeringMessages func(ctx context.Context) ([]protocol.Message, error)
	GetFollowUpMessages func(ctx context.Context) ([]protocol.Message, error)
}

// internal/agent
type Status uint8 // Idle, Running
type ContextSource interface {
	Append(m protocol.Message) error
	Messages() []protocol.Message
}
func Run(ctx context.Context, prompts []protocol.Message, cfg LoopConfig, emit func(protocol.Event) error) ([]protocol.Message, error)
func Continue(ctx context.Context, cfg LoopConfig, emit func(protocol.Event) error) ([]protocol.Message, error)
func New(cfg Config) (*Agent, error)
func (a *Agent) Prompt(ctx context.Context, msgs ...protocol.Message) error // ErrBusy while Running
func (a *Agent) Continue(ctx context.Context) error
func (a *Agent) Abort()
func (a *Agent) WaitForIdle(ctx context.Context) error
func (a *Agent) Reset() error
func (a *Agent) Subscribe(l func(protocol.Event) error) (unsubscribe func())
func (a *Agent) State() State
```

Reference event order for `ask --mode json "echo hi"` on the H2 faux script.

```text
agent_start
turn_start
message_start(user) message_end(user)
message_start(assistant) message_update(toolcall_start) message_update(toolcall_delta)... message_update(toolcall_end) message_end(assistant)
tool_execution_start(echo) tool_execution_end(echo)
message_start(toolResult) message_end(toolResult)
turn_end
turn_start
message_start(assistant) message_update(text_start) message_update(text_delta)... message_update(text_end) message_end(assistant)
turn_end
agent_end
agent_settled
```

Coercion table (D21). Each row is one subtest in `coerce_test.go`.

| Target type | Input | Result |
|---|---|---|
| integer | `"5"` | 5 |
| integer | `"5.7"` | validation error |
| number | `"2.5"` | 2.5 |
| boolean | `"true"`, `1` | true |
| boolean | `"false"`, `0` | false |
| string | 5, true | `"5"`, `"true"` |
| null in `anyOf` | null | null kept |
| optional field | null | field removed |
| object root | missing or null | `{}` |
| `anyOf` with a valid arm | valid value | unchanged |

## Appendix F. Review (local mode, 2026-10-02)

The stack is five local branches on `master-2` (`b0b1f5e`, H1 plus Go 1.27.0). Each was verified by the root at the SHA below with `go test -race ./...`, `go vet ./...` and golangci-lint (0 issues). Nothing is pushed.

| PR | Branch | Verified SHA | Live and perf evidence |
|---|---|---|---|
| H2-A tools | `h2-a` | `1874505` | 10 lanes in `/tmp/swarm-H2-A/lanes.txt`. Prepare is 1.6 µs and 31 allocs (budget 50 µs, 200). |
| H2-B loop | `h2-b` | `fc52b13` | Reference event order end to end; goroutines 1 before and after 200 runs. LoopTurn 15 µs, 195 allocs (budget 500 µs, 2000). |
| H2-C Agent | `h2-c` | `813bf85`, `4b6c193` | 23-event envelope test, seq gap-free across runs. Wrapper adds 3.5 µs per prompt (limit 100 µs). `4b6c193` adds `internal/sessions` to the depguard `core-no-agent` rule. |
| H2-D print | `h2-d` | `5847ca1` | Lanes in `/tmp/swarm-H2-D/lanes.txt` give 0, 1, 130 (tmux C-c), 143 and 129. `ask -p hello` median 4.8 ms; binary +2.68 MiB (limit 8 MiB). |
| H2-E JSON | `h2-e` | `19dbeb9` | Lanes in `/tmp/swarm-H2-E/lanes.txt`. Closed stdout exits 1 (trunk 141). Slow reader loses no line. A 1 MiB reply takes 0.081 s in JSON mode and 0.029 s in print mode (2.8x, rule 3x). |

**Departures added during the build.**

- H2-B. `PrepareRequest` takes `pipeline.Request` and returns `*RequestUpdate`; `BeforeToolCall` may replace the arguments; the partial message is not refreshed per delta; only tool-path panics are recovered in the loop (the wrapper recovers the rest).
- H2-C. `Prompt` returns the run error, so the CLI can exit 1. The failure message uses `aborted` when the run was cancelled. The system message is rebuilt per run and not stored in the log. `Reset` calls `Config.NewContext`. After `Abort` the loop still delivers events the provider already buffered, as Pi does.
- H2-D. Unknown flags and value flags without a value are errors. `--api-key` needs an explicit `--model`. The signal wait is bounded at 2 s. `@file` images, `provider/id` model forms and `--version` are not ported.
- H2-E. JSON mode exits 0 on an assistant error, as Pi does. Text and thinking delta `message_update` events skip reflection (`pkg/protocol/codec_fast.go`); a test pins the bytes to the reflection encoding. Without it JSON mode was 5.3x print mode, because each event paid for encoding/json reflection and usage marshalling.

**Parked, with reasons.**

- Review-gate screenshots and video for H2-D and H2-E. `vhs` and `hyperfine` are not installed; the lane captures in `/tmp/swarm-H2-D` and `/tmp/swarm-H2-E` stand in, and perf used interleaved timing loops.
- The `how` and `interrogate` passes per PR. The owners could not spawn reviewers; H2-C's listener and failure path are the first candidates.
- Forge, Bugbot, CI and rebase boxes. Local mode.
