# H7a live acceptance

## Current status

All live routes passed, and the separate exact command commit-barrier proof is now complete.
Anthropic passed the live login, inference, tool loop, and local logout checks on 2026-10-06.
ChatGPT passed live sign-in, subscription inference, the tool loop, and local logout.
xAI passed live device authorization, subscription inference, the tool loop, and local logout.
No credential, callback code, identity claim, or private auth URL is included in this record.

## Test boundary

The controller built the ordinary `cmd/tui` binary.
The command used a new private `ASK_HOME` for this test.
Ambient provider API keys and capture options were removed from the command environment.
No test HTTP transport, custom token endpoint, TLS exception, or signature bypass was used.
The user completed provider sign-in and consent in the browser.
The loopback callback delivered the authorization code directly to the running command.

## Anthropic result

- Native `anthropic-oauth` login: exit code 0.
- Saved credential: method `anthropic-oauth`, generation 1, expiry present, no pending refresh fence.
- Live model: `claude-sonnet-4-6`.
- Prompt: call `echo` with the fixed test text `H7a-live-check`, then reply with that text.
- Prompt exit code: 0.
- Canonical tool execution: one `echo` result, `isError=false`.
- Assistant messages: two, with stop reasons `toolUse` and `stop`.
- Usage events: present.
- Run settlement: exactly one `agent_settled` event.
- Local logout: exit code 0; the saved Anthropic record was absent after logout.

The callback page confirms receipt only.
The command result and stored record confirm successful token exchange and persistence.
The second assistant turn confirms that the model accepted the tool result.
No API-key fallback was used.

## ChatGPT result

Native `openai-chatgpt` login completed with exit code 0.
The saved record had generation 1, expiry metadata, and no pending refresh fence.
The native flow verified the signed ID token before persistence.

The first prompt selected `gpt-5.5` and was rejected by model readiness.
The live `/v1/models` response listed `gpt-5.5` with visibility `hide` and `gpt-5.6-sol` with visibility `list`.
This refusal occurred before inference and used no API-key fallback.
JSON mode returned exit code 0 with an assistant error event, as required by its existing command contract.
The controller checked terminal events and did not treat the exit code alone as success.

A static `gpt-5.6-sol` row was added through the existing compiled catalog.
The row uses the [official model metadata](https://developers.openai.com/api/docs/models/gpt-5.6-sol).
Existing model defaults were preserved.
The acceptance command selected the new model explicitly.

- Live model: `gpt-5.6-sol`.
- Prompt exit code: 0.
- Canonical tool execution: one `echo` result, `isError=false`.
- Assistant messages: two, with stop reasons `toolUse` and `stop`.
- Usage events: present.
- Run settlement: exactly one `agent_settled` event.
- Assistant error events: none.
- Local logout: exit code 0; the saved OpenAI record was absent after logout.

The connected command regression also verifies hidden GPT-5.5 refusal and listed GPT-5.6 Sol inference.
See the [final proof report](./tester-261006-0835-h7a-final-proof.md).

## xAI result

- Native `xai-oauth` device authorization: exit code 0.
- Saved credential: generation 1, expiry present, no pending refresh fence.
- Live model: `grok-4.7`.
- Prompt exit code: 0.
- Canonical tool execution: one `echo` result, `isError=false`.
- Assistant messages: two, with stop reasons `toolUse` and `stop`.
- Usage events: present.
- Run settlement: exactly one `agent_settled` event.
- Assistant error events: none.
- Local logout: exit code 0; the saved xAI record was absent after logout.

All three provider records were absent from the isolated test home after the last logout.
The test used no remote revocation, in line with the accepted local-logout contract.

## Repository checks

`go test ./...` passed after the live callback listener closed.
`go vet ./...` passed.
The full `golangci-lint` check reported zero issues.
An earlier concurrent test run failed because the live listener held the fixed Anthropic callback port.
The controller reran the full suite after the listener closed; the suite passed.
This test scheduling conflict is not a provider implementation failure.

## Final acceptance closure

- The exact local commit actor passed all five cases twice with race instrumentation, in 38.186 seconds.
See the [executed actor record](./tester-261006-0855-h7a-fsync-actor.md) for the final proof and environment diagnosis.
Final review and full plan sync use that result.
The final independent review approves the implementation and acceptance evidence.
The final tagged CLI lint check also passed with zero issues.
