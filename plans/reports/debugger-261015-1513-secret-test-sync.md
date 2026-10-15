# TestInteractiveSecretNeverOnScreen: deterministic sync

Cause: during the key check the dialog ignores every key but esc (internal/tui/update_providers.go onDialogKey), so "more" has no visible effect. The test could not tell when the program had read it. On a loaded host the program read it after the 401, and the typing cleared the refusal text.

Fix: after "more", the test sends an in-band size report (CSI 8;rows;cols t) with the new emulator size (askTerm.reportSize, cmd/tui/providers_flow_test.go), and waits for the screen drawn at the wider width. The program reads the PTY input in order (bubbletea input goroutine -> message channel -> Update), so the redraw proves the typing was handled. release() runs only after that. No signal is sent, because SIGWINCH takes a path with no order against input. The model-level contract is already covered by TestKeyBoxCheckingLocksInput (internal/tui/secret_input_test.go).

Results (Linux golang:1.27.0, arm64, non-root, --cpus=1, git repo, rg):
- Before (sleep removed): 1 failure in 30 (refusal cleared, "(hidden)" on screen).
- After: 0 failures in 40.
- macOS: pass. go vet ./... clean; go test ./cmd/tui/... ./internal/tui/... pass; golangci-lint 0 issues.

Files: cmd/tui/providers_security_test.go, cmd/tui/providers_flow_test.go. No product code changed.
Residual: baseline rate is low (1/30); proof rests on input ordering, not on the sample.
