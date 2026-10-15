# LLM record/replay: what Claude Code does, and what Ask has

## Claude Code (from the installed binary)

The source is the Bun binary at `~/.bun/install/global/.../claude-code-darwin-arm64`. No public document confirms this design.

- **Layer.** The hook wraps the `queryModel` stream generator, above the SDK. What it records is the list of output events, not HTTP bytes. `countTokens` has its own keyed store.
- **Key.** The fixture name is `fixtures/<sha1(normalized content)[:6]>-<...>.json`, with one hash per message.
  - Only message content goes into the key. The system prompt, tools, model and params do not.
  - These attachment types are left out of the key: date, environment, model, instructions and session_context.
- **Normalization.** These values become placeholders: cwd → `[CWD]`, config home → `[CONFIG_HOME]`, `num_files`, `duration_ms`, `cost_usd`, base64 images → `[IMAGE_DATA]`. UUIDs and timestamps are masked in the token-count key. Tool-use input is normalized recursively.
- **Format.** Each fixture is JSON: `{input, output}`. Only `output` is read on replay. `input` exists so a reviewer can read diffs.
  - On replay, uuid, requestId and timestamp get fixed values.
  - `[CWD]` is turned back into the real path.
  - Usage and cost are counted again from the recorded data.
- **Modes.**
  - Replay is the default.
  - A missing fixture throws an error that says to record it with `CLAUDE_CODE_TEST_ALLOW_REAL_NETWORK=1` and commit the new fixture.
  - A network guard blocks every connection except loopback while tests run (`TestEgressBlockedError`).
  - The production build compiles all of this out.

## Ask today

- **HTTP hook:** `tokenplan.WithHTTPClient(*http.Client)` accepts a client, so a recording `RoundTripper` can go in there.
- **Stream hook:** `providers.StreamFn` / `agent.LoopConfig.Stream` (`internal/agent/loop_run.go:21`). The provider is picked in `cmd/tui/headless_faux.go:70`.
- **Values that change between runs:**
  - The user message timestamp (`cmd/tui/headless.go:221`).
  - The assistant timestamp. `WithNow` can inject a fixed one.
  - Tool call IDs, which come from the LLM.
  - The system prompt is assumed static, but this is not verified.
- **Tests:** faux tests always run. Live tests run only when `ASK_LIVE=1`.

## Unresolved questions
1. Record at the HTTP layer (raw SSE) or at the stream layer (events)?
2. What goes into the key: messages only, as Claude Code does, or also the system prompt and tools?
3. Record mode, and how CI behaves when a fixture is missing.
4. Where fixtures live, and how secrets are redacted.

## Claude Code source (gitnexus repo `claude-code`, `services/vcr.ts`)
- **Gate:** `shouldUseVCR()` at vcr.ts:23 is true when `NODE_ENV=test`. It is also true for staff builds when `FORCE_VCR` is set.
- **Wrappers:**
  - `withVCR` (vcr.ts:88) wraps non-streaming calls.
  - `withStreamingVCR` (vcr.ts:349) buffers the whole stream, then replays it. Event timing is not kept.
  - `withTokenCountVCR` (vcr.ts:382) wraps token counting.
  - `withFixture` (vcr.ts:39) is the generic helper.
  - Call sites are `services/api/claude.ts:770` and `services/tokenEstimation.ts:144`.
- **Record rule:**
  - Locally, a missing fixture leads to a live call, and the result is saved to disk.
  - In CI, a missing fixture throws `Fixture missing ... Re-run tests with VCR_RECORD=1, then commit` (vcr.ts:71, 133).
- **Key and normalization:** These match the earlier binary findings: per-message sha1 cut to 6 hex characters, and the rules at vcr.ts:291-347.
- **On replay:**
  - Each message gets a fresh `randomUUID`.
  - Cost is added again with `addCachedCostToTotalSessionCost`.
- **Not found in this snapshot:** the egress guard (`TestEgressBlockedError`), and any committed fixtures or test files. The installed binary is newer than this snapshot.

## Pi (gitnexus repo `pi`)
- Pi has no record/replay.
- Provider tests call the live API, and each suite is skipped when its key is missing (`describe.skipIf(!process.env.X_API_KEY)`).
- Agent-loop and e2e tests use the faux provider (`packages/ai/src/providers/faux.ts`), with queued responses (`setResponses`/`appendResponses`).
- Ask already follows this model.

## Grok CLI (gitnexus repo `grok-build`, Rust)
- Grok CLI has no recorded files and no RECORD env flag.
- Tests start a **mock inference HTTP server** (`xai-grok-test-support/src/mock_server.rs`).
  - That server generates SSE in the exact wire format (`sse.rs:74`).
  - The SSE comes from a DSL script written in code: `Conversation::nth(1).calls([...]).reply(...)` (`conversation_script.rs:66`).
- Matching:
  - Requests are matched by endpoint plus a body fragment (`InferenceRequestMatcher::foreground_containing`, `inference_override.rs:45`).
  - If no matcher fits, the next turn in the script is served.
  - Tool call IDs are fixed per conversation (`call_mock_1_1`).
- Failure injection is scripted, and can repeat on retry or end the stream (`conversation_replay.rs:142`).
- Plain HTTP calls are mocked with wiremock.

## Decision
- **The recording layer is HTTP (`RoundTripper`).** User chose option A on 2026-10-05.
