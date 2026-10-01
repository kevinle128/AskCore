# waku-agent "Watch it think" distilled for AskCore

Date: 2026-09-30. Source: `/Users/dale/Desktop/workspace/opensources/waku-agent` at 9660fb6 (read only). Paths below are relative to that repo unless prefixed `AskCore:`. Nothing was run; every claim comes from reading code, docs and git history.

## 0. Outcome

Waku's feature is small. The harness calls one observer callback `notify(kind, event)` for each step. Two consumers sit behind it: a JSONL trace file and the dashboard. The dashboard lights up boxes on an architecture diagram from those events. For AskCore the transferable part is the pattern (one event stream, many watchers, the loop never knows who watches), not the code.

Recommendation, ranked:

1. Build the event contract on Pi's event names, with a sequence number, and publish it on `internal/bus`. This is required for every option.
2. Deliver "watch it think" as a TUI inspector in `cmd/tui` (Option B). It needs no user decision on the web-UI rule.
3. Get OTel export (Option C) as a by-product of `internal/tracing`. It is cheap, but it is not a live view.
4. Do not build a web dashboard (Option A) unless the user lifts the "web UI out of scope" rule. This needs a user decision, see section 9.

## 1. Ring 0: what the user sees

- README.md:18: "Watch it think. A local dashboard lights up every message as it flows through the harness."
- docs/tour.md:5-8: `waku dashboard` starts a local server on `http://localhost:7777`.
- docs/tour.md:16-18: type in the chat dock, and the Overview diagram shows "gate lights up -> loop calls a tool -> reply comes back -> memory updates". "The frontend is plain static files. No build step."
- The lighting also works for messages that arrive from other gateways (Telegram, voice, CLI), not only the browser (waku/ops/static/js/diagram.js:101-102, waku/ops/dashboard.py:871-873).

## 2. Ring 1: dashboard features

Tabs, from docs/tour.md:22-31:

| Tab | Content |
|---|---|
| Overview | cost, latency, gate skip/retrieve split, clickable architecture map (the diagram that lights up) |
| Gateway | one conversation across channels, each message tagged by source |
| Loop | each turn with gate decision, tool calls, tokens, cost |
| Graph | graph workflow topology and which route each turn took |
| Memory, Tools, Data | memory sub-tabs, tool list, live SQLite browser with read-only SQL console |
| Ops | eval verdict, gate decisions, slowest turns, inline JSONL traces |

The "lights up" mechanism (waku/ops/static/js/diagram.js:105-113):

- A static table `STAGE` maps event type to diagram node ids and edge ids, plus a label: `turn_start`, `gate`, `llm`, `tool`, `turn_end`, `consolidation`.
- A hit adds CSS class `hot` to nodes and `live` to edges for 1000 ms (diagram.js:117-121).
- Events go into a queue and play one every 620 ms, so a fast turn is still visible (diagram.js:139).
- When the gate decision is `retrieve`, the three memory boxes also light up (diagram.js:127-130).
- The file warns that node ids must match the SVG ids by hand, and no test catches a mismatch (diagram.js:107-110).

Other features:

- Message detail: a Loop card shows user message, gate, LLM calls with token usage, tools, reply (waku/ops/dashboard.py:299-355 builds the turn cards from the trace).
- Hang detection: a `turn_start` with no `turn_end` is shown as "TURN NEVER FINISHED" (dashboard.py:351-354).
- Cost and tokens: every `llm` event carries `usage.in/out`. A permanent `usage.jsonl` ledger keeps spend after traces are reset (waku/ops/tracing.py:103-114). Dollars are computed later from tokens, because prices change (tracing.py:106-108).
- Per-session view: chat history per thread, and `__all__` for the whole timeline (dashboard.py:721-726).
- Filters, search and replay: none found. History is "read all trace files" (dashboard.py:299-311). Nothing lets the user scrub or replay a past turn.
- Terminal alternative: `waku trace` reads the same files (waku/ops/README.md:27, waku/ops/show_trace.py, 177 lines).

## 3. Ring 2: exact surface

| Item | Value | Cite |
|---|---|---|
| Start | `waku dashboard` or `make dashboard` (`python -m waku.ops.dashboard`) | waku/__main__.py:4, Makefile:37-38 |
| Port | env `WAKU_DASHBOARD_PORT`, then `PORT`, then 7777. If busy, walks up to 9 ports | dashboard.py:58, 1229-1240 |
| Bind | `127.0.0.1` unless env `WAKU_DASHBOARD_HOST` is set. A non-loopback host prints a warning | dashboard.py:1208-1224 |
| Server | stdlib `ThreadingHTTPServer`, zero dependencies | dashboard.py:30, 1237 |
| Live channel A: chat SSE | `POST /api/chat/stream`, `text/event-stream`, frames `data: {"kind":..., ...}\n\n`, ends with a `done` event | dashboard.py:1020-1041 |
| Live channel B: cross-gateway polling | `GET /api/events?cursor=N` every 450 ms; the browser uses fetch, not SSE | dashboard.py:986-991, main.js:311, diagram.js:141-152 |
| Full data | `GET /api/data`, rebuilt from all trace files on every call; the page refreshes it every 5 s | dashboard.py:914, 275-355, main.js:310 |
| Trace storage | `<WAKU_HOME>/traces/<YYYY-MM-DD>.jsonl`, default home `.waku` | waku/config.py:64, 148; tracing.py:63 |
| OTel | env `OTEL_EXPORTER_OTLP_ENDPOINT`; optional extra `waku-agent[tracing]`; spans for Phoenix or Langfuse | tracing.py:9-17, 68-88; config.py:141-144 |
| Frontend | plain static JS/CSS, classic scripts in one global scope, no build | waku/ops/static/README.md:3-8 |

Event vocabulary (the observer "kinds"):

- Loop: `text` (delta), `llm` (iteration, stop_reason, usage), `tool` (tool, args, output) - waku/loop/agent.py:93, 111, 130.
- Memory: `gate` (decision, reason), `consolidation` (new_facts) - waku/memory/__init__.py:96, 202.
- Graph: `graph_start`, `node_start`, `node_end`, `route`, `triage`, `graph_end` - waku/graph/engine.py:126-212, waku/graph/workflows/triage.py:101.
- Turn markers: `turn_start`, `turn_end` are written by the tracer, not by the loop - tracing.py:140, 155.
- Sub-agent: `subagent` (agent, type text/tool/turn_end) - waku/tools/experimental.py:148-168.

Envelope: each JSONL line is `{"type": kind, ...event, "ts": ISO-8601 ms UTC}` (tracing.py:99, 126). There is no schema, no session id, no run id, and no sequence number. The `llm` event is stamped with `provider` and `model` (tracing.py:120-125).

## 4. Ring 3: how the harness emits events

- One observer type, `notify(kind, event)`. `Waku.respond` composes three observers with `compose(observer, tracer.event, _capture)` and passes the result down the whole stack (waku/app.py:63; compose at tracing.py:161-167).
- Hook points: the loop for `text`, `llm` and `tool` (agent.py:93, 111, 130); tools receive `notify` through `_notify` (waku/tools/registry.py:22, 47-55); memory `gated_retrieve` and `maybe_consolidate` (memory/__init__.py:91-96, 192-202); the graph engine (engine.py:113-212).
- The events are synchronous. Each observer runs inline in the loop thread and in order. `Tracer.event` opens the daily file in append mode and writes one JSON line for every event (tracing.py:100-101).
- The `text` delta is not traced: "streaming token deltas are for the live UI, not the trace" (tracing.py:118-119). So the cross-gateway polling view never sees tokens. Only the browser's own SSE chat sees them.
- If nobody watches: nothing extra happens. The dashboard is a reader of files, so the harness pays only for the JSONL write, which is always on. "Ops observes, it never participates. You can delete this whole directory and waku still runs" (waku/ops/README.md:3-7).
- OTel spans are one span per event, opened and closed at once (empty `with` body, tracing.py:127-135). They are point events under a per-turn root span `agent_run`, not real durations. `end_turn` flushes the provider with a 2 s timeout so a killed process keeps the trace (tracing.py:156-158).
- Overhead: an open/write/close of a file per event, no buffering. Cheap for a personal agent. For AskCore it is the wrong model at Pi's event rate (many `message_update` per second).

## 5. Ring 4: edge cases

| Case | Waku behaviour | Cite | Verdict for AskCore |
|---|---|---|---|
| Many sessions | Trace has no session id. All gateways append to one daily file. Chat threads are separate only in `state.db` | tracing.py:63; dashboard.py:721 | Add `sessionId` and `runId` to every event |
| Large messages | Full `args` and `output` of every tool call go into the trace. No truncation seen | tracing.py:126 | Need bounded previews, as `internal/tracing/README.md` already says |
| Secrets, redaction | None. grep for redact/secret/mask in tracing.py, dashboard.py, app.py found nothing | - | Gap. Redact in `tracing`, before any subscriber sees it |
| Reconnect | Polling is stateless. The client re-sends its cursor; on a bad cursor the server returns the tail | dashboard.py:884-886 | Use an explicit sequence number and resume with `after=seq` |
| Ordering | Order = line order in one file. Cursor = number of lines | dashboard.py:870-892 | Same idea; use a monotonic `seq` instead of a line count |
| Dashboard opened after the run | First poll has no cursor and receives only the tail, "so the browser starts fresh instead of replaying history" (dashboard.py:873-875, 885). The Loop tab does show past turns from the files | dashboard.py:299-311 | Live view = tail only; history = a separate read path |
| Midnight rollover (inferred from code, not run) | `Tracer.path` is fixed at construction (tracing.py:63). `events_since` recomputes today's file on each call (dashboard.py:877). A process alive past midnight keeps writing yesterday's file while the live poll reads the new one, so the diagram stops lighting | - | Do not key a live stream to a file name |
| Read cost | `events_since` reads the whole day file on every 450 ms poll, per open tab | dashboard.py:881 | O(file) per poll. Avoid: keep events in memory with a ring buffer |
| Slow or gone consumer | SSE `emit` swallows `BrokenPipeError` and `ConnectionResetError` | dashboard.py:1030-1034 | Bus must drop or disconnect slow subscribers, never block the loop |
| One agent for all tabs | One shared agent guarded by `agent_lock` | dashboard.py:96-98; ops/README.md:52-57 | AskCore is multi-session by design |
| Local port security | Default bind is loopback. No authentication. The SQL console runs arbitrary SQL. Only a warning for a non-loopback bind. grep for Origin/Host checks found none | dashboard.py:1211-1224 | Risk: any web page in the user's browser can call a loopback server (DNS rebinding or plain cross-origin POST). If AskCore serves anything over HTTP, require a token |
| Trace encoding | A legacy non-UTF-8 trace file blocks the dashboard payload with a clear error | tracing.py:35-54 | Not relevant |

## 6. Ring 5: history (git)

- 2026-07-10 `cc8c7d2` Phase 3: JSONL + OTel tracing, eval suites, release gate. The tracer and the OTel switch appeared here.
- 2026-07-10 `5154c12` "Live harness animation: the diagram lights up as a turn flows through (n8n-style)". This is the feature.
- 2026-07-11 `bcfc9a6` "Stream the reply as it's generated (Hermes-style live thinking)". Adds the `text` delta and the SSE chat.
- 2026-07-11 `3cb5928` permanent spend ledger (`usage.jsonl`). 2026-07-11 `780d712` project rename from jarvis to waku.
- 2026-07-17 `9aca6de` model-stamped traces (`provider` and `model` on every `llm` event) after live testing showed a multi-model trace was "half a trace".
- 2026-07-18 `69cbe28` frontend split: `app.js` (1386 lines) into 8 files.
- 2026-07-23 `c99d351` JSONL written as UTF-8 (Windows bug).
- 2026-07-31 `5d34d3e` agent graphs: adds `graph_start`, `node_*`, `route` events and graph animation.
- 2026-09-23 `b8310c0` bind host made configurable so the dashboard can run in a container (`WAKU_DASHBOARD_HOST`).
- `dashboard.py` has 80 commits. Lesson: the core idea was stable (one observer, one animation table). The churn is in what surrounds it: tabs, hosting, encoding.

Pi relation: `lab/pi-agent/write-up.md:48-51` says all Pi faces "consume the same event stream - the loop never knows who's watching", and waku reads `pi --mode json`. `waku/tools/experimental.py:135-172` translates Pi `message_update`, `message_end` and `turn_end` into `subagent` events. So Waku already treats Pi's event stream as the model. AskCore copies that model; nothing in Waku needs to be ported.

## 7. Distillation for AskCore

### (a) Minimal event contract

Use Pi's names (AskCore: `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md:233-234`, H-MODE-04 and H-MODE-05). Do not use Waku's six kinds; they are Waku-specific (`gate`, `consolidation`).

Envelope for every event (fields added over Pi):

| Field | Why |
|---|---|
| `seq` (uint64, monotonic per daemon) | ordering, and resume after reconnect. Fixes Waku's line-count cursor |
| `ts` (UTC, ms) | display and latency |
| `sessionId`, `runId` | many sessions at once. Waku has neither |
| `type` | Pi event name |

Minimum set needed to light up a run:

| Event | Payload needed | Lights up |
|---|---|---|
| `agent_start`, `agent_end` | `agent_end` carries the outcome/error | run in and out |
| `turn_start`, `turn_end` | iteration number | each loop pass |
| `message_start`, `message_update` (delta only), `message_end` | `message_end` carries `usage` (in, out, cost), `model`, `provider`, stop reason | "agent reasons", tokens, cost |
| `tool_execution_start`, `tool_execution_end` | tool name, bounded args preview, bounded result preview, status | "tool runs" |

Later, optional: `agent_settled`, `queue_update`, `compaction_*`, `auto_retry_*`, and the extension events. Waku's `gate` maps to whatever context-assembly step Ask adds; do not invent an event for it now.

Rules:

- Delta only for `message_update`, from day 1. Cumulative messages grow quadratically (inventory-harness.md:234).
- `usage` on `message_end`, so cost needs no other source. Keep tokens as truth and compute money later, as Waku does (tracing.py:106-108).
- Bounded previews and redaction happen before publish. A subscriber never sees a raw secret.
- Publish must never block the loop. A slow subscriber loses events (a counter says how many) or is disconnected.
- The bus type lives in `pkg/protocol` because `cmd/tui` imports no `internal/*` package (AskCore: CLAUDE.md, Project Structure).

### (b) Where each piece goes

| Piece | Package | Note |
|---|---|---|
| Event structs, JSON names, envelope | `pkg/protocol` | shared wire contract; `cmd/tui` may import it |
| Publisher interface and in-process fan-out, bounded queue per subscriber, drop counter | `internal/bus` (`EventPublisher`, `Event`) | bus README lists events and denies `internal/agent`, so the agent depends on the bus interface, not the reverse (AskCore: internal/bus/README.md) |
| Emitters | `internal/agent` (turn, message, tool events); `internal/tools` executor wrapper | publish only; no knowledge of who listens |
| Spans: run -> turn -> tool attempt, previews, redaction | `internal/tracing` (`Collector`), fed by a bus subscriber | README already requires bounded previews and redaction; fixes Waku's point-event spans |
| OTLP export | `internal/tracing/otelexport` | replaces Waku's env-switched exporter |
| Event log for history and replay | `internal/store` interface, `gormstore` implementation; SQLite table keyed by `(sessionId, seq)` | optional; only if replay is wanted |
| Live push to clients | `internal/gateway` (WS RPC in `methods/` and/or `GET /events?after=<seq>` SSE), fed by the bus | `internal/realtime` denies other `internal/*` imports, so a bridge in `gateway` or `app` copies bus events into centrifuge |
| Wiring | `internal/app` (fx) | composition root |
| Inspector view | `cmd/tui` | subscribes over the gateway only |
| Hooks | `internal/hooks` async handlers | extension events reuse the same stream; do not build a second path |

### (c) Delivery options

| | A: web dashboard served by the daemon | B: TUI inspector in `cmd/tui` | C: OTel export to an existing viewer |
|---|---|---|---|
| What it is | Static page + SSE/WS from the daemon, like Waku | A bubbletea view: a run tree or a pipeline strip that highlights the active stage, with tokens and cost | `tracing/otelexport` to Phoenix, Jaeger, Langfuse |
| Fit with "web UI out of scope" | Conflict. `docs/ask-architecture-reference.md:9` and CLAUDE.md say front-end and web UI are out of scope | Fits. TUI is in scope | Fits. No UI built |
| Live "lights up" | Yes | Yes | No. Spans are exported in batches when they end |
| Build cost | High: assets, embed, auth token, CSS, tests | Medium: one bubbletea model, plus a WS or SSE client | Low: mostly the existing `tracing` work |
| New security surface | HTTP port with a UI. Needs token and Host/Origin checks (section 5) | None new. Uses the gateway that exists | None new; the viewer is a separate program |
| Maintenance | Frontend churn (Waku: 80 commits in `dashboard.py`) | Moves with the TUI | Nearly none |
| Reuses Waku code | Only ideas. The CSS/SVG/design files are brand-limited (section 8) | Only ideas | none |
| Teaches the harness | Yes, visually | Yes, in the terminal | Weak: a generic trace tree |

Ranking and recommendation:

1. B as the deliverable of this feature. It matches the user goal (learn how a harness works) with no scope change.
2. C as a by-product. `internal/tracing` and `otelexport` are already planned; do not make it a project of its own. State plainly that it is not live.
3. A only after the user decides to relax the web-UI rule. If it is ever built, serve a tiny embedded page from `internal/gateway`, reuse the same `GET /events` SSE feed as B, and bind to loopback with a random token. The event contract in (a) makes A a thin add-on, so deferring loses nothing.

### (d) Roadmap phase and dependencies

The roadmap file is not yet written (plan.md:53 shows phase 2 in progress). So there is no phase number to cite. Placement:

- Depends on, in order: (1) the agent loop with typed events; (2) H-MODE-04 and H-MODE-05, the event stream and delta-only `message_update` (P0 in the inventory); (3) `internal/bus` publisher with bounded subscribers; (4) a gateway path to stream events (WS RPC or SSE); (5) `internal/tracing` collector with redaction (H-TELEM-04 is P1, inventory-harness.md:284).
- Place it as the last harness phase, right after the event stream and the gateway exist and before the extension phases. Reason: extensions add 41 events on the same stream, so the inspector should exist to debug them. It is a P1 feature and must not block P0 items.
- The bus, the event contract and the redaction rule are P0 work in effect, because changing the envelope later breaks every subscriber. Add `seq`, `sessionId`, `runId` when H-MODE-04 is first written.

### (e) License notes

- `LICENSE`: MIT, "Copyright (c) 2026 Sean Chen (ShenSeanChen)". MIT covers the source code.
- `LICENSE-BRAND` (AutoManus Technologies, Inc., all rights reserved) excludes these from MIT: `waku/ops/static/design/` (design system: tokens, controls, type), `waku/ops/static/waku-mark.svg`, the same two under `hosted/gateway/static/`, `docs/brand/`, and the names "Waku" and "Waku Memory" and the mark. It limits names, logos and the design system (the UI look and its CSS files). You may run, build and redistribute with the files unmodified. You may not use the files, names or mark in another product without written permission, and may not suggest endorsement.
- It does not limit the idea, the event model, or a new implementation in Go.
- `hosted/` is under Elastic License 2.0 (`hosted/LICENSE`). Do not copy from it.
- Fonts in `waku/ops/static/fonts/` are SIL OFL 1.1 (separate).
- Practical rules for AskCore: write the Go code from this description; copy no file from `waku/ops/static/` (the diagram SVG, `diagram.js` and CSS sit beside the brand files and use them); do not use the name "Waku" or its look; make our own tokens and styles. MIT would only require a copyright notice if we copied substantial code, and we should not. A one-line credit in docs is optional courtesy. Confidence on the license reading: about 85%. This is not legal advice.

## 8. What to copy, what to fix

| Waku idea | Take? |
|---|---|
| One observer, loop unaware of listeners | Yes: bus publish |
| Stage table mapping event to diagram node, with a hold time and a queue so fast turns are visible | Yes for the inspector: a queue with minimum display time per stage |
| Hang detection (`turn_start` without `turn_end`) | Yes: derive from `agent_start` without `agent_end` |
| Permanent token ledger separate from traces | Yes, if cost reporting is wanted |
| Line-count cursor over a daily file | No: use `seq` |
| Full args/output in traces, no redaction | No |
| Per-event file open/append, synchronous | No |
| Point-event spans | No: real durations |
| Node-id strings duplicated in JS and SVG with no test | No: derive from one table and test it |

## 9. Unresolved questions

1. Does the user keep "web UI out of scope" (docs/ask-architecture-reference.md:9)? If yes, this report needs no change: choose B and C. If the user wants Option A, that is a scope change and needs an explicit decision. I did not reverse the rule.
2. Should the event log be stored (SQLite, replay of past runs), or is live-only plus OTel enough? Waku has no real replay, so nothing here forces it.
3. What does Ask's roadmap phase order look like once `roadmap` is written? Section 7(d) gives the dependency chain only.
4. Redaction policy: which fields are secrets (env values, provider keys in tool args)? This should be decided in `internal/tracing`, not in each emitter.
5. Midnight-rollover bug in Waku is inferred from code; not reproduced. It does not affect our design.

Status: DONE
Summary: Waku's feature is a single observer stream feeding a JSONL trace and a polling dashboard that animates a diagram. The report gives the event contract, package placement, three delivery options with a recommendation (TUI inspector first), roadmap dependencies, and license limits.
Concerns: The roadmap phase list does not exist yet, so the phase is given as a dependency chain. The web-UI option is left for the user to decide.
