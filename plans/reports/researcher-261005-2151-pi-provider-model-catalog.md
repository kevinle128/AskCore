# Pi provider/model catalog and identity audit

Status: source audit complete; no implementation or live inference performed.
Reference: `/Users/dale/Desktop/workspace/opensources/pi`, commit `4c6fb7cfe`.
Question: how Pi knows provider models/capabilities and distinguishes a model served by several providers.
All source paths below are relative to that checkout.

## Evidence path

GitNexus query located model listing, provider refresh, remote catalogs, and model registry definitions.
GitNexus context confirms `ModelRuntime.create` calls `withRemoteCatalog` and model-picker functions use `modelsAreEqual`.
Direct source reads establish the catalog sources, auth checks, merge rules, and resolver branches.
This extends the earlier subscription audit: built-in definitions are static inputs, but the coding-agent's effective catalog is not static-only.

## 1. Catalog has several sources

| Source | How Pi uses it | Source code |
|---|---|---|
| Generated baseline | Provider factories pass generated model records into `createProvider` | `packages/ai/src/providers/anthropic.ts`, `openai.ts`, `xai.ts`; corresponding `.models.ts` files |
| Build-time upstream catalogs | Generator reads models.dev, OpenRouter, Vercel AI Gateway and Radius catalogs, then applies corrections | `packages/ai/scripts/generate-models.ts:1757`, `:2777-2794` |
| Central runtime overlay | Coding-agent wraps built-in providers except Radius with `withRemoteCatalog` | `packages/coding-agent/src/core/model-runtime.ts:214-255` |
| Dynamic provider source | Optional `fetchModels`/`refreshModels` restores cached data and publishes newer records | `packages/ai/src/models.ts:1038-1110`; Radius provider |
| User configuration | `models.json` adds models and overrides provider/model configuration | `packages/coding-agent/src/core/model-config.ts`; `provider-composer.ts` |
| Extension registration | Registered providers/models and optional refresh functions compose over the preceding layers | `packages/coding-agent/src/core/model-runtime.ts:919`; `provider-composer.ts:522-622` |

Pi does not call every vendor's `/models` endpoint to discover everything on every request.
For Anthropic, OpenAI and xAI, the inspected factory definitions use generated baselines and do not implement account-specific model discovery.
The coding-agent can update their metadata through the central catalog overlay.
That overlay is not account entitlement discovery.

The generator imports tool-capable chat models, excludes known incompatible IDs, and corrects metadata with provider-specific rules.
Examples include tool-call filtering, API model-name corrections, reasoning maps, modality corrections and pricing adjustments.
These are curated compatibility records, not capabilities proven by the model name.
Source: `generate-models.ts`, `loadModelsDevData`, `generateModels`.

## 2. Runtime overlay and cache

`withRemoteCatalog` requests `https://pi.dev/api/models/providers/<providerID>` with `types=chat,image,classifier`.
It stores dynamic catalog records in `models-store.json`, separately from user-edited `models.json`.
Source: `packages/coding-agent/src/core/remote-catalog-provider.ts:15-158`; `models-store.ts:51-60`.

The overlay restores cached records before network work.
It uses a four-hour revalidation interval, ETag/304 handling and a four-second per-attempt timeout.
It compares remote Last-Modified against the generated baseline timestamp before using the overlay.
Within a provider it merges by `(model type, model ID)`; overlay records replace matching baseline records and add new ones.
Transient HTTP failures retain cached records and validators.
An offline setting and runtime options control model network access.
Refresh generations and publication checks prevent superseded refreshes from publishing into a replaced provider.
Source: `remote-catalog-provider.ts`; `packages/ai/src/models.ts`, refresh/publication methods.

## 3. Known models and available models are different

`getModels(provider?)` returns the known chat catalog.
`getAllModels` includes other operation types.
`getAvailable` checks provider auth and then applies `filterModels` when supplied.
`getAllAvailable` also supports a type-wide filter.
Source: `packages/ai/src/models.ts:421-477`, `:677-733`.

For a stored OAuth credential, `checkProviderAuth` only requires that the provider has an OAuth method.
This check does not validate every model or run inference.
API-key auth uses the method's check function, or credential resolution if no check exists.
Source: `models.ts:645-675`.

GitHub Copilot is a concrete account-specific filter example.
Its OAuth credential can carry `availableModelIds`, and `filterModels` intersects those IDs with the catalog.
If that metadata is absent or invalid, Pi returns the unfiltered list.
Source: `packages/ai/src/providers/github-copilot.ts:19-28`.
The inspected Anthropic/OpenAI/xAI factories have no equivalent filter.
Thus Pi's available list is useful selection data, not a universal proof of account eligibility.

## 4. Model metadata records capability and routing

Pi's model record includes `id`, `name`, `provider`, `api`, `baseUrl`, input modalities and cost.
Chat records add reasoning support, thinking-level mapping, context/output limits, cache metadata and API-specific compatibility.
Source: `packages/ai/src/types.ts:1097-1139`.
Providers can also expose chat, image-generation and classifier implementations.
`createProvider` dispatches chat requests using `model.api` and reports an error when the provider lacks that implementation.
Source: `packages/ai/src/models.ts:1038-1085`.

A shared model name does not imply identical limits, prices or wire behavior on different providers.
Each provider's record carries its own data.
For custom chat models, Pi supplies some defaults when metadata is omitted, including context/output limits and zero cost.
Those defaults are convenience values, not proof of actual limits or free inference.
Source: `packages/coding-agent/src/core/provider-composer.ts:195-233`.

## 5. Identity includes the provider

`modelsAreEqual` compares `(model type, model ID, provider ID)`.
Chat is the default type when omitted.
Source: `packages/ai/src/models.ts:1253-1256`.
`getModel(provider, id)` looks within that provider's catalog; it does not perform global bare-ID lookup.
The model picker displays the ID with a `[provider]` badge.
Source: `packages/coding-agent/src/modes/interactive/components/model-selector.ts:319-329`.

Canonical user references are `provider/modelID`.
The model ID may itself contain slashes, so simple string splitting is not sufficient.
Pi first checks full canonical matches and has special resolver branches for literal IDs with slashes.
Source: `packages/coding-agent/src/core/model-resolver.ts:88-134`, `:460-555`.

The exact-match helper accepts a bare ID only when it is unique.
The CLI resolver is more permissive: when several exact IDs match, it selects the sole configured-auth provider if there is one.
Otherwise it reports ambiguity and lists qualified choices, with a `--provider` or `provider/model` hint.
Source: `model-resolver.ts:470-511`.
Some pattern and fallback branches also use partial matching and first-match behavior.
Do not describe every Pi selection path as strict ambiguity rejection.
Ask's explicit no-silent-provider-change selection rule remains clearer for API/subscription billing choices.

## 6. Implications for Ask

- Keep a provider-scoped catalog and qualified references; no global map keyed only by model name.
- Keep metadata, configured auth, account entitlement and successful inference as separate evidence.
- Start with built-in data plus user definitions; add provider-specific discovery only where needed.
- Keep dynamic cache separate from user configuration and credentials.
- A central hosted catalog is Pi infrastructure, not a dependency Ask must add to support provider registration.
- Retain unknown metadata as unknown; do not copy guessed limits or zero-cost defaults as facts.
- Include auth-method identity in Ask's selected target because API/subscription credentials can coexist.
- If Ask later supports other operation types, extend qualified identity with model type rather than merging chat/image/classifier records.

The proposed architecture changes are in [the design plan](../261005-2139-provider-auth-design/plan.md).
Relevant Pi tests were inspected in `packages/ai/test/models-runtime.test.ts`, `packages/coding-agent/test/remote-catalog-provider.test.ts`, and model-resolver tests.
Tests were not executed in this source-only audit.

## Unresolved questions

- Catalog membership and configured auth cannot establish actual account eligibility without provider-specific discovery or an inference result.
- Ask does not yet require its own hosted catalog service; refresh ownership can remain local and provider-specific.
