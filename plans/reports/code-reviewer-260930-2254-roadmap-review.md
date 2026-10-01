# Review: `roadmap.md` (Pi harness rebuild in Go)

Date: 2026-09-30. Reviewer: code-reviewer. Document: `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` (417 lines).
Checked against: `plan.md`, `inventory-harness.md` (203 H-* rows), `inventory-extensions.md`, `inventory-tui.md`, `timeline.md`, the scaffold `plan.md`, 14 `internal/*/README.md` files, `docs/ask-architecture-reference.md`, `.golangci.yml`, `internal/config/config.go`, the edge-case and waku reports, and Pi source at `2bbfcca43` (0.99.1).
Method: the P0 and P1 coverage was computed by a script that parses the inventory tier column and every `H-XXX-NN` / `H-XXX-NN..MM` reference in the roadmap, grouped by the section heading. The script handles the escaped `\|` in H-TOOL-01. The Pi paths were checked with `os.path.exists`, and the line ranges were checked with `sed`.

## Verdict

The roadmap is NOT acceptable as written. It fails its own acceptance bullet 1 (P0 coverage). It opens an unauthenticated remote shell in H10. H9b and D13 are stale, and their phase position conflicts with the finished waku report. The phase order is mostly sound. The concept-per-phase format is good. No removed Pi item is rebuilt by mistake, but the hook-deadline rows need an explicit exception (see M9).

---

## Blockers

### B1. H10 exposes `prompt` (and so `bash`) over the network with no authentication

- Evidence: `internal/config/config.go:40,45` sets the default `host` to `0.0.0.0` and `cors_origin` to `*`. H10 builds "the P0 commands: prompting, state, model, compact" over gRPC and WS. H-MODE-09 lists "no ... auth" as a Pi gap that Ask must close. H10's build list and exit do not include authentication. No other phase adds it either. `internal/permissions/README.md` says "Authentication -> `internal/gateway`". The waku report (section 5) warns that a loopback HTTP port without a token can be driven from any web page (DNS rebinding or a cross-origin POST).
- Impact: any host on the LAN, or any browser page through WS, can run shell commands as the daemon user.
- Fix: add to H10 P0: (a) the daemon binds to loopback by default for agent methods; (b) a client token (a file with mode 0600 in the Ask config dir), or mTLS for cloud mode, checked on every gRPC and WS connection; (c) a WS `Origin` allow-list, with no `*`. Add the exit test "an unauthenticated client is rejected; a cross-origin WS upgrade is rejected". State this under D2 too: if print and JSON mode run through `cmd/server` (option A), H2 must guarantee that `-p` and `--mode json` start no HTTP or gRPC listener.

### B2. Acceptance bullet 1 ("every P0 item in exactly one phase") is false

There are 110 P0 rows (tier starts with `P0`). The script found these failures:

| Row | Problem | Fix (target phase) |
|---|---|---|
| H-LOOP-14 (loop hooks, core set P0) | Only in the Phase 0 table, not in any H phase. H2 needs `transformContext`, `convertToLlm`, `prepareRequest` and `finishTurn`. H3 needs `getApiKey`. H8 needs `prepareRequest` (H-COMPACT-02). | H2, core set |
| H-TOOL-21 (pairing invariant) | Only cited in section 3 prose. H2's test cites E§24#1, but no build line owns the row. The "synthesize missing results" part is in `transform-messages.ts` (provider side). | H2 (loop guarantee) + H3 (replay synthesis, next to H-PROV-10) |
| H-PROV-26 (quirk data, chosen vendors) | Not referenced. | H3 |
| H-CONF-14 (config files) | Not referenced. | H5, together with the credential-storage decision (see M3) |
| H-SLASH-01 / 02 / 03 (`/compact`, `/new`, `/model` `/thinking`) | Not referenced. They are covered only implicitly through H-COMPACT-04 and H-MODE-07. | Name them in H8 and H10. `/new` needs H-SESS-18 (see M6). |
| H-SEC-03 (no-permission stance, "P0 (decision)") | Only in D4. H4 never says that it waits on D4. | Name it in H4 with "waits on D4" |
| H-RETRY-07 | Assigned twice: H1 (`Usage` type) and H7 (cost with tiers). | H1 "type only", H7 owns the row |
| H-SESS-04, H-SESS-05 | Assigned twice: H1 and H6. | H1 owns the types; H6 references "persisted as-is", not a second delivery |

### B3. H9b and D13 are stale and in the wrong position

- `researcher-260930-2254-waku-watch-it-think.md` was written at 23:28:34, before `roadmap.md` (23:29:30). The roadmap still says "Pending the report".
- The report recommends option B (a TUI inspector in `cmd/tui`) and option C (OTel, as a by-product). Option B needs the gateway event stream (H10) and a TUI (T1). The roadmap puts H9b before H10 and lists only H1, H2 and H9 as its dependencies. This is a forward dependency.
- Report 7(d): add the envelope fields `seq`, `sessionId` and `runId` "when H-MODE-04 is first written", because a later envelope change breaks every subscriber. H1 does not have them.
- Option A (a web dashboard) conflicts with a confirmed non-goal: scaffold `plan.md` decision 0c ("Out of scope: Front-end and web UI") and `docs/ask-architecture-reference.md:9`. The roadmap lists option A as a neutral choice.
- Fix: (1) add the envelope fields (`seq`, `ts`, `sessionId`, `runId`) to H1 and H-MODE-04; (2) split H9b into two parts. H9b-core (after H10) delivers the bus fan-out with bounded, droppable subscribers, a drop counter, redaction before publish, and `GET events?after=seq` or a WS stream. The inspector view goes into T1 or T2. (3) Rewrite D13 with the user's original decision verbatim ("Add one feature from waku-agent ... A local dashboard lights up ...") next to decision 0c, and ask explicitly: B+C, or relax the web-UI non-goal for A.

---

## Major

### M1. Hidden forward dependencies (check 2)

| Phase | Uses | Built in | Fix |
|---|---|---|---|
| H1 | `agent_settled` | This is a session-level event (`C:core/agent-session.ts:197,1042`), not in `A:types.ts:514-529`. Its meaning ("no automatic work remains") exists only after H7 and H8. | Define two event layers: agent-core events (H1) and session events (`agent_settled`, `queue_update`, `compaction_*`, `auto_retry_*`, `entry_appended`), each added in the phase that emits it. Split H-MODE-04 the same way. |
| H2 | JSON mode "first record is the session header" (H-MODE-03); print mode "auto-saves session" (H-MODE-02) | H6 | State that H2 is in-memory (`--no-session` semantics) and that the header record is added in H6. |
| H2 | `Agent` wrapper queues (H-LOOP-17) | H7 | H2 delivers only the state, the listeners and `WaitForIdle`. The queues come in H7. |
| H2 | Agent state is the history until H6 | This is the pattern removed in 0.87.0 (inventory section 15) | Give the loop a context-source interface in H2 (an in-memory log). H6 then swaps in the session projection without a rewrite. |
| H2, H3 | CLI flags `-p`, `--mode`, `--provider`, `--model`, `--api-key`, `--thinking` (H-CONF-09) | H5 | Split H-CONF-09 per phase, or move basic flag parsing to H2. |
| H3 | Config root (H-CONF-12), `models.json` and `auth.json` locations (H-CONF-14), the `$ENV`/`!cmd` resolver (E§24#22 is an H5 test), `httpIdleTimeoutMs` (H-CONF-06), `defaultModel`/`defaultThinkingLevel` (H-CONF-03), "model restored from session" (H-PROV-19), resume fallback (H-PROV-21) | H5 and H6 | Move catalog, resolution and credentials after H5 (see M5). Change the H3 exit "the same session can switch" to "the same in-memory conversation can switch". |
| H4 | `defaultTools` (H-TOOL-10), `shellPath`/`shellCommandPrefix` (H-TOOL-22), `images.*` (H-CONF-05) | H5 | Move H5 settings before H4, or use hard-coded defaults in H4 and wire them in H5. |
| H5 | Input transform chain (H-PROMPT-08): `input` hook, `before_agent_start` | H9 | Declare the hook slots as no-op stubs in H5. |
| H7 | Test "A 429 never triggers compaction" | Compaction is built in H8 | In H7, test "429 is not classified as overflow". Move the compaction assertion to H8. |
| H8 | Summarization retry (H-RETRY-05, P1, not placed); "RPC call" for manual compaction | H10 | Pull H-RETRY-05 into H8. H8 exposes `Session.Compact()`; H10 wires the RPC. |
| H8 | Compaction entry "carries a checkpoint of prompt sections and tool declarations" (H-COMPACT-08) | Nothing stores the prompt or tool declarations in the log (H-LOOP-15, P1, not placed; timeline section 6 "Transcript-canonical: Follow") | Put the system-section and tool-declaration entries into the H6 schema (promote H-LOOP-15 and say so). |
| H9 | `user_bash` fail-closed | `!cmd` is built in H10 (H-TOOL-24) | Build only the event type and the policy in H9. Test `user_bash` in H10. |
| H10 exit | `fork`, `resume` over RPC | H-SESS-09 is P1 in the same phase; `switch_session` needs H-SESS-18 (P1, not placed) | See M6. |

The six required points pass: faux and partial JSON (H1) come before the loop (H2); the session log (H6) comes before retry (H7) and compaction (H8); events (H1) come before JSON mode (H2) and the gateway (H10); the trust gate comes before project skills (both in H5, with a test); the bus (H9) comes before X1 and H9b; T1 comes after H10.

### M2. The decision table breaks its own rule "ordered by what they block"

| Decision | Stated block | Real first block | Problem |
|---|---|---|---|
| D5 (jitter) | H7 | H7 | Listed before D6, which blocks H6. |
| D6 (crash-resume) | H6 | H6 (schema) | Must come before D5. |
| D12 (Windows) | T1, H4 | H4 | Listed 12th. It must come right after D4. |
| D9 (deadline) | X1 | Phase 0 (the hooks README row) and H9 (its build list) | Must come before H9, after D6. |
| D4 (permissions) | H4 | H4, plus Phase 0 (the `permissions` README) and H9 (option B handler) | H4's body never says that it waits on D4. |
| D7 (Anthropic OAuth) | H10 | H3 as well: H-AUTH-01 precedence and the `auth.json` schema include OAuth | State the H3 impact, or fix precedence for "API keys only" in H3. |
| D13 (waku view) | H9b | H9b, or T1 after the B3 split | Listed last, after the X1 and T1 decisions. |

Correct order: D1, D2, D3, D12, D4, D6, D5, D9, D13, D7, D8, D10, D11. Add "Waits on Dn" as the first line of each phase that is blocked.

### M3. A decision is missing: credential and settings storage

- H3 says "`auth.json` with a cross-process lock and read-merge-write" and also "`store` (credentials for a multi-tenant daemon)".
- `internal/providers/README.md` says "API keys at rest -> `internal/crypto` + `internal/store`". `internal/config/README.md` says "Secrets in files -> environment variables only".
- H5 puts Pi's per-project, trust-gated, writable `settings.json` into `config`. The depguard rule `config-only-in-root` (`.golangci.yml`) allows only `internal/app` and `cmd/*` to import `config`. The agent cannot read per-cwd project settings at run time through `config`.
- Fix: add a decision D3b (blocks H3 and H5): the credential store (file with a lock, or a SQLite row with `crypto`), and where runtime-layered settings live (a new `settings` loader owned by `agent` or `workspace`, fed with typed defaults from `config`). Update the Phase 0 table.

### M4. Phase 0 misses real README conflicts

The four listed rows are correct against the README text (pipeline 8 stages; sessions key `agent:{agentId}:{channel}:direct:{peerId}`; scheduler `queue/followup/interrupt`; hooks "Timeouts are fail-closed"). Missing:

| Package | README text | Conflict with the roadmap |
|---|---|---|
| `bus` | "Inbound messages ... outbound ... events notify subscribers (for example cache invalidation)"; imports stdlib only | H9 makes it the 41-event fan-out with ordered, awaited handlers (H-LOOP-17) and a drop policy. The event types also live in `agent` and `pkg/protocol` (H1). One owner rule is needed: types in `pkg/protocol`, publisher in `bus`, sync dispatch in `hooks`. |
| `tools` / `permissions` | tools: "tool policy: allow, deny, approval (`policy.go`)"; permissions: "Role-based access ... Tool allow/deny policy -> `internal/tools`" | H5 puts the trust store in `permissions`. H9 and D4-B put the tool policy in `permissions`. Pick one home. The trust store is not RBAC. |
| `hooks`, `hooks/handlers` | hooks imports `store`, `tracing`; handlers import `hooks`, `crypto`, `sandbox` | A first-party `tool_call` handler needs the tool input and the content types, and possibly `permissions`. The allowed imports must change. |
| `sessions` | imports `store` only | The H6 context builder needs the message types (`providers`, per H1). |
| `memory` | "auto-injection into the prompt, flush at end of run" | Pi has no memory. Auto-injection breaks the byte-stable prompt (H-PROMPT-09 test in H5). The roadmap says nothing about this. State "parked", or "injects only as a named section outside the cached prefix". |
| `skills` | "search (BM25 and optional embeddings) and hot reload (`watcher.go`)" | Pi puts only metadata in the prompt and has no watcher (E-LD-11: explicit `/reload`). |
| `bootstrap` | "seeding for a new agent or user ... truncation" | Pi discovers `AGENTS.md` from the filesystem (cwd ancestors, H-PROMPT-04). Seeding has no Pi counterpart. |
| `workspace` | "resolver for each run kind: agent default, subagent, cron" | H4 and H5 need an explicit session cwd and a canonical project root for trust. The roadmap never names the owner. |
| `providers` | dewee `Provider`, `ProviderAdapter`, `ThinkingCapable`; "retry and error classification" | Pi splits Api, Provider and Model (H-PROV-01) with a compat record. Agent-level retry lives in the agent (H7). |
| `agent` | `Router` resolves an agent by key; the prompt builder uses "memory" | This is multi-agent routing; Pi has one agent per session. |
| Architecture doc | Sections 2 ("agent (Router, Loop) -> pipeline (stages)"), 3, 4 (`pipeline.Stage`), 9 ("A new pipeline stage"), 7.2 | The roadmap aligns only section 7.2. |

Also fix the D1 facts. "24 lines of `doc.go` in total" is true only if `store` is counted (6 x 4). `store` has real code (`user_store.go`, `post_store.go`), so "They have no code" is wrong for it. "Rewrite 4 READMEs" covers 6 packages, or 13+ with the rows above.

### M5. H3 and H10 are too big to be one learning step (check 6)

- H3 has 13 build lines, 3 wire APIs (1567 + 1726 + 415 lines in Pi), a catalog, resolution, a locked credential store and an optional `crypto`. Split it:
  - H3a: one adapter (Anthropic), the Model struct, an env key, compat as data, and the idle timeout. Exit: `-p` against Anthropic.
  - H3b: the second and third wire APIs, cross-provider replay and H-TOOL-21 synthesis, and the thinking clamp. Exit: switch models mid-conversation with thinking replay.
  - H3c: the catalog, `models.json`, resolution and credentials with a lock. Placing it after H5 also fixes part of M1.
- H10 has a P0 gateway plus 13 P1 items ("each is small" is wrong: MCP client, OAuth PKCE and device code, branch summarization). Its exit needs `fork` (P1). Split it:
  - H10: gateway P0 plus B1 auth. Exit: prompt, steer, abort, compact, `agent_settled` over WS and gRPC.
  - H11: the session tree (H-SESS-08, 09, 12, 18, 20, H-COMPACT-10). Exit: fork, clone, resume and branch summary over RPC.
  - H12: commands and resources (H-PROMPT-06, 07, H-TOOL-24, H-SLASH-08, 09).
  - H13: MCP (H-TOOL-18).
  - H14: auth, network and polish (H-AUTH-06, 07, 12, H-PKG-05, H-PROV-13, 20, H-TOOL-20).
- H5 combines two concepts (settings plus trust; prompt assembly plus context plus skills). Splitting it is optional.
- H9b has no Pi files (none apply), no tests and no exit. After B3 it has content from the waku report 7(a) and (b).

### M6. P1 scope: pulled forward silently, and 22 rows not placed

- Pulled forward, which contradicts roadmap line 18 ("P1 items go to H10 or later"): H-MODE-03 JSON mode (H2), H-LOOP-12 and H-COMPACT-12 (H9), H-SESS-01 and H-SESS-02 (H6), H-PROV-16 (H3, "bundled part" only). The dependency chain needs JSON mode in H2. Fix the rule, not the placement: "P1 items may be pulled forward when a P0 exit needs them; each is marked (P1, pulled)".
- Not placed anywhere (22 rows): H-LOOP-15, H-TOOL-15, H-PROMPT-03, H-PROV-23, H-AUTH-04, H-AUTH-10, H-RETRY-05, H-RETRY-10, H-COMPACT-14, H-SESS-11, H-SESS-16, H-SESS-17, H-SESS-18, H-CONF-07, H-MODE-12, H-SLASH-04, H-SLASH-06, H-SLASH-07, H-SLASH-10, H-SEC-06, H-SEC-07, H-TELEM-04. The P1 halves of split rows are also not placed: H-PROV-05 (Bedrock, Google, Mistral), H-PROV-14 (others), H-AUTH-05 (rest), H-SESS-10 (picker), H-MODE-07 (rest).
- Forced by other phases: H-LOOP-15 (H6, see M1), H-RETRY-05 (H8), H-SESS-18 (H10 and H11 `new_session`/`switch_session`), H-TELEM-04 (H9b-core, per the waku report), H-AUTH-10 (T1: T-CMD-10 `/login` is P0 in the TUI inventory).
- Fix: add a table in section 6 "P1 rows -> phase or deferred, with a reason".

### M7. T1 P0 depends on harness P1 rows

T-CMD-05 (`/session` stats, P0) needs H-SESS-20 (P1). T-CMD-10 (`/login`, P0) needs H-AUTH-10 and D7 (P1). T-CMD-05 `/resume` needs H-SESS-18. Fix: the T1 gate lists these harness rows by id. After the M5 split, T1 depends on H11 and H14, not only on H10.

### M8. D1 does not follow the user's rule for reversing a decision

D1 says "The dewee stages were defaults, not a user decision". Scaffold `plan.md:29` records the confirmed decision "B: dewee model (packages by capability) plus Ask import rules". Line 37 lists the unasked defaults (vendor placement, flat `tools`), and the stages are not among them. Option A ("`pipeline` ... goes away") removes a package from the confirmed layout. Fix: quote decision 0 verbatim. State that A changes part of it. Show the trade-off. Remove the unverified claim.

### M9. D9 per-call deadline versus the do-not-rebuild rows

`inventory-harness.md` section 15 ("No hook timeouts") and roadmap H7 ("Do not rebuild: Hook timeouts") conflict with H9 and Phase 0 ("Add a per-call deadline (D9)"). Extensions section 12 accepts a deadline for out-of-process hooks ("server context, no human at Ctrl+C"). Fix: move "Hook timeouts" out of H7. In H9, write "Deliberate departure from Pi 0.31.0: the deadline applies to tier B (X1) out-of-process calls. Expiry fails closed for `tool_call`/`user_bash` and is logged for the others. Compiled-in tier A handlers get no deadline." Then D9 blocks H9 only if tier A also gets a deadline.

---

## Minor

1. D12 cites "68 fixes (E§14)". E§14 is "Auth and OAuth". The 68 comes from E§3 row 19 (`researcher-...-pi-edge-cases.md:45`). Change it to E§3 #19.
2. H7 reads `:2317-2360` "(abort)". The range starts at `clearQueue()`, and `abort()` is at 2349. Use `:2317-2380` or label it "clearQueue and abort".
3. H1 read list: add `C:core/agent-session.ts:180-200` (session events). `A:types.ts:514-529` has only the 10 agent-core events.
4. T0: `inventory-tui.md` section 3 puts the gate harness in `cmd/tui/internal/testkit/`. The roadmap puts it in a scratch module outside the repo. The roadmap reason (no Go 1.26 bump) is valid. State the deviation and the later move. The roadmap also silently picks the raw-write fallback over the vendored fork (inventory-tui Q2). Add Q2 to D10.
5. H10 "OAuth flows (H-AUTH-06)" does not name the P1 subset (Anthropic, ChatGPT, Copilot). Exclude the deprecated legacy Codex (section 15) and Anthropic if D7 = A.
6. X1 does not provide a real tool `unregister` (extensions section 12, last row; E-LD-13). Add it.
7. H10 does not use the three server/client rules from timeline section 6 (fence stale frames after re-attach, no auto-replay after reconnect, one writer per session) or `seq` resume. Add them to H10 tests.
8. Timeline lesson 5 (one tool-context struct: ctx, cwd, signal, update callback) and lesson 6 (`SourceInfo`) belong in H2's `Tool` interface. The roadmap does not name them.
9. Name the Ask config dir and env prefix (`~/.ask`, `.ask/`, `ASK_*`) in H5. "`.pi`-style" in the H5 exit is ambiguous. Timeline lesson 15 says to decide names before the wire protocol ships.
10. X1, X2, X3, T1 and T2 have no exit criteria. X2 and X3 have no tests. This is outside acceptance section 7, but T1 is 75 P0 rows with no runnable exit.
11. H6: the "per-session lock" must work across processes, because a `-p` process and the daemon can share one SQLite file. Specify a SQLite lease row or `flock`. Test it with two processes (like E§24#20).
12. H2 test: E§24#1 requires pairing "even after ... compaction, branch". Add the compaction case to the H8 tests and the fork case to H11.

## Check 3: removed items

Scanned: `inventory-harness.md` section 15 (35 rows), `timeline.md` section 3 (56 rows), `inventory-extensions.md` section 12 (22 rows), and `inventory-tui.md` section 18 (20 rows). No phase builds a removed item. Only two items need an explicit note: the hook deadline (M9), and the temporary "agent state is history" in H2 (M1, row 4). The removed slash names are guarded in H10. The single-shape edit, `glob`/`think`, cumulative partials and proactive compaction are guarded.

## Check 6: learning quality per phase

| Phase | One concept | Pi files exist | Edge-case tests | Runnable exit | Note |
|---|---|---|---|---|---|
| H1 | yes | yes (all 5) | yes | unit test only | add session-event read (Minor 3) |
| H2 | yes | yes | yes | yes | |
| H3 | no (4 concepts) | yes (8) | yes | yes | split (M5) |
| H4 | yes | yes (11 tools files, shell, exec) | yes | yes | |
| H5 | two concepts | yes (9) | yes | yes | optional split |
| H6 | yes | yes | yes | yes | lock scope (Minor 11) |
| H7 | yes | yes | yes | yes | fix the 429 test (M1) |
| H8 | yes | yes | yes | yes | |
| H9 | yes | yes | yes | yes | |
| H9b | n/a | none | none | none | stale (B3) |
| H10 | no (14 items) | yes (4) | yes | yes, but needs P1 | split (M5) |

All 51 cited Pi paths exist. The script checked every `A:`, `AI:`, `C:` and `CD:` reference plus the 11 files under `C:core/tools/`.

## Check 7: factual spot checks

| # | Claim in roadmap | Source checked | Verdict |
|---|---|---|---|
| 1 | Pi 0.99.1, commit `2bbfcca4` | `git rev-parse` = `2bbfcca43`; `coding-agent/package.json` 0.99.1 | correct |
| 2 | 41 events at `C:core/extensions/types.ts:1545-1612` | 41 unique quoted event names in that range | correct |
| 3 | 19 sync / 22 notify | inventory-extensions section 4 summary lists 19 names | correct |
| 4 | RPC commands at `rpc-types.ts:22-74` (33, via H-MODE-07) | 33 unique `type:` literals in that range | correct |
| 5 | No jitter, `AI:utils/retry.ts:122-126` | `retryDelayMs` at 122; no random term; the "before jitter" comment at 114 is stale | correct |
| 6 | `A:agent-loop.ts` "about 900 lines"; loop at 163-330 | 940 lines; `runLoop` at 163 | correct |
| 7 | "Skip remaining tools" fixed in 0.58.4 | coding-agent CHANGELOG 0.58.4: "Fixed steering messages to wait until ... tool-call batch fully finishes" | correct |
| 8 | `shouldStopAfterTurn` removed 0.87.0 | agent CHANGELOG `[0.87.0]` section, "Removed `AgentOptions.shouldStopAfterTurn`" | correct |
| 9 | Line refs: prompt `:1883`, retry `:3611`, compaction `:2862`, write order `:1096` | `prompt()`, `_isRetryableError`, `_checkCompaction`, extension-then-listener emit | correct |
| 10 | `A:types.ts:514-529` has the H1 events including `agent_settled` | 10 agent-core events; `agent_settled` is at `agent-session.ts:197` | partly wrong (Minor 3) |
| 11 | "24 lines of `doc.go`", "no code" | 6 x 4 = 24 only with `store`, which has code | partly wrong (M4) |
| 12 | Windows "68 fixes (E§14)" | 68 is in E§3 row 19; E§14 is Auth | wrong citation (Minor 1) |
| 13 | `:2317-2360` = abort | starts at `clearQueue`; `abort()` at 2349 | imprecise (Minor 2) |
| 14 | bubbletea v2.0.10 needs Go 1.26.0; repo on v1.3.10 | inventory-tui section 1; `go.mod`: `go 1.25.10`, bubbletea v1.3.10 | correct |
| 15 | Fullscreen from 0.84.0, about 60 fixes | inventory-tui Q3 and section 0 | correct |

## Recommended actions (priority order)

1. B1: add gateway auth, a loopback default and an Origin check to H10. Guarantee that print mode starts no listeners.
2. B2: assign the 7 unassigned P0 rows and remove the 3 double assignments.
3. B3: add the envelope to H1. Split H9b (core after H10, view in T1). Rewrite D13 with both confirmed decisions quoted.
4. M2, M3: reorder D1 to D13, add D3b (credential and settings storage), and add "Waits on Dn" lines.
5. M1, M5: fix the forward dependencies by splitting H3 and H10 and moving settings before H3c and H4.
6. M4, M8, M9: extend the Phase 0 table, rewrite D1 with decision 0 quoted, and make the deadline exception explicit.
7. M6, M7: add a P1 placement table and a T1 gate that lists its harness rows.
8. Minor 1 to 12.

## Unresolved questions

1. Is multi-tenant (per-tenant credentials) a real Ask requirement, or only an inventory Go note? It decides between a file credential store and a store-backed one (M3).
2. Should tier A (compiled-in) hooks get a deadline at all, or only tier B (M9)?
3. Is T0's scratch-module location acceptable to the user, given that inventory-tui places the testkit in `cmd/tui/internal/`?

Status: DONE_WITH_CONCERNS
Summary: The review is complete. The roadmap fails its own P0-coverage criterion (7 rows missing, 3 assigned twice). It has an unauthenticated network shell in H10, and H9b and D13 are stale and in the wrong position. Its order is otherwise sound, and no removed Pi item is rebuilt.
Concerns: B1 is a security blocker for any gateway phase. M3 (credential and settings storage) is a missing user decision that blocks H3 and H5.

---

## Re-check of revision 2 (narrow: order, "Waits on", fixes)

Method: I read all phase bodies and re-ran the coverage script. All 110 P0 rows are placed. Every row that appears in more than one phase is a declared split (P0/P1 part, loop/replay part, or session part). No P1 row is unreferenced. B1, B2, B3, M3, M4, M7, M8 and all 12 minor items are applied correctly. D11 is recorded in `plan.md:62`, and `docs/ask-architecture-reference.md:9` is narrowed to match.

### (a) Dependency order: all 6 stated rules hold. Phase bodies still have these forward uses

| # | Phase | Uses | Built in | Fix |
|---|---|---|---|---|
| A1 | H3, H4, H5 | A model record (contextWindow, maxTokens, cost, thinkingLevelMap) and `provider/id` parsing for `--model` | Catalog H7 (H-PROV-16, H-PROV-19) | State "one hard-coded Anthropic/OpenAI model record until H7", as H5 does for settings. |
| A2 | H5 | Child env names (`PI_SESSION_ID`-style, H-TOOL-22) and the bash spill-file name | D5 names (blocks H6) | Add D5 to H5 "Waits on", or state neutral names for now. |
| A3 | H10 | H-COMPACT-03 emits `session_before_compact` during overflow recovery | Hooks H11 | Add a no-op stub note, as H6 has. |
| A4 | H13 | H-MODE-08 is owned whole, but `get_entries{since}` needs H14 and RPC `bash` through `user_bash` needs H15 | H14, H15 | Split H-MODE-08: core in H13, session cursor in H14, `bash` in H15. |
| A5 | H13 | `cycle_model` (P0 "model" group) cycles `enabledModels` | Scoped models H17 (H-PROV-20) | Say that H13 cycles all available models until H17. |
| A6 | T2 | "Same stage table as W1" and "the extension UI subset" | W1, X1 | Add W1 and X1 to T2's dependencies in the section 3 diagram and rules. |
| A7 | H13 | H-MODE-09 lists "settings changes" as a Pi RPC gap | No phase owns a settings-change method | Place it (H15 fits, with `/reload`) or defer it in section 6. |

### (b) "Waits on" versus the "Blocks" column

| # | Problem | Fix |
|---|---|---|
| W-1 | T1 waits on D6. The D6 Blocks column says only H5. | Add T1 to D6 Blocks. |
| W-2 | X1 waits on D10. The D10 Blocks column says only H11. | Add X1 to D10 Blocks. |
| W-3 | D11 blocks H12, W1 and T2, but none of them has a "Waits on: D11" line. | D11 is decided, so either clear its Blocks column or add "Waits on: D11 (decided)". |
| W-4 | The Phase 0 body needs D4 (trust-store home), D7 (tool-policy home, `permissions` import) and D10 (hooks README deadlines), but it waits only on D1. | Move D4, D7 and D10 before Phase 0, or make those README lines placeholders that H6, H5 and H11 finish. |
| W-5 | The table order breaks its own rule. D6 and D7 block H5 but come after D4 and D5 (H6). D12 blocks H7 but comes after D8 to D11 (H8 to H12). | Use this order: D1, D2, D3, D6, D7, D4, D5, D12, D8, D9, D10, D11, D13, D14, D15. This needs no renumbering if the table rows are just reordered. |
| W-6 | `plan.md:62` records the dashboard decision as "D13", but the roadmap now calls it D11. `plan.md:32` still has "Web UI (out of scope)" under Non-goals. | Fix the id, and narrow the non-goal line. |

### (c) Fixes that are missing or wrong

- **M2: partly wrong.** The order is still not "by phase blocked" (W-5), and three Waits/Blocks pairs do not match (W-1, W-2, W-3).
- **M5: incomplete.** The splits are good and every phase has an exit. But H14, H15, H16 and H17 have no Concept and no "Read in Pi" line, and H15 and H16 have no tests. This breaks the roadmap's own section 7 bullet 3. Suggested Pi reads: H14 `C:core/session-manager.ts:1579-1750`, `C:core/compaction/branch-summarization.ts`; H15 `CD:prompt-templates.md`, `C:core/agent-session.ts:3745-3843`; H16 `CD:mcp.md`; H17 `AI:auth/oauth/` and `CD:providers.md`.
- **M1: mostly applied.** The new forward uses are A1 to A7 above. A1 and A4 are the ones that change what a learner builds.
- **M9: applied, one ambiguity.** D10 gives tier A handlers no deadline, but the H11 build list (tier A) says "Deadlines per D10". Say that H11 builds only the deadline plumbing for X1, or remove the line.
- **M6: correct.** One side note: section 7's "47 P1 rows" figure was not re-counted here.

Re-check status: DONE_WITH_CONCERNS. No blocker is left. The open items are decision-table consistency (W-1 to W-6), four thin phases (M5), and seven small forward uses (A1 to A7).
