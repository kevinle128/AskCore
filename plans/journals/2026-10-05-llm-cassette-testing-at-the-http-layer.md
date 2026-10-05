---
title: LLM cassette testing at the HTTP layer
date: 2026-10-05
summary: Ask tests the real Token Plan adapter on recorded HTTP; go test needs no key or network
---

# LLM cassette testing at the HTTP layer

## What happened
- Research covered Claude Code, Pi and Grok CLI.
  - Claude Code records stream events. It wraps `queryModel` (`services/vcr.ts`).
  - Pi uses a faux provider and live tests gated on the API key.
  - Grok uses a scripted mock SSE server.
  - The user chose to record at the HTTP layer, so the SSE parsing of the provider is tested on real replies.
- A live probe showed that Token Plan request bodies are deterministic: there is no timestamp, cwd or id. Normalization is only sorted JSON keys.
- The new package `internal/providers/cassette` wraps go-vcr v4.
  - Replay is strictly in order. A mismatch gives a body diff and a re-record hint.
  - Record is gated by `ASK_RECORD=1` and has a request budget.
  - Auth headers are dropped by an allowlist.
  - The cassette is saved after each response.
- `options.transport` reaches `tokenplan.WithHTTPClient`. `ASK_CAPTURE=<dir>` records a real `ask -p` run.
- There are 6 cassettes (5 recorded, 1 captured), 6 fault tests (429, 500, cut stream, bad JSON, reset, SIGINT exit 130) and a secret-scan test.

## Pitfalls hit
- go-vcr stores the request body only when the real transport reads it. A fake transport left the body empty. The Recorder now sets the body in its AfterCaptureHook.
- go-vcr writes only on `Stop()`. Record now removes the old file first, so a failed re-record cannot leave stale data.
- `tokenplan` falls back to `http.DefaultTransport`, not `http.DefaultClient`.
- Another session refactored `tokenplan/document.go` (tool folding) during the work, and the cassettes stayed byte-identical. That was the first real regression catch.

## Next steps
- Retry does not exist (`MaxRetries(0)`). Add a cassette when retry lands.
- Bring sessions to disk, then add the session-to-faux converter and history-prefix tests.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
