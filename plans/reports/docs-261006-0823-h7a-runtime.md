# Native auth documentation update

Status: DONE

## Outcome

The owning documentation now explains native auth operation, local credential recovery, and package boundaries.
No product code, generated file, changelog, plan status, or pre-existing ADR was changed.
Other agents' prior changes were kept.

## Updated claims and evidence

| Owning document | Claim or decision | Evidence |
| --- | --- | --- |
| [Root README](../../README.md#native-auth-and-headless-prompts) | Native login and prompt operation without database or leader; explicit Anthropic browser/copy-code; default ChatGPT browser and new-account replacement; xAI device approval; private API-key input. | `cmd/tui/auth_args.go`, `auth_input.go`, `auth.go`, `headless.go`, `internal/app/auth_native.go`, and native strategy files. |
| [Root README](../../README.md#native-auth-and-headless-prompts) | Same inference host and ASK_HOME; isolated live home; fixed loopback ports and loopback forwarding; callback receipt is not saved-credential success. | `internal/settings/paths.go`, `internal/auth/anthropic.go`, `chatgpt.go`, `callback.go`, and the accepted command contract. |
| [Root README](../../README.md#native-auth-and-headless-prompts) | One credential/account per provider; replacement; local-only logout; environment keys remain eligible; no retained ChatGPT client registration after record deletion; no claim of full OpenAI account/session compliance. | `internal/auth/login.go`, `service.go`, `chatgpt.go`, `internal/settings/auth.go`, and the accepted product decision. |
| [Settings README](../../internal/settings/README.md#credential-transaction) | Explicit pending-fence recovery; pre-rename preservation and post-rename uncertainty; 15-second exchange, separate five-second commit, 20-second drain; forced kill and blocked OS sync limits; one authoritative local filesystem. | `internal/settings/auth.go`, `internal/app/module_auth.go`, and `internal/auth/service.go`. |
| [Auth README](../../internal/auth/README.md), [app README](../../internal/app/README.md), [providers README](../../internal/providers/README.md), [package map](../../internal/README.md), and [architecture](../../docs/ask-architecture-reference.md) | Native auth owns protocols and identity; settings owns files; ordinary NewNativeAuth and BindAuth composition; providers receive request-local snapshots and own final guards without auth/store imports. | `internal/app/auth_native.go`, `module_auth.go`, `internal/providers/auth.go`, `anthropic/profile.go`, `openai/profiles.go`, and `.golangci.yml`. |
| [Capture guide](../../docs/testing-llm-cassettes.md#native-authentication-and-capture) | Native inference capture excludes login, refresh, JWKS, and account discovery; cassette content still needs private-data review; offline checks do not establish live acceptance. | `cmd/tui/capture.go`, `headless.go`, `internal/auth/http.go`, `identity.go`, `discovery.go`, and compiled command tests. |
| [Agent context](../../AGENTS.md) | Reuse ordinary auth composition; keep private auth HTTP outside capture; use manifest authority for installed OIDC identity dependency. | `go.mod` lists `github.com/coreos/go-oidc/v3 v3.21.0`; `internal/auth/identity.go` imports it. |

## Validation

Read the complete ak:docs skill and its content, update, and agent-context references.
Read the accepted plan and cumulative documentation requirements, package guides, current source, and implementation reports.
No repository documentation validator was found in the inspected file routes.
A local path and Markdown heading check passed for all 57 links in the nine edited owning documents.
Each owning document remains below 800 lines.
`git diff --check` passed for the edited tracked documents.
`go run ./cmd/tui auth --help` exited zero and confirmed provider, method, private-input, host, and local-logout guidance.
The help command made no login or inference request and started no background process.
No live account, credential, or subscription inference was used for this documentation task.
No offline pass was described as live acceptance.

## Concerns

Live acceptance status is owned by the controller's execution record.
This documentation task made no live acceptance claim.
Implementation reports are stateful evidence and were not converted into completion claims.

## Final model-selection update

The ChatGPT prompt example explicitly selects `gpt-5.6-sol`.
The root and provider guides explain that model access varies by account and that a known denial stops the request without fallback.
The evidence is `internal/providers/catalog.go`, `api.go`, `internal/auth/discovery.go`, and `service.go`.
No price or capability inventory was copied into documentation.
The default model was not changed by this documentation task.
The final local link and heading check passed for all 26 links in the two updated guides and this report.
`go run ./cmd/tui --help --provider openai --model gpt-5.6-sol` exited zero without login or inference.
`git diff --check` passed for the two updated guides.

## Unresolved questions

None for documentation scope.
