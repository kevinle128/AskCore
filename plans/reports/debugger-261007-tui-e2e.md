# TUI E2E resize diagnosis

Status: DONE_WITH_CONCERNS

## Result

The installed Alacritty backend stops serving IPC during the resize from 2×1 to 1×1 with the terminal state from the failed T0 run.
A plain Python process that replays the saved output reproduces this failure.
The reproduction does not run Go or Bubble Tea.
This isolates the failure to the terminal engine or CLI resize path for this terminal state.
It does not identify the internal engine function that fails.

## Controls and evidence

A plain Python process wrote 2,000 ASCII lines and remained alive.
Its resize from 40×12 to 2×1 to 1×1 completed, and state IPC remained available.
Commands are saved in `/private/tmp/ask-debug-resize-56c56f46.json`.

The failing control replayed output events from `/private/tmp/askcore-tui-e2e-uklct51w/runner-initial/alacritty/g3/output.cast`.
It wrote the output for each saved size on SIGWINCH.
The path was 40×12, 13×6, 12×6, 2×1, and 1×1.
State IPC completed at 2×1 with text `Z` and cursor column 1.
The resize to 1×1 exceeded the four-second command limit.
Close and daemon stop also exceeded their four-second limits.
The daemon log ends with `EVENT resize 1x1`.

The replay source and input are `/private/tmp/ask-debug-replay.py` and `/private/tmp/ask-debug-replay.json`.
Commands are saved in `/private/tmp/ask-debug-replay-74cadb0c.json`.
The session was `ask-debug-replay-74cadb0c`, with daemon PID 2169 and child PID 2170.
Both owned processes received SIGTERM after bounded cleanup failed.
A process check confirmed that these PIDs and all earlier diagnostic PIDs were absent.
No worker session was changed.

## Limits and action

History count and size alone did not cause the failure in the ASCII control.
The replay reproduces the failure with the recorded terminal state, but does not yet reduce it to one escape sequence or Unicode cell.
Keep the 1×1 case as an explicit failed engine check.
Do not infer a T0 renderer fix from this result.
Retain bounded cleanup with exact owned PIDs when IPC stops.
Headless results do not certify iTerm2 or Terminal.app.

An initial inline Python control failed because CLI argument decoding changed escaped newlines in the source.
The valid controls use script files and avoid that input path.

## Ghostty normal resize

Exact output replay also reproduces the Ghostty failure at 13×6 without tiny widths.
The source recording is `/private/tmp/askcore-tui-e2e-uklct51w/runner-normal-g3/ghostty/g3/output.cast`.
The initial 40×12 state has one editor row and cursor column 16, row 9.
After resize and recorded redraw, two `editor> abc界` rows remain, followed by `😀Z`.
The cursor is column 3, row 2.
Commands are saved in `/private/tmp/ask-debug-ghostty-ef1b1d9e.json`.

A second replay withheld redraw until after the resized terminal state was read.
Before redraw, Ghostty has one correct reflowed editor: `editor> abc界` at row 0 and `😀Z` at row 1.
The cursor is column 3, row 1.
The recorded repaint starts with carriage return and erase below the cursor, without moving up to the live frame origin.
It writes the complete editor from row 1 and leaves the old first row at row 0.
After redraw, the duplicate editor and cursor column 3, row 2 appear.
Commands are saved in `/private/tmp/ask-debug-anchor-e1844a6e.json`.

This proves a pinned renderer repaint assumption that is incompatible with native resize reflow.
It does not support calling the Ghostty reflow itself defective.
Keep the normal Ghostty G3 assertion failed.
Do not change the immutable archive or product source under this runner task.
Both replay sessions closed normally; their owned PIDs were 7123/7124 and 7817/7818.
