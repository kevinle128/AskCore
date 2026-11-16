---
phase: 3
title: "Leader, ACP and app composition"
status: pending
priority: P2
effort: 4h
dependencies: [4]
---

# Phase 3: Leader, ACP and app composition

## Goal

Fix R4, R5, N12, N13, N14, N15, N16, N17, the export half of N33, and the session text of R2.

Only step 9 (R2 text) waits, and only for the first commit of phase 4 (the `SkipOutsideProject` enum), not for the whole phase. Steps 1 to 8 can start at once. The frontmatter dependency names phase 4 for that one step.

## File ownership

- Modify: `internal/leader/rpc.go`, `internal/leader/spawn.go`
- Modify: `internal/acp/stdio.go`, `internal/acp/host.go`, `internal/acp/agent.go`
- Modify: `internal/app/module_leader.go`, `internal/app/session_start.go`
- Tests: `internal/leader/router_safety_test.go`, `internal/acp/stdio_test.go`, `internal/acp/conformance_test.go`, `internal/acp/agent_test.go`, `internal/app/session_start_test.go`

## Steps

### 1. R4: refuse case-fold duplicate top-level keys

Evidence: `parseMessage` decodes the envelope with no `hasFoldDuplicate` check (`internal/leader/rpc.go:25-45`); only `params()` has it (`rpc.go:57-59`). A key such as `paramſ` (U+017F folds to `s`) sorts after `params`, the SDK decodes `Params` from it, and the route `_meta` that the router puts into `params` is bypassed (triage g1 F7).

Test first in `router_safety_test.go`: `TestParseMessageRefusesFoldDuplicateKeys` with rows `{"params":{},"paramſ":{}}`, `{"method":"a","METHOD":"b"}`, and `{"id":1,"ID":2}`; expect `ErrInvalidMessage`. A row with distinct keys still parses.

Fix: after `decodeObject`, `if hasFoldDuplicate(fields) { return nil, fmt.Errorf("%w: two keys differ only in case", ErrInvalidMessage) }`.

Command: `go test -run '^TestParseMessage' ./internal/leader`

### 2. R5: correct the comment of `checkExecutable`

Evidence: the comment says "not writable by others" (`internal/leader/spawn.go:167-169`); the mask is `0002` (`:186`) and the error says "writable by everyone" (`:187`). The user kept the mask, because a `0022` mask refuses `go build` output under umask 002.

Fix: change the comment to "not writable by everyone (other); group write is allowed, so a build under umask 002 still runs". No code or test change.

### 3. N12: count duplicate pending response ids

Evidence: `ids map[string]struct{}` (`internal/acp/stdio.go:276`); `request` overwrites (`:303`), `written` deletes on the first response (`:324-327`), so the shutdown barrier opens while a second request with the same id still waits. The SDK accepts duplicate ids (`internal/acp/conformance_test.go:633-646`).

Test first in `stdio_test.go`: `request` the same id twice, close, `written` once -> the idle channel stays open; `written` a second time -> it closes.

Fix: `ids map[string]int`; `request` increments; `written` decrements and deletes at zero, then calls `signalLocked`. `len(p.ids)` checks (`:309`, `:332`) keep their meaning.

Command: `go test -run 'Pending|duplicate' ./internal/acp`

### 4. N13: mutations refuse a failed host

Evidence: `guard` (`internal/acp/host.go:470-494`) and `mutationCommit` (`:496-514`) check `closed`, `quiesced` and the generation only. `usable()` also checks `h.failure` (`:173-180`). A run cannot start on a failed host, but model, thinking and queue state of that host can still change.

Test first in `agent_test.go`: latch an output failure (as the tests at `:369` and `:492` do), then call one mutation that goes through `mutationCommit` (for example the model change of a session); expect the latched error (`ErrOutputFailed` wrapped), and no state change.

Fix: in both functions, read `fail := s.host.Failure()` before any lock (`guard` takes `s.host.admit.RLock()` first, `host.go:471`; `mutationCommit` takes `s.commit` first, `:497`), so `h.mu` is never taken under another lock. Return it when it is not nil.

Command: `go test -race -run 'Failure|Mutation' ./internal/acp`

### 5. N33 (export half): one ACP wire version

Evidence: `const wireVersion = 1` (`internal/acp/agent.go:19-20`), used at `:253`; `cmd/tui/version.go:30-31` repeats it.

Fix: rename to exported `WireVersion` with its doc comment, and use it at `agent.go:253`. Phase 6 switches `cmd/tui/version.go` to it and adds the test that ties the two.

### 6. N14 and N15: clean the ACP runtime on the leader error paths

Evidence: `NewACPRuntime` starts the model refresh (`internal/app/module_acp.go:285`). The error path of `acp.NewAdapter` (`internal/app/module_leader.go:95-98`) and of `leader.NewServer` (`:121-125`) close only `setup` (and the adapter), not `acpRT`.

Fix: on both paths, call `_ = acpRT.Cleanup(context.Background())` after `setup.Close()`, the same order as the success cleanup (`:127-130`).

Test: no input of `LeaderParams` reaches these errors today. `leader.NewServer` fails only without an agent stream (`internal/leader/server.go:70-72`), which the module always gives, and `acp.NewAdapter` fails only without a factory or catalog callbacks (`internal/acp/agent.go:141-143`), which `NewACPRuntime` always sets. Do not add a seam only for this; the change is two lines, checked by review and by `go vet`. State this in the commit message.

### 7. N16: a skills warning line holds at most 256 bytes

Evidence: `capRows` gives the path `256 - len(Text) - len(": ")` bytes (`internal/app/session_start.go:343-347`), but `Warnings` adds `"Warning: skills: "` (17 bytes) in front (`:274-279`). `docs/cli.md:238` says each line holds at most 256 bytes. ACP diagnostic rows have no prefix and are correct.

Test first in `session_start_test.go`: one skill diagnostic with a 400-byte path; expect each `Warnings()` line to be at most 256 bytes and the cut to fall on a rune boundary (use a path with multibyte runes).

Fix: give `cappedSkillDiagnostics` and `capRows` a `reserve int` for the prefix. `Warnings` passes `len("Warning: skills: ")`; the ACP caller passes 0.

### 8. N17: label a context file as `user` only when it is in the Ask home folder itself

Evidence: `strings.HasPrefix(path, askHome+sep)` (`session_start.go:362-368`) labels `$ASK_HOME/work/AGENTS.md` of a project as `user`. User context files are read only from `askHome` itself (`UserDir: askHome`, `:116`).

Test first: a project with cwd `$ASK_HOME/work` and an `AGENTS.md` there; expect layer `project`. A file at `$ASK_HOME/AGENTS.md` stays `user`.

Fix: `filepath.Dir(path) == s.askHome`.

Command for steps 7 and 8: `go test -run 'Warnings|ContextLayer|SkillDiagnostic' ./internal/app`

### 9. R2 (session text, after phase 4)

Fix: add `case bootstrap.SkipOutsideProject: return "left out: the file links to a path outside the project"` to `contextSkipText` (`session_start.go:372-385`). The warning count (`:271-272`) then includes such a file.

Test: in `session_start_test.go`, a `Context.Skipped` row with the new reason gives one ACP diagnostic with that text and the count line.

Command: `go test -run 'ContextDiagnostics|Warnings' ./internal/app`

## Verification

- `go test -race ./internal/leader ./internal/acp ./internal/app`
- `go vet ./internal/leader ./internal/acp ./internal/app`
- golangci-lint (`internal/leader` must not import `internal/acp`, `.golangci.yml:231-243`).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A client sends keys that differ only in case and now gets an error | Low | Low | JSON-RPC keys are lower case; the SDK never sends such keys. |
| N13 lock order | Low | High | Read `Failure()` before `s.mu`; `-race` run. |

## Rollback

Revert the phase commits. Revert phase 6 first if `acp.WireVersion` is used there.

## Docs impact

Phase 7: `docs/cli.md:237` (the warning count includes files outside the project). `docs/cli.md:238` is now true and needs no change.
