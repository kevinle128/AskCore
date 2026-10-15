# ACP driver commit fencing and auth idle admission

Status: DONE.

## Result

A session commit lock now orders driver takes against all driver-only mutations.
The session state lock is released before Agent calls, and listeners do not use the commit lock.
Prompt and continue check authority at Agent run admission.
Model changes check authority after model auth and provider readiness finish.
The Agent releases the commit lock before a run starts to emit events or drains a tool.
Cancel uses the same commit boundary and drops stale notifications.
Reset, thinking, steer, follow-up and remove use that boundary too.
Take reads Agent status under the commit lock and outside the session state lock.
Authentication now counts as host work through the same admission counter as session creation.
A closed idle admission refuses a new authentication check.

## Files

- `internal/acp/host.go`
- `internal/acp/agent.go`
- `internal/acp/ask_methods.go`
- `internal/acp/driver_commit_test.go`
- `internal/acp/README.md`
- `internal/agent/agent.go`
- `internal/agent/README.md`

The existing dirty H13b work is preserved.
No dependency, generated file, build flag or commit was added.

## Evidence

| Check | Result |
|---|---|
| Initial original ACP review overlay checks | Both fail before the repair |
| Original ACP review overlay checks after repair | Pass |
| Original ACP review overlay checks under race | Pass, 1.970 s |
| Focused permanent driver commit and auth checks | Pass |
| `go test -race ./internal/acp ./internal/agent -count=1` after the final Take change | Pass; ACP 10.489 s, Agent 4.825 s |
| `go test ./cmd/tui -run 'TestACPE2E' -count=1` | Pass, 30.827 s |
| Overlay with the old pre-commit Take status read | `TestTakeChecksRunStatusAfterCommitLock` fails as expected |
| `git diff --check -- internal/acp internal/agent` | Pass |

The permanent checks use the real SDK adapter for delayed model auth, delayed provider readiness, authentication and prompt/continue admission.
The mutation sibling check holds the actual commit lock, waits for real handler admission, advances the generation under that lock, and checks the delayed handler outcome.
This check uses a controlled generation commit instead of a built client take for those six siblings.
The cancel case holds a real run and checks that it completes without cancellation.
The Take status check waits until Take owns host admission, then starts input-owned work before it can acquire the commit lock.
The existing factory/idle and orphan busy-take checks pass in the full ACP race suite.

## Remaining scope

No known admission gap remains in the ACP driver mutation paths.
The controller owns full repository tests, lint, app cleanup, reverse-route tests, and the final review.
