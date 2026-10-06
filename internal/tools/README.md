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
| Current builtin Echo | [echo.go](echo.go) |

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

Tools are taken from one immutable snapshot for each turn.
A registry change can affect a later request only after the model is told about the changed declarations.
Input schemas compile at registration so an invalid schema cannot enter a request.
No permission popup or separate approval layer is part of the current registry.

## Package boundaries

MCP bridges belong in mcp and provider wire code belongs in providers.
A tool can call providers, but providers must not depend on tools.
Sandbox policy belongs in sandbox and gateway RBAC belongs in permissions.

## File names and imports

Use `<family>_<topic>.go` and `<tool>_<vendor>.go` when a real tool or backend needs that boundary.
Pass dependencies through constructors.
Allowed imports include providers, store, sandbox, bus, skills, workspace, and tracing.
Do not import agent, transport packages, acp, leader, or config.
See [architecture](../../docs/ask-architecture-reference.md).
