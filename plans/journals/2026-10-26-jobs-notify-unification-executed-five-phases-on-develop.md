---
title: "Jobs notify unification executed: five phases on develop"
date: 2026-10-26
summary: Shell jobs now notify through Agent.Notify per conversation; ask -p waits for background work with limits and --max-turns; two latent bugs found during verification.
---

# Jobs notify unification executed: five phases on develop

## What happened

Executed plans/261026-1519-jobs-notify-unification in five phases on develop (c9877a6, 0aef74f, 20fab4c, 2a71602, 484bac8, f1fd5f3).

- The Agent gives each tool call a notice port bound to the conversation generation. Shell jobs send completion notices and monitor events through the port of the conversation that started them. After /clear the old jobs run on, hidden, and their notices are dropped as stale.
- ACP and print mode wait on Agent.Idle() after ErrBusy. ACP binds a call only to its own run (agent.WithRunStart), so a notice run can no longer take a client's early cancel.
- ask -p prints the first reply, waits by signal for quiescence, and prints the reply of each run that a notice wakes. Background jobs have a 10 min default limit (2 h max) in print mode only; monitors are capped too. An idle-wait cap ends a wait with only watches left. --max-turns counts woken turns. ASK_DISABLE_BACKGROUND_TASKS turns background work off.

## Bugs found during verification

- Wait returned Done for a background job whose notice had already started, so the end was reported twice. It showed only under heavy machine load (load average near 300). Older than this work; production waits only on foreground jobs. Fixed with a deterministic test.
- With --mode json, the max-turns line went to stdout and broke the JSON event stream. Now on stderr.

## Lessons

- A full-suite failure under load is not automatically a flake: A/B runs at low load showed 0/30 on both trees, and only reading the lock order found the real race.
- Two load flakes stayed: TestNewerBuildReplacesABusyLeader and TestFindTimeoutReturnsPartialResult. Both pass alone and do not touch the changed packages.
- The ak plan/phase update CLI reported "plan not found" for this plan; status was edited in the files.

## Next steps

- Push develop; CI runs only on master pushes and pull requests, so a draft PR is needed to get CI evidence.
- Open: the stdio watchdog ticker and the gRPC demo sleep in docs/async-task-runtime.md section 12.2; two ADR files share number 0004.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
