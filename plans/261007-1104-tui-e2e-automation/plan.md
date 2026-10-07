---
title: "TUI E2E automation"
status: in-progress
created: 2026-10-07
---

# TUI E2E automation

## Outcome

Add an independent E2E test project at `e2e/tui` in this repository.
Run the real T0 binary with Microsoft tui-test through a single test command.
Use the installed CLI version as the initial verified pin, not the untested current upstream API.
Keep dependencies and test artifacts outside the production Go module.

## Scope and constraints

Use Python standard-library tests to drive the pinned CLI.
Reuse the immutable Phase 5 T0 source archive as the current test target.
Build and run it from a temporary copy with `GOWORK=off`.
Do not change production Go code, root Go dependencies, generated files, or previous source archives.
Retain the Go fixture checks for termios, signals, partial writes, and cleanup.
Use named, owned sessions and close only sessions and daemons created by this runner.
Use conditional waits and exact terminal-state assertions, not arbitrary sleeps or screenshot generation alone.
Save command results, terminal state, screenshots, and recording evidence for failures.
A headless engine result does not certify Terminal.app or iTerm2 compatibility.
Do not mark the previous T0 real-terminal gate complete or select D14 silently.

## Inspection evidence

Ask is a Go modular monolith with a Bubble Tea TUI scaffold.
Shared Go test helpers live in `internal/testsupport`; no separate E2E runner exists.
The accepted T0 prototype is an isolated Go module with 39 automated tests.
Its durable source is `plans/261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source`.
The installed tui-test CLI reports `0.1.0-beta.2`; its live help is the initial API authority.
Python, Go, Node, and uv are available.
The user authorized implementation and removed intermediate approval pauses.

## Phases

1. Review this plan and inspect the installed CLI API and session isolation.
2. Prove a real binary startup, input, state inspection, and shutdown through tui-test.
3. Implement the test project and G1/G2/G3/G6 scenarios with exact assertions and artifacts.
4. Run independent review and tests, then update the owning setup and maintainer documentation.

## Acceptance criteria

- One documented command builds the isolated target and runs the E2E suite.
- The tested CLI version is pinned and checked before use.
- G1 asserts 2,000 ordered lines in terminal screen and scrollback, with editor and cursor checks.
- G2 asserts a bounded selector window, selection retention, and draft/cursor restoration.
- G3 asserts Unicode cell placement and resize with no application transcript replay.
- G6 asserts newline versus submit and complete bracketed-paste integrity.
- Run Alacritty and Ghostty when those backends are available in the pinned binary.
- Unsupported capability paths have explicit results; no expected failure is disguised as PASS.
- Failures leave reviewable artifacts, and all owned child processes, sessions, and daemons close.
- Existing Go fixture tests remain available and production boundary hashes match.
- Independent test and review reports identify all remaining limitations.

## Risks and rollback

The beta CLI may differ from current upstream docs; inspect its live help before adding calls.
The daemon may need permission for its own session directory or socket; use its supported isolated configuration or a narrowly scoped sandbox escalation.
Prefer ephemeral programmatic sessions only if they reduce actual complexity or resolve an observed CLI limitation.
Do not bypass native-app automation restrictions.
On a runner failure, retain artifacts and close only its owned sessions.
Rollback only new E2E files and their owning documentation edits, preserving previous workspace changes.


## Execution checkpoint — 2026-10-07

The independent E2E project and owning command documentation are implemented.
All four numbered plan steps were swept; this plan has no separate phase files or task checkboxes.
The [source review](../reports/code-review-261007-tui-e2e.md) has no unresolved runner finding.
The [independent test report](../reports/tester-261007-tui-e2e.md) confirms six native-engine passes and two G3 failures.
Both Alacritty and Ghostty pass G1, G2, and G6; strict G3 assertions expose stale editor rows during normal resize.
The documented runner returns exit code 1 and retains reviewable failure artifacts.
All 39 isolated Go checks and four Python assertion controls pass.
The tester verified owned cleanup and matching archive and product boundary hashes.

Plan status remains in-progress because the target does not meet full terminal compatibility acceptance.
The CLI has no done-with-concerns status; its update command also returned planstore not_found for the ID that list/show found.
The existing status was preserved without reusing an old plan ID.
Read the [full sync record](../reports/pm-261007-tui-e2e.md) and [resize diagnosis](../reports/debugger-261007-tui-e2e.md) for exact hashes, chronology, controls, and limitations.
No immutable target archive or production Go source was changed to hide these failures.
Real iTerm2 checks remain pending and D14 readiness remains false.
