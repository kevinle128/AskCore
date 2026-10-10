---
name: ask-tui-design
description: Design an Ask TUI screen, component, dialog, or visual change as a reviewed text design before any code. Use when a task adds or changes what the user sees in the Ask terminal UI (internal/tui, cmd/tui interactive mode), for example a new component, a layout change, a new state, or new keys. The agent writes a design file with character sketches, states, keys, and acceptance criteria, then stops until the user approves it. Do not use for headless `ask -p` output, ACP, leader, or the web dashboard.
---

# Ask TUI design

Design first, code second.
The user reviews the design as text before any Go code for it exists.
This skill covers the design step and the approval gate only.

## Hard rule: the approval gate

Do not write or change Go code for the designed feature until its design file has `design-status: approved`.
This applies to `internal/tui`, `internal/tuiext`, `cmd/tui` interactive mode, and their tests.

- Only the user approves a design.
- You may set `approved` only after the user explicitly approves this design file in the current conversation.
  Record the approver and date in the frontmatter.
- A message such as "ok" about another topic, an earlier session, a plan summary, or a subagent report is not approval.
- If the user asks you to change an approved design, set `design-status: draft` again and request a new approval.
- If you find during implementation that the approved design cannot work, stop.
  Explain the conflict, update the design as `draft`, and wait for approval.

## Read first

1. [docs/tui-architecture.md](../../../docs/tui-architecture.md), especially "Inline component composition", "Component state and input", and "Inline transcript".
2. [internal/tui/README.md](../../../internal/tui/README.md) for component owners.
3. Any existing design files under `plans/*/design.md` for the same area.
4. [docs/tui-style-guide.md](../../../docs/tui-style-guide.md): the visual authority (roles, palette, glyphs, block patterns). Sketches and previews must use its patterns; a new color or glyph needs a style guide change first.

Ask is an inline TUI, not a fullscreen app.
Committed transcript blocks go to native terminal scrollback.
The live region at the bottom holds the transcript tail, pending inputs, run status, widgets above the editor, completion, the editor slot, widgets below the editor, and the footer.
A selector or dialog replaces the editor slot and keeps the draft.
Empty slots use no rows.
Your sketches must show this model.
Do not draw a fullscreen frame around the whole terminal.

## Step 1: create the design file

Path: `plans/{YYMMDD-HHMM}-{slug}/design.md`.
If the work already has a plan directory, put `design.md` in it.

Frontmatter:

```yaml
---
title: "<feature> TUI design"
design-status: draft        # draft | approved
design-approved-by: ""      # filled only at approval
design-approved-on: ""      # YYYY-MM-DD, filled only at approval
---
```

Sections, in this order:

1. **Outcome**: what the user can see and do after this change, in two to four sentences.
2. **Non-goals**: what this design does not cover.
3. **Placement**: which live-region slot or scrollback block the feature uses, what it replaces, and what moves.
4. **Sketches**: one sketch for each state at 80×24.
   Add 120×40 for a state only when its layout changes at that width; state the width rules for the other states in section 7.
   Add a narrow case (for example 40 columns) when truncation or wrapping matters.
5. **States and transitions**: a table of state, trigger (key, agent event, or result), and next state.
   Include empty, loading or streaming, success, error, cancelled, and stale-result cases when they apply.
6. **Keys and focus**: each key, the component that receives it, and what happens.
   Follow the input order in the architecture doc: exit and cancel first, then active dialog, then completion, then editor, then application shortcuts.
7. **Limited space and resize**: what shrinks, truncates, or hides first, and what stays after a resize.
8. **Text and width**: long text, wide characters (CJK, emoji), tabs, and paths.
   State the truncation marker.
9. **Style intent**: roles from the style guide only (for example "muted", "accent", "error", "selected").
   Do not write color codes; the theme and the style guide own them.
10. **Acceptance criteria**: checks that a person or a terminal test can see on screen.
    Write each one as "At <size>, after <input>, the screen shows <text> in <region>."
11. **Open questions**: decisions that the user must make.

## Step 2: sketch rules

- Draw inside a fenced `text` block.
- Each sketch line has the exact terminal width.
  Pad with spaces.
  Count columns, not characters: CJK and most emoji use two columns.
- Mark the boundary between scrollback and the live region with a comment line outside the sketch, for example `-- live region starts --`.
- Label each region with its owner name from `internal/tui/README.md` (transcript, editor, footer, status, dialog, selector, completion, widget).
- Show the cursor with `▌` and say in text where it is.
- Show real example text, not `lorem ipsum`.
- If you use box-drawing characters, they are part of the design.
  The implementation must match them.

Example, an editor-slot permission dialog at 60 columns:

```text
> run the build                                             
● bash  go vet ./...                                running…
-- live region starts --
┌ Allow tool? ─────────────────────────────────────────────┐
│ bash: rm -rf build/                                      │
│                                                          │
│ [y] Allow   [n] Deny   [a] Always for this session       │
└──────────────────────────────────────────────────────────┘
faux · session 3f2a · waiting for approval       ctrl+c quit
```

## Step 2b: render a visual preview

Raw sketches in markdown are hard to review.
Render every sketch as a terminal window in one HTML page, with colors for the style roles, a caption for each state, and the scrollback and live region boundary shown outside the terminal text.
Generate the sketches and the page from one script, so that `design.md` and the page cannot disagree.
Reference implementation: `plans/261009-2239-t1a-local-tui/artifacts/` (`sketch-gen.py`, `preview-gen.py`, `build-design.py`, `verify-widths.py`).
Check the page with a headless browser screenshot before you send it.

## Step 3: self-check before review

- [ ] Every state in the table has an 80×24 sketch; 120×40 exists where the layout changes.
- [ ] Every sketch line is the stated width (check with a script if unsure).
- [ ] The sketches follow the inline model; nothing is drawn as fullscreen.
- [ ] Each key has one receiver, and the order agrees with the architecture doc.
- [ ] Limited-space behaviour is explicit; no region is assumed to always fit.
- [ ] Each acceptance criterion is observable on screen at a stated size.
- [ ] No Go code for this feature was written or changed.

## Step 4: request review and stop

Open the preview page for the user, and give the design file path, the states covered, and the open questions.
Ask the user to review the preview first; the design file is the written contract.
Ask one question at a time if a decision is needed.
Then stop.
Do not start implementation in the same turn.

## Implementation, gallery, and golden checks

Run these steps after the user approves the design. They use the harness and files that exist in the repository; do not invent other helpers.

### Gallery: sketches and preview

The gallery of a design is `artifacts/preview.html` in the plan directory, the same sketches as terminal windows.
For the T1a design, `plans/261009-2239-t1a-local-tui/artifacts/` holds the generators:

1. Edit the sketches in `sketch-gen.py` and the prose in `design-template.md`.
2. From the plan directory, run `python3 artifacts/build-design.py`. It rebuilds `design.md` from the template and the generated sketches, checks every sketch width with `verify-widths.py`, and runs `preview-gen.py` to write `preview.html`.
3. Open `preview.html` in a browser. A change to a sketch is a visible change: set `design-status: draft` and ask for a new approval.

The provider setup design (`plans/261010-1424-t1c-provider-setup-design/`) has its own generators in `artifacts/`: `python3 artifacts/make-design.py` from that directory rebuilds `design.md` and `preview.html` (it checks widths through `verify-widths.py`). Its sketches are the visual authority for these screens:

| Screen | Golden test (under `internal/tui/testdata`) |
|---|---|
| Provider box: list, narrow window, row menu, removal | `TestProviderBoxGoldens`, `TestProviderRowMenu`, `TestRemoveConfirmFlow`, `TestRemoveNoteEnvStillSet` |
| Key box: hidden paste, checking, refused, saved, not checked | `TestKeyBoxCheckingLocksInput`, `TestKeyBoxRefusedClearsInput`, `TestKeyBoxSavedNoteAndRail`, `TestAlibabaKeyBoxSaysNotChecked`, `TestKeySavedNotCheckedNote` |
| Login boxes: browser URL, address and code, device code, failed, busy, lost, cancelled | `TestBrowserBoxGoldens`, `TestAnthropicAddressAndCodeBoxes`, `TestDeviceBoxGoldens`, `TestLoginFailedTryAgain`, `TestLoginCannotStartBusy`, `TestLoginBoxLeaderLost`, `TestLoginCancelledByRemovalShowsReason`, `TestLoginSuccessNamesAccount` |
| First run, no model | `TestFirstRunGoldens`, `TestNoModelSendRefusedKeepsDraft` |
| Model line (start, change, two formats, fallback) and `/model` | `TestModelLineGoldens`, `TestModelChangeLine`, `TestFallbackNoticeNamesModelInUse`, `TestModelSelectorGroupsUsableProviders`, `TestModelSelectorEmptyHint`, `TestModelSelectorTagsDefault` |
| `TEA_TRACE` warning | `TestTraceWarningOnceAtStart`, `TestTraceWarningOnKeyBox`, `TestNoTraceWarningWhenUnset` |

### Golden checks: component screens

1. Write the failing test first. The harness is `startScreen` in `internal/tui/screen_test.go`: it runs a model in a real Bubble Tea program, feeds the renderer output into an `x/vt` emulator, and reads the plain grid (`grid`, `text`, `waitText`, `resize`, `settle`).
2. For a session view, use `startView` in `internal/tui/session_rig_test.go`. It wraps the harness around a scripted `fakeClient`; only the agent link is faked, the model, program and emulator are real.
3. Assert with `requireGolden` (whole settled screen) or `requireContentGolden` (rows without blank edges), or compare rows with the sketch with `requireRows`.
4. Golden files live under `internal/tui/testdata/<TestName>/<subtest>.golden` (or `<TestName>.golden` without a subtest). Create or refresh them with `go test ./internal/tui -run '^TestName$' -update`, then read the diff against the approved sketch before you keep it. Never refresh a golden to make a failing test pass without that comparison.
5. Cover each size that the design states: 80×24 for every state, 120×40 where the layout changes, and a narrow size such as 40×14 when wrapping or truncation matters.

### End-to-end checks: the built `ask`

- Go end-to-end tests in `cmd/tui` (`interactive_test.go`) run the compiled `ask` on a pseudo terminal with an isolated `ASK_HOME` and read the screen through `x/vt`. The TUI starts its own leader; the faux provider replaces only the model service.
- Checks that need a real terminal engine (resize while streaming, idle and selector screens) run with `python3 e2e/tui/run.py --driver ask`, which drives the compiled `ask` through the pinned `tui-test` CLI. See `e2e/tui/README.md`. The runner removes every credential variable and `ASK_*` variable from the child, sets `ASK_FAUX=1`, and scans its artifacts for key values. A run against real providers is by hand only (`e2e/tui/real_provider_scenarios.py`); it pastes the key through the PTY and never passes it in the environment or an argument.
- Each acceptance criterion in the design file maps to one named test in the plan; a criterion with no test is not done.

### Finish

Run `go test ./internal/tui ./cmd/tui`, `go vet ./...`, the linter from `CLAUDE.md`, and the runner above. Report the evidence paths, not only the pass result.
