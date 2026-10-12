---
title: "Leader connect-or-spawn port: retry, eviction, newer-build replacement"
date: 2026-10-11
summary: "ConnectOrSpawn now retries early-exit starts, evicts socketless leaders on Linux, and replaces older client-started leaders by build stamp."
---

# Leader connect-or-spawn port: retry, eviction, newer-build replacement

## What happened
Implemented the six phases of plans/261011-2304-leader-connect-or-spawn-port/plan.md in internal/leader, cmd/tui, pkg/protocol and internal/app:
- Spawn retry: up to 3 starts per call; the binary is probed once.
- Owner record fifth line (serving/stopping) written by a Config.Stopping hook at the start of Server.Close.
- Linux eviction: SIGTERM then SIGKILL through one pidfd, only for a serving record older than ZombieAfter that passes process verification. macOS keeps the wait.
- Build stamps on the wire (commitTime, devStamp), additive, protocol stays 1. Dev stamp = executable mtime + SHA-256, computed first in serveLeader.
- Newer-build replacement: forced shutdown of a client-started leader, once per call, never a downgrade, dev builds compared only with dev builds.

## Findings
- Linux container tests must not bind-mount host /tmp: flock is not honoured there, which made two existing Stop tests fail and the eviction test hang.
- The helper leader process needed a global reference to its Lock, else GC closed the file and released the flock.
- go run keeps a stable cached binary (mtime unchanged on reuse); first run uses a temp dir.

## Evidence
macOS go test ./..., vet, golangci-lint clean. Linux: internal/leader whole suite in alpine; cmd/tui eviction, retry, dev-replace E2E in golang:1.27. See plans/reports/tester-261012-0030-leader-connect-or-spawn-linux.md.

## Next steps
Changes are uncommitted and mixed with earlier uncommitted work; decide how to split commits. Two unrelated cmd/tui tests flaked once each under full-package load.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
