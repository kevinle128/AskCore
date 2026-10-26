# Linux CI failure diagnosis

Method: container golang:1.27 (1.27.2) and golang:1.27.0, linux/arm64, non-root user `runner`, real `git init` repo, ripgrep installed, no API-key env. Host: macOS, go1.27.0. No repo file was edited. Logs are in /tmp/dbg/o2, /tmp/dbg/o3.

CI uses `setup-go` with `go-version-file: go.mod` (`go 1.27.0`), so GitHub runs exactly Go 1.27.0 (the golang:1.27 image is 1.27.2).

## Summary table

| # | Test / step | Class | Fails on GitHub? |
|---|---|---|---|
| 1 | golangci-lint | Real Linux failure (linter vs Go 1.27 stdlib), plus a 1.27.2 artifact | Yes |
| 2 | TestVersionJSONCarriesCommitTime | Emulation artifact (no .git) | No |
| 3 | TestLeaderE2ERegularFileAtTheSocketPathIsKept | Real Linux bug in internal/leader | Yes |
| 3b | TestLeaderE2EStaleSocketReplacedByTheLockWinner (not in your list) | Test assumes a new inode number | Very likely |
| 4 | internal/acp TestStateReportsModelFallbackUntilModelChanges | Race in the test, not env-dependent | Flaky, likely |
| 5 | internal/auth TestBrowserCallbackBoundAndCleanup | Test keep-alive race, Linux-sensitive | Yes (33/40 runs fail) |
| 6 | TestPrintFileAndSearchToolsTryPathVariants | Emulation artifact (rg missing in my 2nd run) | No (CI installs rg) |
| 7 | TestInteractiveSecretNeverOnScreen | Flaky under load (1 fail of 8 runs, 30 s `exitWait`) | Unlikely |

"Caught panic: ... panicClient.NewSession" in the cmd/tui output is deliberate test output (interactive_test.go:464), not a failure.
Other packages: all `ok` in the full run (agent, app, auth, bus, leader, tui, tools, sandbox, jobs, settings, and others).

## 1. golangci-lint

Two distinct causes.
- Container 1.27.2: `internal/bus/follow.go:4:2 / :5:2 could not import errors / sync ... export data version 5 is greater than maximum supported version 4`. The `"sync"` import is only the first file the typechecker met. Go 1.27.2 emits export data newer than golangci-lint v2.12.2 / x/tools v0.48.0 can read. Artifact of the image; GitHub gets 1.27.0.
- Go 1.27.0 on Linux (real): `buildir: package "poll": unexpected expr: *ast.KeyValueExpr` from honnef.co/go/tools@v0.7.0 `go/ir/builder.go:888`, then `Running error: can't run linter goanalysis_metalinter`, exit 3. staticcheck cannot build IR for a Linux-only stdlib file in `internal/poll`. Reproduced on the macOS host with the same pinned linter and `GOOS=linux GOARCH=amd64` (fresh `GOLANGCI_LINT_CACHE`), so it is deterministic, not an arm64 effect. Native macOS passes only because the darwin build of `poll` does not hit the construct.
- Verified fix: in a /tmp copy, `go get github.com/golangci/golangci-lint/v2@v2.14.0 && go mod tidy`, then `GOOS=linux GOARCH=amd64 golangci-lint run ./...` gives `0 issues.` (staticcheck goes v0.7.0 to v0.8.1).
- Note: my host lint with the default cache printed 4 bogus issues for `../worried-ferret/...` (a stale cache entry from a deleted sibling worktree). With a fresh cache the host gives `0 issues`. So "passes on macOS" needs a clean `GOLANGCI_LINT_CACHE`.
- Fix (go.mod, go.sum): bump `golangci-lint/v2` to v2.14.0 and tidy. The tidy also bumps lipgloss 2.0.5 to 2.0.6, x/ansi, x/sys, x/term, x/text, jsonschema; re-run the full tests after. Alternative with less churn: add a CI step that lints with `GOOS=linux` only after the bump; there is no cheaper fix. (Option to confirm with user: a dependency bump touches a pinned version.)

## 2. TestVersionJSONCarriesCommitTime

Passes in the container once a real git repo exists (`go version -m` shows `vcs.time`). Emulation artifact. `actions/checkout` (fetch-depth 1) leaves a `.git`, and git is preinstalled on ubuntu-latest. Not verified with an actual shallow clone, but VCS stamping needs only HEAD and git on PATH.

## 3. TestLeaderE2ERegularFileAtTheSocketPathIsKept

Evidence: `cmd/tui/leader_stale_e2e_test.go:49-50`; error: `leader did not start: exit status 1 after 3 starts; log: Error: leader: unsafe path: .../leader.sock is not a socket of this user`.

Root cause: `internal/leader/client.go:296-307` (`attempt`) dials the path and maps only `ENOENT`/`ECONNREFUSED` to `outAbsent` (spawn), and `EACCES`/`EPERM` and anything else to `outFatal` (`ErrUnsafeEndpoint`). Dialing a regular file as a unix socket returns `ENOTSOCK` on macOS (fatal, test passes) but `ECONNREFUSED` on Linux. So Linux treats the file as "no leader", spawns a leader three times, and each leader refuses at `internal/leader/lock.go:221` (`ErrUnsafePath`). The user gets a spawn loop instead of `ErrUnsafeEndpoint`, and the test's final assertion ("no leader was started for an unsafe endpoint") is violated too. Real Linux bug, not an env issue; root vs non-root is irrelevant.

Fix sketch (internal/leader/client.go, in `attempt` before the dial or in the `ECONNREFUSED` branch):
```go
if fi, lerr := os.Lstat(c.cfg.Paths.Socket); lerr == nil && fi.Mode()&os.ModeSocket == 0 {
    return nil, outFatal, fmt.Errorf("%w: %s is not a socket", ErrUnsafeEndpoint, c.cfg.Paths.Socket)
}
```
A stale socket (mode is socket, dial gives ECONNREFUSED) still spawns, as the replace test needs.

## 3b. TestLeaderE2EStaleSocketReplacedByTheLockWinner

`leader_stale_e2e_test.go:38`: `os.SameFile(stale, live)` is true. The winner did replace the socket, but the filesystem reused the freed inode number. Measured: container overlayfs gives the same inode after unlink and re-bind (974719 twice, 3 of 3 times); macOS APFS gives a new one (432728150 then 432728151). ext4 on GitHub runners is likely to reuse too, but I did not measure it.
Fix (test only, cmd/tui/leader_stale_e2e_test.go): hold the old inode alive so it cannot be reused, e.g. `os.Link(paths.Socket, filepath.Join(home, "stale.keep"))` before `ConnectOrSpawn`, or drop the `SameFile` line, since a successful `ConnectOrSpawn` already proves the stale socket was replaced.

## 4. internal/acp TestStateReportsModelFallbackUntilModelChanges

Not environment-dependent. It passes with `env -i HOME=<tmp>` on macOS, yet fails about 1 in 100 on macOS (`-count=100`) and in both container runs.
Cause: `Agent.SetModel` posts `ModelUpdate` with `postLocked` (`internal/agent/emit.go:79-87`), which publishes from a separate goroutine ("never waits for a listener"). Only that event clears `Session.fallback` (`internal/acp/host.go:570-575`, via `observe`). The test calls `p.state(sid)` immediately after `ACPSetModel` returns (`provider_methods_test.go:564`) and can read the state before the listener ran.
Fix (test): replace the line with
```go
require.Eventually(t, func() bool { return p.state(sid).ModelFallback == nil }, 2*time.Second, 5*time.Millisecond)
```
A product change (wait for `pubDone` in the `setModel` handler) is possible but not needed unless clients rely on state being fresh right after set-model; that is a design call.

## 5. internal/auth TestBrowserCallbackBoundAndCleanup

`native_test.go:202`: `Get "http://localhost:53692/callback?code=xxx...": write tcp ...: write: broken pipe`, always on attempt 1 (the second loop pass). With `-count=40`: 33 fail (original), 0 fail with the patch below. Cause: the test uses `http.Get` (default client, keep-alive on). After attempt 0, `server.Shutdown` closes the connection, but the client pool keeps it idle. Attempt 1 reuses it for the 17 KB request line; the multi-chunk write hits the closed socket and returns EPIPE after an RST (a partial write is not retried by the transport). macOS timing hides it. Test-only problem; `internal/auth/callback.go` is fine.
Fix (internal/auth/native_test.go, near line 183):
```go
client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
...
resp, e := client.Get(redirect + suffix)
```
It is probably also worth checking other tests in that file that use `http.Get` against the callback server.

## 6 and 7

- TestPrintFileAndSearchToolsTryPathVariants failed in my 2nd run only because I skipped `apt-get update` and rg was not installed. It passes with rg (`o3/rgtest.txt`).
- TestInteractiveSecretNeverOnScreen failed once at 30.25 s (= `exitWait`, `cmd/tui/interactive_test.go:30`) during the cold full run on emulated arm64, then passed 7 times (0.5 s each; 3.4 s on a cold start). Cause not proven; load or cold start is the likely one. Watch it on the first CI run.

## Unresolved questions

- Whether amd64 ubuntu-latest behaves like arm64 for the flaky items; native amd64 was not run. Item 1 and the lint panic were reproduced with `GOOS=linux GOARCH=amd64`.
- Whether to accept the dependency bump for golangci-lint v2.14.0 (user decision; it touches go.mod/go.sum).
