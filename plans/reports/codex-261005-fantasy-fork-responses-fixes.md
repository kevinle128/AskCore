# Fantasy Responses fixes for Ask H4

Date: 2026-10-05.
Status: F1, F2, F3, F4, and F8 are complete and pushed.
Repository: `/Users/dale/Desktop/workspace/opensources/fantasy`.
Branch: `fix/openai-reasoning-replay-stream-completion`.
Remote: `git@github.com:kevinle128/fantasy.git`.
Final SHA: `08976763bfeaf7743cb489c10852d6bac7f49fdf`.
The remote branch SHA matches the local SHA.
No PR was opened.
No AskCore code or `go.mod` was changed.

## Exact Ask dependency

```go
replace charm.land/fantasy => github.com/kevinle128/fantasy v0.0.0-20261005094512-08976763bfea
```

Verified with `go list -m -json github.com/kevinle128/fantasy@08976763bfeaf7743cb489c10852d6bac7f49fdf`.
The returned module version and origin hash match the replacement above.
The fork has no version tags.
The pseudo-version uses the final commit time, `2026-10-05T09:45:12Z`, and its first 12 SHA characters.
The existing `require charm.land/fantasy ...` line can remain; this replacement selects the fork revision.

## Commits and public fields

| Fix | Commit | Result and public fields |
|---|---|---|
| F1 | `b8c979f6f2031b3140951ccc86666bbcca1c8813` | New `openai.ResponsesTextMetadata` with `ItemID string` and `Phase string`; registered as `openai.TypeResponsesTextMetadata` (`openai.responses.text_metadata`). |
| F2 | `9a2008a9007be45be43fcb23edf04678f833958d` | New `openai.ResponsesToolCallMetadata` with `ItemID string`; registered as `openai.TypeResponsesToolCallMetadata` (`openai.responses.tool_call_metadata`). |
| F3 | `a855426be0c0c7c620638f0c0588453e3923d31c` | New `ExtraBody map[string]any` field on `openai.ResponsesProviderOptions`. |
| F4 | `88f560d3366913e6a8a270763fe545ac5825830b` | New `openai.ResponsesError`; new `ResponseStatus string` and `RawFinishReason string` fields on `openai.ResponsesProviderMetadata`. |
| F8 | `08976763bfeaf7743cb489c10852d6bac7f49fdf` | New `ServiceTier openai.ServiceTier` field on `openai.ResponsesProviderMetadata`. |

### F1: text replay

`Generate` puts message item ID and phase on `fantasy.TextContent.ProviderMetadata[openai.Name]`.
`Stream` puts them on `TextStart` and `TextEnd`; the completed item supplies the final phase.
With `store:false`, text with a non-empty metadata ID becomes an output-message input item with its ID, phase, completed status, and output-text content.
Text parts with the same message ID stay in one message item, in content order.
Metadata-free text and `store:true` requests retain their existing input-message behavior.
No fallback IDs are added.

Tests: `providers/openai/responses_text_replay_test.go`.
They cover Generate, stream start/end metadata, registry JSON storage, message grouping, separate message IDs, and stored replay behavior.

### F2: function-call replay

`Generate` and all function-call stream parts retain the function-call item ID in metadata.
`ToolCallID` and stream `ID` remain the API `call_id`.
With `store:false`, replay sends the item ID only if it starts with `fc_`.
Missing, wrong-prefix, and typed-nil metadata do not add an item ID.
`store:true` retains its existing request shape.
Generate preserves the response item order, so each reasoning item remains before its function call.
Token-limit handling still omits incomplete client tool calls from Generate results.
Custom tools were not added; fantasy does not expose that tool type here.

Tests: `providers/openai/responses_tool_replay_test.go`.
They cover all function-call stream parts, JSON history storage, prefix rules, typed nil, stored replay, and reasoning/call order.

The first full run found stale VCR expected requests that did not contain these IDs.
An offline script regenerated only expected request bodies and their content lengths in 24 existing Responses fixtures, using 30 function-call IDs from earlier recorded responses with the same `call_id`.
All response sections remain byte-identical.
No cassette was re-recorded and no fixture assertion was weakened.
These generated expectation updates are part of F2.

### F3: request overrides

`ExtraBody` uses `params.SetExtraFields` last, with the same semantics as `openaicompat`.
It supports `prompt_cache_retention`, `prompt_cache_options`, and overrides of standard request fields.
A boolean `ExtraBody["store"]` override also controls prompt conversion and the existing `PreviousResponseID` storage check.
This prevents a mismatch between the wire storage value and replay behavior.

Tests: `providers/openai/responses_extra_body_test.go`.
They cover option parsing and registry JSON storage, Generate/Stream/GenerateObject request fields, standard-field overrides, conflicting storage options, and previous-response validation.

### F4: errors and raw finish data

`ResponsesError` has `Code string`, `Type string`, and an embedded `*fantasy.ProviderError`.
The embedded provider error exposes `Message`, `StatusCode`, and the existing HTTP details.
`Unwrap` lets `errors.As` inspect both `*openai.ResponsesError` and `*fantasy.ProviderError`, and the SDK cause when present.
This applies to failed-response events, flat error events, SDK error envelopes, HTTP failures, and failed non-stream response bodies.
Normal and structured-output methods use the same error conversion.
The captured HTTP status has priority over any payload `status_code`; an error inside HTTP 200 remains HTTP 200.
Existing retry rules and normalized finish reasons remain in use.

Error strings contain no request dump or Authorization header.
`ResponsesError` excludes its embedded `ProviderError` from JSON.
Do not separately log or serialize `ProviderError.RequestBody`, which the existing SDK conversion can populate with credentials.

`ResponseStatus` is the terminal `response.status`.
`RawFinishReason` is the unmodified `incomplete_details.reason`, including unknown values.
A completed response normally has an empty raw reason.
Generate, Stream, GenerateObject, and StreamObject expose this metadata.

Tests: `providers/openai/responses_errors_test.go`, `providers/openai/responses_finish_metadata_test.go`, and updated internal helper tests in `providers/openai/responses_params_test.go`.
They cover errors after data has arrived, error fields, HTTP status, conflicting payload status, safe error serialization, completed/incomplete responses, and unknown incomplete reasons.

### F8: actual service tier

`ServiceTier` contains the value from the terminal response, including `response.completed` and `response.incomplete`.
It does not copy the requested tier or an earlier `response.created` tier.
A missing echo remains empty.
Generate, Stream, GenerateObject, and StreamObject expose the same field.

Tests: `providers/openai/responses_service_tier_test.go`.
They cover default, flex, priority, absent echo, terminal precedence, and registry JSON storage.

## Ask H4 adapter mapping

Read part metadata from `ProviderMetadata[openai.Name]` with a type assertion.
For history input, put the reconstructed metadata in the part's `ProviderOptions[openai.Name]`.

| Ask field | Capture | Restore to fantasy |
|---|---|---|
| `ThinkingSignature` | Use `openai.GetReasoningMetadata(fantasy.ProviderOptions(metadata))`; serialize a reasoning item with `type:"reasoning"`, `id:ItemID`, `encrypted_content`, and a summary array of `type:"summary_text"` and `text`. | Restore `*openai.ResponsesReasoningMetadata` with `ItemID`, `EncryptedContent`, `Summary`, and `Finalized:true` on `ReasoningPart.ProviderOptions`. |
| `TextSignature` | Assert `*openai.ResponsesTextMetadata`; serialize `{"v":1,"id":ItemID,"phase":Phase}`, with phase omitted when empty. | Restore `ResponsesTextMetadata{ItemID, Phase}` on `TextPart.ProviderOptions`. |
| `ToolCall.ID` | Assert `*openai.ResponsesToolCallMetadata`; store `call_id + "|" + ItemID` when ItemID is present, otherwise only `call_id`. | Split the composite; put only `call_id` in `ToolCallPart.ToolCallID` and put ItemID in `ResponsesToolCallMetadata`. Tool results use only `call_id`. |

For streamed text, use the final `TextEnd` metadata.
For streamed reasoning, use the finalized `ReasoningEnd` metadata.
For a tool call, use the final `ToolCall` metadata.
Ask must remove provider replay metadata when it selects cross-model or cross-provider history, as agreed in the handoff.
Fantasy does not choose that policy.

Keep `Store:false` and include `openai.IncludeReasoningEncryptedContent` for stateless thinking replay.
The fork does not automatically add that include setting.

At finish, assert `*openai.ResponsesProviderMetadata` and read `ResponseID`, `ResponseStatus`, `RawFinishReason`, and `ServiceTier`.
Use the echoed non-empty tier first for pricing.
For errors, use:

```go
var responseErr *openai.ResponsesError
if errors.As(err, &responseErr) {
    // Read responseErr.Code, Type, Message, and StatusCode.
}
```

Provider types use the existing fantasy JSON type wrapper.
To restore a serialized metadata map, decode it to `map[string]json.RawMessage` and call `fantasy.UnmarshalProviderMetadata` or `fantasy.UnmarshalProviderOptions`.
Direct `json.Unmarshal` into a map of interface values does not restore the registered concrete types.

## Validation and delivery

Before each fix commit, `go build ./...`, `go test ./providers/openai/... -count=1`, and `golangci-lint run` passed.
Final `go test ./... -count=1 -timeout=30m` passed across the repository.
Final lint reported `0 issues`.
Tools: Go 1.27.0 and golangci-lint 2.12.2.
Tests used local HTTP servers and existing VCR recordings only.
The check environment set `GOPROXY=off` and denied external HTTP through a local closed proxy, while permitting localhost.
No live provider test was run.

Review found and resolved duplicate text message IDs, typed-nil tool metadata, conflicting storage overrides, and conflicting HTTP/payload status.
The final review found no remaining actionable issue.
Autosquash retained exactly five fix commits and did not change the verified final tree, `869fc0e92354d1c5be33a39865673b7c26fdf967`.
The branch was pushed only to the user's fork.
The working tree is clean.

Orca checkpoint comments were unavailable.
The CLI reported that no Orca-managed worktree contains this checkout.

## Not verified

Offline tests prove the request shapes and metadata paths.
They do not prove whether a live OpenAI server requires message IDs, phase, or function-call item IDs for the target models.
They do not prove live cross-provider switching, service-tier pricing, or the Ask H4 adapter, which is not implemented by this task.

## Unresolved questions

A live OpenAI test must confirm the target model's F1 and F2 replay requirements when a key is available.
No implementation decision remains open for these five fixes.

## Upstream issue and PR (2026-10-05)

Completed handoff steps 1–8 in the specified order.
Read PR #407, issue #406, and the upstream contribution guide before remote changes.
The new issue, PR, and comment contain plain technical text with no AI references or local machine paths.

- New issue: [#411](https://github.com/charmbracelet/fantasy/issues/411).
- New PR: [#412](https://github.com/charmbracelet/fantasy/pull/412).
  GitHub reports `MERGEABLE`.
  The PR has 11 commits: the six dependency commits from #407, followed by the five new fixes.
  Its body states that it depends on #407 and asks reviewers to review the last five commits.
- PR [#407](https://github.com/charmbracelet/fantasy/pull/407) is restored to exactly six commits at `1610f6bd3cc22d8da115f8f07d3e1f2ec7df7279`.
  The push used the explicit force-with-lease expected SHA `08976763bfeaf7743cb489c10852d6bac7f49fdf`.
  The local `fix/openai-reasoning-replay-stream-completion` branch also points to the restored SHA.
  Its body was not changed.
- One [cross-link comment](https://github.com/charmbracelet/fantasy/pull/407#issuecomment-5993040164) was added to #407.
- Fork `main` SHA: `08976763bfeaf7743cb489c10852d6bac7f49fdf`.
  This was a normal fast-forward push from `82d42a7`, completed before the old branch was restored.
- New fork branch `feat/openai-responses-replay-metadata` SHA: `08976763bfeaf7743cb489c10852d6bac7f49fdf`.
  It was pushed before the old branch was restored.
  `git ls-remote` confirmed that both fork branches point to this exact SHA.

The pinned commit `08976763bfea` is reachable from fork `main` because it is the branch head.
The exact dependency replacement above remains valid.
The current checkout is on `feat/openai-responses-replay-metadata` and is clean.
No source code changed in this task, so the recorded offline build, test, and lint results still apply to the same tree.
No AskCore code was changed.
No step failed or was skipped.
No merge, rebase, label change, review request, or issue closure was performed.
The live-test limits recorded above remain unchanged.
