# Headless TUI checks

The runner has two drivers. The `t0` driver runs the archived T0 gate scenarios. The `ask` driver runs the compiled `ask` (see [Built `ask` driver](#built-ask-driver)). The default run uses both.

Run from the repository root:

```sh
python3 e2e/tui/run.py
```

The runner requires Python 3, Go 1.27, and `tui-test` at the version in [cli-version.txt](cli-version.txt).
Download the binary for your OS from the [official pinned release](https://github.com/microsoft/tui-test/releases/tag/0.1.0-beta.2), extract it, and put `tui-test` on `PATH`.
Check `tui-test --version` before you run the checks.
Do not use Python `-O` or `PYTHONOPTIMIZE`; these options disable assertions.
The runner creates unique named sessions under `~/.tui-test` and closes only those sessions.

The default run uses Alacritty and Ghostty.
Use `--driver t0` or `--driver ask` to select one driver. A `--case` value selects its driver too.
Use `--backend alacritty`, `--backend ghostty`, or `--backend rio` to select an engine.
Use `--case g3` for the normal resize check, `--case g3-draft` for draft and cursor checks, or `--case g3-pending` for resize during a held transcript write.
Use `--target /absolute/path/to/t0` to test an existing binary with the archived T0 command contract.
Archived Go checks still run when you supply a target.
Use `--artifacts /absolute/path` to select an evidence directory.
The default evidence directory is `e2e/tui/artifacts/<run-id>`.

[run.py](run.py) owns the selected source archive, case list, assertions, and build commands.
It verifies archive hashes, copies the isolated Go module to a temporary directory, runs its Go checks, and builds the binary.
The selected [resize candidate](../../plans/261007-1156-inline-resize-fix/artifacts/replay-candidate/README.md) contains pinned modules and local renderer provenance.
The runner sets `GOWORK=off` and uses a temporary Go build cache.
It uses `GOMODCACHE` when set, or a task cache under the system temporary directory.
The vendored graph supports the retained offline build.
Root product files and the frozen source archive are checked before and after the run.

## Resize contract

The accepted inline route retains the full transcript in application state.
After resize settles for 120 ms, the same output owner clears the screen and native scrollback and rebuilds committed content plus the current live frame at the latest size.
This repair can remove terminal output from before application startup.
Replay does not advance the commit frontier or acknowledge entries again.
Ordinary commits remain exact-once ordered output; each resize replay must be a complete ordered generation.
Alternate-screen use remains forbidden.
See the [accepted plan and evidence](../../plans/261007-1156-inline-resize-fix/plan.md).

The checks cover transcript order, editor and selector state, Unicode cells, cursor position, actual resize, input decoding, and intact large paste.
The standalone G2 case has no committed history; retained Go checks cover that history contract.
[Pending-write checks](pending-resize.py) retain the full 2,000-marker assertion at the measured 13×6 → 40×12 sizes.
Each run saves CLI calls, process ownership, terminal state, history, cells, SVG screenshots, and output recordings.
Cleanup verifies saved PID identity before signaling an owned process; a cleanup error fails the case.

Run the assertion negative controls with:

```sh
python3 e2e/tui/test-assertions.py
```

These controls reject missing, duplicate, reordered, or incomplete transcript generations, incorrect cursors, and alternate-screen output.
They validate the oracle; they do not prove a product defect.

## Capacity and extreme-size limits

Run `python3 e2e/tui/run.py --case g3-capacity --backend ghostty` for the separate 52×15 native history capacity probe.
This probe is outside the default acceptance run and retains its failed result.
Plain-process Ghostty controls retain only 1,096 of 2,000 markers at that size, with or without purge and with a larger scrollback profile.
Application retention and ordered output do not guarantee arbitrary native scrollback capacity.
See the [capacity evidence](../../plans/reports/debugger-261007-1230-inline-replay.md).

Run `python3 e2e/tui/run.py --case g3 --extreme-probe` for the separate extreme-size probe.
The earlier Alacritty 1×1 failure was reproduced by replaying captured bytes without the Go target.
Failures remain failures; the retained Go fixture's width-1 and width-2 results do not certify every native engine at those sizes.
Earlier normal resize failures and the false pass from an incomplete oracle remain historical evidence, not current acceptance proof.

These checks use headless terminal engines.
They do not certify real iTerm2 rendering, physical Unicode placement, or every real-terminal exit path.
The user reported Terminal.app/iTerm2 acceptance and D14 is closed in the [decision record](../../plans/reports/pm-261007-1312-d14-tui-decision.md).
That statement is user-reported acceptance, not evidence captured by this runner.
Exact manual terminal versions and physical recordings were not supplied; historical result files retain their original headless-only and D14-false values.

## Built `ask` driver

Run `python3 e2e/tui/run.py --driver ask` (or `--case ask-idle`, `--case ask-selector`, `--case ask-resize`).
The driver compiles `./cmd/tui` into a temporary directory under `/tmp`, so the leader socket path stays short, and starts `ask` with no arguments in an owned tui-test session.
It is a separate driver, not a setting of the T0 path: it has its own cases, its own checks and its own process environment.
The T0 scenarios and their assertions are unchanged.

| Case | Size | What it checks |
|---|---|---|
| `ask-idle` | 80×24 | One startup header, a full-width editor box with the placeholder, the cursor in the editor, a `leader <id>` footer. |
| `ask-selector` | 80×24 | `ctrl+l` replaces the editor with the model selector, the draft is kept, `esc` brings the draft back with its cursor. |
| `ask-resize` | 120×40 to 80×24 | A reply streams, a draft is in the editor, the size changes. The draft and cursor stay, one purge and rebuild follows, and after the run the header, the prompt and the reply each appear once, in order. |

These checks use structure, not exact sketch text: the sketches show `~/src/askcore` and a real model, while the run shows a temporary directory and the `faux-1` model.
The golden tests in `internal/tui` own the exact rows.

Each case has its own `ASK_HOME` and its own leader, which the TUI starts.
A case that shows `in-process` in the footer fails to start, so the fallback cannot pass for a leader test.
`ask` quits with `ctrl+c` to clear the draft and `ctrl+d` to detach; then the runner runs `ask leader stop` for that home and checks the process table for leaders of its own binary path.
A leader of that path that ignores the stop is signaled after its command line is checked again; no other process is touched.
A leader or tui-test process from outside the run is never signaled.

### Environment and secrets

The runner passes `ASK_HOME`, `ASK_FAUX_TPS`, `TERM` and `LANG` to the session through the environment of the `tui-test run` call. It drops other `ASK_*` variables of the caller. Every other variable, including provider keys, is inherited.
Keys never appear in a command argument or in `--env`.
`commands.json` records `environment_names` (names only), and `ask-environment.json` lists the names that were set and the credential-like names that were inherited.
At the end the runner scans every artifact file for the values of credential-like variables (`*API_KEY*`, `*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `*CREDENTIAL*`, `*AUTH*`, value of 8 or more characters).
A file with a value is removed, only the variable names are reported, and the run fails.
The tui-test daemon log is not enabled for this driver, because it records all terminal input and output.

The cases use the faux provider, so no key is needed. Real-provider acceptance is a separate manual record.
Run `python3 e2e/tui/test-assertions.py` for the controls of these assertions and of the secret scan.

### Real-provider driver

`real_provider.py` and `real_provider_scenarios.py` run the built `ask` against real providers for acceptance runs by hand (they are not part of `run.py`). They need the Python package `pyte` (`pip install pyte`, for example into a virtual environment outside the repository). Keys reach `ask` only through the inherited environment. Copy an OAuth `ASK_HOME` to a short path under `/private/tmp` first, because a token refresh rewrites its credential file.
