---
phase: 4
title: "Build stamps on the wire (commit time, dev stamp)"
status: completed
priority: P2
effort: "1d"
dependencies: [3]
---

# Phase 4: Build stamps on the wire (commit time, dev stamp)

## Context

- Plan: decisions P8 (commit time, additive field), P13 (dev stamp, N3 B, user 2026-10-11), P12 (Hello helper); risk rows for the dev stamp (modification time oddities, hash cost, removed `go run` binary).
- Code: `pkg/protocol/leader.go:58-62` (`LeaderRegister`), `:65-72` (`LeaderRegistered`), `:93-102` (`LeaderStatus`), `:106-111` (`VersionInfo`); `internal/leader/handshake.go:24-32` (`ServerConfig`), `:106` and `:213` (plain `json.Unmarshal`), `:127-128` (registered reply); `internal/leader/router.go:842-852` (`status`); `cmd/tui/version.go:28-56` (`buildIdentity`), `:77` (`VersionInfo`); register sites `cmd/tui/connect.go:556`, `cmd/tui/tui_client.go:61`, `cmd/tui/leader.go:123`; `serveLeader` first steps `cmd/tui/leader.go:204-205`, server config `:263-264`.
- Rule: `pkg/protocol/README.md:29`: the control schema stays the same in every protocol version. This phase does not touch the control schema.

## Overview

Every binary reports one build stamp. A VCS-stamped binary reports the commit time of its revision; a binary without a VCS stamp (`go run`, build `dev`) reports a dev stamp: the modification time of its executable and the SHA-256 of its bytes. The leader sends the stamp in `registered` and in `status`; `ask version --json` prints it; a client sends it in `register`. The protocol integer stays 1. No decision uses the stamps yet (phase 5).

## Requirements

- New optional fields in `LeaderRegister`, `LeaderRegistered`, `LeaderStatus`, `VersionInfo`:
  - `CommitTime string \`json:"commitTime,omitempty"\``: commit time of the VCS revision, RFC 3339; empty without a VCS stamp.
  - `DevStamp string \`json:"devStamp,omitempty"\``: for a binary without a VCS stamp only; text `<modification time, RFC 3339 with nanoseconds, UTC> sha256:<64 lowercase hex>`.
- `leader.ServerConfig.CommitTime`, `leader.ServerConfig.DevStamp`; `Accept` puts them in `LeaderRegistered`; `router.status` puts them in `LeaderStatus`.
- `cmd/tui/version.go`:
  - One read of `debug.ReadBuildInfo` gives `buildIdentity`, `vcs.time` and `vcs.revision` (one small helper, shared).
  - `buildStamps() (commitTime, devStamp string)` through `sync.OnceValues`: when `vcs.time` exists, commit time and no dev stamp; else the dev stamp from `os.Executable()`: `os.Stat(exe).ModTime().UTC()` and SHA-256 of the file read with `io.Copy` into the hash (streamed, no full read into memory). Any error gives an empty dev stamp (unknown).
  - `runVersion` puts both into `VersionInfo`.
- `cmd/tui/leader.go` `serveLeader`: call `buildStamps()` as the first step, before `EnsureHome` and the lock, and pass the values to `ServerConfig`. The leader never reads its executable again: a `go run` binary can be removed while its leader still runs.
- `leaderHello(kind string) protocol.LeaderRegister` sets `ClientKind`, `Build`, `CommitTime`, `DevStamp`; the three register sites use it.
- Comments describe behavior ("a binary without a VCS stamp is known by the time its file was written and the hash of its bytes"); no labels.

## Check first (record in the phase report)

Before the code, check and record in `plans/reports/tester-261011-<time>-dev-stamp-facts.md`:

1. Where `go run ./cmd/tui` of the installed Go places the binary it runs (`os.Executable()` path from a small `ask version --json` run under `go run`), whether a second `go run` of unchanged source reuses the same file, and whether its modification time changes on reuse.
2. The time of the dev stamp on the built `cmd/tui` binary (size and milliseconds for `ask version --json`).

These facts confirm or adjust the risk text in plan.md; they do not change the decision (N3 B). A fact that makes the rule unsafe goes to the user before phase 5 starts.

## Runtime Flow

- Actor and trigger: any client registers; `ask version --json` runs before a start; `ask leader status --json` runs.
- Observable result: `commitTime` for a VCS-stamped binary, `devStamp` for a dev binary, in the JSON of `ask version --json` and `ask leader status --json`.
- Proof: `TestVersionJSONCarriesCommitTime`, `TestVersionJSONCarriesDevStamp`, `TestLeaderDevStampSurvivesARemovedBinary`, `TestRegisteredCarriesBuildStamps`.

## File inventory

| File | Action | Test impact |
|---|---|---|
| `pkg/protocol/leader.go` | modify (two fields in four types) | `leader_test.go` |
| `internal/leader/handshake.go` | modify (`ServerConfig.CommitTime`, `DevStamp`, registered reply) | `handshake_test.go` |
| `internal/leader/router.go` | modify (status) | existing status tests |
| `cmd/tui/version.go` | modify (shared build info read, `buildStamps`, `VersionInfo`) | `version_test.go` |
| `cmd/tui/leader.go` | modify (`leaderHello`, stamps first in `serveLeader`, server config) | existing tests |
| `cmd/tui/connect.go`, `cmd/tui/tui_client.go` | modify (use `leaderHello`) | existing tests |
| `cmd/tui/dev_binary_test.go` | create (helper `devBinaries(t)`: writes DEV-OLD with `go build -buildvcs=false` and DEV-NEW with `go build -buildvcs=false -trimpath` into a temp dir once per test run, and `devCopy(t, src, name, mtime)`) | used by phase 4 and 5 tests |

## Tests Before

Phase 3 state passes.

## Tests After (write first, see them fail)

- `TestLeaderFramesCarryBuildStamps` (`pkg/protocol/leader_test.go`): round trip of the four types with and without `commitTime` and `devStamp`; empty values are omitted.
- `TestOlderPeerIgnoresBuildStamps` (`pkg/protocol/leader_test.go`): JSON with both fields and an unknown field decodes without error; JSON without them decodes to `""`.
- `TestRegisteredCarriesBuildStamps` (`handshake_test.go`): server with both values; `Register` gets them in `Info`.
- `TestStatusCarriesBuildStamps` (`internal/leader`): status reply has them.
- `TestVersionJSONCarriesCommitTime` (`cmd/tui/version_test.go`): the built `acpBinary` with `version --json`: `commitTime` parses as RFC 3339 and equals `vcs.time` from `go version -m <bin>`; `devStamp` is absent. Without `vcs.time` the test fails with a message that it needs a VCS-stamped binary.
- `TestVersionJSONCarriesDevStamp`: DEV-OLD with `version --json`: no `commitTime`; `devStamp` equals the modification time of the file (`os.Stat`) and the SHA-256 that the test computes from the file.
- `TestDevStampFollowsTheFile`: DEV-COPY (same bytes, later modification time) reports the same hash and the later time; DEV-NEW reports another hash.
- `TestLeaderDevStampSurvivesARemovedBinary`: copy DEV-OLD to a temp name; start a leader from the copy (`ConnectOrSpawn` with `CallerExe` = copy, `CallerIsAsk` true); remove the copy; `ask leader status --json` (run with DEV-OLD) shows the `devStamp` that the copy reported before removal.

## Steps

1. Do the checks and write the facts report.
2. Write the tests; see them fail.
3. Add the fields, then the leader side, then `cmd/tui`.
4. Narrow tests; gates.

## Validation (gates)

`go test ./pkg/protocol ./internal/leader ./cmd/tui`, `go vet ./...`, `GOOS=linux go vet ./internal/leader`, golangci-lint.

## Risk

| Risk | Mitigation |
|---|---|
| A strict decoder rejects the new fields | No `DisallowUnknownFields` in `internal/leader`; the protocol test proves an older-shape decode. |
| `vcs.time` missing in a build | Then it is a dev build and gets a dev stamp. |
| Hash cost at TUI start | Once per process, streamed, dev builds only; measured in the facts report. |
| The executable cannot be read (removed, no permission) | Empty dev stamp: unknown, phase 5 keeps the leader. The leader reads it before anything else. |

## Rollback

Revert the commit. Phase 5 reads these fields: revert phase 5 first.
