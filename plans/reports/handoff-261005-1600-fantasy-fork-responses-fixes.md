# Handoff: fix the fantasy fork for OpenAI Responses stateless replay (before H4)

Date: 2026-10-05. From: the H4 port-analysis session (Claude, AskCore `master-2`). To: a Codex agent in a new Orca panel.
Language: write code comments, commit messages and the result report in English (ASD-STE100 Simplified Technical English). The user speaks Vietnamese.

## 1. Why this work exists

Ask (`/Users/dale/orca/workspaces/AskCore/master-2`, Go) rebuilds the Pi agent harness (TypeScript, `/Users/dale/Desktop/workspace/opensources/pi`, reference commit `4c6fb7cfe`). Decision D22: every LLM wire API goes through `charm.land/fantasy`. Roadmap phase H4 adds `openai-completions` and `openai-responses`. Its exit: one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay.

Ask keeps history locally and sends the full history on each request (`store:false`). For OpenAI Responses this means: the encrypted reasoning item, the message item id and phase, and the function-call item id from earlier turns must go back in the next request's `input`, as Pi does. Upstream fantasy v0.45.2 cannot do this. The user decided (2026-10-05): **patch fantasy in our fork** ("sửa fantasy, chúng ta có source mà"). **H4 starts only after this work is done.**

Full analysis (read sections "Decisions recorded" and "State of the fantasy fork"): `/Users/dale/orca/workspaces/AskCore/master-2/plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md`.
Pi behavior detail with `file:line`: `/Users/dale/orca/workspaces/AskCore/master-2/plans/reports/researcher-261005-1354-h4-pi-openai-responses.md` (sections 1.4, 1.6, 1.7, 2, 6) and the fantasy gap table: `.../researcher-261005-1354-h4-fantasy-openai-fit.md` (sections 2, 3).

## 2. Repository and state

- Fork: `/Users/dale/Desktop/workspace/opensources/fantasy`, remote `origin` = `git@github.com:kevinle128/fantasy.git`.
- Branch: `fix/openai-reasoning-replay-stream-completion` (pushed, clean at handoff). Base `82d42a7` = upstream `main`, a little after v0.45.2.
- Read `AGENTS.md` in the fork first and follow its conventions.
- Already done on the branch (do not redo):
  - `bff4512` fix(openai): preserve encrypted reasoning for stateless replay. With `store:false`, reasoning parts replay inline as `reasoning` items (id, summary, `encrypted_content`). Stream metadata comes from `output_item.done`. Test `providers/openai/responses_reasoning_replay_test.go`.
  - `76fdec8` fix(openai): allow strict stream completion checks (`WithLanguageModelRequireFinishReason`).
  - `9a5405c` fix(anthropic): preserve large tool argument numbers.

Pi shorthand below: `S` = `pi/packages/ai/src/api/openai-responses-shared.ts`, `R` = `pi/packages/ai/src/api/openai-responses.ts`. Line numbers are at Pi `4c6fb7cfe`; verify them.

## 3. Scope: five fixes (user choice: fix all before H4)

All in `providers/openai/` (mainly `responses_language_model.go`, `responses_options.go`). One commit per fix. Each fix has offline tests (no live keys; there is no `OPENAI_API_KEY` on this machine; do not re-record cassettes).

### F1. Message item id and `phase` on assistant text replay

- Now: text replays as `EasyInputMessage` with no id and no phase (`responses_language_model.go`, assistant `ContentTypeText` branch, about line 566).
- Need: capture the message item id and `phase` on the stream (`output_item.added` / `output_item.done` with `type: "message"`) and in `Generate`, expose them as provider metadata on the text part, and replay an output-message input item with that `id` and `phase` when the metadata is present and `store` is false.
- Pi: `S:53-77` (text signature `{v:1,id,phase}`), `S:271-289` (replay; fallback ids `msg_pi_{idx}[_k]`; ids longer than 64 become `msg_<hash>`). In fantasy, do not invent fallback ids unless the API requires an id; Ask decides same-model versus cross-model before it calls fantasy (cross-model text arrives with no metadata).

### F2. Function-call item id (`fc_...`)

- Now: the stream exposes only `call_id`; replay sends `function_call` with `call_id`, name and arguments, no item `id` (about line 592).
- Need: expose the function-call item id in provider metadata of the tool-call stream parts and the `Generate` content, and replay it as the `id` of the `function_call` input item when present.
- Rule from Pi commit `bc2d8dc1c` (`S:296-305`): send the item id only when its prefix matches the replayed item type (`fc_` for `function_call`; `ctc_` for `custom_tool_call` if fantasy supports custom tools). Otherwise omit it. A reasoning item followed by its function call must keep that order.
- Pi: `S:160-177` (`call_id|item_id` composite), `S:292-333`, bug history `d327b9c76` (cross-model `fc_` ids break pairing; Ask strips metadata cross-model, so fantasy only needs the prefix rule).

### F3. `ExtraBody` for Responses

- Now: `openaicompat` has `ProviderOptions.ExtraBody` (applied with `params.SetExtraFields`); the Responses options have none.
- Need: `ExtraBody map[string]any` on the Responses provider options, applied last so it can override any field, same semantics as `openaicompat/language_model_hooks.go:72-74`. First use: `prompt_cache_retention` and `prompt_cache_options` (`R:97-114,335-336`).

### F4. Error status and raw incomplete reason on Responses streams

- Now: `response.failed` and `error` events become a plain `fantasy.Error` with no status and no code (about lines 1269-1287, 1336-1349); the raw `incomplete_details.reason` is lost (`mapResponsesFinishReason`, about lines 967-985).
- Need: keep the provider error code, type and message (and the HTTP status where one exists) in a typed error that callers can inspect with `errors.As`, mid-stream errors included; keep `response.status` plus `incomplete_details.reason` as a raw finish reason in the Responses finish provider metadata. Pi: `S:743-808` (terminal handling, `Response incomplete: {reason}`, only `max_output_tokens` means length), `R:222-225`.
- Never put the request body or the `Authorization` header in an error string (`ProviderError.RequestBody` already holds a dump with the key; do not log or serialize it).

### F8. Echoed `service_tier`

- Now: only the requested tier is known (about lines 275, 354-371).
- Need: put `response.service_tier` from the terminal response (`response.completed` / `incomplete`) into the Responses finish provider metadata (`ResponsesProviderMetadata`). Pi prices with the echoed tier first (`R:387-415`, `S:578-583`).

## 4. Constraints

- Keep upstream fantasy style; small, focused diffs; no unrelated refactors. Do not change public behavior for `store:true` callers.
- Conventional commits (`fix(openai): ...`, `feat(openai): ...`). No AI references in commit messages.
- Run before each commit, inside the fork: `go build ./...`, `go test ./providers/openai/...`, then the full `go test ./...` before the final push, and the linter the fork uses (see `AGENTS.md` / `Taskfile` / `.golangci.yml`). Fix failures; do not skip or weaken tests.
- Push only the fork branch `fix/openai-reasoning-replay-stream-completion` to `origin` (the user's fork) when everything passes. Do not open PRs. Upstream PRs come later, after the fixes work in Ask (user decision 2026-10-05).
- Do not edit the AskCore repo code. The AskCore H3 work (`internal/providers/tokenplan/`, `go.mod`) is uncommitted work owned by another session. Only write the result report below.

## 5. Done means

1. Five commits (F1, F2, F3, F4, F8) on the branch, each with offline tests, all tests and lint green, branch pushed.
2. A result report at `/Users/dale/orca/workspaces/AskCore/master-2/plans/reports/codex-261005-fantasy-fork-responses-fixes.md` with:
   - each fix: what changed, the new public types and fields (names exactly as in code), the tests;
   - the final commit SHA and the exact `go.mod` line Ask must use: `replace charm.land/fantasy => github.com/kevinle128/fantasy <pseudo-version>` (get it with `go list -m -json github.com/kevinle128/fantasy@<sha>` or build it from the commit time and SHA);
   - how Ask must read the new metadata (for the H4 adapter that serializes it into `ThinkingSignature`, `TextSignature`, `ToolCall.ID`);
   - anything not verified (F1 and F2 server requirements are not proven without a live OpenAI key);
   - unresolved questions at the end.
3. Update the Orca worktree comment at checkpoints if the `orca` CLI is available.
