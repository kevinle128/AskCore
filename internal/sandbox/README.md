# `internal/sandbox`

The process runner of the shell tools: one command in its own process group, with a filtered environment, bounded output and a kill that leaves no process behind.

There are no `Sandbox` or `Manager` interfaces yet and no isolation beyond the process group (Unix only, no PTY, no cgroup). A Docker or other backend is a later addition.

## What belongs here

- `Start`, `Run`, `Process`, `Spec`, `Result` (`process.go`, `run.go`): start a command, observe its output, kill its group
- The output buffer: the first 20 KB and the last 30 KB stay in memory, the middle is counted; an optional spill file receives the whole output (`buffer.go`)
- `View`: the model view of an `Output` with a line and a byte limit, head and tail with an elision marker (`view.go`)
- `ChildEnv`: the environment of a command (`env.go`)
- `ShellArgv`: the shell and its arguments (`shell.go`)
- `Clock` and `SystemClock`: the time source of the kill grace and the drain bound, so tests use a fake clock (`clock.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| The `bash` tool and the other shell tools | `internal/tools` |
| The job table, notices and the monitor | `internal/jobs` |
| A rule that blocks a command | `internal/pipeline` (`command-guard`) |
| Path resolution | `internal/workspace` |

## Rules of the runner

- **Process group:** each command runs with `Setpgid`. A kill goes to the group, so grandchildren stop too.
- **Kill:** `Kill` sends SIGTERM, then SIGKILL after a 3 s grace. The SIGKILL timer stays armed when the main process exits first, so a grandchild that ignores SIGTERM is still killed. `KillNow` sends SIGKILL at once.
- **Drain:** one reader goroutine for each stream reads 32 KB chunks from an `os.Pipe`. After the main process ends, the readers get 3 s. A grandchild that keeps a pipe open cannot hold the result back.
- **Spill:** the spill file starts when the output passes 50 KB and stops at 64 MB. The process is not stopped at the cap; `Output.SpillCapped` is set. The caller creates the file (mode 0600) and removes it.
- **Environment:** `ChildEnv` keeps the inherited environment except names that match `(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL)` and every `ASK_*` name. It adds `TERM=dumb`, `NO_COLOR=1`, `CI=true`, `PAGER=cat`, `GIT_PAGER=cat`, `MANPAGER=cat`, `GIT_TERMINAL_PROMPT=0`, `EDITOR=true`, `VISUAL=true`, `GIT_EDITOR=true` and an empty `SSH_ASKPASS`. The caller passes the session variables (`ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, `ASK_REASONING_LEVEL`).
- **Known risk of the filter:** it matches names only. A secret in a variable with an ordinary name passes, and a harmless name such as `MY_KEYRING_PATH` is dropped. A command can also read files that hold credentials. The filter lowers the chance of a leak into a model-visible `env` output; it is not a boundary.
- **No cwd default:** `Spec.Dir` is required, so a command never runs in the directory of the daemon.

## Main types

- `Spec`, `Result`, `Process`, `Output`, `Truncation`, `Stream`, `Clock`

## File names

`process.go`, `run.go`, `buffer.go`, `view.go`, `env.go`, `shell.go`, `clock.go`

## Imports

- Allowed: standard library only
- Denied: `internal/agent`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>` (core packages do not import transport); `internal/acp`, `internal/leader`; `internal/config`

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam. `Clock` is a test seam.
- A test that starts a process checks that its group is gone (`kill(-pgid, 0)` returns `ESRCH`) in cleanup. The package runs `goleak.VerifyTestMain`.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
