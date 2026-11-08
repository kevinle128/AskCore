# Acceptance: account model discovery

Date: 2026-11-01 (Asia/Saigon). Plan: [account model discovery](../261028-1310-account-model-discovery/plan.md).
Build: working tree on `develop` (not committed). No account id, token, key or header is in this report.

## Method

- One named `ask` binary, made from `./cmd/tui`. Commands that need no leader ran from that binary too.
- Temp homes under `/private/tmp` (mode 0700): one with a copy of `auth.json` and `settings.json` (ChatGPT sign-in), one with `settings.json` only (the OpenAI key from the environment). `~/.ask` was not used. The temp homes are deleted.
- The resolved list was read through the real path: `ask acp` stdio, `_ask/session/get_available_models`. This is the list that the leader and the TUI `/model` read. `ask acp` has no provider-setup filter, so the list is the full `ModelSet`.
- Live inference: 6 requests ("Reply with OK", `--max-turns 3`): 2 on the key route and 4 on the ChatGPT route. Live list requests: about 8 (refresh runs and leader starts) and 3 raw probes for the ChatGPT field names.
- Not used: `anthropic-oauth` (vendor terms), xAI (no credential), Token Plan generation (plan not purchased).

## Live results

| Run | Result |
|---|---|
| OpenAI key, `ask models refresh --provider openai` | `openai 29 models known`, `metadata updated`, exit 0. The vendor list has 134 ids; 29 pass the family rules. |
| OpenAI key, resolved list | 29 `openai` rows. 0 rows match `tts`, `embed`, `image`, `realtime`, `transcribe`, `audio`, `whisper`, `moderation`, `search`, `sora`, `deprecated`. 0 rows have an unknown context window. Families: gpt-4.1, gpt-4o, gpt-5, gpt-5.1/5.2/5.4/5.5 (with mini, nano, pro), gpt-5.3-codex, gpt-5.6, gpt-6, gpt-6.1, o3. |
| OpenAI key, `gpt-5.4-nano` (not a built-in row) | thinking off: reply `OK`. Thinking high: reply `OK`. |
| ChatGPT sign-in, list endpoint | `GET https://api.openai.com/v1/models`: HTTP 200, about 656 KB. Top-level field: `models` only (no `object`, no `data`). 10 items; 8 have `visibility = list`, 2 have `hide`. The items carry a `slug` and `display_name`, plus many more fields (for example `context_window`, `supported_reasoning_levels`, `available_in_plans`, `supported_in_api`, `priority`); Ask reads `slug` and `visibility`. |
| ChatGPT sign-in, `ask models refresh --provider openai` (key unset) | `openai 8 models known`, `metadata updated`. The cached ids equal the 8 `visibility = list` slugs (compared by program). The 2 hidden slugs are not in the cache or the list. |
| ChatGPT sign-in, resolved list | 8 `openai` rows (gpt-5.6 x3 with thinking, gpt-6 and gpt-6.1 x5 with `Reasoning: false`). 0 banned patterns, 0 unknown windows. |
| ChatGPT sign-in, `gpt-5.6-luna` and `gpt-6-luna` | thinking off and high: 4 requests, all reply `OK`. No 4xx or 5xx. |
| Offline (proxy to a closed port), stale cache | `ask models refresh`: `openai 29 models unknown`, `metadata failed`, exit 0. The cache still holds the 134 ids. The resolved list is unchanged (29 openai rows). The failed entry gets a `retryAfter` of 5 min, and a leader start inside that delay does not refetch it. |
| Leader start, stale cache (17 h) | The `ask connect` start refreshed the stale OpenAI entry and the overlay within seconds; the entry younger than 2 h was not refetched. |
| Logout, `ask auth logout --provider openai --method openai-chatgpt` | Cache accounts: none. With no env key, the list shows the catalog rows of the provider (32 `openai` rows, access unknown). With the env key, `ask models refresh` gives the 29-row key list. |
| Token Plan | The key has no state in the list: `unknown`, no list request (no list function). The 2 built-in rows stay. Generation: not tested (403 unpurchased). |
| Secret scan | See [Secret scan](#secret-scan). |

Finding for the ChatGPT list: the response is `{"models": [...]}`, as the OpenAI guide says. Ask filtered it correctly. The two hidden slugs show that the filter matters.

## Success criteria

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | OpenAI key: chat models of the account, more than 1 row, no tts, embedding, image, realtime, deprecated | verified live | 29 rows from 134 ids; 0 banned-pattern rows. Tests: `TestFamilyRuleMatch`, `TestHiddenFamiliesStayHidden`, `TestModelSetDropsListedIDsWithNoShownRule`. |
| 2 | ChatGPT: rows are the `visibility == list` slugs that pass the rules | verified live | 8 listed of 10; cache ids equal the 8; 8 rows. Test: `TestListModelsChatGPTVisibility`. |
| 3 | Anthropic (OAuth or key): rows from `/v1/models`, all pages, pass the rules | verified by test; not tested live | No Anthropic API key. `anthropic-oauth` has no list by decision. Tests: `TestListModelsAnthropicPaging`, `TestListModelsAnthropicPagingBounds`, `TestListModelsMethodsWithoutList`. Without a list, the 17 catalog rows show (access unknown). |
| 4 | Every shown row matches a family rule; Anthropic and Completions rows send `max_tokens` above 1 | verified by test | `TestFamilyRulesAreComplete`, `TestRequestLimits`, `TestModelSetFallbackRow`, `TestStreamMaxTokensPresent`. Live: 6 prompts on rule rows returned `OK`. |
| 5 | Family rule, no context metadata: `?` in selector and footer; no rule: not shown | verified by test | `TestModelSelectorUnknownWindow`, `TestFooterUnknownWindowAfterChoice`, `TestFooterUnknownWindowIsMuted`, `TestModelSetDropsListedIDsWithNoShownRule`. Live: 0 rows with window 0 in all lists. |
| 6 | Refresh at leader start (cache older than 2 h), after login, after logout, `ask models refresh`; one refresh for each account key at a time | verified live (leader start, `ask models refresh`, logout); verified by test (login, single flight) | Leader start refreshed a 17 h entry. Logout deleted the entries. Tests: `TestModelRefreshConcurrentCallersShareOneFetch`, `TestModelRefreshDropsResultAfterLogout`, `TestModelRefreshExplicitRefreshIgnoresTTL`. Login was not run live (no new sign-in by decision). |
| 7 | Failed refresh keeps the last good list; offline still gives a list | verified live | Offline run: `unknown` and `failed`, same 29 rows, cache intact. Test: `TestModelRefreshFailureKeepsListAndWaitsRetryDelay`. |
| 8 | Refresh never changes the running session model; header keeps window and thinking; saved default that left the list falls back with "not in account list" | verified by test | `TestStartFallbackNotInAccount`, `TestFallbackNoticeNotInAccount`, `TestModelSelectorNotInAccount`. Not run live (needs a list that drops the model). |
| 9 | Cache file mode 0600 and no secret | verified live and by test | Live: mode 0600 in both temp homes; secret scan below. Tests: `TestModelCacheFileModes`, `TestModelCacheHoldsNoRawOrPlainHashedSecret`, `TestAccountKeyHoldsNoSecret`. |
| 10 | `go test ./...` sends no request to models.dev or a vendor list endpoint | verified by test and run | The tests use fake transports (`fakeVendor`); the zero `ModelRefresh` reads the cache only. A run of `internal/app`, `internal/auth`, `internal/providers/...`, `internal/settings`, `internal/acp` and `cmd/tui` with `HTTPS_PROXY` and `HTTP_PROXY` set to a closed port passed. |
| 11 | `go vet`, `go test`, lint, drift test pass; no `time.Sleep` or ticker in product code | verified | `go vet ./...` exit 0. `go test ./...` exit 0. Lint: 0 issues. `TestReferenceDocs` passes with and without `ASK_UPDATE_DOCS`. `python3 e2e/tui/test-assertions.py`: 24 tests OK. `python3 e2e/tui/run.py --backend alacritty`: all cases pass. `rg` finds no `time.Sleep`, `NewTicker` or `time.Tick(` in the product files of this change. |

## Docs and e2e changes in this step

- `e2e/tui/real_provider_scenarios.py`: `pick_model` selects an exact id. It types the id, reads the selected row (the `❯` mark inside the box border), moves down until the id is equal, and fails when no row matches. The caller `"claude-sonnet"` became `"claude-sonnet-4-6"`; the other callers already used exact ids. The file parses; it was not run with real providers.
- `docs/reference/providers.md` (generated): the section "Generated rows" no longer says that Ask does not use the rows. The text is in `cmd/tui/reference_docs_test.go`.
- `internal/providers/README.md`: new source files, the three layers, the family-rule rule, and the snapshot update command.
- `internal/providers/generated_catalog.go`: a stale comment corrected.
- `e2e/tui/README.md`: one sentence for `pick_model`.
- Already complete before this step: `docs/cli.md` (`ask models refresh`, `ASK_DISABLE_MODEL_REFRESH`), `AGENTS.md` (command list), `CHANGELOG.md` (`[Unreleased]`), `internal/auth/README.md`, `internal/settings/README.md`, `internal/app/README.md`. No new doc file, so `docs/README.md` needs no change.

## Secret scan

The scan looked for 18 values (the environment keys, and the access, refresh and id tokens of the sign-ins) in 259 files: both temp homes (cache, settings, lock and log files; the copied `auth.json` files were excluded), the scan and helper scripts, every file changed in the working tree, and this report.
Result: 0 matches. Both `model-cache.json` files have mode 0600.

## Open items

- Anthropic list, xAI list and Token Plan generation have no live result (no credential, vendor policy, unpurchased plan).
- Login after a sign-in and the "not in account list" fallback were not run live.
- The fantasy fork drops the effort word of `gpt-6*` rows: they show `Reasoning: false` (thinking has no effect). Both thinking runs on `gpt-6-luna` returned `OK`.
