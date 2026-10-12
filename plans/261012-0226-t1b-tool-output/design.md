---
title: "Tool output, transcript view and /copy TUI design"
design-status: approved
design-approved-by: "user (dale)"
design-approved-on: "2026-10-12"
---

# Tool output, transcript view and /copy TUI design

The remaining TUI items of H5 in T1b: T-LAY-09 (tool output expand), T-MD-10 (truncation display) and T-CMD-08 (`/copy`). See the [inventory](../260930-2254-pi-feature-inventory-go-roadmap/inventory-tui.md) and the comparison report [xia-261012-0211](../reports/xia-261012-0211-t1b-tool-output-copy-compare.md).
Style C (Claude Code) is a user decision of 2026-10-12; the three styles are in the [mockup](../reports/xia-261012-0220-tool-expand-three-styles-mockup.html).

## 1. Outcome

Each tool call keeps its one tool line and gets a short preview of its output under it, with a muted hint `… +N lines (ctrl+o to expand)`. An edit shows `+N -M` on its tool line.
`ctrl+o` opens a temporary transcript view on the alternate screen. It shows the whole session with every tool output in full and the diff of each edit. `ctrl+o` or `Esc` returns to the inline view, which is the same as before.
When the model saw only part of an output, one muted line says how much it saw and names the spill file.
`/copy` puts the text of the last assistant reply on the clipboard and reports the real result.

## 2. Non-goals

- No fold or expand of one block (Grok style). The main view is never drawn again because of an expand.
- No change to `ctrl+t` (thinking toggle).
- No change to job notices; slice N1 owns them ([design](../261012-0155-t1b-job-notices/design.md)).
- No alternate-screen mode for the whole TUI (D15 stays in T2). The transcript view is temporary and holds no session state of its own.
- The TUI does not read the spill file. The transcript view shows the output that the tool result carries (at most 2000 lines or 50 KB, the model view); the spill path tells the user where the rest is.
- No key binding for `/copy` (open question 3).
- No change to the provider box copy (`c` in the login box). It still says `Copied` after OSC 52 (open question 5).

## 3. Placement

| What | Where | Replaces |
|---|---|---|
| Output preview | Rows under the tool line, in the same transcript block. While the tool runs, the block is in the live region; when it ends, the block is committed to scrollback with its preview | Nothing; today a tool block is one row |
| Hint `… +N lines (ctrl+o to expand)` | Last row of the preview, when lines are hidden | Nothing |
| `+N -M` of an edit or write | On the tool line, after the path | Nothing |
| Truncation line | Under the preview (or under the tool line when there is no preview), in both views | Nothing |
| Transcript view | Alternate screen. Row 1 is the header, the last row is the key row, the rows between show the transcript | The whole screen, until the user returns. Native scrollback and the inline frame are not touched |
| `/copy` result | Status rail notice (`setNotice`, `noticeRow`), 3 s | The rail state below it in priority, for 3 s |

Preview size for each tool (Pi values):

| Tool | Preview |
|---|---|
| `bash` | Last 5 lines (tail) |
| `read` | First 10 lines |
| `grep` | First 15 lines |
| `ls`, `find` | First 20 lines |
| `edit`, `write` | No rows; `+N -M` on the tool line. The diff is only in the transcript view |
| Other tools (`job_output`, `monitor`, MCP tools, …) | First 10 lines |

A tool with no output text has no preview rows. A failed tool uses the same preview; its reason stays on the tool line.
The preview size is not changed by the window size. Only the layout rule of the live region (section 7) can cut it.

**Streaming bash.** The agent sends `tool_execution_update` with a partial result while bash runs (`internal/tools/shell_bash.go`, at most one each 50 ms; `internal/agent/tool_coordinator.go`). The leader client passes every event to the TUI whole (`internal/tuiclient/events.go`, `onEvent`). Today the translator ignores this event (`internal/tui/translate.go`, `Event`). The design uses it: each update replaces the preview of the running block with the last 5 lines of the partial text. So a live tail is possible with the current events and no protocol change. The other built-in tools send no updates; their preview appears when they end.

**Transcript view in Bubble Tea.** The view uses the `AltScreen` field of `tea.View` (`third_party/bubbletea/tea.go`, the `View` struct). The renderer enters and leaves the alternate screen when the field changes between two frames (`cursed_renderer.go`, `shouldUpdateAltScreen`, mode 1049 with cursor save and restore). This works at run time in an inline program; no new API is needed. Two limits follow from the renderer and set the rules in section 5:

- `tea.Println` prints nothing while the alternate screen is active (`renderer.go`, comment on `Println`). So the commit of finished blocks to scrollback stops while the view is open, and continues after the return.
- `ReplayInlineTranscript` returns an error in the alternate screen (`cursed_renderer.go`, `replayInline`). So a resize rebuild of scrollback waits until the return.
- When the program ends (ctrl+c, `/quit`, a signal, a fatal error), the renderer close leaves the alternate screen first (`cursed_renderer.go`, `close`, the `lv.AltScreen` branch). So the terminal is restored on every exit path.

## 4. Sketches

Rows above the `-- live region starts --` marker are native scrollback; rows below it are the live region.
The transcript view (V1 to V3) is on the alternate screen: it has no scrollback and no live region, and it uses the full terminal. This is the one stated exception to the inline model; it is temporary.
At 120 columns the layout is the same with more room, so 120×40 is not drawn. Narrow cases at 40 columns show cut rows and short header forms.
The read output in the sketches is the real `internal/tui/status.go`. The sketches are generated by [artifacts/sketch-gen.py](artifacts/sketch-gen.py). To change the design, edit [artifacts/design-template.md](artifacts/design-template.md) or the generator, then run `python3 artifacts/make-design.py` from the plan directory; it rebuilds this file, checks every sketch width and writes `artifacts/preview.html`.

#### P1. Collapsed previews in the main view

Each tool line keeps its one row and gets a short preview: read shows the first 10 lines, bash the last 5, edit only +N -M on the tool line. A muted hint counts the hidden lines.

80×24:

```text
        "time"                                                                  
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also run go vet▌                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

40×14:

```text
    FAIL                                
    FAIL    AskCore/internal/tui    0.4…
    FAIL                                
    … +7 lines (ctrl+o to expand)       
                                        
  ◆ The copy note had no ok mark. I     
    added it; run the test again to     
    check.                              
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓1…
                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────╮
│ ❯ also run go vet▌                   │
╰──────────────────────────────────────╯
  ● 7c1e          claude-sonnet-5-5 12% 
```

#### P2. A bash call streams its output

While bash runs, its tool block is in the live region and the preview shows the latest lines from the partial tool results. Other tools show their preview when they end.

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/web                                       
  model  anthropic/claude-sonnet-5-5                                            
                                                                                
  ❯ run all tests                                                        15:31  
                                                                                
```

`-- live region starts --`

```text
  ◇ bash  go test ./... -count=1                                      running ⠋ 
    ok      AskCore/internal/tools  3.104s                                      
    ok      AskCore/internal/tui    9.877s                                      
    ok      AskCore/internal/tuiclient  1.220s                                  
    ok      AskCore/internal/workspace  0.031s                                  
    ok      AskCore/pkg/protocol    0.044s                                      
    … +37 lines (ctrl+o to expand)                                              
                                                                                
┊ ◌ Working… 12s · esc to interrupt                                             
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

40×14:

```text
                                        
```

`-- live region starts --`

```text
  ◇ bash  go test ./... -co…  running ⠋ 
    ok      AskCore/internal/tools  3.1…
    ok      AskCore/internal/tui    9.8…
    ok      AskCore/internal/tuiclient …
    ok      AskCore/internal/workspace …
    ok      AskCore/pkg/protocol    0.0…
    … +37 lines (ctrl+o to expand)      
                                        
┊ ◌ Working… 12s · esc                  
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e          claude-sonnet-5-5 12% 
```

#### P3. The model saw only part of the output

A muted line under the preview says what the model saw and where the full output is.

80×24:

```text
  ❯ run all tests verbose, then read the big fixture                     15:40  
                                                                                
  ● bash  go test ./... -count=1 -v                                     ✓ 48.2s 
    ok      AskCore/internal/tools  3.104s                                      
    ok      AskCore/internal/tui    9.877s                                      
    ok      AskCore/internal/tuiclient  1.220s                                  
    ok      AskCore/internal/workspace  0.031s                                  
    ok      AskCore/pkg/protocol    0.044s                                      
    … +1995 lines (ctrl+o to expand)                                            
    model saw 2000 lines (50 KB) of 2.4 MB                                      
    full output: /tmp/ask-jobs-81f2/bash-4.log                                  
                                                                                
  ● read  testdata/events.jsonl                                          ✓ 0.2s 
    {"type":"agent_start","ts":1760256001}                                      
    … +1999 lines (ctrl+o to expand)                                            
    model saw 2000 of 5120 lines · next offset 2001                             
                                                                                
  ◆ All packages pass. The fixture is long; I read the first 2000 lines.        
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

40×14:

```text
                                        
  ● read  testdata/events.jsonl  ✓ 0.2s 
    {"type":"agent_start","ts":17602560…
    … +1999 lines (ctrl+o to expand)    
    model saw 2000 of 5120 lines · next…
                                        
  ◆ All packages pass. The fixture is   
    long; I read the first 2000 lines.  
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓1…
                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e          claude-sonnet-5-5 12% 
```

#### V1. Transcript view opened with ctrl+o

A temporary alternate screen with the whole session and every output in full. It starts at the end; this sketch shows it after Home.

80×24:

```text
 Showing detailed transcript · ctrl+o or Esc to return              1–22 of 222 
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/web                                       
  model  anthropic/claude-sonnet-5-5                                            
                                                                                
  ❯ the status test fails, please fix it                                 15:20  
                                                                                
  ● read  internal/tui/status.go                                         ✓ 0.1s 
    package tui                                                                 
                                                                                
    import (                                                                    
        "fmt"                                                                   
        "strings"                                                               
        "time"                                                                  
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    // The status shows one state at a time on the dashed live rail. The order o
    f                                                                           
    // the states below is the order of priority: a lost link hides everything  
    // else, and an idle session with no notice shows nothing. A cancel request 
    // stays on screen as "Cancelling…" until the agent reports that the run    
    // settled; a stopped spinner is never taken as the end of a run.           
 ↑↓ scroll · PgUp PgDn page · Home End                                          
```

40×14:

```text
 Transcript · esc to return 1–12 of 317 
◢◤ ask  v0.1.0 · 7c1e                   
  anthropic/claude-sonnet-5-5           
                                        
  ❯ the status test fails,       15:20  
    please fix it                       
                                        
  ● read  internal/tui/status.…  ✓ 0.1s 
    package tui                         
                                        
    import (                            
        "fmt"                           
        "strings"                       
 ↑↓ PgUp PgDn Home End                  
```

#### V2. Transcript view: the edit diff

The edit shows its diff with line numbers. Removed lines use the error role, added lines the success role.

80×24:

```text
 Showing detailed transcript · ctrl+o or Esc to return           194–215 of 222 
    il, " "))                                                                   
    }                                                                           
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
      90       case s.failure != nil && s.failure.rev == s.editor.Revision():   
      91           return th.Render(RoleDim, GlyphRailStatus) + " " + th.Render(
           RoleError, GlyphFail+" "+s.failure.text)                             
      92       case s.now().Before(s.copiedUntil):                              
      93 -         return s.noticeRow(noticeView{text: "Copied"})               
      93 +         return s.noticeRow(noticeView{text: "Copied", ok: true})     
      94       case s.waitRow() != "":                                          
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusRowOrder                                                
    --- PASS: TestStatusRowOrder (0.00s)                                        
    === RUN   TestStatusJobsRow                                                 
    --- PASS: TestStatusJobsRow (0.00s)                                         
    === RUN   TestStatusCopiedNotice                                            
        status_test.go:88: row = "┊ Copied", want "┊ ✓ Copied"                  
    --- FAIL: TestStatusCopiedNotice (0.00s)                                    
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
 ↑↓ scroll · PgUp PgDn page · Home End                                          
```

#### V3. Transcript view: new output while scrolled up

Events keep updating the view. When the user is not at the end, the position stays and the header shows a marker; End jumps to the end and follows it again.

80×24:

```text
 Showing detailed transcript · ctrl+o or Esc to return         new output · End 
                                                                                
  ❯ the status test fails, please fix it                                 15:20  
                                                                                
  ● read  internal/tui/status.go                                         ✓ 0.1s 
    package tui                                                                 
                                                                                
    import (                                                                    
        "fmt"                                                                   
        "strings"                                                               
        "time"                                                                  
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    // The status shows one state at a time on the dashed live rail. The order o
    f                                                                           
    // the states below is the order of priority: a lost link hides everything  
    // else, and an idle session with no notice shows nothing. A cancel request 
    // stays on screen as "Cancelling…" until the agent reports that the run    
    // settled; a stopped spinner is never taken as the end of a run.           
                                                                                
    // shortWidth is the terminal width under which status, footer and placehold
 ↑↓ scroll · PgUp PgDn page · Home End                                          
```

40×14:

```text
 Transcript · esc      new output · End 
                                        
  ❯ the status test fails,       15:20  
    please fix it                       
                                        
  ● read  internal/tui/status.…  ✓ 0.1s 
    package tui                         
                                        
    import (                            
        "fmt"                           
        "strings"                       
        "time"                          
    )                                   
 ↑↓ PgUp PgDn Home End                  
```

#### R1. Back in the inline view

ctrl+o or Esc leaves the alternate screen. The inline view is as before, and the draft is kept.

80×24:

```text
        "time"                                                                  
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also run go vet▌                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

#### C1. /copy with a native clipboard command

pbcopy, wl-copy, xclip, xsel or termux-clipboard-set returned success.

80×24:

```text
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
┊ ✓ Copied                                                                      
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

#### C2. /copy through OSC 52

Over SSH, or with no native command, the text goes to the terminal. The terminal gives no answer, so the note does not say Copied.

80×24:

```text
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
┊ Sent to the terminal clipboard                                                
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

#### C3. /copy with no assistant text

No assistant message with text exists in this session yet.

80×24:

```text
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
┊ Nothing to copy                                                               
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

#### C4. /copy fails

The native command failed and the text is over the OSC 52 limit.

80×24:

```text
    )                                                                           
                                                                                
    // Run status component.                                                    
    //                                                                          
    … +162 lines (ctrl+o to expand)                                             
                                                                                
  ● edit  internal/tui/status.go  +1 -1                                  ✓ 0.1s 
                                                                                
  ● bash  go test ./internal/tui -run TestStatus                       ✗ exit 1 
    === RUN   TestStatusNoModel                                                 
    --- PASS: TestStatusNoModel (0.00s)                                         
    FAIL                                                                        
    FAIL    AskCore/internal/tui    0.412s                                      
    FAIL                                                                        
    … +7 lines (ctrl+o to expand)                                               
                                                                                
  ◆ The copy note had no ok mark. I added it; run the test again to check.      
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓140 · $0.002                              
                                                                                
```

`-- live region starts --`

```text
┊ Not copied: 180 KB is over the terminal clipboard limit of 75 KB              
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/web            claude-sonnet-5-5  ▰▱▱▱▱▱▱▱ 12%  $0.01 
```

40×14:

```text
    FAIL    AskCore/internal/tui    0.4…
    FAIL                                
    … +7 lines (ctrl+o to expand)       
                                        
  ◆ The copy note had no ok mark. I     
    added it; run the test again to     
    check.                              
    claude-sonnet-5-5 · 2.1s · ↑3.2k ↓1…
                                        
```

`-- live region starts --`

```text
┊ Not copied: 180 KB is over the term…  
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e          claude-sonnet-5-5 12% 
```

## 5. States and transitions

| State | Trigger | Next state |
|---|---|---|
| Tool running, no output | `tool_execution_start` | Tool line `◇ … running ⠋`, no preview rows |
| Tool running, bash output (P2) | `tool_execution_update` with text | Preview shows the last 5 lines; the hint counts the rest |
| Tool done (P1) | `tool_execution_end` | Preview from the final result; the block can be committed |
| Tool done, model view cut (P3) | `tool_execution_end` with `details.truncation` or `details.spillPath` | Preview, hint and truncation line |
| Tool done, no output | `tool_execution_end` with empty text | Tool line only |
| Tool start not seen (late attach) | `tool_execution_end` only | Outcome line and its preview, as for P1 |
| History snapshot (attach, restore) | `SnapshotMsg` | Previews come from the tool result messages of the snapshot; the same sizes apply |
| Inline view | `ctrl+o` in the editor focus | Transcript view, at the end of the transcript (V1) |
| Transcript view at the end | New event (delta, tool update, new block) | The view follows the end |
| Transcript view scrolled up (V3) | New event | The position stays; the header shows `new output · End` |
| Transcript view, marker shown | `End` | The view goes to the end, the marker goes away, the view follows again |
| Transcript view | `ctrl+o` or `Esc` | Inline view (R1). Held commits are written; a held resize rebuild runs |
| Transcript view | `ctrl+c` | The view closes, then the normal ctrl+c rule of the inline view runs (clear draft, quit confirm, or quit) |
| Transcript view | Program ends (signal, fatal client error, `/quit` from another path) | Renderer close leaves the alternate screen; the terminal is restored |
| Transcript view | Resize | The view wraps again for the new width and keeps the top row of its window; the inline rebuild is held until return |
| Transcript view | A selector or box must open (for example the leader asks for a choice) | Not possible today; no dialog opens from an event. A future dialog closes the view first |
| `/copy`, native command ok (C1) | Command exit 0 | Notice `✓ Copied` |
| `/copy`, OSC 52 sent (C2) | SSH session, no native command, or native command failed and the text fits | Notice `Sent to the terminal clipboard` |
| `/copy`, no text (C3) | No assistant message with text | Notice `Nothing to copy` |
| `/copy` fails (C4) | Native command failed and the text is over the OSC 52 limit, or OSC 52 only and over the limit | Error notice `Not copied: <reason>` |
| `/copy` while the agent writes a reply | `/copy` | Copies the last finished assistant message; the reply in progress is not used |

Live events while the view is open (decision of this design): the view keeps updating. When the window is at the end, it follows the end, as the inline view does. When the user scrolled up, the window does not move, and the header shows `new output · End`. Reason: the user opens the view to read; a jump would lose the place, and a frozen view would hide a run that still works. One rule covers both cases.

The draft is kept: the transcript view does not send any key to the editor, and it does not change the editor state. The return shows the same draft and cursor (R1).

## 6. Keys and focus

Input order, as in the architecture doc: exit and cancel first, then the active dialog, then completion, then the editor, then application shortcuts.

| Key | Receiver | Action |
|---|---|---|
| `ctrl+c` | Root (first) | In the view: close the view, then apply the inline ctrl+c rule. Inline: unchanged |
| `ctrl+o` | Application shortcut, after the editor (inline); transcript view (view open) | Open the view; in the view, close it. A selector or box that is open takes `ctrl+o` first and ignores it |
| `Esc` | Transcript view | Close the view. It does not cancel a run while the view is open; in the inline view `Esc` keeps its interrupt meaning |
| `↑` `↓` | Transcript view | Scroll one row |
| `PgUp` `PgDn` (also `space`, `b`) | Transcript view | Scroll one page (window height minus one) |
| `Home` `End` (also `g`, `G`) | Transcript view | Go to the top or the end |
| Mouse wheel | Terminal | No mouse capture (open question 2). Most terminals send `↑` `↓` for the wheel on the alternate screen |
| Other keys | Transcript view | Ignored; not sent to the editor |
| `/copy` | Editor command (completion lists it: `/copy  Copy the last reply`) | Copy, see section 5 |

`ctrl+o` is free today: no editor key and no shortcut uses it (`internal/tui/editor_keys.go`, `internal/tui/update_input.go`).

## 7. Limited space and resize

- Inline: a preview is part of its block. In the live region, the layout rule cuts the transcript tail first; a running block can lose preview rows from the top before the editor or status loses rows. The hint row stays when one preview row stays.
- A preview row is one terminal row. A long line is cut with `…`; it does not wrap. So the preview height is fixed for each tool.
- The truncation line stays whole when it fits. When the spill path does not fit, the path moves to its own row and wraps by columns; the path is never cut (P3).
- After a resize in the inline view, the existing rebuild draws the committed blocks again at the new width with the same collapsed previews.
- Transcript view: header row and key row stay. The header cuts the title first: under 60 columns it is `Transcript · esc to return`, and with the marker `Transcript · esc`. The position `a–b of n` is dropped before the marker.
- Transcript view: long lines wrap (nothing is cut, so the user can read all of it). A diff line wraps under its text column.
- A `/copy` notice longer than the row is cut with `…` (the existing notice row; C4 at 40 columns).
- Under 4 rows the view shows only the content rows and no header or key row.

## 8. Text and width

- Output text: tabs expand to the tab stop of `width.go` (4); control characters show the existing control marker; ANSI sequences are removed. The bash output is already sanitized by the sandbox view.
- CJK and emoji count two columns, for cut and for wrap. A cut never splits a wide character.
- The hint counts lines of the result text, not terminal rows: `N` = all lines minus shown lines. A final line end is not a line.
- For bash the hidden lines are before the shown tail; for the other tools they are after the head. The hint text is the same.
- `+N -M` counts added and removed lines of a line diff of `details.diff.oldText` and `details.diff.newText`. The transcript view shows a unified diff with 3 lines of context, the new line number, `+`, `-` or space.
- Truncation line text from `ToolResultDetails` (`pkg/protocol/tool.go`):
  - `truncation` with `totalLines` > 0 (read, grep, find, ls): `model saw <outputLines> of <totalLines> lines`; with `nextOffset`: ` · next offset <n>`.
  - `truncation` with `totalLines` = 0 (bash and `job_output`; the runner does not count dropped lines): `model saw <outputLines> lines (<outputBytes>) of <totalBytes>`.
  - `spillPath`: ` · full output: <path>`; with `spillCapped`: ` (file stopped at 64 MB)`.
  - Limits are the tool limits: 2000 lines or 50 KB (`internal/tools/truncate.go`, `internal/sandbox/view.go`); the spill cap is 64 MB (`internal/sandbox/buffer.go`).
  - The spill directory is removed when the session ends (`internal/jobs/table.go`), so the path is good only while the session lives.
- Sizes use one decimal under 10 (`2.4 MB`), none from 10 (`50 KB`).
- `/copy` copies the raw markdown text of the text blocks of the last assistant message, joined with one empty line. Thinking, tool calls and tool output are not copied.

## 9. Style intent

- Preview rows: four-space indent, text in `fg`.
- Hint row: `muted`.
- Truncation line: `muted`.
- `+N` in `success`, `-M` in `error` on the tool line.
- Transcript view header: title in `muted`, position in `dim`, marker `new output · End` in `accent` bold. Key row: `dim`.
- Diff in the view: line numbers `dim`, context `muted` sign with `fg` text, removed lines `error`, added lines `success`.
- Notices: `✓ Copied` uses the ok form (`success` mark, `dim` text). `Sent to the terminal clipboard` and `Nothing to copy` use the plain `dim` notice. `Not copied: …` uses the error notice.
- No new glyph and no new color. The style guide needs new pattern rows in the same change: tool preview, hint, truncation line, diffstat, diff line, transcript view header and key row.

## 10. Acceptance criteria

1. At 80×24, after a `read` of `internal/tui/status.go` ends, scrollback shows the tool line, the first 10 lines of the file, and `    … +162 lines (ctrl+o to expand)`.
2. At 80×24, after a `bash` call ends with 12 output lines, scrollback shows the last 5 lines and `… +7 lines (ctrl+o to expand)`.
3. At 80×24, after an `edit` that changes one line, the tool line shows `+1 -1` and no diff row follows it in the inline view.
4. At 80×24, while a `bash` call streams output, the live region shows the latest 5 output lines under `◇ bash` before the call ends.
5. At 40×14, a preview line longer than 36 columns ends with `…` and uses one row.
6. At 80×24, after a `bash` result with `spillPath` and a lines truncation, the row under the hint shows `model saw 2000 lines (50 KB) of 2.4 MB` and the next row shows `full output: /tmp/ask-jobs-81f2/bash-4.log`.
7. At 80×24, after a `read` result with `totalLines` 5120 and `nextOffset` 2001, a row shows `model saw 2000 of 5120 lines · next offset 2001`.
8. At 80×24, after `ctrl+o`, row 1 shows `Showing detailed transcript · ctrl+o or Esc to return` and the last row shows `↑↓ scroll · PgUp PgDn page · Home End`.
9. At 80×24, in the transcript view after `Home`, the screen shows `package tui` and every line of the read output follows when the user scrolls; no row shows `ctrl+o to expand`.
10. At 80×24, in the transcript view, the edit shows `93 -` and `93 +` rows.
11. At 80×24, in the transcript view scrolled up, after a new tool update, the header shows `new output · End` and the top row does not change; after `End` the marker is gone.
12. At 80×24, after `ctrl+o` and `Esc` with the draft `also run go vet`, the screen is the same as before `ctrl+o`, cell for cell, and the editor shows `also run go vet`.
13. At 80×24, after `ctrl+o`, a resize to 100×30 and `Esc`, the inline view is drawn again at 100 columns and the scrollback has no duplicate rows.
14. At 80×24, when a block ends while the view is open, after `Esc` that block is in scrollback once.
15. With the built `ask`, after `ctrl+o` and then `ctrl+c` twice (or SIGTERM), the terminal is back on the main screen with the cursor visible.
16. At 80×24, after `/copy` with a native command that exits 0, the status row shows `┊ ✓ Copied`.
17. At 80×24, after `/copy` with `SSH_TTY` set, the status row shows `┊ Sent to the terminal clipboard`, and the output has one OSC 52 sequence.
18. At 80×24, after `/copy` in a session with no reply, the status row shows `┊ Nothing to copy`.
19. At 80×24, after `/copy` of a 180 KB reply with no native command, the status row shows `Not copied: 180 KB is over the terminal clipboard limit of 75 KB` in the error role and no OSC 52 sequence is written.
20. The text that `/copy` sends has no thinking text and no tool output.

## 11. Open questions

All decided (user, 2026-10-12): every recommendation below is accepted.

1. **Thinking in the transcript view.** Show thinking in the view always, or follow `ctrl+t`? Recommendation: follow `ctrl+t`, so one switch controls thinking everywhere.
2. **Mouse wheel in the view.** Capture the mouse (`MouseModeCellMotion`) for the wheel, or not? Recommendation: no capture; the terminal keeps text selection, and most terminals send arrow keys for the wheel on the alternate screen.
3. **Key binding for `/copy`.** Pi uses `ctrl+x`. Recommendation: command only for M1.
4. **Real-terminal check of the alternate-screen return.** The renderer path looks correct, but no test enters and leaves the alternate screen in an inline program yet. Recommendation: a tui-test spike (Terminal.app, iTerm2, kitty, tmux) before implementation, with a resize inside the view.
5. **Provider box copy.** The login box says `Copied` after OSC 52. Recommendation: use the same copy path and texts as `/copy` in a follow-up, not in this slice.
