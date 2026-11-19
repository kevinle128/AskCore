---
phase: 1
title: "Overflow classification"
status: pending
priority: P1
effort: "5h"
dependencies: []
---

# Phase 1: Overflow classification

## Goal

Every provider overflow error gets the code `CONTEXT_WINDOW_EXCEEDED`, no rate-limit or quota error gets it, a byte-size 413 stays `INVALID_REQUEST`, and one pure function tells a silent overflow from the usage of a reply.

## Context

- `ClassifyProvider` and the regex lists: `internal/providers/failure.go:176-235`.
- The SDK flag returns overflow before the classifier: `internal/providers/fantasykit/errors.go:82-111` (flag at `:102-104`). The fork sets the flag from text only (`<fork>/providers/openai/error.go:85-104`, `<fork>/providers/anthropic/error.go:140-149`).
- Sources (MIT): Pi `packages/ai/src/utils/overflow.ts:37-80,152-168` at `4c6fb7cfe`; omp `packages/ai/src/error/flags.ts:152-167,654-657,747-755` at `eb6132a38b`. Port the patterns and the rules, not the files.
- Rules: [design.md "Overflow rules"](design.md#overflow-rules).

## Requirements

1. New file `internal/providers/overflow.go` holds the overflow text rule: the five existing context regexes move here unchanged, plus the Pi token patterns, each with its provider in a comment. One comment above the list names the source repository, the commit and the MIT license. `request_too_large` is not in the list.
2. The non-overflow veto list (Pi): `rate limit`, `too many requests`, `^(Throttling error|Service unavailable):`. A veto match blocks an overflow text match.
3. `ClassifyProvider` keeps its order. The context rule runs only when no status, type, code or quota rule before it matched (true today) and when the veto list does not match.
4. 413 split: a 413 with token wording (an overflow text match) is overflow, also when it has byte wording too (omp: token evidence vetoes the payload flag, `flags.ts:162-166`). A 413 whose detail has only byte or payload wording (`request_too_large`, `payload too large`, `entity too large`, `maximum (size|number of bytes)`), or that has no body, stays `INVALID_REQUEST`.
5. `classifyProviderError` applies the status veto before it reads `pe.ContextTooLargeErr`: for status 429 or 402, or a detail that `ClassifyProvider` maps to `RATE_LIMIT` or `QUOTA`, the flag is ignored. One ordering, one place: the veto helper lives in `overflow.go` and both call sites use it.
6. `func UsageOverflow(m Model, msg protocol.AssistantMessage) bool` with the Pi thresholds: `StopStop` and prompt above the window; `StopLength`, output 0 and `prompt*100 >= window*99`. Prompt is `Input + CacheRead + CacheWrite`. A window of 0 or any other stop reason gives false.
7. No change to the retry list (`internal/pipeline/recovery.go:14-18`).

## Files owned

- Create: `internal/providers/overflow.go`, `internal/providers/overflow_test.go`, `internal/providers/fantasykit/errors_test.go`
- Modify: `internal/providers/failure.go`, `internal/providers/fantasykit/errors.go`, `internal/providers/failure_test.go`, `internal/providers/anthropic/failure_test.go`, `internal/providers/openai/failure_test.go`

## Tests written first

- `TestOverflowPatternsMatchProviderTexts`: one row for each Pi pattern with the sample text of its provider.
- `TestOverflowVetoKeepsRateLimit`: "ThrottlingException: Too many tokens, rate limit reached" is not overflow; a 400 with "too many requests" is not overflow.
- `TestStatus429WithOverflowWordingIsRateLimit` and `TestStatus402WithOverflowWordingIsQuota`, in `ClassifyProvider` and through `classifyProviderError` with `ContextTooLargeErr` set.
- `TestPayload413IsInvalidRequest` (`request_too_large`, bare 413), `TestToken413IsOverflow`, and `TestToken413WithByteWordingIsOverflow`.
- `TestNonOverflowTextStaysInvalidRequest`: keep the DeepSeek negatives ("input exceeds maximum allowed value", "context window size must be positive").
- `TestUsageOverflowThresholds`: the rows in [design.md test matrix](design.md#test-matrix).
- Adapter rows: Anthropic `prompt is too long: 250000 tokens > 200000 maximum` on 400 is overflow (existing `TestAnthropicContextOverflowIsContextWindow` stays); Anthropic usage limit on 429 stays quota (`TestAnthropicUsageLimitIsQuota`); OpenAI `context_length_exceeded` on 400 is overflow and on 429 is rate limit.

## Steps

1. Write the tests; run them; they fail.
2. Move the context regexes to `overflow.go`; add the patterns, the veto list and the attribution comment.
3. Add the 413 split and the shared veto helper; use it in `ClassifyProvider` and in `classifyProviderError`.
4. Add `UsageOverflow`.
5. Run the validation.

## Validation

```
go test ./internal/providers/...
go vet ./internal/providers/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/providers/...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| A broad pattern (`too many tokens`) matches a throttling text | M x H | veto list plus the status veto; dedicated test rows |
| The fork changes its flag rules on the next pin | L x M | the veto runs before the flag, so a new flag source cannot override a 429 or 402 |
| A 413 token overflow from a provider that sends no wording is missed | L x L | accepted: bare 413 is `INVALID_REQUEST` (default needing approval) |

## Rollback

Revert the commits. `failure.go` returns to the five regexes; no other package depends on `overflow.go` until phase 4.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages. The attribution comment names the source repository and license only.
