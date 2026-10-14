---
name: ask-docs-sync
description: Update the Ask documentation and CHANGELOG.md after a code change. Use after a feature, fix, or refactor is implemented and its tests pass, before the commit or pull request, or when the user asks to sync, update, or check the docs or the changelog. The agent reads the diff, finds the docs that own the changed behavior, edits them, adds user-facing CHANGELOG lines under [Unreleased], regenerates the reference docs, and runs the drift test. Do not use to design a feature or to write plans.
---

# Ask docs sync

Docs follow code.
After a change, every doc that describes the changed behavior must tell the truth again.
This skill does that update in one pass and reports what it changed.

## Inputs

- The change range. Default: `git diff $(git merge-base HEAD master)...HEAD` plus uncommitted changes (`git diff HEAD`).
  If the user names a commit range, use that range.
- `git log --no-merges --format='%h %s' <range>` for the intent of each commit.

## Step 1: Classify the change

For each changed file, decide if the change is user-visible.
User-visible means one of these changed: a command, flag, environment variable, exit code, key, slash command, screen text, tool, provider, model, config or settings file, file location, protocol message, or documented error.
Internal refactors and test-only changes are not user-visible.
They can still need a package README update when they change ownership, file names, or import rules.

## Step 2: Find the owning docs

Use this map. Update the smallest doc that owns the fact. Do not copy the same fact into many docs; link to the owner.

| Changed path | Owning docs |
|---|---|
| `internal/<pkg>/**`, `pkg/<pkg>/**` | `internal/<pkg>/README.md` or `pkg/<pkg>/README.md`; `internal/README.md` if a package is added, removed, or changes role |
| `internal/tools/**`, tool list in `cmd/tui/headless.go` | `internal/tools/README.md`; `docs/reference/tools.md` (generated) |
| `internal/providers/**` catalog or adapters | `internal/providers/README.md`; `docs/reference/providers.md` (generated) |
| `cmd/tui/**` subcommands, flags, env vars, exit codes | `docs/cli.md`; the command list in `AGENTS.md`; `README.md` quick start if the common path changed |
| `internal/tui/**` keys, screens, slash commands | `internal/tui/README.md`; `docs/cli.md` slash commands; `AGENTS.md` TUI lines; `docs/tui-architecture.md` if the structure changed |
| `internal/leader/**`, `internal/acp/**` | the package README; `docs/cli.md` |
| `cmd/server/**`, `migrations/**`, `proto/**` | `README.md` server section; `internal/gateway/README.md` or `internal/http/README.md` |
| `.github/workflows/**`, lint or test commands | `CONTRIBUTING.md` (CI and test sections); `AGENTS.md` Documentation and Common Commands |
| new dependency in `go.mod` | `AGENTS.md` tech stack |
| architecture or import rule change | `docs/ask-architecture-reference.md`; a new ADR in `docs/adr/` when it is a durable decision |
| a new doc file | `docs/README.md` index |

Read each owning doc before you edit it.
Verify every sentence you write against the code (file and line), not against the commit message.

## Step 3: Edit the docs

- Use ASD-STE100 Simplified Technical English: short sentences, active voice, one instruction per sentence.
- Describe behavior. Do not write plan IDs, phase numbers, or finding codes.
- Keep the Better Fullstack managed region in `AGENTS.md` byte for byte.
- `CLAUDE.md` contains only `@AGENTS.md`. Edit `AGENTS.md`, never `CLAUDE.md`.
- Never edit `docs/reference/*.md` by hand.
- Do not move or rename existing docs. Other docs, plans, and memory files link to them.

## Step 4: Update CHANGELOG.md

Add one line for each user-visible change under `## [Unreleased]`, in the matching section.
Use the Keep a Changelog sections: Added, Changed, Deprecated, Removed, Fixed, Security.
Create a section only when it has an entry.

- Write for a user of `ask`, not for a reviewer: "`ask -p` exits with code 143 on SIGTERM.", not "fix signal handling in headless.go".
- Start with the area in bold, as the existing lines do, for example `**TUI:**`.
- One sentence per entry. No commit hashes, no plan codes, no file paths unless the user types the path.
- Merge with an existing `[Unreleased]` line when the same feature changed again before a release.
- Do not create a version heading. Releases and tags are the user's decision.
- Skip internal changes. If the whole change is internal, say so in the report and add no line.

## Step 5: Regenerate and check

Run these commands and fix every failure before you report:

```bash
ASK_UPDATE_DOCS=1 go test -count=1 -run '^TestReferenceDocs$' ./cmd/tui
go test -count=1 -run '^TestReferenceDocs$' ./cmd/tui
git diff --stat -- docs/reference
```

Then check that every relative link in the docs you edited resolves to a file (and to a heading when the link has `#anchor`).
If you removed or renamed a heading, grep the repo for links to its anchor and fix them.

## Step 6: Report

Report in this order:

1. The docs you changed, one line each, with the reason.
2. The CHANGELOG lines you added.
3. The regenerated reference files, or "no change".
4. Doc facts you could not verify, and code-versus-doc conflicts you found but did not fix (for example, a usage text that disagrees with the code). Do not change code to match a doc in this skill.

Do not commit unless the user asks.
