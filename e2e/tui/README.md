# Headless TUI checks

The runner starts the built `ask` in owned tui-test sessions on real terminal engines (Alacritty and Ghostty) and checks the engine screen and the output recording. The interactive `ask` TUI uses the alternate screen, so every check reads one full frame: a header row, a block pane, a prompt pane between two rules, and a shortcut bar. The screen design is in the [alternate-screen design](../../plans/261012-1337-t1b-grok-style-altscreen/design.md) and `docs/tui-architecture.md`.

Run from the repository root:

```sh
python3 e2e/tui/run.py
```

The runner requires Python 3, Go, and `tui-test` at the version in [cli-version.txt](cli-version.txt).
Download the binary for your OS from the [official pinned release](https://github.com/microsoft/tui-test/releases/tag/0.1.0-beta.2), extract it, and put `tui-test` on `PATH`.
Check `tui-test --version` before you run the checks.
Do not use Python `-O` or `PYTHONOPTIMIZE`; these options disable assertions.
The runner creates unique named sessions under `~/.tui-test` and closes only those sessions.

Use `--backend alacritty`, `--backend ghostty`, or `--backend rio` to select an engine; the default run uses Alacritty and Ghostty.
Use `--case <name>` (more than once if needed) to select cases.
Use `--artifacts /absolute/path` to select an evidence directory; it must not exist yet.
The default evidence directory is `e2e/tui/artifacts/<run-id>`.

Each run saves, for each case, the CLI calls, process ownership, the terminal state, the screen text, cell dumps, SVG screenshots, and the output recording.
The runner compiles `./cmd/tui` into a temporary directory under `/tmp`, so the leader socket path stays short.
It hashes the product files (`go.mod`, `go.sum`, `cmd`, `internal`, `pkg`, `proto`, `migrations`, `third_party`; tests, testdata and Markdown excluded) before and after the run, and the run fails when they changed, because the binary under test is then not the code on disk.
Cleanup verifies saved PID identity before signaling an owned process; a cleanup error fails the case.

## Cases

All cases start `ask` with no arguments, with the faux provider and a temporary `ASK_HOME`. A reply comes from the faux script: `echo <text>` calls the echo tool with `<text>` and the model then says the text back, `fail <text>` ends in an assistant error, and any other text is said back.

| Case | Size | What it checks |
|---|---|---|
| `ask-idle` | 80×24 | The alternate screen is entered (`?1049h`) and mouse reporting is on. Row 1 is the header `◢◤ ask  ● leader <id>` with the model, row 2 a `─` rule, the prompt row sits between two rules with the placeholder and the cursor at column 4, and the last row is the bar `Tab blocks · ctrl+t thinking · ctrl+l model · / commands`. No scrollback purge. `ctrl+d` ends the program. |
| `ask-turn` | 80×24 | A prompt that calls the echo tool with nine output lines. The tool block is one row `◆ echo out-01 …` and no output line is on its own row. `Tab` selects the last block, the reply (it has a `┃` rail and cannot fold); `k` selects the tool step (`›` mark, bar `j/k select · e fold`). `e` shows Truncated: the first three lines, `⋮ 3 more lines · e to expand · Enter full screen`, the last three, and no command row. `e` again shows Expanded (`⌄`, nine lines), and a third `e` returns to one row. `Enter` opens the full-screen viewer (`← back  echo …`, the nine lines, the viewer bar), `Esc` returns to the same selection, `Esc` again returns to the prompt. |
| `ask-in-process` | 80×24 | The `ask-turn` flow with the in-process agent. `ASK_HOME` is a symlink, which the leader connect refuses, so the TUI falls back at startup (header `in-process`, no leader process). |
| `ask-resize` | 80×24 to 120×40 to 40×14 | A long reply streams (paced at 10 tokens per second) while a draft is in the prompt. After each size: no row is wider than the terminal, the header and its rule are on the first rows, the bar is on the last row, the short header form shows under 60 columns, the draft and the cursor stay, and the reply still streams. After the end, at 120×40 the whole reply is on the screen once and in order, the long prompt is folded, and no scrollback purge was written. |
| `ask-selector` | 80×24 | `/model` on an empty prompt and `ctrl+l` with a draft both put the selector in the prompt slot (rule above, full-width rows, the hint, then rule and bar) while the header and the block pane stay. `Esc` brings the prompt back, with the draft and its cursor after `ctrl+l`. |
| `ask-providers` | 80×24 | `/providers` puts the box in the prompt slot: seven rows in order, the search row last, full-width rows, the hint ending `your draft is kept`. The filter `key` keeps the four key rows. `enter` on `OpenAI API key` opens the key box at once. A fake key is pasted, the box shows `(hidden)`, the key is on no row of the screen or the recording, and `esc` brings the prompt back. |
| `ask-mouse` | 80×24 | Two long turns fill the pane. One SGR wheel-up report moves the window and shows `▼ End` centered on the last pane row. A new turn while scrolled up shows `▼ <N> new lines · End` centered with the top row unchanged. A click on the marker returns to the end and the marker is gone. Wheel-up and then wheel-down to the last row follows again without a click. |
| `ask-exit-quit` | 80×24 | `/quit` after a tool turn: the main screen is back, the cursor is visible, mouse reporting and bracketed paste are off (last write of each mode), and the last row on the screen is `ask: session <id>`. |
| `ask-exit-ctrl-c` | 80×24 | `ctrl+c` on an empty prompt asks (`Press ctrl+c again to quit`) and the second one ends the program, with the same exit checks. |
| `ask-exit-sigterm` | 80×24 | SIGTERM to the one `ask` process of the run while the viewer is open: the same exit checks. |
| `ask-exit-sighup` | 80×24 | The same with SIGHUP. |
| `ask-copy` | 80×24 | `/copy` before any reply shows `Nothing to copy` and writes no OSC 52. After an echo tool turn and a plain reply, `/copy` shows `Sent to the terminal clipboard`. The child has `SSH_TTY` set, so the native command is skipped and the real clipboard is never touched. The output holds exactly one `ESC ]52;c;` sequence, and its base64 payload decodes to the plain reply and nothing of the tool output. |

These checks use structure and the stable texts of the design, not whole-screen goldens: the run shows a temporary directory and the `faux-1` model. The golden tests in `internal/tui` own the exact rows.

### Notes on the input

- `Tab` in the prompt selects the last block, which is the reply (not foldable). The fold keys of a tool step need `k` first, as in the `ask-turn` case.
- Mouse input goes through `tui-test write` with SGR reports (`ESC [ < 64 ; col ; row M` for the wheel, `0 … M` and `0 … m` for a click). The `tui-test mouse` subcommands are not used: their reports did not reach `ask` on either engine. The raw reports reach it on both engines.
- The engine sometimes loses a mouse report that it writes within a few milliseconds after the last repaint of a reply (about 3 of 8 runs; the same report through a plain PTY in `real_provider.py` arrived in 8 of 8 runs). The runner sends a mouse report only after the screen text has been unchanged for 0.3 s (`AskSession.settle`).
- The alternate screen has no scrollback, so there is no history check; the tests read the visible frame.

Each case has its own `ASK_HOME` and its own leader, which the TUI starts.
A case that shows `in-process` in the header fails to start, so the fallback cannot pass for a leader test.
Only the cases in `IN_PROCESS_CASES` in [run.py](run.py) expect `in-process`; they start no leader, and the runner fails them when a leader of the run binary exists.
The cases in `REMOTE_CASES` add `SSH_TTY` to the child environment (the name is recorded in `commands.json`).
After a case the runner runs `ask leader stop` for that home and checks the process table for leaders of its own binary path.
A leader of that path that ignores the stop is signaled after its command line is checked again; no other process is touched.
A leader or tui-test process from outside the run is never signaled.

Run the controls of these assertions and of the secret scan with:

```sh
python3 e2e/tui/test-assertions.py
```

They feed accepted and broken frames, exit sequences and recordings to the assertions of [run.py](run.py). They validate the oracle; they do not prove a product defect.

### Environment and secrets

The runner passes `ASK_HOME`, `ASK_FAUX`, `ASK_FAUX_TPS`, `TERM` and `LANG` to the session through the environment of the `tui-test run` call (a case can set its own `ASK_FAUX_TPS`). It drops other `ASK_*` variables of the caller and every variable whose name looks like a credential, so no case can reach a billed provider. Every other variable is inherited.
Keys of the caller never appear in a command argument or in `--env`.
`commands.json` records `environment_names` (names only), and `ask-environment.json` lists the names that were set and the credential-like names that were dropped.
The `ask-providers` case types one fake key, made new for each run. The runner replaces it in the records of its own calls (`commands.json`) and in the input events of `output.cast`, and it adds the value to the scan below. The scan then finds it only when the product wrote it: on the screen, in the recording of the output or in a cell dump.
At the end the runner scans every artifact file for the values of credential-like variables (`*API_KEY*`, `*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `*CREDENTIAL*`, `*AUTH*`, value of 8 or more characters) and for the fake key.
A file with a value is removed, only the variable names are reported, and the run fails.
The tui-test daemon log is not enabled, because it records all terminal input and output.

The cases use the faux provider, so no key is needed. Real-provider acceptance is a separate manual record.

### Real-provider driver

`real_provider.py` and `real_provider_scenarios.py` run the built `ask` against real providers for acceptance runs by hand (they are not part of `run.py`).
They need the Python package `pyte` (`pip install --target /private/tmp/ask-rp-py pyte`, then `PYTHONPATH=/private/tmp/ask-rp-py`).
The runner compiles `./cmd/tui` at the start of each run into `/private/tmp/ask-rp-bin/ask` unless `ASK_BIN` names a file.
The scenarios read the header row for the leader id and the model, and `Working` for a running reply.

- `python3 e2e/tui/real_provider_scenarios.py acceptance` runs the provider-setup scenarios in order; `acceptance`, `providers` and `oauth` in [real_provider_scenarios.py](real_provider_scenarios.py) own the steps.
  The OAuth scenario uses existing credentials from a copy of `~/.ask-t1a-oauth`.
  It does not start a new OAuth sign-in.
- The child process gets no credential variable and no `ASK_*` variable (`run.is_credential_name`), so the leader it starts inherits none.
  The key is read from the environment of the runner, then sent as a bracketed paste through the PTY descriptor.
  The paste helper does not place it in a process argument or a runner log.
- Every scenario entry point calls `cleanup` in its `finally` block.
  It stops the sessions and leaders owned by that run, then calls `artifact_scan` before it removes the homes.
  The scan reads the current credential fields as well as the values registered at start, so it includes refreshed tokens.
  A file with a value is removed and the step fails; only names and counts are printed.
- Each scenario run owns a unique home under `/private/tmp`.
  The OAuth home copies only `auth.json` and `settings.json`, because a token refresh rewrites its credential file.
  The source home is never used by the child process.
  A failed process stop keeps the owned homes for cleanup.
- `python3 e2e/tui/test-real-provider-safety.py` checks cleanup and secret scan ordering with fake sessions and processes.
  It reads the real-run source through an AST and makes no provider request.
- Set `RP_OUT` to choose the output directory (default `/private/tmp/ask-rp-out`).
  The older single-purpose scenarios (`repeat`, `cancel-esc`, `cancel-ctrl-c`, `ctrlc-running`, `switch`, `thinking`, `detach`) save the key through `/providers` first.
