---
title: "H9 usage and overflow TUI design"
design-status: approved
design-approved-by: "user (conversation)"
design-approved-on: "2026-11-20"
status: approved
---

# H9 usage and overflow TUI design

**Status: approved by the user on 2026-11-20 in the conversation.** Answers: the cost leaves the footer under 60 columns (question 1); the footer cost format is `$0.42` (question 2); the percent has no estimate mark (question 9); an explicit overflow shows the new generic text with the next-action row instead of the provider sentence (question 6). Every other open question in section 12 takes its default. No preview page is needed (question 11).

Written 2026-11-20 (Asia/Saigon) for phase 6 of [plan.md](plan.md). Facts come from `internal/tui/footer.go`, `translate_errors.go`, `translate.go`, `update_session.go`, `status.go`, `blocks.go`, `docs/tui-style-guide.md` and the goldens under `internal/tui/testdata`. Every sketch is a whole screen with its exact size. The footer rows were made by one script that checks the width of every line, so the sketches and the rules agree.

## 1. Outcome

The footer row shows the cost of the whole session, from the first reply on. A cost that is not fully known has the marker `≥` ("at least"). A model with no known price shows the tokens (`↑in ↓out`) in place of a cost. The context bar and the percent come from the server estimate, so a restored session and a silent overflow show the true figure.

An overflow ends the run with one error block, `Error (context)`, also when the provider sent no error text. A quota error, a rate-limit retry and an overflow stay three different things on the screen: a different kind word and a different next action for the errors, and a status row with a `↻` note for the retry.

## 2. Non-goals

- A key, a command or a dialog for usage. There is no `/usage`, and no per-attempt table. The ACP method `_ask/session/usage` is the detailed view.
- A budget or a limit. The cost never changes color.
- A change of the reply meta line (`gpt-5.5 · 5.4s · ↑178k ↓1.3k · $0.270`). It stays display only.
- A change of the header row, the block pane layout, the status row, the queue rows, the prompt pane, the shortcut bar or the viewer.
- A change of the quota block, the rate-limit block and the retry banner. Section 4 shows them for contrast only.
- Compaction, a "compact now" action, and a model switch on overflow (not in H9).
- Cost in `/model` or in the session selector.

## 3. Placement

| Part | Change |
|---|---|
| footer (`internal/tui/footer.go`) | One new part in the right side: the usage part, between the thinking level and the context meter. The meter reads the server estimate. The row stays one row. |
| block pane (`translate.go`, `translate_errors.go`) | One new headline for the `context` error kind (section 10, texts). Same block, same layout as every error block. |
| header, status, queue, prompt pane, bar, viewer, selectors | No change. |

What it replaces:

- The rule "the footer row shows no session cost" (`footer.go:10-18`, `docs/tui-architecture.md:93`, `docs/tui-style-guide.md:96`, `internal/tui/README.md:119-120`). This reverses a user decision of 2026-10-28 (footer design D9 and D14, [261028 design](../261028-1310-account-model-discovery/design.md), "no cost in any form or at any width"). The newer user decision of 2026-11-20 ([plan.md](plan.md): decision 2A and answered question 3) asks for the cost in the footer, so this design follows it. The user confirms the final form with this review.
- The context figure that the client computes from the last reply (`update_session.go:469-498,521-526`, `footer.go:58-65`, `session.go:182`). The footer reads one server number. The client figure is removed, not kept beside it.

Nothing else moves. The block pane keeps 17 rows at 80x24.

## 4. Sketches

Common data. A new session starts with `openai/gpt-5.5` (window 1 000 000 tokens, thinking level `high`) in `~/src/askcore`. The sessions below have two turns that cost $0.150 and $0.270. The footer total is $0.42. The last request had 179 700 tokens in the context, which is 18% of the window.

Screen parts are named as in `internal/tui/README.md`: row 1 is the header, the rows between the two rules are the block pane (and the status row when one exists), the three last rows are the prompt pane, the footer and the bar. The footer is the second-to-last row in every sketch. The cursor is in the prompt pane after the `❯`; it is not drawn.

### 4.1 No usage yet (80x24)

A new session, or a restored session before the first usage answer. The usage part is absent. The context meter is the empty bar and `0%`.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                             openai/gpt-5.5 ◐ high  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.2 Cost known (80x24)

Every attempt had usage and a known price. The part is `$0.42` in `muted`.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  gpt-5.5 · 6.1s · ↑96.2k ↓2.1k · $0.150                                        
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  gpt-5.5 · 5.4s · ↑178k ↓1.3k · $0.270                                         
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.3 Partial cost (80x24)

At least one attempt has no usage, or a model of an earlier attempt has no price. `≥ $0.42` means the true cost is this or more. The earlier attempt that had no usage is not visible in the pane; the footer is the only mark.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  gpt-5.5 · 6.1s · ↑96.2k ↓2.1k · $0.150                                        
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  gpt-5.5 · 5.4s · ↑178k ↓1.3k · $0.270                                         
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                   openai/gpt-5.5 ◐ high  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.4 Unknown price: tokens (80x24)

`alibaba/deepseek-v4.1-flash` has no known price. The part is `↑275k ↓3.4k` in `muted`: tokens in and tokens out of the whole session. The reply meta lines show no cost for this model, as today.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  deepseek-v4.1-flash · 6.1s · ↑96.2k ↓2.1k                                     
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  deepseek-v4.1-flash · 5.4s · ↑178k ↓1.3k                                      
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore  alibaba/deepseek-v4.1-flash ◐ high  ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

The same state when an attempt also lacks usage. The marker `≥` goes before the tokens (open question 3):

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  deepseek-v4.1-flash · 6.1s · ↑96.2k ↓2.1k                                     
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  deepseek-v4.1-flash · 5.4s · ↑178k ↓1.3k                                      
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/…/askcore  alibaba/deepseek-v4.1-flash ◐ high  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.5 Unknown context window (80x24)

A `models.json` model with no `contextWindow`. The lone `?` in `muted` replaces the bar and the percent, as today. The model has no `cost` entry, so it is free (`$0.00`), and the cost part stays.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  qwen3-coder · 6.1s · ↑96.2k ↓2.1k                                             
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  qwen3-coder · 5.4s · ↑178k ↓1.3k                                              
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                                 lmstudio/qwen3-coder  $0.00  ? 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.6 Long model id: the id gives way first (80x24)

`alibaba-token-plan/deepseek-v4.1-flash` with a partial cost. The directory shortens to `~/…/askcore`, then the end of the model id is cut with `…`. The cost part stays whole. This is the width rule of section 7.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  deepseek-v4.1-flash · 6.1s · ↑96.2k ↓2.1k · $0.150                            
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  deepseek-v4.1-flash · 5.4s · ↑178k ↓1.3k · $0.270                             
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4.… ◐ medium  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

With the same model and a partial token part, the id is cut down to 6 columns (`deepse…`, marker included) and the usage part stays whole. The usage part drops only when the id would keep fewer than 6 columns (strips 70 columns, 4.12):

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline.                                                                     
                                                                                
  deepseek-v4.1-flash · 6.1s · ↑96.2k ↓2.1k                                     
                                                                                
  ❯ raise the deadline to 5 s and rerun it                               14:05  
                                                                                
  Done. TestLeaderStale passes in 1.8 s on the CI image.                        
                                                                                
  deepseek-v4.1-flash · 5.4s · ↑178k ↓1.3k                                      
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/…/askcore alibaba-token-plan/deepse… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.7 Explicit overflow (80x24)

The provider refused the request. The run ends with `CONTEXT_WINDOW_EXCEEDED`. The headline is the new text of section 10; it replaces the provider sentence. The estimate is the last good usage plus the new prompt, so it is over the window: the bar is full, and the bar and the percent use `error`.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ summarize every file in internal/ and docs/                          14:11  
                                                                                
  ✗ Error (context): The conversation is larger than the context window of the  
    model.                                                                      
    Start a new session with /new, or choose a model with a larger window       
    (ctrl+l). Then send again.                                                  
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                    openai/gpt-5.5 ◐ high  $1.84  ▰▰▰▰▰▰▰▰ 101% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.8 Silent overflow (80x24)

The provider accepted the request and answered, and the reported input is over the window. The reply stays in the pane, then its meta line, then the error block. The rows of the error block are the same as in 4.7 apart from the headline.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ summarize every file in internal/ and docs/                          14:11  
                                                                                
  The internal/ tree has 41 packages. The agent, pipeline and sessions          
  packages hold the runtime core.                                               
                                                                                
  gpt-5.5 · 41s · ↑1.0M ↓4.1k · $2.10                                           
                                                                                
  ✗ Error (context): The provider reported more input tokens than the context   
    window of the model.                                                        
    Start a new session with /new, or choose a model with a larger window       
    (ctrl+l). Then send again.                                                  
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                    openai/gpt-5.5 ◐ high  $2.97  ▰▰▰▰▰▰▰▰ 101% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.9 Quota block, unchanged (80x24)

Code `QUOTA`: `errorKinds` text (`translate_errors.go:19`) and `providerErrorText` for status 402. Shown for contrast. The footer is normal.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ summarize every file in internal/ and docs/                          14:11  
                                                                                
  ✗ Error (quota): openai rejected the request (HTTP 402).                      
    Check the plan or the credit of the account, then send again.               
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.10 Retry banner, unchanged (80x24)

The status row and the note are copied from the golden `TestBlockGoldens/retry_wait` and `session_test.go:829`. The `↻` note uses `warning` and no `✗`. No error block exists for a retry that succeeds.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ summarize every file in internal/ and docs/                          14:11  
                                                                                
  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 5 scheduled.            
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ◌ Retrying (1/5) in 4s · rate limit · esc to cancel                           
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

### 4.11 Narrow, 40x14

Under 60 columns the footer has no directory and no usage part. Only the model and the percent stay (open question 1). The first sketch is the cost-known state. The next two show the error blocks at 40 columns.

```text
 ◢◤ ask                                 
────────────────────────────────────────
  ❯ raise the deadline to 5 s    14:05  
                                        
  Done. TestLeaderStale passes in 1.8   
  s.                                    
                                        
  gpt-5.5 · 5.4s · ↑178k ↓1.3k · $0.2…  
                                        
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
                     openai/gpt-5.5 18% 
 Tab blocks · / commands                
```

```text
 ◢◤ ask                                 
────────────────────────────────────────
  ✗ Error (context): The provider       
    reported more input tokens than     
    the context window of the model.    
    Start a new session with /new, or   
    choose a model with a larger        
    window (ctrl+l). Then send again.   
                                        
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
                    openai/gpt-5.5 101% 
 Tab blocks · / commands                
```

```text
 ◢◤ ask                                 
────────────────────────────────────────
  ✗ Error (quota): openai rejected the  
    request (HTTP 402).                 
    Check the plan or the credit of     
    the account, then send again.       
                                        
                                        
                                        
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
                     openai/gpt-5.5 18% 
 Tab blocks · / commands                
```

### 4.12 Width strips

These rows are the footer row at other widths. The first line under each title is a column ruler. Row text is the full row, padded to the width.

```text
120 columns, cost known, openai/gpt-5.5
123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/src/askcore                                                             openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 

100 columns, cost known, openai/gpt-5.5
1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/src/askcore                                         openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 

80 columns, cost known, openai/gpt-5.5
12345678901234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/src/askcore                     openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 

70 columns, cost known, openai/gpt-5.5
1234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/src/askcore           openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 

60 columns, cost known, openai/gpt-5.5
123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/src/askcore openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% 

59 columns, cost known, openai/gpt-5.5
12345678901234567890123456789012345678901234567890123456789
                                        openai/gpt-5.5 18% 

40 columns, cost known, openai/gpt-5.5
1234567890123456789012345678901234567890
                     openai/gpt-5.5 18%
```

Long model ids and the token part:

```text
80 columns, partial tokens, anthropic/claude-sonnet-5-5
12345678901234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore anthropic/claude-sonnet-5… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18% 

70 columns, partial tokens, anthropic/claude-sonnet-5-5
1234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore anthropic/claud… ◐ medium  ≥ ↑275k ↓3.4k  ▰▱▱▱▱▱▱▱ 18% 

60 columns, partial tokens, anthropic/claude-sonnet-5-5
123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore anthropic/claude-son… ◐ medium  ▰▱▱▱▱▱▱▱ 18% 

80 columns, partial cost, alibaba-token-plan/deepseek-v4.1-flash
12345678901234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4.… ◐ medium  ≥ $0.42  ▰▱▱▱▱▱▱▱ 18% 

70 columns, partial cost, alibaba-token-plan/deepseek-v4.1-flash
1234567890123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore alibaba-token-plan/deepseek-v4… ◐ medium  ▰▱▱▱▱▱▱▱ 18% 

60 columns, partial cost, alibaba-token-plan/deepseek-v4.1-flash
123456789012345678901234567890123456789012345678901234567890
 ⌂ ~/…/askcore alibaba-token-plan/d… ◐ medium  ▰▱▱▱▱▱▱▱ 18%
```

## 5. States and transitions

The footer keeps no state of its own. It reads the session view: the last `UsageTotals`, the model row and the terminal width.

### 5.1 The usage part

`tok` is `Input + Output + CacheRead + CacheWrite` of `total`. `cost` is `total.cost.total`. The rules run top to bottom; the first match wins.

| # | Condition | Part | Example |
|---|---|---|---|
| 1 | `tok` is 0 and `cost` is 0 (no totals yet, an empty log, or only refused attempts) | none | (nothing) |
| 2 | `costComplete` is true | cost | `$0.42`, `$0.00` |
| 3 | `costComplete` is false and `cost` is above 0 | `≥` and cost | `≥ $0.42` |
| 4 | `costComplete` is false and `cost` is 0, `complete` is true | tokens | `↑275k ↓3.4k` |
| 5 | `costComplete` is false and `cost` is 0, `complete` is false | `≥` and tokens | `≥ ↑275k ↓3.4k` |

Notes:

- Rule 1 separates a new session from a free model after a reply. A free model has `tok` above 0, so it shows `$0.00` (rule 2). The faux provider has a free tier, so a faux session shows `$0.00` after its first reply. A first reply that reported no usage shows nothing, because no number is known.
- A session that mixes free attempts and attempts with an unknown price has `cost` 0 and `costComplete` false. It shows tokens (rule 4). A cost of `≥ $0.00` would tell the user nothing.
- The footer needs no new field. The five rules use only `total`, `complete` and `costComplete` of `UsageTotals` (`design.md`, "Interfaces").

### 5.2 The context meter

- The percent is `contextTokens` of the server estimate over the context window of the model row. It is rounded to the nearest whole number. It is not capped at 100. It shows `999%` for any value above 999.
- `contextTokens` 0 (an empty log) shows the empty bar and `0%`, as today.
- The window comes from the model row of the session model, as today (`windowUnknown`, `footer.go:95`). The row changes at once on a model change; the totals would arrive later. `contextWindow` of the totals is not read (open question 10).
- The window is known and the row has `ContextWindow` 0: `?`. The row is not known yet: the bar, as today.
- Roles: `accent` under 70%, `warning` from 70%, `error` from 90%. Unchanged.

### 5.3 Transitions

| State | Trigger | Next state |
|---|---|---|
| New session | the first prompt | the footer is as before (rule 1) until the first usage arrives |
| Any | `agent_settled` with `usage` | the footer reads cost, flags and `contextTokens` from the event. No extra call. |
| Any | `agent_settled` without `usage` (an older peer) | the footer keeps its last values; the call of the current code path refreshes them |
| Running | `turn_end` (mid-run) | the view calls `Client.Usage`. The result updates the footer if the session and the run sequence are still the same. |
| Any | a snapshot (load, attach, resync) | the view calls `Client.Usage`. Same guard. A restored session shows its cost as soon as the answer comes. |
| Any | a usage result of an older run sequence or of another session | dropped, the footer does not change |
| Any | `Client.Usage` returns an error | the footer keeps its last values; no error text (the part is display only) |
| Running | a failed attempt, then `auto_retry_start` | the footer does not change; the status row shows the retry (4.10) |
| Retry succeeds | `agent_settled` | totals include both attempts (the server fold). The cost part may show the first attempt as partial (rule 3) when it had no usage and streamed output. |
| Any | `model_update` | cost and tokens stay. The percent uses the new window and the same token estimate, so it can pass 100% (the new window is smaller). `?` shows when the new row has no window. |
| Any | a silent or explicit overflow | the error block shows (4.7, 4.8). The footer meter shows the estimate, over 100%, in `error`. |
| Cancelled run | `agent_settled` | the footer updates as for any run. The cancel marker is not changed. |
| Any | a new session (`/new`) | the footer clears the usage part (rule 1) and shows `0%` |
| Detached or lost link | any | the footer keeps the last values. The status rail and the notices report the link, as today. |

The "stale" case: between a reply and the next usage answer the footer shows the figures of the earlier answer. The error block of an overflow can show before the footer meter moves over 100%; the next usage answer fixes it. No transition blanks or flashes a number.

## 6. Keys and focus

There is no new key and no new focus target. The footer is display only; a click on it does nothing, as today. Input order follows `docs/tui-architecture.md`: exit and cancel, then dialog, then completion, then editor, then application shortcuts. Nothing in this design touches that order. `ctrl+l` (model) is named in the next-action row of the `context` error; it is the same key as in the shortcut bar.

## 7. Limited space and resize

Give-way order of the footer row, when it is too wide. Steps 1 and 4 and the short row of step 5 are today's behavior (`footerText`, `footer.go:115-173`). The usage part in steps 2, 3 and 5 is new.

1. The directory shortens to its last element (`~/…/askcore`). It never goes from 60 columns.
2. The end of the model id is cut with `…`. The provider part stays whole. The usage part stays whole. The id is cut down to 6 columns, marker included, before step 3 applies.
3. When the id would keep fewer than 6 columns, the usage part drops. The id is then cut as today.
4. The rest of the model text (today's last resort).
5. Under 60 columns the row is short: no directory, no thinking level and no usage part. The row is `<provider>/<model> <pct>% ` at the right. The usage part goes first, with the directory, and only the model and the percent stay.

Why the id gives way before the cost: the usage part is the new information, and the model is still named by its provider and the start of its id. The floor of 6 columns keeps the id readable. At 80 columns, `openai/gpt-5.5` and `anthropic/claude-sonnet-5-5` keep the usage part for a cost; for a long provider name such as `alibaba-token-plan/deepseek-v4.1-flash`, the id is cut down to 6 columns and the usage part stays at 80 columns (sketch 4.6, second screen); at 70 columns the id would keep fewer than 6 columns, so the usage part drops (strips 4.12).

Resize: the row is computed again at each render from the width. A resize across 60 columns adds or removes the directory, the level and the usage part at once. The percent and the state do not change.

Other parts: the error blocks wrap at the block width (width minus 6 columns), as every error block does (sketches at 80 and 40 columns).

## 8. Text and width

- All text is one row of single-width characters. `↑`, `↓`, `≥`, `$` and the digits take one column in the width function of the code (`ansi.StringWidth`, `internal/tui/width.go:21`). In a terminal that draws East Asian ambiguous characters as two columns, `≥`, `↑` and `↓` can take two (open question 5).
- Cost format (footer only, open question 2): under $0.01, three decimals (`$0.004`); from $0.01 to $99.99, two decimals (`$0.42`); from $100, whole dollars (`$123`). No thousands separator. With the marker the part is up to 8 columns (`≥ $99.99`).
- Token format: the same `formatTokens` as the meta line (`999`, `3.4k`, `275k`, `1.2M`). `↑` is `total.input`, `↓` is `total.output` (open question 4). The token part is up to 15 columns (`≥ ↑999k ↓999k`).
- The usage part is separated from the thinking level and from the meter by two spaces, as the level and the meter are today.
- Truncation marker: `…`. It cuts the model id and the directory. It never cuts the usage part: the part is whole or it drops.
- Model ids, paths and provider text are untrusted text; the existing clean and wrap functions apply. The new headlines are fixed text and hold no provider text.

## 9. Style intent

Roles come from `docs/tui-style-guide.md`. No new role.

| Part | Role |
|---|---|
| usage part (`$0.42`, `≥ $0.42`, `↑275k ↓3.4k`) | `muted` |
| context bar and percent | `accent`, `warning` from 70%, `error` from 90% (unchanged) |
| model and level | `assistant` (unchanged) |
| error block (`✗`, headline) | `error`; next-action row `muted` (unchanged) |
| retry note `↻` | `warning`; text `fg` (unchanged) |
| retry status row | `warning` word, `muted` tail (unchanged) |

Readable without color: the usage part is plain text and glyphs. The overflow is clear from the `✗ Error (context)` text, not from color.

Style guide changes that phase 7 makes (text only here; this design does not edit the guide):

- Glyph table, new row: `≥` "At least: marks a partial total in the footer (`≥ $0.42`)".
- Glyph table, new rows: `↑` and `↓` "Tokens in and tokens out" (the meta line uses them today, but the table does not list them).
- Footer row pattern: replace "shows no logo, no link state and no session cost" with "shows no logo and no link state". Add the usage part between the level and the meter, in `muted`, and the give-way order of section 7.
- `docs/tui-architecture.md` (screen composition, footer numbers) and `internal/tui/README.md` (footer row, footer numbers): the same change, and "context use is the server estimate".

## 10. Texts (ASD-STE100)

Error headline for the `context` kind. Both forms end the run with the same next action.

| Form | Row |
|---|---|
| Explicit overflow | `Error (context): The conversation is larger than the context window of the model.` |
| Silent overflow | `Error (context): The provider reported more input tokens than the context window of the model.` |
| Next action (both) | `Start a new session with /new, or choose a model with a larger window (ctrl+l). Then send again.` |

Rules:

- The block uses the existing error block (`NewErrorBlock`). The kind word `context` comes from `errorKinds` (`translate_errors.go:21`).
- The translator uses the explicit text for `CONTEXT_WINDOW_EXCEEDED` when the run has an error reply, and the silent text when it has none. The silent case has no provider text, so the fallback "The request failed." never shows.
- The next action text changes from `The conversation is too long for this model. Start a new session with /new, then send again.` to the text above. The old text repeated the headline and did not name the model choice (open question 6).
- The first headline agrees with the ACP text (`the conversation is larger than the context window of the model`). The second agrees with the `ask -p` text for a silent overflow in [phase 5](phase-05-acp-and-headless-surfaces.md).
- The quota text, the rate-limit text and the retry texts do not change.

How the three differ on the screen:

| Case | Mark | Kind word | Next action |
|---|---|---|---|
| Overflow | `✗` `error` block | `context` | start a new session or choose a larger model |
| Quota (402, usage limit) | `✗` `error` block | `quota` | check the plan or the credit |
| Rate limit, transient | `↻` `warning` note and the status row `Retrying (n/m)`. An `✗ Error (rate limit)` block only when the retries end. | `rate limit` | wait a moment |

## 11. Acceptance criteria

Each criterion maps to one named test in phase 6. Golden tests use `startView` and `requireGolden`; the sketches in section 4 are the reference. A golden is refreshed only after a diff against the approved sketch.

1. At 80x24, after a reply with a known price, the screen shows `$0.42` in the footer row, between `◐ high` and the bar (`TestFooterShowsSessionCost`).
2. At 80x24, after a reply when an attempt has no usage and `cost` is above 0, the footer row shows `≥ $0.42` (`TestFooterMarksPartialCost`).
3. At 80x24, after a reply on a model with no known price, the footer row shows `↑275k ↓3.4k` and no `$` (`TestFooterShowsTokensWithoutPrice`). With `complete` false, it shows `≥ ↑275k ↓3.4k` (`TestFooterMarksPartialTokens`).
4. At 80x24, in a new session, the footer row shows `▱▱▱▱▱▱▱▱ 0%` and no `$` and no `↑` (`TestFooterShowsNoUsageBeforeFirstReply`).
5. At 80x24, after one reply of the faux provider, the footer row shows `$0.00` (`cmd/tui/interactive_test.go`, `TestInteractiveFooterShowsCostAfterReply`).
6. At 80x24, when the fake client returns `contextTokens` 180 000 and the model window is 1 000 000, the footer row shows `▰▱▱▱▱▱▱▱ 18%` (`TestFooterUsesServerContextEstimate`).
7. At 80x24, with a model whose window is 0, the footer row shows `$0.00  ?` and no bar (`TestFooterShowsUnknownWindowWithCost`).
8. At 80x24, when `contextTokens` is 1 010 000 for a 1 000 000 window, the footer row shows `▰▰▰▰▰▰▰▰ 101%` in `error` (`TestFooterShowsOverWindowEstimate`).
9. At 80x24, with `alibaba-token-plan/deepseek-v4.1-flash` and a partial cost, the footer row shows `~/…/askcore`, the id cut with `…`, and `≥ $0.42` whole (`TestFooterCutsLongIDBeforeCost`). With a partial token part, the id is cut down to 6 columns (`deepse…`) and `≥ ↑275k ↓3.4k` stays whole at 80 columns; at 70 columns the id would keep fewer than 6 columns, so the footer row has no usage part (`TestFooterDropsUsageForLongID`).
10. At 60, 59 and 40 columns, the footer row follows the strips of 4.12: no usage part at 59 and 40, the usage part at 60 (`TestFooterUsageByWidth`). After a resize from 80 to 40, the usage part and the directory are gone, and the percent is the same.
11. At 80x24, after an explicit overflow, the screen shows the explicit headline and the next action of section 10 in the block pane (`TestExplicitOverflowShowsContextBlock`).
12. At 80x24, after a silent overflow, the screen shows the reply, its meta line, then the silent headline and the next action. It does not show `The request failed.` (`TestSilentOverflowShowsContextBlock`). At 40x14, the block wraps as in 4.11.
13. At 80x24, three runs (a 402, a 429 then success, an overflow) show three different results: `Error (quota)`, a `↻` note with the `Retrying` status row, and `Error (context)` (`TestQuotaRetryAndOverflowLookDifferent`).
14. At 80x24, the retry status row and the `↻` note have the same text as before (`TestBlockGoldens/retry_wait` and `session_test.go` stay unchanged).
15. At 80x24, after a usage result that belongs to an older run sequence, the footer row does not change (`TestFooterIgnoresStaleUsageResult`). After a failed `Client.Usage`, the footer row keeps its last values and no error text shows (`TestFooterKeepsValuesWhenUsageFails`).
16. At 80x24, after `/new`, the footer row has no usage part (`TestFooterClearsUsageOnNewSession`).
17. Existing goldens: `TestFooterRowGoldens` does not change at 40x14 (no usage part). The 80x24 and 120x40 goldens change on purpose only when the fake client has usage.

## 12. Open questions

Each has a default. The design above uses the default.

1. **Cost under 60 columns.** The phase file says "under 60 columns the cost goes first". This design reads "goes" as "leaves", as in the other rules of the code, so a narrow row has no cost. A user on a narrow terminal then never sees the cost. Option: keep a short part at the left of the short row (`$0.42` or `≥ $0.42`, `muted`; 40 columns have room). Default: leave it out. Keep or add?
2. **Cost format.** The approved example is `$0.42`. The reply meta line uses `$0.420` (three decimals under $1000, `formatCost`). The footer here uses `$0.42` and `$0.004` below one cent. The total and the per-reply cost then differ in format. Default: the format of section 8. Or reuse `formatCost`?
3. **`≥` on tokens.** The user approved the marker for a cost. Putting it on the token part (`≥ ↑275k ↓3.4k`, 4.4) goes one step further, to say "at least" when an attempt has no usage. Default: yes. Or tokens never have a marker?
4. **What `↑` counts.** Default: `input` only, as the meta line does. On a cached session `input` can be far below the tokens that the provider read. Option: `input + cacheRead + cacheWrite`.
5. **The `≥` width.** `≥` is an East Asian ambiguous character. It takes one column in Ask's width function and in most terminals. Option: `>=` or `~` for an always-single-width mark. Default: `≥`, as in the approved example.
6. **Explicit overflow text.** Today an explicit overflow shows the provider sentence (for example `anthropic rejected the request (HTTP 400).`), which does not say "overflow". The design replaces it with a fixed sentence and changes the next action for both forms. This changes behavior that exists today. Default: replace. Or keep the provider sentence for the explicit form and add only the silent text?
7. **Priority of the usage part.** The id gives way first, down to 6 columns (section 7). Option: the usage part drops first, so the model id keeps its full text at every width. Default: the id gives way first.
8. **Overflow look.** Overflow and quota use the same `error` block and differ by the kind word and the next action. Option: a different mark for overflow. Default: no new glyph or role.
9. **Estimate mark.** The percent is an estimate. Option: `~18%`. Default: no mark, as today.
10. **Window source.** The footer takes the window from the model row and ignores `contextWindow` of the totals. This keeps one source at each point and updates at once after a model change. Option: read the totals window when it is above 0. Default: model row.
11. **Preview page.** The skill and the phase file ask for `artifacts/preview.html`. This step wrote only this file, so the preview and its generator do not exist yet. Default: build them after the first review, before the approval. Needed?
