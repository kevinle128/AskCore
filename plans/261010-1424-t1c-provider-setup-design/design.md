---
title: "Provider setup TUI design: /providers, /model and first run"
design-status: approved     # draft | approved
design-approved-by: "Kevin Le (user)"   # filled only at approval
design-approved-on: "2026-10-10"       # YYYY-MM-DD, filled only at approval
---

# Provider setup TUI design: `/providers`, `/model` and first run

This design covers what the user sees and types to add or remove a provider, to pick a model from the providers that work, and to start Ask for the first time with no provider.
It follows the `ask-tui-design` skill.
No Go code for this feature exists until this file is `approved`.
The local TUI design is `approved`; this design changes parts of it (the model selector, the footer and the first-run state) and so it needs its own approval.

### Changes since the first approval

The user approved this design on 2026-10-10, then made four decisions (2026-10-10) that change visible text. The status is `draft` again until the user approves these changes. Review only these states:

| Decision | Changed or new states | Change |
|---|---|---|
| Claude Pro/Max flow | P15 (redrawn), P15a (new), P16 (redrawn) | The leader starts the browser flow; the box takes a pasted callback address (masked). If the callback port is busy, the box asks for a code |
| Default model | P25a (new), P28c (new), P29 (rail line added) | Choosing a model in `/model` also saves it as the default; a session start that cannot use the saved default says so; `/model` tags the saved default |
| TEA_TRACE warning | P6a (new), P28b (new), security criteria | A warning in the boxes with a secret input and once at start; the security criteria hold when TEA_TRACE is not set |
| Removal during another sign-in | P12c (new) | The removal wins; the other client's box shows why its sign-in stopped |

The rows of sections 5, 6, 10 and 11 that name these states are changed too.

Second round (user decision, 2026-10-10, after the second approval):

| Decision | Changed or new states | Change |
|---|---|---|
| Alibaba Token Plan key without a live check | P6b (new), P9a (new), P9b (new), states table rows P7 and P9a, criterion for P9b | The leader saves the key without a check, the key box says so, there is no `Checking…` step, and a bad key shows the auth error block at the first prompt |

Sources:
[TUI architecture](../../docs/tui-architecture.md),
[TUI style guide](../../docs/tui-style-guide.md) (direction B4),
[internal/tui README](../../internal/tui/README.md),
[local TUI design](../261009-2239-t1a-local-tui/design.md) (in-process agent, model selector, session selector, footer, keys, width rules),
[provider and model flow analysis](../reports/xia-261010-1416-provider-model-flow.md) (omp, Pi and Grok behavior),
and the real sign-in methods in `cmd/tui/auth_args.go` and `cmd/tui/auth.go`.

User decisions of 2026-10-10 that this design keeps and does not reopen:

- The list is one flat list of login options, one row for each way to sign in, as in the omp provider screen (user decision, 2026-10-10, after the omp screen was reviewed). There is no separate method step.
- The Alibaba Token Plan key is saved without a live check, because no free read-only check is known; the key box says so, and a bad key fails at the first prompt with the usual auth error (user decision, 2026-10-10).
- The Claude Pro/Max row starts the browser flow on the leader; the same box takes a pasted callback address; if the callback port is busy, the leader uses the copy-code flow and the box asks for the code (user decision, 2026-10-10).
- Choosing a model in `/model` switches the session and saves it as the default, as omp does. A new session starts with the saved default if its provider is usable; else the default model of a usable provider; else the first usable model in catalog order; else no model. Adding a provider does not set the default (user decision, 2026-10-10).
- `TEA_TRACE` stays usable. When it is set, the TUI warns that typed and pasted text, keys included, is written to the trace file (user decision, 2026-10-10).
- A removal while another client signs in to the same provider is allowed and wins; the other client's box says why its sign-in stopped (user decision, 2026-10-10).
- The provider and the model of a session show on one line under the header, and on a new line each time the model changes (user decision, 2026-10-10: option A only; the user dropped option D, so the reply meta line keeps the bare model id of the earlier design). The footer keeps the bare model id.
- The command is `/providers`. It lists, adds and removes providers. It is the "sign in" part of the omp screen only. There is no web search tab.
- `/model` lists only the models of usable providers. When there are none, it shows `Use /providers to add providers.`
- A provider that is not signed in is hidden in `/model` and shown in `/providers` with its state.
- An API key is pasted into a masked input and sent once to the leader.
- OAuth is done by a browser URL, a copy-code or a device code, and the TUI shows what the user must do.
- On first run with no usable provider there is no default model. The footer shows `no model`. A notice says `Add a provider with /providers`.
- Custom OpenAI-compatible endpoints are out of scope.
- Slash command names are not case sensitive.
- The TUI never shows or logs a secret.

## 1. Outcome

The user types `/providers` and sees one row for each way to sign in (seven rows today). A row that has a credential shows where it comes from: `signed in`, `api key` or `env <NAME>`.
The user picks a row and finishes the sign-in in the TUI at once: paste a key, open a URL, paste a code, or enter a device code. A row that already has a saved credential offers sign in again or remove.
When the sign-in works, a note in scrollback names the result, a status line says how many models are ready, and `/model` lists the models of that provider.
The user can remove a saved sign-in. The note says when an environment variable still makes the provider usable.
Every session shows which model of which provider it uses, for example `alibaba-token-plan/deepseek-v4.1-flash`, because two providers can offer a model with the same name.
On first run with no usable provider, Ask opens with `no model` in the footer and a notice that names `/providers`. A prompt is not sent; the draft stays and a hint says what to do.

## 2. Non-goals

- A pair `provider/model` in the footer or in the reply meta line (the user dropped it, 2026-10-10). Both keep the bare model id of the earlier design (section 3, "Model line").
- Custom OpenAI-compatible endpoints and any provider that is not in the compiled catalog.
- A web search tab, a setup wizard, and a `/login` or `/logout` alias. `/providers` is the only entry.
- Choosing a model automatically after a sign-in. The user picks the model with `/model`.
- Changing the ACP wire format, the leader, or the credential store. This file lists what the TUI needs from the leader in section 3; the plan owns the wire design.
- Signing in from the in-process agent (no leader). Section 5, state P24, says what the user sees.
- Headless `ask -p` output and `ask auth login`. They stay as they are.
- A light theme.

## 3. Placement

The inline model from the architecture doc applies. Nothing is fullscreen.

| Slot | What this feature puts there |
|---|---|
| Scrollback | The start line and the change line of the model (see "Model line" below). Client-local note blocks after a sign-in, a failed sign-in, a cancel and a removal. They are blocks of the client transcript, so a resize rebuild draws them again. They are not part of the session history that the leader replays to other clients. |
| `status` (live rail `┊`) | The wait line of a pending check (`◌ Checking the key with openai…`), results for 3 s (`✓ openai signed in · 2 models`), the first-run notice, the no-model notice, and send refusals. |
| `completion` | The command list gains `/providers` after `/model`. |
| Editor slot | The `/providers` provider box, the row menu, the key box, the login boxes and the removal box. Each one replaces the editor and keeps the draft, as the model selector does. Only one of them is open at a time. |
| `footer` | `no model` in place of the model, the context bar and the cost, when there is no current model. |

`/providers` can open while a run is active, because it does not change the run. `/model` still waits for an idle session (local TUI rule). A removal asks for a confirmation that also says when a run is active and may use the provider.

### Model line

A session names its provider and its model in scrollback, as plain transcript blocks. They are not live-region slots.
`<pair>` below means `<provider>/<model id>`, for example `alibaba-token-plan/deepseek-v4.1-flash`. The provider id and the model id are the ids of the catalog.

| Line | Text | Where |
|---|---|---|
| Start line | `  model  <pair>` | Directly below the startup header, then one empty row |
| Change line | `  model  <new pair> · was <old pair>` | A new row in the transcript at the point where the model changed, then one empty row. The first model after a time with no model has no `· was` part |

When the model exists on two wire formats under the same provider, the pair on the start line and the change line keeps the format, as the `/model` list does: `  model  alibaba-token-plan/deepseek-v4.1-flash (anthropic-messages)` (P28a). 

When the session has no model (first run, or after the provider of the current model was removed), no model line shows. The `no model` footer and the notice stay (P1, P22).

Default model (user decision, 2026-10-10, as omp): choosing a model in `/model` switches the session and saves it as the default on the leader. A new session starts with the saved default if its provider is usable; else the default model of a usable provider (one default for each provider in the catalog); else the first usable model in catalog order; else no model. Adding a provider with `/providers` does not change the default. When the saved default is not usable, the start line names the model in use and one `muted` row under it, aligned with the pair, gives the reason: `Default model openai/gpt-5.5 is not usable (not signed in).` (P28c). It is in scrollback, not on the rail, so it stays next to the line it explains.

TEA_TRACE (user decision, 2026-10-10): when the environment variable `TEA_TRACE` names a file, the terminal library writes raw keyboard input to that file, pasted keys included. The TUI then shows `TEA_TRACE is on: typed and pasted text, keys included, is written to <file>.` in the `warning` role: once at start in scrollback (P28b), and inside every box with a secret input, above the input (the key box P6a, the Claude Pro/Max box P15 and P15a). The text has no glyph, because the style guide has no warning glyph.

The start line shows when:

| Event | Where the line goes |
|---|---|
| A session starts: the first frame after a leader start, after the in-process agent starts, or after `/new` | Under the startup header. After `/new` the header is not drawn again, so the line goes at the end of the transcript, after the session starts |
| The client attaches to a session as an observer (`/sessions`) | After the divider `── session <id> ──`, before the followed history (P30) |
| The session is restored after a reconnect | After the reconnect note `↻ Reconnected. Session <id> restored.` (P31) |
| The model changes: the `/model` selector, a change that the leader announces (a fallback after a readiness failure), or a cycle | A change line at the end of the transcript (P29) |

Only the leader's current model event creates a line. A key press does not. A late or stale model event (old generation) creates nothing.
The footer and the reply meta line keep the bare model id of the earlier design. A pair is up to 38 columns wide; at 80 columns the footer row has no room for it next to the directory and the meter, and the footer must stay one row.
The footer is always visible, so it shows the model at all times. The start line and the change line give the provider.

### Boxes

All boxes use the rounded box of the style guide: `╭─ Title ───╮`, rows, `╰───╯`, and one `dim` italic hint row below the box.
The provider box is a selector (selected row, window, and a search row at the bottom). The row menu is a selector with two rows.
The key box, the login boxes and the removal box are dialogs: text rows, at most one input row, and the hint row.
A dialog has no search row.

### Provider rows

A row is one way to sign in. It has a selection mark, the provider dot, the name of the option, and at the right a status suffix when the row has a credential.
The rows below come from `cmd/tui/auth_args.go` (methods), `cmd/tui/auth.go` (flows) and `internal/providers/catalog.go` (providers). The TUI asks the leader for the list; the sketches use these seven rows.

| Row name | Provider and method | Flow on enter |
|---|---|---|
| `Alibaba Token Plan API key` | `alibaba-token-plan`, `api-key` | Key box |
| `Claude Pro/Max account` | `anthropic`, `anthropic-oauth` | Browser box with a masked address input; a code box when the callback port is busy |
| `Anthropic API key` | `anthropic`, `api-key` | Key box |
| `ChatGPT Plus/Pro subscription` | `openai`, `openai-chatgpt` | Browser box (browser only) |
| `OpenAI API key` | `openai`, `api-key` | Key box |
| `xAI Grok account` | `xai`, `xai-oauth` | Device-code box |
| `xAI API key` | `xai`, `api-key` | Key box |

Rows are ordered by provider id, with the account row before the API key row, so the provider dots keep the same role in `/providers` and in `/model`.
The Claude Pro/Max row: one authorize URL cannot finish in the browser and also take a pasted code, because the redirect address is part of the URL. So the leader starts the browser flow first, with the callback on the leader host. The same box takes a pasted callback address: the address that the browser opened, for a browser on another machine (P15). The address carries a one-time code, so the input is masked like a key. If the callback port is busy, the leader starts the copy-code flow instead, and the box asks for the code that the page shows (P15a). The user does not choose.

| Status suffix | Meaning | Role |
|---|---|---|
| `signed in` | A saved OAuth sign-in for this row. | `success` |
| `api key` | A saved API key for this row. | `success` |
| `env <NAME>` | No saved key; the environment variable `<NAME>` makes the provider usable. | `success` |
| (none) | This row has no credential. | none |

A provider is usable when any of its rows has a credential. A provider holds one saved credential at a time, because a sign-in replaces the saved one. So at most one row of a provider shows `signed in` or `api key`.
Enter on a row with a saved credential opens the row menu (P5): `Sign in again` or `Remove`. Enter on an `env` row or a row with no suffix starts its sign-in at once.
Delete on a row with a saved credential asks to remove it (P19) without the menu.

### What the TUI needs from the leader

These are data needs, not a protocol. The plan decides names and messages.

| Need | Used for |
|---|---|
| A list of login options: provider id, method id, a display name, the state of the credential (`signed in`, `api key`, `env`, `none`), the env variable name, and a providers generation number. The model count of each provider. | P3, P4, P27 |
| Start a sign-in: returns a login id and one of: nothing to show (key), a URL (browser), a URL that also accepts a code (copy-code), or a page address and a user code (device). A refusal reason such as `provider_busy`. | P5 to P18, P12a |
| Send one secret (key, code or callback address) for a login id. Result: saved, or a refusal reason. The account name, when known. | P6 to P8, P15, P16 |
| Cancel a login id. A closed client connection also cancels it. A removal of the provider by another client cancels it with the reason `removed by another client`. | P13, P12b, P12c |
| For the Claude Pro/Max row: which flow started (browser with address input, or copy-code). | P15, P15a |
| Save the chosen model as the default when the session model is set from `/model`. At session start: the model in use and, when the saved default was not usable, the default and the reason. The saved default, so `/model` can tag it. | P25a, P28c, P29 |
| Remove a saved credential. Result: removed, plus whether an environment variable still applies, plus whether the current model became unusable. | P19 to P23 |
| An announcement `providers changed` with the new generation. Every connected client gets it. | P25, P27 |
| A model list that has only usable models, with the provider of each. The old rows stay available for old clients (versioned or a new field). | P25, P26 |
| A current model that can be empty. | P1, P2, P22, P23 |

The TUI sends a secret once, for one login id, and never keeps it after the reply.

## 4. Sketches

Each sketch has the exact terminal width.
The line `-- live region starts --` is not on screen; it marks where the live region begins.
`▌` is the cursor.
Every key, URL, code and account in the sketches is a fake placeholder. The domain `example.test` does not exist.
**Visual preview:** open [artifacts/preview.html](artifacts/preview.html) in a browser. It shows the same sketches as terminal windows with role colors and a caption for each state.

States with a layout that does not change with width are drawn at 80×24 only; section 7 gives the width rules.
The provider list keeps the same layout at 120 columns, so it is drawn at 80×24 only.
Narrow cases at 40 columns show the first run, the provider list with its search row, and the URL wrap.
The sketches are generated by [artifacts/sketch-gen.py](artifacts/sketch-gen.py) and checked by [artifacts/verify-widths.py](artifacts/verify-widths.py).
To change the design, edit [artifacts/design-template.md](artifacts/design-template.md) or the generator, then run `python3 artifacts/make-design.py` from the plan directory; it rebuilds this file, checks every sketch width and writes `artifacts/preview.html`.

#### P1. First run: no provider

No provider is usable, so there is no default model. The header has no model line, the footer shows no model, and a notice names the next step.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
                                                                                
```

`-- live region starts --`

```text
┊ Add a provider with /providers · then choose a model with /model              
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

(17 empty rows below)

40×14:

```text
◢◤ ask  v0.1.0 · 7c1e                   
                                        
```

`-- live region starts --`

```text
┊ Add a provider with /providers        
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e                       no model 
```

(7 empty rows below)

#### P2. First prompt with no model

You pressed enter. Nothing is sent, the draft stays, and the live rail gives a hint, not an error trace.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
                                                                                
```

`-- live region starts --`

```text
┊ ✗ Not sent: no model · draft kept · Add a provider with /providers            
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ explain how the leader starts▌                                             │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

(17 empty rows below)

#### P2a. Command list shows /providers

You typed /p. The command list opens above the editor, as for the other commands.

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
  ❯ /providers Add or remove providers                                          
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ /p▌                                                                        │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P3. /providers: one row for each login option

/providers replaces the editor with the provider box. Each row is one way to sign in; a row that has a credential shows where it comes from. The search row is always at the bottom. The draft is kept.

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
                                                                                
```

`-- live region starts --`

```text
╭─ Providers ──────────────────────────────────────────────────────────────────╮
│   ● Alibaba Token Plan API key                                       api key │
│   ● Claude Pro/Max account                                                   │
│   ● Anthropic API key                                  env ANTHROPIC_API_KEY │
│   ● ChatGPT Plus/Pro subscription                                  signed in │
│   ● OpenAI API key                                                           │
│ ❯ ● xAI Grok account                                                         │
│   ● xAI API key                                                              │
│  ⌕ ▌Type to search                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter sign in  ·  esc close  ·  your draft is kept                           
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P4. /providers at 40 columns

The box shows a window of three rows and a row that says how many are hidden. Long names are cut with an ellipsis so the status stays. The list is scrolled to the selected row.

40×14:

```text
                                        
                                        
◢◤ ask  v0.1.0 · 7c1e                   
  alibaba-token-plan/deepseek-v4.1-flash
                                        
```

`-- live region starts --`

```text
╭─ Providers ──────────────────────────╮
│   ● Claude Pro/Max account           │
│   ● Anthropic… env ANTHROPIC_API_KEY │
│ ❯ ● ChatGPT Plus/Pro subs… signed in │
│   … 3 more                           │
│  ⌕ ▌Type to search                   │
╰──────────────────────────────────────╯
   enter manage · esc close             
  ● 7c1e        deepseek-v4.1-flash 18% 
```

#### P5. Enter on a signed-in row

A row that has a saved credential asks what to do: sign in again, or remove. Other rows start their sign-in at once.

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
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  Signed in. What do you want to do?                                          │
│                                                                              │
│ ❯ Sign in again  Use another account or refresh this one                     │
│   Remove         Delete the saved sign-in for openai                         │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter choose  ·  esc back  ·  your draft is kept                             
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P6. API key: masked input

The key is pasted or typed. It never appears on screen; the input shows a fixed hint.

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
╭─ OpenAI API key ─────────────────────────────────────────────────────────────╮
│  Paste your API key. It is sent once to the leader and is never shown.       │
│                                                                              │
│  key  ▌paste or type the key; it stays hidden                                │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter save  ·  esc cancel  ·  ctrl+u clear                                   
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P6a. API key with TEA_TRACE set

TEA_TRACE writes raw keyboard input to a file, so the box warns before you paste. The same rows show in the Claude Pro/Max box.

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
╭─ OpenAI API key ─────────────────────────────────────────────────────────────╮
│  Paste your API key. It is sent once to the leader and is never shown.       │
│                                                                              │
│  TEA_TRACE is on: typed and pasted text, keys included, is written to        │
│  /tmp/ask-trace.log.                                                         │
│                                                                              │
│  key  ▌paste or type the key; it stays hidden                                │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter save  ·  esc cancel  ·  ctrl+u clear                                   
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P7. API key: checking

The key went to the leader. The input is locked and shows (hidden). The wait is on the live rail above the box.

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
┊ ◌ Checking the key with openai… 2s · esc to cancel                            
╭─ OpenAI API key ─────────────────────────────────────────────────────────────╮
│  Paste your API key. It is sent once to the leader and is never shown.       │
│                                                                              │
│  key  (hidden)                                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
   esc cancel                                                                   
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P8. API key: refused

The leader refused the key. The reason is shown, the input is empty again, and nothing was saved.

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
╭─ OpenAI API key ─────────────────────────────────────────────────────────────╮
│  Paste your API key. It is sent once to the leader and is never shown.       │
│                                                                              │
│  key  ▌paste a new key                                                       │
│                                                                              │
│  ✗ openai refused the key: invalid_api_key (HTTP 401). Nothing was saved.    │
│    The key is cleared from this screen. Paste a new key to try again.        │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter save  ·  esc cancel                                                    
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P9. API key: saved on first run

The note is in scrollback, a status line names the result, and the footer still shows no model until you choose one.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
                                                                                
  ✓ openai: API key saved. 2 models ready.                                      
    Choose a model with /model.                                                 
                                                                                
```

`-- live region starts --`

```text
┊ ✓ openai signed in · 2 models · choose one with /model                        
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

(14 empty rows below)

#### P6b. API key for a provider that cannot be checked

The Alibaba Token Plan key is saved without a live check, and the box says so before you press enter.

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
╭─ Alibaba Token Plan API key ─────────────────────────────────────────────────╮
│  Paste your API key. It is sent once to the leader and is never shown.       │
│  This provider cannot be checked before use; the key is saved as it is.      │
│                                                                              │
│  key  (hidden) ▌                                                             │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter save  ·  esc cancel  ·  ctrl+u clear                                   
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P9a. API key saved without a check

There is no checking step. The note says the key was saved and cannot be checked before use, then names the next step.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
                                                                                
  ✓ alibaba-token-plan: Saved. This provider cannot be checked before use.      
    Choose a model with /model.                                                 
                                                                                
```

`-- live region starts --`

```text
┊ ✓ alibaba-token-plan key saved · 1 model · choose one with /model             
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

(14 empty rows below)

#### P9b. First prompt with a bad unchecked key

The key was wrong. The first prompt ends with the usual auth error block of the earlier design.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ explain how the leader starts                                        14:05  
                                                                                
  ✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).                
    Replace the key with /providers. Then send again.                           
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(12 empty rows below)

#### P10. Browser sign-in: waiting

The URL is shown in full, wrapped. ctrl+y copies it and the terminal can open it as a link.

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
                                                                                
```

`-- live region starts --`

```text
┊ ◌ Waiting for the browser… 12s · esc to cancel                                
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  Open this URL in a browser and sign in to openai:                           │
│                                                                              │
│  https://auth.example.test/oauth/authorize?response_type=code&client_id=ask- │
│  cli&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+prof │
│  ile&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256   │
│                                                                              │
│  This box closes by itself when you approve.                                 │
╰──────────────────────────────────────────────────────────────────────────────╯
   ctrl+y copy URL  ·  esc cancel                                               
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P11. Browser sign-in at 40 columns

The URL shows as many rows as fit, then a row that tells you ctrl+y copies the full URL. The cancel key always stays.

40×14:

```text
◢◤ ask  v0.1.0 · 7c1e                   
  alibaba-token-plan/deepseek-v4.1-flash
                                        
```

`-- live region starts --`

```text
┊ ◌ Waiting… 12s                        
╭─ ChatGPT Plus/Pro subscription ──────╮
│  Open this URL and sign in:          │
│  https://auth.example.test/oauth/aut │
│  horize?response_type=code&client_id │
│  =ask-cli&redirect_uri=http%3A%2F%2F │
│  127.0.0.1%3A1455%2Fcallback&scope=o │
│  … ctrl+y copies the full URL        │
╰──────────────────────────────────────╯
   ctrl+y copy URL · esc cancel         
  ● 7c1e        deepseek-v4.1-flash 18% 
```

#### P12. Browser sign-in: failed

The browser flow ended with an error. The reason is shown, and you can try again.

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
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  ✗ Sign-in failed: the browser returned access_denied.                       │
│                                                                              │
│  Nothing was saved. You can try again.                                       │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter try again  ·  esc close                                                
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P12a. Sign-in cannot start: another client is signing in

The leader allows one sign-in for each provider at a time. The box says so and nothing else changes.

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
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  ✗ Could not start: another client is signing in to openai.                  │
│                                                                              │
│  Try again in a moment.                                                      │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter try again  ·  esc close                                                
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P12b. Leader lost during sign-in

The connection dropped. The sign-in is not resumed; the dialog says what happened and nothing was saved.

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
┊ ✗ Disconnected from the leader · reconnecting (attempt 1, next in 1s)         
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  ✗ The sign-in stopped because the leader is gone.                           │
│                                                                              │
│  Nothing was saved. When the leader is back, start again with /providers.    │
╰──────────────────────────────────────────────────────────────────────────────╯
   esc close                                                                    
  ● leader lost   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P12c. Sign-in cancelled because another client removed the provider

Another client removed openai while this client was signing in. The removal wins, and this box says why.

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
╭─ ChatGPT Plus/Pro subscription ──────────────────────────────────────────────╮
│  ✗ Sign-in cancelled: another client removed openai.                         │
│                                                                              │
│  Nothing was saved. You can sign in again.                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter try again  ·  esc close                                                
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P13. Browser sign-in: cancelled

esc cancelled the sign-in. A marker stays in scrollback and the draft is back.

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
                                                                                
  ⊘ Sign-in to openai cancelled. Nothing was saved.                             
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P14. Browser sign-in: success

The leader returned the account name. The note names it, and the status line says how many models are ready.

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
                                                                                
  ✓ openai: signed in as dev@example.com. 2 models ready.                       
                                                                                
```

`-- live region starts --`

```text
┊ ✓ openai signed in · 2 models                                                 
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P15. Claude Pro/Max: browser flow, or a pasted address

The leader starts the browser flow. If the browser runs on another machine, you paste the address it opened. The address carries a one-time code, so the input is masked.

80×24:

```text
                                                                                
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
┊ ◌ Waiting for the browser or an address… 8s                                   
╭─ Claude Pro/Max account ─────────────────────────────────────────────────────╮
│  Open this URL and approve. The box closes by itself when you approve.       │
│  If the browser runs on another machine, paste the address it opened.        │
│                                                                              │
│  https://auth.example.test/oauth/authorize?response_type=code&client_id=ask- │
│  cli&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+prof │
│  ile&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256   │
│                                                                              │
│  address  ▌paste the address; it stays hidden                                │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter send address  ·  ctrl+y copy URL  ·  esc cancel                        
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P15a. Claude Pro/Max: the callback port is busy

The leader could not open the callback port, so it started the copy-code flow. The box asks for the code.

80×24:

```text
                                                                                
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
┊ ◌ Waiting for a code… 3s                                                      
╭─ Claude Pro/Max account ─────────────────────────────────────────────────────╮
│  The callback port is busy, so this sign-in uses a code instead.             │
│  Open this URL, approve, then paste the code that the page shows.            │
│                                                                              │
│  https://auth.example.test/oauth/authorize?response_type=code&client_id=ask- │
│  cli&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+prof │
│  ile&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256   │
│                                                                              │
│  code  ▌paste the code; it stays hidden                                      │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter send code  ·  ctrl+y copy URL  ·  esc cancel                           
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P16. Claude Pro/Max: checking the address

The address went to the leader. The input is locked.

80×24:

```text
                                                                                
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
┊ ◌ Checking the address with anthropic… 1s · esc to cancel                     
╭─ Claude Pro/Max account ─────────────────────────────────────────────────────╮
│  Open this URL and approve. The box closes by itself when you approve.       │
│  If the browser runs on another machine, paste the address it opened.        │
│                                                                              │
│  https://auth.example.test/oauth/authorize?response_type=code&client_id=ask- │
│  cli&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&scope=openid+prof │
│  ile&state=Zk3pQ9xT&code_challenge=Vt8f0aLm2sQe&code_challenge_method=S256   │
│                                                                              │
│  address  (hidden)                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
   esc cancel                                                                   
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P17. Device-code sign-in: waiting

xai shows a short page address and a code. The TUI waits while the leader polls.

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
                                                                                
```

`-- live region starts --`

```text
┊ ◌ Waiting for approval… 24s · esc to cancel                                   
╭─ xAI Grok account ───────────────────────────────────────────────────────────╮
│  1. Open this page on any device:                                            │
│     https://auth.example.test/device                                         │
│                                                                              │
│  2. Enter this code:                                                         │
│     WDJB-MJHT                                                                │
│                                                                              │
│  The code expires in 8:12.                                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
   ctrl+y copy code  ·  esc cancel                                              
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P18. Device-code sign-in: code expired

The code ran out of time. enter asks the leader for a new code.

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
╭─ xAI Grok account ───────────────────────────────────────────────────────────╮
│  ✗ The code expired. No sign-in happened.                                    │
│                                                                              │
│  Get a new code to try again.                                                │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter get a new code  ·  esc close                                           
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P19. Remove: confirmation

delete on a row with a saved credential, or Remove in the row menu, asks before it deletes.

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
╭─ Remove ChatGPT Plus/Pro subscription? ──────────────────────────────────────╮
│  This deletes the saved sign-in for openai from this leader.                 │
│  Environment variables are not changed.                                      │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter remove  ·  esc keep it                                                 
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P20. Remove: done, environment key still applies

The saved credential is gone, but an environment variable still works, so the note says so.

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
                                                                                
  ✓ Removed the saved credential for anthropic.                                 
    ANTHROPIC_API_KEY is still set, so anthropic stays usable.                  
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P21. Remove: nothing to remove

delete on a row that only uses an environment variable changes nothing and says how to stop using it.

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
╭─ Providers ──────────────────────────────────────────────────────────────────╮
│   ● Alibaba Token Plan API key                                       api key │
│   ● Claude Pro/Max account                                                   │
│ ❯ ● Anthropic API key                                  env ANTHROPIC_API_KEY │
│   ● ChatGPT Plus/Pro subscription                                  signed in │
│   ● OpenAI API key                                                           │
│   ● xAI Grok account                                                         │
│   ● xAI API key                                                              │
│                                                                              │
│  ✗ Nothing to remove: anthropic uses ANTHROPIC_API_KEY from the environment. │
│    Unset the variable in your shell to stop using it.                        │
│  ⌕ ▌Type to search                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter sign in  ·  esc close                                                  
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P22. Remove the provider of the current model

The current model is no longer usable. The footer shows no model and a persistent notice names the next step.

80×24:

```text
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  openai/gpt-5.5                                                         
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.004                                      
                                                                                
  ✓ Removed the saved credential for openai.                                    
    The current model gpt-5.5 is no longer available. Choose one with /model.   
                                                                                
```

`-- live region starts --`

```text
┊ No model. Choose one with /model                                              
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

#### P23. Prompt after the current model was removed

Enter sends nothing. The draft stays and the live rail points to /model.

80×24:

```text
                                                                                
                                                                                
                                                                                
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  openai/gpt-5.5                                                         
                                                                                
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  ◆ The test waits for the stale socket check, but CI runs it with a 1 s        
    deadline. The leader removes the stale socket after the lock probe, and the 
    probe can take 1.2 s.                                                       
                                                                                
    go                                                                          
    deadline := 5 * time.Second                                                 
    gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.004                                      
                                                                                
  ✓ Removed the saved credential for openai.                                    
    The current model gpt-5.5 is no longer available. Choose one with /model.   
                                                                                
```

`-- live region starts --`

```text
┊ ✗ Not sent: no model · draft kept · Choose one with /model                    
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

#### P24. /providers needs the leader

The agent runs in this process, so sign-in is not available here. The message is the same as for /sessions.

80×24:

```text
◢◤ ask   v0.1.0 · in-process agent · ~/src/askcore                              
                                                                                
```

`-- live region starts --`

```text
┊ Needs the leader; this session runs in process.                               
┊ Sign in from a shell: ask auth login --provider <id> --method <id>            
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌                                                                          │
╰──────────────────────────────────────────────────────────────────────────────╯
  ○ in-process   ⌂ ~/src/askcore                                       no model 
```

(16 empty rows below)

#### P28. Session start: the model line

One line under the header names the provider and the model of the session. At 40 columns the label is dropped so the longest pair fits.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(17 empty rows below)

40×14:

```text
◢◤ ask  v0.1.0 · 7c1e                   
  alibaba-token-plan/deepseek-v4.1-flash
                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e         deepseek-v4.1-flash 0% 
```

(7 empty rows below)

#### P28a. Session start: a model on two wire formats

The same model id exists on two wire formats under one provider, so the line keeps the format in parentheses, as the /model list does. When the row is too wide (at 40 columns), the format moves to its own row.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash (anthropic-messages)            
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(17 empty rows below)

40×14:

```text
◢◤ ask  v0.1.0 · 7c1e                   
  alibaba-token-plan/deepseek-v4.1-flash
  (anthropic-messages)                  
                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e         deepseek-v4.1-flash 0% 
```

(6 empty rows below)

#### P28b. Session start with TEA_TRACE set

The warning shows once at start, in scrollback under the start line.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
  TEA_TRACE is on: typed and pasted text, keys included, is written to          
  /tmp/ask-trace.log.                                                           
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(15 empty rows below)

#### P28c. Session start: the saved default is not usable

The start line names the model in use; the row under it says why the saved default was not used.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
         Default model openai/gpt-5.5 is not usable (not signed in).            
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                              │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore       deepseek-v4.1-flash  ▱▱▱▱▱▱▱▱ 0%  $0.00 
```

(16 empty rows below)

40×14:

```text
◢◤ ask  v0.1.0 · 7c1e                   
  alibaba-token-plan/deepseek-v4.1-flash
  Default model openai/gpt-5.5 is not   
  usable (not signed in).               
                                        
```

`-- live region starts --`

```text
╭──────────────────────────────────────╮
│ ❯ ▌Ask anything                      │
╰──────────────────────────────────────╯
  ● 7c1e         deepseek-v4.1-flash 0% 
```

(5 empty rows below)

#### P29. Model changed: a new line in scrollback

You chose openai/gpt-5.5 in /model. A new line shows the new pair and, when it fits, the old one. The rail says it is now the default. The footer shows the bare model id.

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
                                                                                
  model  openai/gpt-5.5 · was alibaba-token-plan/deepseek-v4.1-flash            
                                                                                
```

`-- live region starts --`

```text
┊ ✓ Default model: openai/gpt-5.5                                               
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ also check the lock timeout▌                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 7c1e   ⌂ ~/src/askcore                  gpt-5.5  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P30. Attach as observer: divider, then the model line

After the session divider, the model line names the model of the session that you watch. The followed history comes below.

80×24:

```text
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
  ── session 8d07 ──                                                            
                                                                                
  model  alibaba-token-plan/deepseek-v4.1-flash                                 
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ The probe lives in internal/leader/client.go.                               
                                                                                
    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004                          
                                                                                
```

`-- live region starts --`

```text
╭──────────────────────────────────────────────────────────────────────────────╮
│ ◉ Watching. Type /take to take control when the run ends.                    │
╰──────────────────────────────────────────────────────────────────────────────╯
  ◉ observer 8d07   ⌂ ~/src/askcore    deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P31. Session restored after a reconnect

The reconnect note is followed by the model line of the restored session.

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

#### P25. /model after sign-in: models grouped by provider

Only models of usable providers are listed. The new provider is a group of its own. Here you pressed down twice, so gpt-5.5 is selected and deepseek is still the current model.

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
                                                                                
```

`-- live region starts --`

```text
┊ ✓ openai signed in · 2 models                                                 
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● alibaba-token-plan                                                       │
│     deepseek-v4.1-flash (anthropic-messages)                  1M  current    │
│     deepseek-v4.1-flash (openai-completions)                  1M  reasoning  │
│   ● openai                                                                   │
│ ❯   gpt-5.5                                                   1M  reasoning  │
│     gpt-5.6-sol                                               1M  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P25a. /model: the saved default differs from the current model

Another client saved openai/gpt-5.5 as the default. This session still uses deepseek. The row tag says default.

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
│  ⌕ ▌type to filter                                                           │
│   ● alibaba-token-plan                                                       │
│ ❯   deepseek-v4.1-flash (anthropic-messages)                  1M  current    │
│     deepseek-v4.1-flash (openai-completions)                  1M  reasoning  │
│   ● openai                                                                   │
│     gpt-5.5                                                   1M  default    │
│     gpt-5.6-sol                                               1M  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
  ● leader 7c1e   ⌂ ~/src/askcore      deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

#### P26. /model with no usable provider

The box shows one hint and no filter row.

80×24:

```text
◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore                                   
                                                                                
```

`-- live region starts --`

```text
╭─ Select model ───────────────────────────────────────────────────────────────╮
│                                                                              │
│  No usable models yet.                                                       │
│  Use /providers to add providers.                                            │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯
   esc close  ·  your draft is kept                                             
  ● leader 7c1e   ⌂ ~/src/askcore                                      no model 
```

(14 empty rows below)

#### P27. Another client: providers changed

The leader announced the change. This client shows a status line and updates the open list in place.

80×24:

```text
                                                                                
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
┊ Providers changed in the leader: xai signed in · 1 model                      
╭─ Providers ──────────────────────────────────────────────────────────────────╮
│   ● Alibaba Token Plan API key                                       api key │
│   ● Claude Pro/Max account                                                   │
│   ● Anthropic API key                                  env ANTHROPIC_API_KEY │
│   ● ChatGPT Plus/Pro subscription                                  signed in │
│   ● OpenAI API key                                                           │
│ ❯ ● xAI Grok account                                               signed in │
│   ● xAI API key                                                              │
│  ⌕ ▌Type to search                                                           │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter manage  ·  delete remove  ·  esc close  ·  your draft is kept          
  ◉ observer 3f2a   ⌂ ~/src/askcore    deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 
```

## 5. States and transitions

### First run and the first prompt

| State | Trigger | Next state |
|---|---|---|
| P1 First run | `ask` starts and no provider is usable (no saved sign-in, no environment key) | The footer shows `no model` and the notice `Add a provider with /providers` stays on the live rail until a model exists. P3 on `/providers`. P2 on a prompt |
| P2 First prompt, no model | enter with text while there is no current model | Nothing is sent. The draft stays. The rail shows `✗ Not sent: no model · draft kept · Add a provider with /providers` (or `Choose one with /model` when a usable provider exists). The refusal row stays until the next key press |
| P2a Command list | `/` or `/p` at the start of an empty draft | Closes as in the local TUI design. enter on `/providers` runs it. `/PROVIDERS` and `/Providers` run it too |

The rail keeps one notice at a time. Priority, highest first: send refusal, wait line of a pending check, result line (3 s), no-model notice.
The no-model notice returns when a higher one ends and there is still no model.

### The provider list and the row menu

| State | Trigger | Next state |
|---|---|---|
| P3 Provider list | `/providers` when connected to a leader | The list loads from the leader. While the list is loading, the box shows one row `Loading providers…`. A load failure shows `✗ Could not load providers: <reason>` in the box, and esc closes it. The search row `Type to search` is always at the bottom of the box |
| P3 Search | A letter, digit or paste in the list | The list is filtered by name and provider id. The selection moves to the first match. When nothing matches, one row shows `No match` |
| P4 Window | The list has more rows than the window | The window follows the selected row. A row `… N more` shows how many rows are below it. There is no scrollbar (section 9) |
| P5 Row menu | enter on a row that has a saved credential | `Sign in again` starts the flow of the row. `Remove` goes to P19. esc goes back to P3 |
| (flow starts) | enter on a row with no saved credential (no suffix or `env`) | The box of the row opens at once: key box (P6), browser box (P10), code box (P15) or device-code box (P17) |
| P24 Needs the leader | `/providers` in the in-process agent | Two rail rows; no box opens. See below |

P24 detail: the rail shows `Needs the leader; this session runs in process.` (the same text as the in-process agent message of the local TUI design) and a second row `Sign in from a shell: ask auth login --provider <id> --method <id>`.
In this mode `/providers` never opens a box. The first-run notice keeps the decided text `Add a provider with /providers`; the two rows above tell the user what to do after that command.

### Sign in

| State | Trigger | Next state |
|---|---|---|
| P6 Key entry | A key row is chosen | Typing or paste fills the hidden buffer. enter with a non-empty buffer goes to P7. esc closes the box and clears the buffer |
| P7 Key checking | enter in P6, for a provider that the leader can check (not Alibaba Token Plan, see P9a) | The key is sent once. The input is locked. The rail shows `◌ Checking the key with <provider>…`. P9 or P14 on success. P8 on refusal. esc requests a cancel (P13) |
| P8 Key refused | The leader refuses the key | The reason shows in the box. The input is empty. Nothing is saved. A new paste and enter go to P7 again |
| P9 Key saved | The leader saves the key | The box closes. A `✓` note goes to scrollback. A rail line shows the result for 3 s. The draft is back. P25 data is refreshed |
| P9a Saved without a check | enter in the Alibaba Token Plan key box (P6b) | No `Checking…` step: P7 does not apply to this row. The leader saves the key at once. The box closes, scrollback shows `✓ alibaba-token-plan: Saved. This provider cannot be checked before use.` and the next step (`Choose a model with /model.` when there is no model), and the rail shows `✓ alibaba-token-plan key saved · 1 model` for 3 s. The provider row then reads `api key`, as for any saved key |
| P9b Bad unchecked key | The first prompt with that provider's model fails with an auth error | The existing auth error block of the earlier design: `✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).` and `Replace the key with /providers. Then send again.` The key stays saved until the user replaces or removes it |
| P10 Browser waiting | The ChatGPT row is chosen and the leader returns a URL | The rail shows `◌ Waiting for the browser…` with the elapsed seconds. P14 on success. P12 on failure. P13 on esc |
| P12 Sign-in failed | The leader reports a failure (for example `access_denied`) | The box shows the reason. enter starts a new sign-in (a new login id). esc closes |
| P12a Cannot start | The leader refuses to start (`provider_busy` or another reason) | The box shows the reason. enter tries again. esc closes |
| P12c Removed by another client | Another client removes the provider during this sign-in | The leader cancels the sign-in. The box shows `✗ Sign-in cancelled: another client removed openai.` and `Nothing was saved. You can sign in again.` enter starts a new sign-in. esc closes and adds `⊘ Sign-in to openai cancelled: another client removed openai.` to scrollback |
| P12b Leader lost | The connection drops during a sign-in | The box shows that the sign-in stopped. The login is not resumed after the reconnect. Nothing is saved. esc closes. The connection states of the local TUI design (disconnected, reconnected) apply to the rest of the screen |
| P13 Cancelled | esc or ctrl+c in any login box | The leader gets a cancel for the login id. The box closes. A `⊘` note goes to scrollback. A late result for that id is ignored |
| P14 Signed in | The leader reports success | As P9. The note names the account when the leader returns one |
| P15 Claude Pro/Max browser flow | The Claude Pro/Max row; the leader opened the callback port | The box shows the URL and a masked `address` input. The rail shows `◌ Waiting for the browser or an address…`. If the browser callback finishes, the result is P14. A pasted address and enter go to P16 |
| P15a Claude Pro/Max code flow | The Claude Pro/Max row; the callback port is busy | The box says `The callback port is busy, so this sign-in uses a code instead.` and shows the URL and a masked `code` input. The rail shows `◌ Waiting for a code…`. A code and enter go to P16 |
| P16 Address or code checking | enter in P15 or P15a | The address or the code is sent once. The input is locked. The rail shows `Checking the address with anthropic…` or `Checking the code with anthropic…`. P14, or P12 on refusal |
| P17 Device code waiting | The xAI Grok account row | The box shows the page address and the user code. The leader polls. P14 on approval. P18 on expiry |
| P18 Code expired | The leader reports expiry | enter asks for a new code (a new login id). esc closes |

After a success the TUI asks the leader for the model list again (the new list gets a new generation).
The status line `<provider> signed in · N models` shows when that list arrives, and N comes from it.
If the list cannot be loaded, the line reads `<provider> signed in · model list not refreshed`.
The TUI does not choose a model. On first run the footer keeps `no model`, and the note ends with `Choose a model with /model.`

### Removal

| State | Trigger | Next state |
|---|---|---|
| P19 Confirm | delete on a row with a saved credential, or `Remove` in the row menu (P5) | enter removes. esc keeps. When a run is active, a third row warns `A run is active and may use this provider.` |
| P20 Removed, env applies | The leader reports removed, and an environment variable still works | The box closes. The note has two rows: the removal and `<NAME> is still set, so <provider> stays usable.` |
| (removed, no env) | The leader reports removed and nothing else works | The box closes. One note row: `Removed the saved credential for <provider>.` The row shows no suffix the next time the list opens |
| P21 Nothing to remove | delete on an env-only row, or on a row with no credential | The list stays open. An error row shows in the box. Nothing is sent to the leader |
| P22 Current model removed | The leader reports that the current model became unusable | The footer shows `no model`. The note has a second row that names the model. The rail shows `No model. Choose one with /model` until a model is chosen |
| P23 Prompt after P22 | enter with text | As P2, with the text `Choose one with /model` |

### `/model`

| State | Trigger | Next state |
|---|---|---|
| P25 Model list | `/model` or ctrl+l when idle | Lists only models of usable providers, one group for each provider, current model selected. The filter matches provider and model id. A group header shows only when one of its rows matches. enter sets the model and saves it as the default; the rail shows `✓ Default model: <provider>/<model>` for 3 s (P29). A model whose provider is not ready shows the error row, as in the local TUI design, and nothing is saved |
| P25a Saved default differs | The saved default is a usable model that is not the current model (another client or an earlier session chose it) | Its row has the tag `default` in the tag column, in `muted`. The current model keeps the tag `current`. When the two are the same, only `current` shows |
| P26 Empty model list | `/model` with no usable provider | The box shows `Use /providers to add providers.` and no filter row. esc closes |

### Model line

| State | Trigger | Next state |
|---|---|---|
| P28 Start line | A session starts (leader start, in-process start, `/new`) and it has a model | The line `  model  <pair>` is added under the header. With no model nothing is added (P1) |
| P28a Two wire formats | The current model exists on two wire formats under one provider | The line adds ` (<api>)`; it moves to its own row when the row is too wide |
| P29 Change line | The leader reports a new current model (selector, fallback, cycle) | A row `  model  <new pair> · was <old pair>` is added. The footer shows the new bare model id. The `· was` part drops when the row is too wide, and it is absent when there was no model before. After a choice in `/model` the rail also shows `✓ Default model: <pair>` for 3 s |
| P28b TEA_TRACE at start | The session starts and `TEA_TRACE` is set to a file name | The warning rows `TEA_TRACE is on: typed and pasted text, keys included, is written to <file>.` go to scrollback once, under the start line (or under the header when there is no model) |
| P28c Saved default not usable | A session starts and the provider of the saved default is not usable | The session uses the next choice (section 3). The start line names it; the row under it reads `Default model <pair> is not usable (<reason>).` in `muted` |
| P30 Observer attach | enter on a session in `/sessions` | Divider, then the start line for that session, then the followed history |
| P31 Restored session | Reconnected to the same leader and the session is live | The reconnect note, then the start line. If the model changed while the client was away, the line shows the current model without `· was` |
| No model | First run, or the current model became unusable (P22) | No line. When a model is chosen later, a change line without `· was` is added |
| Stale event | A model event with an old session generation | Ignored |

### Other clients and stale results

| State | Trigger | Next state |
|---|---|---|
| P27 Providers changed | The leader announces a new providers generation | Every client asks for the provider list and the model list again. The rail shows `Providers changed in the leader: <provider> signed in · N models` for 3 s when the change is a sign-in; for a removal it shows `Providers changed in the leader: <provider> removed`. An open provider box or model box updates in place: the selection stays on the same provider or model id; if that row is gone, the selection moves to the nearest row; the typed filter stays. No note goes to the scrollback of a client that did not start the change |
| Observer client | The client is an observer of a session | `/providers` works (it is leader state, not session state). `/model` waits, as in the local TUI design |
| Stale result | A list, model list, sign-in result or check result carries an old login id, request id or providers generation | Ignored. Nothing changes on screen |
| Connection lost | The leader connection drops while `/providers` is open | The box stays with the last list. Actions show `Reconnecting, please wait…` on the rail and send nothing. After the reconnect, the list loads again |

A signal (SIGINT from outside, SIGTERM, SIGHUP) detaches as in the local TUI design. If a login is pending, the TUI sends a cancel first on a best-effort basis; the leader also cancels the login when the connection closes.

## 6. Keys and focus

Input order follows the architecture doc: host exit and cancel handling, then the active dialog or selector, then completion, then the editor, then application shortcuts.
While a provider box or a login box is open, it is the active dialog. The editor and the application shortcuts do not see any key.

### Host

| Key | Receiver | Action |
|---|---|---|
| ctrl+c | Host | With a login box open: cancel the login and close the box (P13). It never quits and it does not count as the first press of the quit confirmation. With the provider box or the row menu: close it, as for the model selector. With a key or code buffer: the buffer is cleared and not kept for undo |
| ctrl+d | Host | Ignored while a provider box or a login box is open. The user must close the box first, because a quit would drop a pending login without a note |
| esc | Active box | See the tables below. Always closes or steps back one level; with a pending login it cancels the login |

### Provider box and row menu

| Key | Receiver | Action |
|---|---|---|
| up, down, pgup, pgdn | Provider box | Move the selection. In the model box, group headers are skipped |
| enter | Provider box | Start the flow of the selected row. On a row with a saved credential: open the row menu (P5) |
| delete | Provider box | Start a removal (P19) on a row with a saved credential. Otherwise show P21 |
| letters, digits, backspace | Provider box | Edit the search text. The search row is always shown, so `x` and other letters are search text |
| paste | Provider box | Inserted into the search text with line breaks removed |
| esc | Provider box | Close and restore the editor and the draft |
| up, down, enter, esc | Row menu | Move, choose, step back to the provider box |

### Key box, code input and address input

| Key | Receiver | Action |
|---|---|---|
| (all rows) | Secret input | The key input, the code input and the callback address input behave the same. The callback address is masked, because it carries a one-time code |
| characters | Secret input | Appended to a hidden buffer. The screen shows only `(hidden)` |
| paste (bracketed or not) | Secret input | One insertion. A trailing CR, LF or space is removed. A paste with a line break or a space inside is refused with the row `The text has a line break or space inside. Paste it again.` and the buffer is cleared. A paste above the leader's private input limit is refused with `That is too long to be a key.` The editor never receives the paste |
| backspace | Secret input | Removes the last character |
| ctrl+u | Secret input | Clears the buffer |
| enter | Secret input | Send, when the buffer is not empty. A paste that ends in a line break does not send by itself; enter sends |
| left, right, up, down, home, end | Secret input | Ignored. The input has no cursor movement and does not walk prompt history |
| esc, ctrl+c | Active box | Cancel, clear the buffer, close |

### Login boxes (browser, copy-code, device code)

| Key | Receiver | Action |
|---|---|---|
| ctrl+y | Login box | (In the editor ctrl+y is yank; a login box owns its input, so there is no conflict.) Copy the URL (browser, copy-code) or the user code (device code) to the clipboard with the host clipboard call. Shows `Copied` on the rail for 1 s. It never copies a secret that the user typed |
| enter | Login box | Copy-code box: send the code. Failure or expired box: start again. Waiting box: ignored |
| esc, ctrl+c | Login box | Cancel the login (P13) |
| everything else | Login box | Ignored. In the checking states (P7, P16) only esc and ctrl+c work |

### Removal box

| Key | Receiver | Action |
|---|---|---|
| enter | Removal box | Remove |
| esc, ctrl+c | Removal box | Keep and close |

The mouse is not used. Terminal selection of the URL is a fallback only; see section 8.

## 7. Limited space and resize

The live region gives rows in this order. The last item loses rows first.

1. The active box: title row, bottom row, and at least one content row.
2. The hint row of the box.
3. The rail row of a pending check or wait.
4. `footer`: one row. Under 8 rows in total it is hidden (local TUI rule).
5. Blank rows and intro lines inside a login box.
6. The search row of the provider box.
7. URL rows beyond the cap.
8. Transcript tail.

Rules:

- The provider box shows a window of `clamp(height − 11, 3, 8)` rows: 7 (all rows) at 24 rows, 3 at 14 rows (P4). When the list has more rows than the window, a row `… N more` says how many rows are below. The search row stays at the bottom of the box at every size.
- The hint row drops its parts from the right: `your draft is kept` first, then `delete remove` or `ctrl+y copy …`. The parts `enter …` and `esc …` stay.
- If the terminal is so low that the hint row does not fit (under 6 rows), the title carries the cancel key: `Sign in to openai · esc cancel`.
- At 40 columns the name of a row is truncated with `…` so that the status suffix stays whole (P4). The suffix is cut only when it alone is wider than the row.
- A login box shows at most `height − 8` URL rows. If the URL needs more, the last shown row is followed by a row `… ctrl+y copies the full URL` (P11). The URL is never cut mid-row without that row.
- At 100 columns or more the layout does not change. Rows stay at the left and the suffix at the right.

Resize follows the local TUI design: after the size stops changing for 120 ms, the renderer redraws.
A pending login is not affected by a resize. The wait continues. The URL is wrapped again for the new width. The hidden buffer, the filter, the selection and the draft stay. The login id does not change, so no new request goes to the leader.
Scrollback notes are blocks of the client transcript and are drawn again at the new width.

## 8. Text and width

- Width is measured in terminal columns by grapheme; CJK and most emoji use 2 columns and are never split.
- The truncation marker is `…`.
- Provider ids and environment variable names are ASCII. In the provider box the row name is truncated with `…` when the row is too narrow (P4). The status suffix is never truncated before the name is.
- Account names and error reasons wrap in scrollback notes with a four-column indent. They are never truncated there. In the box they wrap on the next row with the same indent.
- Secrets are never drawn. The key and the code show only as `(hidden)`. The text does not show the length.

### URLs

A URL must be easy to use even when it needs several rows. The design uses three ways together:

1. **Full text, wrapped.** The URL is cut by columns, not by words, at `width − 5`, so each row fits inside the box. It is never shortened with `…` when it fits the rows that are available.
2. **ctrl+y copies the exact URL.** The host copies the whole URL with the clipboard call of the terminal library, so the lines and the box borders are not copied. This is the main way to copy.
3. **The URL is a terminal hyperlink.** Each row of the URL is marked as part of one link (OSC 8) with the full URL as the target. Terminals that support links open the full URL on click, even when the text shows on several rows. Other terminals show plain text.

A mouse selection across the rows of the box includes the `│` borders and the line breaks. The design does not try to fix this, because an inline renderer writes exact-width rows. Section 11 lists the alternative.

The device-code page address is short and is shown on one row. The user code is shown in bold on its own row.

### Model line

- The start line and the change line are never cut. From 60 columns the line is `  model  <pair>`. Under 60 columns the label `model` is dropped and the line is `  <pair>`: the longest catalog pair, `alibaba-token-plan/deepseek-v4.1-flash`, is 38 columns, so it fits a 40-column terminal exactly (P28). A pair wider than the terminal wraps after the slash, with the same two-column indent on the second row.
- The `· was <old pair>` part of a change line shows only when the whole row fits, and only from 60 columns. Otherwise it is dropped (the footer and the earlier lines still show the old model).
- The `(<api>)` part shows after the pair when the row fits. When it does not fit, it moves to its own row, aligned with the pair (P28a).

### Other text

- Tabs and control characters in text from the leader show as `?`, as in the local TUI design.
- The account name comes from the leader. It is shown as given, wrapped when long.
- The numbers use `k` and `M` as in the local TUI design. `1,050,000` shows as `1M` in the context column.
- A model id that is listed on two wire formats shows its format in parentheses, as the model selector does now (`deepseek-v4.1-flash (anthropic-messages)`), so two rows never look the same.

## 9. Style intent

Roles, glyphs and patterns come from the [TUI style guide](../../docs/tui-style-guide.md). This design adds no color and no glyph.

| Element | Role |
|---|---|
| Box lines | `border` |
| Box titles, the user code, the spinner and the wait line | `accent` (bold) |
| Provider dot | `prov-1` to `prov-4` by provider position |
| Selected row | `selected` background, bold name |
| Status suffix (`signed in`, `api key`, `env <NAME>`), `✓` notes | `success` |
| Hints, notices, the `Type to search` placeholder, the `(hidden)` input text, the placeholders | `dim` (italic for hints and placeholders) |
| Search icon, intro lines, group headers (bold) | `muted` |
| `✗` lines and the refusal reason | `error` |
| `⊘` cancel note | `muted` |
| `no model` in the footer | `muted` |
| URL text | `fg` |
| `Copied` and 3 s result lines | `dim` rail with a `success` `✓` |

The model line: the label `model` in `muted`, the pair in `fg`, the `· was <old pair>` part in `dim`. No new glyph and no new color. A change is shown in words (`was`), not with an arrow, because the style guide has no arrow glyph.

Every state stays readable without color: the glyphs `✓`, `✗`, `⊘` and `◌`, bold and dim carry the meaning.

### Gaps in the style guide

These are used in this design with existing roles and glyphs. The user must decide if the style guide changes.

| Gap | Used here |
|---|---|
| No success note pattern in scrollback (only retry, error, cancel, detach) | `  ✓ ` row in `success`, with an optional `muted` second row at four spaces |
| No group header row in a selector | A row with the provider dot and the provider id in `muted` bold, rows below it |
| No dialog box pattern with an input row | The selector box shape, no filter row, an input row `key  ▌…` or `code  ▌…` |
| No warning glyph (`⚠` is not in the guide) | The TEA_TRACE warning is text only, in the `warning` role |
| No tag `default` in the model selector | `default` in the tag column, `muted` |
| No glyph for hidden input | The fixed text `(hidden)`. Bullets would need a new glyph (section 11, question 5) |
| No `link` role for URLs | `fg`. A terminal link style would be a new role |
| The provider box has the search row at the bottom, always shown, with the text `Type to search`. The style guide puts the filter row at the top and omp shows it at the bottom. The empty model box (P26) has no filter row | The `⌕` row at the bottom of the provider box only; other selectors keep the top filter row |
| No scrollbar element. omp draws one at the right edge | The existing `… N more` row of the selectors. A scrollbar would need a new glyph |
| No persistent notice rule | The first-run and no-model notices stay until a model exists (section 5) |
| `no model` footer role | `muted`, the same as footer text |
| No pattern for a labelled transcript line (the model line) | A two-column indent, label in `muted`, value in `fg`, like the other transcript notes |
| The label of the model line is dropped under 60 columns | The model line without label at 40 columns (P28) |

## 10. Acceptance criteria

1. At 80×24, after `ask` starts with no usable provider, the screen shows `Add a provider with /providers` on the live rail, `no model` at the right of the footer, and no context bar (P1).
2. At 40×14, in the same state, the footer shows `no model` and the rail shows `Add a provider with /providers` without a second row (P1).
3. At 80×24, after text and enter with no current model, the draft stays in the editor, nothing is added to scrollback, and the live rail shows `✗ Not sent: no model` and `Add a provider with /providers` (P2).
4. At 80×24, after `/p`, the command list shows `/providers  Add or remove providers`; after `/PROVIDERS` and enter, the provider box opens (P2a).
5. At 80×24, after `/providers` and enter, the provider box replaces the editor and shows seven rows: `Alibaba Token Plan API key  api key`, `Claude Pro/Max account`, `Anthropic API key  env ANTHROPIC_API_KEY`, `ChatGPT Plus/Pro subscription  signed in`, `OpenAI API key`, `xAI Grok account` and `xAI API key`. The row `⌕ Type to search` is the last row inside the box, and the hint row ends with `your draft is kept`. After esc the editor shows the same draft (P3).
6. At 80×24, after typing `key` in the provider box, only the rows with `key` in the name remain, the first one is selected, and the typed text shows in the search row (P3).
7. At 40×14, after `/providers` and enter, the screen shows three provider rows, `… 3 more`, the search row, and a long name cut with `…` while its status suffix stays whole (P4).
8. At 80×24, after enter on `ChatGPT Plus/Pro subscription` (signed in), the row menu shows `Sign in again` and `Remove`; after esc the provider box is back. After enter on `OpenAI API key` (no credential) the key box opens at once, with no second step (P5, P6).
9. At 80×24, after a bracketed paste of a key in the key box, the box shows `(hidden)` and the pasted text is not in any row of the screen (P6).
10. At 80×24, after enter with a key, the rail shows `Checking the key with openai…` and the box input shows `(hidden)` and ignores typing (P7).
11. At 80×24, when the leader refuses the key, the box shows `openai refused the key: invalid_api_key (HTTP 401). Nothing was saved.`, the input is empty, and no row contains the key (P8).
12. At 80×24, when the leader saves the key, the box closes, scrollback shows `✓ openai: API key saved. 2 models ready.`, the rail shows `openai signed in · 2 models`, and the editor shows the draft again (P9).
13. At 80×24, in the browser box, the screen shows the whole URL on wrapped rows, the rail shows `Waiting for the browser…`, and the hint shows `ctrl+y copy URL` and `esc cancel` (P10).
14. At 40×14, in the browser box, the screen shows four URL rows, then `… ctrl+y copies the full URL`, and the hint row shows `esc cancel` (P11).
15. At 80×24, after the leader reports `access_denied`, the box shows `Sign-in failed: the browser returned access_denied.` and `enter try again`; after enter a new sign-in starts (P12).
16. At 80×24, when another client is signing in to the same provider, the box shows `Could not start: another client is signing in to openai.` (P12a).
17. At 80×24, when the leader stops during a sign-in, the rail shows `Disconnected from the leader`, the box shows `The sign-in stopped because the leader is gone.`, and after the reconnect the box is not reopened (P12b).
18. At 80×24, after esc in a login box, the box closes, scrollback shows `⊘ Sign-in to openai cancelled. Nothing was saved.`, and the draft is back. A later success event for that login id changes nothing on screen (P13).
19. At 80×24, after ctrl+c in a login box, the same happens as after esc, and a second ctrl+c does not quit the TUI (P13).
20. At 80×24, when the leader returns the account name, scrollback shows `✓ openai: signed in as dev@example.com. 2 models ready.` (P14).
21. At 80×24, after enter on `Claude Pro/Max account`, the box shows the URL and `address  ` with the placeholder `paste the address; it stays hidden`, and the rail shows `Waiting for the browser or an address…`; a bracketed paste shows `(hidden)`, enter shows `Checking the address with anthropic…`, and the address is on no row (P15, P16).
22. At 80×24, when the callback port is busy, the box shows `The callback port is busy, so this sign-in uses a code instead.` and a `code` input; a pasted code shows `(hidden)` and is on no row (P15a).
23. At 80×24, when another client removes openai during this client's ChatGPT sign-in, the box shows `✗ Sign-in cancelled: another client removed openai.`; after esc scrollback shows `⊘ Sign-in to openai cancelled: another client removed openai.` (P12c).
24. At 80×24, in the device-code box, the screen shows the page address, `WDJB-MJHT` and `The code expires in`; after expiry the box shows `The code expired. No sign-in happened.` and `enter get a new code` (P17, P18).
25. At 80×24, after delete on a row with a saved credential, the box `Remove openai?` shows; esc closes it with no change; enter removes and scrollback shows `✓ Removed the saved credential for openai.` (P19).
26. At 80×24, after a removal while `ANTHROPIC_API_KEY` is set, scrollback shows `ANTHROPIC_API_KEY is still set, so anthropic stays usable.` (P20).
27. At 80×24, after delete on the env-only row, the list stays open and the box shows `Nothing to remove: anthropic uses ANTHROPIC_API_KEY from the environment.` (P21).
28. At 80×24, after the removal of the provider of the current model, the footer shows `no model`, scrollback shows `The current model gpt-5.5 is no longer available. Choose one with /model.`, and the rail shows `No model. Choose one with /model`; after text and enter the rail shows `Not sent: no model` and `Choose one with /model` (P22, P23).
29. At 80×24, in the in-process agent, after `/providers` and enter, no box opens, and the rail shows `Needs the leader; this session runs in process.` and the `ask auth login` line (P24).
30. At 80×24, after a sign-in, `/model` shows one group header for each usable provider, the models of the new provider below its header, and no model of a provider that is not signed in (P25).
31. At 80×24, with no usable provider, `/model` shows `Use /providers to add providers.` and no filter row (P26).
32. At 80×24, in a second `ask` client with the provider box open, after a sign-in in the first client, the rail shows `Providers changed in the leader: xai signed in · 1 model`, the `xai` row reads `signed in`, and the selection is still on `xai` (P27).
33. At 80×24, a result for an old login id, an old request id or an old providers generation changes no row (stale).
34. At 80×24, in the key box, up does not show an earlier prompt, and after esc the editor still shows the draft from before `/providers`.
35. During a resize from 120×40 to 80×24 with a login box open, the URL is wrapped again, the rail wait stays, the hidden buffer stays, and no new request goes to the leader.

### Security acceptance

These checks use a fake key such as `sk-test-0000000000`, a fake code and a fake callback address. A terminal test searches for the fake value in every place listed.
Exception (user decision, 2026-10-10): the checks hold when `TEA_TRACE` is not set. When it is set, the terminal library writes the raw input, secrets included, to the trace file; the TUI does not prevent it and shows the warning instead (P6a, P28b).

36. After a paste and enter in the key box, the fake key is in no row of the screen grid at any time, including the checking, refused and saved states.
37. After a sign-in with a fake key, the fake key is not in the scrollback, not in the client transcript blocks, and not in any note.
38. After a sign-in, up in the empty editor does not recall the key, and the prompt history file or in-memory history does not hold it. The key box never writes to the editor history.
39. After a sign-in and ctrl+- (undo) in the editor, the draft is the same as before `/providers`, and the undo ring and the kill ring do not hold the key. The key box uses its own buffer; the editor never receives the paste.
40. After a cancel, a refusal, a save and a closed box, the secret buffer is cleared. A second open of the key box starts empty.
41. After a sign-in, the TUI log (if any) and the event stream seen by the client contain no key and no code. The leader receives each secret once.
42. The key is not in the window title, the terminal bell text, or any clipboard call. ctrl+y copies only a URL or a device code.
43. A paste that has a line break inside, or is above the input limit, is refused with a message that does not repeat the paste.

44. At 80×24, after `ask` starts with a usable model, the screen shows the startup header, and on the next row `  model  alibaba-token-plan/deepseek-v4.1-flash`, then one empty row and the editor; the footer shows `deepseek-v4.1-flash` without the provider (P28).
45. At 120×40, in the same state, the screen shows the same start line at the same place; the layout of the line does not change (P28).
46. At 40×14, in the same state, the screen shows `  alibaba-token-plan/deepseek-v4.1-flash` on one row under the header, with no label and no cut (P28).
47. At 80×24, when the model exists on two wire formats, the start line ends with `(anthropic-messages)`; at 40×14 the `(anthropic-messages)` text is on its own row under the pair (P28a).
48. At 80×24, after `/model` and enter on `gpt-5.5` of `openai`, the transcript shows `  model  openai/gpt-5.5 · was alibaba-token-plan/deepseek-v4.1-flash` as a new row and the footer shows `gpt-5.5` (P29).
49. At 80×24, after `/sessions` and enter on a session, the transcript shows `── session 8d07 ──`, then `  model  alibaba-token-plan/deepseek-v4.1-flash`, then the followed history (P30).
50. At 80×24, after a reconnect to the same leader, the screen shows `↻ Reconnected. Session 3f2a restored.` and on the next row the model line (P31).
51. At 80×24, with no model (first run, or after the removal in P22), the screen shows no `model` line, the footer shows `no model`, and the notice stays (P1, P22).
52. At 80×24, a model event with an old session generation adds no row.
53. At 80×24, after `/model` and enter on `gpt-5.5`, the rail shows `✓ Default model: openai/gpt-5.5`; after `/new` the start line shows `  model  openai/gpt-5.5` (P29, P28).
54. At 80×24, when the saved default is `openai/gpt-5.5` and openai is not signed in, a new session shows `  model  alibaba-token-plan/deepseek-v4.1-flash` and under it `Default model openai/gpt-5.5 is not usable (not signed in).`; at 40×14 the reason wraps under the pair with a two-column indent (P28c).
55. At 80×24, after a sign-in with `/providers`, the saved default and the current model do not change.
56. At 80×24, when the saved default is a usable model that is not current, `/model` shows `default` on its row and `current` on the current row (P25a).
57. At 80×24, with `TEA_TRACE=/tmp/ask-trace.log`, the start shows `TEA_TRACE is on: typed and pasted text, keys included, is written to` and `/tmp/ask-trace.log.` under the start line, and the key box shows the same text above the `key` input (P28b, P6a).
58. At 80×24, without `TEA_TRACE`, no row contains `TEA_TRACE`.
59. At 80×24, in the Alibaba Token Plan key box, the screen shows `This provider cannot be checked before use; the key is saved as it is.`; after a paste and enter there is no `Checking` row, scrollback shows `✓ alibaba-token-plan: Saved. This provider cannot be checked before use.` and `Choose a model with /model.`, and `/providers` then shows the row `Alibaba Token Plan API key` with `api key` (P6b, P9a).
60. At 80×24, after a wrong Alibaba Token Plan key was saved, the first prompt ends with `✗ Error (auth): alibaba-token-plan refused the key (HTTP 401).` and `Replace the key with /providers. Then send again.` in scrollback (P9b).

## 11. Decisions and open questions

None of these changes a decision of 2026-10-10 listed at the top. All questions are decided (user, 2026-10-10). No open question remains; the design waits for approval of the changes listed in "Changes since the first approval". The Alibaba Token Plan key is decided (user, 2026-10-10, option A): the leader saves it without a live check, the key box says so, and a bad key shows the auth error at the first prompt.
The Claude Pro/Max flow is decided (user, 2026-10-10): browser flow first, a masked pasted callback address in the same box, and the copy-code flow when the callback port is busy. The default model is decided (user, 2026-10-10, as omp): `/model` saves the default; the fallback order is in section 3; `/providers` does not set it. TEA_TRACE is decided (user, 2026-10-10): it stays usable, with a warning and a stated exception to the security criteria. Removal during another client's sign-in is decided (user, 2026-10-10): the removal wins and the other box says why.
The provider and model line is decided (user, 2026-10-10): option A only, a start line under the header and a change line in scrollback. The user dropped option D: the reply meta line keeps the earlier form with the bare model id, and the footer keeps the bare model id. It is recorded in section 3 and in the amendments in section 12. No question remains about it.
The question about a method step is decided (user, 2026-10-10): one flat list of login options, no method step. The question about placement is decided (user, 2026-10-10): `/providers` and the login boxes replace the editor slot, as `/model` and `/sessions` do, and keep the draft. The question about removal is decided (user, 2026-10-10): removing a provider asks for confirmation first (P19). The question about the first-run notice is decided (user, 2026-10-10): it lives on the live rail and in the footer only, not in scrollback. The question about copying a long URL is decided (user, 2026-10-10): wrapped rows in the box, ctrl+y to the clipboard and a terminal hyperlink; the URL is not committed to scrollback. The question about hidden input is decided (user, 2026-10-10): a masked input shows the fixed text `(hidden)`, with no new glyph. 

## 12. Amendments to the earlier design

This design changes the earlier [local TUI design](../261009-2239-t1a-local-tui/design.md). That file and its artifacts are not edited here. If this design is approved, the earlier design and its sketches must be updated in the same change. The list is exact; the numbers are those of the earlier file.

### Sketches

| Earlier sketch | What changes |
|---|---|
| S1 Starting | No header row exists in S1, so no row is added. The footer shows `no model` when no provider is usable (P1); with a usable model it is as before. The start line appears in the first frame after S1, which is S3 |
| S2 In-process agent | Under the header: `  model  <pair>` when the agent has a model, and no row when it has none. The footer shows `no model` in the second case |
| S3 Idle, 80×24 and 120×40 | Under the header: `  model  alibaba-token-plan/deepseek-v4.1-flash`, then one empty row |
| S4, S5, S5a, S5b, S6, S7, S8, S9, S10, S11, S12, S12a, S13, S13a, S13b, S14, S15, S16, S18, S19 | Same header line. These sketches use the shared history block, so each of them gains the row under the header (S6 and S7 also at 120×40) |
| S11, S12 | The model list shows only models of usable providers, grouped by provider (P25). A readiness failure can still show for a usable provider whose token expired |
| S12a | After enter, the transcript shows the divider, then the start line of the watched session (P30) |
| S13a | The start line follows the reconnect note (P31). S13b is unchanged: the session is gone and no line is added |
| S17 | No change (it shows no header) |

No reply meta line changes. The footer does not change except for `no model`.

### Text and tables

| Earlier place | Change |
|---|---|
| Section 3, block table, row "Startup header" | Add: the header is followed by the model line `  model  <pair>` when the session has a model |
| Section 5, rows S3, S12a, S13a | The start line is added as described in section 5 of this design ("Model line") |
| Section 6 | No new key. The change line comes from the leader's model event, not from a key |
| Section 7 | Under 60 columns the model line has no label (section 8 of this design) |

### Acceptance criteria

| Earlier criterion | Change |
|---|---|
| 1 | After the startup header, the model line shows when there is a model |
| 2 | The in-process screen shows the model line when the agent has a model |
| 10 | The model change adds the change line to the transcript (P29); the footer still shows the bare model id |
| 11 | Shows only for a usable provider with a failed token; a provider that is not signed in is hidden in `/model` |
| 13 | The restore note is followed by the model line |
| 16 | The attach shows the divider, the model line, then the followed reply |
| 20 | The 120-column sketches of S3, S6, S7 and S11 include the header model line |

The golden files of the earlier design that show a header change in the same way.
