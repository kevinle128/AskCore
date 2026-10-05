# Handoff: H4 OAuth subscription research synthesis

## Mission and current status

**Desired outcome.** The user's Alibaba Token Plan subscription is close to expiry, so they want H4 to gain a way to use provider subscription plans (not only pay-per-token API keys). They asked to study how Pi does OAuth login for Anthropic (Claude Pro/Max), OpenAI (ChatGPT), and xAI (Grok), through `/ak:xia --port`.

**Done.** Four research lanes are complete and written to `plans/reports/`:
- `researcher-261005-2115-h4-oauth-anthropic.md`
- `researcher-261005-2115-h4-oauth-openai-chatgpt.md`
- `researcher-261005-2115-h4-oauth-xai-grok.md`
- `researcher-261005-2115-h4-oauth-shared-infra.md`

Policy checks were verified against primary sources (Anthropic legal page, OpenAI SIWC docs, web search).

**Remaining.** Write the single consolidated `xia --port` report (decision matrix, dependency matrix, risk score) that the xia skill owes, then ask the user the open decisions one per turn. The report is NOT yet written. No roadmap edit has been made for OAuth.

**Priority.** Medium-high: driven by the Token Plan expiry, so the practical replacement options (API-key coding plans) matter as much as OAuth.

## Scope and guardrails

**Repository.** `/Users/dale/orca/workspaces/AskCore/master-2`, branch `master-2`. The Ask project rebuilds the Pi harness in Go.

**Hard refusal — do not implement, design, or write code for Anthropic subscription OAuth that impersonates Claude Code.** Pi's method uses the Claude Code OAuth client id, `user-agent: claude-cli`, a Claude Code identity line in the system prompt, and Claude Code tool-name rewriting. Anthropic's published policy (https://code.claude.com/docs/en/legal-and-compliance, read 2026-10-05) forbids third-party apps from routing requests through Free/Pro/Max credentials and from storing Claude.ai credentials, and an Anthropic engineer described blocking tools that "spoof the Claude Code harness". This is access-control circumvention. The user asked twice for it and was told no each time; the refusal stands regardless of model or of Pi having shipped it. Do not re-open this by writing the spec. Anthropic stays API-key only (roadmap D9 unchanged).

**Permitted.** Research/report writing for the OpenAI ChatGPT and xAI Grok OAuth paths (present facts + each vendor's terms, let the user decide), and for API-key coding-plan providers as Token Plan replacements. This skill invocation is documentation only.

**Workflow constraint (user CLAUDE.md).** Ask the user ONE question per turn, each self-contained: framing, why it matters, options, recommendation, explicit ask. Explain domains in plain words before citing internal labels (see `tasks/lessons.md`).

## Current state

- Branch `master-2`, HEAD `e2f07a1d31afc7b6e2d063d8a23a14e9061c3edf` ("add headless_live_test").
- `go.mod:402` already has `replace charm.land/fantasy => github.com/kevinle128/fantasy v0.0.0-20261005094512-08976763bfea` (applied earlier this session; build, `go test ./...`, `go test -race` on providers/agent, and golangci-lint all passed). This is uncommitted.
- Uncommitted changes exist from a parallel H-LOOP-15 / tool-registry effort (another session owns these): `internal/agent/loop_*.go`, `internal/providers/tokenplan/document.go`, `internal/providers/transcript.go`, `internal/tools/registry.go`, new tests, and `plans/261005-2059-tool-registry-snapshot/`. Do NOT edit those files; they are not part of this OAuth task.
- The OAuth research reports are untracked (`??`) but intentional.
- Roadmap `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` has the H4 wire-API + fantasy-fork decisions (section 9) from earlier today; it has NO OAuth content yet.

## Decisions and rationale

- **Anthropic OAuth: refused on policy grounds** (see guardrails). Keep roadmap D9 "API keys only".
- **xia mode `--port`, provider wire layer is the fantasy fork** (D22). xAI and OpenAI ChatGPT both use the `openai-responses` API that H4 already builds, so the wire plumbing largely exists.
- **Real problem is Token Plan expiry**, so the synthesis must also cover API-key coding plans Pi already supports: Qwen Token Plan (`openai-completions`, the current H4 target), Kimi Coding (`anthropic-messages`, also has OAuth), Z.AI / Z.AI Coding CN (`openai-completions`), MiniMax (`anthropic-messages`), Moonshot (`openai-completions`), Xiaomi Token Plan SGP/CN/AMS (`openai-completions`). These need only a provider+model record, no OAuth.
- Alternatives rejected for Anthropic: spawning the unmodified Claude Code binary over ACP (user said "no ACP").

## Work performed

- Ran 4 `researcher` lanes (GitNexus + source + web). Lead also ran GitNexus cypher: Pi has `lazyOAuth` on anthropic, openai, openai-codex, xai, github-copilot, openrouter, kimi-coding, meta, radius. Per-request refresh is in `coding-agent/src/core/model-runtime.ts`; refresh runs inside the `auth.json` file lock (`ai/src/auth/resolve.ts:110-162`).
- Verified Anthropic legal page and OpenAI SIWC quickstart directly (firecrawl), plus web search on both vendors' third-party policies.
- Key findings per lane are in the four report files; read them, do not re-run the lanes.
- OpenAI SIWC (https://developers.openai.com/siwc/quickstart, read 2026-10-05): "Sign in with ChatGPT is currently available to selected commercial partners through a limited trial. ChatGPT plan usage is available to all open-source partners and selected private clients." Current flow uses the public `api.openai.com/v1/responses` with a dynamically registered `oaiapp_*` client id, PKCE, redirect `http://127.0.0.1:1455/auth/callback`, scope `chatgpt.tokens.use.direct`. Pi's legacy `openai-codex` backend (`chatgpt.com/backend-api/codex`) should be skipped. OpenAI docs say not to point at `backend-api` endpoints.
- xAI: Pi uses an RFC 8628 device-code flow (no PKCE, no callback) to `auth.x.ai`, client id = Grok CLI, token sent as Bearer to `api.x.ai/v1/responses` (same as `XAI_API_KEY`). No official xAI statement found permitting/forbidding third-party OAuth; xAI Terms page returned 403.
- fantasy fork fit for OpenAI ChatGPT current flow and xAI: mostly covered (base URL, headers, `store:false`, streaming, developer role, error codes). Gaps: per-request (rotating) Bearer token injection, `max_output_tokens` not auto-stripped, `include: reasoning.encrypted_content` must be set by Ask, `grok-*` needs a `WithResponsesAPIFunc` override. Details in the openai-chatgpt and xai-grok reports.

## Verification

- Primary-source policy reads confirmed (Anthropic legal page; OpenAI SIWC quickstart). Web search corroborated the Anthropic third-party ban timeline (2026-01-09 enforcement, 2026-02-19 doc clarification, 2026-04-04 Max-limit block).
- No live OAuth/IdP calls were made. Refresh-token rotation claims come from reading Pi source only.
- Pi model catalog JSON is git-ignored and absent, so concrete model ids per provider were not listed.
- `go.mod` replace was build/test/lint-verified earlier this session (not re-run in this handoff).

## Open risks and blockers

- **Anthropic OAuth is out of scope by policy** — not a blocker to route around, a boundary to respect.
- **OpenAI ChatGPT OAuth eligibility**: Ask would need to qualify as an open-source partner / selected private client; commercial/cloud use needs a waitlist. Open question for the user.
- **xAI OAuth**: no official third-party permission found. Policy risk resembles the Anthropic case (presenting a first-party CLI client id). Surface to the user; do not silently adopt.
- **Credential store does not exist in Ask yet**: `internal/settings` and `internal/crypto` are stubs; the `GetAPIKey` hook returns only a string (too narrow for Bearer+headers auth). OAuth needs a store+lock+resolver, which the roadmap puts in H7 (after H4). The shared-infra report proposes splitting H4 into H4a (wires) + H4b (auth) before H5. This reverses the M1/M2 split and H7 ordering — needs explicit user confirmation.
- Decisions that each OAuth option would reverse: D9 (Anthropic), M2 status of `/login`, H7 credential-store ordering.

## Exact next actions

**First safe step.** Read the four `plans/reports/researcher-261005-2115-h4-oauth-*.md` reports and the policy quotes in this handoff; do not re-run the research lanes.

1. Write the consolidated xia `--port` report to `plans/reports/xia-261005-<HHmm>-h4-oauth-subscription-port-analysis.md` with: source manifest; per-provider summary; **decision matrix**; **dependency matrix** (Pi component → Ask target, EXISTS/NEW/CONFLICT); risk score. Mark Anthropic OAuth as "refused, out of scope, policy" with the citation — do not include an implementation path for it.
2. Include a section comparing API-key coding-plan providers (Qwen/Kimi/Z.AI/MiniMax/Moonshot/Xiaomi) as the near-term Token Plan replacement, since that is the user's actual expiry problem and needs only record data.
3. Then ask the user, one question per turn (plain-language framing first): (a) which replacement path for the expiring Token Plan — a new API-key coding plan vs OpenAI ChatGPT OAuth vs xAI OAuth; (b) if OAuth, accept the eligibility/policy risk per vendor; (c) scope/ordering — split H4 into H4a/H4b or defer auth to H7.
4. Only after the user picks, update the roadmap (new section + H4 owns-list) following the pattern of roadmap section 9. Do NOT edit roadmap before the user decides.

## Source pointers

- Reports: `plans/reports/researcher-261005-2115-h4-oauth-{anthropic,openai-chatgpt,xai-grok,shared-infra}.md`
- Prior H4 analysis + decisions: `plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md`; `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` (section 9), `inventory-harness.md:130-141` (H-AUTH-01..12).
- Pi source (`/Users/dale/Desktop/workspace/opensources/pi`, ref `4c6fb7cfe`): `packages/ai/src/auth/oauth/{anthropic,openai-chatgpt,openai-codex,xai,pkce,callback-server,device-code,load,meta}.ts`; `packages/ai/src/auth/{types,resolve,helpers,credential-store}.ts`; `packages/coding-agent/src/core/{auth-storage,model-runtime,runtime-credentials}.ts`.
- Policy: https://code.claude.com/docs/en/legal-and-compliance ; https://developers.openai.com/siwc/quickstart
- Lessons to honor: `tasks/lessons.md` (explain domain before options; test the real code path).
