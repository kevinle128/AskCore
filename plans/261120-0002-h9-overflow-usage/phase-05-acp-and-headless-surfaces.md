---
phase: 5
title: "ACP and headless surfaces"
status: pending
priority: P1
effort: "6h"
dependencies: [4]
---

# Phase 5: ACP and headless surfaces

## Goal

`_ask/session/usage` returns the shared totals, the partial flags and the context estimate; ACP names an overflow with its own error kind; `ask -p` gives a clear overflow text and exit 1; `--mode json` carries the totals on `agent_settled`. The built `ask` proves each one against a scripted provider over real HTTP.

## Context

- ACP usage: `internal/acp/ask_methods.go:476-549`; types `pkg/protocol/acp.go:301-316`; error kinds `pkg/protocol/acp.go:318-354`; `runError` `internal/acp/errors.go:119-130`.
- Current model of a session: `Agent.State()` (`internal/agent/agent.go:617`), field `Model` (`internal/agent/types.go:115`).
- `ask -p`: `printReply` `cmd/tui/headless_print.go:17-48`; printer `cmd/tui/headless_wait.go:150-185`; the run subscribes to events at `cmd/tui/headless.go:684-690`.
- JSON mode streams every event (`cmd/tui/headless_json.go:16-29`), so `agent_settled.usage` from phase 4 reaches it with no change here.
- Built-binary harness: `providerFixture` (`cmd/tui/acp_e2e_test.go:1585`) answers the Anthropic wire over HTTPS; `builtAsk` (`cmd/tui/tools_e2e_test.go:1488`) runs the compiled `ask` with an isolated home. The model is `AnthropicSonnet46` (window 1 000 000, `internal/providers/catalog.go:157-159`). No production test seam.

## Requirements

1. `pkg/protocol/acp.go`: `ACPUsageResult` embeds `UsageTotals` (the JSON keys `total` and `complete` stay); `ACPUsageRow` gets `CostUnknown bool \`json:"costUnknown,omitempty"\``; new kind `ACPErrContextOverflow = "context_overflow"`.
2. `internal/acp/ask_methods.go`: `usage` uses `sessions.FoldUsage` and `sessions.EstimateContext`, and the window of the current model. Delete `usageResult`, `addUsage` and `addOptional`.
3. `internal/acp/errors.go`: `runError` returns kind `context_overflow` for code `CONTEXT_WINDOW_EXCEEDED`, with the text "the conversation is larger than the context window of the model". `stopReason` does not change (an error cycle is an error, not a stop reason). Consequence: for a silent overflow, `session/prompt` returns this error after the reply text has already streamed as `session/update` (the same as any error cycle today).
4. `ask -p`: the printer records the `cycle_end` of each run. When the run ended with cause `context_overflow`:
   - explicit overflow: stderr gets `Context window exceeded: <provider text>. Start a new session or choose a model with a larger context window.`; exit 1 (unchanged code).
   - silent overflow: stdout gets the reply text as today, then stderr gets `Context window exceeded: the provider reported more input tokens than the context window of the model. Start a new session or choose a model with a larger context window.`; exit 1.
   The texts are defaults that need approval (plan.md).
5. `--mode json`: no change to exit codes (0 on an assistant error, `docs/cli.md:64`).

## Files owned

- Modify: `pkg/protocol/acp.go`, `internal/acp/ask_methods.go`, `internal/acp/ask_methods_test.go`, `internal/acp/errors.go`, `internal/acp/errors_test.go`, `cmd/tui/headless.go`, `cmd/tui/headless_wait.go`, `cmd/tui/headless_print.go`, `cmd/tui/headless_print_test.go`
- Create: `cmd/tui/overflow_e2e_test.go`, `cmd/tui/usage_e2e_test.go`

## Tests written first

Built-binary tests (`builtAsk` + `providerFixture`):

- `TestPrintOverflowErrorExits1WithClearText`: 400 `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 1000500 tokens > 1000000 maximum"}}`; one request only (no retry); exit 1; stderr starts with `Context window exceeded:`.
- `TestPrintRateLimitWithOverflowWordingRetries`: a 429 whose body is the Anthropic form `prompt is too long: 1000500 tokens > 1000000 maximum`, which makes the fork set `ContextTooLargeErr` (`<fork>/providers/anthropic/error.go:140-149`), then a reply; two requests; exit 0. Without the veto of phase 1 this test fails, because the flag returns overflow before the status rule (`internal/providers/fantasykit/errors.go:102-104`).
- `TestPrintPayloadTooLargeIsNotOverflow`: 413 `request_too_large`; stderr has no `Context window exceeded`.
- `TestPrintQuotaIsNotOverflow`: 402 billing body; no retry; stderr has no overflow text.
- `TestPrintSilentOverflowPrintsReplyAndExits1`: 200 SSE with `input_tokens` 1 000 001 and a text reply; stdout has the text; stderr has the silent-overflow text; exit 1.
- `TestJSONOverflowCycleEndAndSettledUsage`: `--mode json` on the explicit case; `cycle_end` has `cause:"context_overflow"` and `code:"CONTEXT_WINDOW_EXCEEDED"`; `agent_settled.usage` is present; exit 0.
- `TestJSONTotalsAddRetriedAttempts`: a 500 with usage-less body, then a reply with usage; `agent_settled.usage.total` equals the reply usage and `complete` is true, because a refused attempt that streamed no output counts as zero (user, 2026-11-20); a run of two replies with usage gives the sum and `complete` true.
- `TestResumeKeepsUsageTotals`: run, then `ask -p -c --mode json`; the second `agent_settled.usage.total` is the sum of both runs.
- `TestJSONUnpricedModelIsCostIncomplete`: the Sonnet hand row (no price) gives `costComplete:false`.
- `TestJSONPricedModelShowsCumulativeCost`: `<ASK_HOME>/models.json` sets `providers.anthropic.modelOverrides.claude-sonnet-4-6.cost` with all four prices (an override that names every price makes the row priced, `internal/providers/model_config.go:350-362`); two runs with known usage give `costComplete:true` and `total.cost.total` equal to the sum of the two expected micro-USD costs. The fixture is an HTTPS proxy (`cmd/tui/acp_e2e_test.go:1687-1689`), so the override needs no `baseUrl`.
- `TestACPUsageReportsTotalsAndEstimate` on `ask acp` and through the leader: `_ask/session/usage` has `costComplete`, `contextTokens` above 0 and `contextWindow` 1 000 000; a prompt that overflows returns an error of kind `context_overflow`.

Unit: `printReply` rows for the two overflow forms (`headless_print_test.go`); `runError` row for the new kind.

## Steps

1. Write the tests; they fail.
2. Change the protocol types and the ACP method; delete the private fold.
3. Add the ACP error kind.
4. Add the cycle-end record to the printer and the overflow texts.
5. Run the validation, then the full suite.

## Validation

```
go build -o ask ./cmd/tui
go test ./internal/acp/... ./cmd/tui/... ./pkg/protocol/...
go test ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| An ACP client keyed on kind `internal` for overflow | L x L | the message text keeps the code; CHANGELOG line names the new kind |
| The silent case prints a reply and still exits 1, which a script may not expect | M x L | default needing approval; the stderr text says why |
| The fixture 200 SSE does not reach the silent rule because the adapter reads `input_tokens` from `message_delta` | L x M | send the count in `message_start` and in `message_delta`, as `textSSEWithUsage` does (`internal/providers/anthropic/provider_test.go:546`) |

## Rollback

Revert the commits. ACP returns to its private fold and kind `internal`; `ask -p` prints the provider text only.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
