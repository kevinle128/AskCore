---
phase: 3
title: "TUI job lines and user docs"
status: pending
effort: 2h
---

# Phase 3: TUI job lines and user docs

## Context

- `Translator.reminder` (`internal/tui/translate_jobs.go:23-55`) draws a line only for `ParseJobNotice` and `ParseMonitorEvents`. Any other reminder draws nothing. The limit notice (`internal/jobs/notice.go:72-75`) does not match `jobNoticeRE` (`pkg/protocol/job_notice.go:38`), so today the TUI hides it and keeps the job in its running list. This was harmless while only `ask -p` had a limit. After phase 2 it is a real defect in the TUI.
- Job line styles: `internal/tui/README.md:131`.
- User docs: `docs/cli.md:99-110` (background work; line 110 says the TUI, the leader, and `ask acp` set no time limit), env table `docs/cli.md:626-640`; `CHANGELOG.md` `[Unreleased]` `### Added` (line 9) and `### Changed` (line 72).

## Requirements

- Both texts reach the TUI through the extended `protocol.ParseJobNotice` (phase 1); there is no new parser and no new branch for the limit notice.
- Limit notice (`Status: "killed"`): the existing branch already drops the job and draws `✗ killed · background time limit` (`ToolFailed`). Only a test is new.
- Stall notice (`Status: "running"`): inside the existing branch, skip `t.dropJob` and draw `◆ job <id> <label>` with `still running · <Detail>` (`ToolNoted`). The job stays in the running list.

## Files

- Modify: `internal/tui/translate_jobs.go:24-36` (one `Status == "running"` check in the `ParseJobNotice` branch). It imports only `pkg/protocol`, as depguard `tui-client-boundary` allows (`.golangci.yml:28-37`).
- Modify: `internal/tui/translate_jobs_test.go` (stall line keeps the job running; limit line drops it).
- Modify: `internal/tui/README.md` line 131 (list the two new line forms).
- Modify: `docs/cli.md` lines 99-110 and the env table (add `ASK_BG_LIMIT_MS`, `ASK_BG_STALL_MS`; state the `0` meaning of each and the difference to `ASK_PRINT_BG_LIMIT_MS`; replace the "set no time limit" sentence; state that interactive monitors keep their own timeouts).
- Modify: `AGENTS.md:139`.
- Modify: `CHANGELOG.md` `[Unreleased]`: one `Added` line for the stall notice and `ASK_BG_STALL_MS`; one `Changed` line for the 2 h interactive limit and `ASK_BG_LIMIT_MS`.

## Steps

1. Add the `running` check in the `ParseJobNotice` branch, and tests for the stall line (job stays running) and the limit line (job is dropped).
2. Update the TUI README, `docs/cli.md`, and `CHANGELOG.md` in ASD-STE100.
3. Run the `ask-docs-sync` skill check for owning docs (`AGENTS.md` names it). `AGENTS.md:139` (background jobs) ("a finished job sends the model a `<system-reminder>` notice") gets one sentence on the stall notice and the interactive limit.
4. Run the full gate.

## Tests

```bash
go test -run 'Job|Reminder' ./internal/tui/
go test ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui && git diff --exit-code docs/reference
```

Manual check (optional, records evidence, not a gate): build `ask` with the Write-tool-safe method of the repo, run the TUI with `ASK_BG_STALL_MS=5000` and ask the model to run `sleep 60` with `run_in_background`; the transcript shows the stall line after about 5 s and the completion line after 60 s. Stop the leader after the check (`ask leader stop`).

## Risks

| Risk | L x I | Mitigation |
|------|-------|-----------|
| A leader of an older build sends no stall notice to a new TUI | L x L | Nothing to draw; no change. |
| A new leader sends the stall text to an old TUI | L x L | The old TUI hides unknown reminders (`translate_jobs.go:38-41`). |

## Rollback

Revert the commit. The notices then stay hidden in the TUI, as before.
