---
phase: 6
title: "Docs sync"
status: pending
priority: P2
effort: "3h"
dependencies: [5]
---

# Phase 6: Docs sync

## Goal

Every doc that describes sessions, storage, resume, ACP load and the CLI tells the truth again, through the repository skill `.claude/skills/ask-docs-sync`.

## Context

- The skill owns the procedure: classify, find owners, edit, CHANGELOG, regenerate, check links (`.claude/skills/ask-docs-sync/SKILL.md`).
- Write in ASD-STE100 Simplified Technical English (`CONTRIBUTING.md`). No plan labels, phase numbers or decision codes in any doc outside `plans/`.
- Keep the Better Fullstack managed region of `AGENTS.md` byte for byte. Edit `AGENTS.md`, never `CLAUDE.md`.

## Requirements (stale facts to fix; re-check each line with `rg -n` first)

| File | Fact to change |
|---|---|
| `docs/ask-architecture-reference.md:189` | "SQLite persistence and branch selection remain roadmap work; the current writer is in memory" -> sessions persist as JSONL files behind `store.SessionStore`; a branch is a fork into a child session (later) |
| `docs/ask-architecture-reference.md:414` | "SQLite for both daemon and cloud mode" -> server data uses SQLite; session logs use JSONL files in every mode; a SQLite session backend can follow behind the same interface |
| `docs/ask-architecture-reference.md` sections 4.2 and 9 | name `internal/store/jsonlstore` as the file implementation of a storage interface; add a "new session store backend" row |
| `docs/adr/` | new ADR (next free number): session logs as JSONL files with a per-session flock, linear `seq`, snowflake ids; SQLite backend later behind one interface. Add it to `docs/README.md` |
| `internal/store/README.md:9` | the session store interface is `session_store.go`; implementations are `jsonlstore` (now) and `gormstore` (later) |
| `internal/sessions/README.md` | storage is no longer only in memory; codec, ids and the new entry types; allowed imports become `pkg/protocol` and `github.com/bwmarrin/snowflake` (not `store`) |
| `internal/store/jsonlstore/README.md` | written in phase 2; check it against the final code |
| `internal/agent/README.md` | `NewContext` returns an error; resume repair; model and thinking entries |
| `internal/acp/README.md` | `session/load`, the `loadSession` capability, replay mapping, `session_in_use`, `noSession` |
| `internal/leader/README.md` | `session/load` routing and pre-registration; the "durable session calls" line |
| `internal/app/README.md`, `internal/README.md` | `SessionLog` composition; new package `internal/store/jsonlstore` in the package map |
| `docs/cli.md` | `-c`/`--continue`, `--session`, `--no-session`, conflicts, errors, exit codes, session file location, JSON-mode header line; TUI flags section (`Flags in the TUI and with a leader`) |
| `AGENTS.md` | tech stack line for `github.com/bwmarrin/snowflake` (BSD-2-Clause); project structure (`internal/store/jsonlstore`); `ask -p` and `ask acp` bullets: session files, `-c`, `--session`, `--no-session` |
| `README.md` | quick start line about resume, if the common path mentions sessions |
| `CHANGELOG.md` `[Unreleased]` | Added: session files and resume for `ask -p`, TUI and `ask acp`; ACP `session/load` |
| `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` H8 (lines 364-400) | rewrite Owns, Packages and Tests as in [plan.md](plan.md#roadmap-h8-walk-roadmapmd364-400); mark H8 delivered when done |
| same roadmap, H14 (around line 563) | concept changes from an in-file entry tree to fork into a child session (header `parentSession`, `parentSeq`); E§24#11 test reads "fork correctness"; the H8 durable entries prerequisite stays |
| `cmd/tui/headless_json.go:10-11` | comment "There is no session header until sessions persist" (phase 4 removes it; check) |
| `docs/reference/*` | regenerate only; tools and providers do not change in this plan, so the drift test must show no diff |

Outside the repository: the user's auto-memory note `ask-rebuilds-pi-harness-in-go` has a D1 line "session = entry tree". Report it to the user; do not edit it from this phase.

## Files owned

The files in the table above (repository files only). No Go code.

## Steps

1. Run the skill on the change range of phases 1-5.
2. Fix each row; read each owning doc before the edit; verify each sentence against code (`file:line`).
3. Add the CHANGELOG lines.
4. Regenerate and check, as the skill says.
5. Check that every edited relative link resolves.

## Validation

```
ASK_UPDATE_DOCS=1 go test -count=1 -run '^TestReferenceDocs$' ./cmd/tui
go test -count=1 -run '^TestReferenceDocs$' ./cmd/tui
git diff --stat -- docs/reference
rg -n "SQLite persistence and branch selection|added with the session tree" docs internal
rg -n "session/load" internal/leader/policy.go
```

The first `rg` must find nothing. The second must show `session/load` with its own kind; the "no owner yet" comment stays only for `session/resume` and `session/list`.

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| A doc copies a fact that another doc owns | M x L | link to the owner, as the skill says |
| The roadmap walk and the code drift apart | L x M | write the roadmap text from the merged code, not from this plan |

## Rollback

Revert the docs commit. Docs carry no runtime effect.

## Code hygiene

Docs outside `plans/` carry no plan labels, phase numbers or decision codes. Commit message example: `docs: describe session files, resume flags and ACP session/load`.
