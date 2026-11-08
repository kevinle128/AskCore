---
title: "Model cycling key and models.json problems TUI design"
design-status: approved     # draft | approved
design-approved-by: "dale (user, in conversation, after a second review)"
design-approved-on: "2026-11-07"
---

# Model cycling key and models.json problems TUI design

This design is phase 7 of the [H7 plan](plan.md). It covers the key that cycles through the scoped models (D13/Q3, M9), the display of a `models.json` problem that Ask finds when `/model` opens (D4, D14), the current row that left the user file (M2, `notInConfig`), the request error `model configuration changed; choose the model again`, and the notice of a dropped thinking level (M16).

The key is open question QC. Section 6 uses the proposed key `ctrl+p` (forward) and `ctrl+shift+p` (back) to write the states; the user chooses the key in section 11.

The sketches are written by hand with a throwaway script that checks the width and the height of each sketch. This design has no `artifacts/preview.html` yet (see section 11, "Process note").

Parts that this design does not change copy the current code and goldens: the footer row (`internal/tui/footer.go`), the selector rows and the error area (`internal/tui/selector.go`, golden `TestSelectorModelAccessFailureKeepsModel`), the rail notices (`noticeRow` in `internal/tui/status.go`), the error block (`NewErrorBlock`, `ErrorHeadline` in `internal/tui/blocks.go`), the start notices (`internal/tui/start_notices.go`) and the `Settings problems` block (`diagnosticsBlock` in `internal/tui/trust_notes.go`, test `TestSettingsDiagnosticsNotice`).

## 1. Outcome

In the prompt pane or the block list, `ctrl+p` changes the session model to the next model of the cycle list, and `ctrl+shift+p` changes it to the previous one. The cycle list is the scope (`--models` or `enabledModels`); with no scope, it is every model that `/model` shows for a usable provider. The footer row names the new model and level, and the status row shows `✓ Model: <provider>/<model> · <level> · <n> of <total>` for a short time.
When `/model` opens and the user file has problems, the box shows one summary row, and the block pane gets one `Problems in models.json` block the first time that this set of problems shows. A current row that left the user file has the note `not in models.json`, and a request on it fails with an error block that tells the user to choose a model again.

## 2. Non-goals

- A change to the shortcut bar text. The bar keeps `Tab blocks · ctrl+t thinking · ctrl+l model · / commands`, as `shift+tab` (thinking level) is not in the bar today (`internal/tui/footer.go:194`). Open question 4 asks the user.
- A change to the footer row, the header row, the prompt pane, the queue rows or the viewer.
- A change to the start `Settings problems` block. Phase 5 (requirement 12) adds the `models.json` problems of a session start to the session diagnostics, so the existing block shows them with no TUI change (sketch P0).
- A thinking level selector, or a new key for the thinking level. `shift+tab` stays the thinking key.
- A file watcher, a timer or a poll: the TUI reads the list only at session start, when `/model` opens, and when a press finds no list (D14, P9).
- Headless `ask -p` output (the warning line of M16 is phase 6), `ask acp`, the leader and `ask settings show`.
- Session restore of a model (H8).

## 3. Placement

| Screen part (owner) | What changes | Data it reads |
|---|---|---|
| Application shortcuts (`keys.go`, `onShortcut`) | Two new keys: `ctrl+p` and `ctrl+shift+p` | The last models result (`Scope`, `Models`, `Current`) |
| Status row (`status.go`, `noticeRow`) | The result notice of a press, the busy notice, the "no other model" notice, and the dropped level line of a press | The `SetModel` answer (`ModelID`, `ThinkingLevel`) |
| Footer row (`footer.go`) | No code change: it already shows the model and the level of the session | Session model and level |
| Block pane, new notice block | `Problems in models.json, read again when /model opened`, in the format of the `Settings problems` block | `ACPModelsResult.Diagnostics` (planned) |
| Selector (`selector.go`), `/model` | One summary row in the error area when `Diagnostics` is not empty. The current row with `NotInConfig` gets the note ` · not in models.json` (under 60 columns, the tag `changed` replaces `current`) | `ACPModelsResult.Diagnostics`, `ACPModel.NotInConfig` (planned) |
| Block pane, error block (`blocks.go`) | The request error of a changed row gets a next-action row | The run error kind (planned, see 3.1) |
| Block pane, start notices (`start_notices.go`) | One more start notice: the dropped level of a session start | The dropped level notice of the session (planned, see 3.1) |

The key never opens the selector and never takes the prompt slot. The draft, the focus and the selection of the block list do not change on a press.

### 3.1 Planned wire (phases 5 and 6 are not implemented yet)

This design uses these names from the phase files. If phase 6 changes a name or a shape, update this design and set it back to `draft`.

| Name (planned) | Phase | What the TUI reads |
|---|---|---|
| `ACPModelsResult.Scope []ACPScopedModel{ModelID, ThinkingLevel}` | 6 | The cycle list in order. `ModelID` is the resolved `provider/id@api`; the TUI shows it with the existing pair text (`provider/model`, no `@api`). An empty `ThinkingLevel` means "the entry states no level". |
| `ACPModelsResult.Diagnostics []ACPDiagnostic` | 6 | The problems of the user file when the list was read. The TUI uses `Path`, `Key` and `Text`, as the `Settings problems` block does. |
| `ACPModel.NotInConfig bool` | 5 | Only on the current row. |
| `ACPModelRequest.ThinkingLevel` and the echo in `ACPSetModelResult.ThinkingLevel` | 6 | One `SetModel` call changes the model and the level. The TUI compares the asked level with the echoed level. |

Two parts that this design needs are not named in the phase files. The implementation of phases 5 and 6 must supply them, or this design changes:

- **Error kind of the changed row.** `ErrorHeadline` writes `Error (<kind>): <message>`. The sketches use the kind `config`. The TUI adds the next-action row only for this kind; it does not match the message text.
- **Session-start dropped level notice.** Phase 6 says that "the TUI host adds a session notice" (M16) but names no field. The TUI must get the text, or the model and the dropped level, in a field of its own (for example on `ACPStateResult`), not in `Diagnostics`: a dropped level is not a settings problem and must not show under `Settings problems`.


**As implemented in phase 6 (2026-11-07):** the session-start dropped-level notice is `ACPStateResult.LevelDropped` (`"levelDropped"`, type `ACPLevelDropped{ModelID, Level}`, text from `Text()`), not a `Diagnostics` row. The changed-row error kind is `config` (`ACPErrConfig`). `ACPModelsResult.Scope` is filled only from `--models` or `enabledModels`; an empty `Scope` means no scope, and the TUI builds the cycle list from the usable `/model` rows. The names match this section, so the approval stands.

## 4. Sketches

Every sketch is a whole screen at the stated size. `▌` is the cursor; in the prompt states it is at the start of the placeholder, in the selector states after `⌕ `. The prompt pane has the focus, so no block is selected. Every reply is Expanded, and the pane follows the end of the transcript, so its first rows can be the end of an earlier block. The footer row ends with one space; the goldens trim it.

The example session runs with `--models 'anthropic/claude-sonnet-4-6:medium,openai/gpt-5.5:high,local/llama4-scout:high'`. `local/llama4-scout` is a custom row with reasoning off (phase 3 default). The session starts on `anthropic/claude-sonnet-4-6` at `medium`.

#### C0. Idle with a scope, before a press

The start of the cycle example. The footer row names the first scope entry and its level.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore             anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### C1. One press of `ctrl+p` moves to the next scope entry

The entry `openai/gpt-5.5:high` states a level, so one `SetModel` call carries the model and `high`. The footer row shows the new model and level. The status row shows the result notice with the position in the cycle list for a short time (the time of every rail notice, `noticeFor`). No block is added to the pane. The default model is not saved (open question 2).

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Model: openai/gpt-5.5 · high · 2 of 3                                       
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Under 60 columns the notice drops `Model: ` and the position.

Regions: row 1 header; row 2 rule; rows 3-8 block pane; row 9 status; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  deadline. The leader removes the      
  stale socket after the lock probe,    
  and the probe can take 1.2 s.         
                                        
  claude-sonnet-4-6 · 4.2s · ↑12.4k…    
                                        
┊ ✓ openai/gpt-5.5 · high               
────────────────────────────────────────
 ❯ ▌Ask anything                        
────────────────────────────────────────
                      openai/gpt-5.5 4% 
 Tab blocks · / commands                
```

#### C2. No scope: the key cycles the usable models

No `--models` and no `enabledModels`. The cycle list is every row of `/model` for a usable provider, in the order of `/model` (open question 3). No entry states a level, so the session keeps its level (`medium`), and the host clamps it to the row as `setModel` does today. The position counts the rows of that list.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Model: openai/gpt-5.5 · medium · 6 of 6                                     
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                           openai/gpt-5.5 ◐ medium  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### C3. Nothing to cycle to

The cycle list has one model or none (a scope with one match, or one usable model). The press sends no call. The rail shows a `dim` notice for a short time; the footer does not change.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ No other model to cycle to.                                                   
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore             anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### C4. A press while the session runs is refused

The session runs a tool. A press sends no call and shows the existing busy text of `/model` (`modelBusyText`, `Model can change when the run ends.`) for a short time; then the live rail `┊ ◌ Working… · esc to interrupt` comes back. The model does not change. The same guard (`idleFor`) also covers the start of a session and a lost link, as it does for `/model`.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ Read internal/leader/client.go                                        ⠋ 3s  
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ Model can change when the run ends.                                           
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore             anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 12% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

A press while an earlier press waits for its answer (the list read or the `SetModel` call) is ignored: the screen does not change, and no notice shows. The next press after the answer starts from the new current model. The screen is C0 until the answer, then C1.

#### C5. A press that drops the level of the entry

The entry `local/llama4-scout:high` states `high`, but the row has no thinking. The host clamps the level to `off` and echoes it. The asked level and the echoed level differ, so the notice has a second rail row with the M16 text. The footer shows no level, as for every model without thinking. An entry that states no level never shows this row.

Regions: row 1 header; row 2 rule; rows 3-17 block pane; rows 18-19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Model: local/llama4-scout · 3 of 3                                          
┊ local/llama4-scout has no thinking; level high is not used.                   
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                               local/llama4-scout  ▰▱▱▱▱▱▱▱ 13% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Under 60 columns the second line wraps at spaces on the rail; each wrapped row has its own `┊`.

Regions: row 1 header; row 2 rule; rows 3-6 block pane; rows 7-9 status; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  and the probe can take 1.2 s.         
                                        
  claude-sonnet-4-6 · 4.2s · ↑12.4k…    
                                        
┊ ✓ local/llama4-scout                  
┊ local/llama4-scout has no thinking;   
┊ level high is not used.               
────────────────────────────────────────
 ❯ ▌Ask anything                        
────────────────────────────────────────
                 local/llama4-scout 13% 
 Tab blocks · / commands                
```

#### D1. Session start: the requested level is dropped

`ask --model local/llama4-scout:high`. The session starts on the row with level `off`. The notice is a start notice: its own block at the start of the transcript, with no glyph, in `muted`, indented 2 columns and wrapped at spaces, as the fallback notice. It shows once, only to the driver.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  local/llama4-scout has no thinking; level high is not used.                   
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                                local/llama4-scout  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  local/llama4-scout has no thinking;   
  level high is not used.               
                                        
                                        
                                        
                                        
                                        
────────────────────────────────────────
 ❯ ▌Ask anything                        
────────────────────────────────────────
                  local/llama4-scout 0% 
 Tab blocks · / commands                
```

#### P0. Session start: a models.json problem (existing block, no change)

For comparison with R1. Phase 5 adds the problems of the user file to the session diagnostics, so the `Settings problems` block of today lists them under the file path. This design does not change it.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  Settings problems                                                             
    ~/.ask/models.json                                                          
      providers.local.models[2].contextWindow: must be a number; the model is   
        skipped                                                                 
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             local/qwen3-coder:30b  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### R1. `/model` opens after a bad edit of models.json

The user changed the file during the session. `/model` reads it again (D14), and the list answer has two problems. The process keeps the last valid copy of the rows (D4). Two parts show the problems:

- In the box, one summary row in the error area (the place of a model change error), in `warning`, with no glyph. It shows each time `/model` opens while the answer has problems.
- In the block pane, one block `Problems in models.json, read again when /model opened`, in the format of the `Settings problems` block (heading in `fg` bold, path in `muted`, `key: text` rows wrapped under the key). The block is added only when the set of problems differs from the last set that the pane showed (at session start or at an earlier `/model`). The heading tells it from the start block.

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; rows 11-21 selector; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
                                                                                
  Problems in models.json, read again when /model opened                        
    ~/.ask/models.json                                                          
      providers.local.models[2].contextWindow: must be a number; the model is   
        skipped                                                                 
      providers.gateway: unknown key                                            
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● local                                                                    │
│     llama4-scout                                            128k             │
│ ❯   qwen3-coder:30b                                         256k  current    │
│   ● openai                                                                   │
│     gpt-5.5                                                   1M  reasoning  │
│                                                                              │
│  models.json has 2 problems; the list is in the transcript.                  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             local/qwen3-coder:30b  ▱▱▱▱▱▱▱▱ 6% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Under 60 columns the summary row is `models.json: <n> problems`, and the list gives one row to it.

Regions: row 1 header; row 2 rule; row 3 block pane; row 4 rule; rows 5-11 selector; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
                                        
────────────────────────────────────────
╭─ Select model ───────────────────────╮
│  ⌕ ▌type to filter                   │
│ ❯   qwen3-coder:30b 256k  current    │
│    … 4 more                          │
│  models.json: 2 problems             │
╰──────────────────────────────────────╯
   enter select  ·  esc close  ·  your …
────────────────────────────────────────
               local/qwen3-coder:30b 6% 
 Tab blocks · / commands                
```

#### R2. After `Esc`: the problems block in the pane, narrow

The same session at 40 columns after `Esc`. The block wraps by columns: a long schema path breaks where the row ends, as a path in the `Settings problems` block does (`TestSettingsDiagnosticsNotice`, 40×14).

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  when /model opened                    
    ~/.ask/models.json                  
      providers.local.models[2].contex  
      tWindow: must be a number; the    
      model is skipped                  
      providers.gateway: unknown key    
                                        
────────────────────────────────────────
 ❯ ▌Ask anything                        
────────────────────────────────────────
               local/qwen3-coder:30b 6% 
 Tab blocks · / commands                
```

#### N1. `/model`: the current row left models.json

The user removed `qwen3-coder:30b` from the file, or changed the `baseUrl` or `api` of its provider. The current row stays in the list with `NotInConfig`. It keeps the tag `current` and gets the muted note ` · not in models.json` after its id, as the note `not in account list` does. The footer row does not change.

Regions: row 1 header; row 2 rule; rows 3-11 block pane; row 12 rule; rows 13-21 selector; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● local                                                                    │
│     llama4-scout                                            128k             │
│ ❯   qwen3-coder:30b · not in models.json                    256k  current    │
│   ● openai                                                                   │
│     gpt-5.5                                                   1M  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             local/qwen3-coder:30b  ▱▱▱▱▱▱▱▱ 6% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Under 60 columns, or when the note does not fit, the tag `changed` replaces `current` and the note goes.

Regions: row 1 header; row 2 rule; rows 3-4 block pane; row 5 rule; rows 6-11 selector; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  claude-sonnet-4-6 · 4.2s · ↑12.4k…    
                                        
────────────────────────────────────────
╭─ Select model ───────────────────────╮
│  ⌕ ▌type to filter                   │
│ ❯   qwen3-coder:30b 256k  changed    │
│    … 4 more                          │
╰──────────────────────────────────────╯
   enter select  ·  esc close  ·  your …
────────────────────────────────────────
               local/qwen3-coder:30b 6% 
 Tab blocks · / commands                
```

#### N2. The next request on the changed row fails

The host sends nothing and the run ends with the fixed text of M2. The pane shows the existing error block with a next-action row in `muted`. The draft of the failed prompt is not lost: the prompt band stays in the transcript, as for every run error.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  claude-sonnet-4-6 · 4.2s · ↑12.4k ↓1.8k · $0.040                              
                                                                                
  ❯ and on macOS?                                                        14:06  
                                                                                
  ✗ Error (config): model configuration changed; choose the model again.        
    Choose a model with ctrl+l or /model. Then send again.                      
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             local/qwen3-coder:30b  ▱▱▱▱▱▱▱▱ 6% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

Regions: row 1 header; row 2 rule; rows 3-9 block pane; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
                                        
  ✗ Error (config): model               
    configuration changed; choose the   
    model again.                        
    Choose a model with ctrl+l or       
    /model. Then send again.            
                                        
────────────────────────────────────────
 ❯ ▌Ask anything                        
────────────────────────────────────────
               local/qwen3-coder:30b 6% 
 Tab blocks · / commands                
```

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| C0 idle, list known | `ctrl+p` | One `SetModel(next entry, level of the entry, save = no)` call; presses are ignored until its answer |
| Waiting for a `SetModel` answer | `ctrl+p` or `ctrl+shift+p` | Ignored; the screen does not change |
| Waiting | Answer: success, echoed level = asked level or no level asked | C1 (scope) or C2 (no scope): footer and result notice |
| Waiting | Answer: success, echoed level differs from a level that the entry states | C5: footer, result notice and the dropped level row |
| Waiting | Answer: error (no credential, no access, busy, other) | The existing `modelFailure` text as an error notice on the rail (`┊ ✗ … Model not changed.`); the model does not change |
| Waiting | The session or the link changed before the answer (`gen` differs) | The answer is dropped (stale result); no notice |
| C0, no list yet (the session start list failed or has not come) | `ctrl+p` | One `Models` call; presses are ignored until its answer; then the press acts on that list |
| Waiting for that list | The list answer has `Diagnostics` | As R1 for the pane block (once per set); no box opens |
| Waiting for that list | The list call fails | Error notice on the rail: `Could not read the model list: <reason>.` |
| C0 | Cycle list with fewer than 2 models | C3 |
| Running, sending, starting, or link not up | `ctrl+p` | C4 (running or sending), or the existing start and link notices of `idleFor` |
| C1, C2, C5 | The notice time ends | The rail is empty (or the live rail when a run starts) |
| C1 | `ctrl+shift+p` | The previous entry, with the same rules as `ctrl+p` |
| Current model not in the cycle list (chosen in `/model`, or `NotInConfig`) | `ctrl+p` / `ctrl+shift+p` | The first / the last entry of the list |
| Last entry | `ctrl+p` | The first entry (the list wraps); first entry and `ctrl+shift+p`: the last entry |
| A `SetModel` answer of a press arrives while `/model` is open | Answer | The footer changes; the selector stays open (a press answer never closes `/model`). The `current` tag of the open list stays on the earlier row until the next open, because the list came from the earlier `Models` answer |
| Idle | `ctrl+l` or `/model`; the answer has `Diagnostics` | R1: box summary row; a pane block when the set is new |
| R1 | `Esc` | The selector closes; the pane block stays (R2) |
| R1 | `/model` opens again with the same set | Box summary row only; no new pane block |
| R1 | `/model` opens again; the set changed or is empty after a fix | A changed set: a new pane block. An empty set: no summary row and no block |
| Idle, current row changed in the file | `ctrl+l` or `/model` | N1: note on the current row |
| N1 or idle | A prompt is sent | N2: error block with the next-action row |
| N2 | `ctrl+l`, choice of a model | Footer and `✓ Default model: …` as today; the next prompt runs |
| Session start with a dropped level | State event | D1: start notice at the start of the transcript |
| Session start with file problems | State event | P0: the existing `Settings problems` block |

## 6. Keys and focus

The input order of `docs/tui-architecture.md` holds. `ctrl+p` and `ctrl+shift+p` are application shortcuts (`onShortcut`): they act only when no part before the shortcuts takes the key. Verified receivers today: `onKey` in `internal/tui/update_input.go:28-72`, `onListOtherKey` in `internal/tui/keys.go:58-78`.

| Focus or open part | `ctrl+p` | `ctrl+shift+p` |
|---|---|---|
| `ctrl+c` handling | Not affected | Not affected |
| Viewer | Ignored: the viewer takes every key (`update_input.go:36-40`) | Ignored |
| Trust box | Ignored: the box takes every key (`update_input.go:41-44`) | Ignored |
| Provider, key, login or removal box | List up in the provider list (`providers_box.go:148,164`); ignored in the other boxes, which own every key (`update_input.go:45-49`) | Ignored |
| Selector (`/model`, `/sessions`) | List up (`selector.go:144`), as today | Ignored by the selector; in a terminal that sends it as `ctrl+p`, list up |
| Command list (completion) open | Cycles the model; the list stays open (completion takes only `↑`, `↓`, `Tab`, `Esc` and `Enter`) | Same |
| Prompt pane (editor) | Cycles forward. The editor has no `ctrl+p` binding (`editor_keys.go`), so the key reaches the shortcuts | Cycles back |
| Block list | Cycles forward (the list does not own the key; `keys.go:71`); the selection does not move | Cycles back |

- `ctrl+n` keeps its list meaning in the selector and the provider box and does nothing in the prompt pane and the block list.
- `ctrl+shift+p` arrives as its own key only in a terminal that supports the kitty keyboard protocol or xterm `modifyOtherKeys` (for example Ghostty, kitty, WezTerm, Alacritty, xterm). The patched Bubble Tea copy asks for both by default: it writes `SetModifyOtherKeys2` and pushes kitty flag 1, "basic key disambiguation" (`third_party/bubbletea/cursed_renderer.go:164-171,905-906`); `internal/tui` and `cmd/tui` add no request of their own. In a terminal with neither (for example Terminal.app) the press arrives as `ctrl+p` and cycles forward. The e2e case must check both keys on Ghostty and Alacritty. Open question 1 has alternatives.
- The docs (`internal/tui/README.md` key notes and the TUI keys table in `docs/cli.md`) name both keys.

## 7. Limited space and resize

- The result notice under 60 columns: `✓ <provider>/<model> · <level>`; `Model: ` and the position go. When it does not fit, the end of the model id is cut with `…` (the provider stays whole), as in the footer row.
- The dropped level row wraps at spaces on the rail; each row has `┊`. The busy and "no other model" notices are cut with `…` at the end when they do not fit.
- The status row uses rows that the block pane gives up; the block pane keeps at least one row (`layout.go`). Under 9 rows the rules go, as today.
- The box summary row under 60 columns: `models.json: <n> problems`. The summary row has priority over list rows: the list loses one row and shows `… <n> more`.
- The note ` · not in models.json` follows the rule of ` · not in account list`: under 60 columns, or when it does not fit, the tag `changed` replaces `current`.
- The pane block and the error block wrap as the `Settings problems` block and the error block do today; nothing is cut.
- 120×40: no layout change. The pane has more rows; the rail, the box and the footer are the same as at 80 columns; the bar adds ` · shift/option+drag select` as today.
- A resize while a notice shows keeps the notice and draws it in the form of the new width.

## 8. Text and width

- Model ids in the notice and the footer are user text from `models.json` (for example `local/qwen3-coder:30b`). The TUI measures display columns, not bytes: a CJK id uses two columns for each character. The cut marker is `…`.
- A provider id is lower case `a-z`, `0-9` and `-` (M17), so the provider part is never wide.
- A diagnostic `Key` is a schema path (`providers.local.models[2].contextWindow`), never a value or a user key name (phase 1, Finding 15). A path with no space wraps by columns, as a long file path in the `Settings problems` block. A tab in a text shows as spaces, as in every block.
- The file path of the block uses `~` for the home folder, as the `Settings problems` block does.

## 9. Style intent

| Part | Roles |
|---|---|
| Result notice `✓ Model: …` | `success` for `✓`, `dim` for the text (the existing `noticeRow` with `ok`) |
| Busy and "no other model" notices, dropped level row of a press | `dim` on the rail |
| Model change failure | `error` on the rail (existing) |
| Footer row model and level | `assistant` (existing) |
| Start notice of a dropped level | `muted`, no glyph (as the fallback notice) |
| Pane block `Problems in models.json, …` | As the `Settings problems` block: heading `fg` bold, path `muted`, rows `fg` with the key in `muted` |
| Box summary row | `warning`, no glyph |
| Note `not in models.json`, tag `changed` | `muted` (as `not in account list`, `unlisted`) |
| Error block and next-action row | `error` and `muted` (existing) |

No new role and no new glyph. The mockup proposals `⚠`, `○` and `●` for the level are not used: they are not in the style guide, and `●` means the provider dot.

## 10. Acceptance criteria

1. At 80×24, in C0 after `ctrl+p`, the footer row shows `openai/gpt-5.5 ◐ high` and the status row shows `┊ ✓ Model: openai/gpt-5.5 · high · 2 of 3`. (`TestModelCycleKeyMovesThroughScope`)
2. At 80×24, in C1 after `ctrl+shift+p`, the footer row shows `anthropic/claude-sonnet-4-6 ◐ medium` and the status row shows `┊ ✓ Model: anthropic/claude-sonnet-4-6 · medium · 1 of 3`. (`TestModelCycleKeyMovesThroughScope`, back step)
3. At 80×24, on the last entry after `ctrl+p`, the footer row shows the first entry. (`TestModelCycleKeyMovesThroughScope`, wrap step)
4. At 40×14, in C0 after `ctrl+p`, the status row shows `┊ ✓ openai/gpt-5.5 · high` and the footer row shows `openai/gpt-5.5 4%`. (`TestModelCycleKeyMovesThroughScope`, narrow)
5. At 80×24, with no scope and six usable rows, after `ctrl+p` on the fifth row, the footer row keeps the level `medium` and the status row shows `· 6 of 6`. (`TestModelCycleKeyWithoutScope`)
6. At 80×24, with one model in the cycle list, after `ctrl+p`, the status row shows `┊ No other model to cycle to.` and the client gets no `SetModel` call. (`TestModelCycleKeyNothingToCycle`)
7. At 80×24, while a run works, after `ctrl+p`, the status row shows `┊ Model can change when the run ends.`, the footer row does not change, and the client gets no `SetModel` call. (`TestModelCycleKeyRefusedWhileRunning`)
8. At 80×24, after `ctrl+p` and a second `ctrl+p` before the first answer, the client gets one `SetModel` call and the screen shows C1 after the answer. (`TestModelCycleKeyIgnoredWhileSwitching`)
9. At 80×24, after `ctrl+p` to `local/llama4-scout:high`, the status rows show `┊ ✓ Model: local/llama4-scout · 3 of 3` and `┊ local/llama4-scout has no thinking; level high is not used.`, and the footer row shows no `◐`. (`TestModelCycleKeyDroppedLevel`)
10. At 80×24 and 40×14, a session that starts with a dropped level shows `local/llama4-scout has no thinking; level high is not used.` as the first block of the pane, with no glyph. (`TestDroppedLevelStartNotice`)
11. At 80×24, after `ctrl+p` in the block list, the selection does not move and the footer row shows the next model. (`TestModelCycleKeyInBlockList`)
12. At 80×24, with `/model` open, after `ctrl+p`, the selector cursor moves up and the footer row does not change. (`TestModelCycleKeyInSelectorMovesUp`)
13. At 80×24, after `ctrl+l` on a list answer with two problems, the box shows `models.json has 2 problems; the list is in the transcript.` and the pane shows `Problems in models.json, read again when /model opened` with both `key: text` rows. (`TestModelsReloadProblemShownOnce`)
14. At 80×24, after `Esc` and `ctrl+l` with the same two problems, the box shows the summary row and the pane has one `Problems in models.json` block, not two. (`TestModelsReloadProblemShownOnce`)
15. At 40×14, in R1, the box shows `models.json: 2 problems`; after `Esc` the pane block wraps as sketch R2. (`TestModelsReloadProblemNarrow`)
16. At 80×24, with `NotInConfig` on the current row, `/model` shows `qwen3-coder:30b · not in models.json` with the tag `current`; at 40×14 it shows the tag `changed` and no note. (`TestModelSelectorNotInConfig`)
17. At 80×24, after a prompt on a changed row, the pane shows `✗ Error (config): model configuration changed; choose the model again.` and `Choose a model with ctrl+l or /model. Then send again.` under it. (`TestModelConfigChangedErrorBlock`)
18. At 80×24, a session start with a `models.json` problem shows it in the existing `Settings problems` block under `~/.ask/models.json`. (`TestSettingsDiagnosticsNotice`, a `models.json` subtest)
19. On a real terminal engine (`python3 e2e/tui/run.py --driver ask --case ask-model-cycle`, by hand), with `ASK_FAUX=1` and two faux models in scope, `ctrl+p` changes the footer row model.

## 11. Open questions

1. **QC: the cycling keys.** Every option keeps `ctrl+p` and `ctrl+n` as list keys in the selector and the provider box (the dialog gets the key first).
   - **A (proposed): `ctrl+p` forward, `ctrl+shift+p` back.** The same keys as Pi, so a Pi user has nothing to learn, and `ctrl+l` and `shift+tab` already match Pi. `ctrl+p` is free in the prompt pane and the block list (verified: `editor_keys.go` and `keys.go` have no `ctrl+p`). Risk: `ctrl+shift+p` needs a terminal with the kitty keyboard protocol or `modifyOtherKeys` (Bubble Tea asks for both by default); in Terminal.app it arrives as `ctrl+p` and cycles forward, so that terminal has no back key.
   - **B: `ctrl+p` forward, `alt+p` back.** `alt+p` arrives in every terminal as `ESC p`, so the back key always works. Not Pi. On macOS, `alt` needs "Use Option as Meta key" in Terminal.app and iTerm2.
   - **C: `ctrl+n` forward, `ctrl+p` back.** Both keys work in every terminal, and they mean "next" and "previous" as in the lists. Not Pi: a Pi user who presses `ctrl+p` goes back, not forward.
2. **Does a press save the default model?** `/model` saves it (`SetModel(ctx, id, true)`, `update_input.go:455`). Proposed: no. A press changes only the session model, so a fast cycle does not write `settings.json` on each press and does not move the start choice of M15. Alternative: save, as `/model` does; then the notice says `✓ Default model: …` as today.
3. **Cycle list with no scope.** Proposed: every `/model` row of a usable provider, in `/model` order (Pi cycles every model with valid auth). Alternative: with no scope the key shows `No scope. Set --models or enabledModels.` and does nothing.
4. **Shortcut bar.** Proposed: no change (see Non-goals); the key is in the docs only, as `shift+tab`. Alternative: add ` · ctrl+p cycle` to the prompt focus bar at 60 columns and more. It fits at 80 columns, but it changes the bar row of every session golden.
5. **Planned wire (for the implementer of phases 5 and 6, not a user choice).** The error kind of the changed row (`config` in the sketches) and the field of the session-start dropped level notice (section 3.1). A change of either sets this design back to `draft`.

**Process note.** The skill asks for `artifacts/preview.html` from one generator script. This phase run had write access to `design.md` only, so no preview and no generator are in the plan folder. The sketches were checked with a throwaway script (every line has the stated width, every sketch has the stated height). Before approval, a preview can be made in a later step, or the user can review this file.

### Decisions (user)

| # | Question | Decision (user, 2026-11-07) |
|---|---|---|
| 1 | QC: cycling keys | A: `ctrl+p` forward, `ctrl+shift+p` back. Terminal.app has no back key; this is accepted. |
| 2 | Does a press save the default model? | No. A press changes only the session model. |
| 3 | Cycle list with no scope | Every `/model` row of a usable provider, in `/model` order. |
| 4 | Shortcut bar | No change. The key is in the docs only. |

The user approved this design on 2026-11-07. Item 5 (wire) stays an implementer note: a change of the `config` error kind or of the dropped-level field sets this design back to `draft`.
