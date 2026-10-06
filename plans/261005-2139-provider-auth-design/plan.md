---
title: "Provider and authentication design across H4 and later phases"
status: proposed
created: 2026-10-05
---

# Provider and authentication design across H4 and later phases

## Outcome and scope

Support API keys and subscriptions for Anthropic, OpenAI, and xAI without adding a wire adapter for each vendor or auth method.
Make a new compatible provider a data registration.
Allow users to add compatible API-key providers and models through user configuration without rebuilding Ask.
Make a new login protocol a small auth implementation.
Switch providers, models, and auth methods within one session through one shared selection operation.
Keep transcript semantics, tool execution, and credential storage independent from vendor request rules.
This document records accepted behavior and proposed execution contracts across several roadmap phases; it does not implement Go behavior.
The user requested phase allocation on 2026-10-06 because H4 cannot deliver the complete design.
Use [roadmap revision 6](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md) for schedule ownership.

The user's current request expands the earlier API-key-only scope.
The [roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md) is the current phase-allocation authority.
The accepted file-store choice, one adapter per wire API, vendor-as-data model, fantasy infrastructure boundary, and existing public hooks remain constraints.
Use the [source audit](../reports/researcher-261005-2139-h4-subscription-source-audit.md) with the four earlier OAuth reports.
Use the [Pi catalog and identity audit](../reports/researcher-261005-2151-pi-provider-model-catalog.md) for model sources, availability and name ambiguity.

Requirements include cancellation, cross-process refresh safety, secret redaction, model-specific compatibility, and identical use from headless and leader runtimes.
A normal request with a valid credential must not call an auth endpoint.
Concurrent requests must not each rotate the same stored refresh token.
Provider growth must not require a switch statement in the agent loop.
Authentication failure must not silently charge a different credential or provider.
No database credential store, generic plugin framework, or automatic account load balancing is needed for this scope.

## Architecture and patterns

Use the existing dewee capability packages with a small ports-and-adapters boundary around model access.
Compose services in `internal/app` and pass dependencies through constructors.
Do not impose a second framework or a new set of layers over every package.

| Pattern | Concrete use | Boundary |
|---|---|---|
| Adapter | Existing `providers.Provider` implementations for Messages, Completions, and Responses | Translates the harness transcript and stream; owns its SDK dependencies. |
| Strategy | Auth methods with login, refresh, and material derivation functions | Anthropic browser, ChatGPT browser, and xAI device flows have different protocols. |
| Registry | Validated provider, model, auth-method, and wire-profile records | Compiled implementations plus user provider/model data; duplicate and missing IDs fail validation. |
| Composition | Provider record selects an adapter, auth method, and request profile | Avoid a class or package for every vendor/auth combination. |
| Codec | Request-local tool-name mapping | Forward conversion before send, reverse conversion before stream events. |
| Transaction | Credential read, refresh, merge, and atomic replacement under a store lock | Prevent lost updates and refresh-token reuse between processes. |

These patterns solve observed differences in the three providers.
Do not add an abstract factory, inheritance tree, arbitrary middleware chain, or generic declarative OAuth engine.
Use ordinary Go structs, functions, maps, and the existing adapter interface.
New interfaces need multiple implementations or a real test seam.

```mermaid
flowchart LR
    A[Agent and PrepareRequest] --> R[Model request runner]
    D[Provider and model records] --> R
    R --> AU[Auth service: resolve or refresh]
    AU --> S[Settings: locked auth.json]
    AU --> M[Registered auth method]
    R --> W[Wire adapter]
    P[Selected request profile] --> W
    W --> SDK[Fantasy or native SDK]
    SDK --> H[Provider endpoint]
    H --> W
    W --> AS[Existing assembler and stream]
```

The request runner is a small composed function, not a second provider hierarchy.
It resolves authentication after `PrepareRequest` has selected the final model and endpoint.
It then calls the existing wire adapter with one immutable auth snapshot.

## Independent identities

| Identity | Example | Meaning |
|---|---|---|
| Provider ID | `anthropic`, `openai`, `xai`, `tokenplan` | Model host and endpoint configuration. |
| API ID | `anthropic-messages`, `openai-completions`, `openai-responses` | Wire protocol selected by the model. |
| Auth method ID | `api-key`, `anthropic-oauth`, `openai-chatgpt`, `xai-oauth` | How a credential is acquired, refreshed, and presented. |
| Request profile ID | `anthropic-api`, `anthropic-claude-code`, `openai-api`, `openai-chatgpt`, `xai-responses` | Rules applied to the final request and response. |

A subscription is a billing/product relationship, not an authentication protocol.
Token Plan can use an API key, while an ordinary bearer token need not use a Claude Code profile.
Keep an explicit billing hint (`api`, `subscription`, or `unknown`) separate from credential kind.
Usage and estimated catalog cost keep their existing meaning.
An estimated cost is not an invoice or a guarantee that subscription inference has no extra charge.

Provider records declare allowed auth-method/profile combinations for each API and allowed endpoint origins for account-bound credentials.
Each provider receives an injected `[]AuthMethod` containing its supported authentication strategies.
The collection contains method behavior and metadata, not stored secrets.
Validate unique method IDs within the provider and select exactly one method for each request.
The selected method/API binding determines the request profile.
Do not create separate provider identities solely for API-key and OAuth authentication.
Model compatibility records declare reasoning, tool changes, cache, and replay support.
The profile can narrow those capabilities for a subscription route.
An unsupported combination fails before the HTTP request.
Custom API-key endpoints use their configured provider record; account-bound OAuth is not automatically forwarded to them.

## Ownership and dependency rules

| Owner | Responsibility | Dependencies |
|---|---|---|
| `internal/settings` | Credential schema, provider/model configuration, selection defaults and favorites, locked read/merge/write, atomic file replacement | Standard library; retain its current internal import ban. |
| Proposed `internal/auth` | Auth methods, login interaction, refresh orchestration, resolution and account validation | `settings`, providers core runtime types, standard HTTP/crypto helpers. No wire adapters or agent import. |
| `internal/providers` core | Catalog and registry, target resolution, runtime auth snapshot, stream contract, request runner | Protocol and permitted core dependencies. No settings or auth-store reads. |
| `internal/providers/anthropic` | Messages profiles, tool-name conversion, native tool changes, replay and response conversion | Providers core, protocol, selected infrastructure. |
| `internal/providers/openai` | Completions and Responses profiles, model/request compatibility, replay and errors | Providers core, protocol, selected infrastructure. |
| `internal/providers/fantasykit` | Shared SDK stream folding, timeout, witness and error plumbing | Fantasy infrastructure only; no login or credential store. |
| `internal/app` | Static registrations and dependency wiring | Concrete owners. |
| Entry points and later TUI commands | Render login prompts and invoke auth operations | Never implement OAuth state or store tokens themselves. |
| Session runtime and agent owner | Serialize selection changes, apply request-boundary updates, record model changes | Catalog resolution and existing session/agent contracts; no vendor switches. |

Auth-method files can initially be `anthropic.go`, `openai_chatgpt.go`, and `xai.go` within the auth capability.
Vendor-specific auth protocols are real behavior; they do not justify separate vendor inference packages.
Share PKCE, bounded loopback callback handling, token HTTP parsing, and device polling only where the implementations actually use the same behavior.
An auth method can use typed function fields instead of another interface.
The existing `settings` schema is the single persisted credential model; auth does not create a duplicate DTO.
The runtime snapshot differs because it contains only the material and metadata needed for one inference request.

The provider README already describes target adapter packages that do not all exist yet.
Implementation must update actual depguard rules with these package boundaries; README text alone is not an enforced SDK import rule.

## Adding providers and models

Keep built-in provider/model definitions in versioned data owned by the providers package.
Load custom provider/model definitions from user model configuration through the settings owner and pass them to the registry at composition.
Secrets stay in `auth.json` or the process environment, never in provider/model definitions.
The same validator handles built-in and user records.
Require unique IDs for new custom providers; reject duplicate definitions.
Interview Q9=A accepts explicit field overrides for existing model metadata, including built-in model records, following Pi.
An override references an existing provider/model identity; it is not a duplicate provider declaration and cannot silently change endpoint or auth bindings.
Project configuration may reference known model targets after trust checks; it cannot silently replace account-bound endpoint definitions or credentials.

| Addition | Required change | Rebuild Ask? |
|---|---|---|
| Model on a configured compatible provider | Add model metadata and verified capability values | No |
| Provider using a supported wire API and key auth | Add endpoint, API ID, key source, compatibility and models | No |
| Compatible provider needing an existing request profile | Select that registered profile and supply its allowed configuration | No |
| New nontrivial request/response behavior | Add a tested adapter-local profile transformation | Yes |
| New auth protocol | Register login/refresh/material functions and method/profile bindings | Yes |
| New wire protocol | Implement one adapter and register it | Yes |

This configuration example is proposed syntax, not an existing supported file contract.
The endpoint and model values are placeholders.

```json
{
  "providers": {
    "my-gateway": {
      "baseURL": "https://gateway.example/v1",
      "api": "openai-completions",
      "profile": "openai-api",
      "authMethods": ["api-key"],
      "apiKeyEnv": "MY_GATEWAY_API_KEY",
      "models": [
        {
          "id": "vendor-model-id",
          "name": "My model",
          "input": ["text"],
          "reasoning": false,
          "contextWindow": 32000,
          "maxTokens": 4096
        }
      ]
    }
  }
}
```

Prices and optional compatibility flags are separate validated fields.
Interview Q11=B accepts Pi defaults for new custom JSON model definitions: name equals ID, input is text-only, reasoning is disabled, context is 128000 tokens, output limit is 16384 tokens, and omitted prices are zero.
Apply these defaults on the custom-definition path, including a definition that replaces a catalog model with the same ID.
An ordinary partial metadata override instead preserves the catalog values of fields the user did not supply.
Resolve a custom model's API and base URL from its definition, then provider configuration, then applicable catalog defaults; fail if either remains missing.
Reject explicitly invalid or nonpositive limits rather than replacing them with defaults.
Defaults are configuration values, not verified limits or proof of free inference.
Do not introduce a generic tools capability default that Pi does not define.
Do not infer advanced capabilities from an OpenAI-compatible label.
Interview Q2=B accepts Pi's model-owned routing: each provider-scoped model record has one canonical API.
One provider can expose several APIs across different model records.
Do not add named routes or a separate public API selector for the same provider/model identity.
Alternative endpoint configurations use explicit records and do not split providers solely by auth method.
Refresh user configuration by validating a complete replacement catalog, then atomically publish its immutable snapshot.
Invalid reloads leave the last valid catalog active and report the error.
An in-flight request retains the old definitions until it settles.
Interview Q10=A also keeps the selected session model snapshot unchanged after ordinary background catalog refresh.
Reselection, an explicit model update or a new session obtains the latest validated record; background refresh does not silently replace the active model.
Credential freshness is independent: auth still resolves or refreshes on each request as required; the model snapshot does not pin an expired token.
Do not mutate an adapter or endpoint underneath a running stream.
If a reload removes the selected target, let an already-started request settle and block the next request with an unknown-target error until a valid target is selected.

## Selection and convenient switching

### Model support and evidence

Keep each model record scoped to the provider that serves it.
Store API routing, context/output limits, input modalities, reasoning and compatibility on that offering, not on a global model-name record shared by all providers.
Metadata sources are built-in definitions, user configuration, or a registered discovery source.
Record source and refresh time for dynamic results; a cached record is not a fresh entitlement check.
Do not merge records across providers because their display names or model IDs match.

Track distinct evidence: known in catalog, auth configured, account access known/unknown, and last inference outcome.
A provider model-list response establishes only what that endpoint's documented listing semantics establish.
It does not automatically prove tools, vision, context limits, account allowance, or availability of every API route.
Use verified metadata and route/profile capabilities for those fields.
Represent unsupported and unknown capability values separately where a selection depends on them.
Use provider-specific discovery/filter functions only when the route supplies that evidence; no global assumption that `/models` has one schema.
Authenticated entitlement caches must be bound to provider, auth method, account identity and endpoint configuration; invalidate them on login/logout/account replacement.
Keep discovery caches separate from user definitions and secret-bearing credentials.
A discovery failure retains cached metadata with its freshness state and does not mark every provider unsupported.

Pi itself has generated baselines, a central `pi.dev` overlay and extension/configuration layers.
Ask adopts the catalog and refresh boundaries but does not need a hosted catalog service for this scope.
Interview Q3=B accepts a remote metadata overlay like Pi in addition to the local baseline and user definitions.
Support persisted cache, revalidation, offline use and replacement of newer provider-scoped metadata.
Interview Q7=A selects Pi's public catalog as the initial remote source, through a small Ask schema adapter.
Default the configurable source base URL to `https://pi.dev`; request provider shards at `/api/models/providers/<sourceProviderID>?types=chat`.
Do not send inference credentials to this service or import Pi runtime types into Ask core contracts.
Validate explicit source/provider mapping and preserve Ask endpoint/auth bindings.
Retain local baseline/cache after HTTP or schema failure; no Ask-hosted catalog service is required.
Interview Q9=A composes baseline plus a valid newer remote overlay, then custom definitions, then explicit field overrides.
Supplied override fields win; omitted fields retain the underlying values.
Do not add Pi's extension framework merely to reproduce its metadata precedence.
The detailed evidence and differences are in the linked catalog audit.

### Qualified identity

A model has a qualified identity `(providerID, modelID)`.
A selected inference target adds `authMethodID` and the effective reasoning preference.
The API and profile are resolved from validated definitions, not chosen independently by the user.
The same model ID on two providers therefore cannot select the wrong endpoint.
Treat `providerID/modelID` as a display/reference form; store the two fields separately because a model ID can contain slashes.
For example, `direct/model-x` and `gateway/model-x` are distinct offerings even if they serve the same underlying model.
Show provider and auth method next to the model name in search results, favorites and the active-session indicator.
Only explicitly verified mapping may group offerings as the same underlying model; such grouping never changes their runtime identity or copies capabilities between them.
For this chat-only scope the model type is implicit; add type to the identity when other operation types are implemented.
Switching `openai/model + api-key` to `openai/model + openai-chatgpt` is a target change even if the model name is unchanged.

Expose a shared catalog listing operation and a shared selection operation to TUI, headless and leader command handlers.
The proposed operations are `ListTargets`, `ResolveTarget`, and session-owned `SelectTarget`.
They are functions on existing owners, not three new service interfaces.
Headless startup selection and `PrepareRequest` model updates must use the same resolver and validation rules.

The catalog joins definitions with credential configuration and optional account entitlement.
Show configured, needs-login, unavailable, or entitlement-unknown status separately from verified endpoint success.
Opening a picker does not refresh every provider or require every vendor to be online.
Search uses model/provider display names; selection stores qualified IDs and method IDs.
Disambiguate short names when more than one target matches.
Follow Pi for credential selection: an explicit API-key override takes precedence when supported; otherwise use the stored credential type.
With no stored credential, resolve ambient credentials through the supported API-key method.
A stored API-key record with no key can use that method's environment resolution.
A stored OAuth failure is an error and must not fall back to API-key billing.
If a preset or session requires another method, report the mismatch and require login or a matching explicit override; do not rewrite the stored credential during selection.

For convenience, support named presets, an ordered favorites list, and the previous selected target.
A preset references a target and reasoning preference; it does not copy endpoint or model metadata.
The TUI picker can search the catalog and display provider, model, API/subscription method, and readiness.
Quick cycle uses the ordered favorites and skips targets known to need login or lack entitlement; unknown status remains visible and is validated before use.
The command layer can provide selection by preset and previous-target switching without implementing a second resolver.
These are requested switching capabilities; precise key bindings and command spelling are UI decisions.

Use this selection sequence:

1. Resolve the requested target from one catalog snapshot and validate the method/profile binding.
2. Prepare the destination credential and validate known entitlement and required capabilities.
3. Validate transcript compatibility and context budget for the destination model.
4. At a request boundary with no active stream or unfinished tool batch, commit the target and its effective request options.
5. Record the change in the session and emit the selection result; use the new target for the next request.

Requests to switch during streaming are pending until that boundary.
They do not change the provenance or decoder of the current response.
An explicit cancel action can stop the current run before a switch, using the existing cancellation contract.
If destination validation fails, retain the current target, report the reason, and do not silently send the intended next prompt through the old target.
Selection commands are serialized per session; concurrent sessions can select different targets.
Persist only target IDs and effective preferences, never the resolved token snapshot.

Reuse `providers.TransformMessages` for cross-provider replay and the existing `PrepareRequest` hook for per-request model changes.
Source: `internal/providers/transform.go` and `internal/agent/loop_run.go`, `prepareRequest`.
Rebuild the destination wire request from the canonical transcript.
Normalize tool-call IDs and paired results, strip incompatible signatures, and apply the destination tool-name codec without changing stored history.
No provider-specific replay logic belongs in the selection command.

Resolve effective capabilities as the intersection of model support, API support and selected profile restrictions.
Clamp reasoning preferences using the accepted Pi rule and show the effective level.
Do not carry unsupported sampling settings across targets.
If history exceeds the destination context window, return a clear compaction-required result rather than sending an invalid request or silently discarding history.
When the later compaction capability is available, the same result can invoke that explicit workflow.
Reuse existing image-omission projection for historical images with a visible compatibility notice; reject a new image-only prompt on a text-only target instead of sending an empty substitute.
Require the destination to support active tools when continuation depends on tool execution.

Session selection and global startup defaults are separate.
An ordinary switch changes only the current session.
An explicit save-default action updates user settings; a one-shot headless override does not rewrite them.
Resolve startup selection in this order: explicit invocation target, recorded session target when resuming, trusted project default, user default, then the configured built-in default.
Never fall through to another provider if an explicitly selected target is invalid or unauthenticated.

Pi provides useful selection evidence: `AgentSession.setModel` checks auth, records a model change, and persists a default only on request.
Its `cycleModel` uses scoped or available models and applies per-model thinking preferences.
Source: `packages/coding-agent/src/core/agent-session.ts:2430-2530` in the pinned Pi checkout.
Its runtime also has validated provider registration and immutable-style availability snapshots.
Source: `packages/coding-agent/src/core/model-runtime.ts:900-942`.
The target includes auth-method identity in Ask because the current scope requires convenient API/subscription switching.

## Runtime contract

Add a resolved-auth value to `StreamOptions` while retaining `APIKey` as a compatibility input.
The value contains provider ID, auth-method ID, credential source, selected profile ID, endpoint binding, billing hint, and request auth material.
Request auth material distinguishes API key, bearer token, and explicit header authentication.
It never contains refresh tokens, ID tokens, OAuth codes, or credential-store callbacks.
Secret-bearing values must be redacted by logging and must not use default struct formatting in diagnostics.

An explicitly supplied resolved value and a legacy `APIKey` must not compete.
Use the resolved value when present; otherwise adapt the legacy key through the selected provider's registered API-key method.
Retain `pipeline.Hooks.GetAPIKey` and its empty-value fallback contract.
A nonempty legacy hook override means API-key authentication, unless an explicit method-bound configuration selects another method.
Add an optional typed auth override only where a caller needs bearer or OAuth metadata.
Reject simultaneous nonempty typed and legacy overrides rather than guessing.
Do not infer OAuth from token prefixes.

The runner resolves one auth snapshot per request, after model switching, and passes it into the adapter.
Missing, expired, or invalid stored credentials produce a classified auth error.
Refresh failure does not fall through to an environment key.
An intentional user-selected method change can select a different credential.
Endpoint changes require a new resolution step before any credential is sent.
Bind legacy configured keys and request overrides to their intended provider; a model switch must not reuse the previous provider's `StreamOptions.APIKey` fallback.
Reject an unbound legacy key on a cross-provider switch and resolve the destination's own credential instead.
Reject redirects that would transfer credential material outside the allowed endpoint binding.

## Request construction and profiles

Each adapter keeps one explicit request-building sequence:

1. Copy and normalize the transcript using existing shared conversion and tool snapshots.
2. Apply model compatibility and build the wire document.
3. Apply ordinary supported request options and overrides.
4. Apply the selected auth-specific profile, including names, system blocks and headers.
5. Validate the complete wire body and headers, then send them with SDK retries disabled.
6. Decode vendor events, reverse names, and feed the existing assembler.

The sequence is ordinary adapter code, not six new middleware interfaces.
Profiles use typed compatibility data for simple field support and named local functions for nontrivial transformations.
Required method identity and auth headers are applied last and cannot be replaced by an unrelated override.
Optional user headers can override optional defaults.
Reject unsupported explicit options with a useful error; omit automatically generated defaults when a profile does not support them.
Final validation catches fields reintroduced through extra-body or sampling overrides.

| Provider/method | Adapter | Profile behavior |
|---|---|---|
| Anthropic API key | Messages | Standard API-key auth; common model/replay rules. |
| Anthropic OAuth | Messages | Bearer auth, Claude Code identity headers/system prefix, OAuth betas and tool-name codec. |
| OpenAI API key | Responses or Completions | Standard key auth and selected model capabilities. |
| OpenAI ChatGPT | Responses | Public endpoint, streaming, no server storage, documented subscription restrictions and account entitlement. |
| xAI API key or OAuth | Responses | Same wire profile; distinct auth lifecycle and billing hint; model-gated encrypted reasoning replay. |
| Other compatible vendor | Existing matching API adapter | Provider data and explicit compatibility flags; default API-key method when supported. |

The inspected fantasy fork exposes headers, HTTP-client injection, raw extra-body support, and SDK retry disabling.
These are useful infrastructure seams, but do not replace final profile validation.
Anthropic bearer auth must also disable ambient SDK key/token discovery and any unwanted `x-api-key` header.
Verify the actual outgoing HTTP request with conflicting environment credentials in a test.
Patch the approved fantasy fork only if its existing SDK-option seams cannot express the required behavior.
The adapter can use a native SDK when that is simpler and more reliable, as the accepted infrastructure decision permits.

### Accepted builtin naming reference

The user selected Pi as the tool-name reference after reviewing Claude Code naming differences.
Use the Claude Code compatibility table in Pi `4c6fb7cfe`, `packages/ai/src/api/anthropic-messages.ts:101-119`.
This is Pi's compatibility naming table, not Pi's own coding-agent tool registry and not a claim about the latest Claude Code build.
The reference names are:

`Read`, `Write`, `Edit`, `Bash`, `Grep`, `Glob`, `AskUserQuestion`, `EnterPlanMode`, `ExitPlanMode`, `KillShell`, `NotebookEdit`, `Skill`, `Task`, `TaskOutput`, `TodoWrite`, `WebFetch`, `WebSearch`.

New Ask builtins with matching semantics use those names directly, so their Anthropic wire conversion is the identity operation.
This naming choice does not add all listed tool implementations to H4.
Keep existing public tool names and persisted transcript names; a later rename requires an explicit compatibility migration.
Names outside the reference table, including custom and MCP tool names, are not renamed merely to resemble Claude Code.
Do not automatically alias `find` to `Glob`, `Agent` to `Task`, or `TaskStop` to `KillShell`; a different tool name alone does not prove equivalent semantics.
The tool owner defines canonical names; the provider adapter applies only the compatibility conversion needed for the selected profile.

### Accepted builtin input, behavior and output contract

The user accepted Pi as the reference for builtin input schemas, behavior and output on 2026-10-06.
Keep one canonical tool contract across providers, with the previously selected compatibility names.
Do not copy Claude Code input schemas solely because the wire name matches a Claude Code tool.
For example, Pi's read input uses `path`, optional one-based line `offset`, and optional line `limit`; its bash input uses `command` and optional timeout in seconds.
Source: Pi `packages/coding-agent/src/core/tools/read.ts:14`, `bash.ts:40`.

The tool owner defines and validates argument fields, units, defaults, errors, cancellation, truncation and execution behavior.
Schema, description and implementation must agree; do not change argument meaning for a different LLM provider.
Use existing `tools.Tool`, registry preparation and validation, and `protocol.ToolExecutionResult` rather than adding a parallel provider-specific tool contract.
Keep model-facing `Content`, UI/log `Details`, and optional programmatic `StructuredContent` distinct.
Structured results match an output schema where the tool supplies one; they do not automatically become model-facing content.
Source: Pi `packages/agent/src/types.ts:424`; Ask `pkg/protocol/message.go:395`, `internal/tools/types.go`.
The wire adapter serializes canonical tool declarations and model-facing results into the provider protocol and applies the selected name codec where needed.
It does not own builtin argument semantics or execution.
Custom/MCP tools retain their declared contracts; do not reshape them into builtin Pi schemas.
Existing public contracts require an explicit compatibility migration if a later builtin changes them; this reference decision does not move every builtin's implementation phase into H4.

### Tool-name codec

Build a codec from the immutable request tool snapshots.
Forward conversion covers definitions, historical assistant calls, additions, removals and forced tool choice.
Historical calls use the matching historical declarations; response names resolve against the current active tool set.
Unknown custom names stay unchanged.
Reject ambiguous name mappings before sending a request so a returned name cannot execute the wrong tool.
This is name ambiguity validation, not a change to the accepted tool-call ID policy.
Reverse conversion runs before any public `toolcall_start` event.
Persisted transcripts and tool registry names remain original.
Two concurrent requests can use different profiles without mutating shared tool declarations.

## Credential schema and lifecycle

Keep credentials in `~/.ask/auth.json` under the accepted `ASK_HOME` path rules.
Preserve existing API-key records and unknown fields on updates.
Add explicit OAuth method identity, access/refresh tokens, actual expiry, granted scopes, optional refresh-not-before time, and method-owned metadata.
ChatGPT metadata includes issued client ID, verified account identity and retained ID-token hint.
Bind those fields as one record; do not replace an account's credentials using an unrelated returned identity.
Store one type-tagged credential per provider ID, following Pi.
A provider supports several injected auth methods, but the store contains only one selected method and one account for that provider.
The user's clarification supersedes the earlier per-method storage interpretation of Q5=A.
Successful login through another method or account explicitly replaces the provider's current credential.
Keep the current credential if login fails or persistence fails before atomic replacement.
If replacement succeeds but durability confirmation fails, report an uncertain commit and re-read the store.
Do not claim that the old credential is preserved after replacement.
There is no saved method collection, account picker, or requirement to retain the previous API key after OAuth login.
An environment key or request override can coexist with a stored OAuth record without becoming another saved credential.
Selection preferences live in settings/session state, not in the secret store.
Keep legacy provider-level API-key records readable; do not migrate them into a method collection.
Reject ambiguous legacy OAuth imports until their method can be established from explicit import configuration.
Method metadata can evolve without changing the agent or wire adapters.

Store actual expiry, not an expiry with a hidden margin already subtracted.
Use one method-specific refresh lead time: 10 minutes for Anthropic/xAI and 8 minutes for ChatGPT reproduce Pi's normal effective resolver behavior.
Respect a server-provided earliest-refresh time.
If required request validity cannot be satisfied before refresh is allowed, return a bounded retry-after/auth error rather than repeatedly refreshing.
Keep this scheduling rule in one resolver used by inference and authenticated model discovery.

Refresh procedure:

1. Read the selected credential and test its validity.
2. Acquire the cross-process credential-store lock with cancellation and a deadline.
3. Re-read the current record and recheck method, identity, deletion, and validity.
4. Reject unresolved refresh-attempt state; otherwise durably fence the current generation before a rotating grant is sent.
5. Refresh only if that current record still needs it, with a bounded HTTP deadline.
6. Validate the complete token response and merge the method metadata.
7. Persist the rotated credential and clear its matching attempt fence before releasing the lock or allowing inference.

Cancellation can stop waiting or an in-flight token exchange.
After a successful token response rotates a refresh token, cancellation must not skip durable persistence.
Complete that short local commit with its own bounded context, then report the canceled inference request.
A save failure is an error, not a successful login or refresh.
Do not retry a rotation request blindly after an ambiguous network outcome.
Persist a pending refresh-attempt fence before network I/O so that an error or process restart cannot cause another caller to reuse an uncertain rotating grant.
If fence durability fails, send no exchange.
Only a validated durable replacement or explicit successful login/logout can clear the unresolved state.
A crash before sending can conservatively require reauthentication; this is not a distributed transaction.
CLI shutdown waits for registered auth work through its bounded exchange and local commit budgets, rather than only the inference grace.
A second ordinary signal cannot shorten a validated commit; forced death or blocked OS sync remains an uncertainty limit protected by the durable fence.
The user approved this correction with the other five plan review changes on 2026-10-06.

Lock a stable sidecar path because atomic rename replaces the auth-file inode.
Write a same-directory temporary file with mode 0600, sync it, rename it, and sync the directory before success.
Use mode 0700 for the owner directory.
Read-merge-write preserves other providers and avoids stale whole-file replacement.
Never treat corrupt JSON as an empty store and overwrite it.
For this scope a bounded whole-file lock is sufficient; it serializes refreshes across providers in the same Ask home.
Per-credential coordination becomes useful only if measured contention justifies the extra commit protocol.

Browser login does not hold the store lock while a user signs in.
Capture a record revision at start and compare it under the final commit lock so a concurrent logout or replacement is not undone by an old login attempt.
Expose callback acceptance, token validation, and credential save as distinct states.
Only the last state means login complete.
Logout removes local credentials under the same lock; it does not claim server revocation.
Logout removes the provider's one saved credential, including its issued-client metadata.
The user confirmed this Pi behavior on 2026-10-06; no separate client registration or account collection remains after logout.
If a method-specific operation is exposed, reject a mismatched method rather than deleting a credential for another method.

## Auth protocols and discovery

Anthropic retains browser/copy-code flows, the exact registered redirect URI, state checks, and its JSON token exchange.
Validate token fields at the trust boundary even where Pi relies on a type assertion.
ChatGPT retains PKCE, a stable host identifier, issued-client registration, scope checks and the public Responses endpoint.
Implement ID-token verification and account binding rather than copying Pi's presence-only check.
Use a maintained JOSE/OIDC verifier for signature and claim validation; do not write a custom JWT verifier.
Select the smallest suitable library during implementation and record any new dependency in the owning package documentation.
Reuse the issued client for returning-account sign-in.
See [OpenAI registration](https://developers.openai.com/siwc/token-sharing-open-source/sign-in).
xAI retains device authorization with response validation, server polling intervals, slow-down handling, and optional refresh-token replacement.
Login prompts and browser launch use injected interaction functions shared by both runtimes.

Static provider/model records remain the default catalog.
Interview Q3=B adds automatic remote metadata overlays like Pi, with a cached baseline available offline.
Add authenticated discovery only where the selected route supplies account-specific access, initially ChatGPT.
Follow the route's documented model identifiers and visibility filter, rather than assuming the ordinary API-key catalog is the account's entitlement.
Source: [models and inference](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference).
Use the same auth resolver for discovery and inference.
Discovery narrows the usable catalog; it does not invent prices, context limits or reasoning support.
Keep verified model metadata separate from entitlement.
Interview Q4=A permits inference when account access is unknown and authentication and required capabilities are valid.
Known denial remains a selection error; an unknown state does not prove permission.
Classify the real inference response without silently selecting another method or provider.
Subscription profiles must track the current [preview contract](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations) and [token timing](https://developers.openai.com/siwc/token-sharing-open-source/token-reference).

## Errors and observation

Classify errors as missing credential, reauthentication required, unsupported request, exhausted allowance, rate limit, transient transport, or storage failure.
Use the existing stream error contract and add fields only where callers need a structured decision.
Exhausted subscription allowance must not enter the generic 429 retry loop.
A mid-stream failure must not transparently replay a request after public output or tool-call events.
Keep retry ownership with the existing provider/agent design; auth resolution is not another retry layer.
A request ID, provider ID, method ID, profile ID, duration and outcome are sufficient diagnostics.
Do not log token endpoint bodies or secret-bearing headers.

## Decisions and alternatives

### Accepted decision: one provider with injected auth methods

Status: accepted by the user on 2026-10-05.
Context: a provider can support API-key authentication, subscription authentication, or both.
Choice: one provider with an injected `[]AuthMethod`, using Composition and Strategy.
Alternative: separate `AnthropicAPIKey` and `AnthropicOAuth` providers.
Consequence: adapter and model data remain shared; one credential is saved per provider; its type selects the method and profile unless a supported explicit request override applies.
Use separate provider records for independent endpoint/service configurations, not just different authentication methods.
The provider/auth decision and interview Q1-Q11 policies are individually accepted.
Concrete package contracts, public command syntax and execution details remain proposed until the design is reviewed as a whole.

### Decision: separate auth lifecycle from wire conversion

Context: three different auth protocols use two inference APIs.
Choice: add one auth capability and keep existing wire adapters.
Alternative: combine login and inference in vendor packages.
Consequence: a method can change without changing replay or the agent; composition must validate the selected method/profile pairing.

### Decision: explicit method and profile metadata

Context: Pi uses token-prefix heuristics and caller overrides can bypass shaping.
Choice: method-bound runtime snapshots and final request validation.
Alternative: detect subscriptions from token strings, vendor names or endpoint strings.
Consequence: callers must supply method identity for OAuth imports, and incompatible credentials fail before HTTP.

### Decision: static registration with targeted strategies

Context: many providers share an API, but some have real protocol differences.
Choice: compiled adapters/strategies plus validated built-in and user provider/model data.
Alternative: a generic pipeline DSL or runtime plugin system for all providers.
Consequence: unusual behavior needs small compiled Go functions; ordinary compatible vendors need only records and contract tests.

### Decision: qualified targets and one selection transaction

Context: model names overlap across providers and the same model can use API or subscription credentials.
Choice: qualified provider/model/method targets, one type-tagged saved credential per provider and one session-owned selection operation.
Alternative: independent UI setters for provider, model, endpoint, and key.
Consequence: picker, shortcuts and headless reuse the same checks; changes wait for a safe request boundary and failed switches retain valid state.

### Decision: one file transaction for refresh

Context: leader and headless share rotating credentials.
Choice: bounded store lock, recheck, token exchange and durable commit.
Alternative: per-process mutex only, or network refresh outside a lock with an unchecked write.
Consequence: refreshes within one home serialize, but token rotation and logout ordering are correct.
The network-success/disk-failure boundary cannot be made atomic with a remote auth server.
Surface that failure and request reauthentication when recovery is not safe.

## Roadmap allocation

This is a cross-phase design, not a single H4 implementation plan.
The latest scheduling decision supersedes Q1's H4 delivery date only; minimal login stays coupled to subscription inference in H7a.
Priority update (2026-10-06): H7a starts after H3 alongside required H4 Responses work because Alibaba Token Plan expires soon; full H5-H7 are not prerequisites.
All other accepted behavior and existing public-contract compatibility remain intact.

| Roadmap owner | Scope | Exit condition |
|---|---|---|
| H4 | Wire adapters, compat, cost, key-only in-memory selection and replay | Existing Go-test switch/replay exit; no catalog/store or native login. |
| H5 | Pi builtin names/input/behavior/output | Coding tools preserve shared argument semantics and model-facing output boundaries. |
| H6 | Settings layering and trust | Untrusted project input cannot change account-bound endpoint/auth configuration. |
| H7 | Provider/model configuration, Pi overlay/cache and overrides/defaults; reuse H7a's store | Add compatible providers without recompilation; validate refresh and integration with shared credentials. |
| H7a, priority | Shared secure credential persistence/precedence, native Anthropic/ChatGPT/xAI auth, refresh/discovery, profiles and headless login/logout | Deliver each usable subscription as ready using compiled records; all three are required for phase completion. |
| H8 | Durable qualified selection and provenance | Resume IDs/preferences without tokens; resolve credentials per request. |
| H9 | Allowance/auth/rate-limit classification and retry | No billed fallback or transparent replay after public output. |
| H13 | Shared listing/switching through leader/direct clients | Validated selection at a safe request boundary; retain prior state on failure. |
| H17 | Additional providers/auth, proxy, scoped/favorite cycling and gateway login | Reuse the core resolver and auth lifecycle. |
| T1/T2 | Model/auth status and selection; full login dialogs in T2 | UI consumes shared operations without protocol logic or token storage. |

See [H7a execution detail](../260930-2254-pi-feature-inventory-go-roadmap/phase-h7a-subscription-auth.md) and the [deep TDD implementation plan](../261006-0157-h7a-subscription-auth/plan.md).
Use the existing wire work as a dependency; preserve separate tool-snapshot and cassette-testing workstreams.
Login runs on the inference host and writes to its Ask home, with documented provider-native interaction or callback forwarding for remote use.
Local-client credential transfer stays outside H7a; full TUI login remains later.
The validation below is cumulative across owner phases, not an H4 exit checklist.

## Validation and acceptance

- Table-driven adapter checks inspect the actual outgoing JSON and headers for every supported pairing, including conflicting environment credentials.
- Anthropic checks cover current and historical definitions, additions/removals, forced choice, reverse streaming names, and unchanged custom names.
- Shared tool-contract checks preserve input schema and argument meaning across provider switches; wire names may change, canonical arguments do not.
- Result conversion sends model-facing content and preserves error state and call pairing, without exposing UI/log details or programmatic results by default.
- Requests in parallel prove that mappings and auth material never leak between providers or sessions.
- Store tests use two processes or equivalent OS-lock contention and establish one refresh, preserved unrelated records, and valid JSON after replacement.
- Cancel-after-rotation and save-failure tests establish that credentials are not silently lost and success is not reported early.
- ChatGPT checks cover identity/signature/nonce failure, client reuse, disallowed final fields, earliest-refresh timing, discovery and allowance exhaustion.
- xAI checks cover every device polling state, reasoning-model conditions and missing replacement refresh tokens.
- Headless and leader checks use the same services; live account checks confirm transport behavior after offline checks pass.
- Add a compatible provider and model using only user configuration; a validated reload makes the target selectable without recompilation.
- Duplicate declarations, malformed definitions and unsupported method/API combinations fail validation while the prior catalog stays usable.
- Explicit model metadata overrides survive remote refresh; omitted fields receive current underlying values.
- Custom JSON definitions use Pi defaults; partial overrides preserve unspecified catalog fields, and invalid explicit limits fail.
- Ordinary catalog refresh leaves the selected session model snapshot unchanged; reselection uses the latest record without changing an in-flight request.
- Switch Messages to Responses and back in one transcript containing tool calls/results and thinking; stored history stays unchanged.
- Login with an API key, replace it through subscription login, then replace it through API-key login on the same provider.
  Each successful login leaves exactly one saved credential, and a failed login or save before commit preserves the prior record.
  A save failure after replacement must report uncertain commit state.
- Verify explicit API-key override, stored credential selection, ambient resolution with no saved credential, and no API-key fallback after OAuth failure.
- Select during streaming and tool execution; apply only at the next safe boundary and retain original response provenance.
- Failed auth, unknown entitlement and smaller-context targets do not silently change the target or charge the previous one.
- Presets, favorites, previous-target selection and startup/default precedence use the same qualified target resolver.
- Two providers offering the same model ID remain separate choices with independent API routing, capabilities, prices, credentials and entitlement.
- Bare-ID ambiguity and model IDs containing slashes cannot select a provider by catalog order.
- Discovery failure, stale metadata and unknown capabilities are distinguishable from confirmed lack of support.
- Account replacement invalidates account-bound discovery results without deleting provider-wide metadata.
- Run focused tests first, then package tests, type/build and the existing import/lint gates for changed contracts.

No Go test execution is claimed for this design-only change.
Rollback during implementation keeps legacy key inputs and key-only registrations working; remove new method registrations before removing new runtime fields.
Preserve credential files and unknown records through rollback rather than rewriting the store into an older shape.

## Unresolved questions

- Live credentials are needed to prove account eligibility and endpoint acceptance for the three subscription paths.
- Anthropic integration permission is a separate provider-contract question recorded in the earlier research; successful wire compatibility does not resolve it.
- Minimal headless login/logout is prioritized in H7a after H3, alongside required Responses adapter work; login runs on the inference host and the selected headless surface is `ask auth login --provider <id>` and `ask auth logout --provider <id>`, adapted from Pi interactive operations.
- Pi's public catalog and metadata policies are accepted; source/schema validation and refresh behavior require implementation checks.
  Direct HTTP checks returned 200 without credentials for Anthropic, OpenAI, xAI and Qwen Token Plan; Ask schema conversion and source/provider mapping are required.
