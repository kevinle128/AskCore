# Xia analysis: /provider then /model flow (mode: port)

Date: 2026-10-10. Status: analysis and decision matrix. The user chose the command name `/providers` (like omp) on 2026-10-10 and the other rows stand as recommended, to be confirmed at the TUI design review. No code written yet.

## Request

User flow (user, 2026-10-10): the user adds a provider with `/provider`; after it is added, the models of that provider appear in `/model` for the user to choose. Reference omp for the flow (read through GitNexus) and Grok CLI for the ACP part.

## Source manifest

| Source | Ref | Used for | License |
|---|---|---|---|
| omp (oh-my-pi) `can1357/oh-my-pi`, GitNexus repo `agentic-os-oh-my-pi` | commit `989271449953` | `/login`, `/logout`, `/setup`, `/model` flow | MIT (reported by the research agent; the LICENSE file text was not read) |
| Pi `earendil-works/pi`, `/Users/dale/Desktop/workspace/opensources/pi` | commit `4c6fb7cf` | contrast: `/login`, `/model` shows configured providers only | MIT (reported) |
| Grok CLI `xai-org/grok-build` | commit `2bdd1d6a` | ACP authentication and model list | not checked |

GitNexus was not reachable for the omp research agent, so omp was read directly from the same checkout. Behavior is ported, no code is copied (the sources are TypeScript and Rust).

## What the sources do (verified by reading unless marked)

omp has no `/provider` command. The split is `/login` (pick provider, then OAuth or API-key paste), `/logout`, `/setup providers` (wizard, alias `/providers`) and `/model` (hub).
- A model is available when its provider is keyless or has credentials and is not disabled; the check is synchronous with no network and no token refresh (`model-registry.ts:1964-1976`).
- `/model` also lists locked providers (dimmed, with env var names and a "log in" action); Pi shows only configured providers with the hint "Use /login to add providers." (`components/model-selector.ts:110`).
- After a login omp refreshes only that provider online; a refresh of all providers reuses a fresh empty cache and hides the new models (issue 5780, test `issue-5780-repro.test.ts`). There is no event; the next open of the hub re-reads the registry.
- First run with no credentials: the app starts, no default model, warning "No models available. Use /login or set an API key environment variable. Then use /model to select a model." (`sdk.ts:2606-2610`).
- Logout lists only providers with stored credentials and warns when environment or config auth still applies.
- omp's own ACP offers only "use existing credentials" and a terminal method; login over a protocol exists only in its RPC mode (`get_login_providers`, `login` with `open_url` and input requests, `rpc-mode.ts:1415-1472`).

Grok (ACP):
- `initialize` returns `authMethods` (id, name, meta) and in meta `defaultAuthMethodId` and `modelState` (current model and model list).
- `authenticate` takes a method id and `force_interactive`; the daemon runs the browser or device flow; failure is the standard `auth_required` error with a text in `data`.
- The model list is not refreshed by `authenticate`; the client gets the list again at the next new or loaded session, or through an extension notification (`ModelChanged`). Report claims about auth URL in the `authenticate` response meta are an inference of the research agent and were not verified.
- `/login` and `/logout` have no extension methods; they use the standard calls. `/model` takes a name, optional window and effort.

## Ask today (scout, file:line)

- Standard `initialize` authMethods and `authenticate` exist and only check readiness; they never start a login (`internal/acp/agent.go:228`).
- No ACP method lists providers, starts or completes a login, saves an API key, or logs out. The login code (`internal/auth`, `ask auth login/logout`) runs on the inference host with its ASK_HOME.
- `/model` lists the whole compiled catalog (6 models); readiness is checked only when a model is chosen (`internal/tui/selector.go:287-327`, `update_input.go:389-427`).
- First run with no credentials: default model `deepseek-v4.1-flash` is shown and the first prompt fails (`internal/providers/catalog.go:10`).
- The design of the T1a TUI puts login and logout dialogs out of scope (design.md line 33); the roadmap places them in M2 (H17 gateway transport, T2 dialogs).

## Dependency matrix (omp behavior to Ask)

| Behavior | Ask today | Class |
|---|---|---|
| Provider list with status and credential origin | none | NEW (ACP method, TUI screen) |
| Add by API key (paste, validate, store 0600) | `ask auth login --method api-key` | EXISTS in CLI, NEW over ACP |
| Add by OAuth (browser, copy-code, device code) | `ask auth login` flows | EXISTS in CLI, NEW over ACP (URL and code prompts must reach the client) |
| Remove provider | `ask auth logout` | EXISTS in CLI, NEW over ACP |
| Models list shows only usable models | shows all | CONFLICT (change `get_available_models` or filter) |
| Provider-scoped refresh after login or logout | none (catalog is static; ChatGPT discovery cached for 1 minute) | NEW (cache reset for that provider) |
| Notify other clients that providers changed | none | NEW (omp has none either) |
| First run without credentials: no default, hint to `/provider` | defaults to deepseek | CONFLICT |
| Locked provider rows with env var names and "log in" action | none | NEW (optional) |
| Custom OpenAI-compatible endpoint with discovery | not in the catalog; omp has no UI either | NEW and out of scope |

## Decision matrix and recommendation

| # | Decision | omp or Pi | Grok | Our recommendation |
|---|---|---|---|---|
| 1 | Commands | omp: `/providers` (provider setup, alias of `/setup providers`), `/login`, `/model` hub; Pi: `/login`, `/model` configured only | `/login`, `/logout`, `/model` | `/providers` (user decision: list, add, remove) and `/model` (usable models only), as the user described; `/model` prints "Use /providers to add providers." when empty |
| 2 | Where credentials are entered | omp and Pi paste the key into the TUI dialog | key is read by the daemon from env or `auth.json`; no key entry in the client | Paste the key in a masked TUI input and send it once over the owner-only Unix socket to the leader, which validates and stores it; never log or echo it. Alternative: keep `ask auth login` in a shell |
| 3 | ACP shape | omp RPC: `get_login_providers`, `login` with `open_url` and input requests | standard `initialize.authMethods` and `authenticate` | Keep the standard calls for readiness. Add `_ask` extension methods: providers list, login start (returns auth URL or prompt, then input and cancel), API-key save, logout, and a `providers_changed` notification so every client refreshes |
| 4 | Model list refresh | provider-scoped online refresh after login | list comes again at the next session or by notification | After a login or logout the leader resets that provider's cache, sends `providers_changed`, and the client calls the models list again |
| 5 | First run with no provider | no default model, warning | `needs_login` screen | No default model; footer shows `no model`; a notice `Add a provider with /providers`; sending a prompt opens the same hint |
| 6 | Locked providers in `/model` | omp lists them dimmed; Pi hides them | n/a | Hide them (Pi); they appear in `/provider` with status |
| 7 | Custom endpoints | config file only | n/a | Out of scope |
| 8 | Placement | n/a | n/a | New work after T1a (call it T1c or M2 login dialogs). Needs an approved TUI design first (the `ask-tui-design` rule), then plan and cook |

## Risks

- Secrets: an API key and OAuth codes cross the client socket. The socket is owner-only (0600) and the leader already holds credentials, but this is a new path; it needs tests that logs, transcripts and event streams never contain the key.
- Login on the inference host: the OAuth browser callback runs on the daemon host. With a remote leader (cloud agent) the TUI cannot open the browser; copy-code and device code work, plain browser callback does not.
- Concurrent clients: two clients may start a login at once; the daemon must serialize per provider (the auth store already fences by generation).
- Changing `get_available_models` to return only usable models changes an existing ACP contract used by `ask connect` and the TUI; version or add a field instead of removing rows.
- The TUI design (S11, S12) and criterion 10 change visibly; they return to draft and need approval.

## Unverified

- omp `LoginDialogComponent` layout; `onGenerationChanged` listeners; model cache TTL (2 h or 24 h).
- Grok details about browser URL delivery in `authenticate` and Grok license.
- Remote leader behavior for OAuth callbacks in Ask.
