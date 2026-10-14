# Documentation index

This page lists every document in `docs/`. The audience is user (runs Ask), contributor (changes the code), or agent (an AI collaborator that works in the repository).

Start with the [project README](../README.md). Rules for changes and docs are in [CONTRIBUTING.md](../CONTRIBUTING.md). User-visible changes are in [CHANGELOG.md](../CHANGELOG.md).

## Documents

| Document | Purpose | Audience |
|---|---|---|
| [cli.md](cli.md) | Commands, flags, environment variables, exit codes, TUI commands of `ask` and `cmd/server`. | user |
| [reference/tools.md](reference/tools.md) | Built-in tools. Generated from code; do not edit by hand. | user, agent |
| [reference/providers.md](reference/providers.md) | Providers and models of the compiled catalog. Generated from code; do not edit by hand. | user, agent |
| [ask-architecture-reference.md](ask-architecture-reference.md) | Architecture: dewee package model, layers, process model, import rules, where to put new code. | contributor, agent |
| [CONTEXT.md](CONTEXT.md) | Glossary of provider-access terms (provider, model, access, auth method). | contributor, agent |
| [testing-llm-cassettes.md](testing-llm-cassettes.md) | The test layers, how cassettes record and replay, native auth and capture limits. | contributor |
| [tui-architecture.md](tui-architecture.md) | TUI decisions: Bubble Tea, one update loop, alternate screen, extension boundary. | contributor, agent |
| [tui-style-guide.md](tui-style-guide.md) | Visual rules of the TUI: roles, palette, glyphs, block patterns. | contributor, agent |
| [adr/0001-provider-auth-composition.md](adr/0001-provider-auth-composition.md) | One provider with several auth methods; one credential for each provider. | contributor |
| [adr/0002-minimal-login-in-provider-phase.md](adr/0002-minimal-login-in-provider-phase.md) | Minimal headless login ships with subscription access. | contributor |
| [adr/0003-remote-model-catalog-overlay.md](adr/0003-remote-model-catalog-overlay.md) | Accepted decision to refresh model data from a remote catalog. The catalog is compiled in today; the overlay is not built. | contributor |
| [adr/0004-leader-drivers-output-and-sdk-copy.md](adr/0004-leader-drivers-output-and-sdk-copy.md) | Leader drivers, output queues, version gate, patched ACP SDK copy. | contributor |

## Package documentation

Each package has its own `README.md` that says what belongs there, the file names, and the allowed imports. [internal/README.md](../internal/README.md) is the package map. Some packages are scaffolds, and a few packages with Go code have no README yet.

## Other places

- `e2e/tui/README.md`: the terminal E2E runner.
- `AGENTS.md`: instructions for AI collaborators. `CLAUDE.md` imports it.
- `plans/`: plans and reports. They are records of past work, not product authority.
