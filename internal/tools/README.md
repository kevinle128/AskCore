# `internal/tools`

This package owns what the model can call: declarations, registration, argument preparation, and tool bodies.
Tool execution order belongs in Agent, not in the registry.
Tools have no built-in approval policy; an extension can block a call through the control adapter.

## Source owners

| Responsibility | Owner |
|---|---|
| Tool, ConcurrencySafe, ArgumentPreparer, and call context | [types.go](types.go) |
| Provenance | [source.go](source.go) |
| Registration, snapshots, and declaration projection | [registry.go](registry.go) |
| Argument coercion and validation | [coerce.go](coerce.go), [validate.go](validate.go) |
| Prompt section and `PromptContributor` | [prompt.go](prompt.go) |
| Output limits (2000 lines, 50 KB) | [truncate.go](truncate.go) |
| File tools `read`, `write`, `edit`, `ls`, per-session `FileState`, process-wide `PathLock`, atomic write | `file_*.go` |
| Search tools `grep`, `find` (`rg` and `fd`, in-house walk as last fallback) | `search_*.go` |
| Shell tools `bash`, `job_output`, `job_kill`, `job_list`, `monitor` over a `jobs.Table` | `shell_*.go` |
| Demo `echo` (kept for the faux demo and tests) | [echo.go](echo.go) |

## Decisions and constraints

Concurrency approval belongs to the snapshot tool and the validated arguments of that call.
No declaration, a false answer, or a classifier panic requires exclusive execution.
An exclusive call is a barrier rather than a reason to make the entire batch serial.
The [Agent coordinator](../agent/tool_coordinator.go) owns the rolling pool and ordering.

Arguments are prepared once and frozen before control handlers and the body.
A pre-tool control may allow, deny, or cancel; it cannot mutate the executable arguments.
The model-facing declaration is an allowlist and carries no host callback or concurrency classifier.
See [registry tests](registry_test.go) and [validation tests](validate_test.go).

Cancellation must finish started work without leaving external side effects running.
Tool bodies must return promptly when their context is cancelled; process tools must stop the entire process group.
The Agent waits without a time bound and stays busy until bodies return.
The interface godoc in types.go owns this tool author contract.

Work that must outlive the tool call starts as a task through the call context (field `Tasks` of `tools.Context`, nil when the caller has no registry), not as a goroutine of the tool.
The tool returns the task ID at once; the Agent gets the result as a notice, and no tool waits or polls for it.
See the [async task runtime](../../docs/async-task-runtime.md) sections 8.7 and 11.

Tools are taken from one immutable snapshot for each turn.
A registry change can affect a later request only after the model is told about the changed declarations.
Input schemas compile at registration so an invalid schema cannot enter a request.
No permission popup or separate approval layer is part of the current registry.

## Built-in tools

| Tool | Notes |
|---|---|
| `read` | Numbered lines, `offset` and `limit`, 2000 lines or 50 KB per call. An image is returned as an image block and is resized when too large. A model without image input gets a text note. |
| `write` | Atomic write, parents created, execute bit set for a file that starts with a shebang. Refuses a generated file. |
| `edit` | Pi `edits[{oldText,newText}]`. A match ladder runs from exact to fuzzy; a rung with more than one place is an error. Refuses a file that changed since the session last read it. |
| `ls`, `grep`, `find` | Pi limits and messages. `grep` needs `rg`. `find` uses `fd`, then `rg --files`, then an in-house walk with a `.gitignore` subset. |
| `bash` | Every call is a job. `timeout` is the wait budget (default 120 s, maximum 600 s); a command that runs longer continues in the background. `run_in_background` skips the wait. |
| `job_output`, `job_kill`, `job_list` | Read, stop and list the jobs of the session. `job_output` answers at once unless `wait` is true. |
| `monitor` | Runs a command; each stdout line is an event for the model. |

A non-persistent monitor has a default and maximum timeout of 10 hours.
Positive fractional `timeout_ms` values are rounded up to the next millisecond.
`persistent: true` runs until `job_kill` or session close.

`ConcurrencySafe` is true for `read`, `ls`, `grep`, `find`, `job_list` and `job_output` without `wait`.
`write`, `edit`, `bash`, `monitor` and `job_kill` run alone.
`write` and `edit` also take a process-wide `PathLock` for the resolved path, because one leader holds many sessions.

A session owns one `FileState` and one `jobs.Table`; both are created by the composition root, not by this package.
`NewFileTools`, `Grep`, `Find` and `NewShellTools` are the constructors.
The tools take the working directory from the call context and the child environment from the job table.

A tool that implements `PromptContributor` adds one line and some guidelines to the system prompt.
`PromptSection` renders them sorted by tool name; a tool with an empty snippet is not listed.
The composition root sets the result as the system prompt.

## Package boundaries

MCP bridges belong in mcp and provider wire code belongs in providers.
A tool can call providers, but providers must not depend on tools.
Process execution belongs in sandbox, the job table belongs in jobs, and gateway RBAC belongs in permissions.
A blocking rule for commands belongs in a pipeline handler (`command-guard` in `internal/pipeline`), not in a tool.

## File names and imports

Use `<family>_<topic>.go` and `<tool>_<vendor>.go` when a real tool or backend needs that boundary.
Pass dependencies through constructors.
Allowed imports include providers, store, sandbox, jobs, bus, skills, workspace, tracing, and tasks.
Do not import agent, transport packages, acp, leader, or config.
See [architecture](../../docs/ask-architecture-reference.md).
