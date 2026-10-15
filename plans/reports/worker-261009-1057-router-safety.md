# Leader router and artifact safety repairs

Status: DONE.
Scope: `internal/leader` code, tests, and package README.

The review overlay reproduced the pending take bug before the fix.
Detach now clears pending take ownership even when the caller has no membership.
A late successful host result keeps the committed generation but does not install a detached caller as driver.
Controlled socket tests cover detach followed by reattach and two takes while the first host result waits.

The idle barrier now counts all forwarded requests until their host responses arrive, including requests from disconnected callers.
A controlled check holds an accepted authenticate request in the outbound queue with no host writer, and confirms that host quiesce is not called.
The transformed line limit now checks the final rewritten request rather than reserving an arbitrary 1024 bytes.
Direct checks cover one byte below 65 MiB, exactly 65 MiB, and one byte above it.
The rejected line leaves no pending request or outbound line.

Artifact opens use a validated private directory descriptor and native `openat`.
Log rotation uses `renameat`.
The lifetime lock retains the checked directory and an inode-matched stdlib `os.Root` for socket identity checks.
Socket cleanup uses the held directory and `unlinkat`, and refuses a replacement inode or released lock.
A test moves the home directory and replaces its path, then confirms that cleanup operates on the original directory and preserves the replacement socket.
Concurrent native file creation on macOS returned ENOENT during the built spawn race.
A focused four-creator check reproduced this failure.
Exclusive creation followed by a separate existing-file open fixes the failure without applying truncate or append before owner and type checks.

Linux fallback opens a pidfd before process verification and sends SIGTERM through that handle.
It refuses an exited handle and rechecks the held lifetime lock and unchanged PID, start time, and instance record before signaling.
macOS fallback refuses to signal because no stable process handle is available in this implementation.
Socket shutdown remains the normal stop path.

Validation:

- `go test -race ./internal/leader -count=1`: PASS, 42.506 s after the final owner check.
- Focused router, socket cleanup, concurrent create, and line boundary checks: PASS.
- Final narrow Darwin safety race checks, including absent-home Stop: PASS, 1.915 s.
- `go test ./cmd/tui -run '^TestLeaderManagementWithoutLeader$' -count=1`: PASS after preserving the native ENOENT error cause.
- Original non-member detach review overlay: PASS after repair.
- `GOOS=linux GOARCH=arm64 go test -c ./internal/leader -o /tmp/ask-h13b-leader-linux-arm64.test`: PASS.
- Linux arm64 Alpine container checks for actual pidfd stop, refused and stale PID, changed lifetime owner, socket cleanup, and concurrent private log creation: PASS.
- The CLI owner reports that the selected built leader/connect race suite passes after the log create repair.

No queue bound, aggregate session limit, automatic driver promotion, product test flag, dependency, or generated file was added.
No unresolved question remains in this scope.
