# Review: overflow detection and usage accounting (uncommitted, branch task/h9-plan)

Method: read diff plus new files; ran a throwaway table test through `ClassifyProvider` / `OverflowFlagVetoed` (deleted afterwards). `go build ./...` and `go vet` clean.

## Findings (ranked)

### M1 Medium - overflow patterns are too broad and ignore HTTP status
`internal/providers/overflow.go:36-41,43` (generic `too many tokens`, `token limit exceeded`, Copilot `exceeds the limit of \d+`) and `internal/providers/failure.go:190` (overflow is tested before 5xx and before any status gate).
Verified by probe:
- 400 `Your request exceeds the limit of 20 images` and 400 `array ... Request exceeds the limit of 128 items` -> `CONTEXT_WINDOW_EXCEEDED`.
- 400 `Too many tokens requested for output` -> overflow.
- 503 `Overloaded: too many tokens in flight` -> overflow (never reaches the SERVER class).
- 500 `Prompt exceeds max length` -> overflow.
Scenario: the user attaches 21 images. The run ends with cause `context_overflow`, and ask -p exits 1 with "Start a new session". The real fix is to remove images, so a new session does not help. A 5xx overload is not retried because it is classed as overflow.
Fix: gate the generic and Copilot patterns on status 400/413/0 (and never 5xx), and anchor the Copilot pattern on tokens.

### M2 Medium - throttle veto does not cover the case its comment claims
`internal/providers/overflow.go:62-79`. The veto list holds only `rate limit` and `too many requests`, plus the prefix `^(Throttling error|Service unavailable):`. The comment says `ThrottlingException: Too many tokens, please wait` is handled. Probe: status 0 with type `ThrottlingException` and message `Too many tokens, please wait before trying again.` -> `CONTEXT_WINDOW_EXCEEDED`, `OverflowFlagVetoed=false`. HTTP 429 is saved by the status ordering in ClassifyProvider, but an in-band throttle (status 0, as used by `fantasykit` in-band errors) is read as overflow and ends the run without a retry.
Fix: add `throttl` (type/code/message) to `overflowVetoPatterns`.

### M3 Medium - silent overflow turns a complete answer into a failed run
`internal/providers/overflow.go:121-124` with `internal/agent/loop_stage.go:212,222,284` and `cmd/tui/headless_print.go:63-66`.
The rule `StopStop && prompt > window` trusts `Model.ContextWindow`. If the window of a models.json row or the catalog is below the real limit of the endpoint (proxy, beta 1M window), every normal reply is flagged: the cycle ends as an error, queued steering/follow-up inputs are not claimed, and `ask -p` writes the full reply to stdout and then exits 1 with an "overflow" text. The exit code contract of `ask -p` changes for a successful reply on bad metadata. Intended by the design (reply kept), but there is no margin or opt-out, and the stderr text blames the provider. Tie this to the plan before rating it higher; I did not verify which catalog rows carry a low window.

### L1 Low - `pickTier` fallback can price short prompts with the long-context tier
`internal/providers/cost.go:107-125`. If a row has no tier with `Above == 0` (for example only `{Above: 200000}`), prompts at or below that value get the lowest tier, which is the long-context price. Before the change the cost was 0. Catalog and models.dev rows always carry an `Above 0` tier (`modelsdev_test.go:176-185`), so this is only reachable with an odd models.json override. Low.

### L2 Low - NoOutput can hide billed input after `message_start`
`internal/agent/lifecycle.go:190`, `internal/agent/agent.go:906`. A stream that dies after the provider sent input-token usage but before any content block has `Usage == nil` and `NoOutput == true`, so `FoldUsage` counts zero and `Complete` stays true. The totals can then undercount billed input tokens while claiming completeness. Only matters if the assembler keeps usage seen before finish, and I did not prove that it drops it. Check `a.SetUsage` timing for Anthropic `message_start`.

### L3 Low - failure path reads price state from the live config, not the attempt
`internal/agent/agent.go:904`: `CostUnknown: !a.cfg.Model.PriceKnown()` reads `a.cfg.Model` without `a.mu` (the `settledUsage` read at agent.go:808 does lock), and ignores the per-attempt `priceKnown` that `closeAttempt` captures. After a mid-run model change, a failed attempt gets the new model's price state. Only matters when `Usage != nil`, and the failure path always has a nil usage in `FoldUsage`, so the effect is limited to the ACP row field. The unsynchronized read is the same pattern as the neighbouring lines 850-858.

### L4 Low - TUI refetches usage on every empty-usage settle
`internal/tui/update_session.go:517-523`. `AgentSettled.Usage == nil` (a session with no model attempt, for example a slash-only run) triggers `usageCmd` as if the peer were old. One extra RPC per settle; no wrong state.

## Checked and found sound (no defect)
- Message usage versus AttemptSettled: the fold reads only AttemptSettled; no double count.
- `EstimateContext`: the message entry is committed before the settlement (`loop_stream.go:179`, `recover.go:100`), the anchor drops at a context edit, and an error reply counts 0 chars. A snapshot or model change after the anchor is not re-estimated; this is documented.
- TUI stale guard: `endRun` bumps `run.seq`, so a usage answer from a turn end cannot overwrite the totals of `agent_settled`; the `gen`, `session` and token checks hold.
- Veto versus SDK flag: 429 and 402 veto, and quota/rate-limit classes veto in `fantasykit/errors.go:104`. Anthropic `request_too_large` 413 stays INVALID_REQUEST (tests added).
- PriceKnown/faux free tier consistent with `modelsdev.go` and `model_config.go`.
- Exit codes: print mode overflow adds exit 1 with the new text; `--mode json` is untouched (cycle_end cause is the only new value; the codec carries `usage` with omitempty).

## Test run
`go test` over agent, sessions, providers, acp, protocol, tui, cmd/tui was started; it exceeded 120 s in the foreground. See the final reply for the result if it finished.

## Unresolved questions
- Are the context windows of the catalog rows trusted by design for the silent rule (M3)?
- Should a 5xx ever be classed as overflow (M1)?
