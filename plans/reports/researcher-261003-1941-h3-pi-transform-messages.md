# H3 research: Pi `transformMessages`, replay and pairing

Date: 2026-10-03. Source: Pi commit `2bbfcca43`. Read-only. Code was read directly; the caller list comes from grep over `packages` (excluding `node_modules`, `dist`).

Path abbreviations: **AI** = `packages/ai/src`, **AIT** = `packages/ai/test`, **CA** = `packages/coding-agent/src`, **AG** = `packages/agent/src`. **L** = Ask `master-2`. **RM** / **INV** = `roadmap.md` / `inventory-harness.md` in `plans/260930-2254-pi-feature-inventory-go-roadmap/`.

## 1. Summary

1. Callers: Anthropic (`AI:api/anthropic-messages.ts:1057`), Bedrock (`bedrock-converse-stream.ts:957`), OpenAI Completions (`openai-completions.ts:1220`), Google (`google-shared.ts:200`), OpenAI Responses (`openai-responses-shared.ts:179`), Mistral (`mistral-conversations.ts:142`).
2. `transformMessages` does not strip surrogates, does not drop empty text, and does not filter empty error messages. `sanitizeSurrogates` and empty-text filtering run later, in each adapter's serializer. Errored and aborted assistants are filtered by `stopReason`.
3. INV H-PROV-10 (line 106) and H-PROV-21 (line 117) are wrong: foreign thinking becomes plain text with no `<thinking>` tags (`AI:api/transform-messages.ts:113-116`; on purpose, `AI:../CHANGELOG.md:1913`, `:2055`).
4. INV H-PROV-10 lists surrogate stripping as part of `transformMessages`. That is wrong; the file does not import `sanitizeSurrogates`.
5. Local Ask has no `transformMessages`, no sanitizer and no pairing code. Every Pi field the function reads already exists in the local types (section 6).

## 2. `transform-messages.ts` branch by branch

**Placeholders (12-13).** User: `(image omitted: model does not support images)`. Tool result: `(tool image omitted: model does not support images)`.

**`replaceImagesWithPlaceholder` (15-33).** Each image becomes one text block with the placeholder. Consecutive images collapse to one placeholder (`previousWasPlaceholder`, 17-26). A real text block equal to the placeholder also sets the flag (29), so a following image is swallowed (quirk).

**`downgradeUnsupportedImages` (35-57).** If `model.input` includes `image`, pass through (36). User messages are rewritten only when `content` is an array (41); a string user message is not touched. `toolResult` content always gets the tool placeholder (48-53). System and assistant messages are not touched. An image-only user message on a non-vision model becomes one non-empty placeholder text block.

**Entry (64-75).** Signature `transformMessages(messages, model, normalizeToolCallId?)`; callback `(id, model, sourceAssistant) => string` (67). `content == null` becomes `[]` (73; changelog `:571`, test `lax-message-content.test.ts`). Then the image downgrade (74).

**First pass (77-156).**
- System and user pass through (79-81).
- A `toolResult` is re-keyed only if `toolCallIdMap` already holds its id and the new id differs (84-90). The map fills while walking forward (a result before its call is not re-keyed). The map is global; a reused id overwrites the earlier mapping.
- "Same model" = `provider`, `api` and `model` all equal to the target (95-98). `responseModel` is not used (proxy relabel still counts as same model; `AIT:anthropic-sse-parsing.test.ts:~136-170`, issue #9188).

Thinking blocks (101-117), in order:
1. `redacted`: keep if same model, else drop (104-106).
2. Same model and non-empty `thinkingSignature`: keep, also with empty thinking text (109).
3. Empty or whitespace thinking: drop (111).
4. Same model, unsigned, non-empty: keep (112). The adapter later turns it into text (`anthropic-messages.ts:1339-1359`).
5. Cross-model, non-empty: `{type:"text", text: block.thinking}`, no tags (113-116). Not merged with neighbours.

Text blocks (119-125): same model kept; cross-model rebuilt as `{type, text}` (drops `textSignature`). Empty text not filtered here.

Tool calls (127-145): cross-model with `thoughtSignature` -> deleted from a copy (131-134). Cross-model with a callback -> `normalizeToolCallId`, record in map if changed (136-142). A same-model call is never normalized. `namespace` never stripped. Unknown block types pass (147).

**Second pass (158-235).**
- State: `pendingToolCalls`, `existingToolResultIds`, `heldSystemMessages` (160-166).
- `closePendingToolCalls` (167-186): for each pending call with no result, push `role:"toolResult"`, `toolCallId = tc.id` (normalized), `toolName = tc.name`, content `[{text:"No result provided"}]`, `isError:true`, `timestamp: Date.now()` (171-178). Then clear state and flush held system messages (181-185).
- Assistant (191-212): first close previous pending calls (193), so synthetic results land after real results, just before the next assistant. Then skip the message if `stopReason` is `error` or `aborted` (201-203): its calls are not tracked and get no synthetic results. Otherwise record its calls as pending (206-210).
- Tool result (213-215): add id to the seen set, push unconditionally. **Orphan results are not dropped or de-duplicated.** Changelog `:1785` claims more than the code does.
- System (216-221): held while calls are pending, else pushed.
- User (222-225): closes pending calls first. A result after a user message becomes an orphan next to the synthetic one.
- Other roles pushed (226-228). End of list: `closePendingToolCalls()` (232).

## 3. `sanitize-unicode.ts`

- `sanitizeSurrogates` (`AI:utils/sanitize-unicode.ts:21-25`) removes an unpaired high (U+D800-DBFF) or low (U+DC00-DFFF) surrogate. Valid pairs stay.
- Called in adapters, not in `transformMessages`. Anthropic call sites: `anthropic-messages.ts:148, 156, 1101, 1110, 1267, 1288, 1296, 1330, 1352, 1357, 1363` (system, user text, assistant text and thinking, tool result text). Tool call arguments are not sanitized.
- Go caveat (unverified): Go strings cannot hold lone surrogates; `encoding/json` decodes a lone `\ud83d` into U+FFFD. The Go rule must be chosen.

## 4. Anthropic call site

- `buildParams` calls `transformMessages(context.messages, model, normalizeToolCallId)` at `:1057`; leading system message sliced off (1058).
- `normalizeToolCallId` (1220-1222): `id.replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 64)`. Collisions are possible and not handled. JS counts UTF-16 units (astral char -> two `_`); Go by rune gives one.
- `convertMessages` (1238-1425) filters more after the transform:
  - whitespace-only user string skipped (1286-1290); whitespace text blocks filtered, message skipped if empty (1305-1308) — this is the H-PROV-27 "no empty text part" rule, and it is in the adapter;
  - whitespace-only assistant text skipped (1325-1327);
  - redacted thinking -> `redacted_thinking` with `data = thinkingSignature` (1334-1342);
  - empty thinking without signature dropped (1346); unsigned -> plain text, unless `allowEmptySignature` keeps it with `signature:""` (1347-1360);
  - assistant with zero blocks skipped (1378);
  - consecutive `toolResult` messages merged into one user message (1393-1405);
  - system messages held until just before the next assistant (1246-1252).

## 5. Stage 1: `convertToLlm`

- Pi `CA:core/messages.ts:148-196`: `bashExecution` -> user text (dropped if `excludeFromContext`); `custom` -> user message; `branchSummary`, `compactionSummary` -> user text with prefix/suffix; system, user, assistant, toolResult pass. Pi never drops `custom`.
- `CA:core/sdk.ts:272-395` wraps it for `blockImages` (`"Image reading is disabled."`), a separate rule.
- Loop order: `transformContext`, then `convertToLlm` (`AG:agent-loop.ts:390-395`).
- Local `L:internal/providers/convert.go:9-20` keeps system, user, assistant, tool result and drops everything else (custom, raw). This differs from Pi and from INV H-SESS-04 (line 187). Owner phase not decided.

## 6. Local type support (Ask H1)

| Pi field read | Local type | Status |
|---|---|---|
| `AssistantMessage.api`, `provider`, `model` | `L:pkg/protocol/message.go:165-167` | yes |
| `responseModel` | `message.go:168` | yes (must not be used for same-model) |
| `stopReason` error/aborted | `message.go:27-33` | yes |
| Thinking `thinkingSignature`, `redacted` | `L:pkg/protocol/content.go:35-39` | yes |
| Text `textSignature` | `content.go:28-31` | yes |
| ToolCall `thoughtSignature`, `namespace` | `content.go:49-56` | yes |
| `ToolResultMessage` fields | `message.go:183-192` | yes |
| `Model.input` | `L:internal/providers/model.go:13` | yes |
| `Model.api`, `Provider`, `ID` | `model.go:6-9` | yes |
| `Model.compat` (`allowEmptySignature`) | absent | missing (H-PROV-26 data) |
| Null content | `content.go:268` on decode | yes; in-memory nil to verify |
| `normalizeToolCallId` callback | none | missing |
| Clock for synthetic timestamp | none | missing (inject for tests) |
| Value or pointer message variants | `convert.go:12-15` | function must accept both |

Pi returns unchanged messages by reference; the Go port must build new slices for changed messages and never mutate the input.

## 7. Pi tests

1. `AIT:transform-messages-copilot-openai-to-anthropic.test.ts` (4 tests): foreign thinking -> text (>= 2 text blocks); cross-model `thoughtSignature` removed; trailing orphan call `call_123|fc_123` -> synthetic result `call_123_fc_123`, tool `read`, `isError:true`, `No result provided`; two calls with one result -> one synthetic result for `call_2_fc_2`.
2. `AIT:lax-message-content.test.ts`: null content on user, assistant, toolResult -> `[]`, no crash.
3. `AIT:anthropic-sse-parsing.test.ts:~136-170`: signed thinking survives a proxy `responseModel` relabel.
4. `AIT:openai-responses-reasoning-replay-e2e.test.ts`: live keys; skipped without them.

No Pi unit test for: placeholder text/dedup/tool variant, errored/aborted filter, redacted thinking, empty thinking, held system messages, user message closing pending calls, `textSignature` drop, orphan result.

## 8. Changelog (`packages/ai/CHANGELOG.md`)

`:1757` skip errored/aborted (#838); `:1785` orphan results after errored assistant (#812); `:1797` empty error filter (now `stopReason`); `:1913`, `:2055` untagged thinking text; `:1472` redacted_thinking (#1665); `:1136` trailing synthetic results (#3555); `:1163` non-vision placeholders (#3429); `:562` Anthropic keeps empty-text signed thinking (#6457); `:571` null content (#6343); `:861` `allowEmptySignature` (#4464); `:60`, `:121` unsigned thinking replay for OpenCode/Vercel; `:123` relay relabel (#9188); `:77` image-only user message (#9797); `:110` mid-conversation system messages (#9548); `:2035`, `:2151` empty text/thinking and empty assistant filtered.

## 9. Behavior table

| Behavior | Pi file:line | In H3 test list? | Local support |
|---|---|---|---|
| Errored/aborted assistant dropped (after closing prior calls) | `:193,201-203` | yes | yes |
| Synthetic `No result provided`, placement rules | `:167-186,193,224,232` | yes | needs clock |
| Post-abort pairing (D19) | `:158-180` | yes | yes |
| Foreign thinking -> plain text, no tags | `:113-116` | yes | yes |
| Signatures dropped across models | `:109,121-124,131-134` | partly | yes |
| Same model = provider+api+model, not `responseModel` | `:95-98` | no | yes |
| Same-model signed thinking kept, also empty text | `:109` | partly | yes |
| Redacted thinking kept same-model, dropped cross-model | `:104-106` | no | yes |
| Empty/whitespace thinking dropped | `:111` | no | yes |
| Same-model unsigned thinking -> adapter text | `:112`; adapter `:1347-1360` | no | compat missing |
| Tool-call id normalization + result re-key | `:136-142,84-90` | no | missing |
| Anthropic id rule, 64 chars, no collision guard | adapter `:1220-1222` | no | missing |
| Image placeholder, user and tool variants, dedup | `:12-57` | text only | yes |
| Image-only user message, no empty text | adapter `:1286-1310` | no | adapter |
| Null content -> `[]` | `:73` | no | verify |
| Empty text filtering | adapter `:1325-1327` | no | adapter |
| Surrogate stripping | `sanitize-unicode.ts:21-25` | no | rule undecided |
| Held system messages | `:163-166,184,216-221` | no | yes |
| User message closes pending calls | `:222-225` | no | yes |
| Orphan results not dropped | `:213-215` | no | n/a |
| `convertToLlm` maps custom/bash/summary | `messages.ts:148-196` | no | missing |

## 10. Gaps

1. H3 test list lacks: redacted thinking, empty thinking, `responseModel` same-model rule, held system messages, user message between call and result, result re-keying, placeholder dedup and tool variant, null content, `textSignature` drop, orphan results.
2. INV text must change: H-PROV-10/21 (no tags; no surrogate stripping; `stopReason` filter), H-TOOL-21 cite `:158-235`, H-PROV-27 (empty-text rule is in the adapter).
3. Clock and `(id, model, sourceAssistant)` callback do not exist locally.
4. Anthropic normalizer can create duplicate ids; Pi does not guard.
5. Orphan tool results pass through; Anthropic rejects an unmatched `tool_result`. Decide follow-Pi or drop.
6. Local `ConvertToLLM` drops custom messages; Pi maps them. Owner phase not named.
7. Surrogate handling and empty-text filtering are adapter work; the fantasy adapter must repeat them.
8. `Model` has no compat record (`allowEmptySignature`), H-PROV-26 data.
9. Mid-conversation system message rule for Anthropic not stated for H3.

## Open questions

1. Go sanitizer rule: strip U+FFFD, strip invalid UTF-8 only, or nothing?
2. Drop orphan tool results, or keep Pi pass-through?
3. Does the H3 adapter need the system-message hold, or only the leading system message?

Status: DONE_WITH_CONCERNS. GitNexus not used by this lane; surrogate and Bedrock tests only partly inspected.
