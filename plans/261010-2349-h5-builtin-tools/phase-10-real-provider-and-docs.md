---
phase: 10
title: "Real-provider acceptance and docs"
status: completed
priority: P1
effort: "1d"
dependencies: [9]
---

# Phase 10: Real-provider acceptance and docs

## Context

- Architecture: [Problem & goals](../architectures/h5-builtin-tools-architecture.md#problem--goals) (roadmap exit), [Open questions](../architectures/h5-builtin-tools-architecture.md#open-questions) (env filter list, TUI notices, flood guard numbers), [Missing pieces](../architectures/h5-builtin-tools-architecture.md#missing-pieces) (TUI part is T1b).
- Decisions: [Q3](../architectures/h5-builtin-tools.md#q3-critical-commands-and-the-no-approval-stance-d3), [Q8](../architectures/h5-builtin-tools.md#q8-orphaned-children-after-a-leader-crash-and-the-job-cap), [Q12](../architectures/h5-builtin-tools.md#q12-child-environment-of-bash) (known risk), [Q14](../architectures/h5-builtin-tools.md#q14-message-shape-of-system-notices).
- Spike 1: [Credential handling](../reports/spike-261010-system-reminder-notice.md#credential-handling) (work on a copy; tokens can rotate).
- xia T1b: [Surface by surface](../reports/xia-261010-1642-t1b-tui-surfaces.md#surface-by-surface) rows 1 and 3 (what T1b builds on top of H5).
- Roadmap: [H5](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools) (exit), [T1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core) (T1b rows that wait on H5).

## Overview

Run the roadmap exit with a real provider through the real binary and record it in a report with no secret. Then bring the docs to the implemented state.

## Requirements

- Functional:
  - Real-provider run (exit criterion): the built `ask` in print mode, `ask -p --provider openai --model gpt-5.5 "The test in this repo fails. Find out why, fix the code and run the tests."` in a temp copy of the prepared repo from phase 9 (Go module, one failing test). `OPENAI_API_KEY` comes from the environment and is never printed. `ASK_HOME` points to an empty short temp path (for example `/private/tmp/ask-h5-home`). Pass: the transcript (`--mode json`) shows `read`, `edit` (or `write`) and `bash` calls, the file is changed, `go test ./...` in the copy passes after the run, exit code 0, and `pgrep` finds no process of the run afterwards.
  - OAuth runs (required, same pass criteria): copy `~/.ask-t1a-oauth` to `/private/tmp/ask-h5-oauth` (mode 0700) and point `ASK_HOME` at the copy, with `OPENAI_API_KEY` unset for these runs. Run the same prompt with `--provider anthropic` (`claude-sonnet-4-6`, Anthropic OAuth) and with `--provider openai --model gpt-5.6-sol` (ChatGPT OAuth, method `openai-chatgpt`, `internal/auth/chatgpt.go:39`) when the copy holds that credential; record a missing credential as a deviation. Delete the copy after. A rotated refresh token is not copied back to `~/.ask-t1a-oauth` without the coordinator's approval (plan unresolved question 2).
  - After the OAuth runs (user decision, 2026-10-11): if a refresh token rotated (compare the token generation and a hash fingerprint, never the value), back up `~/.ask-t1a-oauth/auth.json` as `auth.json.before-h5` (mode 0600), copy the refreshed `auth.json` from `/private/tmp/ask-h5-oauth` back (mode 0600), then delete the copy; report only which providers rotated.
  - Background check in the same session: a second prompt `Run "sleep 5; go test ./..." in the background and tell me the result when it finishes.` Pass: the model uses `run_in_background` (or the wait budget), a notice arrives, the model reads the output once or twice (record the poll count, spike 1), and reports the result.
  - Report: `plans/reports/real-provider-acceptance-<date>-h5-tools.md` with commands (secrets as `<from env>`), model ids, tool call sequence, poll count, exit codes, timings, and deviations. Scan the report for key-shaped strings before saving.
  - Docs (only what changed):
    - `internal/tools/README.md`: the tool list, file names (`file_*`, `search_*`, `shell_*`), the prompt section, per-session state, concurrency flags.
    - `internal/sandbox/README.md`: rewrite "What belongs here" to the process runner (no `Sandbox`/`Manager` interfaces yet), env filter list and its known risk (Q12), kill and drain rules, spill cap; imports: standard library only.
    - `internal/jobs/README.md`: create (purpose, states, notices, monitor flood guard numbers, cleanup, allowed imports `sandbox`, `pkg/protocol`).
    - `internal/workspace/README.md`: path resolution rules.
    - `internal/README.md`: add `jobs` to the package map; update `sandbox` and `workspace` lines.
    - `docs/ask-architecture-reference.md` sections 8 and 9: the new package, its import rule, the `agent.Config.Release` close path, `command-guard` as the example control handler.
    - `CLAUDE.md`: Project Structure line for `jobs`; the `ask -p` command line still says the only provider is `faux` until H3 — correct it to the providers that exist and add a sentence on built-in tools, `ASK_COMMAND_GUARD=1`, and that background jobs end when `ask -p` ends.
    - Roadmap `roadmap.md` H5 section: status line "implemented" with plan link and report link; T1b row: H5 delivered, the TUI notice hiding and job status still need the T1b design (N1).
    - `docs/testing-llm-cassettes.md`: the provider used by the cassette tests after N4.
- Non-functional: no secret in any file; links checked; ASD-STE100 style.

## Architecture

No code. Data flow of the run: user → `ask -p` → OpenAI → tools → files and processes → report.

## Runtime Flow

- Actor and trigger: the user runs `ask -p` with a real key.
- Registered entry point: the built `ask` binary.
- Internal path: the full path of the plan matrix.
- Prepared data and external mocks: a temp copy of the phase 9 repo; no mocks (real OpenAI with an API key; Anthropic and ChatGPT OAuth from the copy).
- Observable result: changed file, passing `go test`, transcript, no leftover process.
- End-to-end proof: the report.

## File inventory

| File | Action |
|---|---|
| `plans/reports/real-provider-acceptance-<date>-h5-tools.md` | create |
| `internal/tools/README.md`, `internal/sandbox/README.md`, `internal/workspace/README.md`, `internal/README.md` | modify |
| `internal/jobs/README.md` | create |
| `docs/ask-architecture-reference.md`, `docs/testing-llm-cassettes.md`, `CLAUDE.md` | modify |
| `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` (H5 and T1b lines only) | modify |

## Tests Before

`go test ./...` passes (phase 9 state).

## Tests After

No new code tests. Checks: the real-provider pass criteria above; a link check over the changed docs; a grep of the changed files and the report for `sk-`, `Bearer `, `refresh_token`, `api_key=` returns nothing.

## Function and interface checklist

- [x] real-provider run and report (OpenAI API key and Anthropic OAuth pass; ChatGPT OAuth unverified, see the report)
- [x] READMEs, architecture reference, `CLAUDE.md`, roadmap, cassette doc

## Dependency map

Needs phase 9. Soft order with T1c phase 8 (`plans/261010-1424-t1c-provider-setup/phase-08-real-provider-and-docs.md`), which also edits `CLAUDE.md` and the roadmap: run this phase after T1c phase 8 is done, or merge by hand.

## Related Code Files

- Modify and create: the files in the inventory. No Go code.

## Implementation Steps

1. Prepare the temp repo and the empty `ASK_HOME`; run the API-key prompt; then the OAuth runs from the copy; save each JSON transcript outside the repo.
2. Run the background prompt; count `job_output` calls while running.
3. Write the report; scan it for secrets.
4. Update the docs; check links; grep for secrets.
5. Remove temp copies; check `pgrep` for leftovers.

## Regression Gate

`go test ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` (docs only, but the gate proves nothing moved).

## Success Criteria

- [~] The exit criterion passes with the OpenAI API key and with the Anthropic OAuth credential; the report shows read, edit and bash for each run. The ChatGPT OAuth credential was rejected at refresh and is unverified.
- [x] After the OAuth runs, rotated tokens are copied back to `~/.ask-t1a-oauth` with a backup `auth.json.before-h5`, and the temp copy is deleted.
- [x] Docs describe the implemented state; every changed link resolves.
- [x] No secret in the report or docs; temp copies removed; no leftover process.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| The model does not finish the task in one run | M x M | Allow up to three runs; record each; signal: three failures; response: inspect the transcript for a tool defect and fix it in the owning phase. |
| The model polls `job_output` many times | H x L | Record the count (spike 1); response per phase 6 risk. |
| `CLAUDE.md` or the roadmap conflict with T1c phase 8 edits | M x L | Soft order above; edit only the named lines. |

## Security Considerations

- The key is read from the environment; it is never passed as `--api-key` on a command line (it would show in `ps`).
- OAuth copies live under a short `/private/tmp` path with mode 0700 and are removed after.
