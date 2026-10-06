# H1 scout: faux provider, stream completeness, partial JSON, SSE reader

Date: 2026-10-01. Source: Pi commit 2bbfcca43 (read-only). Scope: H-PROV-09, H-PROV-11 and the helper files named in the task.
Path shorthand: `AI:` = `packages/ai/src/`, `A:` = `packages/agent/src/`, `C:` = `packages/coding-agent/src/`, `AIT:` = `packages/ai/test/`, `AICL:` = `packages/ai/CHANGELOG.md`.
Style: ASD-STE100 simple English.

## 0. Outcome (read this first)

1. The faux provider is small (710 lines, `AI:providers/faux.ts`). Ask can copy its behavior in about 400 lines of Go. Four things in Pi's faux are weak and Ask must fix them: random chunk sizes and random ids (flaky), no way to set usage, no way to cut a stream without a terminal event, and chunking by UTF-16 units.
2. Pi has no unit test for its partial-JSON code. `AI:utils/json-parse.ts` is a 124-line wrapper. The real algorithm is in the npm package `partial-json@0.1.7` (`AI:../package.json:78`). I rebuilt the wrapper in a scratch script, ran the package, and recorded the vectors in section 3. They are experiment results, not Pi test data. The only Pi test vector for repair is `AIT:anthropic-sse-parsing.test.ts:374`.
3. Pi parses partial JSON on every tool-call delta because its events carry a cumulative `partial` message. Ask events are delta-only (H-MODE-05). So Ask needs the tolerant parser only at `toolcall_end` and in UI previews. This removes an O(n^2) cost.
4. The partial-JSON wrapper has one real defect: the repair step runs only when the partial parser throws, and the partial parser seldom throws. A raw tab or newline inside an unterminated string returns `{}`. The Go port should repair first (vector V23).
5. Stream completeness: every Pi adapter ends with a "pending stop reason means error" check. The error text contains "ended without" or "ended before". The retry table matches that text (`AI:utils/retry.ts:77-81`). Ask should use a typed error and keep the same words in the message.
6. Pi's own SSE code has no line limit. Go has a trap: the official Go SDKs use `bufio.Scanner` with a 32 MiB limit (`bufio.MaxScanTokenSize<<9`, verified in `openai/openai-go` and `anthropics/anthropic-sdk-go` `ssestream.go` on the main branch). The roadmap's "64 KB" is the default Scanner limit, so a plain Scanner would fail. Ask writes its own reader with a configurable limit.
7. Tool-argument validation (`AI:utils/validation.ts`) belongs to H2, not H1.

## 1. Feature tree

```
H-PROV-09 Faux provider
  AI:providers/faux.ts
  |- Model definition                          faux.ts:41-50 (type), 463-488 (defaults, build)
  |- Content helpers fauxText/Thinking/ToolCall faux.ts:54-69
  |- fauxAssistantMessage (scripted reply)      faux.ts:78-101
  |- Script queue (set, append, pending count)  faux.ts:445, 665-673
  |- Response factory (fn of context)           faux.ts:109-116, 496
  |- State counters (callCount)                 faux.ts:103-107, 447
  |- Usage and cache simulation                 faux.ts:158-160, 230-268
  |- Chunking (token size to chars)             faux.ts:270-280
  |- Pacing (tokensPerSecond)                   faux.ts:331-337
  |- Event builder streamWithDeltas             faux.ts:339-435
  |    |- abort checks (4 places)               faux.ts:348, 358, 372, 395, 412
  |    |- thinking / text / toolCall blocks     faux.ts:367, 390, 408
  |    |- terminal: done | error | aborted      faux.ts:424-434
  |- Error paths (exhausted, throw, pending)    faux.ts:513-523, 556-560, 424-426
  |- Registration                               compat.ts:162 (registerFauxProvider), faux.ts:687 (fauxProvider)
  |- Deferred handles (skip in Ask)             faux.ts:294-306, 526-552, 569-644

H-PROV-11 Stream completeness and parsing
  |- Event stream object                        AI:utils/event-stream.ts:21-110
  |- Terminal-event rule per adapter            section 4 table
  |- Tolerant partial JSON                      AI:utils/json-parse.ts:85-124 (+ partial-json lib)
  |- String repair (raw control chars, bad \x)  AI:utils/json-parse.ts:32-83
  |- Call sites (per delta + at end)            section 3.5
  |- Null chunk, no choices, missing id/index   AI:api/openai-completions.ts:555, 567, 494-541
  |- Unknown SSE event names ignored            AI:api/anthropic-messages.ts:335-342, 490
  |- Body read aborted after terminal event     AI:api/openai-codex-responses.ts:799-855
  |- Unfinished tool call guard                 AI:api/openai-responses-shared.ts:764-776
  |- Error body normalization                   AI:utils/error-body.ts:44-149
  |- Diagnostics record on messages             AI:utils/diagnostics.ts:3-47

Helpers (owner phase in brackets)
  |- validation.ts   validateToolArguments       [H2: H-LOOP-10, H-TOOL-12]
  |- sanitize-unicode.ts                         [H-PROV-10, outbound text]
  |- abort.ts, abort-signals.ts                  [H2: context.Context replaces both]
  |- error-body.ts                               [provider HTTP errors, H3]
  |- diagnostics.ts                              [field on the message record, H1]

SSE reading
  |- Anthropic: own reader (spec-like)           AI:api/anthropic-messages.ts:323-472
  |- Codex: own reader (data lines only)         AI:api/openai-codex-responses.ts:799-855
  |- pi-messages: own reader (first data line)   AI:api/pi-messages.ts:276-321
  |- openai-completions, openai-responses: vendor SDK (npm `openai`)   openai-completions.ts:371
```

## 2. Faux provider: API and behavior

### 2.1 Pi API (as built)

| Item | Behavior | Cite |
|---|---|---|
| `registerFauxProvider(opts)` | Registers an api id in the global registry. Returns `{api, models, getModel, state, setResponses, appendResponses, getPendingResponseCount, unregister}`. | `AI:compat.ts:162-181` |
| `fauxProvider(opts)` | Same core, but returns a `Provider` for an explicit `Models` collection. No global state. | `faux.ts:687-710` |
| Options | `api`, `provider`, `models[]`, `tokensPerSecond`, `tokenSize{min,max}`, `deferred{...}`. | `faux.ts:118-132` |
| Defaults | api id = `faux:<time>:<random>`. Provider `faux`. Model `faux-1`, not reasoning, text+image, context 128000, max tokens 16384, cost 0. Token size 3 to 5. | `faux.ts:24-30, 437-488` |
| Model definition | `id`, `name`, `reasoning`, `input`, `inputLimits`, `cost`, `contextWindow`, `maxTokens`. Base URL is `http://localhost:0`. | `faux.ts:41-50, 476-488` |
| Script step | An `AssistantMessage` or a factory `(context, options, state, model) => AssistantMessage \| Promise<...>`. | `faux.ts:109-116` |
| Queue | `setResponses` replaces, `appendResponses` adds. One step is taken at call time, in call order (`shift()` in the synchronous part of `stream`). | `faux.ts:507, 665-673` |
| `state.callCount` | Incremented at call time, before the factory runs. A factory sees its own call counted: `"1:1"` in the test. | `faux.ts:508`, `AIT:faux-provider.test.ts:161-173` |
| Exhausted queue | One terminal `error` event. Message: `No more faux responses queued`. Usage is still estimated. | `faux.ts:513-523` |
| Factory throws | One terminal `error` event with the thrown message. No `start` event. | `faux.ts:556-560`, test `:175` |
| Scripted `stopReason: "error"` or `"aborted"` | Blocks stream first (`start`, `*_start`, `*_delta`, `*_end`), then terminal `error` with `reason` = the stop reason and the full scripted message. | `faux.ts:427-431`, tests `:432-480` |
| Scripted `stopReason: "pending"` | All blocks stream, then a thrown error: `Faux response ended without a stop reason`. Terminal event is `error`, never `done`. | `faux.ts:424-426`, test `:196` |
| Identity rewrite | `api`, `provider`, `model` on the reply are overwritten with the registered ones. | `faux.ts:282-292`, test `:99` |
| `onResponse` hook | Called once per request with `{status:200, headers:{}}` before any event. | `faux.ts:512` |
| Helpers | `fauxText(s)`, `fauxThinking(s)`, `fauxToolCall(name, args, {id})`; the id defaults to `tool:<time>:<random>`. `fauxAssistantMessage(content, {stopReason, errorMessage, responseId, timestamp})`; a string becomes one text block; default stop reason is `stop` (not `toolUse`). | `faux.ts:54-101` |

### 2.2 How events are built (`streamWithDeltas`, `faux.ts:339-435`)

1. Make `partial` = the message with empty content and `stopReason: "pending"`.
2. If the signal is already aborted: push one terminal `error` (reason `aborted`), no `start`. End. (`faux.ts:348-353`, test `:482`: exactly 1 event.)
3. Push `start`.
4. For each block, in order:
   - Check abort. If aborted: terminal `error` (aborted) with the partial content so far.
   - `thinking`: `thinking_start`, then one `thinking_delta` per chunk, then `thinking_end` with the full text.
   - `text`: the same with `text_*`.
   - `toolCall`: `toolcall_start` (arguments `{}`), then `toolcall_delta` per chunk of `JSON.stringify(arguments)`, then set `arguments` to the full object and push `toolcall_end` with the whole tool call.
   - Per chunk: wait (`scheduleChunk`), then check abort, then apply the chunk, then push the delta. So an abort during the wait drops that chunk.
5. After all blocks: `error` or `aborted` stop reason gives terminal `error`; any other gives `done` with the message.
6. Exact order for one thinking, one text, one tool call, token size 1: `start, thinking_start, thinking_delta, thinking_end, text_start, text_delta, text_end, toolcall_start, toolcall_delta, toolcall_end, done` (`AIT:faux-provider.test.ts:382-409`). Note that this test uses short strings (`"go"`, `"ok"`, `{}` = 2 chars) and token size 1 means 4 chars, so each block has exactly one delta. The test does not prove chunk size.

Important detail: Pi faux never fills `partial.content[i].arguments` during the deltas. It stays `{}` until `toolcall_end`. Real adapters parse per delta. So the faux cannot test the partial-JSON path by itself. Ask should fix this (section 2.4).

### 2.3 Chunking, pacing, usage

- Chunk size: `tokenSize` is `min..max` tokens. Each chunk gets a random token count in that range. One token = 4 chars. Minimum 1 char. (`faux.ts:270-280`.) Random source: `Math.random` (not seedable). The cut is in UTF-16 units, so it can split an emoji pair.
- Empty text: yields one empty chunk `[""]`, so there is still one delta (`faux.ts:279`).
- Pacing: `tokensPerSecond` unset or zero means a microtask yield (no real delay). Set means `delay = ceil(len/4) / tps * 1000` ms per chunk (`faux.ts:331-337`).
- Usage estimate (`faux.ts:230-268`): `ceil(chars/4)`. Prompt text = `role:text` for each message joined with a blank line. Output text = blocks joined with `\n`; a tool call counts as `name:JSON(args)`. Pi always overwrites usage, so a scripted usage is lost (`resolveResponse`, `faux.ts:497-502`). The coding-agent tests that need exact usage bypass faux and write their own stream function (`C:test/suite/agent-session-compaction.test.ts:45-66`).
- Cache simulation: only if `options.sessionId` is set and `cacheRetention != "none"`. The first call for a session: `cacheWrite = promptTokens`. Later calls: find the common prefix with the last prompt of that session. `cacheRead` = tokens of the prefix. `cacheWrite` = tokens of the rest. `input = max(0, prompt - cacheRead)`. Cost is always zero (`faux.ts:244-266`, tests `:266-345`).
- Test caution: `AIT:faux-provider.test.ts:233-239` expects the prompt text to include `system:sys` and a `tools:` line. Source `faux.ts:198-218` serializes the system message as one message with `tool+:` lines. I did not run the test. The two look different. Do not copy that test's formula. Write the Ask estimate test against the Ask formula.

### 2.4 How Pi tests use faux

| Where | Count | Pattern |
|---|---|---|
| `AIT:` | 6 files (`faux-provider.test.ts` has 25 cases) | Event order, abort at each block type, usage, cache, queue, factory. |
| `packages/agent/test` | 15 files | `e2e.test.ts:186-260`: prompt, tool run, abort with `tokensPerSecond:20, tokenSize 2:2`, state updates with `tokenSize 1:1`, multi-turn factory that reads `context.messages`. Harness tests: `harness/execution-assistant.test.ts:208` uses `fauxProvider({tokenSize 1:1})`. |
| `packages/coding-agent/test` | 64 files | `test/suite/harness.ts:150` wraps `registerFauxProvider`. Retry tests script `fauxAssistantMessage("", {stopReason:"error", errorMessage:"overloaded_error"})` then a good reply (`suite/agent-session-retry-events.test.ts:42-45`). Compaction tests use factories and `stopReason:"length"` (`suite/agent-session-compaction.test.ts`). The README says: use faux, no real provider (`suite/README.md:7`). |

Pattern for Ask: faux is the base of every loop, retry, compaction and session test. It must be easy to script and fully deterministic.

### 2.5 Proposed Go API

Package: `internal/providers/faux` (see Unresolved Q1). It imports only `internal/providers`. It is non-test code so that `agent`, `sessions` and `gateway` tests can import it.

```go
package faux

// Provider implements providers.Provider. One Provider = one api id.
func New(opts ...Option) *Provider

// Options.
func WithModels(defs ...ModelDef) Option   // default: one model "faux-1"
func WithAPI(id string) Option             // default: "faux"
func WithProvider(id string) Option        // default: "faux"
func WithChunk(minTokens, maxTokens int) Option // default 3..5, 4 runes per token
func WithSeed(seed int64) Option           // default: fixed seed 1 (deterministic)
func WithTokensPerSecond(tps float64) Option    // 0 = no delay
func WithClock(c Clock) Option             // fake clock for delay tests

// Script.
func (p *Provider) Set(steps ...Step)      // replace queue
func (p *Provider) Append(steps ...Step)
func (p *Provider) Pending() int
func (p *Provider) Calls() int             // callCount
func (p *Provider) Requests() []providers.Request // every request seen, for assertions
func (p *Provider) Model(id ...string) providers.Model

// Steps. One Step = one model call.
func Reply(blocks ...Block) Step           // stop reason: toolUse if any ToolCall, else stop
func Say(text string) Step                 // Reply(Text(text))
func Func(f func(ctx context.Context, r providers.Request, s State) (Step, error)) Step
func Fail(msg string) Step                 // blocks (if any) then terminal error
func AbortAt(...) Step                     // scripted aborted message
func Truncate(after int) Step              // NEW: close the stream after N events, no terminal event
func Raw(events ...providers.Event) Step   // NEW: exact events, for adapter-level tests

// Step modifiers (return Step).
func (s Step) Stop(reason providers.StopReason) Step
func (s Step) WithUsage(u providers.Usage) Step   // NEW: bypass the estimate
func (s Step) Error(msg string) Step
func (s Step) ResponseID(id string) Step
func (s Step) Delay(d time.Duration) Step         // wait before the first event
func (s Step) Pace(tps float64) Step              // per-step pacing

// Blocks.
func Text(s string) Block
func Thinking(s string) Block
func ToolCall(name string, args any, opts ...ToolOpt) Block // marshals with encoding/json
func ToolCallRaw(name, id, rawJSON string) Block             // NEW: send these exact bytes as deltas (broken JSON allowed)
func ID(id string) ToolOpt
```

Behavior rules for Go (each maps to a Pi line or a stated fix):

1. The stream function never returns an error for a runtime failure. It returns a stream whose last event is `error` (H-LOOP-16). Exhausted script, a failing `Func`, and an unknown step all become a terminal `error` event with Pi's messages (`No more faux responses queued`, the thrown text).
2. Take the step in the synchronous part of the call, in call order (`faux.ts:507`). Increment the call count first.
3. Event order and abort checks: copy section 2.2 exactly. Abort uses `ctx.Done()`. A delay is a `select` on a timer and `ctx.Done()`. After an abort the stream emits one terminal `error` with reason `aborted`, `errorMessage: "Request was aborted"`, and the partial content built so far. It never emits `*_end` for the block that was cut.
4. Determinism (fix): chunk sizes come from a seeded PRNG. Tool-call ids default to a counter (`tool:1`, `tool:2`), not time and random.
5. Chunk on rune boundaries (fix). A Go string delta must be valid UTF-8. For tool-call JSON, cut anywhere on a rune boundary, including inside a `\uXXXX` escape. This is on purpose: it tests the tolerant parser.
6. Usage: default estimate `ceil(runes/4)`, same shape as Pi, including the per-session cache. `WithUsage` overrides it (fix; Pi cannot). Cost stays zero unless the step sets usage. The `Usage` type is micro-USD integers (H1 "also builds").
7. Feed the faux through the same assembler as real adapters (see 8.1). The faux emits raw JSON deltas through `Assembler.ToolDelta`. This makes every faux tool-call test also a partial-JSON test.
8. `Truncate(n)` (new): emit the first n events and close the channel with no terminal event. This gives the agent and retry tests a real "stream ended early" case. Pi cannot do this in faux (it only tests it per adapter with SSE fixtures).
9. Default stop reason is inferred (`toolUse` when a tool call exists). Pi needs the caller to say `toolUse` (`AIT:faux-provider.test.ts:49-60`). This is a deliberate deviation; it removes a common script mistake. The caller can still override with `.Stop(...)`.
10. Not copied: deferred handles (H-PROV-08 is "skip"), `onResponse` hook (add when H11 needs `after_provider_response`), cross-model rewrite helpers.

Minimum H1 tests for the faux (all from Pi cases): queue order and exhaustion; replace and append; factory sees context and call count; factory error; exact event order for `Thinking, Text, ToolCall`; multiple tool calls; scripted error and aborted terminal; pending-without-stop error; abort before first chunk (1 event); abort mid-text, mid-thinking, mid-tool-call (exactly 1 delta, no `*_end`); identity rewrite; usage estimate and cache per session id; no cache without session id or with `cacheRetention none`; the three new cases (`WithUsage`, `Truncate`, `ToolCallRaw`); same seed gives same chunks.

## 3. Partial JSON: algorithm and vectors

### 3.1 Pi wrapper (`AI:utils/json-parse.ts`)

`parseStreamingJson(s)` (`:104-124`) runs four tries in order and never throws:

1. If `s` is empty or only whitespace: return `{}`.
2. `parseJsonWithRepair(s)`: strict `JSON.parse`. On failure, run `repairJson(s)`. If the text changed, `JSON.parse` the repaired text. If it did not change, rethrow.
3. If step 2 threw: `partialParse(s)` (the npm package, `Allow.ALL`). Return `result ?? {}`.
4. If step 3 threw: `partialParse(repairJson(s))`, with `?? {}`.
5. If step 4 threw: return `{}`.

`repairJson` (`:32-83`) walks the text once and tracks "in string":
- Outside a string: copy the char; a `"` starts a string.
- Inside a string: `"` ends it. A raw control char (U+0000 to U+001F) becomes `\b \f \n \r \t` or `\u00XX`.
- Backslash: at end of text, emit `\\`. `\u` followed by four hex digits: copy. A valid escape (`" \ / b f n r t u`): copy. Any other char after a backslash: emit `\\` (the backslash is doubled, so `\H` becomes the text `\H`).
- Note: `\u` followed by fewer than four hex digits falls to the "valid escape" test (`u` is in the set), so it is copied as `\u` + the rest. This is invalid JSON but `partialParse` cuts it (vector V12).

### 3.2 The partial-JSON package (`partial-json@0.1.7`, all parts allowed)

Source read: `dist/index.js` of the package (I found it in another repo's `node_modules`; Pi's own `node_modules` is not installed here).

1. Trim the input. Empty input throws (the wrapper catches it).
2. `parseAny`: skip blanks. At end of input: throw "partial".
3. `"` string: scan to the closing quote, counting backslash escapes. If closed: `JSON.parse` that slice (a bad escape throws "malformed"). If not closed: `JSON.parse(slice + '"')`. If that throws, cut the text at the last backslash and retry. If there is no backslash, this retry throws too.
4. `{` object: skip blanks. Loop until `}`. At end of input: return the object so far. Read a key with the string rule (a non-string key throws). Skip blanks. Skip exactly one char as the colon without checking it. Parse a value. If the value throws, return the object so far (the incomplete key is dropped). Skip blanks. Skip one comma if present.
5. `[` array: same loop. At end of input or on a throw: return the array so far.
6. Literals `null true false Infinity -Infinity NaN`: match in full, or match as a prefix only when the input ends inside the word (`tr` gives `true`, `nul` gives `null`).
7. Number: scan to the next `,` `]` or `}`. Try `JSON.parse`. On failure try the text before the last `e`. On failure throw "malformed". A lone `-` throws.
8. After the first value, the rest of the input is ignored (trailing garbage is dropped).

### 3.3 Test vectors (experiment; Pi has no unit test for these)

Result column = what `parseStreamingJson` returns. "Pi test" marks the one vector that comes from a Pi test file.

| # | Input | Output | Note |
|---|---|---|---|
| V1 | `` (empty) and `   ` | `{}` | step 1 |
| V2 | `{`, `{"`, `{"a`, `{"a"`, `{"a":` | `{}` | key without value is dropped |
| V3 | `{"a":1` | `{"a":1}` | |
| V4 | `{"a":1,` | `{"a":1}` | |
| V5 | `{"a":"x` | `{"a":"x"}` | open string closed |
| V6 | `{"a":"x\` | `{"a":"x"}` | dangling backslash cut |
| V7 | `{"a":"\` | `{"a":""}` | |
| V8 | `{"a":"he said \"hi` | `{"a":"he said \"hi"}` | escaped quote kept |
| V9 | `{"a":tr` | `{"a":true}` | literal prefix |
| V10 | `{"a":nul` | `{"a":null}` | |
| V11 | `{"a":-` and `{"a":1.` | `{}` | partial number drops the key AND stops the object (later keys are lost, earlier keys stay) |
| V12 | `{"a":"x\u00` | `{"a":"x"}` | incomplete `\u` cut |
| V13 | `{"a":"xé` | `{"a":"xé"}` | complete `\u` kept |
| V14 | `{"a":"\ud83d\ude` | `{"a":"\ud83d"}` | JS keeps a lone surrogate; Go stdlib gives U+FFFD (section 8.4) |
| V15 | `{"a":1e` | `{"a":1}` | text before last `e` |
| V16 | `{"a":1e5` | `{"a":100000}` | |
| V17 | `{"a":[1,2` and `{"a":[1,2,` | `{"a":[1,2]}` | |
| V18 | `{"a":{"b":"c` | `{"a":{"b":"c"}}` | nested open string |
| V19 | `{"a":[{"b":1},{"c"` | `{"a":[{"b":1},{}]}` | empty object appears for a half-written element |
| V20 | `{"edits":[{"old":"a","new":"b"}` | `{"edits":[{"old":"a","new":"b"}]}` | typical edit tool prefix |
| V21 | `{"path":"A\H","text":"col1<TAB>col2"}` (raw tab, bad `\H`) | `{"path":"A\\H","text":"col1\tcol2"}` | Pi test: `AIT:anthropic-sse-parsing.test.ts:374-443`, expected `{path:"A\\H", text:"col1\tcol2"}`. Repair path (step 2). |
| V22 | `{"a":1}garbage`, `{"a":1}}`, `{"a":"x"} {"b":1}` | first object only | trailing text ignored |
| V23 | `{"a":"x<TAB>y` (raw tab, unterminated) | Pi: `{}`. Go target: `{"a":"x\ty"}` | Pi defect: step 3 does not throw (it returns `{}`), so repair never runs |
| V24 | `{"a":"line1<LF>line2` | Pi: `{}`. Go target: `{"a":"line1\nline2"}` | same defect |
| V25 | `{"a":"\q` | `{"a":""}` | |
| V26 | `{"a":"\q"}` | `{"a":"\\q"}` | repair doubles the backslash |
| V27 | `{a:1}`, `{"a" 1}`, `{"a":1 "b":2}` | `{}` | malformed |
| V28 | `{"a":[1 2]}` | `{"a":[]}` | |
| V29 | `{"a":1e5x` | `{"a":1}` | odd but harmless |
| V30 | `{"a":NaN`, `{"a":Infinity` | `{"a":null}` (JS value NaN) | not JSON; Go drops these (no support) |
| V31 | `null`, `42`, `"abc"` | `null`, `42`, `"abc"` | Pi returns scalars although the type says object. Go target: `{}` |
| V32 | `nul` | `{}` | |
| V33 | `{"a":"x","a":"y"}` | `{"a":"y"}` | last key wins |

Property test (I ran it on an 83-char document with strings, nested array, emoji, escape, int, bool, float): cut at every prefix length. No call threw. The last prefix equals the full parse. Truncated numbers give smaller numbers (`123` appears as `1`, `12`, `123`). So a partial result must never be executed.

### 3.4 What Ask must build

Package `internal/providers/partialjson` (about 150 lines, no dependency; GL§4 found no credible Go library).

```go
// Parse never panics. It returns an object map; an input it cannot read gives an empty map.
func Parse(s string) map[string]any
```

Algorithm for Go (Pi order, plus one extra fall-through for V23 and V24):

1. Empty or blank input: empty map.
2. `json.Unmarshal` into `map[string]any`. If it works, return it. Else, if `Repair(s)` changed the text, try `json.Unmarshal` on the repaired text.
3. Run the tolerant parser on the original text (rules in 3.2, minus NaN and Infinity). If it gives a non-empty object, return it.
4. Else run the tolerant parser on `Repair(s)` (port of `repairJson`; treat `\u` + 4 hex as in Pi). If it gives an object, return it.
5. Else return the result of step 3 if it was an object, or an empty map. A non-object result (array, string, number, null) is an empty map (V31).

Differences from Pi, each on purpose: an empty object from step 3 also triggers the repaired retry (V23, V24 give the better answer; Pi only retries on a throw, and the tolerant parser seldom throws); a non-object result becomes `{}`; no NaN or Infinity. I ran this variant on all 33 vectors and on the prefix sweep (145 inputs). V6, V7 and V25 stay as in Pi (repair-first would change them). Only V23, V24, V31 and the top-level array and string cases change.

Property tests: no panic and an object result at every prefix (also for random byte noise: add a fuzz target); the full text equals `json.Unmarshal`; a valid document cut at each byte offset; no prefix result may contain a key that is absent from the full document (check this one; I did not check it on Pi, but it holds for all vectors above).

### 3.5 Where Pi calls it

| Call site | When | Cite |
|---|---|---|
| Anthropic `input_json_delta` | every delta, on the whole buffer | `AI:api/anthropic-messages.ts:709` |
| Anthropic `content_block_stop` | once, then the scratch field `partialJson` is deleted | `:745-748` |
| openai-completions | every delta; and in `finishBlock` | `AI:api/openai-completions.ts:459, 651` |
| openai-responses | every delta; at `arguments.done`; at `output_item.done` (with `item.arguments || partialJson || "{}"`) | `AI:api/openai-responses-shared.ts:660, 667, 717` |
| Bedrock, Mistral | every delta and at the end | `bedrock-converse-stream.ts:629, 742`; `mistral-conversations.ts:735, 750` |
| Frame replay | `assistant-message-frame.ts:247, 252, 454, 486` | wire replay of recorded streams |
| `streamProxy` client | after rebuilding a streamed block | `A:proxy.ts:356` |

Consequence: Pi re-parses the whole buffer on every delta (quadratic). With delta-only events Ask parses once at `toolcall_end` (and a UI may parse a throttled copy for preview).

Truncation guard (owned by H2, but it depends on this parser): the final parse never fails, so a message cut by the token limit can give tool calls with arguments that parse and validate but are incomplete. The agent loop fails all tool calls when `stopReason == "length"` (`A:agent-loop.ts:262-270, 478`; agent changelog 0.80.4). H1 must carry the `length` stop reason through every path for H2 to use this.

## 4. Stream completeness and error classification

### 4.1 Rule

A stream is complete only when the protocol's terminal event arrived. EOF without it is an error, not a success. The error is retryable. This rule is per protocol, because each wire format has a different terminal event.

| Protocol | Terminal event | Error when missing | Cite |
|---|---|---|---|
| Anthropic | `message_stop` | `Anthropic stream ended before message_stop` (only if `message_start` was seen); then a second check: stop reason still `pending` gives `Anthropic stream ended without a stop reason` | `anthropic-messages.ts:510-512, 807-809` |
| openai-completions | a chunk with `finish_reason` | `Stream ended without finish_reason`. If compat says the server sends no finish reason, infer `toolUse` or `stop` | `openai-completions.ts:684-697` |
| openai-responses | `response.completed`, `response.incomplete` or `response.failed` | `OpenAI Responses stream ended before a terminal response event`. Also: stop reason `toolUse` with a tool call that has no `output_item.done` gives `...completed with an unfinished tool call: <name> (<id>)` | `openai-responses-shared.ts:761-776` |
| Codex | same events as Responses | `Codex stream ended without a stop reason` | `openai-codex-responses.ts:110-112` |
| pi-messages | `done` or `error` event | `<provider> stream ended without a terminal event` | `pi-messages.ts:423` |
| Google, Vertex, Mistral, Bedrock | finish reason | `... ended without a finish reason` / `stop reason` | `google-generative-ai.ts:277`, `google-vertex.ts:285`, `mistral-conversations.ts:160`, `bedrock-converse-stream.ts:336` |
| Faux | `stopReason != pending` | `Faux response ended without a stop reason` | `faux.ts:424-426` |
| Agent loop (backstop) | `done` or `error` event | If the event loop ends with no terminal event, the loop takes the final message from `result()` | `A:agent-loop.ts:457-465` |

Event stream object (`AI:utils/event-stream.ts`): a push after the terminal event is ignored (`:34`). `end(result)` resolves `result()` only if a result is given (`:55-58`). Pi hazard: if an adapter closes the stream without a terminal event and without a result, `result()` never resolves. Pi avoids this because every adapter has a catch that pushes `error`. Ask should make it impossible: when the event channel closes with no terminal event, `Result()` returns a synthesized error message `stream ended without a terminal event`.

### 4.2 Classification (retry)

- Pi does not use error types. It tests the message text with two regexes (`AI:utils/retry.ts:7-58, 249-250`). Non-retryable limit patterns win (`:249`). Retryable patterns include `ended without`, `stream ended before message_stop`, `stream ended before a terminal response event` (`:77-81`), network words, HTTP 5xx and 429.
- The "unfinished tool call" error does not match any retry pattern by its own words. It contains no "ended without". So Pi does not retry that case. I consider this probably intended (the server is broken), but I did not find a stated reason.
- Ask rule for H1 (retry itself is H9): define one sentinel `providers.ErrStreamIncomplete` (use `errors.Is`) and keep the words "ended without" in its message so the H9 text table also matches. H9 may then classify by type first and by text second.
- Stop reasons that are not mapped must become errors, not `stop`: Pi keeps `rawStopReason` and turns unknown terminal reasons into provider errors (AICL 0.83.0, #7272). For Responses, only `max_output_tokens` means `length`; other incomplete reasons are non-retryable errors (AICL 0.84.0 #7540; tests `openai-responses-terminal-event.test.ts:396-437`).

### 4.3 Tolerance rules that go with it

| Input problem | Pi behavior | Cite | Go rule |
|---|---|---|---|
| `null` or non-object chunk | skip | `openai-completions.ts:555` | skip |
| Chunk without `choices` | skip (`Array.isArray` guard) | `:567-568` | skip |
| Tool-call delta without `id` | keep a block by stream `index`; fill `id` when it arrives; a block starts with `id: ""` | `:494-541` | same; if the id never arrives, make one (`call_<n>`) at `toolcall_end` |
| Gateway changes the id between chunks | merge by stable `index` first, then by id | `:494-520`, AICL 0.70.0 | same |
| Mistral continuation without id | same `index` merge | AICL 0.84.4 | same |
| Responses server omits `output_index` | unfinished call becomes an error | `openai-responses-shared.ts:764-776`, AICL 0.99.0 | same |
| Unknown SSE event name (proxy `done`, `proxy.stats`) | ignored, even with data that is not JSON | `anthropic-messages.ts:490`, test `AIT:anthropic-sse-parsing.test.ts:680-701` | ignore unknown names before parsing data |
| Bad JSON in a known event | error with event name, data and raw lines | `anthropic-messages.ts:502-506` | same, with a size cap on the quoted data |
| `event: error` | throw with the data as message | `:486-488` | same; if data is empty, use the raw lines |
| `[DONE]` data | ignored | `openai-codex-responses.ts:831`, `pi-messages.ts:317` | ignore at adapter level (the SSE reader does not know it) |
| Usage in `choice.usage` | accept | `openai-completions.ts:574-577` | H3/H4 |
| Abort during the stream | check at loop start and after read; the final state is `aborted` | `anthropic-messages.ts:426`, `openai-codex-responses.ts:810-815` | `ctx.Err()` check; never emit events after cancel |

## 5. SSE reading

### 5.1 What Pi does

| Reader | Where | Behavior |
|---|---|---|
| Anthropic (own, since 0.68.1) | `anthropic-messages.ts:323-472` | Spec-like. `TextDecoder` with `stream:true`. Line ends: `\n`, `\r`, `\r\n`. A `:` line is a comment. Field split at the first `:`; one leading space removed. `event` and `data` kept (`data` lines joined with `\n`); `id`, `retry` ignored. Blank line dispatches. At EOF: decode the rest, flush the last line, then flush a pending event. No line limit. No BOM strip. Dispatches when there is an `event` name even with no data. |
| Codex (own) | `openai-codex-responses.ts:799-855` | Splits on `\n\n` only (assumes `\n`). Keeps only `data:` lines, trimmed. At EOF a leftover buffer gets `\n\n` added so the last frame is read (AICL 0.85.0, #9047). Invalid JSON gives `CodexProtocolError` with the payload. On abort it cancels the reader. In `finally` it always cancels the reader and releases the lock (AICL 0.78.0). |
| pi-messages (own) | `pi-messages.ts:276-321` | Replaces `\r\n` with `\n`, splits on `\n\n`, uses the first `data:` line only. |
| openai-completions, openai-responses | npm `openai` SDK | The SDK reads SSE. Pi sets `maxRetries: 0` and does its own retry (`openai-completions.ts:368`). Pi keeps no limit of its own. |
| Google, Bedrock, Mistral | vendor SDKs | Not SSE in Pi's code. |

Pi has no line-length limit and no cap on the buffer. Pi's Anthropic reader re-scans the whole buffer after each chunk (`consumeLine` slices the text each time), so one huge line costs quadratic time.

### 5.2 Go facts (verified)

- `openai/openai-go` and `anthropics/anthropic-sdk-go`: `packages/ssestream/ssestream.go` uses `bufio.NewScanner` with `scn.Buffer(nil, bufio.MaxScanTokenSize<<9)`. That is a 32 MiB limit per line. A longer line stops the stream with a scan error. The default Scanner limit is 64 KiB.
- If Ask uses its own HTTP code (roadmap: "SSE reader without bufio.Scanner"), the reader must meet the requirement below. If Ask later uses an official SDK, this limit still applies and a very large tool-argument delta could hit it.

### 5.3 Requirements for `internal/providers/sse`

```go
type Event struct{ Name, Data string; Raw []string }
func NewReader(r io.Reader, opts ...Option) *Reader // WithMaxLineBytes(n), default 32 MiB
func (r *Reader) Next(ctx context.Context) (Event, error) // io.EOF at the end
```

1. No `bufio.Scanner`. Use `bufio.Reader`; read byte runs up to the next `\n` or `\r` (scan for either byte, as Pi does at `anthropic-messages.ts:386-396`), and append to a growing buffer when the run is longer than the read buffer. A line of any size up to the limit passes. A longer line returns `ErrLineTooLong` (not a silent cut, not a hang).
2. Line ends: `\n`, `\r\n`, bare `\r`. A `\r` directly before `\n` is one line end. A stream that uses only `\r` must work.
3. Field rules as in the Anthropic reader. Remove one leading space only. Comment lines (`:`) are ignored but may go to `Raw`. A line without `:` is a field name with an empty value.
4. Dispatch on a blank line. At EOF, flush a pending event even without a blank line (AICL 0.85.0). At EOF, treat an unterminated last line as a line.
5. Strip a UTF-8 BOM at the start of the stream (an extension; Pi does not do it; the SSE standard does).
6. Work on bytes and build a string per complete line, so a multi-byte rune split between reads is safe.
7. Respect `ctx`: check `ctx.Err()` before every read and after it returns. Return the context error and no event once the context is cancelled (AICL 0.39.0: late events after abort must not change state). The caller makes the request with `http.NewRequestWithContext`, so a cancel unblocks `Read`.
8. After the terminal event the consumer stops reading and closes the body (cancel the request context). It must not wait for EOF, because a server may keep the socket open (AICL 0.78.0).
9. No `http.Client.Timeout` on a stream. Use an idle timeout that resets on every read, plus a bounded wait for response headers (AICL 0.76.0, 0.80.3 for Codex; E§25). Owned by H3; the reader only exposes the hook.
10. Linear time: total work must grow in proportion to the input (AICL 0.86.0, quadratic drain of buffered events).

### 5.4 Test list

1. One `data:` line of 200 KiB passes (this is the roadmap's "line > 64 KB" test). Also 10 MiB.
2. A line over the limit gives `ErrLineTooLong` and no events after it.
3. `\n`, `\r\n`, bare `\r`, and mixed endings give the same events.
4. The input split in 1-byte chunks (`iotest.OneByteReader`) gives the same events as unsplit input. Include a `\r` and `\n` split across two reads.
5. A multi-byte rune and an emoji split across reads.
6. Comment lines; `data:x`, `data: x`, `data:  x` (only one space removed).
7. Two `data:` lines join with `\n`.
8. A line without a colon; unknown fields (`id`, `retry`) ignored.
9. Empty events (blank line with no fields) produce nothing.
10. EOF with a pending event and no blank line: event is returned (0.85.0).
11. EOF in the middle of a line: handled as a line, then flushed.
12. Leading BOM.
13. `event: error` with data and with empty data (adapter test).
14. Cancel while blocked in `Read`: `Next` returns within a short time; `goleak` shows no leaked goroutine.
15. Events that arrive after cancel are not returned.
16. A server that keeps the connection open after the terminal event: the consumer returns and the server sees a closed connection (`httptest` server).
17. 1 million events: time grows linearly (benchmark with two sizes, compare the ratio).
18. Fuzz: split at every offset equals the unsplit result.
19. Adapter fixtures (H3/H4, listed here so H1 defines the helper): each protocol truncated at every event boundary and in the middle of a line gives `ErrStreamIncomplete`; unknown event names ignored; `[DONE]` ignored; null chunk; chunk with no `choices`; tool call without id; tool-call id changing between chunks.

## 6. Edge cases with versions

Versions are from the `AICL:` and agent changelog headings. Line numbers are in `packages/ai/CHANGELOG.md` unless stated.

| Edge case | Version | Cite | Test in section |
|---|---|---|---|
| Faux provider added (`registerFauxProvider` and helpers) | 0.64.0 | `AICL:1274`; git `ef6af5ebb` 2026-03-29 | 2.5 |
| `fauxProvider()` for explicit `Models` | 0.80.0 | `AICL:700` | n/a |
| `pending` stop reason for partial messages | 0.83.0 | `AICL:375` | 2.5 |
| Faux deferred pending/ready/failed/cancelled | 0.84.0 | `AICL:331` | skip |
| Inventory says faux is 0.70.0; the changelog says 0.64.0. Use 0.64.0. | n/a | `AICL:1274` | n/a |
| Partial JSON added to streaming tool calls (first commit) | 2025-09-16 | git `39c626b6c` | 3.3 |
| `parseStreamingJson` exported | 0.45.6 | `AICL:1829` | n/a |
| Malformed trailing tool-call JSON must not fail (completions, responses) | 0.52.10 | `AICL:1536` (#1424) | V1-V33, prefix sweep |
| Own Anthropic SSE parser with defensive JSON repair (applied, reverted, applied again on 2026-04-21) | 0.68.1 | `AICL:1146` (#3175); git `4b926a30a`, `fc9220d2d`, `e58d631c8` | V21 |
| `partialJson` scratch field leaked into saved tool calls | 0.67.2 | `AICL:1225`; test `AIT:openai-responses-partial-json-cleanup.test.ts` | assembler test: saved message has no scratch field |
| Null chunk crashed | 0.62.0 | `AICL` (#2466) | 5.4 #19 |
| Chunk without `choices` crashed | 0.55.2 | `AICL` (#1671) | 5.4 #19 |
| Anthropic stream ends before `message_stop` is an error | 0.71.0 | `AICL:1045` (#3936) | 5.4 #19 |
| Anthropic ignores unknown proxy events such as OpenAI-style `done` | 0.70.3 | `AICL:1083` (#3708) | 5.4 #19 |
| completions with no `finish_reason` is an error, so it can retry | 0.74.1 | `AICL:949` (#4345) | 5.4 #19 |
| Responses stream ends before terminal event is an error; `response.incomplete` is a length stop | 0.80.0 | `AICL:706` (#5526) | 5.4 #19 |
| Only `max_output_tokens` is a length stop; unknown terminal reasons are errors | 0.84.0, 0.83.0 | `AICL:361, 376` | 4.2 |
| Responses unfinished tool call (no `output_index`) is an error | 0.99.0 | `AICL:61` | 5.4 #19 |
| Tool-call deltas merge by stable index when ids change | 0.70.0 | `AICL:1122` (#3576) | 5.4 #19 |
| Mistral continuation chunk without tool-call id | 0.84.4 | `AICL` (#8387) | 5.4 #19 |
| Codex SSE: body reads aborted after terminal event | 0.78.0 | `AICL:847` | 5.4 #16 |
| Codex SSE: terminal event with no blank line | 0.85.0 | `AICL:178` (#9047) | 5.4 #10 |
| Codex SSE error events keep message, code, status | 0.38.0 | `AICL:1928` (#551) | adapter test |
| Gemini CLI: cancel SSE reader when abort fires | 0.39.0 | `AICL:1911` (#568) | 5.4 #14, #15 |
| Codex bounded waits for headers and events | 0.76.0, 0.79.2, 0.80.3 | `AICL:873, 789, 606` (#4945) | H3 |
| Quadratic CPU draining buffered events | 0.86.0 | `AICL` (#9055) | 5.4 #17 |
| Tool calls from a length-truncated message must fail, not run | agent 0.80.4 | agent `CHANGELOG.md:196` (#6285); `A:agent-loop.ts:262-270` | H2 |
| Tool argument validation with TypeBox, also in runtimes without `eval` | 0.69.0 | `AICL:1131` (#3112) | n/a in Go |
| Unsigned thinking from aborted streams leaked as text | 0.27.7 | `AICL:2055` | H-PROV-10 |

## 7. Gaps against the H1 rows

"Own" = H1 must build and test it. "Defer" = give it to the phase named, with the reason.

| # | Item | Decision | Reason |
|---|---|---|---|
| G1 | Faux provider with script, factory, chunking, pacing, usage, abort, errors | Own (H-PROV-09) | Exit test of H1 needs it. |
| G2 | Faux: explicit usage override (`WithUsage`) | Own | Pi lacks it; compaction and cost tests need exact usage (`C:test/suite/agent-session-compaction.test.ts:45`). Not a Pi feature, but cheap and needed by H9/H10 tests. |
| G3 | Faux: `Truncate` (no terminal event) and `ToolCallRaw` (broken JSON) | Own | These are the only way to test H-PROV-11 and the agent backstop without an HTTP fixture. |
| G4 | Faux: deterministic seed and ids | Own | Pi's randomness makes event-count tests flaky. |
| G5 | Faux deferred handles | Defer (never, H-PROV-08 is skip) | Not used by the shipping loop. |
| G6 | Faux `onResponse` hook | Defer to H11 | Needed only for `after_provider_response`. |
| G7 | Partial-JSON parser with the V23 fix | Own (H-PROV-11) | Roadmap row. Pi has no test, so Ask writes the vectors in 3.3. |
| G8 | Per-delta parse of tool args | Do not build in H1 | Events are delta-only; parse at `toolcall_end` only. A UI preview calls `partialjson.Parse` itself. Confirm in Unresolved Q2. |
| G9 | Assembler (builds the message from deltas, strips scratch fields, parses at end) | Own | H1 builds the message record; H3/H4 adapters and the faux share it (DRY). Not a named row, but "a scripted faux stream becomes a correct event sequence and a final message" is the H1 exit. |
| G10 | Stream object: one terminal event, push-after-terminal ignored, `Result()` synthesizes an error when the channel closes early | Own (H-LOOP-16 and H-PROV-11) | Prevents Pi's hang hazard (4.1). |
| G11 | `ErrStreamIncomplete` sentinel with Pi's words | Own (type), Defer (retry table) to H9 | H9 owns H-RETRY rows. |
| G12 | Terminal-event rule per wire protocol | Defer to H3 (Anthropic) and H4 (completions, Responses); H1 owns the rule, the sentinel and the test helper | Adapters do not exist in H1. |
| G13 | SSE reader with test list 5.4 #1-18 | Own | Named in the H1 roadmap block. |
| G14 | Unknown events, null chunks, missing ids, `output_index` guard | Defer to H3/H4 (adapter code); H1 supplies the fixture helper | Adapters do not exist in H1. |
| G15 | Body read stopped after terminal event | Defer to H3 (needs HTTP); the reader API supports it | |
| G16 | Tool argument validation (`validation.ts`) | Defer to H2 | Called in the loop prepare step (`A:agent-loop.ts:726`); owned by H-LOOP-10 and H-TOOL-12. Needs a JSON Schema validator (GL§4: not chosen yet). |
| G17 | `sanitize-unicode.ts` | Defer to H-PROV-10 (replay) | Applies to outbound text. In Go it is `strings.ToValidUTF8(s, "")`. Apply it in the request builder of each adapter (H3/H4); not needed in H1. |
| G18 | `abort.ts`, `abort-signals.ts` | Do not port | `context.Context` and `context.WithCancel` replace both. H-LOOP-08 is H2. |
| G19 | `error-body.ts` (status and body from SDK errors, 4000-char cap) | Defer to H3 | Needed when the first real HTTP error arrives. Keep the cap (`MAX_PROVIDER_ERROR_BODY_CHARS = 4000`, `error-body.ts:21`). If Ask makes its own HTTP calls, this is simpler: read the body up to 4000 chars. |
| G20 | `diagnostics` field on the assistant message | Own (field only) | H-SESS-05 lists `diagnostics?`. Shape: `{type, timestamp, error{name,message,stack,code}, details}` (`AI:utils/diagnostics.ts:3-47`). Producers come later. |
| G21 | Message record fields and stop reasons (`pending` never saved) | Own (H-SESS-05) | Faux and assembler use them. |
| G22 | `rawStopReason` and unknown-reason-is-error | Own the field; Defer the mapping to H3/H4 | |

## 8. Go port notes

1. **One assembler (DRY).** Make `providers/stream.Assembler`. Methods: `Start`, `TextDelta`, `ThinkingDelta`, `ToolStart(id, name)`, `ToolDelta(json)`, `ToolEnd`, `Done(reason, usage)`, `Fail(err)`. It owns the block list, the raw JSON buffer per tool call, the final parse, and the rule "scratch fields never reach the saved message" (AICL 0.67.2). The faux and every adapter call it. Then faux tests exercise the same code as real streams.
2. **Event shape.** Delta-only events with a block index (H-MODE-05). No cumulative `partial`. `toolcall_start` carries `id` and name. `message_end` is the authority. Pi's cumulative `partial` was removed from the wire in 0.84.0.
3. **Channels.** Use a buffered channel for events, closed after the terminal event. A send must select on `ctx.Done()` so that a slow consumer cannot leak the producer goroutine after cancel. Test with `goleak`.
4. **Strings and surrogates.** Go strings are bytes; there are no lone surrogates in valid text. But `encoding/json` decodes a lone `\ud83d` escape to U+FFFD, while JS keeps the lone surrogate (V14). So Go gives a replacement character where Pi gives a character that later breaks the provider call. This is safe. Add one test for it. For outbound text use `strings.ToValidUTF8(s, "")` (G17).
5. **Numbers.** `encoding/json` into `any` gives `float64`. Tool schemas that want integers must coerce in H2 (H-TOOL-12). Do not use `json.Number` in the parser; keep one numeric type.
6. **Key order.** `json.Marshal` of a Go map sorts keys. Pi tests compare after parsing, so Ask tests must also compare parsed values, not strings.
7. **Chunk by rune.** Use `utf8.DecodeRuneInString` to find cut points (rule 5 in 2.5). A byte cut makes invalid UTF-8 in the delta.
8. **Clock.** The faux uses an injected clock for `tokensPerSecond` and `Delay`, so abort-mid-stream tests run without real sleeps. Pi's paced tests use real timers (`AIT:faux-provider.test.ts:505-605`).
9. **Package placement.** `internal/providers/{faux,sse,partialjson,stream}`. All four import only the standard library and `providers` types, which fits the README import rules (`internal/providers/README.md`). `internal/testsupport` README says "test code only, no production code", so the faux does not go there.

## Limitations

- I did not run Pi's test suite. I ran only a scratch copy of the wrapper (`parseStreamingJson` re-written by me) against the `partial-json@0.1.7` package that I found in another repo. Vectors V1-V33 are my results; the package version matches `AI:../package.json:78`.
- I did not read the `openai` npm SDK's SSE decoder. I only cite that Pi uses it (`openai-completions.ts:371`).
- Go SDK limits (32 MiB) come from a fetch of the main branch, not from the versions Ask will pin.
- I read Pi's faux test file fully but only grepped the other 85 test files that use faux.
- `AICL` has no "Fixed" entry for surrogates (grep for `surrogate` found none); the only surrogate code is `sanitize-unicode.ts` (G17).
- I did not check the claim that no prefix result has a key that is missing from the full document; it holds for the vectors I ran.

## Unresolved questions

1. Where does the faux live: `internal/providers/faux` (my recommendation: it is non-test code that three packages import, and `testsupport` README forbids production code) or `internal/testsupport/faux` (roadmap H1 says "providers (types, SSE reader, faux provider)", which agrees with the first)? Needs a user answer before H1 code starts.
2. Is "no per-delta partial parse" acceptable? It follows from the delta-only decision (H-MODE-05), but it drops Pi's live `partial.arguments` on the message. The TUI preview for large `write` and `edit` arguments (H-TOOL, T1) would then parse its own buffer on a timer. Confirm with the user or at T1 design.
3. Should a faux step with a tool call default to `toolUse` (my recommendation, differs from Pi) or require an explicit stop reason like Pi?
4. Does Ask classify stream errors by type (`ErrStreamIncomplete`) with the Pi words kept for the text table, or only by text like Pi? This is an H9 decision; H1 only needs the type to exist.
5. Maximum SSE line size: 32 MiB (same as the Go SDKs) or lower? A 100 KiB `write` argument is normal; 32 MiB is a memory risk only on a hostile server.
6. Does Ask use official Go SDKs for Anthropic and OpenAI (they bring a 32 MiB Scanner limit and own retry) or its own HTTP and SSE reader? The roadmap says own SSE reader; the Go libraries report (GL§2) leans to "own thin layer over official SDKs". These two statements need one decision at H3.

Status: DONE_WITH_CONCERNS
Summary: Report covers faux API with a Go proposal, the partial-JSON algorithm with 33 vectors, completeness rules per protocol, SSE reader requirements with 19 tests, 30 edge cases with versions, and 22 gaps with owners. Concern: Pi has no json-parse unit test, so the vectors are my own experiment; one Pi faux test looks stale against its source (not run).
Concerns/Blockers: See Unresolved questions 1, 2 and 6 (they change where code lives and how H3 reads streams).
