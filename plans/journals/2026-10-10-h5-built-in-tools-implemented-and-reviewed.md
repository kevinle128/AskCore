---
title: H5 built-in tools implemented and reviewed
date: 2026-10-10
summary: All 10 H5 phases landed with tests; an independent Codex review fixed 7 defects; three release items stay open.
---

# H5 built-in tools implemented and reviewed

## What happened
The ten phases of the H5 built-in tools plan were implemented test-first: file tools, process runner, search tools, job table with monitor and notices, shell and job tools, composition with session close and command-guard, ACP tool mapping, end-to-end tests with OpenAI cassettes, and docs plus a real-provider run.

An independent review (Codex gpt-6.1-sol, medium) found and fixed seven defects: large read paging hints, overlapping edit matches, named pipes in read and edit, missing path variants in grep, find and write, command-guard wrapper options, a lost final monitor line, and monitor completion ordering. Each has a regression test.

## Verification
go vet, go test ./internal/... ./pkg/..., go test ./cmd/tui (230 s) and golangci-lint all pass; secret scan and stray-process checks are clean. OpenAI (API key) and Anthropic (OAuth) real runs read, edited and tested a repo.

## Decision
No commits were made; work is uncommitted in the worktree. plan.md stays in-progress until the open items close.

## Next steps
- N1: TUI notice hiding (separate T1b design slice) gates the H5 release.
- Add a session-close operation to the leader wire so TestLeaderSessionCloseKillsBackgroundJobs can run.
- Sign in to ChatGPT again and re-run the real-provider acceptance for it.
- Watch TestFindTimeoutReturnsPartialResult and TestACPE2ERequestCancel for flakes.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
