---
title: H13b Phase 1 transport and security
date: 2026-10-08
summary: "Leader frames, ids, paths, lock, peer check, handshake and depguard rule done; route meta spike says go"
---

# H13b Phase 1 transport and security

## What happened
Phase 1 of the H13b plan is implemented in `internal/leader` and `pkg/protocol`: socket frames, line codec for the agent link, id table, private paths, lifetime lock with a stable inode, peer UID check, and the handshake with a management-only path after a version mismatch. A depguard rule keeps the leader away from the agent and ACP packages.

## Findings
- `LineReader` first returned its reuse buffer. A test showed a held line turning into the next one. It now returns a copy.
- macOS keeps `/tmp` as a symlink. A relaxed parent check in `EnsureHome` looked helpful, but `internal/settings` refuses a symlinked parent, so native auth would fail on the same home. The leader now uses the same strict rule.
- The SDK delivers per-request route meta to the Agent methods, so the shared-adapter design stands (spike result: go).
- The installed SDK stops a line at 10 MiB. A pinned patch is needed before Phase 3.

## Evidence
Race tests, Linux container run with the real peer-credential call, lint with a depguard negative control, and the existing acp, app and cmd/tui suites all pass.

## Next steps
Decide how to pin the SDK parser fix, then scout and start Phase 2 (router).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
