# Provider setup review

The review fixed 28 grouped findings and left four concerns that need acceptance evidence or belong to the concurrent glossary work.
The provider flows, secret checks and concurrency regressions pass their focused repeated runs.
Vet, lint and all 26 terminal cases pass.
The full and race suites fail only on four text expectations from the excluded glossary work.
No commit, push, real-provider call, dependency change or approved provider-design edit was made.

The review used `git status` and `git diff HEAD`, including the new files.
It read the approved plan, each implementation phase and the UI design.
The [60-row audit](review-261010-codex-t1c-provider-setup-coverage.md) records each named test, its assertions and its limits.
Each behavior fix followed a failing reproduction at the real socket, terminal or closest deterministic production entry point.
Existing assertions were retained.

## Fixed findings

| Severity | Source | Defect and repair |
|---|---|---|
| High | `internal/auth/broker.go:190` | A released client could admit a late sign-in or key save. Keep released-owner tombstones and refuse later admission. |
| High | `internal/auth/keycheck.go:92` | A valid error-code string could contain the submitted key. Drop codes that contain the key. |
| High | `internal/auth/login.go:119`, `internal/settings/auth.go:356` | A save for another provider could invalidate an OAuth sign-in. Use provider revisions, retain removal fences and test legacy writers. |
| Medium | `internal/app/provider_setup.go:48` | Old provider rows could carry a newer generation. Retry until rows and generation agree. |
| Medium | `internal/auth/broker.go:678` | Successful saves retained the provider cache. Clear that provider's cache on every credential change. |
| Medium | `internal/auth/callback.go:136` | Callback shutdown cut off HTTP responses. Drain active responses before closing, with a five-second bound. |
| Medium | `internal/tui/update_providers.go:164` | Old provider replies crossed reopened boxes or leader restarts. Keep request numbers monotonic and check request, session and provider generations. |
| Medium | `internal/tui/update_providers.go:838` | Old model replies restored stale catalogs. Check request tokens, provider generation and link state. |
| Medium | `internal/tui/update_providers.go:584` | Late login replies closed a stopped box after link loss. Reject those replies. |
| Medium | `internal/tui/login_box.go:216` | Small dialogs lost input, borders or cancel controls. Reserve the required rows before wrapping optional text. |
| Low | `internal/tui/update_providers.go:336` | Address checking still accepted ctrl+y. Ignore it while input is locked. |
| Medium | `internal/tui/status.go:57`, `internal/tui/update_session.go:605` | Removal of the last current provider pointed to `/providers`. Keep the approved `/model` rail and refusal text. |
| Medium | `internal/tui/update_providers.go:838` | A pending background refresh removed the ready-model count from a valid completion note. Pass successful reply data to the note while retaining cache guards. |
| Medium | `internal/app/agent_native.go:51`, `cmd/tui/headless.go:279` | A native saved default caused the faux stream to register under the wrong API. Bind the demo stream to its own API. |
| High | `internal/acp/ask_methods.go:228`, `internal/acp/provider_methods.go:256` | Model selection could commit after provider removal, or removal could clear a newly selected provider. Recheck usability and serialize the comparison and mutation. |
| High | `internal/acp/agent.go:294` | Removal during session construction could leave an unusable model in the new session. Check the published session under its mutation lock and clear it. |
| Medium | `cmd/tui/tui_client.go:145` | Intentional pipe closure could make in-process cleanup fail. Ignore that transport close and preserve other joined cleanup errors. |
| Medium | `cmd/tui/tui_client_e2e_test.go:573` | The queue test could enqueue before its asynchronous prompt started. Wait for that prompt's acceptance before checking its queue. |
| Medium | `cmd/tui/interactive_flow_test.go:103` | The model helper returned before the selector closed. Wait for the editor before sending the next input. |
| Medium | `e2e/tui/run.py:180` | Recording call artifacts retained typed secrets. Remove input events before writing the call artifact; keep output leaks visible to the scanner. |
| Medium | `e2e/tui/run.py:887` | The Ghostty provider case inspected the loading screen. Wait for all seven rows. |
| High | `e2e/tui/real_provider_scenarios.py:32,57` | Fixed temporary homes could collide with another run or delete its files. Use unique owned homes and reject resets outside them. |
| Medium | `e2e/tui/real_provider.py:154`, `real_provider_scenarios.py:163` | Cleanup could leave children or leaders alive. Close tracked sessions, reap stopped children and stop owned leaders before deleting homes. |
| Medium | `e2e/tui/real_provider_scenarios.py:125` | Some scenario paths skipped secret scans or missed refreshed tokens. Scan every exit path and reread credential fields before removal. |
| High | `plans/261009-2239-t1a-local-tui/artifacts/design-template.md:213` | The template had lost accepted decisions beyond the provider amendments. Restore the source and regenerate through its owner script. |
| Medium | `internal/acp/README.md:101`, `docs/tui-architecture.md:119`, `e2e/tui/README.md:122` | Several owner, release, input-lifetime and runner claims were stale. Correct the smallest owning documents, including `CLAUDE.md`, and check their links. |
| Medium | `plans/261010-1424-t1c-provider-setup/phase-08-real-provider-and-docs.md:4`, `plan.md:25`, `tasks/todo.md:55` | Completion claims included an unrun fresh OAuth sign-in. Leave that phase and checkbox open. |
| Medium | `cmd/tui/providers_security_test.go:233`, `providers_flow_test.go:559,876`, `internal/tui/secret_input_test.go:335` | Acceptance tests missed successful copy-code secrecy, actual `/new`, unchecked-key entry and buffer lifecycle assertions. Add or strengthen those checks without removing existing assertions. |

## Left findings

| Severity | Source | Reason left |
|---|---|---|
| Medium | `internal/tui/secret_input_test.go:355`; coverage rows 5, 12, 18, 34, 39 | Nonempty draft and undo isolation pass through the box handlers, but public `/providers` entry over a nonempty draft is unproved. The approved design gives no key or command gesture for that entry. No new gesture was added. |
| Medium | Approved design `design.md:1281,1319,2095`; `internal/tui/model_line.go:330` | The removal sketches retain the historical model line, while criterion 51 says there is none. Code matches the sketches and emits no new line for an empty model. Both accepted statements were preserved. |
| Medium | `plans/261010-1424-t1c-provider-setup/phase-08-real-provider-and-docs.md:23` | A fresh real OAuth sign-in remains unverified. This review forbids real-provider calls. The recorded earlier run used existing OAuth credentials. |
| Medium | `internal/tui/secret_input_test.go:122,142`, `cmd/tui/providers_flow_test.go:945,966`; plan map rows 26, 54 | Concurrent glossary work leaves text expectations and mapped names inconsistent. Per the user's instruction, those renames and their tests were not fixed or reverted. |

Owner tombstones retain one small entry per departed client until broker close.
This is the admission fence for asynchronous handlers.
Provider revision metadata is additive under the existing store schema; old global APIs and old-writer fences remain tested.
The accepted unchecked Token Plan save, offline usable status and `TEA_TRACE` exception remain in place.
Startup removal clears the constructed model; it does not choose a different model for a live session.

## Verification

Commands ran from this worktree through a wrapper that removes credential environment variables and redacts their values from output.
Go checks used a private temporary build cache.
Required checks that needed process inspection, terminal lifecycle files or the lint cache were retried with approved sandbox access.

| Command | Result |
|---|---|
| `go vet ./...` | PASS. |
| `go test ./... -count=1` | Exit 1: four glossary-only expectation failures in `internal/tui` and `cmd/tui`; all other packages pass. |
| `go test -race ./internal/auth ./internal/acp ./internal/agent ./internal/leader ./internal/tui ./internal/tuiclient ./internal/app ./pkg/protocol ./cmd/tui` | Exit 1: the same four expectation failures; no race report; the other seven packages pass. |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | PASS: 0 issues, including depguard. |
| `python3 e2e/tui/run.py --artifacts /private/tmp/ask-provider-review-e2e-complete` | PASS: all 26 cases, unchanged archive/baseline/product hashes, no secret or boundary error, eight clean leader stops. |
| `python3 e2e/tui/test-assertions.py` | PASS: 17 controls. |
| `python3 e2e/tui/test-real-provider-safety.py` | PASS: 3 fake-process controls; only selected AST definitions ran. |

Focused `-count=20` runs passed for released-owner admission, provider revisions, refusal secrecy, coherent status, cache invalidation, callback denial, browser/cancellation cleanup, model-selection/removal races, session-build/removal races, UI request ordering, secret resize/zeroing, narrow dialogs and removal notices.
The real fixture OAuth completion test passed 20 runs after reproducing the note race.
The combined copy-code, faux-after-native-default, in-process client and cleanup-error tests passed 20 runs after reproducing the timing and close failures.
Focused race runs passed for the repaired auth, store, ACP and UI paths.
The 60-row appendix identifies the criterion-specific tests and the remaining partial assertions.
Terminal artifacts are in [/private/tmp/ask-provider-review-e2e-complete/results.json](/private/tmp/ask-provider-review-e2e-complete/results.json).
These headless results do not certify a physical Terminal.app or iTerm2 run.

Both final Go runs failed `TestKeyBoxSavedNoteAndRail`, `TestKeySavedOnFirstRunPointsToModel`, `TestInteractiveFallbackNoticeWhenDefaultRemoved` and `TestInteractiveProvidersChangedSecondClient`.
Their expected `signed in` or `not signed in` text differs from the concurrent `key saved`, `credential saved` or `no credential` text.
The user expressly excluded fixes to those renames and their inconsistencies.
The panic printed by the intentional panic-client test is recovery evidence, not another failure.

No real-provider runner was executed.
The tracked `__pycache__/sketch-gen.cpython-314.pyc` restore was blocked by the repository path hook; it was left as the user permits.
No process from another owner was stopped.
`go.mod` and `go.sum` are unchanged.
The full whitespace check flags 23 padding rows in generated fixed-width sketches; ordinary source and prose checks pass.
