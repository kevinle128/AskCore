# Triage summary: ocr review master..develop (2026-11-10)

Source: [ocr report](./ocr-review-261110-master-develop.md) (176 findings; 46 of 372 items failed, so some files were not reviewed).
Scope of this triage: every High finding and every security finding, 59 in total. Code checked at `develop` `1940e70`. Group tables with file:line evidence:
[g1 leader/acp](./triage-261110-1804-g1-leader-acp.md), [g2 agent/pipeline/tasks](./triage-261110-1804-g2-agent-pipeline-tasks.md), [g3 providers/auth/settings/app](./triage-261110-1804-g3-providers-auth-settings-app.md), [g4 tools/bootstrap/skills](./triage-261110-1804-g4-tools-files-skills.md), [g5 tui/cmd/e2e/testsupport](./triage-261110-1804-g5-tui-cmd-e2e-test.md).

## Counts

| Group | Findings | REAL | FALSE | DESIGN | FIXED |
|---|---|---|---|---|---|
| g1 | 12 | 3 (one duplicate) | 5 | 4 | 0 |
| g2 | 13 | 4 | 4 | 5 | 0 |
| g3 | 11 | 1 | 8 | 2 | 0 |
| g4 | 12 | 3 | 5 | 4 | 0 |
| g5 | 11 | 1 | 8 | 2 | 0 |
| Total | 59 | 12 (11 distinct) | 30 | 17 | 0 |

No finding is Critical or High after triage. About half of the tool's findings are false.

## Real defects (11 distinct)

| # | Sev | Location | Defect | Fix |
|---|---|---|---|---|
| R1 | Medium | internal/agent/agent.go:231-248 | `Continue` takes the run slot, then returns early; input queued in that window waits in an idle Agent | Check the messages before taking the slot (or set `wake` on early return); race test first |
| R2 | Medium | internal/bootstrap/read.go:20-29 | A project `AGENTS.md`/`CLAUDE.md` symlink to a file outside the repo is sent to the provider with no trust decision | Skip a project context-file target outside the git root (or cwd) |
| R3 | Medium | internal/tools/file_read.go:123 | `read` loads the whole file into the leader (about 3x size), each page again | Cap the size or read lines with a buffered reader |
| R4 | Low | internal/leader/rpc.go:24-30 | Top-level keys are not checked for case-fold duplicates; a Unicode key can skip the route `_meta` (same user only) | `hasFoldDuplicate(fields)` in `parseMessage` + test |
| R5 | Low | internal/leader/spawn.go:167-187 | Comment says "not writable by others", mask is `0002` | Fix the comment, or a group rule that keeps umask 002 builds |
| R6 | Low | internal/providers/openai/isolate.go:60-64 | A models.json `Authorization` header replaces the resolved key on the Completions path (Responses filters it) | Filter credential headers as `profileClient` does |
| R7 | Low | internal/tools/file_edit.go:192-233 | Another process can write between the read and the rename of `edit` | Stat after read and before write; fail on change |
| R8 | Low | internal/agent/tasks.go:159-176 | A typed-nil message other than `*CustomMessage` passes the check (latent) | Generic typed-nil check + test |
| R9 | Low | internal/pipeline/registry.go:276-301 | An in-place edit of `Context.Messages` by a handler is lost (latent) | Copy the slice before dispatch, or document |
| R10 | Low | internal/pipeline/registry.go:362-382 | Same for `AfterTool` result edits (latent, no handler today) | Clone `in.Result` for `orig` |
| R11 | Low | cmd/tui/capture.go:35 | `ASK_CAPTURE` dir 0755 and cassette 0644 | 0700 dir, 0600 file |

## Decisions for the user

1. R2: should a project `AGENTS.md` symlink to another file inside the repo still load (monorepos)? Proposed: yes; block only targets outside the repo.
2. R5: is a group-writable `ask` (built under umask 002) a supported install? Decides comment fix vs mask change.
3. R6: is a models.json `Authorization` header a supported way to sign in to a proxy? If yes it is DESIGN and the docs must say it wins.
4. g4 F9: should `write` refuse to overwrite a file changed since the session read it (Claude Code does)? Today it overwrites, as Pi does.

## Not covered

- 117 Medium and Low findings (bug, other, performance) are not triaged.
- 46 failed ocr items: parts of `internal/tui`, `internal/tuiclient`, `pkg/protocol`, `internal/tools` and others had no review.
