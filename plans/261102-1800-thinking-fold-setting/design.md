---
title: "Thinking and reply look TUI design"
design-status: approved     # draft | approved
design-approved-by: "Kevin Le (user, in the Claude Code session; interactive mockup and the general rules R1-R4)"
design-approved-on: "2026-11-02"
---

# Thinking and reply look TUI design

Preview: [artifacts/preview.html](artifacts/preview.html). Rebuild this file and the preview with
`python3 artifacts/make-design.py` from this directory; the sketches come from `artifacts/sketch-gen.py`.
The four style options that the user compared are in [artifacts/style-options.html](artifacts/style-options.html); the user chose option A.
The interactive mockup that the user reviewed is [artifacts/mockup.html](artifacts/mockup.html).

## 1. Outcome

The block pane looks like Grok CLI (style option A). Four general rules apply to every block; no block type has a rule of its own.

| # | Rule | Applies to |
|---|---|---|
| R1 | **Spacing.** Exactly one blank row between any two blocks, and one blank row after the last block. | All blocks |
| R2 | **Selection.** A selected block gets a thin box in the `border` role: `│` in the first and the last column of each block row, `┌ ┐` on the blank row above, `└ ┘` on the blank row below (Grok's `SelectionBox`). The box never adds a row. When scrolling clips the block, the clipped edge row shows `┆`. | All blocks: prompt, thinking, tool, hook, reply |
| R3 | **Step mark.** A step shows `›` when it is open (Truncated or Expanded, also while it streams) or selected. Its own mark (`◆`, or `⚓` for a hook) shows only when it is collapsed and not selected. The mark keeps its role color. | All steps: thinking, tool, hook |
| R4 | **Open step.** An open step is its header row, one blank row, then its body at column 4. There is no rail. | All steps |

In addition:

- Thinking text is in the `thinking` role, not italic. An ended thinking step shows all its lines by default. The key `thinkingFold` in `<ASK_HOME>/settings.json` sets the mode of an ended step: `"expanded"` (the default), `"truncated"` or `"collapsed"`. `ctrl+t` sets the mode for later thinking steps in the same TUI process, as Grok's `Ctrl+E` does.
- Replies render markdown without raw syntax: headings without `#`, in a color for each level, with one blank row under each; inline code in blue bold without backticks; `•` bullets; tables in a dim box; code blocks on the code panel without a language row.
- The first row of a reply shows its 24h time at the right, as the prompt does, only when that row is a text row. A reply that starts with a table or a code block shows no time.
- The model's text before a tool call is a reply block of its own (bright text), not part of the thinking.

## 2. Non-goals

- No copy row under tables (`◇ table [Copy TSV] [Copy CSV] [Copy Markdown]`). A later design can add it.
- No change to the text of tool and hook rows (verbs, details, exit marks), the prompt band colors, the viewer, the status row, the footer row or the shortcut bar. Only R1-R4 change their layout.
- No grouping of consecutive tool rows (Grok's `Explored 2 files`).
- No new colors. h2 and inline code use `#7aa2f7`, the value of `prov-4` today, under a new role name.
- No slash command or selector for `thinkingFold`, and no write to `settings.json` from the TUI.
- No option to hide thinking, and no change to `ask -p`, `ask connect`, ACP or the leader.
- No file watch: a change to `settings.json` takes effect at the next start of the TUI.
- No markdown images, footnotes or HTML. Glamour's behavior for them stays.

## 3. Placement

All changes are in the block pane:

| Part | Owner today | Change |
|---|---|---|
| Gaps between blocks (R1) | pane layout (`internal/tui/pane.go`, block spacing in `blocks.go`) | One blank row between any two blocks and after the last |
| Selection (R2) | `blocks_mark.go` (accent `┃` gutter, `selected` shade) | Thin box for every selected block; the gutter and the shade of a selected step go |
| Step mark (R3) | `stepRow` and `indicatorFor` (`blocks_step.go`), hook rows (`blocks_hook.go`) | `›` when open or selected |
| Open step body (R4) | `railRows` (`blocks_step.go`), tool forms (`blocks_preview.go`) | Header, blank row, body at column 4, no rail |
| Thinking fold on end | `thinkingBlock.collapseFold` | Takes the session mode (from `thinkingFold`, then `ctrl+t`) |
| Reply text | `markdown.go` (`markdownStyle`, glamour), `markdown_code.go` | Style A markdown (section 9) |
| Reply first row | `blocks.go` reply block | 24h time at the right, `dim` |

`docs/tui-style-guide.md` changes: principle 2 and the `┃` glyph row (no rail for thinking or selection), principle 6 (exception for ended thinking; R1 spacing), the step patterns (R3, R4), the selection rule (R2), the role table (`h1`, `h2`, `h3`), and the markdown section.

## 4. Sketches

The first example turn has six lines of thinking and a one-line reply. The markdown example explains `tool_coordinator.go`. The model is `anthropic/claude-sonnet-5-5`.

#### T1. Streaming: the last lines, for every setting

While the thinking streams, the step is open: › Thinking…, one blank row, then … and the last 3 lines at column 4 in the thinking color. There is no rail. The setting does not change this state.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ run the tests and fix what fails                                     15:31  
                                                                                
  › Thinking… 2s                                                               ⠋
                                                                                
    …                                                                           
    parser.go splits on commas and counts every part.                           
    An empty part still adds one to the count.                                  
    So the loop must skip f == "".                                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T2. Ended with thinkingFold "expanded" (the default)

When the thinking ends, the step stays open: › Thought, one blank row, all its lines, no rail. One blank row separates every two blocks. The reply has the 24h time at the right of its first row.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ run the tests and fix what fails                                     15:31  
                                                                                
  › Thought for 4.2s                                                            
                                                                                
    The test fails on an empty field.                                           
    parser.go splits on commas and counts every part.                           
    An empty part still adds one to the count.                                  
    So the loop must skip f == "".                                              
    Then TestCount gets 2, not 3.                                               
    I will edit Count and run the tests again.                                  
                                                                                
  Count now skips empty fields, and go test ./... passes.                15:31  
  claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
    I will edit Count and run the       
    tests again.                        
                                        
  Count now skips empty fields,  15:31  
  and go test ./... passes.             
  claude-sonnet-5-5                     
                                        
────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl…
────────────────────────────────────────
 ⌂ ~/src/p anthropic/claude-sonnet-5-5 …
 Tab blocks · / commands                
```

#### T3. Ended with thinkingFold "collapsed"

When the thinking ends, the step folds to one row with ◆.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ run the tests and fix what fails                                     15:31  
                                                                                
  ◆ Thought for 4.2s                                                            
                                                                                
  Count now skips empty fields, and go test ./... passes.                15:31  
  claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T4. Ended with thinkingFold "truncated"

When the thinking ends, the step keeps … and its last 3 lines.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ run the tests and fix what fails                                     15:31  
                                                                                
  › Thought for 4.2s                                                            
                                                                                
    …                                                                           
    So the loop must skip f == "".                                              
    Then TestCount gets 2, not 3.                                               
    I will edit Count and run the tests again.                                  
                                                                                
  Count now skips empty fields, and go test ./... passes.                15:31  
  claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T5. ctrl+t collapsed all, so the next thinking ends collapsed

The setting is "expanded". After ctrl+t folds all thinking, the second turn's thinking ends collapsed too.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ run the tests and fix what fails                                     15:31  
                                                                                
  ◆ Thought for 4.2s                                                            
                                                                                
  Count now skips empty fields, and go test ./... passes.                15:31  
  claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007                                
                                                                                
  ❯ now add a test for a trailing comma                                  15:33  
                                                                                
  ◆ Thought for 1.8s                                                            
                                                                                
  Added TestCountTrailingComma; it passes.                               15:33  
  claude-sonnet-5-5 · 5.1s · ↑9.0k ↓220 · $0.004                                
                                                                                
                                                                                
                                                                                
┊ Thinking blocks: collapsed · ctrl+t to expand                                 
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### T6. ctrl+t expanded all, so the next thinking ends expanded

The setting is "collapsed". After ctrl+t opens all thinking, the second turn's thinking ends expanded.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
    Then TestCount gets 2, not 3.                                               
    I will edit Count and run the tests again.                                  
                                                                                
  Count now skips empty fields, and go test ./... passes.                15:31  
  claude-sonnet-5-5 · 9.4s · ↑8.1k ↓410 · $0.007                                
                                                                                
  ❯ now add a test for a trailing comma                                  15:33  
                                                                                
  › Thought for 1.8s                                                            
                                                                                
    A trailing comma gives one empty part at the end.                           
    The new test wants 2 for "a,b,".                                            
                                                                                
  Added TestCountTrailingComma; it passes.                               15:33  
  claude-sonnet-5-5 · 5.1s · ↑9.0k ↓220 · $0.004                                
                                                                                
┊ Thinking blocks: expanded · ctrl+t to collapse                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### M1. Reply markdown in style A

Headings without #, in h1/h2/h3 colors and bold, with one blank row under each. Inline code in blue bold with no backticks. A muted • for bullets and muted numbers. Tables in a dim box with a bold header. A code block on the code panel with no language row. At 80×24 the pane follows the end; at 120×40 the whole turn shows.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  │ updater  │ Progress that never blocks │                                     
  └──────────┴────────────────────────────┘                                     
                                                                                
  The batch loop                                                                
                                                                                
  1. Find the last assistant entry in the log.                                  
  2. MaxParallelTools (default 10) limits the workers.                          
  3. Commit in order: progress, afterTool, emitToolEnd.                         
                                                                                
  Safety                                                                        
                                                                                
  • toolGuarded turns a panic into an error.                                    
  • cloneToolResult deep-copies before emit.                                    
                                                                                
    body, e = p.tool.Execute(bodyCtx, tc, args)                                 
  claude-sonnet-5-5 · 14s · ↑10.2k ↓1.8k · $0.012                               
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-35 block pane; row 36 rule; row 37 prompt pane; row 38 rule; row 39 footer; row 40 bar.

120×40:

```text
 ◢◤ ask                                                                                                                 
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                                                                                        
    The user wants a summary of tool_coordinator.go.                                                                    
    I will group it: the types, the batch loop, and the safety parts.                                                   
                                                                                                                        
  ◆ Read internal/agent/tool_coordinator.go                                                                             
                                                                                                                        
  tool_coordinator.go runs one batch of tool calls: it prepares them, runs them in parallel with a limit, and    17:30  
  commits them in order.                                                                                                
                                                                                                                        
  Types                                                                                                                 
                                                                                                                        
  ┌──────────┬────────────────────────────┐                                                                             
  │ Type     │ Job                        │                                                                             
  ├──────────┼────────────────────────────┤                                                                             
  │ toolSlot │ State of one call          │                                                                             
  │ toolDone │ A worker is done           │                                                                             
  │ updater  │ Progress that never blocks │                                                                             
  └──────────┴────────────────────────────┘                                                                             
                                                                                                                        
  The batch loop                                                                                                        
                                                                                                                        
  1. Find the last assistant entry in the log.                                                                          
  2. MaxParallelTools (default 10) limits the workers.                                                                  
  3. Commit in order: progress, afterTool, emitToolEnd.                                                                 
                                                                                                                        
  Safety                                                                                                                
                                                                                                                        
  • toolGuarded turns a panic into an error.                                                                            
  • cloneToolResult deep-copies before emit.                                                                            
                                                                                                                        
    body, e = p.tool.Execute(bodyCtx, tc, args)                                                                         
  claude-sonnet-5-5 · 14s · ↑10.2k ↓1.8k · $0.012                                                                       
                                                                                                                        
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                                                         
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                                                                     anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                                                               
```

#### S1. The reply is selected: thin box

Tab, then k to the reply. A │ in the first and the last column of each reply row; ┌ ┐ on the blank row above and └ ┘ on the blank row below. No row is added. The accent ┃ gutter is gone.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
┌                                                                              ┐
│ tool_coordinator.go runs one batch of tool calls: it prepares them,    17:30 │
│ runs them in parallel with a limit, and commits them in order.               │
│                                                                              │
│ Types                                                                        │
│                                                                              │
│ ┌──────────┬────────────────────────────┐                                    │
│ │ Type     │ Job                        │                                    │
│ ├──────────┼────────────────────────────┤                                    │
│ │ toolSlot │ State of one call          │                                    │
│ │ toolDone │ A worker is done           │                                    │
│ │ updater  │ Progress that never blocks │                                    │
│ └──────────┴────────────────────────────┘                                    │
│                                                                              │
│ The batch loop                                                               │
│                                                                              │
│ 1. Find the last assistant entry in the log.                                 │
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 j/k select · e fold · Enter open · Tab prompt                                  
```

#### S2. An open thinking step is selected

The box goes around the open step: header, blank row, body. ┌ ┐ on the blank row above, └ ┘ on the blank row below, before the Read step.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ explain tool_coordinator.go                                          17:30  
┌                                                                              ┐
│ › Thought for 2.1s                                                           │
│                                                                              │
│   The user wants a summary of tool_coordinator.go.                           │
│   I will group it: the types, the batch loop, and the safety parts.          │
└                                                                              ┘
  ◆ Read internal/agent/tool_coordinator.go                                     
                                                                                
  tool_coordinator.go runs one batch of tool calls: it prepares them,    17:30  
  runs them in parallel with a limit, and commits them in order.                
                                                                                
  Types                                                                         
                                                                                
  ┌──────────┬────────────────────────────┐                                     
  │ Type     │ Job                        │                                     
  ├──────────┼────────────────────────────┤                                     
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 j/k select · e fold · Enter open · Tab prompt                                  
```

#### S3. A collapsed thinking step is selected

A one-row block gets the same box: ┌ ┐ on the blank row above, │ beside the row, └ ┘ on the blank row below. The glyph is › because the step is selected.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ explain tool_coordinator.go                                          17:30  
┌                                                                              ┐
│ › Thought for 2.1s                                                           │
└                                                                              ┘
  ◆ Read internal/agent/tool_coordinator.go                                     
                                                                                
  tool_coordinator.go runs one batch of tool calls: it prepares them,    17:30  
  runs them in parallel with a limit, and commits them in order.                
                                                                                
  Types                                                                         
                                                                                
  ┌──────────┬────────────────────────────┐                                     
  │ Type     │ Job                        │                                     
  ├──────────┼────────────────────────────┤                                     
  │ toolSlot │ State of one call          │                                     
  │ toolDone │ A worker is done           │                                     
  │ updater  │ Progress that never blocks │                                     
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/p                             anthropic/claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%
 j/k select · e fold · Enter open · Tab prompt                                  
```

## 5. States and transitions

"Session mode" is the fold mode that an ended thinking step takes. It starts as the value of `thinkingFold` when the TUI starts.

| State | Trigger | Next state |
|---|---|---|
| No thinking step | `thinking_start` event | T1: step streams, Truncated, mark `›` (R3) |
| T1, not set by the user | `thinking_end` or the end of the message | The session mode: T2 (expanded, `›`), T3 (collapsed, `◆`) or T4 (truncated, `›`) |
| T1, the user set its fold (`e`, `→`, `←`, `ctrl+t`) | `thinking_end` | Keeps the fold the user set (as today) |
| Any step | `e` `E` `→` `←` `l` `h` on it | Fold changes as today; R3 and R4 follow the new fold; the session mode does not change |
| Any | `ctrl+t` (or `ctrl+e` in the list); some thinking step is not Expanded | All thinking Expanded; session mode = expanded; notice `Thinking blocks: expanded · ctrl+t to collapse` (T6) |
| Any | `ctrl+t`; every thinking step is Expanded, or there is none | All thinking Collapsed; session mode = collapsed; notice `Thinking blocks: collapsed · ctrl+t to expand` (T5) |
| History loads (attach, resume) | Ended thinking steps | Each takes the session mode |
| TUI starts | No `thinkingFold`, no file, an unknown word, or a file that cannot be read | Session mode = expanded; no notice |
| Reply streams | Text deltas | Markdown renders as it arrives (as today); the time shows from the first row |
| Block list focus | `Tab`, `j`/`k` to any block | Box on that block (R2: S1, S2, S3); a collapsed step's mark becomes `›` (R3) |

## 6. Keys and focus

No new key. The input order of `docs/tui-architecture.md` does not change.

| Key | Receiver | Effect |
|---|---|---|
| `ctrl+t` | Application shortcut, any focus | Opens or folds all thinking (as today) and sets the session mode |
| `ctrl+e` | Block list | Same as `ctrl+t` |
| `e` `E` `→` `←` `l` `h` | Block list | Unchanged; they do not set the session mode |
| `j` `k` and the arrows | Block list | Move the selection; the box or the shade follows |
| `Enter` | Block list | Opens the viewer, unchanged |

## 7. Limited space and resize

- The box uses column 0 and the last column. Block text starts at column 2 and right-aligned parts end 2 columns before the edge, so the box covers no text at any width.
- R1 guarantees a blank row above and below every block, so `┌ ┐` and `└ ┘` always have a row. When the viewport cuts the block or its gap row, the edge row that shows shows `┆` instead of a corner.
- When the list scrolls to a selected block, it keeps the gap rows above and below in view, so the whole box shows when the block fits.
- The reply time shows only when the first row has room for it and 2 spaces; otherwise the first row wraps without it (40×14 in T2).
- A table wider than the text width narrows its columns and wraps the cell text (`table_box.go`). Only when that still does not fit, glamour draws the chunk. A cell is never cut without `…`.
- Resize changes no fold mode and no session mode. Wrapping follows `wrapLines` at the new width.

## 8. Text and width

- Thinking text wraps by words at the width minus 6 columns (4 at the left, 2 at the right).
- CJK and emoji count two columns in text, tables and the time layout.
- Inline code drops its backticks, so `` `a` `` is one column narrower on each side. A code span never wraps inside a word.
- Headings drop the `#` marks and the space after them. One blank row follows each heading.
- Truncated thinking shows `…` on the first body row when rows are hidden.
- An empty thinking text gives a step row with no body rows in every mode.

## 9. Style intent

| Part | Roles |
|---|---|
| Step mark | `›` or the step's own mark in the step's role (`thinking`, `tool`, `hook`; `warning` or `error` for a running or failed tool, as today) |
| Step header | verb in `fg` bold; details in `muted` |
| Thinking body | `thinking`, not italic, no rail |
| Tool body | unchanged colors, at column 4 after one blank row |
| Heading 1, 2, 3 | new roles `h1` (value of `tool`), `h2` (value of `prov-4`), `h3` (value of `assistant`); bold |
| Heading 4 to 6 | `fg` bold |
| Inline code | `h2` bold, no backticks |
| Bullet `•`, list numbers | `muted` |
| Table | box lines in `dim`; header cells `fg` bold; one space of padding in each cell |
| Code block | text in `code` with chroma colors on `code-panel`; no language row; 2 columns of padding |
| Block quote, rule | `│` and `───` in `muted` |
| Links | unchanged: `tool`, underlined |
| Reply time | `dim` |
| Selection box | `border` |

## 10. Acceptance criteria

1. At 80×24, with no `thinkingFold`, after a turn with six lines of thinking ends, the block pane shows `› Thought for`, one blank row, and all six lines at column 4, with no `┃` on any row (T2).
2. At 80×24, with `"thinkingFold": "collapsed"`, after the turn ends, the thinking step is one row that starts with `◆ Thought` (T3).
3. At 80×24, with `"thinkingFold": "truncated"`, after the turn ends, the step shows `›`, a blank row, `…` and the last three lines (T4).
4. At 80×24, for each value, while the thinking streams, the block pane shows `› Thinking…`, a blank row, `…` and the last lines, with no `┃` (T1).
5. At 80×24, with `"expanded"`, after `ctrl+t` folds all thinking and a second turn ends, the second step is one row, and the status row shows `Thinking blocks: collapsed · ctrl+t to expand` (T5).
6. At 80×24, with `"collapsed"`, after `ctrl+t` opens all thinking and a second turn ends, the second step shows all its lines (T6).
7. At 80×24, with `"expanded"`, after the user collapses a streaming step with `←` and the thinking ends, the step stays one row.
8. At 80×24, with `"thinkingFold": "sideways"`, after a turn ends, the step is Expanded, as in criterion 1.
9. At 40×14, with the default, after the turn ends, the block pane shows `I will edit Count and run the` and `tests again.` at column 4, and the reply below them (T2 at 40×14).
10. After the TUI starts again with `"collapsed"`, an ended step from the loaded history is one row.
11. At 80×24, a reply with `## Types` shows `Types` at column 2 with no `#` (M1).
12. At 80×24, a reply with `` `MaxParallelTools` `` shows `MaxParallelTools` with no backtick (M1).
13. At 80×24, a reply with a markdown table shows `┌`, `├`, `└` box rows and the header cells (M1).
14. At 80×24, a reply with `- item` shows `• item` (M1).
15. At 80×24, a fenced Go block shows its code on the code panel with no `go` row above it (M1).
16. At 80×24, between any two blocks of a turn (prompt, thinking, tool, reply) there is exactly one blank row, and one blank row follows the last block (R1; T2, M1).
17. At 80×24, after `Tab` and `k` to a reply, the reply rows show `│` in columns 1 and 80, the blank row above shows `┌` and `┐`, the blank row below shows `└` and `┘`, and no row shows the accent `┃` (R2; S1).
18. At 80×24, a selected open thinking step has `┌ ┐` on the blank row above its header and `└ ┘` on the blank row below its body (R2; S2).
19. At 80×24, a selected collapsed thinking step shows `› Thought`, with `┌ ┐` on the row above and `└ ┘` on the row below (R2, R3; S3).
20. At 80×24, a selected collapsed tool step shows `›` in place of `◆`, in a box (R2, R3).
21. At 80×24, an expanded tool step shows its header, one blank row, then its body at column 4 (R4).
22. At 80×24, the first row of a finished reply ends with its time `17:30` and 2 spaces (M1, S1).
23. At 80×24, moving the selection between blocks does not change the row of any block on the screen (R2).
24. At 80×24, a reply that starts with a table or a code block shows no time on any row (added on 2026-11-03).

## 11. Open questions

None. The user chose: style option A; the thin box like Grok for every block; colors from the existing palette; the 24h time on the reply; no table copy row; one blank row between any two blocks; `›` for a step that is open or selected; general rules R1-R4 for all blocks; `thinkingFold` in `settings.json` with the default `"expanded"`; `ctrl+t` sets the session mode like Grok; an invalid value falls back to `"expanded"` with no notice. The interactive mockup is [artifacts/mockup.html](artifacts/mockup.html).

Changes after the approval, on 2026-11-03: (1) the user chose one blank row under each heading, as in the Grok screenshots; sketches M1 and S1 and the mockup show it. (2) The reply time shows only when the first row of the reply is a text row; a reply that starts with a table or a code block shows no time, as the code panel already shows none.

