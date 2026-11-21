# H9 design detail

Companion of [plan.md](plan.md). Line citations are from commit `7f1b4a0` (branch `task/h9-plan`), checked with `rg -n` and `sed -n` on 2026-11-20. The fantasy fork is cited as `<fork>` = module `github.com/kevinle128/fantasy@v0.0.0-20261109011938-f9c19ad3b1c6` in the Go module cache.

This file is the technical design. The TUI design file that the `ask-tui-design` skill asks for is `tui-design.md` in this folder (phase 6), because this file already has the name `design.md`.

## Current state (verified)

### Overflow

| Fact | Evidence |
|---|---|
| The code `CONTEXT_WINDOW_EXCEEDED` exists | `internal/providers/failure.go:21` |
| `ClassifyProvider` order: auth, quota (402, quota text), rate limit (429), context text, 400/413 invalid, 5xx server, status 0 server | `internal/providers/failure.go:180-200` |
| Five context regexes; five quota regexes | `internal/providers/failure.go:202-216` |
| The SDK flag `ContextTooLargeErr` returns overflow before `ClassifyProvider` runs, so a 429 or 402 with matching text becomes overflow | `internal/providers/fantasykit/errors.go:102-104` (flag), `:109` (classifier) |
| The fork sets that flag from the message text only, not from the status | `<fork>/providers/anthropic/error.go:140-149`; `<fork>/providers/openai/error.go:85-104` |
| Overflow is never retried: the retry list is EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT | `internal/pipeline/recovery.go:14-18` |
| An error cycle carries the failure code; the TUI already has a `context` error kind and next action for the code | `internal/agent/lifecycle.go:133-135`; `internal/tui/translate_errors.go:17-30` |
| `cycle_end.cause` is set only for `aborted` today | `internal/agent/lifecycle.go:130-132`; doc `internal/sessions/entry.go:31-34` |
| A length stop maps to `max-tokens`; a length stop drops tool calls | `internal/agent/lifecycle.go:214-225`; `internal/agent/loop_stream.go:226-228` |
| `finishAttempt` blanks the content of an error reply, so a silent overflow that became a `StopError` would lose the reply text | `internal/agent/loop_stream.go:171-183` |
| A cut-off answer counts as a completed attempt | `internal/agent/lifecycle.go:202-212` |
| An error reply is not sent to the model again | `internal/providers/transform.go:329-330` |
| The turn decision ends the run for a failed message | `internal/agent/loop_stage.go:56-58,263-279` |
| The request model of an attempt is `l.cfg.Model` | `internal/agent/loop_stream.go:58-63` |
| ACP `Prompt` returns kind `internal` with "model request failed: CONTEXT_WINDOW_EXCEEDED" for an overflow; `stopReason` maps `error` to `end_turn` | `internal/acp/errors.go:119-130`; `internal/acp/agent.go:423-436` |
| `ask -p` exits 1 for an error reply and prints its error text; it reads only the last message | `cmd/tui/headless_print.go:17-48`; caller `cmd/tui/headless_wait.go:150-174` |

The cause word that H9 adds is redundant with `Code` for an explicit overflow. The user decision asks for a distinct cause, so H9 adds it; for a silent overflow the cause and the code are the only markers.

### Usage and price

| Fact | Evidence |
|---|---|
| `AttemptSettled.Usage` is nil when the provider sent no usage | `internal/sessions/entry.go:63-72`; `internal/agent/lifecycle.go:168-172,186-192` |
| The assistant message also carries usage (display only after H9) | `internal/agent/loop_stream.go:171-183`; `internal/tui/translate.go:499-521` |
| ACP totals sum `AttemptSettled.Usage`; `Complete` is false only when an attempt has no usage; `CostUnknown` is ignored | `internal/acp/ask_methods.go:476-538`; `pkg/protocol/acp.go:301-316` |
| `pickTier` gives a zero price when no tier has `Above < tokens` (no tiers, or zero prompt tokens) | `internal/providers/cost.go:102-112` |
| Only the OpenAI adapters call `CalculateCost` | `internal/providers/openai/usage.go:12-31`; `openai/completions.go:187`; `openai/responses.go:181` |
| The Anthropic adapter passes no `Usage` function, so every Anthropic-wire attempt has cost 0, also for a priced row | `internal/providers/anthropic/provider.go:314-321`; default copy `internal/providers/fantasykit/fold.go:270-280` |
| Hand rows `TokenPlanMessages`, `TokenPlanCompletions`, `AnthropicSonnet46`, `XAIGrok47` have no `Cost` and `CostUnknown` false | `internal/providers/catalog.go:37,53,157,164` |
| A hand row wins over the generated models.dev row, so those rows stay unpriced | `internal/providers/generated_catalog.go:16-40` |
| models.dev gives tiers or `CostUnknown` true; `models.json` sets one tier; a `models.json` model without `cost` is free (zero-price tier, Pi default) | `internal/providers/modelsdev.go:223,231-252`; `internal/providers/model_config.go:258-264,350-362`; `docs/cli.md:627` |
| Faux rows have no `Cost` | `internal/providers/faux/faux.go:170-189` |
| `internal/providers/acp` has no adapter code | `internal/providers/acp/` holds `README.md` and `doc.go` only |
| The reference docs already read "no tiers" as an unknown price | `cmd/tui/reference_docs_test.go:298-300` |
| Two `AttemptSettled` writers; the failure-step writer carries no usage | `internal/agent/lifecycle.go:168`; `internal/agent/agent.go:871-887` |
| `Model.CostUnknown` and `Model.ContextWindow` (0 = unknown) | `internal/providers/model.go:47-69` |
| The TUI computes the context figure on the client from the last reply usage | `internal/tui/update_session.go:469-498,521-526`; `internal/tui/footer.go:58-65` |
| The TUI client already has `Usage`, but no screen calls it | `internal/tui/client.go:157`; `internal/tuiclient/client.go:775-782`; leader route `internal/leader/policy.go:62` |
| The footer shows no session cost by design | `internal/tui/footer.go:10-18`; `docs/tui-architecture.md:93`; `docs/tui-style-guide.md:96`; `internal/tui/README.md:119-120` |
| `agent_settled` has no payload; the JSON stream ends with it | `pkg/protocol/events.go:82`; `pkg/protocol/codec.go:278-279`; `cmd/tui/headless_json.go:8-15`; emit `internal/agent/agent.go:788` |

### Session log

| Fact | Evidence |
|---|---|
| `attempt_settled` uses `plainKind`: data is decoded without `DisallowUnknownFields`, so an added field stays readable for an older build | `internal/sessions/codec.go:109-125,214-232` |
| `MessagesOf` applies `ContextEdit` entries | `internal/sessions/entry.go:335-360` |
| `sessions` may import only `pkg/protocol`, snowflake and the standard library, and owns projections | `internal/sessions/README.md` ("Package boundaries", "File names and imports") |
| `internal/tui` may import only `pkg/protocol`, `internal/tui` and `internal/tuiext` | `.golangci.yml:28-37` |

## Data flow

```
provider HTTP / SSE
  -> adapter (anthropic, openai) ---- usage + CalculateCost (all wires) ----+
  -> fantasykit.classify: status veto, then SDK flag, then ClassifyProvider |
  -> agent.streamAttempt (model = l.cfg.Model)                              |
       settleAndRecover: retry list unchanged (overflow never retried)      |
       closeAttempt: AttemptSettled{Usage, CostUnknown = !model.PriceKnown()}
       reasonStage: overflow = failure code CONTEXT_WINDOW_EXCEEDED
                              || providers.UsageOverflow(model, msg)        <- H10 seam
       decideStage: overflow -> cycle reason error, code, cause context_overflow, end run
       settle: AgentSettled{Usage: sessions.FoldUsage + sessions.EstimateContext}
  -> session log (JSONL)
readers (one fold, sessions.FoldUsage / sessions.EstimateContext):
  ACP _ask/session/usage  -> rows + totals + context estimate + window
  TUI footer              -> agent_settled.usage at settle; Client.Usage after turn_end and snapshot
  ask -p --mode json      -> agent_settled.usage
  ask -p                  -> CycleEnd cause -> overflow text, exit 1
```

## Interfaces (names may change in review)

```go
// internal/providers/overflow.go
func isOverflowText(detail string) bool             // Pi patterns minus byte-size wording, with the veto list
func UsageOverflow(m Model, msg protocol.AssistantMessage) bool
// stop:   prompt > window;  length: output == 0 && prompt*100 >= window*99
// prompt = Input + CacheRead + CacheWrite; window = m.ContextWindow; 0 window -> false

// internal/providers/model.go
func (m Model) PriceKnown() bool { return !m.CostUnknown && len(m.Cost.Tiers) > 0 }

// pkg/protocol/usage.go
type UsageTotals struct {
    Total         Usage `json:"total"`
    Complete      bool  `json:"complete"`      // every attempt reported usage
    CostComplete  bool  `json:"costComplete"`  // Complete, and every attempt had a known price
    ContextTokens int64 `json:"contextTokens,omitempty"` // estimate of the next request; 0 = unknown
    ContextWindow int   `json:"contextWindow,omitempty"` // window of the current model; 0 = unknown
}

// internal/sessions/usage.go
type AttemptUsage struct { AttemptID, CycleID, Outcome string; Usage *protocol.Usage; CostUnknown bool }
func FoldUsage(entries []Entry) (rows []AttemptUsage, totals protocol.UsageTotals) // ContextTokens/Window left 0

// internal/sessions/context_estimate.go
func EstimateContext(entries []Entry) (tokens int64, ok bool)

// internal/sessions/entry.go (additive field, no new entry type)
type AttemptSettled struct { ...; CostUnknown bool `json:"costUnknown,omitempty"` }

// pkg/protocol: AgentSettled{Envelope; Usage *UsageTotals `json:"usage,omitempty"`}
// pkg/protocol: ACPUsageResult{Attempts; UsageTotals (embedded, keeps "total" and "complete")}
//               ACPUsageRow{...; CostUnknown bool `json:"costUnknown,omitempty"`}
//               ACPErrContextOverflow ACPErrorKind = "context_overflow"
```

## Overflow rules

1. Status veto first: status 429 or 402, an error type `rate_limit_error`, an error code `rate_limit_exceeded` or a quota match is never overflow. This veto runs before the SDK flag in `classifyProviderError` and before the text rule in `ClassifyProvider`.
2. Non-overflow text veto (Pi): `rate limit`, `too many requests`, and the Bedrock `Throttling error:` / `Service unavailable:` prefixes block an overflow text match on any status.
3. Overflow text: the existing five regexes plus the Pi token patterns (`<pi>/packages/ai/src/utils/overflow.ts:37-63` at `4c6fb7cfe`), each with its provider in a comment, and an MIT attribution comment above the list. `request_too_large` is left out: it is a byte-size 413.
4. 413 split (omp `flags.ts:152-167,747-755` at `eb6132a38b`): a 413 with token wording is overflow, also when byte wording is present too (token evidence wins, as in omp); a 413 with only byte or payload wording (`request_too_large`, `payload too large`, `entity too large`, `maximum number of bytes`) or with no body stays `INVALID_REQUEST`. The Cerebras bare 400/413 rule is not taken (no Cerebras provider in Ask).
5. Silent overflow: `UsageOverflow` with the Pi thresholds. A `toolUse` stop is not checked (Pi).
6. Action: no retry and no model switch. The reply of a silent overflow stays in the log and in model history; the attempt stays `completed` with its usage. The cycle ends with reason `error`, code `CONTEXT_WINDOW_EXCEEDED` and cause `context_overflow`.

## Usage fold rules

- One row for each `AttemptSettled`, in log order, with the cycle of the last `CycleOpened`. `MessageEntry` usage is never read, so nothing counts twice.
- `Usage == nil` sets `Complete` and `CostComplete` false. `CostUnknown` sets `CostComplete` false; its tokens still add to `Total`.
- A log written before H9 has no `costUnknown` field, so its attempts count as priced. The CHANGELOG line says this.
- The fold covers the whole log, so totals survive resume and include failed and retried attempts.

## Context estimate rules (one number for the footer and for the H10 trigger)

- Anchor: the latest `AttemptSettled` with outcome `completed`, a non-nil usage, and a token sum above zero, that comes after the latest `ContextEdit`. Anchor tokens = `TotalTokens`, or `Input+Output+CacheRead+CacheWrite` when `TotalTokens` is 0 (Pi `calculateContextTokens`).
- Trailing part: `ceil(chars/4)` of each projected message after the anchor entry. Text, thinking text, tool-call name plus JSON arguments, tool-result text; an image counts as 4800 chars; an error reply counts 0.
- No anchor: `ceil(chars/4)` of the latest `SystemSnapshot` (prompt plus JSON of the tool declarations) plus all projected messages. An empty log gives `ok == false`.
- Known limits: a `SystemSnapshot` or model change after the anchor is not re-estimated; chars/4 under-counts CJK text and JSON. Both are accepted for H9 (no tokenizer, user decision).

## Extension trace: H10 compact-and-retry

`RecoverModel` runs only for a `StopError` reply (`internal/agent/recover.go:62`), so it never sees a silent overflow. The seam therefore differs by form:

| Part | H9 | H10 |
|---|---|---|
| Overflow signal | one turn-state flag set for every overflow form | reads the same flag |
| Explicit overflow (error reply) | `pipeline.RecoverModel` gets the failure; the default does not retry `CONTEXT_WINDOW_EXCEEDED` | adds the compact-and-retry in the recovery hook |
| Silent overflow (stop or length reply) | the turn-state flag in `reasonStage`/`decideStage` | acts on the flag before `decideStage` ends the run |
| Shrink proof | `sessions.EstimateContext` | retries only when the estimate after compaction is below the estimate of the failed request |
| `context_edit` writer | none | compaction writes it |
| Silent overflow | cycle ends with the overflow cause | compacts; retry per H10 decision |

## Test matrix

| Level | What | Where |
|---|---|---|
| Unit | Pi patterns, veto list, 429/402 veto on text and on the SDK flag, 413 split, quota stays quota | `internal/providers/overflow_test.go`, `failure_test.go`, `fantasykit/errors_test.go`, `anthropic/failure_test.go`, `openai/failure_test.go` |
| Unit | `UsageOverflow` thresholds: stop at window (no), above (yes); length output 0 at 99% (yes), at 98% (no); output > 0 (no); window 0 (no) | `internal/providers/overflow_test.go` |
| Unit | `pickTier` fallback, `PriceKnown`, Anthropic cost on a priced row, zero on an unpriced row | `internal/providers/cost_test.go`, `anthropic/usage_test.go` |
| Unit | Fold: retries add, failed attempt with usage counts, nil usage gives `Complete` false, `CostUnknown` gives `CostComplete` false, message usage ignored, old-log field absent | `internal/sessions/usage_test.go` |
| Unit | Estimate: anchor plus tail, error and aborted attempts skipped, `ContextEdit` after anchor drops it, no anchor uses snapshot, images, empty log | `internal/sessions/context_estimate_test.go` |
| Codec | `costUnknown` round-trips; an older-shape line without it decodes | `internal/sessions/codec_test.go` |
| Agent | silent overflow ends the cycle with reason, code and cause and keeps the reply; explicit overflow gets the cause; no retry entry; `agent_settled.usage` matches `FoldUsage`; estimate agrees with `rebuildRequest` of the next attempt; resume after an error reply keeps the anchor of the last good attempt | `internal/agent/overflow_test.go`, `settled_usage_test.go` |
| Built binary | 400 overflow, 429 with the Anthropic overflow wording (fails without the veto), 413 byte-size, silent overflow (200 with `input_tokens` above the window), quota 402; a priced row (`models.json` cost override) gives a non-zero cumulative cost; exit codes and stderr; JSON `cycle_end` and `agent_settled.usage`; retry then success totals = sum of both attempts; resume with `-c` keeps totals | `cmd/tui/overflow_e2e_test.go`, `cmd/tui/usage_e2e_test.go` (`providerFixture` `cmd/tui/acp_e2e_test.go:1585`, `builtAsk` `cmd/tui/tools_e2e_test.go:1488`) |
| ACP | `_ask/session/usage` fields and `context_overflow` kind on `ask acp` and on the leader | `cmd/tui/usage_e2e_test.go`, `internal/acp/ask_methods_test.go` |
| TUI | footer goldens with cost, partial cost, unknown price, and the server estimate; overflow block for a silent overflow; quota and retry banners unchanged | `internal/tui/footer_test.go`, new `internal/tui/usage_footer_test.go`, `cmd/tui/interactive_test.go` |

## Rollback

Each phase is a separate commit series. Reverting a phase restores the earlier behaviour: classification falls back to the five regexes, totals ignore `costUnknown` (old readers ignore the field), `agent_settled` loses its optional `usage`, and the TUI footer goes back to the client-side figure. The session format version stays 1 and no entry type is added, so a log written by H9 stays readable after a revert.

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| A quota or rate-limit error read as overflow | M x H | veto runs first on status, type, code and text, also before the SDK flag; table tests port Pi and omp veto cases |
| A new pattern matches a non-overflow 400 ("input exceeds maximum allowed value") | L x M | keep the DeepSeek negative cases already in `failure_test.go`; add each Pi negative case |
| Silent overflow false positive when a provider counts tokens across a server-side tool loop | L x M | only stop and length are checked; `toolUse` is not |
| A row whose `ContextWindow` is below the real window ends good large replies as overflow (exit 1 in `ask -p`); Pi only compacts in that case | M x M | `UsageOverflow` trusts the row; the user corrects it with a `models.json` `contextWindow` override; plan.md default 2 names it |
| Cache-write heavy prompt missed by the Pi sum | M x L | prompt sum includes `CacheWrite` (default needing approval) |
| A refused attempt (429 or 500 before any stream) wrongly makes the total partial | H x L | counted as zero when it streamed no output (user, 2026-11-20); test in phase 3 |
| A total that looks complete but is not, for logs written before H9 | M x L | CHANGELOG line; fields absent means priced |
| `agent_settled` payload breaks a strict JSON reader | L x M | additive optional field; the exact-JSON tests at `pkg/protocol/events_test.go:57` and `cmd/tui/headless_test.go:339` are updated on purpose |
| Footer too wide at 80 columns | M x L | the TUI design step fixes the give-way order before code |
