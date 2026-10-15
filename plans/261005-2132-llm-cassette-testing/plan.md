# LLM cassette testing (record/replay at the HTTP layer)

Status: implemented, in review (approved by the user on 2026-10-05 through /ak-cook)
Research: `plans/reports/researcher-261005-2111-llm-record-replay.md` (Claude Code, Pi, Grok CLI)

## Outcome

Ask tests real provider code (HTTP, SSE parse, event mapping) without a live LLM.
A normal `go test ./...` is deterministic, makes no network calls and costs nothing.
A bug seen during real use can become a regression test from a captured session.

## Decisions (user-confirmed 2026-10-05)

| # | Decision |
|---|----------|
| D1 | Record at the HTTP layer (`http.RoundTripper`), not at `StreamFn`. |
| D2 | Use four test layers. 1: recorded cassettes. 2: the provider faux (exists). 3: hand-written error cassettes. 4: live smoke with `ASK_LIVE=1` (exists). |
| D3 | Use `go-vcr` v4. Do not write our own recorder. |
| D4 | Match requests in order. Compare the normalized body with the recorded body. On a mismatch, fail with a readable diff and the re-record command. |
| D5 | Replay-only is the default, locally and in CI. A missing cassette fails the test. Recording needs `ASK_RECORD=1`. |
| D6 | When `ASK_RECORD=1` is set, a request budget per run stops runaway loops. |
| D7 | Live smoke tests check properties only (stream ends in `done`, tool call is valid, usage > 0, exit 0). They never check exact text. |
| D8 | `ASK_CAPTURE=<dir>` writes a cassette from a real `ask -p` session. It is in scope now. |

## Non-goals

- Session to faux-script converter, and seeding a test with a stored session prefix. These need sessions on disk, which `internal/sessions` does not have yet (`internal/sessions/memory.go`, `MemoryLog` only).
- Replay of SSE timing. Chunking is covered by the faux tests.
- Cassettes for providers other than `tokenplan`.

## Current state (verified)

- `tokenplan.New` accepts `WithHTTPClient(*http.Client)` (`internal/providers/tokenplan/provider.go:89`). The provider wraps that client (`provider.go:229`).
- `ask -p` builds the provider in `tokenPlanStream` (`cmd/tui/headless_faux.go:84`). It does **not** pass an HTTP client yet, so there is no hook today.
- Live tests drive the real path through `newHeadlessAgent(options, getenv, tools...)` (`cmd/tui/headless_live_test.go:79`), gated by `ASK_LIVE=1`.
- Temperature and max_tokens come from `StreamOptions` (`internal/providers/tokenplan/document.go:42,69`).
- The `fantasy` SDK is a fork (`go.mod:402`). SSE parsing lives there, which is one more reason to record raw HTTP.

## Phases

### Phase 0: Check placement and wire format
- [x] The recorder package is `internal/providers/cassette`. It sits next to `faux/` and `sse/`. Depguard denies only fantasy and the Anthropic SDK outside `tokenplan/`.
- [x] Live probe: `ask` ran "1+1*2" twice with the math tools, and the request bodies were dumped and diffed. Results:
  - Request 1 was byte-identical across the two runs. The body keys are `max_tokens, messages, model, output_config, tools, stream`. The body has no timestamp, cwd, date or UUID, and no system prompt.
  - Requests 2 and later differed only in the content echoed back from the previous LLM reply (thinking, text, `tool_use` ids). On replay that content comes from the cassette, so it is deterministic.
  - **Change:** normalization is canonical JSON (sorted keys) only. The `[CWD]`/`[TIME]`/`[UUID]` rules are dropped, because there is nothing to replace. This stays one function, so a rule can be added later.
  - Temperature is not plumbed, and `max_tokens` comes from the model catalog. Recording relies on narrow prompts. Nothing is added to the request for recording, so record and replay send the same body.
- [x] Add `gopkg.in/dnaeon/go-vcr.v4` v4.0.7 to `go.mod`. Update the CLAUDE.md tech stack.

### Phase 1: Recorder package
- [x] `cassette.Transport(mode, path, opts)` returns an `http.RoundTripper` built on the go-vcr recorder.
  - Modes: `Replay` (default), `Record`, `Capture`.
  - Replay skips latency.
- [x] Normalizer: canonical JSON with sorted keys, used for comparison.
- [x] Order matcher: request N is matched to interaction N.
  - Method and URL path must match.
  - The normalized body must be equal. If not, the test fails with a unified diff and `ASK_RECORD=1 go test -run <Test> ./...`.
- [x] Redaction before save. Drop `Authorization`, `x-api-key`, `api-key`, `cookie` and `set-cookie`. Keep only an allowlist of request headers.
- [x] Record budget: `ASK_RECORD_MAX_REQUESTS`, default 10 per test. Going over it fails the test.
- [x] Unit tests use `httptest.Server`. They cover replay, mismatch diff, missing cassette, redaction and budget.

### Phase 2: Wire into the real path
- [x] `options.transport` (`http.RoundTripper`) is passed to `tokenPlanStream`, which uses `tokenplan.WithHTTPClient`. Tests set it directly.
  - `ASK_CAPTURE=<dir>` sets it in `ask -p` (`cmd/tui/capture.go`).
  - With nothing set, behaviour does not change.
  - Change from the draft: there is no `ASK_CASSETTE` env. Tests inject the transport, so the production binary has only the capture flag.
- [x] In replay, `useCassette` gives a placeholder key, so CI needs no secret.
- [x] Test helper `useCassette(t, model)`. The path is `cmd/tui/testdata/cassettes/<TestName>.yaml`.

### Phase 3: Layer-1 cassette tests (real `tokenplan`, through `newHeadlessAgent`)
Use the cheapest model, `temperature=0`, small `max_tokens`, and narrow prompts.
- [x] Plain text answer.
- [x] One tool call, then the final answer (math tools, 2 requests).
- [x] Several tool calls in one turn.
- [x] `--mode json`: assert the full event sequence.
- [x] Thinking content, if the model supports it.

### Phase 4: Layer-3 error tests (hand-written cassettes or `httptest`)
- [x] 429. The adapter has `MaxRetries(0)`, so the test checks one request, then exit 1. Retry does not exist yet.
- [x] 500.
- [x] SSE cut mid-stream: a stream with no terminal event, and a connection reset mid-body.
- [x] Bad JSON in one event.
- [x] ~~Slow response, then the idle timeout fires~~. This is not driven through `ask -p`, because the idle timeout is fixed at 5 min. `TestStreamIdleTimeoutAndActiveStream` already covers it at the provider level.
- [x] SIGINT mid-stream gives exit code 130.

### Phase 5: Capture flow (`ASK_CAPTURE`)
- [x] `ASK_CAPTURE=<dir> ask -p ...` writes `<dir>/<timestamp>.yaml`, normalized and redacted, and prints its path on stderr.
- [x] Documented flow: a bug in real use → capture → move the file to `testdata/cassettes/` → write the test → review the diff → commit.
- [x] Limit: tools run again on replay. A captured session must use deterministic tools or a fixed workspace (`testdata/workspace/`). Otherwise the body check fails.

### Phase 6: Guards and docs
- [x] A test scans every `testdata/cassettes/**`. It fails on auth headers or key-like strings.
- [x] Docs: `docs/testing-llm-cassettes.md`, `internal/providers/README.md` and `CLAUDE.md`.
- [x] Run `golangci-lint` and `go test ./...` with no network and no keys.

## Acceptance criteria

- `go test ./...` passes offline, with no API key and no `ASK_*` env set.
- Deleting a cassette makes its test fail with the re-record hint. It never makes a live call.
- Changing a tool description makes the matching cassette test fail with a body diff.
- `ASK_RECORD=1 go test -run TestX ./cmd/tui` rewrites only TestX's cassette, within the request budget.
- `ASK_CAPTURE=<dir> ask -p "..." --provider alibaba-token-plan` writes a cassette that replays green in a test.
- No cassette contains an auth header or key.
- Production `ask -p` with no `ASK_CASSETTE` or `ASK_CAPTURE` behaves exactly as before.

## Risks

| Risk | Mitigation |
|------|------------|
| The LLM answers differently when re-recording. | Narrow prompts and temperature 0. If the shape changes, re-record or fix the prompt. Never loosen asserts. |
| A secret leaks through tool output in a captured session. | Normalizer, scan test, and human review before commit. |
| The body changes every run (a field we did not find). | Phase 0 inspects a real body. The diff shows the field at once. |
| Env-driven transport in a production binary. | No env means no change. The capture mode is explicit. |

## Unresolved questions
None. Phase 0 closed both of them.
