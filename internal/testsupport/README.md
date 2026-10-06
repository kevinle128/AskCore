# `internal/testsupport`

Shared test helpers and mocks. Test code only.

## What belongs here

- Fixtures, fakes, testcontainers helpers

## What does not belong here

| Code | Put it in |
|---|---|
| Production code | the capability package |

## File names

`<topic>.go`

## Imports

- Allowed: any
- Denied: none

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)


## Filesystem fault tests

Use the test-only [filesystem sync gate](fsfault/fsync-gate.c) to test CLI signals during a real credential commit.
Use the [container runner](fsfault/run-tests.sh) for its build, test selection, and cleanup.
The [Dockerfile](fsfault/Dockerfile) owns the Linux toolchain and fixture dependencies.
These dependencies belong to the test container only.

The container engine must support Linux, `/dev/fuse`, and the `SYS_ADMIN` capability.
The runner mounts the source read-only and removes its test container when it exits.
It keeps the dedicated `askcore-fsync-fault-cache` volume for later Go builds.
After tests finish, remove that owned cache with `docker volume rm askcore-fsync-fault-cache`, or use the same Podman connection with `volume rm askcore-fsync-fault-cache`.
Use an existing Docker runtime with:

```sh
FSFAULT_ENGINE=docker internal/testsupport/fsfault/run-tests.sh
```

For remote Podman, select the connection explicitly.
Set `FSFAULT_SOURCE` to the checkout path on the remote VM; it can differ from the host path.
For example, a VM that shares this checkout at `/workspace` uses:

```sh
FSFAULT_ENGINE=podman \
FSFAULT_CONNECTION=test-linux-root \
FSFAULT_SOURCE=/workspace \
internal/testsupport/fsfault/run-tests.sh
```

The first runner argument is an optional Go test expression.
Arguments after it go directly to `go test`.
For race checks and repeated runs, use:

```sh
FSFAULT_ENGINE=docker internal/testsupport/fsfault/run-tests.sh 'SignalsDuringLocalDurableCommit' -race -count=2
```

Ordinary `go test` does not select the `fsfault` build tag.
The runner selects that tag.
The [CLI actor test](../../cmd/tui/auth_fsync_test.go) fails if its Linux mount or external fixture is missing.
Do not report a pass from an unselected or skipped filesystem fault test.

For an existing external fixture, set `ASK_TEST_FSYNC_HOME`, `ASK_TEST_FSYNC_BACKING`, and `ASK_TEST_FSYNC_CONTROL` together.
Use only an owned test mount, its matching backing directory, and its control socket.
The CLI uses the FUSE mount.
The separate file observer reads the backing directory because FUSE serializes reads behind a held file sync.
The runner normally lets the actor create and remove its own fixture.
