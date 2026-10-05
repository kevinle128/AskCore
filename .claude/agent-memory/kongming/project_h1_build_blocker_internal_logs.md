---
name: h1-build-blocker-internal-logs
description: Pre-existing `go build ./...` failure in fresh worktrees because internal/logs is untracked (.gitignore `logs/` pattern); known, out of H1 scope, owner decision pending
metadata:
  type: project
---

`go build ./...` fails in fresh worktrees (seen 2026-10-01 on master-2): `internal/app/app.go` imports `AskCore/internal/logs`, which was never tracked because `.gitignore:44` has the pattern `logs/` that also matches `internal/logs`.

**Why:** The H1 implementation run (plans/261001-0836-h1-messages-events-faux) found it; it was declared out of H1 scope and reported to the user rather than fixed. Fix options were: anchor the pattern as `/logs/`, or add the package.

**How to apply:** When advising on build or lint failures in this repo, check whether this blocker is still present (`git ls-files internal/logs`) before blaming new work. Package-scoped builds and tests (`./pkg/...`, `./internal/providers/...`) are the valid evidence until the owner decides.
