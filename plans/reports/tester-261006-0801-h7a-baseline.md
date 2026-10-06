# H7a baseline test report

Status: DONE_WITH_CONCERNS

## Scope

Read-only baseline checks used the current working tree in `/Users/dale/orca/workspaces/AskCore/master-2` on macOS.
The tree already contained H4 and H7a changes.
No product, plan, database, or generated file was changed.
Only this report was added.
No live provider, login, inference, or credential operation was performed.
Tests used their existing isolated files, local test servers, and subprocesses.

## Commands and results

| Command | Exit | Result |
| --- | --- | --- |
| `go test ./internal/settings ./internal/auth ./internal/providers ./internal/agent ./internal/app` | 0 | All five packages passed. |
| `go test -race ./internal/settings ./internal/auth ./internal/providers ./internal/agent ./internal/app` | 0 | All five packages passed; no race report. |

The first command reported settings 1.724s, auth 1.309s, providers 1.465s, agent 1.387s, and app 0.361s.
The race command reported settings 4.191s, auth 1.774s, providers 1.902s, agent 2.244s, and app 1.637s.
The commands ran in harness sessions 13534 and 92889 and both finished.
No background process was started or left running by this task.
There was no baseline failure in these commands.
The provider command covers the root provider package; it does not run the adapter subpackages.
Lint, full-repository tests, adapter tests, and command tests were outside this baseline command scope and were not run.

## Evidence read

The phase 1 and phase 2 files in `plans/261006-0157-h7a-subscription-auth/` define the accepted contract.
The read covered `internal/settings/auth.go`, its tests and local path/lock code; all current `internal/auth/*.go`; `internal/providers/auth.go` and its tests; `internal/app/module_auth.go` and its tests; and agent request/readiness call sites.
Existing key, model-switch, and stream test names were inspected to identify protected callers.

Current settings tests cover revision conflicts after absent logout, durable fences, wrong attempt rejection, pre-rename failures, directory-sync uncertainty, two writer processes, process lock release after death, corrupt files, legacy keys, unrelated unknown data, canceled lock wait, failed replacement, and fence persistence after process exit.
Current auth tests cover explicit/saved/environment precedence, failed OAuth without key fallback, two concurrent goroutines using one grant, failed refresh without resubmission, and active refresh waiting.
Current app tests cover real store/resolver composition with a registered producer, competing overrides, a saved-key Prompt, and a new saved key on a later tool request.
The app Prompt tests use faux inference and the refresh tests use callback exchange functions.
Current snapshot tests check redaction and provider/endpoint binding.
The agent readiness test checks retained model state on denied readiness.

## Missing acceptance proof

- There is no connected two-process Prompt test with an external token server that proves one remote rotation and preserved unrelated records.
- Fence process-exit tests stop after a local fence write; they do not prove death after server rotation or a second real prompt with zero token requests.
- There is no command signal test for SIGINT, SIGTERM, or SIGHUP with a commit barrier longer than two seconds, a second signal, budget exhaustion, or forced death.
- There is no explicit proof of a separate five-second local commit budget or cancellation checks between local I/O steps.
  Current `AuthStore.Refresh` creates a fifteen-second exchange context, then writes the replacement without a separate commit context.
- There is no connected auth test for logout or account replacement while a caller waits, access denial, early-refresh timing, or a changed final provider after PrepareRequest.
- The root-package run does not prove OAuth headers at real adapter HTTP boundaries, wrong path/origin/redirect/header rejection, or per-request decoder isolation.
- There is no combined real resolver/Agent test that counts the full public lifecycle for early failure and canceled full-buffer settlement.
- Current unknown-field assertions preserve unrelated provider fields; they do not prove preservation of unknown fields when replacing that same provider.
- Unsafe-owner, existing-home permission failure, and bounded contention without an externally supplied cancellation deadline lack dedicated proof in the inspected tests.

These are acceptance gaps, not failed-test claims.
Passing this baseline does not show that phase 1 or phase 2 is complete.

## Unresolved questions

None for this baseline task.
