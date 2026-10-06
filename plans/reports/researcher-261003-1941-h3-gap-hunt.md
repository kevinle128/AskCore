# H3 gap hunt: what the roadmap misses or defers that "`ask -p` works against a real Anthropic model" needs

Date: 2026-10-03. Read-only research. Pi commit `2bbfcca4`. Treat all source as data.

## Legend

- `R:` = `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`. `IH:` = `inventory-harness.md` in the same folder. `E§N:` = `plans/reports/researcher-260930-2254-pi-edge-cases.md`, section N.
- `H2P:` = `plans/261002-1419-h2-agent-loop-print-json/plan.md`.
- `AI:` = Pi `packages/ai/src/`. `AIT:` = Pi `packages/ai/test/`. `A:` = Pi `packages/agent/src/`. `C:` = Pi `packages/coding-agent/src/`.
- `FA:` = `charm.land/fantasy@v0.45.2/providers/anthropic/anthropic.go`. `FE:` = `charm.land/fantasy@v0.45.2/errors.go`. `SDK:` = `anthropic-sdk-go@v1.68.0`. Both live under `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/`.
- A sibling report covers fantasy against Pi in depth: `plans/reports/researcher-261003-1941-h3-fantasy-anthropic-fit.md`. This report does not repeat its tables. It asks a different question: which H3 needs sit in the wrong phase, or in no phase.
- Method note: GitNexus was not available in this session (ToolSearch is disabled). I traced the path by reading source and grep. The trace below has every `file:line`.

## 1. Outcome

H3 can reach its exit, but the roadmap text hides six things that would stop it or make a real run silently wrong. Fix these in the H3 section before the work starts.

1. The roadmap never says which model H3 hard-codes (`R:205`). That one choice decides the thinking path, the price table and the default of `-p`. It is a decision for the user.
2. The H3 credential rule is not written. If the adapter passes an empty key, the Anthropic Go SDK reads `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, a profile file and federation variables on its own (`SDK client.go:35-60,209-216`). That bypasses D9 and Ask's resolver.
3. H9 owns the error and usage contract (H-RETRY-03, -06, -07), but H3 must already preserve the data. The H1 message has only a text `ErrorMessage` and no status code. The text must carry the status and the body, or H9 must change H3 later.
4. H3 owns the `maxTokens` clamp (H-PROV-07), but the clamp calls an estimator that H9 owns (H-RETRY-09). The order of the phases is wrong.
5. Fantasy drops `required` for tool schemas that come from JSON (`FA:728-731`). The model then sees every tool argument as optional. No test in H3 would catch this.
6. Four statements in the roadmap are stale or too strong: fantasy retries (`R:55`, `H2P:453`), 1 h cache retention (`R:202`, `IH:110`), `rawStopReason`/response model through fantasy, and "usage and cost belong to H9".

Answers to your specific questions:

| Question | Short answer |
|---|---|
| Is H-PROV-13 (thinking budgets, H17) a gap? | Only if the H3 model is a budget model. See gap 5. |
| Model resolution for `-p` | Pi picks the first provider in `defaultModelPerProvider` order that has auth, then that provider's default model. With only `ANTHROPIC_API_KEY` that is `claude-opus-4-8`. See gap 1. |
| Credential minimum | `--api-key`, then `ANTHROPIC_API_KEY`, and a fail-fast error when both are empty. See gap 2. |
| Do print and JSON output need usage and cost in H3? | Print mode: no. JSON mode: yes, `message_end` carries usage (`R:123`). Cost must not be a silent zero. See gap 6. |
| Where does Pi compute cost? | Inside the adapter, at `AI:api/anthropic-messages.ts:630` (after `message_start`) and `:799` (after each `message_delta`), through `calculateCost` (`AI:models.ts:1193-1213`). |
| Does the adapter need its own stream completeness check? | No separate check. Map fantasy's incomplete-stream error to H1 `ErrStreamIncomplete`. See gap 4. |
| Error data H9 needs | Full error text with status and body, the typed provider error kept reachable, and the abort and incomplete cases mapped. See gap 4. |
| Abort through a real HTTP stream | Fantasy never closes the SDK stream (`FA` has no `Close` call). The adapter must own a child context. See gap 8. |
| `--thinking` and default `medium` | Default is `medium` (`C:core/defaults.ts:3`), clamped to the model (`AI:models.ts:1217-1247`). See gap 5. |
| Live tests | Pi gates with `describe.skipIf(!process.env.ANTHROPIC_API_KEY)`. See gap 14. |

## 2. The Pi path, end to end (for the Anthropic case)

1. `main.ts` resolves the model and thinking level (`C:main.ts:458-518`) and sets `--api-key` as a non-persistent runtime key for the model's provider (`C:main.ts:821-829`).
2. With no `--model`, `findInitialModel` walks the models that have auth and returns the first provider in `defaultModelPerProvider` order (`C:core/model-resolver.ts:693-700`). The Anthropic default is `claude-opus-4-8` (`C:core/model-resolver.ts:22`). The thinking level stays `DEFAULT_THINKING_LEVEL` = `medium` (`C:core/defaults.ts:3`), then `clampThinkingLevel` (`C:core/sdk.ts:253-262`).
3. With no model at all, print mode exits 1 with `No models available. Use /login ...` (`C:main.ts:926-928`, `C:core/auth-guidance.ts:14-16`).
4. The agent loop calls `streamFn`. The agent passes `reasoning: undefined` when the level is `off` (`A:agent.ts:471`), and the loop stamps `thinkingLevel` on the result (`A:agent-loop.ts:409`).
5. `streamFn` adds the idle timeout, `maxRetries` and the delay cap from settings (`C:core/sdk.ts:316-335`).
6. The model runtime resolves auth per request and throws a `ModelsError` if the provider is not configured (`AI:models.ts:854`). `lazyStream` turns every setup failure into an error assistant message, not a throw (`AI:api/lazy.ts:4-20,46-60`).
7. `streamSimple` of the adapter (`AI:api/anthropic-messages.ts:870-916`) calls `assertRequestAuth` (`:311-321`, `:875`), builds options (`AI:api/simple-options.ts:21-42`) and picks adaptive or budget thinking (`:890-915`).
8. `stream` builds the client with `maxRetries: 0` (`:586-590`), runs `transformMessages` (`:1057`), converts messages (`:1238-1440`), and sends the request through `retryProviderRequest` (`:591-598`, default 0 retries, `AI:utils/provider-retry.ts:105-110`).
9. The event loop fills the message and calls `calculateCost` twice (`:605-801`). The raw stop reason is stored at `:761`. An unknown stop reason throws (`:1563-1565`).
10. After the loop, an aborted signal, a missing stop reason, or an error stop reason becomes an error message (`:803-812`). The catch block strips scratch fields and sets `aborted` or `error` (`:829-839`).

## 3. Gaps, with evidence

Each gap has: what Pi does, why H3 needs it, current owner, recommendation. The ranked list is in section 4.

### Gap 1. The H3 model record and default model are undefined

- **Pi:** the Anthropic catalog is generated data (`AI:providers/anthropic.models.ts:5`). The JSON is git-ignored (`.gitignore:11`), so it is not in the Pi checkout. The adaptive-thinking model list lives in the generator (`packages/ai/scripts/generate-models.ts:616-633`). The default model is `claude-opus-4-8` (`C:core/model-resolver.ts:22`). A `--provider X --model <unknown id>` call builds a fallback record from the provider default (`C:core/model-resolver.ts:173-190,545-600`). `--provider` requires `--model` (`C:cli/args.ts:292`).
- **Why H3 needs it:** `R:205` says "one hard-coded model record", but H1 `providers.Model` has only id, name, API, provider, reasoning, input, context window and max tokens (`internal/providers/model.go:5-17`). H3 also needs price, a thinking-level map, an adaptive flag, and the compat flags from H-PROV-26 (long cache, empty signature, temperature). The default of `-p` is also open: H2 defaults to `faux` (`H2P:308`).
- **Owner today:** H3 (H-PROV-18, H-PROV-26). Nobody names the id or the source of the data.
- **Recommendation: decision needed.** Pick one adaptive model (Pi's default `claude-opus-4-8`) as the default record, and add one budget model (Pi's overflow test uses `claude-haiku-4-5`, `AIT:context-overflow.test.ts:96-99`) only if gap 5 is pulled into H3. Take the numbers from Anthropic's published model table, because Pi has no readable copy. Keep `--model` as an exact-id match in H3. Allow the unknown-id fallback only if the user wants it. The default provider is `anthropic` when a key resolves, and `faux` stays an explicit `--provider faux` (`H2P:451`).

### Gap 2. The credential minimum is not written, and the SDK has an ambient credential chain

- **Pi:** precedence is `--api-key` > stored credential > `ANTHROPIC_AUTH_TOKEN` (Bearer) > `ANTHROPIC_OAUTH_TOKEN` > `ANTHROPIC_API_KEY` (`AI:providers/anthropic.ts:19-41`). An empty key throws `No API key for provider: <id>` before any request (`AI:api/anthropic-messages.ts:311-321`). Pi does not read `ANTHROPIC_BASE_URL`: grep finds it only in a Cloudflare constant (`packages/ai/scripts/generate-models.ts:9`, `AI:api/cloudflare.ts:18`).
- **Why H3 needs it:** `anthropic.NewClient` prepends `DefaultClientOptions()` unless told not to (`SDK client.go:209-216`). That chain reads `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_PROFILE`, federation variables and a config file (`SDK client.go:35-80`). Fantasy passes `WithAPIKey` only when the key is non-empty (`FA:279-281`) and does not use `WithoutEnvironmentDefaults`. An empty key therefore lets an `ANTHROPIC_AUTH_TOKEN` or a profile file authenticate the call. D9 says API keys only. The base URL is safe: fantasy sets `https://api.anthropic.com` as the default (`FA:186`) and passes it explicitly (`FA:282-284`), which beats `ANTHROPIC_BASE_URL` (`SDK client.go:59`).
- **Owner today:** H-AUTH-05 in H3 (`R:208`), H-AUTH-01 in H7. The minimum between them is not written.
- **Recommendation: pull into H3.** Write the rule in H3: the key comes from `--api-key`, then `ANTHROPIC_API_KEY`. The adapter fails fast with Pi's text when the key is empty, and never calls fantasy without a non-empty key. `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_OAUTH_TOKEN` stay out (D9). Add a test: with `ANTHROPIC_AUTH_TOKEN` set and no key, no HTTP request leaves. The H2 hook `GetAPIKey` (`H2P:164,492`) is the carrier. A real key must also be per request (H-AUTH-08): fantasy bakes the key into a client at `LanguageModel()` (`FA:275-348`), so the adapter builds one client for each request on a shared `http.Client`.
- **Message for no key:** Pi's text points to `/login`, which is M2. The headless text must name `ANTHROPIC_API_KEY`, `--api-key` and `~/.ask/auth.json` (the T1 hint at `R:648` says the same for the TUI).

### Gap 3. Fantasy silently drops `required` for JSON-decoded schemas

- **Pi:** sends `{type:"object", properties, required}` (`AI:api/anthropic-messages.ts:1517-1528`).
- **Fantasy:** reads `required` only when it is a `[]string` (`FA:728-731`). H1 `ToolDecl.Parameters` is `json.RawMessage` (`pkg/protocol/tool.go:5-10`). A decoded schema holds `[]any`, so `required` is lost. The API accepts the request. The model just sees optional arguments.
- **Owner today:** nobody. The H3 test list has no tool-schema wire test.
- **Recommendation: pull into H3.** Convert `required` to `[]string` in the adapter. Add a wire-JSON golden test on a fake server that checks `required`, the tool name and the `cache_control` on the last tool. Also validate the tool name against `^[a-zA-Z0-9_-]{1,64}$`, because Anthropic rejects other names, and neither H2's registry nor Pi's adapter checks this (Pi normalizes only call ids, `AI:api/anthropic-messages.ts:1220-1222`). The H5 tool names are safe, but MCP and extension names (H16, X1) are not.

### Gap 4. Error data H9 needs is not fixed in H3

- **Pi:** the error message is the SDK message (status, URL, body). H9's retry list matches text such as `overloaded`, `rate.?limit`, `429`, `ended without`, `stream ended before message_stop` (`AI:utils/retry.ts:34-100`). Overflow detection includes HTTP 413 `request_too_large` (`AI:CHANGELOG.md:1266`). The raw stop reason is stored (`AI:api/anthropic-messages.ts:761`). E§22 says error text must include status, code and body (`E§22`, first bullet).
- **Ask H1:** the message has only `ErrorMessage *string` and `RawStopReason *string` (`pkg/protocol/message.go:174-176`). The incomplete-stream text is `stream ended without a terminal event` (`internal/providers/errors.go:14-15`). The abort text is `Request was aborted` (`errors.go:17-18`).
- **Fantasy:**
  - The incomplete-stream error says `stream transport error: unexpected EOF` (`FE:125-131`). Pi's text rule `ended without` would not match it.
  - A mid-stream SSE error has `Message` = the inner message only, without the error type (`providers/anthropic/error.go:79-107`). A rate-limit message may not contain the words `rate limit`.
  - `ProviderError.RequestBody` is `httputil.DumpRequestOut` output (`providers/anthropic/error.go:33`, `SDK internal/apierror/apierror.go:76-82`). It holds the `X-Api-Key` header and the whole prompt. Nothing may log, wrap or persist it.
  - `Error()` is `Title: Message` (`FE:62-67`). The SDK message adds method, URL, status text and body (`SDK apierror.go:62-74`).
- **Owner today:** H9 (`R:354-360`). H3 has no test for it.
- **Recommendation: pull into H3 (cheap, prevents a change in H3 later).** The adapter must:
  1. Build `ErrorMessage` from the full provider error text: status, status text and body. Never use `RequestBody`.
  2. Keep the typed `*fantasy.ProviderError` reachable by wrapping with `%w` (status, `TransientError`, `ContextTooLargeErr`), so H9 can add structured rules without changing H3.
  3. Map `io.ErrUnexpectedEOF` from fantasy to `ErrStreamIncomplete`.
  4. Map `ctx.Err() != nil` to the aborted result first, as Pi does (`AI:api/anthropic-messages.ts:835`).
  5. Map an unknown finish reason to an error, as Pi does (`:1563-1565`, `AI:CHANGELOG.md:376`). Map `refusal` to an error that keeps the explanation, if the explanation is available (gap 6).
  Tests: a fake server returns 429, 529 with `overloaded_error`, 413, 401 and a mid-stream error event. Assert the `ErrorMessage` text, the stop reason, that `errors.As` finds the provider error, and that the secret is absent from the message and from the log output.
- **Stream completeness:** fantasy requires `message_stop` and a stop reason (`FA:1704-1714`). Pi errors on missing `message_stop` after `message_start` (`AI:api/anthropic-messages.ts:510-512`) and on a pending stop reason (`:807-809`). The two rules are the same in effect. No second check is needed. Test it with the H1 truncation approach (E§24#4).

### Gap 5. Thinking: budgets, display, off, and the default level

- **Pi:**
  - Adaptive models (`forceAdaptiveThinking`) use an effort level (`AI:api/anthropic-messages.ts:850-868,890-897`). The list includes Opus 4.6 to 5, Sonnet 4.6 and 5, Fable 5 and Mythos 5 (`generate-models.ts:616-633`).
  - Other models use budget thinking: `adjustMaxTokensForThinking` with the defaults 1024, 2048, 8192, 16384 and at least 1024 tokens left for the answer (`AI:api/simple-options.ts:44-91`, `AI:api/anthropic-messages.ts:899-915`).
  - Display defaults to `summarized` (`AI:api/anthropic-messages.ts:1174-1176`), because Opus 4.7 and Mythos hide the text otherwise (`AI:CHANGELOG.md:1194`).
  - Off on a reasoning model sends `thinking:{type:"disabled"}` unless `thinkingLevelMap.off` is null (`:1191-1193`, `AI:CHANGELOG.md:791,1309`).
  - The interleaved-thinking beta header is sent for non-adaptive models (`:1031-1038`).
- **Fantasy:** `Thinking` without a budget is an error (`FA:431-434`). `Effort` selects adaptive. It never sends `disabled`. It adds no interleaved header (sibling report gap 2 and 3). Its default display is `summarized` only for the Opus 5, Sonnet 5, Fable 5 and Mythos 5 families (`FA:73-90`). Opus 4.7 and 4.8 are not in that list.
- **Is H-PROV-13 a gap?** It depends on gap 1.
  - If H3 hard-codes only an adaptive model, the budget table is not needed for the exit. The adapter uses `Effort`. Keep H-PROV-13 in H17, but write in H3 that non-adaptive models are rejected with a clear error.
  - If H3 must accept any budget model (for example `--model claude-haiku-4-5`), H3 needs the four default values, the answer-room clamp and the interleaved header. That is about 25 lines of arithmetic. The `thinkingBudgets` setting stays in H17.
- **Always:** set `ThinkingDisplay` to `summarized` explicitly, or the default `-p` run on an Opus 4.7 or 4.8 model returns empty thinking text in JSON mode. Define how `""` and `off` map: both mean no thinking, as in Pi (`A:agent.ts:471`). Verify the `off` behavior of the adaptive model with the live test in gap 14, because omitting the field may differ from `disabled`.
- **Recommendation:** keep H-PROV-13 deferred, but make the H3 record adaptive and add the explicit display. If the user wants a budget model in H3, pull the default table and clamp, not the setting.

### Gap 6. Usage, cost and stop reason through fantasy

- **Pi:** the adapter captures usage at `message_start` so an aborted stream still has input tokens (`AI:api/anthropic-messages.ts:620-630`). It updates usage at `message_delta` and tolerates proxies that omit it (`:768-798`). `totalTokens` = input + output + cache read + cache write (`:628-629`, `:797-798`). It sets `reasoning` from `thinking_tokens` (`:790-794`) and the 1 h write split (`:626,783-789`). It prices by the returned model (`:611-619`).
- **Fantasy:** usage appears only on the finish part (`FA:1729-1737`). `TotalTokens` is input + output without cache (`FA:1732`). There is no 1 h split, no reasoning count and no response model. On abort or error there is no usage. The finish reason has no raw string, and the refusal `stop_details` is lost (`FA:1302-1318`).
- **Owner today:** H9 owns H-RETRY-07 (`R:357`). H1 builds the `Usage` type only (`R:122`). H9's context estimate trusts `totalTokens` (`IH:155`, `AI:utils/estimate.ts:23-25,96-110`).
- **Does H3 need it?** Print mode does not. JSON mode shows `message_end` with `usage` and `cost` (`R:123`). The H1 `Cost` has no "unknown" value (`pkg/protocol/usage.go:20-26`). A real run with cost 0 looks free. A `totalTokens` that excludes cache would also make the `maxTokens` clamp too optimistic once caching starts (gap 7).
- **Recommendation: split.**
  - Pull into H3: compute `totalTokens` with Pi's formula in the adapter, and apply the plain price table (input, output, cache read, cache write) in micro-USD for the hard-coded record. This is Pi's call site (`:630`, `:799`) and needs about 10 lines. Tiers, 1 h writes and service tiers stay in H9.
  - Decision needed: `RawStopReason`, response model, the 1 h split, reasoning tokens, usage on abort, and the refusal explanation. Fantasy cannot give them. The sibling report proposes an SSE tap in a custom `RoundTripper`. The alternatives are to accept the loss and document it, or to patch fantasy. H9 does not need `RawStopReason` for its rules (retry and overflow use the text, the stop reason and the usage), so this is not an exit blocker. Usage on abort matters for the H9 totals (`IH:154`, E§11, "Retried requests accumulate usage").

### Gap 7. The `maxTokens` clamp needs an estimator that H9 owns

- **Pi:** every request sends `max_tokens` = model max, clamped to `contextWindow - estimate - 4096` (`AI:api/simple-options.ts:12-17,20-42`; `AI:api/anthropic-messages.ts:1084`). The estimate is `estimateContextTokens` (`AI:utils/estimate.ts:96-110`, about 40 lines).
- **Fantasy:** the default `MaxTokens` is 4096 when the call sets none (`FA:404`). Pi's fix for the same problem on Bedrock is `AI:CHANGELOG.md:887`.
- **Owner today:** H-PROV-07 in H3 (`R:202`); H-RETRY-09 in H9 (`R:358`).
- **Recommendation: decision needed.** Either pull the minimal estimator into H3 (chars/4, image 4800 chars, last-usage shortcut), or send `model.maxTokens` and write the deviation. In both cases the adapter must always set the output limit. Add a test that the request body carries `max_tokens` and that it is never 4096 by default.

### Gap 8. Abort, idle timeout and goroutine leaks over a real HTTP stream

- **Pi:** abort goes through the signal into the SDK and the body read (`AI:api/anthropic-messages.ts:586-590,803-805,835`). The idle timeout is 300000 ms (`C:core/settings-manager.ts:1009-1011`), applied as the SDK timeout (`C:core/sdk.ts:316-335`) and as the undici body and header timeout (`C:core/http-dispatcher.ts:91-95`). `0` means no timeout (`C:core/sdk.ts:322`).
- **Fantasy:** it never calls `Close` on the SDK stream, and it keeps reading until EOF after `message_stop` (`FA:1473-1730`, no `Close` call). The SDK drops `ping` events before fantasy sees them (`SDK packages/ssestream/ssestream.go:208-209`), so the idle timer must sit at the body level. There is no idle timeout in fantasy.
- **Owner today:** H-PROV-25 in H3 (`R:206`). H2 tests goleak only on the faux provider (`H2P:183`).
- **Recommendation: pull into H3 (test scope).**
  - The adapter owns a child context with a cancel cause and cancels it when the stream settles, so the body always closes.
  - The idle timeout is a body-read wrapper in a custom `http.Client` plus a response-header timeout. Never use `http.Client.Timeout` (E§15).
  - Build the transport from `http.DefaultTransport.Clone()` so `HTTP(S)_PROXY` and `NO_PROXY` keep working (H-AUTH-12 stays in H17 but then costs nothing).
  - Tests with `httptest` and `goleak`: cancel in the middle of a stream and exit in under one second; a server that keeps the socket open after `message_stop`; a stalled stream that times out; a slow but active stream that does not time out; SIGINT through `cmd/tui` against the fake server.

### Gap 9. Message conversion rules that Pi has and fantasy does not

- **Pi:** skips empty or whitespace-only text blocks and empty messages (`AI:api/anthropic-messages.ts:1285,1309-1315,1327,1376`). Keeps signed thinking even when the text is empty (`:1341-1366`, `AI:CHANGELOG.md:562`). Turns unsigned thinking into plain text (`:1347-1359`). Keeps `redacted_thinking` (`:1334-1340`). Joins several text results with `\n` and adds `(see attached image)` for image-only results (`:146-149,170-176`). Replays `is_error` (`:1229`). Omits `tools: []` (`AI:CHANGELOG.md:1085`).
- **Fantasy:** keeps an empty text part as an empty block (`FA:1092-1094`, `FA:1209-1216`). Drops thinking without a signature or redacted data with a warning (`FA:1113-1131`). A tool result takes one text or one image only, and `is_error` only for the error type (`FA:1010-1062`).
- **Owner today:** H-PROV-10 and H-PROV-27 in H3 (`R:210-211`), but their H3 tests (`R:217`) cover `transformMessages` only, not the wire body.
- **Recommendation: pull into H3 as wire tests.** Run the conversion in Ask before fantasy, and test the JSON body on a fake server for: empty text, whitespace text, empty tool result (Pi's other adapters use `(no tool output)`, `AI:api/openai-completions.ts:1414`, and `IH:63` lists it; the Anthropic path sends an empty string), unsigned thinking to text, signature round trip (E§24#2), redacted thinking, error results with text, and an image tool result.
- **Surrogates:** Pi strips unpaired surrogates (`AI:utils/sanitize-unicode.ts:22-24`). Go strings are UTF-8, and `encoding/json` replaces invalid bytes with U+FFFD, so the failure does not occur. Keep it as one test (binary bytes in a tool result give a valid JSON request body). No code.

### Gap 10. Prompt caching: placement is manual, and 1 h retention is unreachable

- **Pi:** `cache_control` goes on the system block (`AI:api/anthropic-messages.ts:1105-1113`), the last tool (`:1126,1536`) and the last block of the last user or system message (`:1411-1437`). Default `short`. `long` adds `ttl: "1h"` (`:76-80`) and costs 2x on writes (`AI:models.ts:1205-1210`).
- **Fantasy:** it sets `cache_control` only where the caller passes per-part options (`FA:731-742,914-926`), and `CacheControl` has only a `Type` field (`providers/anthropic/provider_options.go:205-207`). A TTL cannot be set. The sibling report lists `ExtraBody` or a patch as the way (gap table 5).
- **Owner today:** H-PROV-14 in H3 (`R:204`, `IH:110`: "optional 1 h TTL").
- **Recommendation: decision needed.** Keep `short` (default) and `none` in H3, and place the three breakpoints. Move `long` and the 1 h price to a later phase and state it in `R:204`, or accept the `ExtraBody` workaround. A `none` test must show no `cache_control` anywhere. The 4-breakpoint API limit is not a problem (Pi uses 3).

### Gap 11. Hard-coded defaults, env names and settings (H6) before H6 exists

| Setting | Pi | H3 value | Evidence |
|---|---|---|---|
| `httpIdleTimeoutMs` | 300000 ms, `0` = none | hard-code 300000 | `C:core/settings-manager.ts:1009-1011`, `C:core/sdk.ts:316-335` |
| Provider `maxRetries` | undefined = 0 | hard-code 0 | `AI:utils/provider-retry.ts:105-110`, `C:core/settings-manager.ts:1034-1040` |
| `maxRetryDelayMs` | 60000 | not needed with 0 retries | same |
| `cacheRetention` | `short`; env `PI_CACHE_RETENTION=long` | `short` | `AI:api/anthropic-messages.ts:66-76` |
| `thinkingBudgets` | settings | not in H3 | `AI:api/simple-options.ts:44-91` |
| `defaultThinkingLevel` | `medium` | `medium` | `C:core/defaults.ts:3` |
| Base URL | `https://api.anthropic.com`, no env | same; do not read `ANTHROPIC_BASE_URL` | `AI:providers/anthropic.ts:78`, `FA:186` |

- **Env vars Pi reads on this path:** `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_OAUTH_TOKEN`, `ANTHROPIC_API_KEY` (`AI:env-api-keys.ts:29-31`) and `PI_CACHE_RETENTION` (`AI:api/anthropic-messages.ts:73`). Proxy variables are read by the HTTP layer (H-AUTH-12).
- **Ask names:** D7 sets the `ASK_*` prefix. The vendor key stays `ANTHROPIC_API_KEY` (the T1 line `R:648` already says so). The roadmap does not name the cache variable. Suggest `ASK_CACHE_RETENTION`, read in `cmd/tui` and passed in `StreamOptions.CacheRetention` (`internal/providers/types.go:33-43`), so the provider package stays free of `os.Getenv` (it may not import `config`, `internal/providers/README.md` imports rule). Decision needed on the name only.
- **H1 gap:** `StreamOptions` has no field for headers, idle timeout, retries or thinking budgets (`types.go:33-43`). Add only the fields H3 uses (idle timeout). Do not add the rest.

### Gap 12. A stale statement about fantasy retries

- `R:55` (D22) and `H2P:453` say fantasy leaves the SDK retries on. At v0.45.2 it sets `option.WithMaxRetries(0)` (`FA:277`). The sibling report verified the same.
- **Recommendation:** correct both texts. Keep a lock-in test, because H9 needs exactly one retry layer (`IH:150`, E§24#6): a fake server returns 529 once and the test asserts one request on the wire.

### Gap 13. Image limits cross H3 and H5 (kept deferred, but name the risk)

- Anthropic caps an image at 5 MB (E§13, Anthropic row). Pi resizes images before they enter history (H-TOOL-20: 2000x2000, 4.5 MiB, `IH:70`). The roadmap puts H-TOOL-20 in H17 (`R:571`) and H5 owns the read tool (`R:247`).
- If the H5 `read` tool returns a large image in M1, the request fails with 400 or 413 on every later turn, because the image stays in history. A 413 `request_too_large` is an overflow signal in H9 (`AI:CHANGELOG.md:1266`) and would trigger compaction in a loop.
- **Recommendation: keep H-TOOL-20 deferred, but make H5 refuse or text-replace an image above a fixed size** until H17. State it in H5. This is not an H3 exit item. The H3 `@file` text-only rule (`R:170`) and the non-vision placeholder (H-PROV-27) stay as planned.

### Gap 14. Live-test and fixture strategy

- **Pi:** live tests use `describe.skipIf(!process.env.ANTHROPIC_API_KEY)` with `{ retry: 2, timeout: 30000 }` and capture the payload with `onPayload` (`AIT:anthropic-opus-4-8-smoke.test.ts:24-25,37-40`). Other live tests: `AIT:anthropic-thinking-disable.test.ts:163`, `AIT:context-overflow.test.ts:96-99` (Haiku 4.5), `AIT:empty.test.ts:229`, `AIT:abort.test.ts:156` (uses `ANTHROPIC_OAUTH_TOKEN`, not the API key). Offline tests build SSE text by hand and pass a fake `fetch` (`AIT:anthropic-sse-parsing.test.ts:9-15`). Pi has no recorded cassettes.
- **Fantasy:** its own provider tests use recorded `charm.land/x/vcr` cassettes (`providertests/common_test.go:11,61`, `providertests/testdata/TestAnthropicThinking`).
- **Recommendation:**
  1. Offline tests (default in CI): `httptest.Server` with scripted SSE and golden request-body assertions. These cover gaps 3, 4, 8, 9, 10, 12.
  2. Live tests: one Go test file gated by `ANTHROPIC_API_KEY` (name it `ASK_LIVE=1` plus the key if the user wants an explicit opt-in), with a small `max_tokens` and a retry of 2. Cases: hello with text and usage; one tool call and result; thinking with a signature replay on the second turn (E§24#2); an errored assistant message plus an unfinished tool call is accepted (`R:218`); the full pairing after abort (`R:219`, which can only be proven live); the `off` thinking behavior of the default model.
  3. Do not record cassettes with a real key unless the recorder strips `x-api-key` (the same leak as gap 4).

### Gap 15. Lower items (no phase change)

- **Eager tool-input streaming and strict tools.** Pi sends `eager_input_streaming` per tool (`AI:api/anthropic-messages.ts:1533`) and strict schemas when allowed (`:1514-1528`). Fantasy builds only name, description and schema (`FA:714-745`). Effect: long tool arguments arrive in larger chunks, so `toolcall_delta` events are sparse. Correctness is not affected. The idle timer sees `ping`s only if the body wrapper sits below the SDK (gap 8). Accept in H3.
- **`pause_turn`, `stop_sequence`.** Pi maps them to `stop` (`:1557-1560`). Fantasy does the same (`FA:1304-1305`). They need server tools or stop sequences, which H3 does not send. No action.
- **Server-side fallbacks, mid-conversation effort, native tool changes, OAuth identity.** Not in H3 (D9, H8 tool deltas). No action.
- **Binary size and supply chain.** Importing the fantasy Anthropic provider links the AWS SDK and Google auth packages (`FA:22-27`, fantasy `go.mod:7-24`). D22 is decided. Record the size for the user; do not reopen D22.
- **Lazy API loading.** Pi loads each API module on demand (`AI:api/lazy.ts:73-79`). Go links at build time, so this is not needed. The `StreamFn` contract "never fails at call time" (`internal/providers/types.go:46-48`) is the same as Pi's `lazyStream` setup-error rule.

## 4. Ranked gap list

Severity means: could stop the H3 exit or give a wrong real result.

| # | Gap | Severity | Phase today | Recommendation |
|---|---|---|---|---|
| 1 | The H3 model record, its data source and the default model are undefined (gap 1) | High | H3, not specified | Decision needed (user) |
| 2 | Credential minimum, fail-fast, SDK ambient credential chain (gap 2) | High | H3 and H7, not specified | Pull into H3 |
| 3 | Error data for H9: full text with status and body, typed error kept, incomplete and abort mapping, no secret in `RequestBody` (gap 4) | High | H9 | Pull the preservation into H3 |
| 4 | Fantasy drops `required` for JSON schemas, and the tool name charset is unchecked (gap 3) | High (silent) | none | Pull into H3 with a wire test |
| 5 | Usage and cost: `totalTokens` formula, base cost, JSON-mode zero cost, plus the decision on stop reason, response model, 1 h split and abort usage (gap 6) | Medium to high | H9 | Split: pull the formula and base price into H3; decision on the rest |
| 6 | Message conversion wire rules fantasy lacks: empty text, empty tool result, unsigned thinking, error results (gap 9) | Medium to high | H3 (function), no wire test | Pull the wire tests into H3 |
| 7 | Thinking: model choice drives the path; explicit `summarized` display; `off` semantics; H-PROV-13 stays in H17 only with an adaptive model (gap 5) | Medium | H3 and H17 | Keep H-PROV-13 deferred; pull the display; decision tied to gap 1 |
| 8 | Abort, idle timeout, body close, goroutine leaks, proxy-safe transport (gap 8) | Medium | H3 (idle), H2 (leak test on faux) | Pull the real-HTTP tests into H3 |
| 9 | `maxTokens` clamp needs the H9 estimator; fantasy defaults to 4096 (gap 7) | Medium | H3 and H9 | Decision needed: minimal estimator in H3 or a written deviation |
| 10 | Prompt caching: manual breakpoints, 1 h TTL unreachable (gap 10) | Medium | H3 | Decision needed: defer `long`; place three breakpoints |
| 11 | Hard-coded defaults and the cache env name (gap 11) | Low to medium | H6 | Hard-code the listed values; decide `ASK_CACHE_RETENTION` |
| 12 | Live and offline test strategy (gap 14) | Medium | not specified | Adopt the three-part plan |
| 13 | Stale fantasy retry claim (gap 12) | Low (text), medium if it regresses | `R:55`, `H2P:453` | Fix the text; keep a lock-in test |
| 14 | Image size cap vs H17 resize (gap 13) | Low for H3, medium for M1 | H17, H5 | Keep deferred; H5 refuses large images |
| 15 | Lower items: surrogates, eager streaming, `pause_turn`, binary size, lazy load (gap 15) | Low | none | No phase change |

## 5. Items that look like gaps and are not

- **Stream completeness needs no extra check** (gap 4). The H1 assembler and fantasy cover it.
- **Tool-call id charset for Anthropic.** Pi normalizes ids only for foreign messages (`AI:api/transform-messages.ts:110-119`), and Anthropic's own ids already match. The normalizer callback (`AI:api/anthropic-messages.ts:1220-1222`) belongs to the H3 `transformMessages` work (`R:210`) and is exercised in H4.
- **Surrogate sanitizer.** Go does not need it (gap 9).
- **`PI_CACHE_RETENTION` and `ANTHROPIC_BASE_URL`.** The first only needs an Ask name. The second is not read by Pi and is neutralized in Ask by the explicit base URL (gap 2).
- **Print mode and usage.** Print mode writes text only (`R:164`).

## 6. Limitations

- GitNexus was not used. Tracing used direct reads and grep, with exact lines. A call graph could show a caller I missed.
- I did not run any live API call, so two behaviors are unproven: the API reaction to a missing `thinking:{type:"disabled"}` on the default model, and the API reaction to an empty tool-result text block.
- The Pi model catalog is not in the checkout, so I could not read the real context window, max tokens or prices of the Anthropic models (gap 1).
- I read the sibling fantasy report for its conclusions and re-checked eight of its lines. I did not re-audit its whole table.
- I did not read the full H2 port analysis (`plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md`) or the H1 reports, only the roadmap, the H2 plan and the H1 code that H3 touches.
- Path note: the task named this report path. The hook text named a nested reports folder with a different time stamp. I used the path from the task.

## 7. Unresolved questions (for the user, one per turn when needed)

1. Which model does H3 hard-code, and does it accept any non-adaptive model (gap 1, gap 5)?
2. Do you accept dropping `long` cache retention and the 1 h price from H3 (gap 10)?
3. For raw stop reason, response model, the 1 h write split and usage on abort: accept the loss, build an SSE tap, or patch fantasy (gap 6)?
4. Pull a minimal token estimator into H3, or clamp `maxTokens` to the model maximum and document it (gap 7)?
5. The name of the cache environment variable, and whether `--model <unknown id>` may fall back to the default record (gaps 1 and 11).
6. Should H5 refuse large images until H17 resize exists (gap 13)?
