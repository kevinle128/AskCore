---
phase: 7
title: "Docs, changelog and full gate"
status: pending
priority: P2
effort: 3h
dependencies: [1, 2, 3, 4, 5, 6]
---

# Phase 7: Docs, changelog and full gate

## Goal

Describe every user-visible change in the owning docs and in `CHANGELOG.md`, then run the full gate on the result of phases 1 to 6.

## File ownership

- Modify: `docs/cli.md`, `docs/testing-llm-cassettes.md`, `CHANGELOG.md`
- Modify: `internal/agent/README.md`, `internal/pipeline/README.md`, `internal/bootstrap/README.md`, `internal/tools/README.md`, `internal/sandbox/README.md`
- Do not edit `docs/reference/` by hand (generated).

Write in ASD-STE100 (`CONTRIBUTING.md`). Read each document before the edit, and check each claim against the merged code after it. Use the `ask-docs-sync` skill (`.claude/skills/ask-docs-sync`) for the final pass.

## Docs lines

| Defect | File and line | Change |
|---|---|---|
| R2 | `docs/cli.md:147` | Add: a project `AGENTS.md` or `CLAUDE.md` whose link target is outside the project (git root, or cwd without one) is left out. A link inside the project loads. Ask home files are not checked. |
| R2 | `docs/cli.md:237` | The count also includes files outside the project. |
| R2 | `docs/cli.md:336`, `:391` | Add the word `outside_project` to the text list and to the `context[].skipped` row of the JSON table. |
| R2 | `internal/bootstrap/README.md` | The containment rule and the new skip reason. |
| R3 | `internal/tools/README.md:52` | `read` reads a file of up to 4 MiB whole; a larger text file is read page by page with bounded memory, and each page still reports the total line count. |
| R6 | `docs/cli.md:553` | An `Authorization` header wins over a static API key (saved key, `apiKey`, environment, `--api-key`); a saved OAuth sign-in wins over the header. The other credential headers are removed. |
| R7 | `internal/tools/README.md:54` | `edit` also fails when the file changes between its read and its write; a change in the same mtime tick with the same size is not seen. |
| R11 | `docs/cli.md:639`, `docs/testing-llm-cassettes.md:67`, `:92` | Ask makes the capture folder with mode 0700 and writes the cassette with mode 0600. An existing folder keeps its mode. |
| N1 | none (no doc names the empty-reply rule) | Changelog only. |
| N3/N4 | `docs/cli.md:550` | A trailing `/` of `baseUrl` is removed. |
| N7 | `docs/cli.md:554` | Name `supportsReasoningEffort` (default true; `false` stops `reasoning_effort`), if the user confirms the wire choice. |
| N25 | `internal/tools/README.md:52` | An image of more than 16 million pixels is refused with a text note. |
| N27 | `internal/sandbox/README.md` | `Truncation.TruncatedBy` names the limit that cut the output. |
| N29/N34 | `docs/cli.md:449` | A signal during setup exits 130, 143, or 129; a lost leader exits 1, also after the end of input. |
| N30 | `docs/cli.md:639` | `ASK_CAPTURE` also works when `--model` alone selects the provider. |
| N31/N32 | `docs/cli.md:418` | `--json` exits 1 when the output cannot be written. |
| N35 | `docs/cli.md:631` | No change: the text already says `faux` only, which is now true. |
| R1 | `internal/agent/README.md` | `Continue` with an empty context or an assistant tail runs queued input, and input that arrives later starts the next run. |
| R9/R10 | `internal/pipeline/README.md:24-28` | A handler may change messages or the result in place; the change counts. |

## CHANGELOG

Add one line for each user-visible fix under `[Unreleased]` > `### Fixed` (`CHANGELOG.md:101`): R1, R2, R3, R6 (under `### Changed`, because the rule is new), R7, R11, N1, N3/N4, N7 (under `### Changed` if wired), N25, N26, N29, N30, N31/N32, N34, N35. Internal fixes (R4, R5, R8 to R10, N2, N5, N6, N8 to N24, N27, N28, N33) get no line, as the changelog rule covers only user-visible changes (`AGENTS.md`, Documentation).

## Full gate

Run in this order. Each command must pass; fix the cause, do not weaken a test.

1. `go fmt ./...` and `git diff --exit-code` on the formatting.
2. `go vet ./...`
3. `go test ./...`
4. `go test -race ./internal/agent ./internal/pipeline ./internal/tasks ./internal/acp ./internal/leader ./internal/app ./internal/tools ./internal/bootstrap ./internal/sandbox ./cmd/tui`
5. `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` (depguard).
6. `go test -run '^TestReferenceDocs$' ./cmd/tui` with no `ASK_UPDATE_DOCS` (expect no drift).
7. `go test -run '^TestCassette|^TestCapturedSessionReplays' ./cmd/tui` (expect replays to pass with no re-record).
8. Search the diff for plan labels: `git diff master...HEAD -- '*.go' | rg '^\+' | rg -n '\b(R[0-9]{1,2}|N[0-9]{1,2}|F[0-9]{1,2}|B[0-9]{1,2}|H6a)\b'` finds no plan code in added Go lines (comments, test names). Check each hit by hand; a hit inside an existing identifier is not a defect.
9. Optional, by hand: `python3 e2e/tui/run.py` on one engine for a smoke run of the built `ask`.

Cassette rule: if step 6 or 7 fails because a tool text that goes to the model changed, that is a phase defect, not a doc task. Revert the text, or, if the change is required, regenerate the reference (`ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui`) and re-record all six cassettes one by one with `ASK_RECORD=1 go test -run '^<TestName>$' ./cmd/tui`. Take `OPENAI_API_KEY` from the environment and never print it.

## Success criteria

- Each row of the docs table is done or marked "no change" with its reason.
- The gate passes on one commit that contains phases 1 to 7.
- The 46 defects map to a merged change; the four items with no new test (R5 comment, N14, N15, N28) are listed in the PR description with their reason.

## Rollback

Docs commits revert on their own. The gate changes nothing.
