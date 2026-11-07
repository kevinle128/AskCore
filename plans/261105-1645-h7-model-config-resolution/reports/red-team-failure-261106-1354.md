# Red-team review: failure modes of the H7 plan

Date: 2026-11-06. Lens: Failure Mode Analyst (flow tracer). Scope: plan.md, decisions.md, phases 3, 5, 6, 8 (phase 2 read where the flow passes through it). Method: each claim traced to the current code with `rg` and file reads. No build, lint or test ran.

## Finding 1: A models.json-only provider is never "usable", so the TUI and the leader refuse it

- **Severity:** Critical
- **Location:** Phase 5, "Requirements" 5, 6 and 8; Phase 6, "Requirements" 6.4; decisions.md "Extension trace" step "Key".
- **Flaw:** The plan wires the models.json key into `AuthRunner` and `NativeAuthReady` only. Four other gates decide if a provider can serve, and all of them read `auth.Service.Status`, which lists only the static `LoginOptions()` providers. A custom provider (`local`) is never in that list, and a built-in provider that has only a models.json key has state `none`.
- **Failure scenario:** The user adds `local` with `apiKey: "ollama"` as in the extension trace and runs `ask`. (a) `firstUsable` skips every `local` row, so the session starts on another model or on no model. (b) A saved default `local/qwen3-coder@openai-completions` falls back with reason `no credential`. (c) In the leader, `/model` → `local/...` is refused by the mutation guard with `ErrNoAPIKey`. (d) A `usableOnly` model list hides the `local` rows. Only `ask -p` works, because it takes the `spec.Start` path. The plan outcome fails in the main TUI path, and the plan.md acceptance criterion for a models.json provider exercises `ask -p` only, so the TUI and leader path stays untested.
- **Evidence:** `internal/app/module_acp.go:303-314` (`providerUsable` reads `service.Status`); `internal/auth/status.go:54-68` (rows come from `LoginOptions()`, default `StateNone`); `internal/app/default_model.go:60-65,146-148` (start choice and saved default use `usable`); `internal/acp/ask_methods.go:192` (usable filter) and `:263` (`ProviderSetup.Usable` in the setModel guard); `internal/app/provider_setup.go:45,147-149` (`rt.Usable`). The plan names no change to these sites (`rg Usable` over the plan finds only `firstUsable`).
- **Suggested fix:** Make "usable" session-view aware in phase 5: `usable(ctx, scope, provider)` is true when `Status` says so OR the session view has a config key for that provider that resolves without a run (`KeySource` not empty). Pass the scope into `ProviderSetup.Usable` and `choice.usable`. Add a leader test: TUI path, `local` provider with only a file key, start choice, saved default, `setModel` and `usableOnly` list all accept it.

## Finding 2: A new client's `--model` and `--models` are silently dropped by an older leader

- **Severity:** High
- **Location:** Phase 6, "Requirements" 7 and 9, "Validation" ("An older TUI against this leader still works"); plan.md "Rollback" ("an older peer ignores them").
- **Flaw:** The plan checks only one direction (old client, new leader). In the other direction (new TUI, old leader) the old host decodes `_meta["ask.dev/session"]` with plain `json.Unmarshal` into the old struct, so `model`, `thinking` and `models` vanish with no error. The leader replacement does not cover this case: a leader that the user or a supervisor started is never replaced.
- **Failure scenario:** The user runs `ask leader` under launchd (older build), then a newer `ask --model anthropic/claude-x:high`. The session starts on the saved default or the first usable model with the default level. The user believes the request goes to `claude-x` at `high`. Wrong model, wrong cost; only the generic build-mismatch hint prints, and nothing says that the model option was dropped. The same happens after a rollback of phase 6 on the leader side only.
- **Evidence:** `internal/acp/session_options.go:37-40` (plain `json.Unmarshal`, unknown fields dropped); `pkg/protocol/acp_session.go:19-24` (current fields); `internal/leader/README.md:66,68` (a leader started by hand or by a supervisor stays; the client only prints a hint).
- **Suggested fix:** The client must verify the result: when `Model` was sent, compare the session start model (and thinking level) with the requested pattern resolution and fail the start with a clear message when they differ. Better: the host echoes the options it applied (for example `appliedOptions` in the new-session `_meta`, `omitempty`), and the client refuses a start whose echo lacks `model`. Add the reverse-direction test to phase 6 validation.

## Finding 3: A cancelled first caller poisons the shared `!command` run for 30 s in every session

- **Severity:** High
- **Location:** Phase 2, "Requirements" 2 ("Concurrent calls share one run", "Timeout 10 s through the context of the run", 30 s backoff); Phase 5, "Requirements" 10.
- **Flaw:** The shared run uses "the context of the run", which is the request context of the first caller. `sandbox.Run` kills the process and returns `ctx.Err()` when that context ends. The plan does not say that a cancellation is not a failure, so it enters the cache with `failedAt` and the 30 s backoff. The plan also does not say that a waiter honors its own context.
- **Failure scenario:** In a leader with three sessions on a provider with `apiKey: "!op read ..."`, session A sends a prompt; the password manager CLI waits for a touch-ID prompt; the user presses Esc in A. The run is killed, the cancellation is cached as a failure, and sessions B and C (and A's next prompt) get `command ... failed` for 30 s without a new run. A waiter in B that arrives during the run also blocks for up to 10 s even after B is cancelled.
- **Evidence:** `internal/sandbox/run.go:16-22` (on `ctx.Done()` it kills and returns `ctx.Err()`); phase-02 requirement 2 text; `internal/app/module_auth.go:28` (Resolve runs on the request context of each stream).
- **Suggested fix:** Run the command on `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)`, owned by the cache entry, not by a caller. Each caller (first or waiter) selects on `entry.running` and its own `ctx.Done()`. Cache only exit failures, empty output and the 10 s timeout, never the caller's cancellation. Add `TestConfigValueCallerCancelDoesNotPoisonCache` with `-race`.

## Finding 4: After a models.json edit, a session row and its key and headers come from two different files

- **Severity:** High
- **Location:** Phase 5, "Data flow" (`view.KeyFor(m.Provider)`, `view.HeadersFor(m.Provider, m.ID)`), "Requirements" 8; decisions.md M2, guard U6.
- **Flaw:** The session model `m` is a frozen clone (U6 is right to keep it). But the plan resolves its key and headers per request from the live view by provider name only. After a file change, `m.BaseURL` is from the old file and the key, its endpoint and the headers are from the new file. The plan does not define this state, and the phase 5 test `TestModelRefreshKeepsOverrideAndSessionModel` checks only a cache write, not a file edit.
- **Failure scenario:** (a) The user changes `local.baseUrl` from host A to host B and edits `headers.X-Token` for B. The running session still sends to A, now with B's header value (a secret meant for B goes to A), and the file key does not bind (`Endpoint` B ≠ `m.BaseURL` A), so it falls to the env path, where `LookupKey("local")` is empty: `ErrNoCredential` with no hint that the file changed. (b) The user removes the `local` provider: the session keeps the row, every request fails with "no credential", and `models()` marks the row `notInAccount`, which is the wrong reason (it was never in an account list).
- **Evidence:** `internal/app/module_auth.go:28-31` (Resolve with the frozen `model`); `internal/auth/service.go:141-153` (no saved credential → env lookup); `internal/providers/keys.go:76-83` (unknown provider → empty key); `internal/acp/ask_methods.go:204-210` (`NotInAccount = !contains(rows)` for any missing current row).
- **Suggested fix:** Key and header lookup must use the full triple `(m.Provider, m.API, m.BaseURL)` of the session row, and headers apply only when the view row with that triple exists. When it does not exist, fail with a fixed reason `model configuration changed; choose the model again` and add a `ConfigChanged` (or `NotInConfig`) flag to the wire row instead of `notInAccount`. Add a phase 5 test: a session on a custom row, edit `baseUrl` and `headers`, next request does not send the new header to the old host.

## Finding 5: The 401 forget runs in a detached goroutine, so the next request can reuse the stale key and the planned test is flaky

- **Severity:** Medium
- **Location:** Phase 5, "Requirements" 9; "Tests" `TestConfigCommandForgottenAfter401`.
- **Flaw:** `Forget(command)` runs in a goroutine that waits on `stream.Result`. The agent reads the terminal event from `Events()` on another goroutine. Nothing orders the forget before the agent ends the turn and the client sends the next prompt.
- **Failure scenario:** After a key rotation the server answers 401. The user (or the test) sends the next prompt at once. `Resolve` reads the cache before the goroutine has called `Forget`, so the old key goes out again and gets a second 401. The test "the command runs again on the next request" passes or fails by scheduler order and fails more often under `-race`.
- **Evidence:** `internal/providers/stream.go:84-95` (`Result` waits on `done`, independent of the event channel); `internal/app/module_auth.go:47` (the stream returned to the agent is the registry stream; the agent consumes it directly).
- **Suggested fix:** Do the forget synchronously before the consumer can see the end: wrap the registry stream in `AuthRunner` with a small relay (`providers.NewStream` body that forwards items and, on the terminal failure with `Status 401`, calls `Forget` before it emits the terminal item). Then no extra goroutine exists and the test is deterministic. Alternatively mark the entry "suspect" in `Resolve` from the last binding failure, but the relay is simpler.

## Finding 6: A per-request view rebuild puts file I/O and lock waits on the hot request path

- **Severity:** Medium
- **Location:** Phase 5, "Requirements" 3 ("The view is built per call") and 8 (`Models func() SessionModels` in `AuthRunner`); "Risk" row "Per-call view build is slow" (Low x Low).
- **Flaw:** Each model request (also each retry attempt and each `NativeAuthReady`) calls the view function, which calls `Current()` (takes `loadMu` and stats two files), stats and maybe reads the project file under the `ModelsFiles` mutex, and builds a full `NewModelSet` (merge, account filter, sort) only to read one key. `Load` holds `loadMu` while it calls `service.CurrentAccountKey` for every catalog provider, which reads the credential store. A request then waits for a concurrent `Load` of another session or of a refresh write.
- **Failure scenario:** In a leader with several sessions, a refresh writes the cache; every request that starts during the rebuild blocks on `loadMu` behind store reads for all providers. The risk table rates this Low x Low with no measure, and the mitigation ("measure with the existing refresher tests") measures `Load`, not the request path.
- **Evidence:** `internal/app/model_refresh.go:187-213` (`Current` → `reloadIfChanged` → `Load` under `loadMu`); `internal/app/model_refresh.go:258-260` (`CurrentAccountKey` per provider inside the build); `internal/app/model_refresh.go:272-273` (two `NewModelSet` per build).
- **Suggested fix:** Memoize the session view by `(snapshot pointer, project file stamp)`; the request path reads the snapshot pointer and the key table only (`atomic.Pointer`, no `Current()`), and only `models()`, `setModel` and session start do the mtime check (D14 says exactly these points). This also removes the request-path dependency on `ForgetAll` timing.

## Finding 7: `saveDefault` writes a project-layer model into the user settings file

- **Severity:** Medium
- **Location:** Phase 5, "Requirements" 5 and 6; decisions.md M13; Phase 6, "Requirements" 6.3.
- **Flaw:** With M1, `setModel` can select a row that only the project file defines. `saveDefault` then calls `SetDefaultModel`, which writes the user file. The plan does not restrict this.
- **Failure scenario:** In trusted project A the user picks `local/qwen@openai-completions` (from `A/.ask/models.json`, host A) with "save as default". (a) In every other project the start falls back with `not in the catalog`. (b) In project B, whose file also defines a `local` provider (a common name) at another host with its own key, the saved user default silently resolves to B's `local/qwen`: a different server than the one the user chose.
- **Evidence:** `internal/app/module_acp.go:207-213` (`SaveDefault` → `layered.SetDefaultModel`); `internal/settings/settings.go:569-583` (writes the user layer; only a project shadow is allowed); `internal/acp/ask_methods.go:278-282` (save after any successful `setModel`).
- **Suggested fix:** Use the phase 3 provenance: when the row origin is `project`, refuse the save with a fixed reason (`ACPDefault...` "model is defined by the project file"), or write it to the project settings layer. Add a test in phase 5.

## Finding 8: Cycling to a scope entry with a level takes two RPCs and can leave a half-applied state

- **Severity:** Medium
- **Location:** Phase 8, "Requirements" 3 ("one `SetModel` (and the thinking level of the entry when the design says so)"); Phase 6, "Requirements" 7 (`ACPScopedModel{ModelID, ThinkingLevel}`).
- **Flaw:** `ACPModelRequest` has no level field, so an entry such as `gpt-5.5:low` needs `setModel` and then `setThinking`. The two calls are not atomic: a prompt can start between them, the second call can be refused (busy session), and a second key press while the first pair is in flight can interleave with it. Requirement 2 also lets a press trigger a `Models` request, so two quick presses can start two list requests and two switches.
- **Failure scenario:** The user presses the cycle key and then Enter at once. The prompt runs on the new model with the old level (for example `high` on a model that the user scoped to `low`, a cost surprise). Or `setThinking` is refused and the footer shows the new model with the wrong level, against requirement 3 ("A refusal changes nothing").
- **Evidence:** `pkg/protocol/acp.go:60-70` (`ACPModelRequest` has no level; `ACPThinkingRequest` is separate); `internal/acp/ask_methods.go:235-237` (the session guard is held for one call only).
- **Suggested fix:** In phase 6 add `ThinkingLevel ThinkingLevel \`json:"thinkingLevel,omitempty"\`` to `ACPModelRequest`, applied inside the same guard as the model, with the same reverse-compat check as finding 2 (the result echoes the applied level). In phase 8 drop presses while a cycle request is in flight and add `TestModelCycleKeyIgnoredWhileSwitching`.

## Finding 9: Rollback and leader replacement lose state that the plan says is safe

- **Severity:** Medium
- **Location:** plan.md "Rollback" ("no data migration exists"); Phase 5 "Rollback"; plan.md "Acceptance criteria" ("A malformed file keeps the last valid copy in a leader").
- **Flaw:** (a) Ask never writes models.json, but phases 5 and 6 let `saveDefault` write a custom or project row id into the user `settings.json`. After a revert of phases 9, 8, 6 and 5, that saved default names a row that no source has. (b) The last valid copy lives in process memory only, and a newer client replaces a client-started leader also when it is busy.
- **Failure scenario:** (a) After the rollback, every session start falls back with `not in the catalog`, and print mode prints the warning on each run, until the user edits `settings.json` by hand. (b) The user breaks `models.json` while the leader runs; a newer `ask` replaces the leader; the new leader has no earlier copy, so every custom row is gone and sessions on custom rows end. The acceptance criterion is true only for a leader that was already running when the file broke.
- **Evidence:** `internal/settings/settings.go:569-583` (`SetDefaultModel` writes the user layer); `internal/app/module_acp.go:207-213`; `internal/app/default_model.go:140-146` (missing row → `not in the catalog`); `internal/leader/README.md:68` (replacement of a busy client-started leader ends all sessions).
- **Suggested fix:** State in the rollback section that a saved default on a custom row becomes a fallback, and keep the fallback text helpful ("defined in models.json; that file is not read by this build" is not possible after revert, so document the manual fix). Narrow the acceptance wording to "a leader that loaded a valid copy before the file broke", and have the leader replacement test in phase 9 show the documented result (custom rows missing plus a file problem row).

## Unresolved questions

- Does the phase 7 design let the cycle key act while the session runs (Pi allows a model change for the next turn)? If yes, finding 8 needs a queue rule, not a drop rule.
- Is `Authenticate` expected to run `!command` values (phase 5 requirement 5 says "user layer only")? A command there would add a 10 s stall to ACP `authenticate`.
