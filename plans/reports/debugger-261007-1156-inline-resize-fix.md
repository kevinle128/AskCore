# Inline resize fix scout

Status: DONE_WITH_CONCERNS

## Verified cause

The earlier paused Ghostty replay proves that native resize reflows the editor correctly before repaint.
The physical cursor moves to column 3, row 1 in the live frame at width 13.
The pinned renderer still stores the old cursor at column 16, row 0.
Its repaint begins at the physical row 1 and leaves the old first editor row above it.
See [the reproduction report](debugger-261007-tui-e2e.md).

## Exact call chain

Bubble Tea v2.0.10 `tea.go:857` handles `WindowSizeMsg` by calling `renderer.resize` before `model.Update`.
The event loop then stores the model view through `Program.render`.
`cursed_renderer.go:667` marks the UV screen for erase, changes dimensions, calls UV Resize, and marks pending erase.
`cursed_renderer.go:290` builds the new cell buffer and calls UV Render.
Only after UV Render does it apply the model cursor with UV MoveTo at line 515.

The exact UV pin is `v0.0.0-20260703014108-f5a850f9c2b7`.
Its `terminal_renderer.go:1304` Resize changes tab stops only.
It does not reconcile the cursor with native reflow.
Render calls clearUpdate at line 1223.
clearUpdate calls clearBelow with row 0 at line 1133.
That movement uses the old logical cursor to decide how far to move.
The physical cursor and the stored cursor no longer refer to the same live row.

The renderer state is private to Bubble Tea.
The public model has no supported method to change this state before erase.
Changing View.Cursor corrects the final cursor after repaint, which is too late to stop the stale row.
RawMsg or output-writer escape insertion would add another output path and leave the renderer state inconsistent.

## Newer cached UV

The August cache pin `v0.0.0-20260811164956-006e29f97886` adds cursor invalidation for absolute mode.
Its Resize comment explicitly excludes relative mode because an unknown cursor marker means the inline origin.
Bubble Tea uses relative cursor mode for inline output.
The newer Render changes for fullscreen shrink and buffer growth therefore do not prove a fix for this failure.
No current upstream release or newer dependency has been tested in this scout.

## Recommendation

Keep the correction in the renderer that owns erase, relative movement, and terminal writes.
Use a reviewed local T0 dependency patch or a verified upstream fix, with provenance and license files.
Do not change the old archive, root dependency graph, or model cursor to conceal the defect.

The renderer must reconcile the old rendered live frame and physical cursor after resize before it erases or redraws.
The retained old cell buffer and old cursor provide the required content evidence for a reflow calculation.
A cursor-position query can provide a physical checkpoint, but a screen coordinate alone does not identify the live frame origin.
Do not treat a simple cursor row assignment or width quotient as a complete repair.
Native wrapping, wide cells, hard line boundaries, height clipping, and grow behavior must remain explicit inputs or verified constraints.

The narrow first regression is the paused replay: old cursor column 16, row 0, width 40, then width 13 with the physical cursor at live row 1.
The repaired erase must start at live row 0, preserve transcript history, and leave one editor with cursor column 3 on its second row.
Broaden to selector replacement, multiline draft cursors, width 12, grow, height shrink, repeated resize, and stream insertion.
Run both real headless engines and existing Go fixture checks.
Keep 1×1 engine failure separate from the normal resize repair.

## Limits

This scout changed no target code and started no terminal session.
It proves the repair boundary and rejects model-only fixes.
It does not yet prove one reflow algorithm across both engines.

## Cursor-query repair path

Bubble Tea already decodes UV CursorPositionEvent to CursorPositionMsg.
RequestCursorPosition queues DSR through executeQuery and the owned output buffer.
The event loop can therefore support a local renderer repair without another input decoder or direct writer.

On resize, suspend frame flush and transcript insertion before the first erase.
Queue one cursor query through the existing output owner.
On its reply, reconcile the renderer state before allowing repaint.
Use a bounded timeout and a generation for a later resize; do not accept a reply for an older size.
Continue terminal cleanup if the query fails.

The content input must be the last frame whose write succeeded, not the new model View.
For each old hard row, preserve its cell sequence, wide-cell continuations, terminal wrap state, and cursor position.
Reflow those rows with the new width only under a verified native reflow rule.
The reflowed cursor offset can then identify the origin relative to the reported physical cursor.
Move to that origin through the renderer owner, reset UV logical position, then erase and render.
Keep transcript insertion suspended until the new frame is confirmed.

This is a viable repair under a stated and tested reflow contract, but CPR alone cannot prove that contract.
For example, an old cursor at column 16 on live row 0 and a new physical cursor on row 1 can describe either a reflowed frame with origin row 0 or an unreflowed frame with origin row 1.
Both states return the same CPR.
A saved origin is also insufficient because saved coordinates can clamp on height shrink.
Do not select a rule from the terminal name or silently treat the new editor layout as the old physical frame.

A general terminal contract needs a verified reflow behavior or another terminal capability that identifies the live origin.
Without that evidence, a small width-quotient patch is a sample-specific repair.
It must not be reported as robust for arbitrary hard rows, clipped frames, or mid-frame cursors.
The required checks include old live rows that move into scrollback on height shrink, because an origin clipped to zero cannot recover those rows without changing history.

## Strengthened mid-frame cursor failure

Paused replay now proves the history failure in the strengthened Alacritty test.
The source is `/private/tmp/askcore-inline-resize-fix-5rjbma6z/resize-evidence/strengthened/alacritty/middle/output.cast`.
That recording omits the typed draft output before its first resize.
The replay therefore reconstructs the known initial draft and cursor, then uses the exact recorded resize repaint bytes.
It uses raw terminal mode to prevent query responses from being echoed into the screen.
Commands and states are saved in `/private/tmp/ask-mid-raw-f0b00a60.json`.

At width 40, the initial editor is on row 1 with cursor column 11.
Before the width 13, height 6 repaint, Alacritty has moved the entire editor into scrollback.
Only the reflowed help remains visible, with cursor column 11, row 0.
The recorded repaint writes a new editor at row 0.
Before the next width 12 repaint, the first editor row has again moved into history.
The viewport starts with `😀Z` and the help text.
The recorded repaint writes another new editor at row 0.

Before the grow to 40×12 repaint, Alacritty restores the retired live rows from history.
The viewport contains the original full draft on row 2, the width 13 first editor row on row 3, and the current width 12 editor on rows 4 and 5.
The physical cursor is column 11, row 4.
The current cursor mapper correctly treats the current width 12 editor as starting on its first row.
The recorded grow repaint replaces that current frame and leaves the retired live rows above it.

This cause is wider than a cursor-offset error.
Clipping an inferred origin to zero permits old live rows to remain in native history.
Later growth restores those rows.
A larger cursor-up count can erase unrelated transcript or startup rows, so it is not a safe repair.

A correct renderer must track owned live rows that enter history and remove only those rows when they return, or use a supported terminal mechanism that prevents this history transfer.
The current cell buffer and cursor mapper do not retain that ownership information.
CPR alone does not identify retired live rows.
Before implementation, the selected mechanism must prove the live-row boundary after shrink and after history restoration on grow.
The unchanged exact assertion remains necessary.

The diagnostic session `ask-mid-raw-f0b00a60` closed normally.
Its owned daemon and child PIDs were 33397 and 33398.
Earlier replay attempts were invalid because startup was not awaited or query responses were echoed; those attempts do not supply acceptance evidence.

## Terminal mechanism controls

Four raw-mode plain-process controls tested DECSTBM and disabled autowrap on Alacritty and Ghostty.
They used the known initial draft with hardware cursor column 11, the exact recorded repaint bytes, and sizes 40×12, 13×6, 12×6, and 40×12.
DECSTBM restricted scrolling to the two initial live rows, terminal rows 2 through 3.
The second control disabled DECAWM before resize.
The initial `T0 startup` transcript row stayed outside the live region.
Commands, ownership, and pre/post redraw states are saved in `/private/tmp/ask-boundary-control.json`.
The child source is `/private/tmp/ask-boundary-child.py`.

Neither mechanism prevented the Alacritty failure.
In both controls, shrink retired editor rows into history and grow restored those rows above the current frame.
After the final repaint, three editor prefixes remained visible.
Both Ghostty controls ended with one editor, but disabled autowrap also clipped editor content before repaint during shrink.
These results do not establish a cross-engine terminal mechanism that preserves the original history and cursor contract.

All four sessions closed normally.
Their owned PIDs were 41009/41030, 41044/41045, 41059/41060, and 41074/41075.
A process check found none of these PIDs running.
No target or archive patch was made.

The tested standard terminal controls cannot reach or selectively erase owned rows while those rows remain outside the addressable viewport in native history.
The product options therefore need an explicit trade-off: restrict supported resize behavior, allow old live rows in native history, or move the live UI to a buffer whose history is owned by the application.
The last option changes the native scrollback requirement.
These experiments do not select an option for the user.
