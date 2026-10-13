# Lessons

## 2026-10-05: A decision question must explain the domain first

- **What went wrong:** I asked the user to pick an option for "Responses wire layer (D22)". The question used internal labels (D22, `ExtraBody`, `encrypted_content`, `fc_` ids, stateless replay) and assumed the user knew how the OpenAI Responses API keeps reasoning between turns. The user could not answer.
- **Rule:** before the options, explain in plain words (1) what the API or feature does, (2) a concrete example conversation that shows the problem, (3) what breaks for the user if nothing is done. Only then cite decision ids and file lines. A question that needs the report to be understood is not self-contained.

## 2026-10-05: A test must run the real code path, never a test-only harness

- **What went wrong:** the user asked to test the Ask agent loop with "1+1*2" and mul/add tools. I wrote `internal/agent/live_math_test.go`, which built its own agent with test-only tools, hooks and a system prompt that Ask does not have, and set `MaxTokens: 8192`. It passed, but the real `ask -p "1+1*2" --model qwen3.7-max` would have failed: no add/mul tools, no hook mechanism, HTTP 400 on max_tokens. The test proved the harness, not the product.
- **Rule:** find the real entry point first (for Ask: `newHeadlessAgent` + `runHeadless`, what `ask -p` calls) and drive it; also run the real command. If the product lacks a piece the test needs, add it to the product or report it — never fake it in the test. Never override a production value just to make a test pass; that override is a product bug. When reporting, say which parts are real and which are test-only.

## 2026-10-06: Check the plan status before asking about sequencing

- **What went wrong:** I asked the user whether to do the lifecycle redesign or H7a first. H7a was already complete (`plans/261006-0157-h7a-subscription-auth/plan.md` status `completed`, commit `e991b5c`), and a subagent had already reported that the auth work was committed. I passed on the subagent's question without checking.
- **Rule:** before asking a sequencing or "is X done" question, read the other plan's `status`, its phase checkboxes and `git log`. Do not relay a subagent's open question until it survives that check.

## 2026-10-06: A red-team fix must be checked against recorded user decisions

- **What went wrong:** I accepted red-team finding #15 ("bound the abort drain, then outcome unknown"). The planner turned it into a 5 s bound that abandons a running tool body. D19 (revised by the user the same day) requires started bodies to drain before the Step closes, as DeepSeek does (`tool-calls.ts:233`, `Promise.allSettled`). I did not trace the fix to D19, so a hardening suggestion silently reversed a user decision. A later source audit found it.
- **Rule:** before accepting any red-team fix, grep the roadmap decisions (D-rows) and the plan's fixed constraints for the behavior it changes. If it touches a user decision, present it as a question, not as an accepted finding. Never write "Remaining contradictions: none" without a second pass that compares each phase task against each decision row.

## 2026-10-10: When the user's test contradicts your code reading, find the path you missed

- **What went wrong:** the user asked how Pi and Grok CLI treat slash command case. I read one lookup (`text === "/settings"`, an exact-case `HashMap`) and said both are case-sensitive. The user ran `/MEMORY` and `/MODEL` and both worked; I was corrected three times. The missing paths: Pi applies the highlighted completion on Enter before it submits; Grok passes an unknown command to the agent shell, which lowercases it (`slash_key`). I also gave a wrong mechanism for Grok before checking its smart-case menu.
- **Rule:** a report of observed behavior beats a code reading. Trace the whole path from key press to execution (menu, accept, pass-through, fallback) before stating a conclusion, say which parts were read and which were run, and when the user contradicts you, find the missing path first instead of restating or defending the first answer.


## 2026-10-11: TUI test guide sent the user to `go run ./cmd/tui`
- Pattern: I gave run instructions from CLAUDE.md without checking how the leader starts. The client spawns the leader only from a binary named `ask` next to the caller or on PATH (`internal/leader/spawn.go:156,161,224`). `go run` makes a temp binary named `tui`, so the TUI falls back to in-process and `/providers` is refused.
- Rule: before telling the user how to run a flow, trace its startup path (binary name, PATH, home dir) and try it once in a temp `ASK_HOME`. Tests that pass with the compiled `ask` do not prove the `go run` path.
