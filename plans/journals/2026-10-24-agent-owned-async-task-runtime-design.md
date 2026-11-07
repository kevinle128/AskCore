---
title: Agent-owned async task runtime design
date: 2026-10-24
summary: Designed push-based async tasks owned by the Agent; ADR 0004 accepted; A1 plan ready
---

# Agent-owned async task runtime design

## What happened
- Researched Go goroutine/channel practice and reference harnesses (Pi, dewee, Claude Code, Codex, Hermes, OpenCode, goose, agentcore, agentgo). Reports in `plans/reports/researcher-261024-1559-*`.
- Wrote `docs/async-task-runtime.md` and `docs/adr/0004-agent-owned-async-task-runtime.md` (accepted). Twelve Mermaid diagrams render with `mmdc`.
- Core shape: per-Agent `tasks.Registry` on a lifetime context; one terminal state and one notice per task; `Agent.Notify` is the only delivery port and maps notices onto the existing steering, follow-up, and context claims. No new queue.

## Decisions (user)
- D1: no cap on wake-started runs. Prompt cache makes one wake cheap; the risk is the count, to revisit when watches or sub-agent fan-out ship.
- D2: no goroutine pool and no concurrency limit in M1. A semaphore per resource class is the documented path if processes or 429s become a problem.
- D3: `ask -p` waits for background work and takes one more turn per completion, as `claude -p` does (10-minute default limit in print mode).
- D4: a foreground command that reaches its timeout moves to the background, as in Claude Code.
- `smarty/go-disruptor` rejected: its consumers wait by sleep-poll or busy spin (`config.go:144-146`), about half a core while idle.

## Found on the way
- `providers.ConvertToLLM` drops custom roles, so the notice message needs a converter change.
- `acp/host.go:512-522` has a 2 ms sleep loop; `WaitForIdle` ignores `dispatching`. Replaced by an idle broadcast in phase A2.

## Next steps
- Execute `plans/261024-0958-async-task-runtime-a1/plan.md` with `--tdd`.
- Open: D5 (sub-agent depth) at phase A4; task events in headless JSON at A3.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
