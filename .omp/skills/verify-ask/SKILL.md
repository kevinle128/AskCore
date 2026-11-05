---
name: verify-ask
description: Drive the real `ask` binary of AskCore (Go agent harness) the way a user does and capture proof. Covers headless `ask -p`, project trust and `ask settings show`, the interactive TUI over a PTY, `ask leader` and `ask connect`, and `ask acp` over stdio, all on the offline faux provider. Use before claiming a change to cmd/, internal/ or the CLI behavior works.
---

# Verify AskCore (`ask`)

Surfaces: a CLI (`ask -p`, `ask settings`, `ask leader`, `ask connect`, `ask acp`) and an interactive TUI on the alternate screen. Everything here uses the `faux` provider, so there is no network and no credential. Never put a real provider key in the environment of a run. The helper strips every `ASK_*` and credential-like variable of the caller and sets its own.

Faux script: `echo <text>` calls the echo tool and the model says `<text>` back, `fail <text>` ends in an assistant error (exit 1 in `-p`), any other text is said back.

Helper: `.omp/skills/verify-ask/helpers/askv.py` (run from the repo root; `-h` lists commands). Paths below are relative to the repo root.

## Launch

For a CLI there is no server to keep up. Launch is build once, then isolate each run.

```sh
S=.omp/skills/verify-ask/helpers/askv.py
python3 $S build                      # go build -o /tmp/askv/ask ./cmd/tui  (override with ASKV_BIN)
python3 $S new-run                    # prints JSON: run, home, work, evidence, bin
python3 $S new-run --gated            # same, plus work/.ask/settings.json (a trust-gated project)
```

The binary MUST be named `ask`: the TUI, `ask connect` and `ask leader` start the leader from a binary named `ask`. `go run ./cmd/tui` makes `tui` and the TUI then falls back to in-process, which is a different path.

Every run gets its own `ASK_HOME=/tmp/askv-<run>/home` (short, absolute, mode 0700; the leader socket path must stay short) and its own work folder `/tmp/askv-<run>/work`. Export both in your shell for the journey:

```sh
RUN=<run>; export ASK_HOME=/tmp/askv-$RUN/home; cd /tmp/askv-$RUN/work; ASK=/tmp/askv/ask
```

Never use `~/.ask`, and never drive a leader that this run did not start. Two runs side by side are safe because home, work folder, and leader socket are all per run.

Ready means: `doctor` prints `OK`. No home content exists before the first command; the first leader or TUI start creates `leader.sock`, `leader.lock`, `leader.log`, `model-cache.json`.

## Doctor

```sh
python3 $S doctor $RUN
```

Read-only. Fails when: the run dir or binary is missing, the binary build is not the `HEAD` revision (`+dirty` is fine, a stale binary is not), `ask settings show --json` does not resolve the run's home, or a leader already runs for this home. Warns when `tui-test` is missing (needed only for the TUI journey). Run it first whenever a result looks wrong; a stale binary is the usual cause.

## Drive

Pick the feature file in [features/README.md](features/README.md) and follow its recipe. Summary of the three harnesses:

- **Plain CLI.** Run `$ASK ...` in the run's work folder, record command, stdout, stderr and exit code (`{ echo "+ cmd"; cmd; echo "exit=$?"; } >> $EV/<feature>.txt 2>&1`).
- **ACP stdio.** `python3 $S acp $RUN [--text "echo hi"]` speaks JSON-RPC (`initialize`, `session/new`, `session/prompt`), writes `acp-transcript.jsonl` and prints a JSON verdict. ACP responses and `session/update` notifications interleave; do not pipe fixed lines with printf, since `session/prompt` needs the `sessionId` from `session/new`.
- **TUI.** Use the existing runner `e2e/tui/run.py` whenever the feature is covered by one of its cases (`ask-idle`, `ask-turn`, `ask-trust-prompt`, `ask-trust-prompt-no-leader`, `ask-in-process`, `ask-selector`, `ask-exit-*`, ...). It builds its own `ask` into `/tmp`, runs on real engines (Alacritty, Ghostty), owns its `ASK_HOME` per case, scans artifacts for secrets, and cleans up its own sessions. Needs `tui-test` 0.1.0-beta.2 on `PATH` (`tui-test --version`). Read `e2e/tui/README.md` for the case list. Use it instead of hand-rolled tmux. Hand-rolled drive is only for a behavior no case covers (see the tmux fallback in [features/tui-start.md](features/tui-start.md)).

  ```sh
  python3 e2e/tui/run.py --backend alacritty --case ask-idle --artifacts /tmp/ask-verify-evidence/$RUN/tui
  ```

  `--artifacts` must not exist yet. The runner has its own isolation and ignores your exported `ASK_HOME`.

## Evidence

Evidence goes to `/tmp/ask-verify-evidence/<run>/` (printed by `new-run`). Cleanup never touches it. Copy anything you need to keep longer.

Proof standard:

- Exercise the real user path: the shipped `ask` binary and its documented commands. No internal setters, no test-only endpoints, no editing of `trust.json` to fake a decision except where the feature file says the TUI writes it.
- Capture the action and the resulting state: the command and its output plus the second view (exit code, files in `ASK_HOME`, `ask leader status`, `ask settings show`).
- Verify side effects, not only text: `trust.json` content, `leader.sock` presence, leader process, `.ask/` files.
- Faux is the isolated production boundary. Do not claim anything about real providers, OAuth, or billing from these runs. `ask auth`, `ask models refresh` and `/providers` with real keys are out of scope.
- `ask -p` ignores `ASK_FAUX` and always has faux; `ask acp`, `ask connect` (through the leader) and the in-process TUI need `ASK_FAUX=1`. The leader inherits it from the client that starts it.
- `-a` / `--approve` is not a dry run: it trusts the project for that run and loads `.ask/settings.json` (proven by `thinkingFold` flipping to `project`). It saves nothing (`trust.json` does not appear).

## Cleanup

```sh
python3 $S cleanup $RUN
```

Runs `ask leader stop` for this run's home only, checks `ask leader status` reports no leader, then deletes `/tmp/askv-<run>`. It never kills by process name. It keeps the evidence directory. If it fails with a surviving leader, kill the PID that `ask leader status` printed for this home, nothing else. Run cleanup also after a failed journey. `e2e/tui/run.py` cleans up its own sessions and leaders.

## Helpers

| Command | Purpose |
|---|---|
| `python3 $S build` | Build `./cmd/tui` to `/tmp/askv/ask`. |
| `python3 $S new-run [--gated]` | Isolated home, work folder, evidence dir. Prints JSON. |
| `python3 $S doctor RUN` | Read-only health check. |
| `python3 $S acp RUN [--text T]` | ACP stdio journey with transcript. Exit 1 unless `stopReason` is `end_turn`, the process exits 0 and the text appears in the updates. |
| `python3 $S cleanup RUN` | Stop this run's leader, delete scratch, keep evidence. |
| `python3 e2e/tui/run.py --case C --backend B --artifacts DIR` | Existing TUI runner. |
| `python3 e2e/tui/test-assertions.py` | Controls for the TUI runner's own assertions. |
