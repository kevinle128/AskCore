# Real-provider acceptance of the Ask TUI (T1a)

Date: 2026-10-10. Binary: `ask` build `25ebbea71f69+dirty`, installed with `go install` into `/tmp/ask-rp-bin` (not committed). Every scenario ran `ask` with no prompt in a PTY, against its own leader that the TUI started.

## Status

DONE_WITH_CONCERNS. All scenarios ran on real providers and all pass on behavior. There are 4 findings (2 medium, 2 low) and 2 caveats for the orchestrator, listed below. No product code, design.md or plan.md was edited.

## Method and tools

- Driver: `e2e/tui/real_provider.py` (PTY session, pyte screen, ANSI stripped) and `e2e/tui/real_provider_scenarios.py` (the scenarios). Both are new files. `pyte` came from `pip install --target /tmp/ask-rp-py pyte` (outside the repo). Run with `PYTHONPATH=/tmp/ask-rp-py python3 e2e/tui/real_provider_scenarios.py <repeat|cancel-esc|cancel-ctrl-c|ctrlc-running|invalid|switch|thinking|detach> [cols rows]`.
- Plain-text screens of every step are in `/tmp/ask-rp-out/*.txt` and `/tmp/ask-rp-out/steps.log` (outside the repo, not tracked).
- Credentials: the OpenAI key was only the inherited variable `OPENAI_API_KEY`. The invalid-key child got `OPENAI_API_KEY=<dummy>` only in its own environment. OAuth runs removed `OPENAI_API_KEY` from the child environment, so the OAuth credential was the only one available. No token or key value was read or printed.
- The terminal capture shows a stray `1u` before the "Detached" notice. This is a pyte limit: the raw bytes hold the kitty keyboard pop `ESC [ < 1 u`, which pyte prints as text. It is not a product defect (verified from the raw bytes).

## Scenario table

| # | Scenario | Size | Provider and model | Result |
|---|---|---|---|---|
| 1 | 3 prompts in one session | 80x24 | OpenAI API key, gpt-5.5 | pass |
| 2 | 3 prompts in one session | 120x40 | OpenAI API key, gpt-5.5 | pass |
| 3 | Cancel with esc during a long reply, then new prompt | 80x24 | OpenAI API key, gpt-5.5 | pass |
| 4 | ctrl+c during a long reply (quit confirm), then esc cancel | 80x24 | OpenAI API key, gpt-5.5 | pass |
| 5 | ctrl+c during a long reply, reply runs to the end, next prompt | 120x40 | OpenAI API key, gpt-5.5 | pass (see note) |
| 6 | Provider error (invalid key), then new session with the good key | 80x24 | OpenAI API key, gpt-5.5 | pass, finding F1 |
| 7 | Model switch OpenAI to Anthropic and back, prompt after each | 80x24 | ChatGPT OAuth gpt-5.6-sol, Anthropic OAuth claude-sonnet-4-6 | pass, findings F2, F4 |
| 8 | Thinking toggle ctrl+t | 80x24 | Anthropic OAuth claude-sonnet-4-6 (level `low`) | pass |
| 9 | ctrl+d while a run is active, second `ask` attaches via /sessions as observer | 80x24 | OpenAI API key, gpt-5.5 | pass |
| 10 | Visible retry (429) | | | not run, see below |

Note for 4 and 5: design says ctrl+c is not a cancel key. With a non-empty run and an empty draft the first ctrl+c shows the quit confirm and the run goes on. This is the behavior in the design (S15, key table). Cancel is esc only. The task text asked for "esc/ctrl+c"; esc is the cancel key.

## Evidence (screens, ANSI stripped)

### 1 and 2. Repeated prompts

80x24, final screen after the third prompt. The meta line under each reply shows model, time, tokens, cost. Footer keeps the leader id.

```
◢◤ ask   v25ebbea71f69+dirty · leader c8fc · /tmp

  ❯ reply with the single word ok

  ◆ ok
    gpt-5.5 · 1.9s · ↑53 ↓5 · $0.000

  ❯ reply with the single word ok

  ◆ ok
    gpt-5.5 · 1.4s · ↑70 ↓5 · $0.001

  ❯ reply with the single word ok

  ◆ ok
    gpt-5.5 · 2.6s · ↑87 ↓5 · $0.001

╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ Ask anything  ·  / commands  ·  ctrl+l model                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader c8fc   ⌂ /tmp                      gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

Replies streamed ("Working…" row then the reply). No stale rows. The footer cost stays `$0.00` after three tiny replies (sum about $0.002 rounds to `$0.00`); after a 446-token reply (scenario 5) the footer shows `$0.01` and after the next prompt `$0.02`, so cost updates. The context bar stays `0%` (about 500 of 1.05M tokens). 120x40 gave the same structure (leader id `1ae7`).

### 3. Cancel with esc

Screen after esc and after settle (the same text at 150 ms and at 1.5 s):

```
  ∴ Thinking...
  ◆ The Keeper of Greyhaven Light
    Every evening, just as the sun slipped into the sea like a coin into a
    wishing well, Elias climbed the winding iron stairs of Greyhaven
    Lighthouse. He had done so for thirty-seven years, his boots knowing each
    step by heart. At the top, he polished the great glass lens until it shone
    like frozen
  ⊘ Cancelled by you. Partial reply kept above.
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ Ask anything  ·  / commands  ·  ctrl+l model                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 953a   ⌂ /tmp                      gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

The next prompt ("reply with the single word ok") streamed `◆ ok` with `gpt-5.5 · 1.1s · ↑146 ↓18 · $0.001`. The marker is present at the first sample, 150 ms after esc. Real providers cancel too fast to see the "marker only after settle" order; `TestInteractiveCancelThenPrompt` and `TestCancelMarkerAtSettleOnly` own that order.

### 4. ctrl+c during a run

0.4 s after ctrl+c the status row shows the quit confirm and the reply keeps streaming:

```
    like lost children.

    Elias had kept the light for thirty-seven years.

    He
┊ Press ctrl+c again to quit · the run continues in the leader
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ Ask anything  ·  / commands  ·  ctrl+l model                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader c0dc   ⌂ /tmp                      gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

esc afterwards showed `⊘ Cancelled by you. Partial reply kept above.` The quit notice replaces the `Working…` row for 2 s; the design does not forbid this.

### 5. Long reply at 120x40 (cost update)

After the 446-token reply: meta `gpt-5.5 · 10s · ↑59 ↓446 · $0.014`, footer `... gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.01`. The following "ok" prompt gave `↑503 ↓5 · $0.003` and the footer `$0.02`. No defect.

### 6. Provider error, then good key

Child with `OPENAI_API_KEY=<dummy>`:

```
  ❯ reply with the single word ok

  ✗ Error (auth): HTTP 401: { "error": { "message": "Incorrect API key
    provided: dummy-in**********0000. You can find your API key at
    https://platform.openai.com/account/api-keys.", "type":
    "invalid_request_error", "param": null, "code": "invalid_api_key" } }.
    Run in a shell: ask auth login --provider openai. Then send again.
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ Ask anything  ·  / commands  ·  ctrl+l model                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 2794   ⌂ /tmp                      gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

The block shows the auth kind and the login hint (pass). After `ctrl+d` and `ask leader stop`, a new session in the same home with the good key answered `◆ ok  gpt-5.5 · 1.2s · ↑53 ↓5 · $0.000` (pass). See F1.

### 7. Model switch across providers

Home: copy of the OAuth home. Footer after each switch: `gpt-5.6-sol`, then `claude-sonnet-4-6`, then `gpt-5.6-sol` again; each prompt got a reply. Last screen:

```
  ❯ reply with the single word ok
  ◆ ok
    gpt-5.6-sol · 3.1s · ↑53 ↓5 · $0.000

  ❯ reply with the single word ok
  ● echo  ok                                                            ✓ 0.0s
  ◆ ok
    claude-sonnet-4-6 · 0.9s · ↑663 ↓4

  ❯ reply with the single word ok
  ◆ ok
    gpt-5.6-sol · 3.3s · ↑115 ↓5 · $0.001
╭──────────────────────────────────────────────────────────────────────────────╮
│ ❯ Ask anything  ·  / commands  ·  ctrl+l model                               │
╰──────────────────────────────────────────────────────────────────────────────╯
  ● leader 3412   ⌂ /tmp                  gpt-5.6-sol ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

A first attempt with `openai/gpt-5.5` in the OAuth home failed (F2). See F4 for the `echo` line and the missing cost.

### 8. Thinking toggle

claude-sonnet-4-6 at level `low` (shift+tab twice) streams thinking text. Shown: `∴ 391`. After ctrl+t: `∴ Thinking...` and the status row `┊ Thinking blocks: hidden · ctrl+t to show`. After ctrl+t again: `∴ 391` and `┊ Thinking blocks: visible · ctrl+t to hide`. Pass (criterion 18).

### 9. Detach and observe

ctrl+d at the first `ask` while it was streaming: process exited with code 0, screen ended with

```
  ⏏ Detached from session 30da. The run continues in leader e33d.
    Run ask in this directory to see it again.
```

A second `ask` showed `/sessions`:

```
╭─ Live sessions ──────────────────────────────────────────────────────────────╮
│  ⌕ type to filter                                                            │
│ ❯ 30da  /tmp                                  no driver           0 watching │
│   86f0  /tmp                                  this client drives  1 watching │
╰──────────────────────────────────────────────────────────────────────────────╯
   enter watch  ·  esc close
```

enter on `30da` attached as observer; the followed reply streamed and finished (`gpt-5.5 · ↑59 ↓427 · $0.013`):

```
┊ ◌ Working… 1s · another client drives this session
╭──────────────────────────────────────────────────────────────────────────────╮
│ ◉ Watching. Type /take to take control when the run ends.                    │
╰──────────────────────────────────────────────────────────────────────────────╯
  ◉ observer 30da   ⌂ /tmp                    gpt-5.5 ◐ off  ▱▱▱▱▱▱▱▱ 0%  $0.00
```

The footer showed `$0.01` after the run. Pass (criteria 15, 16, 17). Note the list says `no driver` for a detached session whose run was still going when the detach happened; the status row says `another client drives this session`. Both are true at their moment; no action.

## Calls and models

A "call" is one model request. A prompt that triggers the `echo` tool costs 2 requests.

| Provider | Model | Requests |
|---|---|---|
| OpenAI, API key (`OPENAI_API_KEY`) | gpt-5.5 | 14 good (3+3 repeats, 2+2 cancel scenarios, 1 ctrl+c scenario, 1 after the invalid key, 1 detach, 1 first probe) |
| OpenAI, API key, dummy key | gpt-5.5 | 1 (HTTP 401) |
| OpenAI ChatGPT OAuth | gpt-5.6-sol | 2 |
| Anthropic OAuth | claude-sonnet-4-6 | 4 (2 prompts, each with an `echo` tool round trip) |

Total: 21 requests. No cheaper model exists in the catalog (`ctrl+l` lists faux, alibaba-token-plan/deepseek-v4.1-flash, openai/gpt-5.5, openai/gpt-5.6-sol, anthropic/claude-sonnet-4-6, xai/grok-4.7). gpt-5.5 was the cheaper OpenAI row. Replies were short except the three 300-word stories (about 450 output tokens each).

## Findings

| ID | Severity | Finding | Minimal reproduction |
|---|---|---|---|
| F1 | medium | The auth error block shows the raw provider JSON body (4 rows) with a masked fragment of the key. The retry note was fixed for 429 (`rate limit (HTTP 429)`), the error block was not. | `OPENAI_API_KEY=dummy ASK_HOME=/private/tmp/h ask`, ctrl+l, pick `openai/gpt-5.5`, send any prompt. The `✗ Error (auth)` block holds the whole `HTTP 401: { "error": ...}` body. |
| F2 | medium | In a home signed in with ChatGPT OAuth, `openai/gpt-5.5` is listed and selectable, but selecting it gives `openai is not ready: not signed in. Model not changed. Run in a shell: ask auth login --provider openai`. The user is signed in; `openai/gpt-5.6-sol` works in the same home. The text is misleading. Cause not verified (the adapter hides the readiness error text by design, `internal/acp/agent.go:39`). It could be a model that the ChatGPT method does not serve. | Home with an `openai-chatgpt` credential, no `OPENAI_API_KEY`. ctrl+l, pick `openai/gpt-5.5` (error), pick `openai/gpt-5.6-sol` (works). |
| F3 | low | The footer says thinking `off` for gpt-5.5, but the model still reasons (`↓18` for the one-word reply in some turns) and the transcript gets a `∴ Thinking...` row with no text, although thinking blocks are shown by default. The placeholder row appears without ctrl+t. | Send "reply with the single word ok" to gpt-5.5 several times; some replies have the `∴ Thinking...` row (seen at 80x24 in the cancel run and at 120x40 in the second repeat). |
| F4 | low | The demo `echo` tool is offered to real models. Claude called it for "reply with the single word ok" and for a multiplication prompt (`● echo  ok ✓ 0.0s`), which adds a request. The Anthropic meta line has no cost and the footer cost does not move (cost is empty for the OAuth subscription, unlike OpenAI). | Select `anthropic/claude-sonnet-4-6`, send "reply with the single word ok". |

Notes that are not defects:

- Under macOS, `ASK_HOME` below `/tmp` is refused as an unsafe path (`/tmp` is a symlink), so `ask` silently falls back to the in-process agent (footer `in-process`). The runs used `/private/tmp/...`. The task text suggested `/tmp/ask-rp-XXXX`; the real directory is required.
- Visible retry (429) cannot be forced against real providers. Evidence is `TestInteractiveRetryVisible` (criterion 7). Not run here.
- Observation for the other agents: the `Working…` row is replaced by the quit-confirm notice for 2 s (scenario 4).

## Caveats for the orchestrator

1. **OAuth token rotation.** The copy-then-run approach protects the file, not the server side. The ChatGPT access token in `$HOME/.ask-t1a-oauth` was due to expire at 04:26 (+07) and the first run in the copy refreshed it. The refresh token in the copy changed (generation 2); the original file still holds the old one, which the server may have invalidated. The Anthropic credential did not change. I kept the refreshed copy at `/private/tmp/ask-rp-oauth` (mode 700, files 600, sockets and logs removed) instead of deleting it, because deleting it would drop the only valid ChatGPT refresh token. Decide: copy `auth.json` back over the original, or run `ask auth login --provider openai --method openai-chatgpt` again, then `rm -rf /private/tmp/ask-rp-oauth`. I did not touch the original.
2. The temp binary directory `/tmp/ask-rp-bin`, the pyte directory `/tmp/ask-rp-py` and the screen artifacts `/tmp/ask-rp-out` remain (no secrets; see the scan below). Remove them when the report is read.

## Cleanup and secret scan

- Every leader I started was stopped with `ask leader stop` (return code 0 each time). Leader 94970 and the other leaders that were not mine were not touched. After the last scenario no process of `/tmp/ask-rp-bin/ask` remained.
- Temp homes `/private/tmp/ask-rp-key` and `/private/tmp/ask-rp-bad` were deleted. The OAuth home stays, see caveat 1.
- Secret scan: all 42 artifact and script files, and this report, were scanned for the value of `OPENAI_API_KEY`, `sk-` strings, `eyJ` strings and long base64-like strings. The scan passed; no file was removed.

## Not run

- Visible 429 retry: cannot be forced on a real provider (see above).
- Manual Terminal.app or iTerm2 run (S3, S6, S7, S11, resize): outside this task. The PTY used the pyte emulator, so it does not certify physical rendering or cursor placement; stale-row and cursor checks were made on pyte's cell grid only.
- xAI and alibaba-token-plan providers: no credentials in scope.

## Unresolved questions

1. Is `openai/gpt-5.5` meant to work with ChatGPT OAuth (F2), or should the selector hide or explain it?
2. Should the `echo` demo tool stay available to real providers (F4)?
3. What should happen to the refreshed OAuth home (caveat 1)?

Status: DONE_WITH_CONCERNS
Summary: All 9 real-provider scenarios pass; 4 product findings and an OAuth token-rotation caveat need an orchestrator decision.
Concerns: F1 and F2 are medium; the refreshed OAuth copy was kept, not deleted.
