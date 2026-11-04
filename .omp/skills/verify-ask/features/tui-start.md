# TUI start

`ask` with no arguments on a terminal opens the interactive screen on the alternate screen: a header, a block pane, a prompt between two rules, a footer row and a shortcut bar. It talks to a leader it starts, or runs the agent in process.

## Sub-features

- `tui-idle` the idle frame and `ctrl+d` exit.
- `tui-turn` a prompt that calls the echo tool, block folding, the reply.
- `tui-leader` the program runs through a leader that holds the session.
- `tui-in-process` `--no-leader` or a symlinked home runs in process and starts no leader.
- `tui-exit` `/quit`, `ctrl+c` twice, SIGTERM and SIGHUP restore the terminal and leave `ask: session <id>`.
- `tui-resize`, `tui-selector`, `tui-providers`, `tui-mouse`, `tui-copy` other covered screens.

## How to get to it (user POV)

- Run `ask` in a terminal.
- Run `ask --no-leader` to stay in process.
- Inside: `/model`, `/providers`, `/new`, `/sessions`, `/trust`, `/copy`, `/quit`.

## Driving it with e2e/tui/run.py (preferred) or tmux

Preconditions: Python 3, Go, `tui-test` 0.1.0-beta.2 on `PATH` (`tui-test --version`). Run from the repo root. The runner builds its own binary, owns `ASK_HOME` per case and cleans up its own sessions, so it needs no `new-run`. Use a fresh evidence path.

- **Idle.** Run `python3 e2e/tui/run.py --backend alacritty --case ask-idle --artifacts /tmp/ask-verify-evidence/<run>/tui`. Output ends `alacritty/ask-idle: pass` and `Artifacts: ...`. Row 1 is ` ◢◤ ask`, the last row is the shortcut bar, `ctrl+d` ends it.
- **Turn.** Same with `--case ask-turn`. Proves the echo tool block, the reply row, `Tab`/`k`/`e` folding.
- **Leader versus in process.** `--case ask-in-process` and `--case ask-trust-prompt-no-leader`. They assert no leader process and no `leader.sock` in the case home. Every other case asserts a leader that holds a session.
- **Exit.** `--case ask-exit-quit --case ask-exit-ctrl-c --case ask-exit-sigterm --case ask-exit-sighup`.
- **All cases, both engines.** `python3 e2e/tui/run.py` (default Alacritty and Ghostty). Use `--backend` to narrow.
- **Proof.** Per case `results.json`, `*-state.json`, `*.svg`, `output.cast`, `commands.json`, `ownership.json` under the artifacts path. Open an `.svg` or the final screen text to see the frame.
- **Tmux fallback (not exercised in this skill's own proof).** Only for a behavior no case covers. `tmux new-session -d -s askv-$RUN -x 80 -y 24 "env -i HOME=$HOME PATH=$PATH TERM=xterm-256color ASK_HOME=$ASK_HOME ASK_FAUX=1 /tmp/askv/ask"`, then `tmux send-keys -t askv-$RUN "echo hi" Enter`, `tmux capture-pane -p -t askv-$RUN`, and finish with `tmux kill-session -t askv-$RUN` plus `$ASK leader stop`. Run it in the run work folder of a `new-run`. Never kill tmux by pattern.

## Gotchas

- Do not use Python `-O`: assertions would be disabled.
- `--artifacts` must not exist yet. A rerun needs a new path.
- The runner fails when product files (`cmd`, `internal`, `pkg`, `proto`, `migrations`, ...) change between its start and end. Do not edit code during a run.
- The screen shows no leader state. Learn it from `ask leader status` and the process table, as the runner does.
- A mouse report right after a repaint can be lost on the engines; the runner waits for a settled screen first. Copy that approach in any hand-rolled drive.
- The runner drops all `ASK_*` and credential-like variables from the caller. Do not rely on `ASK_FAUX_TPS` from your shell.
- `go run ./cmd/tui` makes a binary named `tui` and forces the in-process path.
