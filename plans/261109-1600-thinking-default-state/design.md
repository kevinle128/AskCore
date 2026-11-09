---
title: "Thinking level with no choice (default) TUI design"
design-status: draft        # draft | approved
design-approved-by: ""      # filled only at approval
design-approved-on: ""      # YYYY-MM-DD, filled only at approval
---

# Thinking level with no choice (default) TUI design

Preview: [artifacts/preview.html](artifacts/preview.html). Rebuild this file and the preview with
`python3 artifacts/make-design.py` from this directory; the sketches come from `artifacts/sketch-gen.py`.

This file is one design and one approval. It covers the TUI (sections 1 to 11, as the `ask-tui-design` skill requires) and, in section 3.1, the parts of the agent, the wire, the hooks and ACP that the TUI reads. The TUI cannot show the correct word unless these parts carry it, so they are in the same approval.

Parts that this design does not change copy the current code and goldens: the footer row (`internal/tui/footer.go`, `TestFooterRowGoldens`), the cycle notice (`cycleRows` in `internal/tui/model_cycle.go`, `TestModelCycleKeyMovesThroughScope`), the rail notices (`noticeRow` in `internal/tui/status.go`) and the busy text (`cycleThinking` in `internal/tui/update_input.go`).

## 0. Problem

A session has three kinds of thinking level today:

| Stored level (`Options.Reasoning`) | What the request sends | What `Agent.State()` reports |
|---|---|---|
| empty: the user chose no level | no level; the adapter default applies (see below) | `off` (`internal/agent/agent.go:573`) |
| `off` | the lowest level of the model (`ClampThinkingLevel`, `internal/providers/clamp.go:22`); `grok-4.7` sends `low`, `gpt-5.5` sends `none` | `off` |
| a level word | that level, clamped to the model | that level |

Two problems follow:

1. **Footer row and ACP.** The footer row shows `◐ off` for a session with no chosen level, but the request sends no level and the model thinks (for example `grok-4.7`). The same word goes to every reader of `State()`: `_ask/session/get_state` (`internal/acp/ask_methods.go:141`), the `setModel` result (`:320`), the `setThinking` result (`:370`) and `ASK_REASONING_LEVEL` for background jobs (`cmd/tui/headless.go:477`).
2. **Hooks.** A `PrepareRequest` hook sees `off` for the empty level (`loop.thinkingLevel`, `internal/agent/loop_run.go:422`). `Registry.PrepareRequest` keeps only the fields that a handler changed (`internal/pipeline/registry.go:286`). A hook that sets `off` on such a session makes no change, so it cannot force thinking off. The assistant message stamp (`internal/agent/loop_stream.go:224`) also records `off`.

"No level" does not always mean the vendor default. The OpenAI Responses and xAI adapters send no effort, so the vendor decides. The Anthropic adapter and the OpenAI Completions adapter use `medium` for an empty level (`internal/providers/anthropic/document.go:112`, `internal/providers/openai/completions_body.go:77`). This design names the state for what Ask knows: the user did not choose, and the provider path picks. It does not claim what the provider picks.

## 1. Outcome

When the user chose no thinking level, the footer row shows `◐ default` and not `◐ off`. `shift+tab` cycles `default → off → minimal → low → medium → high → xhigh → max → default`, so the user can go back to "no choice" after a choice. The cycle notice of `ctrl+p` names `default` in the same place where it names a level today. The state and model results of the leader and of `ask acp`, the `PrepareRequest` hook view, `ASK_REASONING_LEVEL` and the message stamp all use the same word, so a hook that sets `off` forces thinking off, and a hook that sets `default` gives the choice back to the provider.

## 2. Non-goals

- A new key, a thinking level selector or a `/thinking` command. `shift+tab` stays the only thinking level key (decision of the [H7 design](../261105-1645-h7-model-config-resolution/design.md), section 2).
- A change to the shortcut bar text. `shift+tab` is not in the bar today, and this design does not add it.
- A status notice for `shift+tab`. Today only the footer row changes when the level changes; this stays.
- Showing the level that the provider uses for `default` (for example `default (medium)` for Anthropic). Ask does not know it for most providers. See open question 4.
- A new color or glyph. `default` uses the `assistant` role and the `◐` glyph, as every level does.
- A `defaultThinkingLevel` setting, and a change to the start level rules of `alibaba-token-plan` or of `ask -p --provider anthropic` (`cmd/tui/headless.go:449`).
- Keeping a clamped level across model changes (a level that a model clamps stays clamped; behavior of today).
- Headless `ask -p` text output. It shows no level today.

## 3. Placement

| Screen part (owner) | What changes | Data it reads |
|---|---|---|
| Footer row (`footer.go`, `footerText`) | The level text can be `default`. No layout change: `default` has 7 columns, as `minimal` has | `sessionView.thinking`, from `ACPStateResult.ThinkingLevel`, `ACPSetModelResult.ThinkingLevel` and `ACPThinkingResult.Level` |
| Application shortcuts (`keys.go`, `shift+tab` to `cycleThinking`) | The cycle list gets `default` as its first entry (`thinkingLevels` in `footer.go:29`) | The same |
| Status row (`model_cycle.go`, `cycleRows`) | The cycle notice tail can be ` · default` | The `SetModel` answer |
| Status row (`status.go`, `noticeRow`) | A new two-line error notice when an older leader refuses `default` (sketch D7) | The `SetThinking` error |

Nothing moves. The header row, the block pane, the queue rows, the prompt pane and the viewer do not change.

### 3.1 Agent, wire, hooks and ACP

**One word: `default`.** Add `protocol.ThinkingDefault = "default"` to `pkg/protocol/message.go`. It means "no level chosen". The agent still stores the empty string in `Options.Reasoning`, so the provider adapters do not change. The word is converted at the edges only:

| Edge | Input rule | Output rule |
|---|---|---|
| `Agent.State()` (`agent.go:573`, `thinkingLevel`) | – | empty gives `default`, for every model (with thinking, without thinking, and no model) |
| `Agent.SetThinkingLevel` (`agent.go:459`) | `default` stores empty with no clamp; any other word is clamped as today | – |
| `Agent.SetModel` and `agent.New` | no change: an empty level stays empty (commits `0c33171`, `f74cbf9`); a start level `default` stores empty | – |
| Hook view `pipeline.Request.ThinkingLevel` (`loop.thinkingLevel`, `loop_run.go:422`) | – | empty gives `default` |
| Hook update `RequestUpdate.ThinkingLevel` (`loop_run.go:392`) | empty keeps the level (as today); `default` stores empty with no clamp; any other word is clamped | – |
| Message stamp `AssistantMessage.ThinkingLevel` (`loop_stream.go:224`) | – | empty gives `default` (open question 3) |
| `ASK_REASONING_LEVEL` (`headless.go:477`) | – | `default` (from `State()`) |
| Session log `RequestOptions.Reasoning` (`internal/sessions/entry.go:159`) | – | no change: it records what the request sent, and empty stays absent |

**Lists of level words.** The word must be accepted in these lists and refused nowhere else:

| List | Accepts `default` | Why |
|---|---|---|
| `validThinking` (`internal/acp/ask_methods.go:330`): `setThinking`, `setModel.thinkingLevel` | yes | The TUI sends it with `shift+tab`, and a scope entry `:default` sends it with `setModel` |
| `validThinking` through `session_options.go:62`: `_meta["ask.dev/session"].thinking` | yes (open question 2) | Same word as `--thinking` |
| `validLevel` (`internal/app/default_model.go:249`) | yes (open question 2) | Scope entries and settings use it |
| `thinkingLevels` in `cmd/tui/args.go:106`: `--thinking` | yes (open question 2) | `--thinking default` overrides a `:level` of the pattern and the `medium` start of `ask -p --provider anthropic` |
| `splitLevel` (`internal/providers/model_pattern.go:195`): `:level` of a pattern | yes (open question 2), with its own check | `thinkingRank` is the clamp order and must not get `default` |
| `thinkingRank` (`internal/providers/clamp.go:5`) | no | It is the clamp order. `ClampThinkingLevel` never sees `default`: the edges above convert it first |
| `thinkingLevels` in `internal/tui/footer.go:29`: the `shift+tab` cycle | yes, first entry | Sketches D1 to D3 |

**Results.** `ACPStateResult.ThinkingLevel`, `ACPSetModelResult.ThinkingLevel` and `ACPThinkingResult.Level` carry `default` for no choice. The JSON fields and tags do not change. An empty `ACPModelRequest.ThinkingLevel` still means "keep the level", and `ACPThinkingRequest.Level` still must not be empty.

**Dropped level notice.** `onCycleSet` (`model_cycle.go:210`) shows `ACPLevelDropped` when the entry states a level and the answer is `off`. A model with no thinking clamps a chosen level to `off`, so this does not change. An entry with `:default` never shows the notice.

**Standard ACP.** `ask acp` exposes the level only through the `_ask/session/*` methods; `session/set_config_option` answers `unsupported` (`internal/acp/agent.go:533`). An ACP client such as Zed sees no change. A client of the `_ask` methods sees `default` where it saw `off`.

**Hooks.** Problem 2 is fixed because the hook now sees `default`, so `off` is a change and `Registry.PrepareRequest` keeps it. A hook that sets `default` gives the choice back to the provider for this request and the later ones. The doc comments change:

- `pipeline.Request.ThinkingLevel` (`points.go:53`): "`default` when no level is chosen and the provider path picks; `off` when the request asks for the lowest level of the model."
- `pipeline.RequestUpdate.ThinkingLevel` (`points.go:66`): "Empty keeps the current level. `default` removes the chosen level with no clamp. Any other word is clamped to the model of the request, like a level that the user sets."

#### 3.1.1 Alternatives for the wire (choice A)

| | A1: the word `default` (recommended) | A2: the empty string in results |
|---|---|---|
| Result for no choice | `"default"` | `""` |
| How a client goes back to no choice | `setThinking` with `default` | A new field, for example `ACPThinkingRequest.Clear`, because an empty `Level` is refused and an empty `ACPModelRequest.ThinkingLevel` means "keep" |
| How a hook goes back to no choice | `RequestUpdate.ThinkingLevel = default` | Not possible: an empty update means "keep" (`points.go:69`) |
| Does a hook that sets `off` force off | yes | yes |
| Older TUI with a new leader | The footer row shows `◐ default` (it prints the word, `footer.go:150`). `shift+tab` goes to `off`, because an unknown base starts the cycle at the first entry (`update_input.go:562`) | The footer row shows no level (`footer.go:148` hides an empty level) |
| `--thinking`, `:level`, `_meta.thinking` | One more word | No word for "no choice" |

A1 uses one word in every place, and every request path can carry it. A2 needs a second request shape and leaves hooks with no way back. This design uses A1.

#### 3.1.2 Older and newer builds

A client of a newer build replaces an older leader that a client started (AGENTS.md, `ask leader`). The only mixed pair that remains is an older leader that the user started by hand with `ask leader`.

| Pair | What the user sees | Behavior |
|---|---|---|
| New TUI, new leader | `◐ default` | This design |
| New TUI, older leader started by hand | `◐ off` for no choice (the older `State()`) | `shift+tab` from `max` asks `default`. The older leader answers `invalid_params` with the message `unknown thinking level` (`ask_methods.go:365`). The TUI shows sketch D7, and the next press asks `off`. `--thinking default` fails at session start: the older leader refuses the session option with `invalid_params` `the thinking option is not a thinking level` (`internal/acp/session_options.go:63`) |
| Older TUI, new leader | `◐ default` | The older cycle has no `default` entry, so it cannot go back to no choice. It never sends `default` |
| `ask acp` with an older `_ask` client | `default` in the result fields | A client that checks the word against a fixed list must add `default` |

No capability flag is added: the leader replacement and the refusal of the older leader cover the start, and D7 covers the cycle.

## 4. Sketches

#### D1. Session start, no level chosen

The TUI started with no --thinking and no :level, on a model that has thinking. The request sends no level, so the default of the provider applies. The footer row says default, not off. No notice.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                          openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 0% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D2. shift+tab from default goes to off

One press of shift+tab while the run is idle. The level is now a choice: off. Only the footer row changes, as for every other level today; the status row stays empty.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                              openai/gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D3. shift+tab from the last level comes back to default

From max (or from the highest level that the model has), the next press asks default. The footer row says default again, and the next request sends no level.

Regions: row 1 header; row 2 rule; rows 3-19 block pane; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                          openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D4. ctrl+p to a scope entry that states no level

The level stays default after a model change. The cycle notice names it, as it names a chosen level today.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Model: anthropic/claude-sonnet-4-6 · default · 2 of 3                       
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore            anthropic/claude-sonnet-4-6 ◐ default  ▰▱▱▱▱▱▱▱ 12% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D5. ctrl+p to a model that has no thinking

A model with no thinking shows no level in the footer row and in the notice, as today. The session keeps default; the next model with thinking shows default again.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ ✓ Model: local/alpha · 3 of 3                                                 
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                                       local/alpha  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D6. shift+tab while the run works

The text is the text of today. The level does not change, and the footer row keeps default.

Regions: row 1 header; row 2 rule; rows 3-18 block pane; row 19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
  ❯ show me the probe code                                               14:03  
                                                                                
  ◆ Reading internal/leader/client.go                                        ⠋  
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ Thinking level can change when the run ends.                                  
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                          openai/gpt-5.5 ◐ default  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D7. An older leader refuses default

A leader that was started by hand from an older build answers invalid_params for the word default. The level stays max. The next press skips default and asks off, so the cycle does not stop.

Regions: row 1 header; row 2 rule; rows 3-17 block pane; rows 18-19 status; row 20 rule; row 21 prompt pane; row 22 rule; row 23 footer; row 24 bar.

80×24:

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ why does TestLeaderStale fail on CI?                                 14:02  
                                                                                
  The test waits for the stale socket check, but CI runs it with a 1 s          
  deadline. The leader removes the stale socket after the lock probe, and the   
  probe can take 1.2 s.                                                         
                                                                                
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k · $0.040                                        
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
┊ Could not change the thinking level: the leader does not know default.        
┊ Restart it with this build: ask leader stop                                   
────────────────────────────────────────────────────────────────────────────────
 ❯ Ask anything  ·  / commands  ·  ctrl+l model                                 
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                              openai/gpt-5.5 ◐ max  ▱▱▱▱▱▱▱▱ 4% 
 Tab blocks · ctrl+t thinking · ctrl+l model · / commands                       
```

#### D8. Narrow terminal: ctrl+p notice and footer row

Under 60 columns the footer row shows no level (unchanged). The short cycle notice names default.

Regions: row 1 header; row 2 rule; rows 3-8 block pane; row 9 status; row 10 rule; row 11 prompt pane; row 12 rule; row 13 footer; row 14 bar.

40×14:

```text
 ◢◤ ask                                 
────────────────────────────────────────
  check, but CI runs it with a 1 s      
  deadline. The leader removes the      
  stale socket after the lock probe,    
  and the probe can take 1.2 s.         
                                        
  gpt-5.5 · 4.2s · ↑12.4k ↓1.8k …       
┊ ✓ openai/gpt-5.5 · default            
────────────────────────────────────────
 ❯ Ask anything                         
────────────────────────────────────────
                      openai/gpt-5.5 4% 
 Tab blocks · / commands                
```

## 5. States and transitions

The cycle order is `default`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, then `default` again.

| State | Trigger | Next state |
|---|---|---|
| Start with no `--thinking` and no `:level` (D1) | – | Footer row `◐ default` on a model with thinking; no level on a model without thinking |
| Start with `--thinking default` or `:default` | – | Same as D1 |
| Start with a chosen level | – | Footer row shows that level (no change) |
| `default` (D1) | `shift+tab`, idle | `off` (D2); `SetThinking(off)`; footer row only |
| A level | `shift+tab`, idle | The next entry, clamped by the model as today; `thinkTry` keeps the asked entry, so a clamped entry does not stop the cycle |
| `max`, or the highest level of the model | `shift+tab`, idle | `default` (D3); `SetThinking(default)`; no clamp |
| Any level | `shift+tab` while the run works | No change; status row `Thinking level can change when the run ends.` (D6) |
| Any level, model with no thinking | `shift+tab` | No change and no notice (`cycleThinking` returns early, as today) |
| `default` | `ctrl+p` to an entry with no level | Model changes, level stays `default`; notice `✓ Model: <provider>/<model> · default · <n> of <total>` (D4) |
| `default` | `ctrl+p` to an entry with a level | That level, clamped; the notice names it (no change) |
| `default` | `ctrl+p` to an entry `:default` | Level stays `default` (D4) |
| A chosen level | `ctrl+p` to an entry `:default` | `default`; notice tail ` · default` |
| `default` | `ctrl+p` to a model with no thinking | Notice with no level; footer row with no level (D5). The session keeps `default` |
| `default` | `/model` choice | Footer row keeps `◐ default` (no notice, as `TestModelChangeShowsInFooterOnly`) |
| `max` (older leader) | `shift+tab` asks `default`, answer `invalid_params` | Level stays `max`; error notice D7; `thinkTry` becomes `default`, so the next press asks `off` |
| Any | Other `SetThinking` error | `Could not change the thinking level: <reason>` (no change) |
| Any | A hook sets `off` | The request uses the lowest level of the model; the stored level becomes `off`; the footer row shows `◐ off` after the next state read (`refreshCmd`, for example at `agent_settled`) |
| Any | A hook sets `default` | The stored level becomes empty; the footer row shows `◐ default` after the next state read (`refreshCmd`) |
| Any | A stale answer (`msg.gen != s.gen`) | Ignored (no change) |

## 6. Keys and focus

The input order of `docs/tui-architecture.md` does not change. No key is added.

| Key | Receiver | Action |
|---|---|---|
| `shift+tab` | Application shortcuts (`keys.go`), prompt focus, block list focus and with the command list open | Next entry of the cycle, which now starts with `default`. A dialog or a selector in the prompt slot gets the key first, as today |
| `ctrl+p`, `ctrl+shift+p` | Application shortcuts | No change; the notice can name `default` |

## 7. Limited space and resize

- The footer row keeps its give-way order (`docs/tui-style-guide.md`, footer row). `default` has the same width as `minimal`, the longest level today, so no width rule changes.
- Under 60 columns the footer row shows no level, as today (`footer.go:148`). Sketch D8.
- The cycle notice cuts the end of the model id first; the tail ` · default` stays (no change of `cycleRows`). Under 60 columns it is `✓ <provider>/<model> · default`.
- The D7 notice has two rows (a line feed in the notice text, as `noticeRow` splits it). At 80 columns the first row has 72 columns. In a narrower terminal the layout cuts the end of each row with `…` (`truncateTo` in `layout.go:189`), as for every notice.
- At 120×40 the layout is the same as at 80×24; only the padding of the footer row is wider. No 120×40 sketch is needed.
- A resize does not change the level or the notice.

## 8. Text and width

- `default` is ASCII, 7 columns. No wide characters.
- The model text follows the existing rules (`cutModelText`): the provider stays whole and the end of the id is cut with `…`.
- The D7 notice uses the fixed text of this design. It does not show the raw message of the leader, so a long message cannot make the row wider.

## 9. Style intent

- `◐ default` uses the `assistant` role, as every level.
- The cycle notice: `✓` in `success`, the rest in `dim` (no change).
- The D7 notice: both rows in `error`, the rail `┊` in `dim` (as every error notice).

## 10. Acceptance criteria

The TUI checks use `startView` (`internal/tui/session_rig_test.go`) with goldens under `internal/tui/testdata`. The end-to-end checks run the built `ask` with `python3 e2e/tui/run.py --driver ask`.

1. At 80×24, after a session start with no level on a model with thinking, the footer row shows `◐ default` and not `◐ off`. Golden: `TestFooterRowGoldens/default_80x24` (new subtest).
2. At 80×24, after one `shift+tab` from `default`, the footer row shows `◐ off`, the status row is empty, and the client got `SetThinking(off)`. Test: `TestThinkingCycleFromDefault` (new).
3. At 80×24, after `shift+tab` from `max`, the footer row shows `◐ default` and the client got `SetThinking(default)`. Test: `TestThinkingCycleBackToDefault` (new).
4. At 80×24, on a model that clamps `xhigh` and `max` to `high`, after eight presses of `shift+tab` from `default`, the footer row shows `◐ default`. Test: `TestThinkingCycleBackToDefault/clamped` (new).
5. At 80×24, after `ctrl+p` to a scope entry with no level while the level is `default`, the status row shows `✓ Model: anthropic/claude-sonnet-4-6 · default · 2 of 3` and the footer row shows `◐ default`. Golden: `TestModelCycleKeyKeepsDefault` (new).
6. At 80×24, after `ctrl+p` to a model with no thinking, the status row shows `✓ Model: local/alpha · 3 of 3` and the footer row shows no `◐`. Existing goldens `TestModelCycleKeyWithoutScope` and `TestModelCycleKeyDroppedLevel` do not change.
7. At 80×24, after `shift+tab` while the run works, the status row shows `Thinking level can change when the run ends.` and the footer row keeps `◐ default`. Test: `TestThinkingCycleRefusedWhileRunning` (new).
8. At 80×24, after `shift+tab` from `max` when the client answers `invalid_params` for `default`, the status row shows `Could not change the thinking level: the leader does not know default.` and `Restart it with this build: ask leader stop` in the error role, the footer row keeps `◐ max`, and the next `shift+tab` sends `SetThinking(off)`. Golden: `TestThinkingDefaultRefusedByOlderLeader` (new).
9. At 40×14, after `ctrl+p` to `openai/gpt-5.5` with no level, the status row shows `✓ openai/gpt-5.5 · default` and the footer row shows `openai/gpt-5.5 4%` with no level. Golden: `TestModelCycleKeyKeepsDefault/40x14` (new).
10. `_ask/session/get_state` of a session with no level answers `"thinkingLevel":"default"`; `_ask/session/set_thinking` with `default` answers `{"level":"default"}`; `_ask/session/set_model` with no `thinkingLevel` on a session with no level answers `"thinkingLevel":"default"`. Tests: `internal/acp` `TestAdapterThinkingDefault` (new); `ask_methods_test.go:49` and `driver_commit_test.go:171` change from `off` to `default`.
11. A `PrepareRequest` hook on a session with no level sees `default`; when it sets `off`, the request of `grok-4.7` sends the lowest effort of the model and `State()` reports `off`; when it sets `default` on a session at `high`, the request sends no effort and `State()` reports `default`. Tests: `internal/agent` `TestPrepareRequestHookForcesOff`, `TestPrepareRequestHookRestoresDefault` (new).
12. `ask -p --thinking default --provider anthropic` sends no thinking level from the command line, and the adapter rule applies. `ask -p --thinking ultra` still prints `Invalid thinking level "ultra". Valid values: default, off, minimal, low, medium, high, xhigh, max`. Test: `cmd/tui` `TestThinkingFlagDefault` (new). (Depends on open question 2.)
13. End-to-end, case `ask-thinking-default` (new; `ASK_HOME` with a `models.json` that has a provider `local` and a model `gamma` with `"reasoning": true`; args `--model local/gamma`): at 80×24 the footer row shows `local/gamma ◐ default`; after one `shift+tab` it shows `◐ off`; after seven more presses it shows `◐ default` again. The case captures `start`, `off` and `back`.
14. End-to-end, the existing cases `ask-idle` and `ask-model-cycle` pass with no change (`faux-1`, `local/alpha` and `local/beta` have no thinking, so their footer rows show no level).

## 11. Open questions

1. **Choice A and choice B.** Choice A (section 3.1.1): use the word `default` on the wire (A1, recommended) or the empty string in results with a new clear field (A2)? Choice B: put `default` as the first entry of the `shift+tab` cycle (B1, recommended; the only way back to no choice with no new key), or keep it out of the cycle and add a `/thinking default` command (B2; a new command, more scope)?
2. **Start words.** Is `default` a valid value of `--thinking`, of `_meta["ask.dev/session"].thinking` and of the `:level` suffix (recommended: yes, one word list everywhere)? With yes, `--thinking default` also removes the `medium` start of `ask -p --provider anthropic`.
3. **Message stamp.** Should `AssistantMessage.ThinkingLevel` record `default` (recommended: it records what Ask asked, as it does for a level), or stay absent for no choice?
4. **The word.** `default` reads correctly in the footer row, in the cycle, in `--thinking default` and in `:default`, and it does not collide with `auto` (adaptive effort in Oh My Pi). It does not say that the vendor decides, which is correct for the Anthropic and OpenAI Completions adapters (they use `medium`). Keep `default`?
5. **D7 text.** Keep the two rows `Could not change the thinking level: the leader does not know default.` and `Restart it with this build: ask leader stop`?
