# Lessons

## 2026-10-05: A decision question must explain the domain first

- **What went wrong:** I asked the user to pick an option for "Responses wire layer (D22)". The question used internal labels (D22, `ExtraBody`, `encrypted_content`, `fc_` ids, stateless replay) and assumed the user knew how the OpenAI Responses API keeps reasoning between turns. The user could not answer.
- **Rule:** before the options, explain in plain words (1) what the API or feature does, (2) a concrete example conversation that shows the problem, (3) what breaks for the user if nothing is done. Only then cite decision ids and file lines. A question that needs the report to be understood is not self-contained.

## 2026-10-05: A test must run the real code path, never a test-only harness

- **What went wrong:** the user asked to test the Ask agent loop with "1+1*2" and mul/add tools. I wrote `internal/agent/live_math_test.go`, which built its own agent with test-only tools, hooks and a system prompt that Ask does not have, and set `MaxTokens: 8192`. It passed, but the real `ask -p "1+1*2" --model qwen3.7-max` would have failed: no add/mul tools, no hook mechanism, HTTP 400 on max_tokens. The test proved the harness, not the product.
- **Rule:** find the real entry point first (for Ask: `newHeadlessAgent` + `runHeadless`, what `ask -p` calls) and drive it; also run the real command. If the product lacks a piece the test needs, add it to the product or report it — never fake it in the test. Never override a production value just to make a test pass; that override is a product bug. When reporting, say which parts are real and which are test-only.
