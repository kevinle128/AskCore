# Flaky TestAuthStoreTwoProcessesAndReader: root cause

## Root cause
`ensureHome` (internal/settings/paths.go) did Lstat, then `os.Mkdir`, and returned any Mkdir error. The test starts two child processes and a reader against a home directory that does not exist yet. When two actors both saw "not exist", the loser got `mkdir: file exists` and failed. A child exits with status 2 (its stderr is not captured, so CI only showed `exit status 2`). A reader would fail with the raw mkdir error.

Classification: product bug (check-then-create race between processes on first use). No torn or stale credential: the lock, temp file, fsync, rename path is unaffected and the home owner/type/mode checks are kept.

## Evidence
- macOS, -race, 300 runs: 0 failures (the race window is tiny when CPUs are free).
- Linux arm64 container (golang:1.27.0, uid 1000), `--cpus=1`, 6 spinning loops: 2 failures in 500 runs, both `auth_test.go:276: exit status 2`. `--cpus=2` with load: 0 in 500. Without load, `--cpus=1`: 0 in 200.
- New unit test `TestEnsureHomeConcurrentCreate` (16 goroutines, 50 rounds) fails before the fix with `mkdir ...: file exists` on round 0.

## Fix
`ensureHome` accepts `os.ErrExist` from Mkdir and then applies the same Lstat checks (directory, not symlink, owner, mode 0700) to the existing entry. Files changed: internal/settings/paths.go, internal/settings/auth_test.go (new test).

## After the fix
- Container `--cpus=1` and `--cpus=2` with load, 500 runs each: 0 failures. (Longer runs: see the final status in the reply.)
- `go vet ./...` clean; `go test -count=1 ./internal/settings/...` pass; golangci-lint on the package: 0 issues.

## Suggested CHANGELOG line
Fixed: first use of a new `ASK_HOME` by two processes at once no longer fails with "file exists".

## Unresolved
- The helper child discards stderr, which hid the message. Optional: capture it in the test (not changed).
