# Ask verification map

This directory is the maintained source for verifying the user-facing behavior of `ask`. Read this index, then the matching feature file, before driving the app.

## Baseline preconditions

- `python3 .omp/skills/verify-ask/helpers/askv.py build`, then `new-run` (add `--gated` where a file says so), then `doctor <run>` printing `OK`.
- In the shell: `export ASK_HOME=/tmp/askv-<run>/home; cd /tmp/askv-<run>/work; ASK=/tmp/askv/ask; EV=/tmp/ask-verify-evidence/<run>`.
- The provider is `faux`. No credential variable is set.
- Never drive an instance this run did not start. Never touch `~/.ask`.

## Driving conventions

- Treat every command as literal. Keep quoted text and flags unchanged.
- Record each command with its stdout, stderr and exit code into `$EV/<feature>.txt`.
- Prefer stable text handles: the screen strings the runner checks, `ask leader status` fields, JSON keys of `ask settings show --json` and the ACP `stopReason`.
- Start every recipe from a fresh run unless the preconditions say otherwise. Cleanup with `askv.py cleanup <run>` keeps the evidence.

## Proof and skip reporting

- CLI proof is the command, stdout, stderr and exit code, plus one second view of the stored state.
- TUI proof is the runner's `results.json`, the engine screen text and the `.cast` recording.
- Report an unreachable path with the attempted command and the unmet precondition. Do not report a skipped entry point as verified through another path.

## Feature entry contract

Each file has an H1, one paragraph, and the four H2s `Sub-features`, `How to get to it (user POV)`, `Driving it with <harness>`, `Gotchas`.

## Features

- [Headless prompt](./headless-print.md): `ask -p` and `--mode json` on faux tools, exit codes, `@file`, stdin.
- [Trust and settings](./trust-settings.md): the project trust gate, `ask settings show`, `--approve` and `--no-approve`.
- [TUI start](./tui-start.md): the interactive screen, a turn, exit, in-process fallback, via the existing PTY runner.
- [Leader and connect](./leader-connect.md): `ask leader status|list|stop` and the line client `ask connect`.
- [ACP stdio](./acp-stdio.md): `ask acp` JSON-RPC session on stdin and stdout.
