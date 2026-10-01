# Go ecosystem survey for a Pi-style agent with extensions (lane I)

Date: 2026-09-30. Target: AskCore (Go 1.25, fx, echo, grpc, zap, gorm/sqlite; go.mod checked).
Repo metadata (stars, push date, license, latest release) was pulled from the GitHub API on 2026-09-30 and is verified. Latency, memory, and cgo cost figures are NOT measured here; they come from general knowledge and are marked "unmeasured". I did not re-read the three prior Ask reports; this is an independent survey.

## Bottom line

1. Extension runtime: layer two mechanisms.
   - Primary (out-of-process): a subprocess speaking JSON-RPC over stdio, in the LSP/MCP style. Use it for tools, providers, commands, TUI dialogs, and blocking hooks. This is the only option that gives Pi-like power with real isolation and cross-platform support in pure Go.
   - Secondary (in-process, cheap): shell-command hooks, modeled on Claude Code and Crush. They cover the 80% case of "block or rewrite this tool call".
   - Defer embedded JS (goja). Add it only if you must run existing Pi TypeScript extensions unchanged. goja cannot run most real npm/Node-API code, so it will not give Pi compatibility anyway.
2. LLM layer: write a thin own layer (pi-ai style) with one adapter per wire protocol, built on the three official SDKs (anthropic-sdk-go, openai-go, go-genai). Do not adopt langchaingo. Treat eino and charm's fantasy as reference designs, not dependencies.
3. MCP: use modelcontextprotocol/go-sdk (official) for the client side. mark3labs/mcp-go is the fallback if you hit a gap. Pin versions; both are moving fast.
4. Coding-tool libs: mostly small, healthy, MIT libs. Details and picks in section 4.
5. Existing Go agents: Crush (charmbracelet) is the only serious Go peer, and its extension story is MCP + shell hooks + skills, with a full plugin API explicitly deprioritized. That validates the layered plan above.

## 1. Extension runtime options

### Key requirement: synchronous, mutating hooks
A Pi hook must be able to block or modify a tool call before it runs. That means the agent loop waits on the extension. Any runtime works if you accept a round trip. The question is cost, isolation, and author DX.

### Comparison

| Option | Status (2026-09-30) | License | Sync block/modify hook | Latency (unmeasured) | Sandboxing | Hot reload | Author DX | Cross-platform |
|---|---|---|---|---|---|---|---|---|
| Subprocess JSON-RPC over stdio | Pattern, not a lib (LSP, MCP, ACP) | n/a | Yes: request/response, agent awaits reply | ~0.1-1 ms per call after spawn; spawn 10-100+ ms (runtime dependent) | OS process; add seccomp/sandbox-exec/job objects yourself | Restart the child | Any language; needs an SDK per language to feel good | Excellent |
| hashicorp/go-plugin (gRPC subprocess) | 6.1k stars, pushed 2026-09-28, v1.8.0 (2026-04-29) | MPL-2.0 | Yes (gRPC unary) | Similar to stdio | Process only; has mTLS and checksum | Restart the child | Go-first; other languages via gRPC but clumsy | Good (no Windows unix-socket issue via TCP) |
| goja (+ goja_nodejs) | 7.1k stars, pushed 2026-09-30; goja_nodejs pushed 2026-09-18 | MIT | Yes, in-process call; single-threaded per Runtime | Microseconds per call; JS is slow (no JIT, tree-walking/bytecode VM) | Weak: no OS isolation; you control exposed globals, but CPU/memory limits need Interrupt() and your own accounting | Cheap: new Runtime per reload | ES5.1 plus most of ES6+; no native TypeScript; no Node APIs beyond goja_nodejs shims (require, console, process, buffer, url; no fs/net/child_process) | Excellent (pure Go) |
| esbuild Go API (for TS transpile) | 40k stars, v0.28.2 (2026-08-08), MIT | MIT | n/a (compile step) | ~ms per file | n/a | Rebuild on change | Strips types and bundles TS to one JS file; you can embed it as a library | Excellent (pure Go). Pre-1.0 versioning, so pin |
| v8go (rogchap) | 3.5k stars, last push 2024-08-02 | BSD-3 | Yes | Fast JIT | V8 isolates | Yes | Full modern JS | Poor: cgo, prebuilt V8 archives, effectively unmaintained. Reject |
| QuickJS bindings: buke/quickjs-go (v0.7.6, pushed 2026-09-28), fastschema/qjs (WASM QuickJS via wazero, pushed 2025-12-22), modernc/quickjs (repo path 404 on GitHub API, not verified) | Small communities (185 and 610 stars) | MIT | Yes | Faster startup than V8; ES2020+ | buke: cgo. qjs: runs inside wazero sandbox with memory limits | Yes | Modern JS incl. async/await, ES modules; still no Node APIs | buke: needs a C toolchain (bad for cross-compile). qjs: pure Go |
| wazero (WASM runtime) | 6.4k stars, v1.12.0 (2026-05-29), pushed 2026-09-28 | Apache-2.0 | Yes: host function calls both ways; guest is single-thread | ~us per call; module instantiate ~ms; compiler mode is fast | Strong: capability-based, memory/time limits | Yes (recompile module) | Poor to medium: authors need Rust/Go(TinyGo)/AssemblyScript/JS-in-WASM toolchains; component model not supported by wazero | Excellent (pure Go, no cgo) |
| Extism (go-sdk on wazero) | extism/extism 5.8k stars pushed 2026-09-02; go-sdk v1.7.1 released 2025-03-19, pushed 2025-05-14 | BSD-3 | Yes | Like wazero, plus (de)serialization | Strong; HTTP and host functions are allow-listed | Yes | Better than raw wazero: PDKs for Rust, Go, JS, Python, C#, Zig. JS PDK is QuickJS in WASM | Excellent. Concern: the Go SDK looks stagnant (last push 2025-05) |
| yaegi (Go interpreter) | 8.4k stars, v0.16.1, last push 2026-02-09 | Apache-2.0 | Yes | Slow (interpreted) | None | Yes (re-eval) | Authors write plain Go; stdlib support good, generics and newer language features incomplete; no cgo | Excellent. Activity has slowed |
| gopher-lua | 7.0k stars, pushed 2026-04-01 | MIT | Yes | Fast startup, small | Good: you choose which libs open; no fs/os unless you load them | Yes | Lua 5.1; small language; unfamiliar to JS/TS extension authors | Excellent |
| Go native `plugin` pkg | stdlib | BSD | Yes | Fast | None | No (cannot unload) | Terrible | Linux/macOS only; needs identical toolchain, flags, and dependency versions; cgo. Reject |

Notes:
- The "latency" column is qualitative and unmeasured. If a decision hangs on it, benchmark a no-op hook in each candidate (30 minutes of work).

### What this means for Pi parity
- Pi extensions are TypeScript running in-process with full Node access and the Pi API objects. There is no Go runtime that runs this code faithfully. Options to run TS extensions: (a) embed a real Node/Bun as a sidecar (subprocess, so back to option 1), or (b) transpile with esbuild and run in goja/QuickJS, which loses Node APIs.
- So "run Pi extensions unchanged" implies a Node/Bun sidecar plus a JSON-RPC bridge. That is the honest path if compatibility matters.
- If compatibility does not matter, define Ask's own extension protocol (see recommendation) and ship a TypeScript SDK that talks to it over stdio.

### Fit to AskCore
- AskCore is a daemon with gRPC and echo already. A subprocess protocol fits the existing `internal/tools`, `internal/hooks`, `internal/mcp`, `internal/skills` scaffolds (per the project CLAUDE.md map).
- fx makes it easy to give each extension host a lifecycle (OnStart/OnStop kill the child), which also helps with the process-management rule about orphaned processes.

### Recommendation (ranked)
1. Subprocess JSON-RPC over stdio as the extension protocol. Reuse MCP framing and message shapes where they overlap (tools, prompts, resources). Add Ask-specific methods for hooks (`hook/beforeToolCall` returning allow/deny/modify), commands, and UI requests. This mirrors Pi's RPC mode and Crush/Claude hooks. Write it as JSON-RPC 2.0 so the TS SDK is trivial.
2. Shell-command hooks (Claude Code compatible) as the zero-dependency tier. Crush already ships this and documents it as Claude Code-compatible (https://github.com/charmbracelet/crush/tree/main/docs/hooks).
3. Optional later: WASM via wazero if you need a safe in-process plugin for untrusted third parties. Prefer wazero directly or wazero+Extism only after checking Extism Go SDK maintenance.
4. Optional later: goja + esbuild for tiny inline scripts (config-level `if tool == "bash"` logic). Only if users ask.
5. Reject: v8go, native `plugin`, yaegi (low activity, slow, no isolation), hashicorp/go-plugin unless extensions are written in Go only (MPL-2.0 is fine as a dependency but the gRPC codegen burden is a poor DX for TS authors).

Adoption risk: subprocess protocols carry no library risk but a large design cost (versioning, capability negotiation, error semantics). That is the price of getting it right once.

## 2. LLM SDKs

| Lib | Version / activity | License | Notes |
|---|---|---|---|
| anthropics/anthropic-sdk-go | v1.76.0 (2026-09-28), 1.2k stars | MIT | Official, generated (Stainless). Streaming with accumulator helper, tools, extended thinking, `cache_control`, images/PDF, beta namespace. Very frequent releases; API surface churns |
| openai/openai-go | v3.68.0 (2026-09-29), 3.5k stars | Apache-2.0 | Official, generated. Chat Completions and Responses API, streaming, tools, reasoning. Major-version path `/v3`: expect breaking bumps |
| googleapis/go-genai | v1.71.0 (2026-08-31), 1.2k stars | Apache-2.0 | Official Gemini/Vertex SDK; streaming, function calling, thinking config, caching resources, images |
| tmc/langchaingo | v0.1.14 (2025-10-20), last push 2026-01-11, 9.7k stars | MIT | Lowest common denominator abstraction; lags provider features (thinking, caching). Slowing. Reject |
| cloudwego/eino | 13.2k stars, pushed 2026-09-29 | Apache-2.0 | Full agent framework (graphs, components, ByteDance). Heavy and opinionated; would fight AskCore's own agent/pipeline packages |
| nlpodyssey/openai-agents-go | 273 stars, pushed 2026-03-26 | Apache-2.0 | Port of OpenAI Agents SDK; too small and OpenAI-shaped |
| charmbracelet/fantasy (charm.land/fantasy) | 1.0k stars, pushed 2026-09-30 | Apache-2.0 | Multi-provider Go agent lib from the Crush team (the README pitches "Multi-provider, multi-model, one API"). Closest thing to pi-ai in Go, but young and tied to Crush's needs |

Feature verification: I confirmed repo health and versions. I did NOT verify each SDK's support of thinking, caching, and image blocks by reading source (a README grep for "cache|thinking" on anthropic-sdk-go returned 0 hits, which means the README is thin on it, not that the feature is missing). Check `message.go` / `CacheControlEphemeralParam` before building on it.

Recommendation:
- Write a thin own layer: an internal `providers` interface with normalized events (text delta, thinking delta, tool-call delta, usage, stop reason) and per-provider adapters built on the official SDKs. Reasons: (1) prompt caching, thinking blocks (signatures must round-trip), and tool-call streaming differ per provider in ways generic abstractions flatten; (2) Pi extensions register providers, so the interface must be yours; (3) three official SDKs are actively maintained by the vendors.
- Add an OpenAI-compatible adapter using openai-go with a custom base URL for the long tail (OpenRouter, Ollama, vLLM, etc.).
- Read fantasy's provider interface before designing yours. Cheap, and it shows what a Go team hit in practice.
- Risk: generated SDKs release almost daily. Pin and upgrade on a schedule.

## 3. MCP

| | modelcontextprotocol/go-sdk | mark3labs/mcp-go |
|---|---|---|
| Latest | v1.8.0 (2026-09-14), pushed 2026-09-30, 5.2k stars | v1.1.1 (2026-09-23), 9.1k stars |
| License | Mixed: repo is moving MIT to Apache-2.0; older contributions stay MIT (LICENSE text read) | MIT |
| Maintainers | Official MCP org with Google collaboration | Community (Ed Zynda et al.) |
| Client + server | Both; stdio, streamable HTTP, SSE; auth support | Both; same transports |
| Fit | Tracks the spec first; semver 1.x commitment | Larger installed base and more examples |

Recommendation: go-sdk for a new project. It is official, past 1.0, and current. Watch the license mix (Apache-2.0/MIT, both permissive, so low risk but note it in a NOTICE file). Keep MCP behind your own `internal/mcp` interface so swapping is a one-file change. Unverified: feature parity on elicitation, sampling, and roots between the two.

## 4. Supporting libs for coding tools

| Need | Pick | Alternatives / notes | Evidence |
|---|---|---|---|
| Apply edits / diff generation | aymanbagabas/go-udiff (BSD-3, pushed 2026-07-16, 235 stars) | sergi/go-diff (MIT, 2.1k stars, last push 2025-06, character-level diff-match-patch, not unified diff). sourcegraph/go-diff parses unified diffs (license shows NOASSERTION, pushed 2026-09-14). Pi's edit tool is exact-string replace, so you mostly need a diff for display and result summaries, not a patch engine | GitHub API |
| Parsing model-emitted patches | Write it yourself | Formats (apply_patch, search/replace) are model-specific and small | judgment |
| Fuzzy file search | sahilm/fuzzy (MIT, pushed 2026-06-24) | Enough for file pickers. For big trees shell out to `fd`/`rg --files` | GitHub API |
| gitignore matching | go-git/go-git `plumbing/format/gitignore` (Apache-2.0, very active) | sabhiram/go-gitignore (last push 2024-02) and monochromegane (2023-05) are stale. go-git is a large dependency for one subpackage, but you can import just the subpackage | GitHub API |
| Glob (`**`) | bmatcuk/doublestar v4 (MIT, pushed 2026-09-20) | Standard `filepath.Match` lacks `**` | GitHub API |
| Content search | Shell out to `rg` (BurntSushi/ripgrep, pushed 2026-08-04) with a Go fallback (`filepath.WalkDir` + `regexp` + gitignore) | No mature pure-Go ripgrep. I did not find one worth naming. Crush and Pi both shell out | search returned nothing usable |
| PTY / process management | creack/pty (MIT, v1.1.24, pushed 2026-06-01) | Unix only; Windows needs ConPTY (e.g. UserExistsError/conpty or aymanbagabas/go-pty; not verified here). Use `os/exec` with process groups (`Setpgid`) for non-interactive bash | GitHub API |
| Shell detection | Do it yourself: `$SHELL`, then bash/zsh/sh lookup via `exec.LookPath`; PowerShell/cmd on Windows | Trivial code; no lib worth a dependency | judgment |
| JSON Schema from Go types | invopop/jsonschema (MIT, pushed 2026-04-23, 958 stars) | google/jsonschema-go (MIT, pushed 2026-05-22, 480 stars) is what the official MCP go-sdk uses, so it aligns with MCP tool schemas. Pick one; if you adopt go-sdk, prefer google/jsonschema-go to avoid two schema libs (DRY). Extension-registered tools arrive as raw JSON Schema anyway, so Go-type reflection is only for built-in tools | GitHub API |
| JSON Schema validation of tool args | Choose a validator (e.g. santhosh-tekuri/jsonschema); not surveyed | Needed to validate extension-supplied schemas. Unverified pick | gap |
| Partial JSON for streaming tool args | Write a small tolerant parser (~100 lines) or use tidwall/gjson only for reads on complete docs | My searches for a maintained Go partial-JSON library found nothing credible. Pi uses a TS partial-json lib for live UI previews; in Go you only need it for display, since execution waits for the complete args. Anthropic's SDK accumulator concatenates `input_json_delta` for you | search returned nothing |
| Token counting | Estimate: chars/4 for budgeting, with the provider's usage numbers as truth. pkoukk/tiktoken-go (MIT, pushed 2026-05-12, ~1k stars) for OpenAI-family exact counts | Anthropic and Gemini offer server-side count-tokens endpoints; no offline Go tokenizer for Claude that I can vouch for. tiktoken-go downloads BPE files at runtime unless you embed the offline loader | GitHub API |

## 5. Existing Go coding agents

| Project | Status | Extension approach | Community signal |
|---|---|---|---|
| charmbracelet/crush | v0.97.1 (2026-09-29), 28.4k stars, pushed 2026-09-30. License is FSL-1.1-MIT (source-available for two years, then MIT). Not OSI-open now: check before copying code | MCP servers (stdio/http/sse), LSP integration, skills, and shell-command `PreToolUse` hooks that can block, rewrite input, inject context, auto-approve; Claude Code compatible; only that one hook so far. Source: https://github.com/charmbracelet/crush/tree/main/docs/hooks | The search for plugin issues in the Crush repo returned unrelated-looking issue numbers (#1-#225), and #1661 fetched as a different issue than its title, so I could not confirm the "[Deprioritized] Plugin/extension API + marketplace" issue text. Treat "plugin API deprioritized" as unconfirmed |
| opencode-ai/opencode (Go) | Archived; last push 2025-09-18, 13.8k stars, MIT. The team moved to a TypeScript rewrite | Was MCP only | Confirms the original Go version was abandoned in favor of TS |
| anomalyco/opencode (TS, formerly sst/opencode) | 211k stars, pushed 2026-09-30, MIT | TS plugins + hooks in-process, plus MCP | Shows the market pull toward in-process TS plugins, the exact thing that is hard in Go |
| goose (aaif-goose/goose) | 54.8k stars, Apache-2.0. Rust, not Go | MCP as the extension mechanism | Cross-reference to the existing goose-vs-pi report |

Lessons:
- No Go agent ships an in-process plugin runtime. The best-funded Go one chose MCP + shell hooks. The one that wanted rich plugins rewrote in TypeScript.
- Licensing caveat on Crush: reading it for design is fine; copying code needs an FSL review.

## Decision matrix for AskCore

| Decision | Choice | Confidence | Main risk |
|---|---|---|---|
| Extension protocol | Stdio JSON-RPC, MCP-shaped, plus hook methods | High | Protocol design effort and versioning |
| Lightweight hooks | Claude-compatible shell hooks | High | Process spawn cost per hook (~10 ms scale, unmeasured) on hot tool paths |
| Embedded scripting | None at first | Medium | Users may expect in-process TS like Pi; mitigated by a TS SDK over stdio |
| LLM layer | Own thin layer over official SDKs | High | Ongoing adapter maintenance |
| MCP | modelcontextprotocol/go-sdk | Medium-high | Fast releases, license mix |
| Diff | go-udiff | Medium | Low star count but maintained by a Charm-adjacent author |
| Search | Shell out to rg with fallback | High | rg not installed on host |

## Limitations
- No benchmarks were run. Latency and cgo-cost statements are unmeasured.
- I did not read SDK source to verify thinking, caching, or image support in each provider SDK.
- I did not review the three prior Ask reports, so overlaps or contradictions with them are unchecked.
- Windows PTY, JSON-schema validator choice, and MCP go-sdk vs mcp-go feature parity were not researched in depth.

## Unresolved questions
1. Must Ask load existing Pi TypeScript extensions unchanged? If yes, a Node/Bun sidecar is required and the Go-side runtime choice becomes moot.
2. Is Windows a supported target for the daemon? It affects PTY, sandboxing, and shell hooks.
3. Which trust model applies to extensions (user-authored only, or third-party marketplace)? Third-party pushes toward WASM or OS sandboxing.
4. Does Crush have an open plugin API decision? I could not confirm the issue text.
5. Should the Crush FSL-1.1-MIT license restrict how much Crush code Ask may reference?

Status: DONE_WITH_CONCERNS
