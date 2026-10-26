# `internal/jobs`

The background work of one session: the job table behind the `bash`, `job_output`, `job_kill`, `job_list` and `monitor` tools.

One `Table` belongs to one session. The composition root (`newNativeAgentOn` in `cmd/tui`) creates it, passes it to `tools.NewShellTools`, and closes it through `agent.Config.Release` when the Agent is disposed. Unix only (`//go:build unix`).

## What belongs here

- `Table`: start a command as a job in its own process group (through `sandbox`), wait for a job with a budget, read its output from a cursor, kill it, list jobs, close the table (`table.go`, `read.go`)
- Completion notices: one `<system-reminder>` user message when a job ends without the model having seen its end (`notice.go`)
- The monitor: each stdout line of a command is an event; batches and a flood guard (`monitor.go`)
- `Owner`: the conversation that starts a job. Every start names its owner; the notices of the job go to that conversation only, and only that conversation sees the job

## What does not belong here

| Code | Put it in |
|---|---|
| The tool declarations and their arguments | `internal/tools` |
| Process start, kill, output buffer, environment filter | `internal/sandbox` |
| The Agent queues and the notice port (`tasks.Port`) | `internal/agent`; the shell tools wrap the port of the call as the `Owner` |
| Hiding a notice in a view | the client (`internal/tui`, T1b) |

## States

| Status | Meaning |
|---|---|
| `running` | The process runs. |
| `stopping` | A kill was sent; the group is not gone yet. |
| `completed` | The command exited. The exit code can be non-zero. |
| `killed` | The model, the table (close, cancel) or a limit (timeout, overload) stopped it. |
| `failed` | A signal that the table did not send ended it, or waiting for it failed. |

At most 10 jobs run or stop at the same time (monitors count). The 11th start returns `ErrTooManyJobs`; when jobs of an earlier conversation hold some of the slots, the refusal says how many and that they end when the session closes, and names none of them. A finished job leaves the table when the model has seen its end.

## Background limit and quiescence

- `Options.BackgroundLimit` stops a job that runs too long in the background, with the kill reason `background time limit` and the notice `Background command "<label>" was stopped after reaching its background time limit`. A background start asks for a limit (`StartSpec.BackgroundLimit`, zero takes `Default`, `Max` caps it); a foreground job that moves to the background gets `Default` from the move. The zero value means no limit: only `ask -p` sets one.
- With a limit set, every monitor gets a deadline of at most `Default`, a `persistent` one included.
- `Options.DisableBackground` (`ASK_DISABLE_BACKGROUND_TASKS=1`) stops a foreground command at its wait budget and refuses a background start and a monitor with `ErrBackgroundDisabled`.
- `Table.Idle` is closed while every job has handed off its completion notice or has none to send. A job counts from its start until `own` has run `sendNotice`, so `Agent.WaitForQuiescence` (through `agent.Config.Background`) never sees the end of a job before its notice is queued.

## Conversations

Each job keeps the generation of its owner (`Owner.Gen`). `Read`, `Kill` and `List` take the generation of the caller: a job of another generation is unknown to it and is not listed. After a session reset the old jobs keep running, hidden, and still count toward the cap; the table closes them with the session. A finished job of an older generation is removed when a newer caller next uses the table, because nobody can read it any more.

## Notices

- A job that ends while no tool call waits for it, that the model did not stop, and while the table is not closing sends one notice through its owner. The Agent delivers it at the next turn boundary of a running run, or starts a run on an idle Agent. The text names the job and its status line and tells the model to read it with `job_output`. A job that has ended stays in the table until the model reads its final status.
- A job whose end was reported by a waiting `bash` or `job_output` call, or that the model stopped with `job_kill`, sends no notice.
  A foreground `bash` call starts its job with `StartSpec.Foreground`: the wait of the call is registered before the process can end, so a command that ends before `Wait` runs still gets no notice.
  If the wait budget ends before the job ends, the completion notice is still sent.
- A refusal that `Options.IsExpectedDrop` accepts (a reset conversation or a disposed Agent) mutes the job: no report and no more monitor events. Any other refusal, or a job with no owner, goes to `Options.OnError`. The job stays readable with `job_list` and `job_output`.
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
Events go out through the owner; a still-queued batch is replaced with `Owner.Remove` so one message holds the merged lines.
The event lines are escaped (`&`, `<`, `>`), so command output cannot close the `monitor-event` element or the system reminder around it.
The merged output keeps the latest 3,000 characters and adds `[... more lines cut]` when older output is removed.

## Cleanup

- `Close` sends SIGTERM to every live group, waits one grace (3 s), then sends SIGKILL to what is left, waits for the groups and removes the spill directory. It sends no notices.
- `KillRemaining` sends SIGKILL to every live group at once, marks the table closed, and removes the spill directory without waiting for the owners.
  `ask -p` calls it at exit, including when the signal grace ends before Agent disposal finishes.
  A cleanup error goes to `Options.OnError`.
- No registry is kept on disk. A job of a leader that crashes is an orphan; nothing sweeps it.
- The spill directory (mode 0700, files mode 0600) is created at the first spill and holds `<job-id>.log`.

## Imports

A strict depguard list in `.golangci.yml` (`jobs-minimal-imports`) enforces the rule below.

## File names

`table.go`, `read.go`, `notice.go`, `monitor.go`, `doc.go`

## Imports

- Allowed: standard library, `internal/sandbox`, `pkg/protocol`
- Denied: `internal/agent`; transport packages, `acp`, `leader`, `config` (see `.golangci.yml`)

## Rules

- Never hold the table lock while you call the owner or wait for a process.
- Inject time through `sandbox.Clock`; tests use a fake clock.
- A test that starts a process checks that its group is gone (`kill(-pgid, 0)` returns `ESRCH`). The package runs `goleak.VerifyTestMain`.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
