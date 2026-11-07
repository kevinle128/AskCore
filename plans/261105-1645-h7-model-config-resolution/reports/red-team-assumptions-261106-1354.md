# Red team: assumption audit of the H7 plan

Date: 2026-11-06. Lens: Assumption Destroyer (scope auditor). Revision: `develop` at `97c3052`.
Files read: `plan.md`, `decisions.md`, phases 2, 3, 4, 9 (and phases 5 and 6 for cross-checks).
Fixed user decisions (Q1, Q2, Q3, QA, QB, QD) are not questioned here. Findings attack only the claims that the plan makes about the code.

## Finding 1: A models.json key cannot pass `keySnapshot` for a custom provider, so the extension trace fails

- **Severity:** Critical
- **Location:** Phase 2, section "Data flow" (`models.json key ... -> keySnapshot(source config)`) and requirement 4 (`--api-key ... is accepted for a row whose endpoint the config key names`); decisions.md "Extension trace", step "Key".
- **Flaw:** The plan routes the config key through the existing `keySnapshot`. `keySnapshot` accepts a key only when a registered `api-key` method binds `(provider, api, endpoint)` or when the row is one of three hard-coded OpenAI/Token Plan endpoints. A file-defined provider such as `local` has no registered method and is not in the hard-coded list. The same gate rejects `--api-key` for that row.
- **Failure scenario:** `ask -p --model local/qwen3-coder` with `apiKey: "ollama"` in `<ASK_HOME>/models.json`: `Resolve` reaches `keySnapshot(m, "ollama", "config")`, `registered` and `known` are both false, and the run fails with `ErrMethod: local/api-key`. `TestPrintLocalProviderFromModelsFile` and the acceptance criterion "no Go change for that provider" fail. Phase 1 makes `apiKey` required for a custom provider with models (`phase-01-models-file-loader.md:37`), so every custom provider hits this gate.
- **Evidence:** `internal/auth/service.go:247-258` (`_, registered := s.method(m, "api-key")`, `known := ...OpenAIURL... TokenPlanMessagesURL... TokenPlanCompletionsURL`, `if !registered && !known { return ErrMethod }`); `internal/auth/service.go:139-140` (override also goes through `keySnapshot`).
- **Suggested fix:** Add a separate `configSnapshot(m, key, ck)` that checks only `ck.Endpoint == m.BaseURL` and never consults the method table, and state in phase 2 that override keys for a file-defined endpoint use the same check. Add `TestResolveConfigKeyForFileDefinedProvider` in phase 2 (a provider with no registered method), so the gap shows before phase 9.

## Finding 2: `Store.Read` rejects custom provider ids before the config key is tried

- **Severity:** High
- **Location:** Phase 2, section "Data flow" (saved credential before models.json key) and requirement 3; phase 1 (provider id rules are not stated).
- **Flaw:** `Resolve` reads `auth.json` for `m.Provider` before any later source. The store refuses any provider name that is not `[a-z0-9-]`, and it returns an error, not an empty credential. The plan keeps this read in the second position for every row, and no phase restricts the characters of a models.json provider id (phase 1 requirement 3 checks `baseUrl`, `apiKey` and `api` and takes the built-in id list, but states no id character rule: `phase-01-models-file-loader.md:36-41`). Phase 4 also compares provider ids case-insensitively, so the plan expects mixed-case ids to exist.
- **Failure scenario:** A user writes `"providers": { "LMStudio": {...} }` or `"my_vllm"`. Every request fails with `ErrUnsafeStore: provider` from `Store.Read`, and the file key is never used. The error text says the auth store is unsafe, which sends the user to the wrong place.
- **Evidence:** `internal/settings/auth.go:100-109` (`validProvider` allows only `-`, `a-z`, `0-9`); `internal/settings/auth.go:362-364` (`!validProvider(provider)` returns `ErrUnsafeStore`); `internal/auth/service.go:141-144` (the read error returns at once).
- **Suggested fix:** Phase 1 validates a provider id with the same rule as `validProvider` and makes a bad id a provider-level problem. Or phase 2 skips the saved-credential read for a provider that is not a valid store name. Add a test for each.

## Finding 3: Colon-tagged ids (the Ollama format of the extension trace) conflict with the parse-time level error, and the `/` split hides vendor-prefixed ids

- **Severity:** High
- **Location:** Phase 4, requirements 1 and 3; phase 6, requirement 2 and the `TestArgs` row `--model x:hgih` (error).
- **Flaw:** Phase 4 says a non-level suffix stays part of the id, and `ErrUnknownLevel` comes only from resolution when no exact row exists. Phase 6 puts `--model x:hgih` as an error row in `TestArgs`, which runs `parseArgs` only, with no rows. If the implementer follows phase 6, every id with a colon fails before resolution. Ollama model names use `name:tag` (`qwen3-coder:30b`, `llama3:8b`), which is the extension target in decisions.md. Phase 4 requirement 1 also splits the provider at the first `/` with no row check, as `parseModelID` does today. Resolve rule 1 saves an exact bare id such as `qwen/qwen3-coder`, but the alias and fuzzy steps (rules 4 and 5) run on the split form, so `--model qwen/qwen3-coder-2507` or `--model qwen/coder` looks in a provider `qwen` that does not exist.
- **Failure scenario:** `ask -p --model local/qwen3-coder:30b` exits 1 at argument parsing with "unknown level 30b", although the row exists. With phase 4 alone and a row `qwen3-coder:30b`, `--model local/qwen3-coder:7b` (a typo in the tag) reports "unknown level, valid words off..max", which is a wrong diagnosis.
- **Evidence:** `cmd/tui/args_test.go:16,85` (`TestArgs` calls `parseArgs(tt.argv)`, no model rows); `cmd/tui/args.go:105` (`--model` is stored as a plain string today, so the plan moves validation); `internal/acp/ask_methods.go:121-123` (first-`/` split that phase 4 copies).
- **Suggested fix:** Phase 6 validates the level only after `ResolvePattern`; replace the `TestArgs` row with a `runWithDependencies` row. Report `ErrUnknownLevel` only when the suffix is close to a level word, else `ErrUnknownModel`. Add `local/qwen3-coder:30b` and an alias/fuzzy `vendor/id` row to `TestResolveModelPattern`, and treat a first segment that is not a known provider as part of a bare id in every resolve step.

## Finding 4: The kept Token Plan demo branch bypasses the grammar, the ModelSet and models.json

- **Severity:** High
- **Location:** decisions.md M4 ("The provider-name switch stays only for `faux` and the Token Plan demo path"); phase 5 requirement 11; phase 6 requirement 1 (`--model` keeps the raw string).
- **Flaw:** For provider `alibaba-token-plan`, `openProvider` calls `tokenPlanStream(o.model, ...)`, which builds a row with `anthropic.ModelFor(id)`. `ModelFor` accepts any id and always gives the Anthropic Messages row. It never sees `ParsePattern`, `ResolvePattern`, `@api`, `modelOverrides`, a user `baseUrl` (M14), or the auth service. The catalog has the same Token Plan id on two APIs, so D10 says a bare id is ambiguous, but this branch silently picks Messages.
- **Failure scenario:** `ask -p --model alibaba-token-plan/qwen3.7-max:high` sends the model id `qwen3.7-max:high` to the API (the level suffix is not removed), and the API refuses it. `--model alibaba-token-plan/x@openai-completions` silently uses Messages. A user `modelOverrides` entry for a Token Plan row works in the TUI and the leader but not in `ask -p`.
- **Evidence:** `cmd/tui/headless_faux.go:82-83` (`case anthropic.ProviderID: return tokenPlanStream(o.model, ...)`); `internal/providers/anthropic/provider.go:22` (`ProviderID = providers.ProviderTokenPlan`); `internal/providers/anthropic/provider.go:50-59` (`ModelFor` sets `m.ID = id` for any id); `cmd/tui/headless.go:390-392` (the default `medium` level applies only on this branch).
- **Suggested fix:** Route Token Plan through the same `CachedModelSet` plus layers path as the other providers, and keep only `faux` on the switch. If the demo branch must stay, phase 6 states that the branch receives the resolved `Model` and level, and a print-mode test covers `alibaba-token-plan/<id>:high` and `@openai-completions`.

## Finding 5: The account filter runs before the provider `baseUrl` change, so rows of a re-pointed built-in provider are filtered by the wrong account

- **Severity:** Medium
- **Location:** Phase 3, section "Data flow" (step 2 account filter, then step 3a provider `baseUrl`) and requirement 5 (M14).
- **Flaw:** The account list of a built-in provider is the list of the current saved credential at the default host. Phase 3 filters rows with that list first and changes `baseUrl` afterwards. After the change, M3/QA skip the saved credential for the new endpoint, so the list that removed rows belongs to a credential that the rows no longer use.
- **Failure scenario:** A user with a saved OpenAI OAuth sign-in points `openai` at a company proxy in the user models.json, with a proxy key. The ChatGPT account list hides every row that the subscription does not list, although the proxy serves them. `/model` shows fewer rows than the proxy supports, and `--model openai/<hidden id>` fails as unknown.
- **Evidence:** `internal/providers/model_set.go:55-58` (rows not in a known account list are deleted); `internal/app/model_refresh.go:257-268` (the list comes from `service.CurrentAccountKey` for the provider, independent of the row endpoint); `internal/auth/service.go:121-128` (the method binds to `m.BaseURL`, so the changed row no longer binds).
- **Suggested fix:** In phase 3, do not apply the account view of a provider whose `baseUrl` a layer changed (treat its access as unknown). Add `TestUserBaseURLIgnoresDefaultHostAccountList`.

## Finding 6: A trusted project `apiKey` on an existing provider wins over the user's env key

- **Severity:** Medium
- **Location:** Phase 2, section "Data flow" (models.json key before env); phase 5 requirement 4 ("A project `apiKey` on an existing provider is allowed").
- **Flaw:** Q2 protects only one direction: a saved credential never goes to a host that the project chose. The opposite direction is open. Today the env key is used whenever no saved credential exists. Under Q1 order the config key comes before env, and phase 5 lets a project layer set the config key of `openai` or `anthropic`. A project file in a cloned repository then replaces the user's own env key with a key of the repository owner, on the real vendor host.
- **Failure scenario:** A user with only `OPENAI_API_KEY` in the environment trusts a cloned repository. Its `.ask/models.json` sets `"openai": {"apiKey": "sk-attacker"}`. Every prompt and file excerpt of the session goes to the attacker's account (usage logs and stored responses on the vendor side), with no sign in the UI except `KeySource` in `ask settings show`. This is plausible only after a trust decision, so it is not Critical. It depends on product intent.
- **Evidence:** `internal/auth/service.go:145-155` (env is the source whenever no saved credential exists); phase 5 requirement 4 (`phase-05-session-model-view.md:49`).
- **Suggested fix:** Ask the user whether a project `apiKey` may apply to a provider that exists outside the project file. A safe default: a project `apiKey` binds only to a provider that the project file itself defines, and each refusal is a problem row (same shape as the `baseUrl` refusal).

## Finding 7: "Malformed config keeps the last valid copy" in an `ask acp` process has no real-entry test

- **Severity:** Medium
- **Location:** plan.md acceptance criterion 4 ("in a leader and an `ask acp` process"); phase 9, section "Tests (all offline)"; phase 5 tests.
- **Flaw:** Phase 9 proves the last-valid rule only through a leader (`TestLeaderKeepsLastValidModelsFile`). The ACP test `TestACPProjectModelsFileNeedsTrust` checks trust only. Phase 5 has `TestBadUserModelsFileKeepsLastValidRows` in `internal/app`, which does not run the `ask acp` entry, and `ask acp` builds its composition through `internal/app.ACPModule`, not the leader path. Phase 9 validation says each criterion has a test, which is not true for this half.
- **Failure scenario:** The ACP composition builds the model view once without the last-valid memory, or it re-reads the file on each `session/new` with no copy. A malformed file then removes the custom rows from a running `ask acp` session, and CI stays green.
- **Evidence:** `cmd/tui/acp_e2e_test.go:434,523` (real `ask acp` e2e helpers exist, so the test is cheap); `internal/app/module_acp.go:132-137` (separate ACP model composition, cited by M1).
- **Suggested fix:** Add `TestACPKeepsLastValidModelsFile` to phase 9 on the `ask acp` e2e helpers: list models, break the file, list again, and check the custom row and the diagnostic.

## Finding 8: M3 says a non-binding source is skipped, but phase 2 requirement 4 returns `ErrMethod`

- **Severity:** Medium
- **Location:** decisions.md M3 ("A source that does not bind is skipped, and the next source is tried"); phase 2 requirement 4 ("In every other case the result stays `ErrMethod`").
- **Flaw:** The two texts give different results when a saved credential does not bind and no config key exists. Today env is never read when any saved credential exists. An implementer who follows M3 changes current behavior and breaks H7a tests; one who follows phase 2 leaves M3 false. `KeySource` (phase 2 requirement 6) must report one of them, and `ask settings show` then shows a source that `Resolve` does not use if the two disagree.
- **Failure scenario:** The divergence needs a row where the saved OAuth method does not bind but the registered `api-key` method does, for example a saved ChatGPT or xAI sign-in plus `OPENAI_API_KEY`/`XAI_API_KEY`, and a row at the default API-key endpoint that the OAuth method does not cover. `KeySource` (written from M3) says `env:<NAME>`; `ResolveWith` (written from requirement 4) fails with `ErrMethod`. The user sees a ready key in `ask settings show` and a failed request. For a user `baseUrl` change, no method binds, so both texts give a failure; there the gap is only the error kind (`ErrMethod` against `ErrNoCredential`). If the catalog has no such row today, the severity is Low, but the contradiction remains for the next row.
- **Evidence:** `internal/auth/service.go:145-165` (env only when `c.Method == ""`; a saved method that does not bind returns `ErrMethod`); `internal/app/auth_native.go:22-30` (one `api-key` method per `(provider, api, endpoint)`, independent of the OAuth methods).
- **Suggested fix:** Make M3 match phase 2 requirement 4, and require `KeySource` and `ResolveWith` to share one source-selection function, with a test that compares them on the same inputs.

## Checked and not a finding

- Import rules: phase 3 types live in `internal/providers` with no `settings`/`auth` import, phase 2 code lives in `internal/auth`, which may import `settings` (`.golangci.yml:155-170`). The plan placement is correct.
- `NewModelSet` signature and callers match the plan (`internal/providers/model_set.go:46`, `internal/app/model_refresh.go:272-273`); a variadic layer argument keeps them compiling.
- `TestLeaderRefreshKeepsSessionModel` is meaningful: a leader re-reads `model-cache.json` that another process wrote (`internal/app/model_refresh.go:185-213`).
- `Model.Headers` exists and both wires send it (`internal/providers/model.go:61`, `internal/providers/openai/profiles.go:129-131`, `internal/providers/anthropic/provider.go:249-250`), so per-request headers need no wire change.

## Unresolved questions

- Finding 6 depends on product intent: may a trusted project key replace the user's env key for a built-in provider?
- Finding 8: does the current catalog have an OpenAI or xAI row that an `api-key` method binds and the saved OAuth method does not? Not checked inside the tool budget.
- Not raised as a finding: custom rows default to `Reasoning false` (phase 3) and the caller clamps the level (phase 4), so `local/x:high` silently runs with thinking off. No phase maps a warning for this.
