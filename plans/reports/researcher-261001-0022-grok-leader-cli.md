# Grok Build "leader" CLI model, distilled for AskCore

Date: 2026-10-01. Grok Build version observed: 1.0.44 (`~/.grok/version.json`, `stable_version` 1.0.44).
Method: docs + `--help` text + read-only `grok leader list`. No leader was started or killed. No forbidden file was read.

## 0. Outcome first

1. Grok's leader is **optional and off by default**. The default Grok path is an in-process agent inside the same binary that draws the TUI. The leader is a shared-backend mode, not the base architecture.
2. The docs say almost nothing about lifecycle. Everything about spawn, discovery, stop and the wire protocol is either in help text (strong) or inference (marked below).
3. Grok can start a leader from the same binary. AskCore cannot copy that part directly, because `cmd/tui` must not import `internal/*`. AskCore already has the leader as a separate binary: `cmd/server`.
4. Recommendation: use **C now (`cmd/server -p`, as roadmap D5 already decided), then grow into B at H13** (the `ask` client attaches to `cmd/server` over a 0600 Unix socket plus the H13 token rules). Auto-spawn (D) is a later opt-in, never the default. Details in section 6.

## 1. Sources and credibility

| Source | Type | Weight |
|---|---|---|
| `grok --help`, `grok leader --help`, `grok leader list/info/kill --help`, `grok agent --help`, `grok agent leader --help` | Binary's own help text | High (authoritative for flags, weak for behavior) |
| `~/.grok/docs/user-guide/*.md` | Vendor docs | High, but leader is mentioned in only 6 places |
| `~/.grok/README.md` | Vendor docs | Same text as the guides for the leader parts |
| `grok leader list` output, file listing | Observation | High for "no leader now", nothing more |

Closed source: there is no code or production write-up. No version history for the leader exists in these docs (Ring 5: see section 5).

## 2. Ring 0: what leader mode is and why it exists

- Help text of `grok agent leader`: "Run as the shared leader process for other clients". Help text of `--leader`: "Connect to a shared leader process instead of starting a new agent. Allows multiple clients to share one backend. Defaults to [cli] use_leader in config.toml" (`grok agent --help`).
- Config doc: `cli.use_leader` = "Use the leader process for config reload and MCP watches" (`26-config-reference.md:113`). This is the one stated reason: one process does config reload and MCP watching instead of each terminal doing it.
- Other stated roles of the leader:
  - It can register as a Cursor private worker so Cursor Cloud Agents run against this machine (`26-config-reference.md:135-144`, `grok agent leader --cursor-worker`). This needs a long-lived process that outlives one terminal.
  - It can hold the grok.com relay WebSocket for headless IPC clients (`grok agent leader --relay-on-demand`).
- Inference: the real driver is "many clients, one backend": shared MCP connections, shared file watchers, one relay connection, one place for background work. Sharing sessions across terminals is not claimed anywhere.

## 3. Ring 1: lifecycle

| Question | Evidence | Status |
|---|---|---|
| Who starts the leader? | `--relay-on-demand` is "Passed by leaders auto-spawned from interactive clients (TUI/IDE)" (`grok agent leader --help`). `grok agent leader` is also a manual subcommand. | Fact: both auto-spawn by an interactive client and explicit start exist. Inference: auto-spawn happens only when leader mode is on (`--leader` or `use_leader`). |
| How do others find it? | `--leader-socket <PATH>`: "custom leader socket path instead of the default `~/.grok/leader.sock`" (`grok --help`). | Fact. The socket is a Unix domain socket by name (inference: the protocol on it is not documented). |
| What happens with no leader? | `--no-leader`: "Start a new agent even when config enables leader mode". `grok leader list` prints "No leader candidates found." and exits with no side effect (observed). | Fact for `--no-leader` wording. Inference: with leader mode on and no leader, the client spawns one (that is what "auto-spawned" means). With leader mode off, nothing ever looks for a socket. |
| How does it stop? | `grok agent leader --no-exit-on-disconnect`: "Keep the leader running after the last client disconnects". `grok leader kill`: "Stop all running leader processes". | Fact: the default is to **exit when the last client disconnects** (no idle timer is documented). `--no-exit-on-disconnect` turns that off. |
| What runs in the leader? | Stated: config reload, MCP watches (`26-config-reference.md:113`), relay WebSocket, Cursor worker door, auth retry at startup (`02-authentication.md:224-231`), periodic auto-update checks (`--no-auto-update` on `grok agent leader`). Agent proxy snoops the `model` session option to keep each client's `default_model` in sync (`15-agent-mode.md:193`). | Fact for each item. The docs never list model calls or tools as leader-owned, except the sandbox rule below. |
| Do tools run in the leader? | Sandbox doc: with a sandbox profile the agent "runs in-process, not through the shared leader, so tool calls stay in this process"; the leader is refused "so tools are not delegated elsewhere" (`18-sandbox.md:178-179`, `15-agent-mode.md:70`). | Fact, and an important inference: in leader mode **tool calls are delegated to the leader process**. The agent, sessions and tools live in the leader. |
| What stays in the client? | The TUI (rendering, input). The client is "the proxy" (`15-agent-mode.md:193`: "the proxy snoops `configId: model`"). | Fact for the word "proxy". Inference: the client speaks ACP (JSON-RPC) through the socket. The ACP agent doc says "Agent options apply to every transport (`stdio`, `serve`, `headless`, `leader`)" (`15-agent-mode.md:57`), so `leader` is a fourth ACP transport. |
| Do `grok -p` and the TUI use it? | `grok agent --leader` exists for the ACP path. The headless doc (`14-headless-mode.md:650`) says SDKs "inject `GROK_DISABLE_AUTOUPDATER=1` for the non-leader agents they spawn". No text says `-p` attaches to a leader. | Not documented. Inference: `-p` runs in-process by default, like every Grok path while `use_leader` is off. |

Startup metrics name the steps of a connect: `config_load`, `managed_policy`, `bootstrap`, `model_catalog`, `worker_spawn`, `leader_connect`, `acp_initialize`, `eager_auth` (`24-monitoring-usage.md:248`). Inference from the order: `worker_spawn` then `leader_connect` then `acp_initialize` means the client first spawns the worker/leader if absent, connects to its socket, then does the ACP `initialize` handshake. Only the step names are fact.

## 4. Ring 2: surface

| Item | Value | Source |
|---|---|---|
| Enable | `--leader` flag, or `[cli] use_leader = true`. Off by default. | `02-authentication.md:224`, `grok agent --help` |
| Disable | `--no-leader` | `grok agent --help` |
| Socket | `~/.grok/leader.sock`; override `--leader-socket <PATH>` (global on `grok`, `grok leader *`, `grok agent leader`) | `grok --help` |
| Isolated builds | Name the socket `~/.grok/leader-*.sock` so `grok leader list/kill` still find it. Other paths work but are not auto-discovered. | `grok --help` |
| Discovery | `grok leader list` (`--json`), `grok leader info [--pid PID] [--json]`, `grok leader kill` (all leaders) | help text |
| Leader flags | `--no-exit-on-disconnect`, `--relay-on-demand`, `--no-auto-update`, `--cursor-worker*` | `grok agent leader --help` |
| Log | `~/.grok/leader.log` (background auth attempt stderr goes here) | `02-authentication.md:231` |
| Plugins | `--plugin-dir` is "ignored in leader mode, where the shared leader discovers its own plugins" | `09-plugins.md:427` |
| Session registry | `cli.session_registry`: "Participate in the cross-process session registry" (related; not stated to be leader-only) | `26-config-reference.md:111` |
| Env vars | No leader-specific env var found in the docs. | grep of all guides |

Observed on disk (names only): `active_sessions.json` is `[]`, `active_sessions.lock` is 0 bytes, `auth.json.lock` is 16 bytes. No `leader.sock` or `leader.log` exists now, which matches "off by default". Inference: `active_sessions.json` plus its lock is a file-based cross-process registry that works without a leader.

## 5. Rings 3 to 5: interactions, edge cases, history

What the docs cover:

- **Auth (Ring 3/4).** The one documented edge: leader mode, no credential at all. The leader makes one extra background attempt right after startup, with `GROK_AUTH_EXPIRED` unset, like a sign-in. A binary that can mint without help succeeds and "the session heals itself". One that must prompt "just sits, up to the 300s sign-in ceiling"; nothing waits on it, the sign-in screen is already up, and its stderr goes to `~/.grok/leader.log` not to the user (`02-authentication.md:224-231`, README.md:304). Lesson: a background process has no terminal, so its errors need a log file and its prompts need a foreground owner.
- **Sandbox (Ring 3/4).** Sandbox requested means no leader, even if the profile fails to apply; one startup line says so (`18-sandbox.md:178-179`). Lesson: a security boundary wins over the shared-process convenience, and the refusal is loud.
- **Plugins (Ring 3).** Per-process plugin dirs are ignored under a leader (`09-plugins.md:427`).
- **Model option (Ring 3).** The client-side proxy snoops the `model` option so each client's `default_model` stays in sync; option updates are broadcast to every subscribed client via `config_option_update` (`15-agent-mode.md:193`). Lesson: shared backend means shared mutable state, and someone must broadcast changes.
- **Stale-client hint (Ring 3/4 upgrade).** A blank status-line row means "a `grok` or leader process older than this client. Restart the leader or update Grok" (`25-status-line.md:139`). This is the only upgrade text. It shows that **a leader can be older than its client**, there is no documented version handshake, and the remedy is manual (`grok leader kill`). Inference: the client does not auto-restart a stale leader.
- **Dashboard and background tasks (Ring 3).** `23-dashboard.md` and `20-background-tasks.md` contain no "leader" mention. They do not say whether background tasks survive the client. Not documented.
- **Worktrees and `cursor-worker` (Ring 3).** Worktrees: not linked to the leader in any doc. `cursor-worker`: a leader-only feature (section 4).
- **Ring 4 not documented at all:** stale socket file after a crash, socket permissions, two leader versions at once, what error the user sees when the socket is dead. `grok leader list` says "candidates", which suggests it probes sockets and may list dead ones (inference, unverified). I did not test a stale socket because I was told not to start a leader.
- **Ring 5 history:** no leader version notes in the README or the guides. The only date-like signals are the `[cli] use_leader` key being `pin` (managed-config pinnable) and the feature being gated by `cursor_worker` "requires a build with Cursor worker support". I cannot say when the leader was added or what changed.

## 6. Distillation for AskCore

### 6.1 The leader model mapped onto AskCore

| Grok concept | AskCore mapping | Note |
|---|---|---|
| Leader process | `cmd/server` (the daemon) | Grok has one binary that is both. AskCore has two, so the client/leader split is already enforced by the import rule. This is better than Grok for H13. |
| Client (TUI, `-p`) | `cmd/tui` (`ask`) | Imports no `internal/*`. Speaks only `pkg/protocol`. |
| `~/.grok/leader.sock` | `~/.ask/server.sock` (name it `server`, not `leader`; AskCore has no "non-leader" mode) | Mode 0600, directory 0700. Path override flag and env var, for isolated test builds. |
| `grok leader list/info/kill` | `ask server status/stop` (later) | Keep it small: one socket, one pid file. |
| `use_leader`, off by default | `[cli] autostart_server`, off by default, added only when auto-spawn (option D) is built | Grok's default is off; copy that. |
| Who owns the harness? | The daemon. Always. | Agent, sessions, tools, provider calls, credentials all live in `cmd/server`. The client renders and sends commands. This matches Grok's leader mode (tools run in the leader). |
| Sandbox refuses the leader | Not needed. The daemon is where the sandbox lives. | Grok needs the carve-out only because it has an in-process path. |

### 6.2 Options for "where does the harness run when the user types `ask -p`"

Roadmap facts used: H2 exit is `-p "hello"` and `--mode json` via `cmd/server` (D5 = A, recommended; the user also wants `ask -p` in `cmd/tui`). H2 test: "Print mode opens no network listener". H8 makes sessions a SQLite tree with a cross-process lease (H-SESS-15). H13 brings gateway auth. T1 (the real TUI) comes after H13.

| Option | Learning | Confirmed rules | Security | Fit to H13 |
|---|---|---|---|---|
| **A. In-process harness in `cmd/tui`** | Fastest to run. Teaches nothing about the client/server split. | Breaks "cmd/tui imports no internal/*". To keep the rule you would have to move the harness to `pkg/`, which breaks the dewee `internal/` layout. | Best: no socket, no port. Tools run with the user's rights in the user's own process. | Worst. Throw-away code: H13 then needs a second path (remote) and `-p` has two behaviors. |
| **B. `cmd/tui` talks to `cmd/server` over a minimal Unix socket built in H2** | Teaches the protocol early. Risk: designing the wire before the loop is understood. | Keeps all rules. Breaks the H2 test "no listener" unless the test is reworded to "no TCP/HTTP/gRPC listener; only the Unix socket". Adds work to H2 that D5 did not plan. | Good: 0600 socket + 0700 dir gives same-user access only, without tokens. Does not stop other processes of the same user. | Good, if the envelope is the H1 `pkg/protocol` envelope. Wire names are fixed early (timeline lesson 15), so a wrong guess costs a rename. |
| **C. `cmd/server -p` until T1** (current D5 = A) | Teaches the loop, not the client. Zero extra code. | Keeps all rules. H2 test holds: print mode is in-process in the server binary and opens no listener. | Best now: no socket. Same as A. | Clean: H13 adds the listener and `ask -p` as a client; `cmd/server -p` stays as the dev/test path. No throw-away code. Weakness: `ask -p` does not exist yet, which does not match the user's wish until H13. |
| **D. Grok-style: first `ask` auto-spawns the daemon, later clients attach** | Teaches process lifecycle, which is more than the stated goal "learn the harness". | Keeps the rules, but adds a hidden background process. Conflicts with `process-management.md` (orphan processes) unless exit-on-last-disconnect and a pid file are built. | Worst by default: a daemon with bash tool access is started without the user asking. Needs the same-user 0600 socket plus the H13 token. | Natural end state for T1 (users type `ask`, not `ask-server`), but only as opt-in. |

Ranking for now: **C, then B at H13, then D as opt-in in T1, then A (reject).**

Trade-off summary: C is the only option that keeps every confirmed rule, adds no code and leaves the wire design until the loop is understood. B pulls forward H13 work and risks renaming the protocol. D is correct for the final user experience and wrong as the default. A is cheap and contradicts a confirmed rule.

### 6.3 What the socket protocol needs in H2 (if B is chosen) and how it grows into H13

Minimum (four capabilities, matches the request):

| Need | Shape | Why |
|---|---|---|
| Version handshake | First frame in both directions: `{protocol: N, build: "..."}`. Server answers with its own version and closes on mismatch with an error frame that says which side must upgrade. | Grok has no documented handshake; its only upgrade symptom is a blank status row (`25-status-line.md:139`). That is the failure to avoid. |
| Prompt | Request `prompt{sessionId?, text}`, reply is an ack with `runId`. | Matches Pi RPC `prompt`. |
| Event stream | Server pushes H1 envelope frames (`seq`, `ts`, `sessionId`, `runId`, `type`). Client may send `after=<seq>` on attach. | The envelope is already in H1. Resume by `seq` is an H13 exit requirement, so build the field now even if replay is not yet used. |
| Abort | `abort{runId}`. Maps to `context.Context` cancel (H-LOOP-08). | Must work before any other control command. |

Framing: newline-delimited JSON, one frame per line, no line limit (roadmap E§24#29), stdlib only. Inference: Grok uses ACP JSON-RPC on this socket; AskCore does not need JSON-RPC here. Plain typed frames are enough, but keep an `id` on requests so errors can be correlated.

Growth into H13:
1. Same frames become the WS and gRPC payloads. The Unix socket stays as the local transport. No second protocol.
2. Add auth. On the Unix socket the filesystem permission is the credential (0600 file, 0700 directory, optional peer-UID check). On TCP/WS the H13 client token (0600 file) or mTLS applies. The Unix socket must not become a way around the token for remote access: it is local by nature.
3. Add `steer`, `follow_up`, `compact`, `new_session`, `set_model`, `get_state` (H9 to H13).
4. Add multi-client rules: one writer per session (H-SESS-15 lease), stale-frame fencing after re-attach, broadcast of shared-state changes (the model option lesson from Grok).

## 7. Recommendation

1. **Now to H12: option C.** Keep D5 = A. `ask -p` is not needed to learn the harness. `cmd/tui` stays a stub.
2. **H13: make `ask` a client of the daemon (option B, done once, properly).**
   - Add the Unix socket (0600, dir 0700) as a second transport next to loopback WS/gRPC, with the four H2-minimum frames plus the H13 commands.
   - `ask -p "..."` connects, sends `prompt`, prints events, exits with the run's exit code. If the daemon is not running, print one clear line: `ask server not running; start it with: ask-server`.
   - Re-word the H2 test now to "no TCP/HTTP/gRPC listener" so it survives this change.
3. **T1 or later: optional auto-spawn (option D), opt-in.** Copy Grok's behavior exactly: off by default; exit when the last client disconnects unless told otherwise; own log file `~/.ask/server.log`; socket name override for test builds; a pid file so `ask server stop` can find it; refuse auto-spawn when a sandbox is requested.
4. Copy these Grok decisions: off by default; a log file for background output; isolated socket paths for dev builds; exit on last disconnect.
5. Do not copy: the missing version handshake; undocumented stale-socket behavior (define it: on connect failure with `ECONNREFUSED` remove the stale socket, then retry once, only if the pid file points to a dead process).

Adoption risk: Grok's leader is undocumented in depth and closed, so nothing here can be verified against code. Treat it as a pattern, not a spec. Risk of B: the protocol becomes a public contract at H13; mitigate with the version field.

## 8. Limitations

- No leader was run, so Ring 4 (stale socket, crash, two versions, permission errors, error text) is entirely unobserved.
- I did not confirm whether `grok -p` or the TUI attach when `use_leader` is on; docs are silent.
- No changelog was found. Ring 5 is empty.
- The wire format on `leader.sock` is unknown; "ACP over the socket" is inference from the `leader` transport listed beside `stdio/serve/headless`.

## 9. Unresolved questions

1. Does the user accept C until H13 even though `ask -p` will not exist before then? (Current roadmap D5 says yes; the user's new wish says `cmd/tui` is the CLI.) If not, choose B and move the Unix socket into H2.
2. Socket name and directory: `~/.ask/server.sock` or `~/.ask/leader.sock`? This follows D7 (Ask names).
3. Should `ask -p` without a running daemon fail with a hint (recommended) or auto-spawn once (option D on by default)?
4. Is same-user 0600 enough for the local socket, or should the token be required on the socket too? (A local malicious process of the same user can already read `~/.ask/auth.json`, so a token adds little; this is an inference and a product choice.)

Status: DONE_WITH_CONCERNS
Summary: Grok's leader is opt-in (off by default), auto-spawned by interactive clients, exits when the last client leaves, and runs tools in the leader; recommendation is C now, B at H13, D as opt-in later.
Concerns: Ring 4 and Ring 5 are undocumented and unobserved (no leader was run); much of the lifecycle is inference from help text.
