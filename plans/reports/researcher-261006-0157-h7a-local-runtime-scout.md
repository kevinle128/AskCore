# H7a local runtime and test scout

Date: 2026-10-06.
Scope: read-only architecture and test inspection for the six proposed H7a phases.
Work context: `/Users/dale/orca/workspaces/AskCore/master-2`.
This report describes the current working tree, including uncommitted H4 work.
No Go test, build, lint, live login, token exchange, or provider request was run.
New file and function names below are implementation proposals, not existing contracts.

## Authority and scope

The [H7a architecture report](xia-261006-0143-h7a-subscription-auth-architecture.md), [roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md), and [H7a detail](../260930-2254-pi-feature-inventory-go-roadmap/phase-h7a-subscription-auth.md) own scope.
The [architecture reference](../../docs/ask-architecture-reference.md) and package READMEs own import rules.
The user selected one saved credential per provider and local-only logout.
Do not add an account picker, multiple saved accounts, or remote revocation.
Keep verified ChatGPT identity, issued-client reuse, scope checks, and safe refresh.
The selected commands are `ask auth login --provider <id>` and `ask auth logout --provider <id>`.
The final plan uses explicit Pi-style method selection; a missing method prompts for selection or fails clearly when noninteractive.
API-key login uses `--method api-key` and private stdin input.
These commands are an Ask adaptation of Pi's auth lifecycle, as [Pi runtime proof section 6](researcher-261006-0157-h7a-pi-runtime-proof.md#6-pi-referenced-headless-command-decision) explains.
H7a does not need SQL, full project settings, full builtin tools, a gateway, a leader, or a full TUI.
The first live subscription prompt is an early checkpoint.
All three providers remain necessary for phase completion.

## Existing request flow

`cmd/tui/main.go:main` calls `run` in `headless.go`.
`run` parses arguments, prints help, selects a mode, starts capture, creates an agent, reads prompts, and installs signal handling.
`newHeadlessAgent` calls `openProvider`, registers tools, creates the wire registry, and calls `agent.New`.
`openProvider` in `headless_faux.go` accepts only faux or Alibaba Token Plan.
The OpenAI wire constructors are registered only when the initial provider is Token Plan.
The direct CLI still rejects `--provider openai`.
`headless_test.go:TestPrint` explicitly expects that rejection and must change when OpenAI becomes a supported CLI target.

`Agent.execute` combines project context and supplied hooks with `pipeline.Compose`.
`loop_stage.go` runs `prepareRequest` before `streamAssistantResponse`.
`prepareRequest` can replace the model and context for that request.
`streamAssistantResponse` transforms the context, converts messages, normalizes the request, calls `apiKey`, and invokes the configured stream.
`apiKey` resolves the final model's provider through `providers.ResolveKey`.
This is the correct location to preserve the legacy override seam before a composed auth runner resolves stored credentials.
The runner must resolve every request, including each request after a tool result.
Do not resolve only at process start.

`Agent.SetModel` validates the target, checks idle state, resolves an untyped API key, rejects an empty key, then stores the model and clamped thinking level.
This key-only check is a second auth entry point.
It will reject a valid saved OAuth target unless it uses the same injected readiness rules as the runner.
Keep its second idle check after resolution because another run can start during resolution.
Failed readiness must leave model, thinking level, and messages unchanged.

## Existing test inventory

Counts are top-level `Test*` functions in direct package files, excluding `TestMain`.
They are source counts, not test execution or subtest counts.

| Owner | Test files | Test functions | Relevant baseline |
|---|---:|---:|---|
| `internal/settings` | 0 | 0 | Only `README.md` and `doc.go` exist. |
| `internal/auth` | 0 | 0 | The package does not exist. |
| `internal/providers` | 11 | 75 | Key precedence, catalog, registry, transcript, normalization, replay, stream settlement. |
| `internal/providers/anthropic` | 2 | 20 | JSON body, schema, tool changes, missing key, HTTP error, idle and abort. |
| `internal/providers/openai` | 4 | 33 | Completions, Responses replay, header isolation, HTTP/stream errors, idle and abort. |
| `internal/agent` | 8 | 57 | Model changes, hook order, key fallback, tools, settlement, cancellation. |
| `internal/pipeline` | 1 | 10 | Compose rules for every hook. |
| `cmd/tui` | 8 | 47 | Parsing, output, subprocess signals, cassettes, fault injection, one live Token Plan test. |
| `internal/app` | 1 | 1 | `fx.ValidateApp(Module)` only. |

OpenAI has one opt-in live API-key test, `TestLiveOpenAIResponsesReplay`.
It does not prove ChatGPT subscription access.
`TestLiveHeadlessMathQwen` proves only opt-in Token Plan behavior.
Existing `TestMain` functions use goleak in Anthropic, OpenAI, agent, and CLI packages.
Providers core also uses goleak in `stream_test.go`.
CLI excludes the persistent `os/signal.loop` goroutine explicitly.
No existing test proves credential-file coordination, native OAuth, verified identity, account discovery, or subscription request profiles.
The working tree changed during this scout; final counts include `TestPrepareRequestSwitchesWireWithinActiveToolRun` in `agent_model_test.go`.
That test covers active key-only wire switching, not saved OAuth resolution.

## Production caller inventory

The following counts include production Go call sites only.
Definitions, comments, and test calls are excluded.
Each list is complete because no listed production symbol has more than ten callers.

| Function | Total | Existing callers |
|---|---:|---|
| `providers.ResolveKey` | 2 | `internal/agent/agent.go:142` (`SetModel`); `internal/agent/loop_stream.go:98` (`apiKey`). |
| `providers.LookupKey` | 3 | `anthropic/provider.go:165`; `openai/responses.go:76`; `openai/completions.go:81`. |
| `Agent.SetModel` | 0 | Only tests call it today; H13 public switching is later. |
| `loop.apiKey` | 1 | `loop_stream.go:25` (`streamAssistantResponse`). |
| `loop.prepareRequest` | 1 | `loop_stage.go:114`. |
| `loop.streamAssistantResponse` | 1 | `loop_stage.go:123`. |
| `providers.NormalizeRequest` | 2 | `loop_stream.go:22`; `internal/agent/systemprompt.go:18`. |
| `providers.TransformMessages` | 3 | `anthropic/provider.go:182`; `openai/responses.go:87`; `openai/completions.go:88`. |
| `providers.CurrentTools` | 4 | `internal/agent/loop_tool_changes.go:38`; `anthropic/document.go:133`; `openai/completions_body.go:113`; `openai/responses_prompt.go:72`. |
| `anthropic.buildDocument` | 1 | `anthropic/provider.go:183`. |
| `openai.buildResponsesCall` | 1 | `openai/responses.go:88`. |
| `openai.isolateClient` | 2 | `openai/responses.go:102`; `openai/completions.go:104`. |
| `openai.Isolate` | 1 | `openai/completions.go:167` (`isolateClient`). |
| `newHeadlessAgent` | 1 | `cmd/tui/headless.go:76` (`run`). |
| `openProvider` | 1 | `cmd/tui/headless.go:138` (`newHeadlessAgent`). |
| `tokenPlanStream` | 1 | `cmd/tui/headless_faux.go:76` (`openProvider`). |
| `registerOpenAIWires` | 1 | `cmd/tui/headless.go:165` (`newHeadlessAgent`). |
| `startCapture` | 1 | `cmd/tui/headless.go:72` (`run`). |
| `runHeadless` | 1 | `cmd/tui/headless.go:90` (`run`). |
| `pipeline.Compose` | 1 | `internal/agent/agent.go:238` (`execute`). |
| `providers.Find` | 0 | Only catalog tests call it today. |
| `openai.NewResponses` | 1 | `cmd/tui/headless.go:198` (`registerOpenAIWires`). |

Paths without an `internal/` prefix in the adapter rows are relative to `internal/providers/`.
Test callers are concentrated in the files identified per phase below.
Production caller counts identify both shared key-resolution paths and all three ambient-key branches.

## Phase 1: settings store and contracts

Existing files to change: `internal/settings/README.md`, `internal/settings/doc.go`, `internal/providers/types.go`, and `.golangci.yml`.
New files: `internal/settings/auth.go`, `auth_test.go`, `lock.go`, `lock_unix.go`, `lock_test.go`, `paths.go`, `paths_test.go`, and `internal/providers/auth.go`, `auth_test.go`.
These names follow the settings README and nearby provider file patterns.
Use a Unix build constraint for Darwin and Linux lock code.
Use an explicit unsupported-platform implementation if the package must compile on other targets before Windows support.

Settings owns the persisted tagged record, file version, unknown fields, host metadata, and durable provider generation.
Providers owns the immutable inference snapshot and provider/method/API/profile bindings.
Keep refresh tokens, ID tokens, codes, store handles, and callbacks outside `StreamOptions`.
The separate persisted and runtime values contain different data and are justified by the architecture's DTO rule.
Proposed operations are constructor/path resolution, read, locked update, compare-generation replacement, and local delete.
Absence needs a durable generation or tombstone so a login begun before logout cannot recreate the deleted credential.
Keep one record per provider and preserve unrelated provider records and unknown fields.

TDD gaps: modes 0700/0600; absent file; invalid JSON/tag/version; duplicate or ambiguous records; symlink and nonregular paths; relative/invalid home policy; unknown-field preservation; atomic readers; two-process merge; canceled lock waiter; stale login after logout; failed replacement.
Test failures before rename separately from parent-directory sync failure after rename.
The latter has an indeterminate durability result and must not claim that the prior bytes still exist.
Use isolated temp homes and subprocess barriers for cross-process tests.
The lock belongs to a stable sidecar, not the replaced auth inode.
Settings permits standard-library imports only.
`gofrs/flock`, `x/sys`, and `x/oauth2` exist in the manifest, but settings must not use them without an explicit import-rule change.
Standard-library platform locking fits the existing rule.

## Phase 2: auth resolver and composed runner

Existing files to change: `internal/agent/agent.go`, `loop_run.go`, `loop_stream.go`, `types.go`, `agent_model_test.go`, `loop_run_test.go`, `loop_stream_test.go`, and `.golangci.yml`.
Reuse `internal/providers/keys.go`, `keys_test.go`, `registry.go`, `registry_test.go`, `stream.go`, and `assembler.go`.
New files: `internal/auth/README.md`, `doc.go`, `types.go`, `registry.go`, `resolve.go`, `resolve_test.go`, `refresh_test.go`, and `internal/app/provider_runner.go`, `provider_runner_test.go`.
Proposed auth operations are method registration, resolve, readiness, login, logout, and refresh.
The app runner wraps an injected resolver around a `providers.StreamFn`.
Do not put file reads in providers or auth imports in the agent.

Keep `ResolveKey` and the current key-only fallback path for direct legacy callers.
`BoundKey` already prevents a CLI key from crossing a provider change.
`pipeline.firstKey` stops at the first nonempty key, treats empty as no opinion, and stops on error.
`chainPrepare` shows each hook all preceding model/context updates.
Do not add an auth hook to `pipeline.Compose` unless the new contract actually requires it.
The shared runner and injected readiness function can retain the existing hook contract.

TDD gaps: explicit supported override wins; competing typed/key override fails; saved OAuth wins over env; missing record permits env; invalid saved OAuth never falls back; nonempty/empty/error key hooks retain behavior; final-model hook changes destination; each tool-loop request resolves again; valid credential avoids HTTP.
Add OAuth readiness tests beside `TestSetModelNoAPIKeyLeavesState`.
Test unknown account access separately from known denial and missing metadata capability.
Add two-process refresh with a locked fresh read/recheck, one rotation, and unrelated-record preservation.
Cancel immediately after a valid rotation response and prove that the replacement commits before cancellation returns.
Commit uses a bounded independent context after validated rotation.
Resolver errors must create exactly one settled terminal stream and must contain no token material.

## Phase 3: Anthropic, common commands, and composition

Existing files to change: `internal/providers/anthropic/provider.go`, `document.go`, `provider_test.go`, `tool_changes_test.go`; `internal/providers/api.go`, `catalog.go`, `catalog_test.go`; `cmd/tui/args.go`, `args_test.go`, `headless.go`, `headless_faux.go`, `capture.go`, `headless_test.go`, `headless_cassette_test.go`; `internal/app/README.md`, `app_test.go`.
New files: `internal/auth/anthropic.go`, `anthropic_test.go`, `pkce.go`, `pkce_test.go`, `callback.go`, `callback_test.go`, `http.go`, `http_test.go`; `internal/providers/anthropic/profile.go`, `profile_test.go`, `tool_names.go`, `tool_names_test.go`; `internal/app/module_providers.go`, `module_auth.go`; `cmd/tui/auth.go`, `auth_test.go`.
Do not reuse `app.Module` for these commands because it provides DB stores, migrations, and servers.
Expose a focused composition with shared constructors and owned cleanup.
Faux can remain CLI demo setup; production provider/auth wiring needs one owner in app.

`anthropic.ProviderID`, `BaseURL`, `Model`, and `ModelFor` currently mean Token Plan, not native Anthropic.
Add distinct compiled native provider/model records rather than changing the Token Plan constants.
`prepareModel` supplies Token Plan defaults when fields are empty.
`produce` resolves empty `APIKey` against fixed Token Plan env names and uses `fanthropic.WithAPIKey`.
The typed path must bypass this fallback and enforce final bearer/header/origin binding.
The architecture report confirms that the installed fantasy adapter lacks a direct token option.
Use a supported SDK/header/client seam and wire tests; do not add a fake API key.

`buildDocument`, `foldTools`, `encodeMessages`, and `encodeToolCall` are the current body/name owners.
The current public `StreamOptions` has no named tool-choice field.
Add only the narrow required contract for named choice, then test its profile encoding with declarations/history/add/remove.
`fantasykit.Options.Tool` rewrites IDs only.
`Fold` sends `part.ToolCallName` directly to `Assembler.ToolStart` at line 153.
Restore canonical response names before Fold publishes them, with an adapter-local stream mapping or a small justified fold seam.
Do not change IDs, arguments, schemas, results, or stored history while mapping names.
Use historical tool snapshots for historical calls and the active snapshot for response names.
Reject name collisions before HTTP and retain unknown custom/MCP names.

TDD gaps: browser and supported copy-code login; PKCE/state checks; malformed redirect; listener bind error; deadline/cancel; callback/input loser cleanup; validated token response; failed login preserving old record; local-only logout; identity/beta/system shaping; conflicting SDK env; concurrent request immutability; every name field round-trip; no tool replay after output.
Reuse `WithHTTPClient`, the existing SSE helpers, and goleak rather than new test infrastructure.
Dispatch auth commands before interactive-mode selection, prompt reading, capture, and agent creation.
Auth commands need signal cancellation and existing exit mappings, but must not wait for stdin intended for an inference prompt.
Capture drops credential headers but keeps request/response bodies.
Therefore auth HTTP must never use the cassette transport; header redaction cannot protect token response bodies.
Test `ASK_CAPTURE` with login and refresh and prove no exchange or token is recorded.

## Phase 4: ChatGPT identity, discovery, and profile

Existing files to change: `internal/providers/openai/responses.go`, `responses_prompt.go`, `isolate.go`, `isolate_test.go`, `responses_test.go`; provider catalog/bindings; common auth registry/composition.
New files: `internal/auth/openai_chatgpt.go`, `openai_chatgpt_test.go`, `identity.go`, `identity_test.go`, `discovery.go`, `discovery_test.go`; `internal/providers/openai/chatgpt_profile.go`, `chatgpt_profile_test.go`.
`go.mod` and `go.sum` change only for the selected maintained identity verifier.
The current manifest has no evident JOSE/JWT/OIDC verifier.
Select a maintained verifier instead of hand-written signature checks or unsigned JWT decoding.

`buildResponsesCall` already sends `store:false`, instructions, local prompt input, and conditional encrypted reasoning.
It can also send `max_output_tokens` and `prompt_cache_retention` for current API-key compat records.
`encodeResponsesTools` uses flat `fantasy.FunctionTool` values.
ChatGPT needs adapter-owned restrictions and the supported tool grouping form.
Inspect final SDK JSON in tests because fantasy can add defaults after the Ask builder.
Keep API-key Responses behavior intact.
`Isolate` presently lets `Model.Headers` replace Authorization and has no endpoint/redirect binding check.
The typed path must reject competing auth headers and enforce the selected origin/path before sending a token.

TDD gaps: new registration; saved issued-client reuse; host ID stability; exact callback reuse; verified signature/issuer/audience/expiry/nonce; subject/client binding; missing scopes; changed identity/client rejected; refresh-not-before; full rotation metadata; malformed and bounded responses.
Discovery must use the resolved account, parse the subscription schema, keep visible server-order slugs, join compiled metadata, and keep unknown access separate from denied access.
Cache keys include provider/method/issued client/verified subject and generation invalidation.
Test replacement while discovery is in flight so an old result cannot grant a new account access.
Validate prohibited final fields, system items, previous response IDs, tools, extra body, and explicit unsupported options before HTTP.
Classify spent allowance separately from transient 429 in HTTP and streamed errors.
Do not replay after public output or a tool-call event.
Local-only logout remains the user decision; do not add provider revocation or an account picker.

## Phase 5: xAI device auth and Responses

Existing files to change: shared auth registry/composition, provider catalog/bindings, `openai/responses.go`, `responses_prompt.go`, `responses_test.go`, and common CLI auth tests.
New files: `internal/auth/xai.go`, `xai_test.go`, `device.go`, `device_test.go`, and `internal/providers/openai/xai_profile.go`, `xai_profile_test.go` if a separate profile file reduces real complexity.
xAI remains provider data on the Responses adapter; it does not need a new wire package.

`prepareResponsesModel` currently supplies OpenAI defaults and `produce` reads `OPENAI_API_KEY` when its key is empty.
Use explicit xAI records and typed auth so an absent xAI credential cannot use OpenAI billing.
Both xAI key and OAuth requests use the same selected xAI Responses endpoint/profile.
Keep encrypted reasoning conditional on compiled reasoning support.

TDD gaps: device response validation; HTTPS verification URL; no poll before first interval; pending; slow-down; access denied; expiry; cancel during wait and HTTP; total deadline; each HTTP timeout bounded by remaining device lifetime.
An injected clock/wait function is a useful test seam for this protocol.
Missing replacement refresh token retains the old token only for xAI's method contract.
Do not invent verified xAI account identity from an opaque access token.
Use durable generation as the replacement boundary where verified identity is absent.
Test key and OAuth wire headers, conflicting OpenAI env, endpoint binding, model-specific reasoning, canonical tool events, and terminal settlement.

## Phase 6: cumulative acceptance and docs

Existing test owners to extend: `cmd/tui/headless_fault_test.go`, `headless_record_test.go`, `headless_live_test.go`; `internal/agent/agent_model_test.go`; `internal/app/app_test.go`; settings/auth/profile tests from earlier phases.
New integration file: `cmd/tui/headless_auth_test.go` for binary login/logout/inference and signal scenarios with isolated homes.
Use controlled HTTP servers for protocol faults and subprocess barriers for durable races.
Keep all live auth and paid inference explicitly opt-in and separate from offline checks.

Run the narrow new test first, then settings/auth/provider/agent/pipeline/CLI/app packages.
Run race checks for store, resolver, profiles, and agent integration.
Run the build and configured lint/depguard after public types, composition, or imports change.
Do not enable `ASK_LIVE`, `ASK_LIVE_OPENAI`, or `ASK_RECORD` for offline gates.
The CLI's binary helpers `buildBinary`, `TestSignal`, and `TestEPIPE` already support subprocess checks.
Add login timeout, SIGINT/SIGTERM/SIGHUP, no prompt read, no capture, no leaked token, listener cleanup, and local logout checks.
Add a complete composed prompt/tool/request sequence for all methods and key-only regressions.
`fx.ValidateApp` must cover the focused graph; constructor tests must also prove no DB or gateway start.

Update the smallest owning docs: settings/auth/provider/app READMEs, `internal/README.md`, root `README.md`, architecture package/import table, `docs/testing-llm-cassettes.md`, and the existing H7a execution detail when their contracts change.
Update `AGENTS.md` only for actual new package/dependency/feature/workflow facts.
Do not edit generated files or changelogs.
Record live acceptance as a stateful report with sanitized command, provider/method/model/profile, terminal result, usage/tool evidence, and local logout result.
Use `ask auth login --provider <id>` and `ask auth logout --provider <id>` in final documentation.
All three authorized live routes are required for final H7a exit.
No fixture or public model list can prove live account entitlement.

## Integration risks and unresolved questions

Settings' internal-package ban and providers' auth/settings ban are documented but not fully enforced by current depguard.
Add those rules and auth's agent/transport/wire-adapter/config bans with the new package.
Do not copy outdated README interface names such as `ThinkingCapable`; they are dewee reference names, not current Go types.
Current H4 files are changed or untracked, so plan against the inspected working tree and do not restore old Token Plan paths.
One authoritative host/home coordinates credentials; file locks do not provide distributed refresh coordination across replicas.
The maintained ChatGPT verifier and supported Anthropic SDK bearer seam need implementation-time selection and wire evidence.
Live provider approval, account eligibility, and current ChatGPT tool grouping still need authorized acceptance evidence.
The user has already resolved saved-account count and logout semantics; those are not open questions.
