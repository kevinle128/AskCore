# Leader and connect

One local leader serves all clients of an `ASK_HOME` on `<ASK_HOME>/leader.sock`. `ask connect` is a line client that sends prompts and prints one line per event. `ask leader` reports and stops it.

## Sub-features

- `leader-absent` `status` reports no leader and exits 1, while `list` and `stop` exit 0.
- `connect-turn` a prompt over stdin runs on the leader and ends with `result ... stop=end_turn`.
- `connect-start` `connect` starts the leader when none runs.
- `leader-status` `status` and `status --json` show pid, build, clients, sessions.
- `connect-sessions` `/new` and `/sessions` list live sessions; leaving only detaches.
- `leader-stop` `stop` ends the leader and removes the socket.
- `connect-error` a bad `--cwd` returns an `error` line.

## How to get to it (user POV)

- `ask leader status|list|stop` in a terminal.
- `printf 'echo hi\n' | ask connect` or interactive `ask connect [--cwd DIR | --attach SESSION]`.
- The TUI starts the same leader on its own.

## Driving it with askv.py and plain shell

Preconditions: a fresh `new-run`, `doctor` printed `OK` (it fails if a leader already runs), env exported, `$ASK` is named `ask`. Export `ASK_FAUX=1` for the `connect` calls: the leader that `connect` spawns inherits it, which lists the faux provider.

- **Absent.** `$ASK leader status` prints `no leader is running`, exit `1`. `$ASK leader list` and `$ASK leader stop` exit `0`.
- **Turn and start.** `printf 'echo conn-ok\n' | ASK_FAUX=1 $ASK connect`. Lines in order: `connected instance=... build=... protocol=1`, `session id=sess_... role=driver cwd=...`, `update ... kind=tool_call`, `update ... kind=agent_message_chunk text="conn-ok"`, `result session=... stop=end_turn`, `bye`. Exit `0`.
- **Leader survives the client.** `$ASK leader status` now shows `leader:            running`, `spawned by client: yes`, `sessions:          1`. `ls $ASK_HOME` has `leader.sock`, `leader.lock`, `leader.log`.
- **Sessions.** `printf '/new\n/sessions\n' | ASK_FAUX=1 $ASK connect` shows `sessions count=` rising and a `live id=` row per session (earlier sessions stay live after their client left).
- **JSON.** `$ASK leader status --json` has `instanceId`, `build`, `protocolVersion`, `pid`, `clients`, `sessions`, `activeRuns`, `spawnedByClient`.
- **Stop.** `$ASK leader stop` prints `leader stopped`, exit `0`. `ls $ASK_HOME` no longer has `leader.sock`; `$ASK leader status` exits `1` again.
- **Proof.** Save each command with output and exit code to `$EV/leader-connect.txt`, and the `ls $ASK_HOME` before and after stop. `askv.py cleanup` then also proves the stop path: it exits non-zero if the leader survives.

## Gotchas

- A leader must come from a binary named `ask`; a spawned leader is found next to the caller or on `PATH`. Use the binary from `askv.py build`.
- Without `ASK_FAUX=1`, `connect` still starts a leader and creates a session, but the prompt fails: observed `error session=sess_... op=prompt code=-32000 kind=no_api_key` for `echo x`, exit `0`. Report that as an environment error, not a product failure.
- `/new` in `connect` opens sessions that stay live in the leader after the client leaves. Count them with `leader status`.
- The leader socket path must be short: keep `ASK_HOME` directly under `/tmp`.
- A newer client build replaces an older client-started leader and ends its sessions. Do not mix binaries on one run home.
- The leader log (`leader.log`) rotates at 8 MiB and is a useful second view after a failure.
