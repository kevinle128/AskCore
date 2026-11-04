# ACP stdio

`ask acp` serves the Agent Client Protocol (v1) as JSON-RPC lines on stdin and stdout, the way an editor drives it. It opens no listener and no database.

## Sub-features

- `acp-init` `initialize` returns agent info and capabilities.
- `acp-order` any method before `initialize` is refused.
- `acp-session` `session/new` returns a `sessionId`.
- `acp-prompt` `session/prompt` streams `session/update` notifications and ends with `stopReason`.
- `acp-eof` closing stdin ends the process with exit 0.
- `acp-flags` `--approve` and `--no-approve` together exit 1.

## How to get to it (user POV)

- An editor spawns `ask acp` and speaks JSON-RPC lines on its stdio.
- A developer pipes JSON lines into `ask acp`.

## Driving it with askv.py

Preconditions: a fresh `new-run`, `doctor` printed `OK`, `RUN` set. The helper sets `ASK_FAUX=1` and a work folder cwd for the child.

- **Whole journey.** Run `python3 $S acp $RUN --text "echo hi"`. It sends `initialize` (`protocolVersion` 1), `session/new` (`cwd`, `mcpServers: []`) and `session/prompt`, then closes stdin. It prints one JSON line, expected `{"stopReason": "end_turn", "exit": 0, "update_kinds": ["agent_message_chunk", "tool_call", "tool_call_update"], "updates": 5, "ok": true}` and exits `0`.
- **Transcript.** `/tmp/ask-verify-evidence/$RUN/acp-transcript.jsonl` has every line in both directions (`dir` is `out` or `in`). The `initialize` result has `agentInfo.name` `ask` and `sessionCapabilities.close`; the prompt result has `stopReason`.
- **Order check.** `printf '{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"'$PWD'","mcpServers":[]}}\n' | ASK_FAUX=1 $ASK acp`. The answer is error `-32600`, `initialize the connection first`, `data.kind` `not_initialized`. Exit `0` at end of input.
- **Flags.** `$ASK acp --approve --no-approve` exits `1`.
- **Failure path.** `python3 $S acp $RUN --text "fail boom"` prints `{"stopReason": null, ... "ok": false}` and exits `1` (the `session/prompt` answer is not a result with `end_turn`; read the `in` entry with `id` 3 in the transcript for the error). The helper is correct to fail it; do not edit it to pass.
- **Proof.** The transcript file plus the helper's verdict line; both stay after cleanup.

## Gotchas

- Responses and `session/update` notifications interleave. Match by `id`, never by line position (the helper does).
- Stdout is protocol only. Diagnostics such as `msg="connection closed"` go to stderr; keep them separate with `2>$EV/acp.stderr`.
- Without `ASK_FAUX=1` the session has no usable provider and the credential-free path fails. The helper sets it.
- Sign-in is not part of the connection. `authMethods` in the `initialize` result only advertises host-side `ask auth login`.
- The exact JSON-RPC error body of the `fail boom` prompt was not inspected when this map was written, only that the helper reports `ok: false`.
- `session/new` accepts `_meta["ask.dev/trust"]` of `approve` or `deny`; the helper does not use it, so trust over ACP is only covered by `ask acp --approve` flags (not proven here).
