# Headless prompt

`ask -p` runs one or more prompts on the faux provider, prints the final reply and exits with a code that reflects the reply. `--mode json` writes one JSON event per line.

## Sub-features

- `print-echo` the faux `echo` tool runs and the reply is printed.
- `print-fail` an assistant error exits 1 and prints the error text.
- `print-json` `--mode json` emits an ordered event stream and exits 0 even on an assistant error.
- `print-input` `@file`, piped stdin and several messages feed the run.
- `print-limit` `--max-turns` stops the run with a visible error.
- `print-args` bad arguments exit 1.

## How to get to it (user POV)

- Run `ask -p "<message>"` in a terminal.
- Pipe text: `echo "echo x" | ask -p`.
- Pass a file: `ask -p @file.txt "<message>"`.
- Run `ask --mode json "<message>"`.

## Driving it with askv.py and plain shell

Preconditions: a run from `askv.py new-run` (no `--gated`, so there is no trust warning), `doctor` printed `OK`, `ASK_HOME`, `ASK` and `EV` exported, cwd is the run work folder.

- **Echo.** Run `$ASK -p "echo hello-faux"`. Stdout is `hello-faux`, exit `0`. `ls $ASK_HOME` is empty: print mode starts no leader and writes no file.
- **Fail.** Run `$ASK -p "fail boom"`. Stdout is `boom`, exit `1`.
- **JSON.** Run `$ASK --mode json "echo x"`. Every stdout line parses as JSON with `seq`, `sessionId` and `type`; the first type is `agent_start`, `seq` rises by one. With `"fail boom"` the exit is `0`.
- **File.** Run `echo note-body > n.txt; $ASK -p @n.txt "echo got"`. Stdout is `echo got` and exit is `0`. The faux model echoes only the instruction, so the file content is not visible in stdout; a missing file (`@nope.txt`) is the observable error path (not exercised here).
- **Stdin.** Run `echo "echo piped" | $ASK -p`. Stdout is `piped`.
- **Several messages.** Run `$ASK -p "echo a" "echo b"`. Both run in order; stdout shows only the reply of the last prompt, `b`.
- **Max turns.** Run `$ASK -p --max-turns 1 "echo a" >o.txt 2>&1; echo $?`. `o.txt` is `Error: Reached max turns (1)` and the exit is `1`.
- **Bad args.** `$ASK -p -a -na x` prints `Error: use only one of --approve and --no-approve`, exit `1`. `$ASK -p --mode bogus x` prints `Invalid mode "bogus"`, exit `1`.
- **Proof.** Save each command with stdout, stderr and exit code to `$EV/headless-print.txt`, plus one `--mode json` output to `$EV/print.jsonl`.

## Gotchas

- In a gated work folder `-p` writes the trust warning on stderr. Do not match stdout against stderr merged with `2>&1`; use `2>/dev/null` or a separate file.
- `-p` ignores `ASK_FAUX` and always has faux when no `defaultModel` is saved. A saved `defaultModel` in `$ASK_HOME/settings.json` with no credential makes `-p` warn and fall back to faux, which is also correct.
- Piped stdin or a non-terminal stdout also selects print mode: running bare `ask` in a script does not open the TUI.
- A pipe such as `| tail` hides the exit code of `ask`. Redirect to a file and read `$?`, or use `${PIPESTATUS[0]}`.
- Several messages print only the reply of the last prompt.
