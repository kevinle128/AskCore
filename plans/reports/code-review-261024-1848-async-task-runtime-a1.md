# Code review: async task runtime A1 (uncommitted)

Date: 2026-10-24. Reviewer: code-reviewer. Plan: `plans/261024-0958-async-task-runtime-a1/`. Design: `docs/async-task-runtime.md`.

## Scope

- Tracked diff (27 files, +831/-61) plus new: `internal/tasks/**` (~2.5k LOC incl. tests), `internal/agent/{tasks,idle}.go` and 4 test files, `internal/settings/paths_test.go`, `pkg/protocol/{custom_message,task_events}_test.go`.
- Checks run: `go vet` (changed pkgs) clean; `go test -race -count=3 ./internal/tasks/... ./internal/agent/...` green; `go test -race` of protocol, providers, settings, app, tools, acp green; golangci-lint on changed pkgs: 0 issues.
- Probes and mutations ran in throw-away copies under `/tmp` (deleted after). No repo file changed except this report.

## Findings

### High

**H1. A Wake notice that arrives during a turn that ends without a queue claim is stranded; `WaitForQuiescence` then reports quiescence.**
- Evidence: `internal/agent/tasks.go:117-123` (running, not closing: steering only, no `wake`); `internal/agent/loop_stage.go:278-279` (a failed message or a `pipeline.End` decision returns `flowEndRun` with no claim; the same for admission reject `:97-100` and stage errors); `internal/agent/agent.go:474` (`end()` starts a run only when `a.wake`); `internal/agent/tasks.go:159-176` (quiescence checks only `a.idle`; the plan's `in.len() > 0` term was dropped).
- Scenario (probe, confirmed): model starts a 1 s Wake timer; the next model reply streams for longer than 1 s and ends with `StopError`. The timer fires during that reply. Result: `p.Calls()==2`, registry `Info.Notice == delivered`, Agent idle, `WaitForQuiescence` returns nil, and one hour of fake time later still no run. The notice reaches the model only with the next human input.
- Exposure: zero in A1 production (no production tool calls `tools.Context.Tasks`). Real in A3: `ask -p` would exit with an undelivered completion after any provider error, hook End, or blocked admission.
- Fix options (decide with the lead):
  1. Set `a.wake = true` in the running-not-closing Wake branch. Simplest. `end()` already checks `in.len() > 0`, so a notice that the run claims gives no extra run. Trade-off: after a failed run, a wake-started run also claims stranded human steering (behavior change for human input).
  2. Narrower: in `end()`, start the next run when `in.steering` holds an input with `origin == originTask` (keep `wake` for human input as today).
  - In both: `WaitForQuiescence` must count queued task-origin steering as pending work.
- Test gap: no test for a notice that lands during a turn that ends with `StopError`, `pipeline.End`, or a rejected admission.

**H2. `tasks.Sweep` follows a symlinked root and removes any old directory under it; the root is never checked before the sweep.**
- Evidence: `internal/app/agent_native.go:118-123` sweeps `<home>/tasks` on the first `NewNativeAgent` of every process, before any check of `home` or `root` (`ensureTaskDir` runs only lazily, at the first task write). `internal/tasks/sweep.go:26` `os.ReadDir(root)` follows a symlink at `root`; `:38-50` treat every child directory as a session directory and call `os.RemoveAll`. The doc comment at `sweep.go:18` says Sweep "does not follow symbolic links"; that is true only for children.
- Scenario (probe, confirmed): `home/tasks -> /…/projects`, `projects/important-work/notes.txt` 30 days old. `Sweep` returns `err=nil` and `important-work` is gone. The same happens without a symlink when `ASK_HOME` points at a directory that has a user `tasks/` folder.
- Severity rationale: irreversible, silent data loss, and it violates the plan constraint "each directory level is checked as `settings.ensureHome` does". Rated High, not Critical, because it needs a non-default configuration (user-set `ASK_HOME` or a user-made symlink); the default `~/.ask` is 0700 and an outside attacker cannot plant the link. Treat as Critical if `ASK_HOME` overrides are common for your users.
- Fix: before `ReadDir`, `Lstat` home and root and require dir, not symlink, owner = euid, perm 0700 (skip the sweep, do not create, if root is missing). Per entry, require the session shape before removal: 0700, owned, plain name, and only regular `*.log` children whose names match the task-ID form; remove files one by one, then `os.Remove(dir)` (not `RemoveAll`). Add a test with a symlinked root and with a foreign-shaped child directory.

### Medium

**M1. Dispose can return while a Reset still closes the old registry, so tasks of the Agent outlive Dispose.**
- Evidence: `internal/agent/agent.go:252` dispose reads only the current registry; `:264` closes it; `:369` Reset closes the old registry after it released every lock, and nothing records that close.
- Scenario (probe, confirmed): body ignores ctx; `go a.Reset()` blocks in `oldTasks.Close`; `a.Dispose()` returns with the old body still running (`ended=false`). `endLife` cancels the old Root, but Dispose does not wait. This breaks the contract "Dispose stops and waits for every task" (`agent.go` Dispose doc, criterion 5), and a sink can write after Dispose.
- Fix: track retiring registries (for example a `sync.WaitGroup` that `swapContext` adds to under `a.mu` and Reset marks done after `oldTasks.Close`); dispose waits on it after `endLife`.

**M2. A Context-policy notice gives an extra model request or none, depending on timing.**
- Evidence: `internal/agent/queue.go:112-113` `claimSteering` takes `q.context`; `queue.go:391-392` the closing `claimCycle(ending=true)` therefore returns a context-only claim, which `loop_run.go` turns into a new cycle.
- Scenario (probe, confirmed): a Context timer that fires while the final reply streams gives `p.Calls()==3` (a new cycle with only the notice). The same notice that fires after `claimCycle(ending)` gives no request. Design 7.2 says "claimed with next admission" (ambiguous), but 7.3 says for `task_stop` "a wake is not needed". Before tasks, `in.context` was filled only for cancelled runs, so this path is new.
- Fix: in `claimCycle`, when steering and follow-up are empty, leave `q.context` in place (claim it only together with real input), or record in the design that Context extends an open run. Add a test for both timings.

**M3. Phantom test: `TestWaitForQuiescenceRace` does not exercise the race it names; the work counter is unverified.**
- Evidence: `internal/agent/idle_test.go:142-173`; mutation `if after == before` -> `if true || after == before` at `internal/agent/tasks.go:174` survived `-count=5` of every `Quiescence` test. The test proves "registry idle while Agent busy", which the two channel waits already cover.
- Real interleaving the counter guards: registry idle at the first check, a run (busy at that time) starts a task and ends before the `a.idle` read; without the counter the call returns with a task active.
- Fix: add a test hook between the registry check and the `after` read (export_test), drive that interleaving, and keep the mutation killed. Or state in the doc comment that the counter is defense only.

**M4. The injected `Env` does not isolate the task home; tests sweep the developer's real `~/.ask/tasks`.**
- Evidence: `internal/app/agent_native.go:110-113`: when `in.Env("ASK_HOME")` gives "", `settings.ResolveHome("")` reads `os.Getenv("ASK_HOME")` and then `~/.ask` (`internal/settings/paths.go:71`). `internal/acp/stdio_test.go:115` calls `NewNativeAgent` with no `Home` and no `Env`; `cmd/tui` in-process tests inject a `getenv` that returns "" for `ASK_HOME`.
- Impact: `go test ./internal/acp/...` and `./cmd/tui/...` run `Sweep` on the real home and give each test Agent `TaskDir=~/.ask/tasks/<id>` (lazy, no write in A1). With H2 this is the path to data loss on a developer machine. The process-wide `sync.Once` also makes sweep coverage depend on test order (only `TestNativeAgentSweepsOnce` resets it).
- Fix: when `in.Env` is set, treat it as authoritative (no fallback to `os.Getenv`). The auth store has the same fallback (`settings.NewAuthStore` -> `ResolveHome`; `cmd/tui/headless.go:82,143` passes `deps.getenv("ASK_HOME")`), so change both or scope the change on purpose; else one process can use two homes. Set `Home: t.TempDir()` in `internal/acp/stdio_test.go:115` and in the `cmd/tui` in-process runs.

### Low

- **L1.** `EnsurePrivateDir` doc says it refuses "a mode other than 0700" (`internal/settings/paths.go:90`), but the code corrects the mode (`:116-121`) and the test expects correction. The plan said "refused". Correction matches `ensureHome` and is arguably better; fix the doc and record the deviation.
- **L2.** Status race in `internal/tasks/runner.go:24-28`: a body that returns success, then a Stop/Close/deadline that lands before the `ctx.Err()` read, gives `stopped`/`timed_out` instead of `completed`. Fix: give priority to the result when the body returned without observing cancellation, or accept and document.
- **L3.** Design mermaid `docs/async-task-runtime.md:302` says closing -> "append to followUp"; the table above it and the code (`tasks.go:117-124`) use steering.
- **L4.** `notice.go:116` passes the output path through `boundField`, so a path over 1 KiB becomes a wrong path in the text; `noticeDetails` keeps it whole on purpose. Skip bounding for `output-file` as done for details.
- **L5.** `Notify` (`tasks.go:104-108`) accepts a `CustomMessage` with an empty `CustomType` or a typed-nil pointer; `CustomMessage.MarshalJSON` rejects the first when the log writes it. Validate at `Notify`.
- **L6 (forward risk, not a defect now).** Every task event posted on an idle Agent runs a publisher dispatch, and `dispatching > 0` gives `ErrBusy` to `Prompt`/`Reset`. With `task_update` every second per task (A3), idle Agents turn busy more often. The ACP host has a 250 ms busy loop; other callers need `Idle()`-based retry.

## Verified OK

- `retryBusy` in `queue_test.go`, `reentry_test.go`, `tool_context_test.go` hides no regression: HEAD (`git archive`) fails the same tests 3 times in 600 runs with "a run is active". `busyLocked` equals the old predicate (`agent.go` `idleLocked`).
- Mutations killed: removing `wake` in the closing branch (`TestNoticesDuringClosingShareCycle` fails); removing the idle edges in `dispatch` (`TestIdleCoversPublisherDispatch` fails); moving `active--` before `Deliver` (`TestIdleAfterDeliver` and `TestWaitForQuiescenceBlocksWithoutSpin` fail).
- Idle channel: one predicate, edge-only make/close at `startRunLocked`, `end`, `dispatching++/--`; the wake chain in `end` keeps busy true (`idle.go:21-31`, `agent.go:470-481`).
- `WaitForQuiescence` waits only on open channels; no spin; dropping `in.len() > 0` avoids the spin the plan's step 3 would cause on a closed `Idle()` (but see H1).
- Registry terminal order: claim -> `task_end` -> `Deliver` (no lock) -> `active--`/idle -> `work` -> `close(done)` -> `wg.Done` (`runner.go:78-153`, `timer.go:15-35`). Timer ownership: `timer.Stop()==true` gives ownership; `finish` claim is the second guard; `wg.Done` once in both orders.
- Throttle: `endPosted` under `evMu` stops `task_update` after `task_end` (`runner.go:171-186`).
- Lock order: the registry never calls `Deliver`, `Emit`, `Work`, `NextID`, or a body under `r.mu`; the emit gate takes only `a.mu`; `swapContext` builds the new registry under `emitMu`+`mu` (no callbacks) and Reset closes the old one after both are released; `a.posted = nil` drops old-generation events that were posted and not flushed.
- Generation gate: `Notify` refuses old `Gen` with `ErrStaleNotice` (wraps `ErrNotDelivered`); disposed refusal matches `ErrDisposed` and `ErrNotDelivered`.
- Queue cap: `humanLen` counts only human input (`queue.go` `humanLen`, `enqueue`); notices never get `ErrQueueFull`. Abort moves task-origin inputs to `context` and drops human ones (`queue.go` `abort`).
- Dispose order: `disposed` set first, run drained, `reg.Close` outside locks, `endLife`, then the existing drain; `task_end` precedes `agent_disposed` (test).
- Notice escaping: `&`, `<`, `>` only, line breaks kept, framing line fixed, tail cut after escaping on a rune boundary; `</task-notification>` in a title cannot close the block (test).
- Sink: lazy open with `O_CREATE|O_EXCL|O_WRONLY|O_NOFOLLOW`, mode 0600; `PrepareDir` once per registry through `settings.EnsurePrivateDir` at home, root, and session levels; Sweep skips symlinked children and never schedules itself.
- Protocol: `CustomMessage` codec, reserved role, deep clone (value and pointer); `ConvertToLLM` maps custom to user; `queue_update` preview at most 200 bytes, on a rune boundary, from `Details` for task notices.
- No `time.Sleep`/`NewTicker` in changed non-test code (`settings/lock.go` ticker is old and unchanged). No plan, phase, or finding labels in code comments, test names, or READMEs. Strict depguard for `internal/tasks` is in place; goleak `VerifyTestMain` covers `internal/tasks` and `internal/agent`.

## Recommended actions

1. H1: choose fix option 1 or 2 with the lead; update `WaitForQuiescence`; add failed-turn, End-decision, and blocked-admission notice tests.
2. H2 + M4: validate root and entries before the sweep; make `Env` authoritative; give test Agents a temporary home.
3. M1: make Dispose wait for registries that Reset retires.
4. M2: decide the Context semantics; change `claimCycle` or the design text.
5. M3: add a deterministic quiescence race test (hook) or document the counter as defense only.
6. L1-L5 as convenient.

## Plan follow-ups

Phases 1-3 code exists and the listed tests are present; acceptance criteria 1, 2, 3, 4, 5a, 6 have passing tests. Criterion 7 is verified only for the changed packages and `internal/acp` (race tests and lint); `./...` and `cmd/tui` were not run in this review. Criterion 5 is incomplete because of M1, and H1 is a gap in the meaning of criterion 3. Phase files still say `status: pending`; the lead updates them.

## Unresolved questions

- ACP `admit()` (`internal/acp/host.go:458-488`) binds the first event with a run ID after admission to the client call. A notice-started run would be mis-attributed. Not reachable in A1; must be solved before tasks run under ACP (A2/A3).
- Is the mode correction in `EnsurePrivateDir` (L1) the accepted behavior, instead of the refusal in the plan?

Status: DONE
