---
title: "Project trust prompt and /trust TUI design"
design-status: approved     # draft | approved
design-approved-by: "user"  # filled only at approval
design-approved-on: "2026-11-04"  # YYYY-MM-DD, filled only at approval
---

# Project trust prompt and /trust TUI design

Preview: [artifacts/preview.html](artifacts/preview.html). Rebuild this file and the preview with
`python3 artifacts/make-design.py` from this directory. The sketches come from `artifacts/sketch-gen.py`;
the prose comes from `artifacts/design-template.md`. Do not edit `design.md` by hand.

Plan: [plan.md](plan.md). This file is the contract of [phase 6](phase-06-tui-trust-prompt-and-trust-command.md).
Wire facts come from the phase 4 code: `pkg/protocol/acp_trust.go`, `internal/app/trust.go`,
`internal/workspace/trust.go`, `internal/tuiclient/trust.go`.

## 1. Outcome

When the user starts `ask` in a folder that has gated project files and no saved trust decision, the TUI asks one question before the first session starts.
The block pane says which files wait, what trust loads, and what trust does not do.
A box in the prompt pane slot offers four choices. **Not now** is first and highlighted, so a stray Enter grants nothing and saves nothing.
After the choice, the session starts and one short note in the block pane records the result.

`/trust` shows the saved decision of the folder and the trust of the current session, and changes the saved decision.
A change applies to the next session only. The TUI never changes the trust of a running session.
The TUI also shows the untrusted note, the trust store warning and the settings problems of a session start.

### Design decisions (decided by the user in section 11)

| # | Decision | Recommendation | Reason |
|---|---|---|---|
| C1 | Choices in the startup box | Four: **Not now** (this session, nothing saved), **Trust this folder** (saved), **Trust parent folder** (saved), **Do not trust** (saved). No "Trust this session only" row. | Four rows fit the 7-row slot at 40×14 with no window. `ask --approve` covers a one-time trust. Not now is Pi's "Do not trust (this session only)". |
| C2 | Default highlight | **Not now**, the first row. | The prompt opens at start, while the user can still type. Enter on the first row must grant nothing and save nothing. |
| C3 | Esc and ctrl+c | Both mean **Not now**. ctrl+d quits: no `SetTrust`, no `session/new`. | Cancel never grants (phase 6 security rule). ctrl+c closes the box first, as it closes the viewer first. |
| C4 | Typed-ahead input | The box acts only on ↑, ↓, Enter, Esc, ctrl+c and ctrl+d. It has no letter or digit keys, and no `j`/`k`. It drops printable keys and pastes; the draft stays. It ignores the mouse. | A stray `j` and Enter could grant trust. The architecture already says a box ignores the mouse. |
| C5 | First frame and composer | Before the trust answer, the prompt pane shows the editor and keeps typed text; Enter sends nothing (no session exists). When the answer needs a decision, the box replaces the prompt pane and the explanation goes to the block pane. | The block pane is empty before the first session; a box with all the text would not fit the half-screen slot. |
| C6 | Where the text goes | Explanation: a start notice block in the block pane. Choices: the box. After the choice, one result note replaces the explanation block. | Same pattern as the fallback notice and the `TEA_TRACE` warning (`start_notices.go`). |
| C7 | Trust parent folder | Hide the row when the parent is the file system root (the host refuses it) or the user home (`sessionView.home`). | Trusting the home trusts every project under it. |
| C8 | `/trust` | A box with the folder, `Saved:`, `This session:` and up to four rows. Each change says "applies to the next session". `/new` starts that session. | Same rule as Pi (`interactive-mode.ts:5250-5274`), adapted: the leader decides at each `session/new`. |
| C9 | No implicit trust | No command in the TUI re-decides trust in a running session. The TUI has no `/reload`. `/new` and a new process ask the leader again; nothing saves trust that the user did not choose. | A model can create `.ask/` during a session. |
| C10 | Diagnostics | One `Settings problems` block after the trust note, grouped by file, at most 8 item rows, then `… <n> more · ask settings show`. | One place for every settings problem; the full list is in `ask settings show` (phase 7). |

## 2. Non-goals

- No tool permission popup. The box is a startup gate for project files, so decision D3 of the roadmap (`roadmap.md:53`, no tool permission popups) stays. The box names no tool and asks nothing about a command.
- No "Trust this session only" row (see C1 and section 11).
- No change to the trust of a running session. `/trust` saves for later sessions only; it does not call `session/new`.
- No trust UI in `ask -p`, `ask acp` or `ask connect`. They use the flags, the saved decision and `defaultProjectTrust`.
- No editor for `trust.json` entries of farther ancestors. `/trust` changes only the folder entry and the parent entry, because `_ask/trust/set` takes only `folder` or `parent`.
- No visible change for `thinkingFold`. The first frame uses the user file (`cmd/tui/thinking_fold.go`). When `_ask/session/state` arrives, the TUI applies its effective `thinkingFold`. No note shows. Test: `TestThinkingFoldFromSessionState`.
- No new color, role or glyph.

## 3. Placement

| Part | Owner | What it shows | Replaces or moves |
|---|---|---|---|
| Startup trust box | dialog (prompt pane slot); new file `internal/tui/trust_dialog.go` | Title, four (or three) choice rows, hint row | Replaces the prompt pane while open. The draft stays in the editor and comes back after the choice. |
| Explanation block | start notices (`start_notices.go`), block pane | Folder, gated entries, what trust loads, what trust does not do | Shows only while the startup box is open. The result note replaces it. |
| Trust result and untrusted notes | start notices, block pane | One note, the text of section 8 | Comes before the fallback notice. |
| Trust warning | start notices, block pane | `Warning: ` and the `warning` text of `_ask/trust/state`, verbatim | First block of the transcript. |
| Settings problems | start notices, block pane | Diagnostics of `_ask/session/state` | After the trust note. |
| `/trust` box | dialog (prompt pane slot), same file | Folder, saved decision, session trust, choice rows | Replaces the prompt pane while open and keeps the draft, as `/providers` does. |
| `/trust` result | status row (`setResult`, `setNotice`) | `✓ Saved: …` or an error | Uses the rail for the notice time, as `/model` does. |
| `/trust` in the completion list | completion (`completion.go`) | `/trust   Trust or do not trust this folder` | New row after `/providers`. |

The header row, the footer row and the shortcut bar do not change. Before the first session the footer row has no model text and shows the meter only, as today (`footer.go`).

### Start flow (the order phase 6 implements)

This flow runs before the first `session/new`, before the `session/new` of `/new`, and never in the middle of a session.

1. The link opens (existing `Starting leader…` rail).
2. A command-line flag (`--approve`, `-a`, `--no-approve`, `-na`) exists: the TUI does not call `_ask/trust/state`. Every `session/new` of this process carries `_meta["ask.dev/trust"]` = `approve` or `deny`. Go to step 6.
3. The TUI calls `_ask/trust/state` with its cwd.
   - `tuiclient.ErrTrustNotAvailable` (older leader): no box, no trust note, no warning. `session/new` with no override. Go to step 6.
   - Any other error, or no answer in the call time: no box. `session/new` with no override. The leader decides with no UI (source `no_ui` when the folder has gated files). The TUI keeps the error text for the note. Go to step 6.
4. The answer has a `warning`: the TUI adds the warning block first.
5. `needsDecision` is true: the box opens (state P1). Else `session/new` with no override.
   - **Not now**, Esc, ctrl+c: no `SetTrust`; `session/new` with override `deny`.
   - **Trust this folder**: `SetTrust{cwd, folder, trust}`, then `session/new` with no override.
   - **Trust parent folder**: `SetTrust{cwd, parent, trust}` (the host also removes the folder entry), then `session/new` with no override.
   - **Do not trust**: `SetTrust{cwd, folder, distrust}`, then `session/new` with no override.
   - `SetTrust` fails: the box stays open with an error row (state P5). Nothing starts.
6. `_ask/session/state` arrives: the TUI shows the trust note (section 8, table B), then the settings problems (state D1), and applies `thinkingFold`.

The source word of a session cannot tell a prompt choice from a flag: `workspace.Decide` returns `flag` for every override. The TUI keeps its own record of what it sent (prompt choice, flag, or nothing) and picks the note text from that record first, then from the session source.

A retry of the same start does not ask again. When the first `session/new` fails, the next prompt creates the session (`promptCmd` with `needNew`); that call reuses the override of the last start and never opens the box.

## 4. Sketches

Sizes: every state at 80×24. P1 also at 120×40 and 40×14. The narrow cases (40×12, 40×14) show truncation, the option window and CJK paths.
The draft `fix the failing invoice test` shows that typed-ahead text stays. `▌` is the cursor at the end of the draft.
In the box, `❯` and the `selected` background mark the highlighted row; the cursor is hidden while a box is open.

#### S0. Before the trust answer: typed-ahead draft

The link is open; the TUI waits for _ask/trust/state. No session exists, so Enter sends nothing and the rail shows `Starting, please wait…`. The draft stays. Today `opening` clears when the link opens; phase 6 keeps the view in this starting state until the first session exists.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ Starting, please wait…                                                        
────────────────────────────────────────────────────────────────────────────────
 ❯ fix the failing invoice test▌                                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                       ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### P1. Startup prompt: folder with gated files and no saved decision

The explanation block is in the block pane; the trust box takes the prompt pane slot. Not now is first and highlighted. At 120×40 the layout is the same; the text uses fewer rows. At 40×14 the box shows labels only, and the pane has room for one row only (the gap row), so the explanation does not show.

Regions: row 1 header; row 2 rule; rows 3-13 block pane; row 14 rule; rows 15-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ~/src/acme has project files that Ask has not read or run:                    
    .ask/settings.json  .ask/skills  .agents/skills                             
  If you trust this folder, Ask loads its project settings, system prompt       
  files, skills, prompts, extensions and MCP config.                            
  Trust does not give permission to run shell commands or tools. Trust does     
  not apply to AGENTS.md or CLAUDE.md.                                          
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────╮
│ ❯ Not now              Do not load them now. Ask again in the next session.  │
│   Trust this folder    Load them now and in later sessions.                  │
│   Trust parent folder  Trust ~/src and every folder in it.                   │
│   Do not trust         Do not load them. Do not ask again.                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept           
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                       ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-29 block pane; row 30 rule; rows 31-37 dialog; row 38 rule; row 39 footer; row 40 bar.

120×40:

```text
 ◢◤ ask                                                                                                                 
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ~/src/acme has project files that Ask has not read or run:                                                            
    .ask/settings.json  .ask/skills  .agents/skills                                                                     
  If you trust this folder, Ask loads its project settings, system prompt files, skills, prompts, extensions and MCP    
  config.                                                                                                               
  Trust does not give permission to run shell commands or tools. Trust does not apply to AGENTS.md or CLAUDE.md.        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ Not now              Do not load them now. Ask again in the next session.                                          │
│   Trust this folder    Load them now and in later sessions.                                                          │
│   Trust parent folder  Trust ~/src and every folder in it.                                                           │
│   Do not trust         Do not load them. Do not ask again.                                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept                                                   
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                                                               ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                                                               
```

Regions: row 1 header; row 2 rule; row 3 block pane; row 4 rule; rows 5-11 dialog; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
                                        
────────────────────────────────────────
╭─ Trust this project folder? ─────────╮
│ ❯ Not now                            │
│   Trust this folder                  │
│   Trust parent folder                │
│   Do not trust                       │
╰──────────────────────────────────────╯
   ↑↓ · enter · esc not now             
────────────────────────────────────────
                                     0% 
 Tab blocks · / commands                
```

#### P2. Startup prompt: long path, long gated list, small height

The root path wraps by columns and is never cut. Relative entries wrap at spaces; an ancestor .agents/skills shows as an absolute path on its own row. More than 4 file rows: 3 rows and `… n more`. In the option description the path shortens to ~/…/<last element>. At 40×12 the slot has 5 rows: the option list is a window that keeps the highlighted row, with `… n more`.

Regions: row 1 header; row 2 rule; rows 3-13 block pane; row 14 rule; rows 15-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ~/work/clients/northwind-traders/platform/services/billing-reconciliation-wo  
  rker has project files that Ask has not read or run:                          
    .ask/settings.json  .ask/SYSTEM.md  .ask/APPEND_SYSTEM.md  .ask/mcp.json    
    .ask/models.json  .ask/skills  .ask/prompts  .ask/extensions                
    .agents/skills                                                              
    ~/work/clients/northwind-traders/.agents/skills                             
  If you trust this folder, Ask loads its project settings, system prompt       
  files, skills, prompts, extensions and MCP config.                            
  Trust does not give permission to run shell commands or tools. Trust does     
  not apply to AGENTS.md or CLAUDE.md.                                          
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────╮
│ ❯ Not now              Do not load them now. Ask again in the next session.  │
│   Trust this folder    Load them now and in later sessions.                  │
│   Trust parent folder  Trust ~/…/services and every folder in it.            │
│   Do not trust         Do not load them. Do not ask again.                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept           
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/…/billing-reconciliation-worker                                ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### P2n. Startup prompt at 40×12

Same state as P2 at 40×12.

Regions: row 1 header; row 2 rule; row 3 block pane; row 4 rule; rows 5-9 dialog; row 10 rule; row 11 footer; row 12 bar.

40×12:

```text
 ◢◤ ask                                 
────────────────────────────────────────
                                        
────────────────────────────────────────
╭─ Trust this project folder? ─────────╮
│ ❯ Not now                            │
│   … 3 more                           │
╰──────────────────────────────────────╯
   ↑↓ · enter · esc not now             
────────────────────────────────────────
                                     0% 
 Tab blocks · / commands                
```

#### P3. Startup prompt with a check warning

ResolveTrust could not check a gated entry. The state has needsDecision and a warning; the warning is its own block above the explanation, in the warning role, verbatim after `Warning: `.

Regions: row 1 header; row 2 rule; rows 3-13 block pane; row 14 rule; rows 15-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  Warning: cannot check the project files of /Users/kevin/src/acme; the         
  project needs a trust decision                                                
                                                                                
  ~/src/acme has project files that Ask has not read or run:                    
    .ask/skills                                                                 
  If you trust this folder, Ask loads its project settings, system prompt       
  files, skills, prompts, extensions and MCP config.                            
  Trust does not give permission to run shell commands or tools. Trust does     
  not apply to AGENTS.md or CLAUDE.md.                                          
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────╮
│ ❯ Not now              Do not load them now. Ask again in the next session.  │
│   Trust this folder    Load them now and in later sessions.                  │
│   Trust parent folder  Trust ~/src and every folder in it.                   │
│   Do not trust         Do not load them. Do not ask again.                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept           
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                       ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### P4. Startup prompt: highlight moved

After ↓ ↓ the highlight is on Trust parent folder. Only Enter confirms it.

Regions: row 1 header; row 2 rule; rows 3-13 block pane; row 14 rule; rows 15-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ~/src/acme has project files that Ask has not read or run:                    
    .ask/settings.json  .ask/skills  .agents/skills                             
  If you trust this folder, Ask loads its project settings, system prompt       
  files, skills, prompts, extensions and MCP config.                            
  Trust does not give permission to run shell commands or tools. Trust does     
  not apply to AGENTS.md or CLAUDE.md.                                          
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────╮
│   Not now              Do not load them now. Ask again in the next session.  │
│   Trust this folder    Load them now and in later sessions.                  │
│ ❯ Trust parent folder  Trust ~/src and every folder in it.                   │
│   Do not trust         Do not load them. Do not ask again.                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept           
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                       ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### P5. Startup prompt: the save failed

Enter on Trust this folder; SetTrust returned an error. The box stays open with an error row under the options; the highlight stays. No session starts until a choice succeeds or the user takes Not now.

Regions: row 1 header; row 2 rule; rows 3-12 block pane; row 13 rule; rows 14-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ~/src/acme has project files that Ask has not read or run:                    
    .ask/settings.json  .ask/skills  .agents/skills                             
  If you trust this folder, Ask loads its project settings, system prompt       
  files, skills, prompts, extensions and MCP config.                            
  Trust does not give permission to run shell commands or tools. Trust does     
  not apply to AGENTS.md or CLAUDE.md.                                          
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Trust this project folder? ─────────────────────────────────────────────────╮
│   Not now              Do not load them now. Ask again in the next session.  │
│ ❯ Trust this folder    Load them now and in later sessions.                  │
│   Trust parent folder  Trust ~/src and every folder in it.                   │
│   Do not trust         Do not load them. Do not ask again.                   │
│  ✗ Not saved: no answer from the leader in 5s.                               │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept           
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                                                       ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### R1. After Trust this folder

SetTrust(folder, trust), then session/new with no override. The explanation block is replaced by one success note. The draft is back in the prompt pane. The footer row shows the model of the new session.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ✓ Trusted ~/src/acme. Its project files load in this session.                 
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ fix the failing invoice test▌                                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### R2. After Trust parent folder

SetTrust(parent, trust): the leader saves ~/src and removes the entry of ~/src/acme. Then session/new with no override.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ✓ Trusted ~/src and every folder in it. The project files of ~/src/acme load  
    in this session.                                                            
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ fix the failing invoice test▌                                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### R3. After Not now, Esc or ctrl+c

No SetTrust. session/new with _meta["ask.dev/trust"] = "deny". Nothing is saved. At 40×14 the path has CJK characters (2 columns each) and the note wraps at spaces.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  The project files of ~/src/acme are not loaded in this session. The question  
  comes again in the next session. To save a choice, use /trust.                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ fix the failing invoice test▌                                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  The project files of                  
  ~/项目/客户-acme are not loaded in    
  this session. The question comes      
  again in the next session. To save a  
  choice, use /trust.                   
                                        
                                        
────────────────────────────────────────
 ❯ fix the failing invoice test▌        
────────────────────────────────────────
         anthropic/claude-sonnet-5-5 0% 
 Tab blocks · / commands                
```

#### R4. After Do not trust

SetTrust(folder, distrust), then session/new with no override (source "saved").

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  Saved: do not trust ~/src/acme. Its project files are not loaded. To change   
  it, use /trust.                                                               
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### U1. Untrusted start with no prompt: --no-approve

The command line had --no-approve: no _ask/trust/state call, the override rides every session/new of this process. The other no-prompt sources (saved false, saved false on an ancestor, defaultProjectTrust "never", no_ui) use the same one-block layout with the texts of section 8.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  The project files of ~/src/acme are not loaded (--no-approve).                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### W1. Corrupt or unsafe trust.json

_ask/trust/state has source store_error and a warning. No prompt. session/new with no override; the session is untrusted. One warning block, the server text verbatim after `Warning: `. No second untrusted note.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  Warning: trust store /Users/kevin/.ask/trust.json is corrupt or unsafe; no    
  project is trusted until you fix or remove it                                 
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  Warning: trust store                  
  /Users/kevin/.ask/trust.json is       
  corrupt or unsafe; no project is      
  trusted until you fix or remove it    
                                        
                                        
                                        
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
         anthropic/claude-sonnet-5-5 0% 
 Tab blocks · / commands                
```

#### D1. Settings diagnostics after the session starts

_ask/session/state has diagnostics. One block after the trust note: a `Settings problems` row, then the rows of each file (home shown as ~). Paths wrap by columns; texts wrap at spaces under the key. The 40×14 example has one diagnostic and a CJK path.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ✓ Trusted ~/src/acme. Its project files load in this session.                 
                                                                                
  Settings problems                                                             
    ~/src/acme/.ask/settings.json                                               
      defaultProjectTrust: a project file cannot set this key; it is ignored    
      DefaultModel: unknown key; did you mean defaultModel?                     
    ~/.ask/settings.json                                                        
      thinkingFold: must be "expanded", "truncated" or "collapsed"; the         
        default is used                                                         
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  Settings problems                     
    ~/项目/客户-acme/.ask/settings.jso  
    n                                   
      defaultProjectTrust: a project    
        file cannot set this key; it    
        is ignored                      
                                        
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
         anthropic/claude-sonnet-5-5 0% 
 Tab blocks · / commands                
```

#### T0. /trust: waiting for the trust state

/trust asks _ask/trust/state again. Until the answer comes, the rail shows one dim row; the prompt pane and the draft do not change.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ Reading project trust…                                                        
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T1. /trust: saved trust inherited from the parent

/trust opens the box in the prompt pane slot and keeps the draft. The rows come from a fresh _ask/trust/state and the trust of the current session. The highlight starts on the current row. At 40×14 the box shows the options only.

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; rows 11-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Project trust ──────────────────────────────────────────────────────────────╮
│  ~/src/acme                                                                  │
│  Saved: trusted, from ~/src                                                  │
│  This session: trusted                                                       │
│                                                                              │
│   Trust this folder        Save trust for ~/src/acme only.                   │
│ ❯ Trust parent folder      Trust ~/src and every folder in it.      current  │
│   Do not trust             Do not load the project files here.               │
│   Forget the saved choice  Remove the entry of ~/src. Ask again.             │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter save  ·  esc close  ·  applies to the next session       
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; row 3 block pane; row 4 rule; rows 5-11 dialog; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
                                        
────────────────────────────────────────
╭─ Project trust ──────────────────────╮
│   Trust this folder                  │
│ ❯ Trust parent folder       current  │
│   Do not trust                       │
│   Forget saved choice                │
╰──────────────────────────────────────╯
   enter save · esc close               
────────────────────────────────────────
         anthropic/claude-sonnet-5-5 0% 
 Tab blocks · / commands                
```

#### T2. /trust: saved

Enter on Do not trust: SetTrust(folder, distrust). The box closes; the rail shows the result. This session does not change.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Saved: do not trust ~/src/acme · applies to the next session (/new)         
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T3. /trust: corrupt or unsafe trust.json

The box shows the warning and no options. Only Esc acts.

Regions: row 1 header; row 2 rule; rows 3-14 block pane; row 15 rule; rows 16-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Project trust ──────────────────────────────────────────────────────────────╮
│  ✗ trust store /Users/kevin/.ask/trust.json is corrupt or unsafe; no         │
│    project is trusted until you fix or remove it.                            │
│  Ask does not change it. Fix or remove it; the next session reads it again.  │
╰──────────────────────────────────────────────────────────────────────────────╯
   esc close                                                                    
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T4. /trust: the host has no trust methods

An older leader answers unsupported. The rail shows the error text; no box opens.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ /trust is not available: the leader is an older version.                      
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T5. /trust: the save failed

SetTrust returned an error. The box stays open with an error row; the highlight and the draft stay. Forget is not shown because no entry is saved.

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; rows 11-21 dialog; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestInvoiceTotals fail?                                     09:12  
                                                                                
  The test rounds each line before the sum. The fix rounds once, at the end.    
                                                                                
  claude-sonnet-5-5 · 6.1s · ↑9.2k ↓380 · $0.006                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Project trust ──────────────────────────────────────────────────────────────╮
│  ~/src/acme                                                                  │
│  Saved: none                                                                 │
│  This session: not trusted (not now)                                         │
│                                                                              │
│ ❯ Trust this folder    Save trust for ~/src/acme only.                       │
│   Trust parent folder  Trust ~/src and every folder in it.                   │
│   Do not trust         Do not load the project files here.                   │
│  ✗ Not saved: no answer from the leader in 5s.                               │
╰──────────────────────────────────────────────────────────────────────────────╯
   ↑↓ choose  ·  enter save  ·  esc close  ·  applies to the next session       
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/acme                          anthropic/claude-sonnet-5-5  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| S0 waiting for trust | Link open, no command-line flag; `_ask/trust/state` sent | P1 or P3 (needsDecision); W1 (store error); session start with no box (other answers); session start with no box and no note (not available) |
| S0 waiting for trust | Enter in the prompt pane | S0; the rail shows `Starting, please wait…` (existing text); the draft stays. Today `submit` shows this text only while `opening` is true, and `adoptClient` clears `opening` when the link opens. Phase 6 must keep the view in the starting state until the first session exists, so that Enter sends nothing. |
| P1 startup box | ↑ / ↓ | P1 with the highlight moved (P4); it stops at the first and the last row |
| P1 startup box | Enter on Not now; Esc; ctrl+c | Session start with override `deny`; R3 |
| P1 startup box | Enter on Trust this folder | Box locked, `SetTrust` sent; R1 on success; P5 on error |
| P1 startup box | Enter on Trust parent folder | Box locked, `SetTrust` sent; R2 on success; P5 on error |
| P1 startup box | Enter on Do not trust | Box locked, `SetTrust` sent; R4 on success; P5 on error |
| P1 startup box | ctrl+d | Program ends; no `SetTrust`, no `session/new` |
| P1 startup box | Printable key, paste, Tab, mouse | P1 unchanged; the draft unchanged |
| P1 startup box | Resize | P1 at the new size; highlight kept (section 7) |
| P1 locked | `SetTrust` answer | R1, R2 or R4; on error P5 |
| P1 locked | Any key except ctrl+d | Dropped |
| P5 save failed | ↑ / ↓ / Enter / Esc | As P1; the error row goes at the next key |
| P1 from `/new` | `/new` and needsDecision | Same box; the block pane keeps what `/new` shows today, the explanation block comes after it |
| R1–R4 | `_ask/session/state` arrives | Result note stays; D1 when diagnostics exist |
| Session start, no box | Session trust is untrusted | U1 note with the text for its source (section 8, table B) |
| Session start, no box | Session trust is trusted, or the host omits trust | No note |
| W1 store warning | Session starts untrusted (`store_error`) | Warning block only; no untrusted note |
| Any session | `/trust` | T0, then T1 (answer), T3 (store error), T4 (not available) |
| T0 waiting | Answer | T1 / T3 / T4; an answer of an older generation is dropped |
| T1 `/trust` box | ↑ / ↓ | T1 with the highlight moved |
| T1 `/trust` box | Enter on the `current` row | Box closes; rail `No change.` (dim); no `SetTrust` |
| T1 `/trust` box | Enter on another row | Box locked, `SetTrust` sent; T2 on success; T5 on error; T3 when the host answers the store warning |
| T1 `/trust` box | Esc, ctrl+c | Box closes; no change |
| T5 save failed | Keys | As T1 |
| T2 saved | Notice time ends | Rail empty; the current session does not change |

Stale results: each trust call carries the request generation. A late answer of a closed box, an older `/trust` call or an older session generation changes nothing.

## 6. Keys and focus

Input order (architecture doc): exit and cancel first, then the active dialog, then completion, then the editor, then application shortcuts.
The two trust boxes are dialogs. While a trust box is open it takes every key; the editor, the block list and the application shortcuts (`ctrl+t`, `ctrl+l`, `ctrl+End`, `Tab`) get nothing.

| Key | Receiver | Startup box (P1–P5) | `/trust` box (T1, T3, T5) |
|---|---|---|---|
| ctrl+d | quit flow | Quit. No `SetTrust`, no `session/new`. | Quit (existing quit rule). |
| ctrl+c | dialog first | Same as Esc. A later ctrl+c follows the quit rule. | Close, no change. |
| Esc | dialog | Not now: override `deny`, nothing saved. | Close, no change. |
| Enter | dialog | Confirm the highlighted row. | Save the highlighted row; `No change.` on the `current` row. Ignored in T3. |
| ↑ / ↓ | dialog | Move the highlight; stop at the ends. | Same. Ignored in T3. |
| Printable keys (`j`, `k`, `y`, `n`, digits, space), paste | dialog | Dropped. The draft does not change. | Dropped. |
| Tab, Home, End, PgUp, PgDn | dialog | Dropped. | Dropped. |
| Mouse (wheel, click, drag, motion) | none | Ignored, as for every box. | Ignored. |
| Any key while locked | dialog | Dropped, except ctrl+d. | Dropped, except ctrl+d. |

After the box closes, focus goes back to the prompt.

## 7. Limited space and resize

The slot rule does not change: a box takes at most half of the screen height and at most 14 rows (`modalMaxRows`); the block pane keeps one row.

What gives way first, in order:

1. Under 60 columns, the description column of the box goes; the labels stay whole. The `/trust` box also drops the folder row, the `Saved:` row, the `This session:` row and the blank row; the `current` tag stays.
2. Under 60 columns, or when the long hint does not fit, the hint row takes its short form: `↑↓ · enter · esc not now` (startup) and `enter save · esc close` (`/trust`). The long form drops `your draft is kept` first.
3. When the box rows do not fit the slot, the option rows become a window that keeps the highlighted row; the last row is `  … <n> more` in `muted` (as the provider box). At 40×12 only the highlighted row and `… 3 more` show (P2n).
4. The explanation block follows the normal pane rule: the pane follows the end, so its top rows go first. At 40×14 the pane has one row (the gap row), so the explanation does not show. The title and the labels must say enough alone.
5. In the explanation, more than 4 file rows show as 3 rows and `    … <n> more`, where `<n>` counts the hidden entries.

Resize keeps the box, the highlight, the lock and the draft. The text wraps again at the new width. Crossing 60 columns adds or removes the description column. Trust state never changes on resize.

## 8. Text and width

All texts below are the contract. `{root}` is the canonical root of the cwd (`root` of `_ask/trust/state`, else the TUI cwd). `{parent}` is its parent folder. `{entry}` is `entryPath`.

**Paths.** The TUI shows a path field (root, parent, entry, a gated entry, a diagnostic path) with the user home as `~`. The `warning` field of `_ask/trust/state` and the `text` of a diagnostic are server texts: the TUI shows them verbatim and does not change the path in them. A path in the block pane wraps by columns and is never cut. A path in a box description shortens to `~/…/<last element>` (the footer row rule); when the row is still too wide, the description is cut with `…` at the end. Control characters (C0, C1, DEL) and tabs in a path or a server text show as `?`. A path never writes a terminal sequence.

**Wide characters.** Width counts columns: a CJK character and most emoji use 2. A row never ends in the middle of a wide character. R3 and D1 at 40×14 show CJK paths.

**Truncation marker.** `…` everywhere.

### A. Startup box and explanation

| Item | Text |
|---|---|
| Box title | `Trust this project folder?` |
| Explanation, row 1 | `{root} has project files that Ask has not read or run:` |
| Explanation, files | Relative entries (`.ask/settings.json`, `.ask/skills`, …) joined by two spaces, at column 4. An ancestor entry is an absolute path with `~`, one per row. |
| Explanation, body | `If you trust this folder, Ask loads its project settings, system prompt files, skills, prompts, extensions and MCP config.` |
| Explanation, limits | `Trust does not give permission to run shell commands or tools. Trust does not apply to AGENTS.md or CLAUDE.md.` |
| Not now | `Do not load them now. Ask again in the next session.` |
| Trust this folder | `Load them now and in later sessions.` |
| Trust parent folder | `Trust {parent} and every folder in it.` |
| Do not trust | `Do not load them. Do not ask again.` |
| Hint (long / short) | `↑↓ choose  ·  enter confirm  ·  esc not now  ·  your draft is kept` / `↑↓ · enter · esc not now` |
| Save error row | `✗ Not saved: {error text}` |

The text never says that `AGENTS.md` or `CLAUDE.md` is blocked or loaded. Ask does not load them today (`internal/bootstrap` is a scaffold), and they are never gated.

### B. Notes after the session starts

The TUI picks the first row that matches. "Sent" is the TUI's own record of the start.

| # | Case | Role | Text |
|---|---|---|---|
| B1 | Sent: prompt choice Trust this folder | `success` `✓`, text `fg` | `✓ Trusted {root}. Its project files load in this session.` |
| B2 | Sent: prompt choice Trust parent folder | `success` `✓`, text `fg` | `✓ Trusted {parent} and every folder in it. The project files of {root} load in this session.` |
| B3 | Sent: prompt choice Do not trust | `muted` | `Saved: do not trust {root}. Its project files are not loaded. To change it, use /trust.` |
| B4 | Sent: Not now, Esc or ctrl+c | `muted` | `The project files of {root} are not loaded in this session. The question comes again in the next session. To save a choice, use /trust.` |
| B5 | Sent: `--no-approve` | `muted` | `The project files of {root} are not loaded (--no-approve).` |
| B6 | Source `saved`, untrusted | `muted` | `The project files of {root} are not loaded: this folder is not trusted. To change it, use /trust.` |
| B7 | Source `saved_ancestor`, untrusted | `muted` | `The project files of {root} are not loaded: {entry} is not trusted. To change it, use /trust.` |
| B8 | Source `default_never` | `muted` | `The project files of {root} are not loaded: defaultProjectTrust is "never". To trust this folder, use /trust.` |
| B9 | Source `no_ui` (the trust check failed) | `warning` | `The project files of {root} are not loaded: Ask could not check the trust of this folder ({error text}).` |
| B10 | Source `store_error` | none | No note. The warning block (W1) says it. |
| B11 | Trusted by `--approve`, saved, saved ancestor, `default_always`, `no_gated_files` | none | No note. |
| B12 | Host omits `trust` (older leader) | none | No note. |
| W | `warning` of `_ask/trust/state` is not empty | `warning` | `Warning: {warning}` (verbatim) |

Source `prompt` never reaches a session: the leader decides with no UI at `session/new`.

### C. `/trust`

| Item | Text |
|---|---|
| Completion row | `/trust   Trust or do not trust this folder` |
| Box title | `Project trust` |
| Folder row | `{root}` (bold) |
| Saved row | `Saved: none` / `Saved: trusted` / `Saved: not trusted` / `Saved: trusted, from {entry}` / `Saved: not trusted, from {entry}` (`from` when `entryPath` is not the root) |
| Session row | `This session: trusted` or `This session: not trusted`, then the reason in parentheses: `(--approve)`, `(--no-approve)`, `(not now)`, `(no project files)`, `(default always)`, `(default never)`, `(trust store error)`; `This session: unknown` when the host omits trust |
| Trust this folder | `Save trust for {root} only.` |
| Trust parent folder | `Trust {parent} and every folder in it.` (hidden as in C7) |
| Do not trust | `Do not load the project files here.` |
| Forget the saved choice (short: `Forget saved choice`) | `Remove the entry of {entry}. Ask again.` Shown only when `entryPath` is the root (`clear` on `folder`) or the parent (`clear` on `parent`). |
| `current` tag | On the row that matches the saved entry: root trusted, parent trusted, or root not trusted |
| First highlight | The `current` row; with no `current` row, **Do not trust** |
| Hint (long / short) | `↑↓ choose  ·  enter save  ·  esc close  ·  applies to the next session` / `enter save · esc close` |
| Waiting rail | `┊ Reading project trust…` (dim) |
| Result rail | `✓ Saved: {what} · applies to the next session (/new)`; with a command-line flag in this process: `✓ Saved: {what} · {flag} still applies in this process` |
| `{what}` | `trust {root}` / `trust {parent} and every folder in it` / `do not trust {root}` / `removed the saved choice of {entry}` |
| No change | `No change.` (dim) |
| Store error box | `✗ {warning}.` then `Ask does not change it. Fix or remove it; the next session reads it again.` (`muted`); hint `esc close` |
| Not available rail | `/trust is not available: the leader is an older version.` (`error`) |
| Save error row | `✗ Not saved: {error text}` |

### D. Settings problems

`Settings problems` (`warning`, bold), then for each file: the path at column 4 (`muted`), then each diagnostic at column 6: `{key}: ` (`fg` bold) and the text (`muted`); continuation rows at column 8. A diagnostic with no key shows its text only. User file first or project file first follows the order of `diagnostics`. At most 8 item rows; then `    … <n> more · ask settings show` (`muted`).

## 9. Style intent

Roles only; the theme owns the colors (`docs/tui-style-guide.md`).

- Box: lines in `border`; title in `accent`, bold; `❯` in `accent`, bold; the highlighted row on the `selected` background with its label in bold `fg`; descriptions and the `current` tag in `muted`; an error row in `error` with `✗`; the hint row in `dim` italic. This is the row menu of `TestProviderRowMenu`.
- Explanation block: the folder in bold `fg`; sentences in `fg`; file entries in `code`; `… <n> more` in `muted`.
- Warning block (W, P3): `warning`.
- Notes: B1 and B2 use `✓` in `success` and `fg` text; B3 to B8 use `muted`; B9 uses `warning`.
- Settings problems: header `warning` bold; paths `muted`; keys bold `fg`; texts `muted`.
- Rail: the result uses `✓` in `success` and a `dim` text (existing `setResult`); the not-available text uses `error`.
- No color: the highlighted row uses reverse and keeps `❯`; the warning block is bold; every state stays clear with bold, dim and glyphs.

No new role and no new glyph: `❯ ✓ ✗ … ┊ ╭ ╮ ╰ ╯ │ ─` are in the style guide.

## 10. Acceptance criteria

Each item names the phase 6 test that pins it. "Fake client" is `fakeClient` in `internal/tui/session_rig_test.go`. Goldens live under `internal/tui/testdata/<TestName>/`.

1. At 80×24, after start with a fake client whose `TrustState` returns `needsDecision` for `~/src/acme` with gated `.ask/settings.json`, `.ask/skills`, `.agents/skills`, the screen shows sketch P1: `╭─ Trust this project folder? ─` in the dialog slot, `❯ Not now` on the `selected` row, and `~/src/acme has project files that Ask has not read or run:` in the block pane. Test: `TestTrustPromptShownForUndecidedProject` (golden `prompt_80x24`).
2. At 120×40 and at 40×14, after the same start, the screen equals sketch P1 at that size; at 40×14 the box shows the four labels and no description. Test: `TestTrustPromptShownForUndecidedProject` (goldens `prompt_120x40`, `prompt_40x14`).
3. At 80×24, after the same start, the block pane shows `Trust does not give permission to run shell commands or tools.` and `Trust does not apply to AGENTS.md or CLAUDE.md.`, and no row of the screen has the word `blocked`. Test: `TestTrustPromptTextNamesLimits`.
4. At 80×24, after Enter with no other key, the fake client records no `SetTrust` call and one `NewSession` with override `deny`, and the block pane shows note B4. Test: `TestTrustPromptCancelIsUntrustedSession` (subtest `enter`).
5. At 80×24, after Esc, and in a second run after ctrl+c, the result is the same as item 4. Test: `TestTrustPromptCancelIsUntrustedSession` (subtests `esc`, `ctrl_c`).
6. At 80×24, the session-only choice is Not now: it records no `SetTrust` and `NewSession` with override `deny`. Phase 6 test `TestTrustPromptSessionOnlyChoiceSendsOverride` checks this case; there is no `approve` session-only row (section 11, decision 1).
7. At 80×24, after ↓ and Enter, the fake client records `SetTrust{cwd, folder, trust}` and then `NewSession` with no override, the block pane shows `✓ Trusted ~/src/acme. Its project files load in this session.` and the prompt pane shows the draft again (sketch R1). Test: `TestTrustPromptChoiceSavesAndStartsSession` (subtest `folder`).
8. At 80×24, after ↓ ↓ Enter, the fake client records `SetTrust{cwd, parent, trust}`, and the block pane shows note B2 (sketch R2). Test: `TestTrustPromptChoiceSavesAndStartsSession` (subtest `parent`).
9. At 80×24, after ↓ ↓ ↓ Enter, the fake client records `SetTrust{cwd, folder, distrust}`, and the block pane shows note B3 (sketch R4). Test: `TestTrustPromptChoiceSavesAndStartsSession` (subtest `distrust`).
10. At 80×24, after the keys `j`, `k`, `1`, `y`, space, a paste of `trust`, Tab and a click on the row `Trust this folder`, the highlight stays on `Not now`, the fake client records no call, and after Esc the draft is the draft from before the box. Test: `TestTrustPromptIgnoresTypedKeysAndMouse`.
11. At 80×24, after ctrl+d in the startup box, the program ends, and the fake client records no `SetTrust` and no `NewSession`. Test: `TestTrustPromptQuitStartsNoSession`.
12. At 80×24, when `SetTrust` returns an error after ↓ Enter, the box stays open with `✗ Not saved: ` in its last row and the highlight on `Trust this folder`, and no `NewSession` is recorded (sketch P5). Test: `TestTrustPromptSaveErrorKeepsBox`.
13. At 80×24, with the command-line override `deny`, the fake client records no `TrustState` call, no box shows, every `NewSession` carries `deny`, and the block pane shows note B5 (sketch U1). With `approve`, no note shows. Test: `TestTrustPromptSkippedWithFlag`.
14. At 80×24, when `TrustState` returns `ErrTrustNotAvailable`, no box and no trust note show, and `NewSession` has no override. Test: `TestTrustPromptSkippedForOldLeader`.
15. At 80×24, when `TrustState` returns another error, no box shows, `NewSession` has no override, and the session source `no_ui` gives note B9 with the error text. Test: `TestTrustCheckFailureStartsUntrusted`.
16. At 80×24 and 40×14, when `TrustState` returns source `store_error` and a warning, no box shows and the block pane shows `Warning: trust store ` and the rest of the warning, and no second note (sketch W1). Test: `TestTrustStoreWarningShown`.
17. At 80×24, when `TrustState` returns `needsDecision` and a warning, the warning block shows above the explanation (sketch P3). Test: `TestTrustPromptShowsCheckWarning`.
18. At 80×24, with the long root and the gated list of sketch P2, the root wraps by columns with no `…`, and the parent row says `Trust ~/…/services and every folder in it.` Test: `TestTrustPromptLongPath` (golden `long_80x24`).
19. At 40×12, after the same start, the box shows `❯ Not now` and `… 3 more`; after ↓ it shows `❯ Trust this folder`. Test: `TestTrustPromptLongPath` (goldens `window_40x12`, `window_40x12_down`).
20. At 40×14, after Not now in the CJK folder `~/项目/客户-acme`, the block pane shows note B4 wrapped at spaces with every row 40 columns wide (sketch R3). Test: `TestTrustNoteCJKPath`.
21. At 80×24, after `/new` in a session, the fake client records a second `TrustState` call; when it returns `needsDecision`, the box shows again. Test: `TestNewCommandAsksTrustAgain`.
22. At 80×24, after Not now and a failed first `NewSession`, the next prompt calls `NewSession` with override `deny` and no box shows. Test: `TestLazySessionReusesTrustChoice`.
23. At 80×24, for each session source of table B (B5 to B9, B11, B12), the block pane shows the text of that row, or no note. Test: `TestUntrustedNoticeShowsSource` (one subtest for each row).
24. At 80×24 and 40×14, when `_ask/session/state` has the diagnostics of sketch D1, the block pane shows `Settings problems`, the paths with `~`, and each key and text. With 9 item rows, the block shows 8 and `… 1 more · ask settings show`. Test: `TestSettingsDiagnosticsNotice`.
25. At 80×24, after `/trust` with a saved trusted entry on `~/src`, the dialog slot shows `Saved: trusted, from ~/src`, `This session: trusted`, the four rows, and `❯ Trust parent folder` with `current` (sketch T1). At 40×14 it shows the four short labels only. Test: `TestTrustCommandShowsInheritedEntry`.
26. At 80×24, after `/trust`, ↓ and Enter on `Do not trust`, the fake client records `SetTrust{cwd, folder, distrust}` and no `NewSession`, and the status row shows `✓ Saved: do not trust ~/src/acme · applies to the next session (/new)` (sketch T2). Test: `TestTrustCommandAppliesToNextSession`.
27. At 80×24, in a process with the override `approve`, after a `/trust` save, the status row shows `--approve still applies in this process`. Test: `TestTrustCommandAppliesToNextSession` (subtest `flag`).
28. At 80×24, after `/trust` and Enter on the `current` row, the fake client records no `SetTrust`, and the status row shows `No change.` Test: `TestTrustCommandNoChange`.
29. At 80×24, `Forget the saved choice` shows only when `entryPath` is the root or its parent, and Enter on it records `SetTrust` with decision `clear` and the matching target. Test: `TestTrustCommandForgetTarget`.
30. At 80×24, after `/trust` when the state source is `store_error`, the box shows the warning and no option rows, and Enter does nothing (sketch T3). Test: `TestTrustCommandStoreError`.
31. At 80×24, after `/trust` when `TrustState` returns `ErrTrustNotAvailable`, no box opens and the status row shows `/trust is not available: the leader is an older version.` (sketch T4). Test: `TestTrustCommandNotAvailable`.
32. At 80×24, after a `/trust` save that fails, the box stays open with `✗ Not saved: ` and the highlight (sketch T5). Test: `TestTrustCommandSaveError`.
33. At 80×24, when the root is `/srv` (parent `/`), or the root is `~/acme` (parent is the user home), the startup box and the `/trust` box have no `Trust parent folder` row. Test: `TestTrustParentHiddenAtHomeAndRoot`.
34. At 80×24 with the startup box open and the highlight on `Trust this folder`, after a resize to 40×14 and back, the highlight stays and the description column goes and comes back. Test: `TestTrustPromptResizeKeepsHighlight`.
35. At 80×24, with the user `thinkingFold` `expanded` and a session state `thinkingFold` `collapsed`, after a reply with thinking, the block pane shows the collapsed row `◆ Thought for`. Test: `TestThinkingFoldFromSessionState`.

## 11. Decisions

The user approved the design on 2026-11-04 with every recommendation of the review.

1. **C1 choices.** Decided (user, 2026-11-04): four choices: Not now, Trust this folder, Trust parent folder, Do not trust. No "Trust this session only" row.
2. **C2 default.** Decided (user, 2026-11-04): Not now is the first row and is highlighted (safe, nothing saved).
3. **C3 cancel keys.** Decided (user, 2026-11-04): Esc and ctrl+c mean Not now; ctrl+d quits with no session.
4. **C7 parent row.** Decided (user, 2026-11-04): hide Trust parent folder when the parent is the user home; it is always hidden for `/`.
5. **AGENTS.md wording.** Decided (user, 2026-11-04): the text is "Trust does not apply to AGENTS.md or CLAUDE.md." The text does not say that these files always load.
6. **C6 placement.** Decided (user, 2026-11-04): the explanation is in the block pane and the choices are in the box. At 40×14 the explanation does not show; this is accepted.
7. **`/trust` first highlight.** Decided (user, 2026-11-04): the `current` row; with no `current` row, Do not trust.
8. **C10 diagnostics cap.** Decided (user, 2026-11-04): 8 item rows, then `… <n> more · ask settings show`.
9. **Gated list: `models.json`.** Decided (user, 2026-11-04): `.ask/models.json` is gated. The list in `internal/workspace/project.go` includes it, and the file list of sketch P2 shows it.
10. **Store warning with a flag.** Decided (user, 2026-11-04): accepted. With `--approve` or `--no-approve` the TUI does not call `_ask/trust/state`, so it shows no trust store warning.
11. **`--no-approve` note in a plain folder.** Decided (user, 2026-11-04): accepted. Note B5 shows also in a folder with no gated files.
12. **Client-local notes.** Decided (user, 2026-11-04): accepted. A client that attaches later or restores a view does not show the trust notes or the settings problems.
