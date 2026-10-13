# `internal/jobs`

The background work of one session: the job table behind the `bash`, `job_output`, `job_kill`, `job_list` and `monitor` tools.

One `Table` belongs to one session. The composition root (`newNativeAgentOn` in `cmd/tui`) creates it, passes it to `tools.NewShellTools`, and closes it through `agent.Config.Release` when the Agent is disposed. Unix only (`//go:build unix`).

## What belongs here

- `Table`: start a command as a job in its own process group (through `sandbox`), wait for a job with a budget, read its output from a cursor, kill it, list jobs, close the table (`table.go`, `read.go`)
- Completion notices: one `<system-reminder>` user message when a job ends without the model having seen its end (`notice.go`)
- The monitor: each stdout line of a command is an event; batches and a flood guard (`monitor.go`)
- `Notifier`: the way to the Agent, set after the Agent exists (`SetNotifier`)

## What does not belong here

| Code | Put it in |
|---|---|
| The tool declarations and their arguments | `internal/tools` |
| Process start, kill, output buffer, environment filter | `internal/sandbox` |
| The Agent queues (`FollowUp`, `Steer`, `Remove`) | `internal/agent`; the composition root passes them as a `Notifier` |
| Hiding a notice in a view | the client (`internal/tui`, T1b) |

## States

| Status | Meaning |
|---|---|
| `running` | The process runs. |
| `stopping` | A kill was sent; the group is not gone yet. |
| `completed` | The command exited. The exit code can be non-zero. |
| `killed` | The model, the table (close, cancel) or a limit (timeout, overload) stopped it. |
| `failed` | A signal that the table did not send ended it, or waiting for it failed. |

At most 10 jobs run or stop at the same time (monitors count). The 11th start returns `ErrTooManyJobs`. A finished job leaves the table when the model has seen its end.

## Notices

- A job that ends while no tool call waits for it, that the model did not stop, and while the table is not closing sends one notice with `Notifier.FollowUp`. The text names the job and its status line and tells the model to read it with `job_output`. A job that has ended stays in the table until the model reads its final status.
- A job whose end was reported by a waiting `bash` or `job_output` call, or that the model stopped with `job_kill`, sends no notice.
  A foreground `bash` call starts its job with `StartSpec.Foreground`: the wait of the call is registered before the process can end, so a command that ends before `Wait` runs still gets no notice.
  If the wait budget ends before the job ends, the completion notice is still sent.
- A notice that the Agent refuses (no model, queue full, disposed) goes to `Options.OnError`. The job stays readable with `job_list` and `job_output`.
- `job_output` on a running job answers at once and says that a notice will arrive.
  It tells the model not to poll.
- A canceled output read keeps the read cursor and the finished job. A later read can still report the output and the final status.
- When output passes the 64 MB spill cap, each output read shows `[spill file capped at 64 MB]`.
- `protocol.IsSystemReminder` finds a notice by its shape only: one text block that starts with `<system-reminder>` and ends with `</system-reminder>`. A person who types the same tags gets that message treated as a notice too. This only hides the text from the view of that person, and `ask -p` skips it when it looks for the last reply. Nothing else depends on it.
- The log and the `--mode json` stream keep the notice as an ordinary `user` message. Hiding it in a view is the client's job: the TUI draws a job line instead (see `internal/tui/README.md`). ACP clients get no `user_message_chunk` for it.

## Monitor flood guard

All numbers are constants in `monitor.go`.

| Rule | Value |
|---|---|
| Line length | 500 characters, then cut |
| Batch | 3,000 characters, 200 ms debounce |
| Token bucket | 10 events, one refilled each 2 s |
| Suppressed events | one `[N events suppressed ...]` note |
| Overload | 30 s of continuous suppression stops the monitor and kills its process group |
| Timeout | default and maximum 10 h; `persistent` has no timeout |

Blank lines are not events.
Events go out with `Notifier.Steer`; a still-queued batch is replaced with `Notifier.Remove` so one message holds the merged lines.
The merged output keeps the latest 3,000 characters and adds `[... more lines cut]` when older output is removed.

## Cleanup

- `Close` sends SIGTERM to every live group, waits one grace (3 s), then sends SIGKILL to what is left, waits for the groups and removes the spill directory. It sends no notices.
- `KillRemaining` sends SIGKILL to every live group at once, marks the table closed, and removes the spill directory without waiting for the owners.
  `ask -p` calls it at exit, including when the signal grace ends before Agent disposal finishes.
  A cleanup error goes to `Options.OnError`.
- No registry is kept on disk. A job of a leader that crashes is an orphan; nothing sweeps it.
- The spill directory (mode 0700, files mode 0600) is created at the first spill and holds `<job-id>.log`.

## File names

`table.go`, `read.go`, `notice.go`, `monitor.go`, `doc.go`

## Imports

- Allowed: standard library, `internal/sandbox`, `pkg/protocol`
- Denied: `internal/agent`; transport packages, `acp`, `leader`, `config` (see `.golangci.yml`)

## Rules

- Never hold the table lock while you call the Notifier or wait for a process.
- Inject time through `sandbox.Clock`; tests use a fake clock.
- A test that starts a process checks that its group is gone (`kill(-pgid, 0)` returns `ESRCH`). The package runs `goleak.VerifyTestMain`.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
