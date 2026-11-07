# Trust and settings

A project folder with gated files (`.ask/settings.json`, skills, prompts, MCP and similar) is not read until a trust decision exists. `ask settings show` prints the effective settings and which layer set each, without starting a leader.

## Sub-features

- `trust-untrusted` no decision means untrusted, the project file is not read, and `-p` warns.
- `trust-approve` `--approve` loads the project file for one run and saves nothing.
- `trust-deny` `--no-approve` keeps it unloaded explicitly.
- `trust-conflict` both flags together exit 1.
- `settings-json` `ask settings show --json` has the documented shape.
- `trust-prompt` the TUI asks, `/trust` shows and changes the saved decision (via the PTY runner).

## How to get to it (user POV)

- Run `ask settings show [--json] [--approve|--no-approve]` in a project folder.
- Run `ask -p` in a gated folder and read stderr.
- Start the TUI in a gated folder and answer `Trust this project folder?`; type `/trust` later.

## Driving it with askv.py, plain shell and e2e/tui/run.py

Preconditions: `askv.py new-run --gated` (writes `work/.ask/settings.json` with `{"thinkingFold": "collapsed"}` as verification scaffolding, removed by cleanup), `doctor` printed `OK`, env exported, cwd is the work folder.

- **Untrusted.** Run `$ASK settings show`. The text has `Project trust:    untrusted (no_ui)`, `Gated files:      .ask/settings.json`, `Project settings: not read (untrusted)` and `thinkingFold         expanded                         default`. Exit `0`.
- **Warning.** Run `$ASK -p "echo hi" 2>$EV/trust-warn.txt`. Stderr holds `Warning: project settings ignored: ... is not trusted. Use --approve, or save a decision in the TUI with /trust.` Stdout is `hi`.
- **Approve.** Run `$ASK settings show --approve`. It shows `trusted (flag)`, `Project settings: loaded` and `thinkingFold         collapsed                        project`. Then `ls $ASK_HOME`: no `trust.json`. Run `$ASK settings show --json --approve | python3 -c 'import sys,json;p=json.load(sys.stdin)["project"];print(p["layer"],p["source"])'` and expect `loaded flag`.
- **Deny.** Run `$ASK settings show --json --no-approve`. `project.layer` is `untrusted`, `project.source` is `flag`, `project.trusted` is `false`.
- **Conflict.** Run `$ASK settings show --approve --no-approve`. Exit `1`.
- **Prompt in the TUI.** Run `python3 e2e/tui/run.py --backend alacritty --case ask-trust-prompt --case ask-trust-prompt-no-leader --artifacts $EV/tui-trust` from the repo root. Both cases print `pass`. This covers `Not now` leaving no `trust.json`, `Trust this folder` writing `trust.json` with the folder and `true`, and `/trust`.
- **Proof.** Keep the command output in `$EV/trust-settings.txt` and the JSON in `$EV/settings.json`; the second view for a decision is the content of `trust.json` (absent after every flag run above).

## Gotchas

- macOS reports the work folder as `/private/tmp/...` while `ASK_HOME` stays `/tmp/...`. Compare with `realpath`.
- `--approve` is not a dry run: the project file is really loaded for that process. It still saves nothing.
- Context files (`AGENTS.md`, `CLAUDE.md`) are not gated. A folder without gated files shows source `no_gated_files` and needs no decision.
- The saved-decision paths (`saved`, `saved_ancestor`, `default_always`, `default_never`) are reachable by hand-writing `trust.json` or `settings.json` in the run home. That is an acceptable precondition for a settings test, not proof of the TUI writer.
- The TUI cases need `tui-test`; if it is missing, report the TUI half as unverified rather than skipped silently.
