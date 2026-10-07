# Headless TUI checks

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
