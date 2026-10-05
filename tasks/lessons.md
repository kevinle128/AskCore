# Lessons

## 2026-10-05: A decision question must explain the domain first

- **What went wrong:** I asked the user to pick an option for "Responses wire layer (D22)". The question used internal labels (D22, `ExtraBody`, `encrypted_content`, `fc_` ids, stateless replay) and assumed the user knew how the OpenAI Responses API keeps reasoning between turns. The user could not answer.
- **Rule:** before the options, explain in plain words (1) what the API or feature does, (2) a concrete example conversation that shows the problem, (3) what breaks for the user if nothing is done. Only then cite decision ids and file lines. A question that needs the report to be understood is not self-contained.
