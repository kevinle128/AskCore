---
title: "T1a local TUI design"
design-status: approved     # draft | approved
design-approved-by: "Kevin Le (user, in session after the B4 preview review; covers the in-process startup fallback and visual direction B4)"
design-approved-on: "2026-10-10"
---

# T1a local TUI design

This design covers what the user sees and types in the first real Ask TUI.
It follows the `ask-tui-design` skill.
No Go code for T1a exists until this file is `approved`.

Sources:
[roadmap T1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core),
[TUI architecture](../../docs/tui-architecture.md),
[internal/tui README](../../internal/tui/README.md),
[T0 prototype gate](../261006-1649-t0-inline-prototype-gate/plan.md),
[D14 decision](../reports/pm-261007-1312-d14-tui-decision.md),
the Pi interactive mode (`packages/coding-agent/src/modes/interactive/`) as the look-and-feel reference,
and the ACP methods in [pkg/protocol/acp.go](../../pkg/protocol/acp.go).

## 1. Outcome

`ask` with no arguments opens an inline TUI that connects to the local leader through its Unix socket.
The user types a prompt, sees the reply stream as markdown, sees status, retries, errors, usage and the current model, and can cancel, queue more input, and switch model.
If the leader cannot be reached at startup, `ask` runs the agent in its own process instead and continues, as Grok does; the footer shows `in-process`. After a successful start, a lost leader is reconnected, never replaced by an in-process agent.
Quitting detaches from the session and restores the terminal; a running turn continues in the leader.

## 2. Non-goals

- Fullscreen or alternate-screen mode (D15), overlays, images (T2).
- Login and logout dialogs. The [provider setup design](../261010-1424-t1c-provider-setup-design/design.md) adds `/providers` and the login boxes; the headless `ask auth login` still works.
- Tool approval dialogs and the permission policy (M2).
- Rich tool renderers, diffs, bash mode, compaction, session picker, session tree, resume, and resources.
  These arrive with their owner phases in T1b.
- Keybinding config files and user themes. T1a uses fixed defaults and one built-in theme.
- Windows (D8).

## 3. Placement

The inline model from the architecture doc applies:

- **Scrollback** holds a one-line startup header and the committed transcript blocks: user prompts, replies, thinking, tool lines, meta lines, retry notes, error blocks and cancel markers.
- **Live region**, from top to bottom, with empty slots using no rows:
  1. `transcript` tail: the part of the reply that is not stable yet, and running tool lines.
  2. `status`: one row on the dashed live rail (`┊`) for working, sending, starting, retrying, reconnecting, and short notices.
  3. `pending`: queued steer and follow-up inputs on the dashed rail (`┆`), and one hint row.
  4. `completion`: slash-command list, above the editor.
  5. Editor slot: the rounded `editor` box, or a rounded `selector` box that replaces it and keeps the draft.
  6. `footer`: one row with connection, directory, model, context use and cost.

A prompt block commits to scrollback only after the leader accepts the prompt.
Until then the text stays in the editor, as the architecture doc requires.
Tool lines show `running ⠋` in the transcript tail and commit with their outcome.

Visual rules, roles, glyphs and every block pattern come from the [TUI style guide](../../docs/tui-style-guide.md) (direction B4, user, 2026-10-10).
The editor is a rounded box; the prompt marker is `❯ `; continuation rows indent two spaces.

Committed block formats (details in the style guide, "Patterns"):

| Block | Format |
|---|---|
| Startup header | `◢◤ ask   v<version> · leader <id> · <cwd>`, then the model line `  model  <provider>/<model>` when the session has a model, then one empty row |
| User prompt | Full-width `panel` band: `❯ ` and the prompt in bold, send time at the right |
| Reply | `◆ ` then rendered markdown, four-space indent on later rows |
| Code block | `code-panel` band: language name, then code |
| Meta line | `<model> · <duration> · ↑<in> ↓<out> · $<cost>` in `dim`, after each finished reply |
| Thinking | `∴ ` then italic thinking text; hidden: one row `∴ Thinking...` |
| Tool call | `◇ <tool>  <args>` with `running ⠋`, then `● <tool>  <args>` with `✓ <duration>` or `✗ <reason>` |
| Retry note | `↻ Attempt <n> failed: <reason>. Retry <k> of <max> scheduled.` |
| Error | `✗ Error (<kind>): <message>` and an indented next-action row |
| Cancel | `⊘ Cancelled by you. Partial reply kept above.` |
| Detach | `⏏ Detached from session <id>. The run continues in leader <id>.` |

## 4. Sketches

Each sketch has the exact terminal width.
The line `-- live region starts --` is not on screen; it marks where the live region begins.
`▌` is the cursor.
**Visual preview:** open [artifacts/preview.html](artifacts/preview.html) in a browser. It shows the same sketches as terminal windows with role colors and a caption for each state.

States with a layout that does not change with width are drawn at 80×24 only; see section 7 for the width rules that apply at 120 columns.
The sketches are generated by [artifacts/sketch-gen.py](artifacts/sketch-gen.py) and checked by [artifacts/verify-widths.py](artifacts/verify-widths.py).
To change the design, edit [artifacts/design-template.md](artifacts/design-template.md) or the generator, then run `python3 artifacts/build-design.py` from the plan directory; it rebuilds this file and checks every sketch width.

#### S1. Starting: leader is spawned

80×24:

`-- live region starts --`

```text
┊ ◌ Starting leader…  first run can take a few seconds                          
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ◌ leader starting   ⌂ ~/src/askcore   deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(19 empty rows below)

120×40:

`-- live region starts --`

```text
┊ ◌ Starting leader…  first run can take a few seconds                                                                  
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
  ◌ leader starting   ⌂ ~/src/askcore                                           deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(35 empty rows below)

#### S2. Leader unavailable: in-process agent

80×24:

```text
◢◤ ask   v0.1.0 · in-process agent · ~/src/askcore                              
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ○ in-process   ⌂ ~/src/askcore        deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(17 empty rows below)

#### S3. Idle: empty editor

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

120×40:

```text
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                                                           
  model  alibaba-token-plan/deepseek-v4.1-flash                                                                         
                                                                                                                        
  ❯ why does TestLeaderStale fail on CI?                                                                         14:02  
                                                                                                                        
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket  
    after the lock probe, and the probe can take 1.2 s.                                                                 
                                                                                                                        
    go                                                                                                                  
    deadline := 5 * time.Second                                                                                         
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                                                                  
                                                                                                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                              deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S4. Slash command completion

80×24:

```text
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
  ❯ /model     Select a model                                                   
    /new       Start a new session in this directory                            
    /sessions  Watch another live session                                       
    /take      Take control of this session                                     
    /quit      Detach and quit                                                  
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ /▌                                                                         │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S5. Multi-line draft

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ rewrite the probe so that it:                                              │
│   - uses an injected clock                                                   │
│   - returns ErrLeaderStarting after the deadline▌                            │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S5a. Sending: prompt not accepted yet

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
┊ ◌ Sending…                                                                    
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ show me the probe code▌                                                    │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S5b. Send failed

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
┊ ✗ Not sent: not_driver · draft kept · /take to drive                          
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ show me the probe code▌                                                    │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S6. Streaming reply with a tool call

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
```

`-- live region starts --`

```text
  ◇ echo  probe code request                                          running ⠋ 
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
                                                                                
┊ ◌ Working… 4s · esc to interrupt                                              
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

120×40:

```text
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                                                           
  model  alibaba-token-plan/deepseek-v4.1-flash                                                                         
                                                                                                                        
  ❯ why does TestLeaderStale fail on CI?                                                                         14:02  
                                                                                                                        
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket  
    after the lock probe, and the probe can take 1.2 s.                                                                 
                                                                                                                        
    go                                                                                                                  
    deadline := 5 * time.Second                                                                                         
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                                                                  
                                                                                                                        
  ❯ show me the probe code                                                                                       14:03  
                                                                                                                        
```

`-- live region starts --`

```text
  ◇ echo  probe code request                                                                                  running ⠋ 
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits         
                                                                                                                        
┊ ◌ Working… 4s · esc to interrupt                                                                                      
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                              deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S7. Running with queued steer and follow-up

80×24:

```text
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ● echo  probe code request                                             ✓ 0.1s 
                                                                                
```

`-- live region starts --`

```text
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
                                                                                
┊ ◌ Working… 9s · esc to interrupt                                              
┆ ⧗ steer   use the fake clock from internal/testsupport                        
┆ ⧗ next    then run the leader tests                                           
┆ alt+↑ edit queue · dashed rail = not sent yet                                 
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

120×40:

```text
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                                                           
  model  alibaba-token-plan/deepseek-v4.1-flash                                                                         
                                                                                                                        
  ❯ why does TestLeaderStale fail on CI?                                                                         14:02  
                                                                                                                        
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket  
    after the lock probe, and the probe can take 1.2 s.                                                                 
                                                                                                                        
    go                                                                                                                  
    deadline := 5 * time.Second                                                                                         
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                                                                  
                                                                                                                        
  ❯ show me the probe code                                                                                       14:03  
                                                                                                                        
  ● echo  probe code request                                                                                     ✓ 0.1s 
                                                                                                                        
```

`-- live region starts --`

```text
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits         
                                                                                                                        
┊ ◌ Working… 9s · esc to interrupt                                                                                      
┆ ⧗ steer   use the fake clock from internal/testsupport                                                                
┆ ⧗ next    then run the leader tests                                                                                   
┆ alt+↑ edit queue · dashed rail = not sent yet                                                                         
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                              deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S8. Retry wait

80×24:

```text
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled.            
                                                                                
```

`-- live region starts --`

```text
┊ ◌ Retrying (1/5) in 4s · rate limit · esc to cancel                           
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S9. Run ended with an error

80×24:

```text
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits.                                        
                                                                                
  ✗ Error (auth): anthropic token expired and refresh failed.                   
    Run in a shell: ask auth login --provider anthropic. Then send again.       
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore claude-opus-5-5 ◐ medium  ▰▰▱▱▱▱▱▱ 21%  $0.11 
```

#### S10. Run cancelled

80×24:

```text
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
  ⊘ Cancelled by you. Partial reply kept above.                                 
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S11. Model selector open

80×24:

```text
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● alibaba-token-plan                                                       │
│     deepseek-v4.1-flash                                     128k  current    │
│     qwen4-coder-plus                                        256k             │
│   ● anthropic                                                                │
│ ❯   claude-opus-5-5                                           1M  reasoning  │
│     claude-sonnet-5-5                                         1M  reasoning  │
│   ● openai                                                                   │
│     gpt-5.5-codex                                           400k  reasoning  │
│   ● xai                                                                      │
│     grok-5-code                                             256k  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

120×40:

```text
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
                                                                                                                        
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                                                           
  model  alibaba-token-plan/deepseek-v4.1-flash                                                                         
                                                                                                                        
  ❯ why does TestLeaderStale fail on CI?                                                                         14:02  
                                                                                                                        
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket  
    after the lock probe, and the probe can take 1.2 s.                                                                 
                                                                                                                        
    go                                                                                                                  
    deadline := 5 * time.Second                                                                                         
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                                                                  
                                                                                                                        
```

`-- live region starts --`

```text
╭─ Select model ───────────────────────────────────────────────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                                                                   │
│   ● alibaba-token-plan                                                                                               │
│     deepseek-v4.1-flash                                                                             128k  current    │
│     qwen4-coder-plus                                                                                256k             │
│   ● anthropic                                                                                                        │
│ ❯   claude-opus-5-5                                                                                   1M  reasoning  │
│     claude-sonnet-5-5                                                                                 1M  reasoning  │
│   ● openai                                                                                                           │
│     gpt-5.5-codex                                                                                   400k  reasoning  │
│   ● xai                                                                                                              │
│     grok-5-code                                                                                     256k  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                                                                    
  ● leader 7c1e   ⌂ ~/src/askcore                                              deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S12. Model selector: filter and readiness failure

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ anth▌                                                                     │
│   ● anthropic                                                                │
│ ❯   claude-opus-5-5                                           1M  reasoning  │
│     claude-sonnet-5-5                                         1M  reasoning  │
│                                                                              │
│  ✗ anthropic is not ready: not signed in. Model not changed.                 │
│    Run in a shell: ask auth login --provider anthropic                       │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S12a. Live session selector

80×24:

```text
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭─ Live sessions ──────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   3f2a  ~/src/askcore                                     this client drives │
│ ❯ 8d07  ~/src/askcore                                            driver busy │
│   c1e9  ~/src/pi-notes                                             no driver │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter watch  ·  esc close                                                    
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S13. Disconnected from the leader

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
┊ ✗ Disconnected from the leader · reconnecting (attempt 2, next in 2s)         
┊ draft kept · sending waits until the leader is back · nothing is resent       
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader lost   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S13a. Reconnected: same leader, session restored

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ↻ Reconnected. Session 3f2a restored.                                         
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S13b. Reconnected: leader restarted, session gone

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ✗ Session 3f2a is gone: the leader restarted.                                 
    The transcript above is kept. Your next prompt starts a new session.        
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 91b0   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

#### S14. Observer: another client drives

80×24:

```text
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
```

`-- live region starts --`

```text
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
                                                                                
┊ ◌ Working… 6s · another client drives this session                            
╭──────────────────────────────────────────────────────────────────────────────╮
│ ◉ Watching. Type /take to take control when the run ends.                    │
╰──────────────────────────────────────────────────────────────────────────────╯
  ◉ observer 3f2a   ⌂ ~/src/askcore    deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S15. Quit confirmation while a run is active

80×24:

```text
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
```

`-- live region starts --`

```text
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
                                                                                
┊ Press ctrl+c again to quit · the run continues in the leader                  
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### S16. After quit: detached

80×24:

```text
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits                                         
                                                                                
  ⏏ Detached from session 3f2a. The run continues in leader 7c1e.               
    Run ask in this directory to see it again.                                  
$ ▌                                                                             
```

#### S17. Narrow terminal with wide characters

40×14:

```text
                                        
                                        
                                        
  ❯ giải thích lỗi               14:05  
    日本語のテスト                      
                                        
  ◆ The 日本語 test fails because each  
    wide character uses two columns.    
```

`-- live region starts --`

```text
                                        
┊ ◌ Working… 3s · esc                   
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e        deepseek-v4.1-flash 18% 
```

#### S18. Thinking shown (default)

80×24:

```text
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ∴ The stale check runs before the deadline starts, so the deadline is too     
    short only when the probe is slow. I should look at the probe timing first. 
                                                                                
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits.                                        
    claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031                              
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore claude-opus-5-5 ◐ medium  ▰▰▱▱▱▱▱▱ 21%  $0.11 
```

#### S19. Thinking hidden after ctrl+t

80×24:

```text
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ∴ Thinking...                                                                 
                                                                                
  ◆ The probe lives in internal/leader/client.go. It dials the socket, then     
    sends an initialize frame and waits.                                        
    claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031                              
                                                                                
```

`-- live region starts --`

```text
┊ Thinking blocks: hidden · ctrl+t to show                                      
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore claude-opus-5-5 ◐ medium  ▰▰▱▱▱▱▱▱ 21%  $0.11 
```

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| S1 Starting | `ask` starts; no leader answers | S3 when the leader is ready; S2 on any connect error (`unsafe_endpoint`, `malformed_peer`, `version_refused`, `startup_failed`, start timeout) |
| S2 In-process agent | Leader connect failed at startup | Same states as S3 to S12 and S15 to S19, with these differences: the footer shows `in-process` and no leader id; `/sessions` and `/take` show `Needs the leader; this session runs in process.`; S13 to S14 do not occur; quitting stops the run, so S15 says `Press ctrl+c again to quit. The run stops.` and S16 says `Run stopped. Session ended.` |
| S3 Idle | Leader ready and the session is attached as driver. The header is followed by the model line. With no usable provider the session has no model: there is no model line and the footer shows `no model` | S5a on enter with text; S4 on `/`; S11 on ctrl+l |
| S4 Completion | `/` at the start of an empty draft | Closes on esc, on a space, or when no command matches; enter runs the selected command |
| S5 Multi-line draft | shift+enter or ctrl+j | Same as S3 |
| S5a Sending | enter with text while idle | The draft stays in the editor and `status` shows `Sending…`. On acceptance: the draft is cleared (only if it was not edited since enter), the `❯` prompt band commits, then S6. On failure: S5b |
| S5b Send failed | The prompt request fails (`not_driver`, a frame error, or a disconnect before acceptance) | The draft is kept and `status` shows the reason. After a disconnect the outcome is unknown: the TUI does not resend, and the resync after reconnect shows whether the prompt arrived |
| S6 Streaming | `agent_start` and `message_update` events | Committed blocks move to scrollback; S3 on `agent_settled`; S8 on `auto_retry_start`; S9 on an error end; S10 on cancel |
| S7 Queued input | enter (steer) or alt+enter (follow-up) while running; `queue_update` event | Rows leave the list when the agent takes the input; alt+up moves them back to the editor |
| S8 Retry wait | `auto_retry_start` | S6 when the retry starts; S9 when `auto_retry_end` reports failure; S10 on esc |
| S9 Error | `cycle_end` with reason `error`, or a failed `auto_retry_end` | S3; the error stays in scrollback |
| S10 Cancelled | esc while running; lifecycle reports `aborted` | S3 only after `agent_settled`; the marker is committed at settle, not at the key press |
| S11 Selector | ctrl+l or `/model` while idle. The list shows only models of usable providers, grouped by provider | S3 on esc or after a successful `set_model`; the transcript then shows a change line `model  <provider>/<model> · was <old>` and the footer keeps the bare model id; S12 on a readiness failure |
| S12 Selector error | `set_model` fails | Stays open; the previous model stays current |
| S12a Session selector | `/sessions` | Lists `list_live` sessions with id, cwd and driver state. enter: `detach` from the current session, `attach` to the chosen one as observer, then `follow`; the transcript shows a divider `── session 8d07 ──`, then the model line of the watched session, then the followed history. esc: S3 |
| S13 Disconnected | The socket closes or a frame fails | Reconnect with backoff from 1 s, doubling to 30 s, with no attempt limit; `ConnectOrSpawn` may start a new leader. Typing works; enter shows `Reconnecting, please wait…` and sends nothing. S13a or S13b when connected |
| S13a Restored | Reconnected to the same leader and the session is live | `attach`, then `follow` from the last seen cursor; missed blocks are added below the transcript. The restore note is followed by the model line; then S3 or S6 |
| S13b Session gone | Reconnected, but the session id is not in `list_live` (the leader restarted; H8 storage does not exist yet) | Transcript kept as history; the next prompt creates a new session; then S3 |
| S14 Observer | Another client is the driver | S3 after `/take` succeeds when the session is idle |
| S18 Thinking shown | Default; ctrl+t while hidden | S19 on ctrl+t |
| S19 Thinking hidden | ctrl+t while shown | S18 on ctrl+t. Each toggle rebuilds the transcript with the resize mechanism and shows `Thinking blocks: hidden` or `visible` in `status` for 3 s |
| S15 Quit confirm | First ctrl+c with an empty draft | Quit on a second ctrl+c within 2 s; otherwise the notice disappears |
| S16 Detached | ctrl+d with an empty draft, a confirmed ctrl+c, or `/quit` | Process exits with code 0 and a restored terminal |
| Stale result | A `set_model`, completion or reconnect result whose generation is old | Ignored; nothing changes on screen |

A signal (SIGINT from outside, SIGTERM, SIGHUP) detaches, restores the terminal, and exits with the codes that `ask -p` uses (130, 143, 129).

## 6. Keys and focus

Input order follows the architecture doc: host exit and cancel handling, then the active selector, then completion, then the editor, then application shortcuts.

| Key | Receiver | Action |
|---|---|---|
| ctrl+c | Host | Draft not empty: clear the draft (undo can restore it). Draft empty: S15, then quit on the second press |
| ctrl+d | Host | Draft empty: detach and quit. Otherwise delete the character under the cursor |
| esc | Selector, completion, then host | Close the selector or completion. Else, when running: cancel the run. Else: nothing |
| up, down | Selector, completion, then editor | Move the selection. In the editor: move the cursor; on the first or last row, walk prompt history |
| enter | Selector, completion, then editor | Select. In the editor: idle sends a prompt; running sends a steer |
| alt+enter | Editor | Running: queue a follow-up. Idle: send, the same as enter |
| shift+enter, ctrl+j | Editor | Insert a new line. ctrl+j works in every terminal; shift+enter works when the terminal reports it |
| alt+up | Editor | Move all queued inputs back into the draft and remove them from the queue |
| ctrl+l | Application | Open the model selector when idle. Running: notice "Model can change when the run ends." |
| `/sessions`, `/take`, `/new`, `/model`, `/quit` | Editor command | Run when the draft is exactly the command and enter is pressed; completion offers them after `/` |
| shift+tab | Application | Cycle the thinking level when the model supports reasoning |
| ctrl+t | Application | Hide or show all thinking blocks. The choice lasts until quit; settings (H6, T1b) will save it |
| left, right, home, end, ctrl+a, ctrl+e, alt+b, alt+f | Editor | Cursor movement by grapheme and by word |
| backspace, ctrl+w, ctrl+u, ctrl+k, ctrl+y | Editor | Delete and kill ring |
| ctrl+- | Editor | Undo |
| Typing in the selector | Selector | Filter by substring of provider and model id |

Pasted text goes to the editor as one insertion, with CRLF changed to LF.
A paste of more than 10 lines shows a marker such as `[paste #1, 42 lines]` in the draft and sends the full text.

## 7. Limited space and resize

The live region gives rows in this order, and the last items lose rows first:

1. Editor slot: at least 1 row; it grows up to 30% of the terminal height, then scrolls inside.
2. `status`: 1 row when it has content.
3. `footer`: one row.
4. `pending`: up to 3 rows, then `+N more`.
5. `completion` and selector list: up to 8 rows at 24 rows, then `… N more`; at least 3 rows when the selector is open.
6. Transcript tail: the rest. If it does not fit, it shows its last rows; the renderer commits stable blocks so that the tail stays short.

At fewer than 8 rows the footer is hidden. At fewer than 4 rows only the editor and status remain.

At 120 columns the layout is the same as at 80; long lines wrap later and the footer has room for full values.
Under 60 columns the model line has no `model` label; a pair that is still too wide wraps after the slash.
Below 60 columns the footer uses the short form in S17.

Resize follows D14: after the size stops changing for 120 ms, the renderer clears the screen and native scrollback and draws the committed transcript again at the new width.
The draft, cursor, selector state, and queue stay the same.
Output from before `ask` started can disappear on resize; this is a known and accepted effect.

## 8. Text and width

- Width is measured in terminal columns by grapheme; CJK and most emoji use 2 columns and are never split.
- The truncation marker is `…`.
- Paths use `~` for the home directory; when too long they keep the last element: `~/…/askcore`.
- Model ids that do not fit keep the model name: `…/deepseek-v4.1-flash`.
- Tabs expand to the next multiple of 4 columns; control characters show as `?`, as in T0.
- Long single words in the transcript break at the column limit.
- Token counts use `k` and `M` with one decimal place below 100 (`12.4k`); cost uses `$` with three decimal places below $1.

## 9. Style intent

Roles, palette, glyphs and motion are defined in the [TUI style guide](../../docs/tui-style-guide.md).
This design uses the dark theme of direction B4.
Without color support, the roles fall back to bold, dim, italic and plain text, and every state stays readable.

## 10. Acceptance criteria

1. At 80×24, after `ask` starts with no leader, the screen shows `Starting leader…` in `status`, then the startup header, the model line when there is a model, and an empty editor (S1, S3).
2. At 80×24, with an unsafe socket, `ask` starts an in-process agent, the footer shows `○ in-process`, the model line shows when the agent has a model, and a prompt gets a reply (S2).
3. At 80×24, after a prompt and enter, `status` shows `Sending…` with the draft still in the editor; after acceptance the `❯` prompt band is in scrollback, the reply streams in the transcript tail, and `status` shows `Working…` with elapsed seconds (S5a, S6).
4. At 80×24, when the prompt is refused, the draft stays in the editor and `status` shows `✗ Not sent:` with the reason (S5b).
5. At 80×24, a tool call shows `◇ <tool>` with `running` in the tail, and then commits as `● <tool>` with `✓ <duration>` or `✗` (S6, S7).
6. At 80×24, after enter and alt+enter during a run, `pending` shows `⧗ steer` and `⧗ next` rows on the dashed rail; after alt+up the draft holds both texts and `pending` is empty (S7).
7. At 80×24, after a provider 429 with retry, `status` shows `Retrying (1/5) in 4s` and scrollback shows `↻ Attempt 1 failed` (S8).
8. At 80×24, after an expired subscription token, scrollback shows `✗ Error (auth)` and the next action line (S9).
9. At 80×24, after esc during a run, scrollback shows `⊘ Cancelled by you.` only after the run settles, and the next prompt runs normally (S10).
10. At 80×24, after ctrl+l, the selector replaces the editor, the draft is kept, and enter on a ready model changes the model in the footer and adds the change line `model  <provider>/<model> · was <old>` to the transcript; the footer keeps the bare model id (S11).
11. At 80×24, `/model` lists only the models of usable providers; a provider that is not signed in is hidden. Selecting a model whose provider has a failed token shows `✗ <provider> is not ready` and the footer model does not change (S12).
12. At 80×24, after the leader stops, `status` shows `Disconnected from the leader` with the attempt number, enter sends nothing, and the draft is kept (S13).
13. At 80×24, after only the socket drops and the leader stays up, the screen shows `Reconnected. Session 3f2a restored.`, then the model line, and blocks produced during the outage appear once (S13a).
14. At 80×24, after the leader restarts, the screen shows `Session 3f2a is gone`, the old transcript stays, the next prompt starts a new session, and no earlier prompt is sent again (S13b).
15. At 80×24, a second client in the same session shows `◉ observer` in the footer and the watching text in the editor slot (S14).
16. At 80×24, in a second `ask`, `/sessions` lists the first client's session as `driver busy`; enter attaches as observer, the transcript shows the divider and the model line, and the followed reply streams in the transcript tail (S12a, S14).
17. At 80×24, after ctrl+d during a run, the screen shows `Detached from session` and the leader still runs the turn (S16).
18. At 80×24, after ctrl+t, every thinking block in scrollback shows `Thinking...` and `status` shows `Thinking blocks: hidden`; after ctrl+t again, the full text is back (S18, S19).
19. At 40×14, a prompt with `日本語` wraps without split characters, and the footer uses the short form (S17).
20. At 120×40, S3, S6, S7 and S11 match their 120-column sketches, with the model line under the header.
21. After normal quit, an error exit, SIGINT, SIGTERM, and SIGHUP, the terminal echo, cursor, and line mode are restored.
22. During a resize from 120×40 to 80×24 while streaming, the draft and cursor stay, and no row is drawn twice after 120 ms.

## 11. Decisions and open questions

All questions below are decided.
No open question remains.

12. **Kind text of a refused prompt.** Decided (user, 2026-10-10): S5b shows `not_driver`, the kind the leader sends.
    The sketch row and criterion 4 are updated.
11. **Retry count.** Decided (user, 2026-10-10): the retry sketch shows `Retrying (1/5)`, the number of attempts the retry policy allows.
10. **Sessions list.** Decided (user, 2026-10-10): the `/sessions` list shows id, cwd and driver state only; the `N watching` column is removed.
9. **Model the sign-in cannot use.** Decided (user, 2026-10-10): a model that the ChatGPT sign-in cannot use gives `<provider>: this model is not available with your ChatGPT sign-in. Model not changed.` with no login hint.
    Selector rows get no readiness mark.

8. **Login hint text.** Decided (user, 2026-10-10): the auth error block says `ask auth login --provider <id>`, because the real command requires `--provider` and the old text `ask auth login anthropic` fails.
    Only this text changed.

7. **Visual direction.** Decided (user, 2026-10-10): B4 "Soft + live rail", chosen from directions A, B, B2, B3, B4 and C in [artifacts/style-directions.html](artifacts/style-directions.html). Rules are in [docs/tui-style-guide.md](../../docs/tui-style-guide.md).

6. **Leader unavailable at startup.** Decided (user, 2026-10-10): Grok behavior. `ask` falls back to an in-process agent when the leader connect fails at startup (Grok `xai-grok-pager/src/app/mod.rs:1107-1128`). This replaces the earlier rule that a connect failure exits with an error. The footer shows `in-process` as state, not as a notice.

1. **Newline key.** Decided (user, 2026-10-09): Pi keys. alt+enter queues a follow-up while running; ctrl+j or shift+enter inserts a new line. This replaces the T0 alt+enter new line.
2. **Thinking text.** Decided (user, 2026-10-09): Pi behavior. Shown by default; ctrl+t hides or shows all blocks by rebuilding the transcript; observer take-control moves to `/take`. Not saved until H6 settings.
3. **Reconnect.** Decided (user, 2026-10-09): Grok behavior adapted to Ask. Automatic reconnect with 1 s to 30 s backoff and no limit, may spawn a leader, typing allowed but sending blocked, restore by attach and follow, explicit notice when the session is gone, never resend.
4. **Slash commands in T1a.** Decided (user, 2026-10-09): `/model`, `/new`, `/sessions`, `/take` and `/quit`.
    `/sessions` reuses the selector to watch a live session.
    Resume and the session tree stay in T1b.
    Command names are case-insensitive like Grok CLI (user, 2026-10-10): only the command token is folded; arguments keep their case.
5. **Sketch sizes.** Decided (user, 2026-10-09): 80×24 for every state; 120×40 only where the layout changes; a narrow case when truncation matters. The skill is updated to match.
