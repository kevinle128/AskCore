# Flaky ACP tests on CI: root cause and fix

Both failures are test bugs. No product change, no CHANGELOG line.

## 1. TestAdapterFailStopsRunsAndClosesOutput (agent_test.go:504)

- Reproduced: `go test -race -count=300 -cpu 1` failed 35/300 (cpu 2 and 4: 0/300). The frame at index 2 was an ERROR response (`output_failure`), not a success.
- Cause: `Adapter.Fail` latches the host (host.go:141). The goroutine in `bindNotifier` (agent.go:146-149) then closes the output. The test calls `Fail` directly, so the checked writer is still healthy. The SDK writes the error answer of the stopped prompt (connection.go handleInbound). Whether that frame reaches the pipe before the close is a scheduler race. On macOS the close always won or lost the same way; with one CPU the frame won 12% of the time.
- A second symptom of the same race: with the close winning, the response never exists (stack dump showed no reader or SDK goroutine, `await` timed out in my first attempt). So "wait for the response" is also wrong.
- Contract (README: "no success after a lost output"): a success must never be written. An error frame before the close is allowed. The old assertion `responseIndex == -1` asserted more than the contract.
- Fix (internal/acp/agent_test.go, adapter_peer_test.go): the peer exposes `outDone` (closed when the output reader hits EOF). The test waits for it, then checks every frame for the prompt id has an error and no result. Same contract, deterministic.
- After: `-race -count=400` at `-cpu 1` and `-cpu 4`: 0 failures.

## 2. TestACPE2ECancel "timeout: first update" (acp_e2e_test.go:649)

- Reproduced with 10 busy-loop processes and `GOMAXPROCS=1 -cpu 1`: 3/40 failed.
- Cause: the test sent `session/cancel` (idle session) just before `session/prompt`. The SDK handles requests on a new goroutine at once and notifications on a separate queue goroutine (third_party/acp-go-sdk/connection.go:406-445 and 462). Under load the prompt is admitted first, and the cancel then reaches the new binding. The adapter keeps a cancel that comes after admission and before the run exists (README, Adapter contract), so the run is cancelled before any update. `waitRunning` then waits 30 s.
- This is correct product behaviour. The wire does not order a notification before a later request.
- Fix (cmd/tui/acp_e2e_test.go): the two idle-cancel notifications moved to after the cancelled run, followed by a state check. They still prove an idle or unknown-session cancel is harmless. The other cancels in the file already follow `waitRunning`.
- After: same load, 0/40 and 0/80.

## Verification
- `go vet ./...` clean; `go test -count=1 ./internal/acp/... ./cmd/tui/...` ok; golangci-lint on both trees: 0 issues.

Unresolved: Linux container run was not done; reproduction used `-cpu 1` and CPU load on macOS.
