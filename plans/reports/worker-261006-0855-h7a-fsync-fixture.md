# H7a filesystem sync gate

Status: DONE.
Strict Linux C compilation, image build, and the final mounted ten-case race actor all passed.

The fixture is in `internal/testsupport/fsfault/fsync-gate.c`.
It uses libfuse3 and forwards file operations to an owned mode-0700 backing directory.
File reads, writes, creation, mode changes, rename, deletion, directory reads, directory sync, and BSD file locks use real backing OS operations.
The mount uses kernel permission checks and disables attribute and entry caches.

`ask-fsync-gate BACKING MOUNT SOCKET` runs in the foreground.
Each control connection sends one newline-terminated command and receives one newline-terminated reply.
Control reads have a two-second inactivity limit and a 31-byte command limit.
The protocol does not return file contents or credential values.

| Command | Reply | Behavior |
|---|---|---|
| `ARM` | `OK` | Count regular-file sync calls and gate the second call. |
| `WAIT` | `BLOCKED` | Wait for the second regular-file sync to enter the gate. |
| `RELEASE` | `OK` | Let the real backing file sync run. |
| `WAIT_SYNC` | `SYNCED` | Wait for the later real backing directory sync to complete. |
| `RELEASE_EXIT` | `OK` | Let the directory-sync reply return to the CLI. |
| `STATUS` | `IDLE`, `ARMED`, `BLOCKED`, `RELEASED`, or `SYNCED` | Read the gate state. |

The first regular-file sync after `ARM` passes through.
The fence directory sync also passes through.
The second regular-file sync blocks before its backing sync.
After `RELEASE`, the replacement file sync runs normally.
The later directory sync blocks only after its backing sync succeeds.
This second gate lets a separate process read the durable replacement while the CLI remains alive.
The separate observer reads the owned backing file directly.
The credential sidecar lock remains held through the commit.
FUSE also serializes a mounted temp-file read behind its active sync.

Control waits have a 20-second limit.
Both filesystem gates have a 30-second limit.
Blocked callbacks check the libfuse termination state every 100 milliseconds.
Cleanup releases both gates, joins the control thread, closes the socket, and removes the owned socket path.

The Dockerfile uses official `golang:1.27-bookworm` and Debian libfuse3 packages.
It compiles the fixture with `-Wall -Wextra -Werror`.
It adds no production hook or Go dependency.
The shell runner uses an explicit container engine and optional explicit Podman connection.
It mounts the source read-only, enables `/dev/fuse` and `SYS_ADMIN`, runs the tagged CLI actor test, and removes its named container on exit.

Run the actor with:

```sh
FSFAULT_ENGINE=/opt/podman/bin/podman \
FSFAULT_CONNECTION=askcore-fs-fault-root \
FSFAULT_SOURCE=/workspace \
internal/testsupport/fsfault/run-tests.sh
```

`bash -n internal/testsupport/fsfault/run-tests.sh` passed.
Source and whitespace checks passed.
The Linux C build passed with `-Wall -Wextra -Werror`.
The container image build passed.
The first runner attempt stopped before the tests because remote Podman resolves bind sources on the VM.
The runner now accepts `FSFAULT_SOURCE` for that server-side path.
The next attempt reached the Go build, but the 2 GiB VM ran out of memory during SDK compilation.
No actor test ran in that attempt.
The container was removed and the VM was stopped.
The runner now uses `go test -p 1` to limit build parallelism.
It forwards arguments after the test expression directly to Go, so callers can use `-race -count=2` without shell evaluation.
The controller increased the owned VM memory to 6 GiB.
The first mounted race run reached the gates, but its observer read the replacement temp file through FUSE.
That read waited for the active sync and caused the gate timeout before the signals could be sent.
The actor owner changed only its observer paths to the owned backing directory.
The CLI still uses the real FUSE mount and file sync.
The runner now uses the dedicated `askcore-fsync-fault-cache` volume with standard `GOCACHE` and `GOMODCACHE` paths.
The normal runner keeps this volume for later builds.
The controller removed its owned VM and cache after the final successful run.
The isolated VM maps the host checkout to `/workspace`, so the controller uses `FSFAULT_SOURCE=/workspace`.
The controller owns creation and removal of the isolated Linux VM.
No user Docker process was stopped or changed.
No live account or credential home was used.

The callback contracts were checked against the [official libfuse operations reference](https://libfuse.github.io/doxygen/structfuse__operations.html).

## Test setup documentation

The existing `internal/testsupport/README.md` now links to the fixture, Dockerfile, runner, and CLI actor test.
It explains the Linux FUSE requirement, normal Docker use, explicit remote Podman connection, remote source path, and optional Go test arguments.
`AGENTS.md` has one optional fault-test command with a link to that setup.
Local documentation links and whitespace checks passed.
The [exact actor report](./tester-261006-0855-h7a-fsync-actor.md) records the mounted checks.

The existing fixture setup also documents the retained build cache and the three required external-fixture paths.
The final corrected Linux `-race -count=2` run passed all five cases twice in 38.186 seconds with exit code 0.
No race or cleanup diagnostic was reported.
The earlier resource-limited repeat and damaged cache were resolved by space recovery, removal of the owned cache, and a cold rebuild.
Final tagged lint passed with zero issues.
The test container, VM, and owned cache were removed; no owned process remains.
The original Podman default connection and user Docker processes were unchanged.
