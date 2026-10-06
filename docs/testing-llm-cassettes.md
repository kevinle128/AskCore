# Testing with LLM cassettes

Default tests do not call live model services or require API keys.
Provider cassette tests run on recorded HTTP responses; native auth checks use isolated homes, local callbacks, and injected external HTTP boundaries.
They do not prove live subscription eligibility or provider approval.

## The four test layers

| Layer | What it catches | Where |
|---|---|---|
| 1. Cassettes | Errors in the provider's HTTP handling, SSE parsing and event mapping, found on real replies | `cmd/tui/headless_record_test.go`, `cmd/tui/testdata/cassettes/` |
| 2. Faux provider | Agent loop logic: tools, turns, aborts | `internal/providers/faux`, `cmd/tui/headless_test.go` |
| 3. Hand-written faults | HTTP 429/500, a cut stream, bad JSON, a connection reset, SIGINT | `cmd/tui/headless_fault_test.go` |
| 4. Live smoke | Changes in the vendor API. Run it by hand only: `ASK_LIVE=1 go test -run Live ./cmd/tui` | `cmd/tui/headless_live_test.go` |

Layers 1 and 3 go through `newHeadlessAgent` and `runHeadless`. This is the same setup and run code as `ask -p`. Only the HTTP transport is replaced.

## How a cassette works

The package is [internal/providers/cassette](../internal/providers/cassette/cassette.go).
It is built on `go-vcr` v4 and records at the `http.RoundTripper` layer.

- **Replay is the default.** Request N is compared with recorded request N, and the server is never called.
- **Matching:**
  - Each request must have the same method and URL as the recorded one.
  - The JSON body must match, with keys sorted.
  - A mismatch fails the test with a diff of the two bodies and the re-record command.
- **Replay failures:**
  - A missing cassette fails the test.
  - A request that is not in the cassette fails the test.
  - A recorded request that was never sent fails the test.
- **Record** first removes the old file, so a failed recording cannot leave stale data. It then sends every request to the real server and saves the cassette after each response. Only `Content-Type`, `Anthropic-Version` and `Anthropic-Beta` request headers are kept. Auth headers and per-request ids never reach the file.
- **Determinism:** Token Plan request bodies have no timestamps, cwd or ids. Request N repeats the earlier replies, and on replay those come from the cassette. So no normalization is needed beyond sorting the keys.

## Re-record a cassette

Re-record when a test fails with `body does not match the recording`. That means a prompt, a tool or a provider change made the request different. To re-record:

```sh
ASK_RECORD=1 go test -run '^TestCassetteOneToolCall$' ./cmd/tui
```

- Always use `-run`, so you re-record only the test you need.
- A recording may make at most 10 requests by default. Change the limit with `ASK_RECORD_MAX_REQUESTS`. When the limit is reached, the run stops, so a looping agent cannot spend money.
- The key comes from `ALIBABA_TOKEN_PLAN_API_KEY` or `ASK_ALIBABA_TOKEN_PLAN_API_KEY`.
- Each cassette uses the default model `deepseek-v4.1-flash`, and costs a few requests.
- Review the YAML diff in the PR. The model may answer in other words. Narrow prompts keep the shape stable, for example "reply with only the number".
- If the shape changes, fix the prompt or the assertion on purpose. Never loosen an assertion only to make the test pass.

## Turn a real session into a test

```sh
ASK_CAPTURE=/tmp/ask-capture ask -p "..." --provider alibaba-token-plan
# stderr: ask: capturing provider HTTP to /tmp/ask-capture/20261005-215018.780.yaml
```

1. Move the file to `cmd/tui/testdata/cassettes/<TestName>.yaml`.
2. Write `TestName` with `useCassette`, and use the same prompt and tools. `TestCapturedSessionReplays` is an example.
3. Read the YAML before you commit it. Tool output in a real session can contain file contents or paths.
4. Commit it. `TestCassettesHoldNoSecrets` fails on auth headers, key-like strings or the key from the environment.

Tools run again on replay. If a tool result changes, the next request body changes and the test fails. Capture only sessions whose tools give the same result every time.

## Native authentication and capture

[Capture setup](../cmd/tui/capture.go) covers inference for native Anthropic, OpenAI, and xAI, as well as Token Plan.
It does not record login, token refresh, JWKS retrieval, or authenticated account discovery.
[Native composition](../internal/app/auth_native.go) uses a separate private auth client, and auth command dispatch runs before capture setup.
Do not attach the cassette transport to that private client.

Use an isolated `ASK_HOME` and the [native operating commands](../README.md#native-auth-and-headless-prompts) for authorized live checks.
Each subscription route needs real login, prompt/tool, and local logout acceptance in addition to offline checks.
Offline command tests use the real parser, auth service, store, agent, and adapters, with only external HTTP and time boundaries injected.
The owning scenarios are [auth command tests](../cmd/tui/auth_command_test.go), [native OAuth command tests](../cmd/tui/auth_oauth_command_test.go), and [signal tests](../cmd/tui/auth_signal_test.go).

A captured inference cassette can contain prompts, tool results, file paths, and private user data even when auth headers are removed.
Review and sanitize it before sharing or committing it.
Never put credentials, authorization codes, ID tokens, or account hints in an evidence report.

## Environment flags

| Flag | Effect |
|---|---|
| `ASK_RECORD=1` | Tests record their cassettes from the real model instead of replaying. |
| `ASK_RECORD_MAX_REQUESTS=<n>` | The request limit for each recording. The default is 10. |
| `ASK_CAPTURE=<dir>` | `ask -p` records the provider HTTP of the run to a new cassette in `<dir>`. |
| `ASK_LIVE=1` | Runs the live smoke tests. |

## Known limits

- Replay serves the whole response at once. It does not replay SSE timing. The faux provider tests cover chunking and pacing.
- Record reads the whole response before the client gets it. A reply that streams for longer than the 5-minute idle limit records as an idle timeout. Only `Content-Type` is kept in the response headers, so code that reads rate-limit or request-id headers sees none of them on replay.
- The Token Plan adapter does not retry (`MaxRetries(0)`). So HTTP 429 and 500 end the run after one request.
- The idle timeout of `ask -p` is fixed at 5 minutes. It is tested at the provider level (`TestStreamIdleTimeoutAndActiveStream`), not through `ask -p`.
