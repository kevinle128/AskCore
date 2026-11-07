# H7 mockup: model configuration and resolution

Status: illustration only. It shows every behavior that the plan contains after the red-team review (2026-11-06).
The TUI sketches are proposals. The binding TUI design is `design.md` (phase 7), and only the user approves it.
The cycling key (`ctrl+p`) is a proposal for the open question QC.

## 0. What changed between versions

| Topic | First plan (2026-11-05) | Current plan (after red-team, 2026-11-06) |
|---|---|---|
| Files read | `~/.ask/models.json` and trusted `.ask/models.json` | `~/.ask/models.json` only (Q2b). `.ask/models.json` is never read |
| Key of a custom provider | Refused by the credential binding | Accepted when the key endpoint equals the row `baseUrl` |
| Custom provider in TUI and leader | Not usable (only `ask -p` worked) | Usable everywhere: start model, saved default, `/model`, `setModel` |
| Provider id | Any text | Lower case letters, digits and `-`; a bad id is a file problem |
| Id with a colon (`qwen3-coder:30b`) | Read as an unknown level | Whole id first, then the `:level` rule of oh-my-pi |
| Model list rebuild | On every request | Session start, `/model` open, `setModel`, cache write |
| File edit during a session | Old row could mix with new key | Request fails: `model configuration changed; choose the model again` |
| `!command` cancelled | Cached as a failure for 30 s, all sessions | Never cached; each caller waits only for itself |
| Older leader | Dropped `--model` with no message | Session start fails with a clear message |
| `ask -p` with Token Plan | Own code path, sent `x:high` as the id | Same model list path as other providers |
| Requested level on a model with no thinking | Dropped with no message | Warning line and a session notice |
| `ask settings show` | Problems only | Problems and the key source of each provider (never the value) |

## 1. The user file `~/.ask/models.json`

Mode must be 0600 (QB). JSON with comments and trailing commas.

```jsonc
{
  "providers": {
    // A local Ollama server. A new provider must state api and baseUrl.
    "local": {
      "api": "openai-completions",
      "baseUrl": "http://127.0.0.1:11434/v1",
      "apiKey": "ollama",                       // a bare string is a literal
      "models": [
        { "id": "qwen3-coder:30b", "contextWindow": 262144 },
        { "id": "llama4-scout" },               // Pi defaults: 128000 / 16384, text, no thinking
      ],
    },

    // A company gateway. The key comes from an env var or a command.
    "gateway": {
      "api": "openai-responses",
      "baseUrl": "https://llm.company.example/v1",
      "apiKey": "!op read op://work/llm/key",   // command output, cached in the process
      "headers": { "X-Team": "${TEAM_ID}" },     // env template
      "models": [ { "id": "company-llm", "reasoning": true, "maxTokens": 32000 } ],
    },

    // Point a built-in provider at a proxy. The saved auth.json key is NOT sent there (QA);
    // this file key is used, and the account model list no longer filters this provider.
    "openai": {
      "baseUrl": "https://proxy.company.example/openai/v1",
      "apiKey": "$PROXY_OPENAI_KEY",
      "modelOverrides": {
        "gpt-5.5": { "maxTokens": 64000 },       // wins after every models.dev refresh
      },
    },
  },
}
```

Value grammar (user file only): `$NAME`, `${NAME}`, whole-value `!command`, `$$` for a literal `$`, `$!` for a literal `!`.

Key order for one request (Q1): `--api-key`, then the saved credential (`auth.json` or OAuth), then the `apiKey` of this file, then the env var of the provider. A source that does not bind to the row endpoint is skipped.

## 2. Command line

### 2.1 Model patterns

```text
$ ask -p --model anthropic/claude-sonnet-4-6:high "hi"
Hi! How can I help?

$ ask -p --model sonnet-4-6:high "hi"                 # bare id, alias, :level
Hi! How can I help?

$ ask -p --model local/qwen3-coder:30b "hi"          # the whole id matches first
Hello.

$ ask -p --model openai/gpt-5.5@openai-responses:low "hi"   # @api still works
Hi.
```

### 2.2 Errors and warnings

```text
$ ask -p --model gpt-5.5 "hi"
Error: gpt-5.5 is ambiguous. Choose one:
  openai/gpt-5.5
  gateway/gpt-5.5
exit 1

$ ask -p --model anthropic/claude-sonnet-4-6:hgih "hi"
Error: unknown model anthropic/claude-sonnet-4-6:hgih
exit 1

$ ask -p --model local/llama4-scout:high "hi"
Warning: local/llama4-scout has no thinking; level high is not used.
Hello.

$ ask -p --model LMStudio/x "hi"         # when the file has a provider "LMStudio"
Warning: models.json: providers[3]: provider id must use lower case letters, digits and "-"
Error: unknown model LMStudio/x
exit 1
```

### 2.3 Scope list and offline

```text
$ ask --models 'anthropic/*:medium,local/qwen*,openai/gpt-5.5:high'
  # starts on the saved default when it is in this list (QD), else on the first entry
  # ctrl+p cycles only through these models

$ ask --offline
  # same as ASK_DISABLE_MODEL_REFRESH=1 for this process and a leader it starts
```

`~/.ask/settings.json` can hold the same list:

```json
{ "enabledModels": ["anthropic/*:medium", "local/qwen*", "openai/gpt-5.5:high"] }
```

### 2.4 Older leader

```text
$ ask --model anthropic/claude-sonnet-4-6:high
Error: the running leader did not apply --model. It is an older build.
Run "ask leader stop", then start ask again.
exit 1
```

## 3. `ask settings show`

```text
$ ask settings show
Trust        ~/src/askcore: trusted (user, 2026-11-04)

Settings
  defaultModel      openai/gpt-5.5@openai-responses     user
  enabledModels     3 patterns                           user
  thinkingFold      truncated                            default

Models file  ~/.ask/models.json  (0600, 3 providers, 4 models)
  Problems
    providers.gateway.models[0]: unknown key "maxToken"
    providers[3]: provider id must use lower case letters, digits and "-"

Key source
  anthropic   saved (oauth)
  openai      models.json (user)        # saved key skipped: baseUrl changed
  local       models.json (user)
  gateway     models.json command (cached)
  xai         env XAI_API_KEY
  alibaba-token-plan   none

Not read
  ~/src/askcore/.ask/models.json   project models files are not read
```

A key value, a header value and a command output never print.
A problem names the schema path or a position, never a value or a user key name.

## 4. TUI (proposals for the phase 7 design)

### 4.1 `/model`: custom providers next to built-in providers

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ refactor the cache loader                                            10:12  
                                                                                
  Done. The loader now reads the file once per session.                         
  claude-sonnet-4-6 · 5.1s · ↑22.0k ↓1.4k                                       
                                                                                
────────────────────────────────────────────────────────────────────────────────
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● anthropic                                                                │
│ ❯   claude-sonnet-4-6                                         1M  current    │
│     claude-opus-4-9                                            ?  reasoning  │
│   ● gateway                                                                  │
│     company-llm                                             128k  reasoning  │
│   ● local                                                                    │
│     llama4-scout                                            128k             │
│     qwen3-coder:30b                                         256k             │
│   ● openai                                                                   │
│     gpt-5.5                                                   1M  reasoning  │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  your draft is kept                            
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore          anthropic/claude-sonnet-4-6 ◐ medium  ▰▱▱▱▱▱▱▱ 11%    
 Tab blocks · ctrl+t thinking · ctrl+l model · ctrl+p next model · / commands   
```

### 4.2 Cycling key: one press moves to the next model of the scope

The scope entry carries its level (`openai/gpt-5.5:high`). One call switches the model and the level. The footer changes; one notice line says what happened. Presses while a switch runs are ignored.

```text
 ◢◤ ask                                                                         
────────────────────────────────────────────────────────────────────────────────
  ❯ refactor the cache loader                                            10:12  
                                                                                
  Done. The loader now reads the file once per session.                         
  claude-sonnet-4-6 · 5.1s · ↑22.0k ↓1.4k                                       
                                                                                
  ○ model: openai/gpt-5.5 · high  (2 of 3 in scope)                             
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
                                                                                
────────────────────────────────────────────────────────────────────────────────
 ❯ ▌Ask anything  ·  / commands  ·  ctrl+l model                                
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                  openai/gpt-5.5 ● high  ▰▱▱▱▱▱▱▱ 4%            
 Tab blocks · ctrl+t thinking · ctrl+l model · ctrl+p next model · / commands   
```

While the agent runs, a press shows `○ model change waits until the run ends` and changes nothing.

### 4.3 `/model` opens after a bad edit: the last valid copy stays

```text
────────────────────────────────────────────────────────────────────────────────
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⚠ models.json has 1 problem; the last valid copy is in use                  │
│    providers.local.models[2]: "contextWindow" must be a number               │
│  ⌕ ▌type to filter                                                           │
│   ● local                                                                    │
│ ❯   llama4-scout                                            128k             │
│     qwen3-coder:30b                                         256k  current    │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter select  ·  esc close  ·  ask settings show for all problems            
```

### 4.4 The current model left the file

The user removed `qwen3-coder:30b` from `models.json`. The row keeps `current` and gets a note. The next prompt fails with a fixed text.

```text
╭─ Select model ───────────────────────────────────────────────────────────────╮
│  ⌕ ▌type to filter                                                           │
│   ● local                                                                    │
│ ❯   qwen3-coder:30b · not in models.json                    256k  current    │
│     llama4-scout                                            128k             │
╰──────────────────────────────────────────────────────────────────────────────╯

  ✗ model configuration changed; choose the model again  ·  ctrl+l model        
```

### 4.5 Requested level dropped

```text
  ○ local/llama4-scout has no thinking; level high is not used                  
────────────────────────────────────────────────────────────────────────────────
 ⌂ ~/src/askcore                  local/llama4-scout ○ off  ▱▱▱▱▱▱▱▱ 0%         
```

## 5. Not in H7

- A project `.ask/models.json` (removed by Q2b; the file stays in the trust list, but nothing reads it).
- Session restore of a model (H8).
- A file watcher or polling: the file is read again only at the points of row 0 "Model list rebuild".
