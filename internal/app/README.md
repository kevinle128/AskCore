# `internal/app`

Composition root. fx modules that build every package, bind implementations to interfaces with `fx.As`, and map `config.Config` to the typed config of each package.

## What belongs here

- fx modules and lifecycle hooks

## Native headless composition

[NewNativeAuth](auth_native.go) is the ordinary constructor for native credential methods and the local settings store.
[BindAuth](module_auth.go) installs the same resolver for model readiness and each inference request; `AuthWait` owns shutdown drain.
[cmd/tui](../../cmd/tui/headless.go) uses these constructors without the database, gateway, or leader.
Keep this composition reusable by later fx runtimes instead of adding command-specific auth services.
Auth and inference use separate HTTP clients so capture cannot record token exchange, identity keys, or account discovery.

## Editor connection composition

[ACPModule](module_acp.go) provides `ACPRuntime` for `ask acp` from `ACPParams`: the native auth service (`NewNativeAuth`), the adapter callbacks and `Cleanup`, which stops the credential refresh and waits for `AuthWait`.
The module reads no configuration, opens no database and starts no listener. `fx.ValidateApp` in [module_acp_test.go](module_acp_test.go) proves that the graph stands alone.
The command supplies `NewAgent(service, AgentSpec)`, which must call `NewNativeAgent` with the given service, so all sessions share one credential resolver and direct headless runs keep their behavior. `AgentSpec` holds the session ID, the cwd, the start model and the `SessionStart` of the session.
`Authenticate` and `ModelAuth` only check readiness through `NativeAuthReady`; they never start a sign-in. The adapter gets function fields, never the service.

The model list comes from the [model refresher](model_refresh.go), a field of `ACPRuntime`. It merges the built-in catalog, the models.dev overlay and the account lists of the cache (`<ASK_HOME>/model-cache.json`) into one `providers.ModelSet` and swaps it atomically.
It loads the cache again when the file changed, and a model list request with a stale cache (2 h) starts one background refresh on its own root context; a failure keeps the last list and waits 5 min. There is no timer and no polling.
A credential change of the leader broker only schedules work (the broker holds its change lock); a result from before the change is dropped. `ACPParams.ModelRefresh` turns the network part on; the zero value reads the cache only, which keeps tests offline. `Cleanup` stops the refresher before the credential refresh drain.

## Session start

[PrepareSession](session_start.go) does all the file reads of one session start, for print mode, `ask settings show` and the ACP Factory (so the leader, the in-process TUI and `ask acp`). The order is fixed: the trust decision and the settings layers (no project file opens before the decision), the tool selection (`defaultTools` setting, then `--tools`/`--no-tools` on top), the prompt files (`SYSTEM.md`, `APPEND_SYSTEM.md` and the prompt flags; the project folder only when the project is trusted), the context files (`AGENTS.md` and `CLAUDE.md`, for every project) and the skills. Project skill folders come from the gated list that the trust decision used, and only for a trusted project.
The Factory reads the flags from the session request, never from process-wide state, because sessions start concurrently. `SessionInput.Now` (`ACPParams.Now`) sets the date once for the session.
`SessionStart.Render(snapshot, contextWindow)` is pure and returns a `RenderResult` (`SystemPrompt`, `Seed`, `Warnings`). It builds the system prompt (sections in the order of `agent.SystemPrompt`) and the seed items (`context-file`, then `skill-list` when `read` or `bash` is registered). The skill list gets the budget of `skills.BudgetFor(contextWindow, skillsMaxContextTokens)`, and the one budget warning goes to `RenderResult.Warnings`; the caller that builds the agent hands it to `AgentSpec.Warn` (stderr in `ask -p`, the session warnings of the ACP Factory). The tool set is known only after `Selection.Resolve` ran on the built-in tools, so the caller renders after it registers them.
`Warnings` gives one fixed-text line for each trust and settings problem, one summary line for the context files, and the skill problems: at most four rows of 256 bytes and then a `N more skill problems` row. `sessionTrust` sends the same capped skill rows and the context problems to the TUI in the diagnostic list of the session, with the key `context` or `skills`. `SkillDiagnostics` keeps the full list for `ask settings show`. A tool name that the flags or the settings give and that no built-in tool has fails the session start with `invalid_params` and the list of the known names.
`SessionStart.ExpandInput` is the `agent.Config.RewriteInput` of each session: it loads the skill that a prompt names with `/skill:<name>` (see `skills.Expand`), so print mode, the leader, the in-process TUI and `ask acp` expand in the same place, and the log keeps the expanded text. `SessionStart.SkillRows` gives the skills for `Config.StartSkills`. Both read the skills of the start, which hold project skills only when the project is trusted. A skill with `disable-model-invocation` is in both.

## Leader composition

[LeaderModule](module_leader.go) provides `LeaderRuntime` for `ask leader` from `LeaderParams`. It builds the editor composition (`NewACPRuntime`) once, sets `RequireRoute`, and puts one adapter and host behind one in-process byte link, with the `leader.Server` router on the other end. `Serve` starts the router and serves a listener that the command opened. The command (`cmd/tui/leader.go`) owns the lock, the socket, the log and the signals.
`Stop` runs in this order: the router stops taking clients and tells them, the link closes, the host disposes every session and waits for the started tool bodies (no time bound), while the credential refresh drains in parallel.
A client that leaves never reaches this path.
`ActiveRuns` and `QuiesceIfIdle` of the adapter feed the status and the idle shutdown. The tests with a real socket, the real router and the real host are in [leader_routing_test.go](leader_routing_test.go).

The leader owns interactive provider setup; provider auth calls on an editor connection and the in-process link check readiness only.
[NewLeaderRuntime](module_leader.go) owns the provider broker and its shutdown before the credential refresh drain.
See the [provider method boundary](../acp/README.md#provider-methods).
[Default model selection](default_model.go) and the [settings store](../settings/README.md#default-model) share the choice across new sessions.

## What does not belong here

| Code | Put it in |
|---|---|
| Business logic | the capability package |

## File names

`app.go`, `module_<area>.go`

## Imports

- Allowed: every package
- Denied: none

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
