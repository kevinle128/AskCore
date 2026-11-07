---
phase: 7
title: "Settings dump, CLI surface and docs"
status: completed
effort: 4h
depends_on: [4, 6]
---

# Phase 7: Settings dump, CLI surface and docs

## Context links

- [plan.md](./plan.md): S4, S13, S14, [Flags, env vars and files](./plan.md#flags-env-vars-and-files-h-conf-09-12-14), [Hand-off note for H6b](./plan.md#hand-off-note-for-h6b-ungated-context-files)
- Dispatch: `cmd/tui/headless.go:88-125` (subcommands before `parseArgs`)
- Docs rules: `AGENTS.md` "Documentation" (ASD-STE100, `docs/cli.md` hand-written, one `CHANGELOG.md` line per user-visible change, run `ask-docs-sync` after a feature), `.claude/skills/ask-docs-sync`
- Docs to change (each verified to hold the text that changes):
  - `internal/workspace/README.md:7-11,43` (planned files) and `:55` (rule that says project `AGENTS.md` loads only after trust: reversed by user decision 1)
  - `internal/settings/README.md:3,8,17,56-70,94` (owner text, `merge.go`, trust receipt, default model, thinking fold, file names)
  - `docs/ask-architecture-reference.md:58,77,101,380` (files under `~/.ask`, settings and workspace rows, config rule)
  - `docs/cli.md:21-56` (root options), `:122-131` (`ask acp` "takes no other argument" at line 128), `:185-203` (TUI commands and `thinkingFold`), `:207-221` (environment table: `ASK_HOME` row lists the home files)
  - `AGENTS.md` `ask acp` bullet ("It dispatches before prompt and mode parsing and takes no other argument")
  - `internal/acp/README.md` (new trust methods, `StartTrust`, `Factory` input), `internal/leader/README.md:47` area (trust methods in the policy description), `pkg/protocol/README.md` (new file `acp_trust.go`), `internal/tui/README.md` (`/trust`, dialog owner)
  - `CHANGELOG.md` `[Unreleased]`

## Overview

Priority P1. Add `ask settings show [--json]`, add a check that one function builds the Ask home path, and update every owning doc. The docs state the new trust model, including that `AGENTS.md` and `CLAUDE.md` load in untrusted projects.

## Key insights

- The dump runs the same `app.ResolveTrust` with no UI and never opens an untrusted project file (S14). It starts no leader and no database, like `ask models refresh`.
- Two documents say `ask acp` takes no argument (`docs/cli.md:128`, `AGENTS.md`). Both change.
- The workspace README rule at line 55 contradicts user decision 1. It must change in this phase, and the H6b hand-off note must be linked from it.

## Requirements

1. `ask settings show [--json]` in `cmd/tui/settings_command.go`: prints the trust result for the cwd (root, trusted, source, gated list, warning), then each shipped key with value and source layer, then the diagnostics. `--json` prints one object. An untrusted project prints "project settings not read (untrusted)". Exit codes: 0 done, 1 usage or read error, 130, 143, 129.
2. Dispatch line in `cmd/tui/headless.go` next to `models` (`headless.go:110-112`); usage text lists `ask settings --help`.
3. A test that no Go file outside `internal/settings/paths.go` joins `".ask"` with the user home: `TestAskHomeHasOneRootFunction` in `cmd/tui` that scans the non-test module sources (`_test.go` excluded) with `go/parser` for the string literal `".ask"` and allows only the listed owners. Today two non-test files hold the literal: `internal/settings/paths.go:80` and `internal/leader/paths.go:38` (the leader may not import `internal/settings`, `.golangci.yml:210-222`, so it keeps its own copy). H6a adds `internal/workspace/project.go` (the project `.ask` name). The test lists these three with the reason for each, and fails on a fourth.
4. Docs as listed in Context links, in ASD-STE100. The `docs/cli.md` root options list gains `--approve`, `-a`, `--no-approve`, `-na`; `ask acp` usage shows them; a new `ask settings` section; the TUI table gains `/trust`; the environment table `ASK_HOME` row names `trust.json`; a short "Project trust" section states the decision order, the gated paths, that context files always load, and that trust is not a security boundary against tools (H-SEC-03).
5. `CHANGELOG.md` `[Unreleased]`: one line each for: project settings layer with trust gate; `--approve`/`--no-approve`; TUI trust prompt and `/trust`; `ask settings show`; `/model` save reports a shadowed default.
6. Run the `ask-docs-sync` skill and apply its findings.
7. Reference docs: `docs/reference/` is generated for tools and providers only; run the drift test to prove no change is needed.

## Related code files

Create: `cmd/tui/settings_command.go`, `cmd/tui/settings_command_test.go`, `cmd/tui/ask_home_root_test.go`.
Modify: `cmd/tui/headless.go` (dispatch and usage only), the docs in Context links.

## Implementation steps

1. Tests Before.
2. `ask settings show`.
3. Home-root check.
4. Docs, changelog, `ask-docs-sync`.
5. Regression gate.

## Tests Before (written first, fail first)

| Test | Checks |
|---|---|
| `TestSettingsShowNamesSourceLayer` | user `defaultModel`, trusted project `defaultModel`: output names `project`; `--json` has `source: "project"` |
| `TestSettingsShowNeverReadsUntrustedProject` | untrusted project file with mode 000: exit 0, text "not read (untrusted)", no diagnostic for that file |
| `TestSettingsShowReportsCorruptTrustStore` | warning names `trust.json` path; exit 0 |
| `TestSettingsShowStartsNoLeader` | no `leader.sock` and no `leader.lock` in the temp home after the run |
| `TestSettingsUsage` | `ask settings --help` and an unknown subcommand (exit 1) |
| `TestAskHomeHasOneRootFunction` | only the listed files hold the `.ask` literal |

## Refactor

None.

## Tests After

All pass; `go test -run '^TestReferenceDocs$' ./cmd/tui` passes with no update.

## Regression gate

```text
go test ./cmd/tui
go test -run '^TestReferenceDocs$' ./cmd/tui
go test ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
rg -n 'takes no other argument|loads? only after' AGENTS.md docs internal --glob '*.md'
```

The last command may show only sentences that stay true after H6a: project settings (and skills, `SYSTEM.md`, extensions) load only after trust, for example `internal/settings/README.md:3`. It must not show `ask acp` "takes no other argument" or any sentence that says project `AGENTS.md` or `CLAUDE.md` waits for trust (today `internal/workspace/README.md:55`).

## Todo

- [ ] Tests Before
- [ ] `ask settings show`
- [ ] Home-root check
- [ ] Docs and changelog
- [ ] `ask-docs-sync` run
- [ ] Regression gate green

## Success criteria

- Every doc in Context links states the H6a behavior; no doc says project `AGENTS.md` waits for trust.
- `Docs impact: major` recorded in the phase result.

## Risk assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Docs drift from code | M x M | `ask-docs-sync`; the `rg` gate above |
| The source scan test is fragile | L x L | It scans string literals only and lists its owners |

## Security considerations

The dump prints values and paths only from files the user may read; it never prints `auth.json` content.

## Rollback

Revert the command and docs commits.

## Next steps

Phase 8 runs the real entry paths.
