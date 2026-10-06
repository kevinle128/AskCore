# H7a wire profiles and catalog

Status: DONE_WITH_CONCERNS.
The offline wire scope is complete.
Live subscription acceptance is not recorded.

## Changes

Added compiled native Anthropic Sonnet 4.6 and xAI Grok 4.7 records, provider IDs, native base URLs and environment key lookup.
Preserved the existing Token Plan and OpenAI GPT-5.5 records.
No dependency or SDK fork change was needed.

Anthropic OAuth now checks the provider, method, profile and endpoint before HTTP.
The final request checks bearer-only auth, the Messages destination, exact beta flags, CLI headers and the leading CLI system block.
Typed API-key snapshots use the key profile and suppress ambient SDK bearer auth.
Bound clients disable redirects and cookie jars.
The tool-name codec checks every historical declaration snapshot, maps active declarations and named choice, and restores canonical response names before the first public tool event.
Historical tool names use the declaration state at the historical call.
Native adaptive thinking is enabled when applicable.
Forced native tool choice disables default thinking and rejects an explicit conflicting thinking option.

ChatGPT uses public HTTP Responses with full local history, instructions, stream=true and store=false.
The final guard runs after SDK serialization, model headers and credential isolation.
Each forbidden field has an actual serialized extra-body check.
Model sampling data can carry supported grouped namespace tools through the installed SDK ExtraBody seam.
Named choice remains canonical and gains the namespace of the selected grouped tool.
Request-owned model, input, instructions, stream, store and tool-choice fields cannot be replaced by model sampling data.

xAI key and OAuth use the same native Responses endpoint with distinct typed credential profiles.
Encrypted reasoning inclusion follows the model capability.
No Anthropic tool-name codec is used on Responses.
Completions now accepts bound typed API-key snapshots and sends named tool choice.
Unknown OAuth methods fail before HTTP in all adapters.

Added allowance, rate-limit, authentication, unsupported-request and transport error sentinels for consumers.
Responses HTTP and SSE errors retain their underlying provider error while exposing the applicable class.
No automatic retry was added.

The command test found a pre-existing shared Fold bug: rewriting a Responses tool ID discarded streamed arguments.
A failing regression proved argument loss with item metadata at stream start and with metadata first available at completion.
The fix compares the completed ID with the actual opened ID and preserves arguments when late metadata changes the ID.
Assembler authoritative-final semantics remain unchanged.

## Failing checks before fixes

- Native catalog lookup returned unknown model.
- An actual ChatGPT SDK request failed because the profile guard ran before removal of ambient organization and project headers.
- Anthropic sent wrong-provider and unknown-method credentials to HTTP.
- A collision in a historical tool snapshot reached HTTP after the conflicting tool was removed.
- Grouped tools remained flat because model sampling data was not carried into SDK ExtraBody.
- Completions ignored the typed credential and named choice.
- Rewritten Responses tool IDs replaced nonempty arguments with an empty object.

## Verification

`go test -race ./internal/providers ./internal/providers/anthropic ./internal/providers/openai ./internal/providers/fantasykit -count=1` passed.
`go vet ./internal/providers/...` passed.
`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/providers/...` reported zero issues.
`go test ./cmd/tui -run TestAuthKeyCommandNextPromptAndLogout -count=1` passed.
`go test ./cmd/tui -run TestAuthChatGPTCommandVerifiedIdentityReturningAndNewAccount -count=1` passed after the Fold fix.

Tests inspect the actual SDK HTTP body and headers at fixed logical production URLs through injected external transports.
Checks cover ambient credentials, protected model headers, wrong destinations, redirects, every forbidden ChatGPT field, grouped tools, named choice, canonical history and response names, xAI reasoning inclusion and partial-output allowance failure with one request.
No live login or provider request was made by this worker.

## Metadata evidence

[Anthropic Sonnet 4.6 model documentation](https://platform.claude.com/docs/en/models/sonnet-4-6/overview) supplies the model ID, reasoning, text/image inputs, 1M context and 128K output limit.
[xAI Grok 4.7 documentation](https://docs.x.ai/developers/models/grok-4.7) supplies the model ID, reasoning, text/image inputs and 500K context.
[Pi Grok 4.7 catalog](https://pi.dev/models/xai/grok-4-7) supplies the native Responses route, 500K output limit and supported reasoning levels.
[OpenAI tools documentation](https://developers.openai.com/api/docs/guides/tools) supplies the namespace shape for grouped function/custom tools.
The installed Pi source at `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/api/anthropic-messages.ts` supplies the CLI version, tool-name table, OAuth beta flags and identity headers.
The pinned local checkout delegates model metadata to JSON files that are absent locally, so current primary provider and Pi catalog pages were checked.

## Concerns

Live eligible-account prompt, tool and logout acceptance remains required for each subscription provider.
Namespace/custom declarations have no separate field on canonical ToolDecl; grouped tool wire metadata uses the existing model sampling-data seam.
No new canonical tool schema was added.
