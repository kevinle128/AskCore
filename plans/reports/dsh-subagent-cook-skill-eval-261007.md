# DSH sub-agent cook skill evaluation

## Outcome

Created the project-local skill at `.agents/skills/dsh-subagent-cook/` for a controller-only Claude implementation and Codex review cycle through provider-specific native DSH tools.

## Routing contract

Positive branches:

- explicit Claude implementation plus Codex review through DSH sub-agents;
- controller-only phased delivery;
- repair and independent re-review after findings;
- missing-provider recovery without runtime fallback.

Adjacent negatives:

- generic parallel research;
- DSH provider installation or profile repair;
- a single model-specific code review without an implementation cycle.

The live skill catalog discovered `dsh-subagent-cook` with the authored description after creation.

## Validation evidence

- `uv run scripts/quick_validate.py <skill-dir>`: pass.
- `uv run --with PyYAML==6.0.3 scripts/lint_cruft.py <skill-dir> --routing`: pass with high=0, medium=0, low=0.
- `python3 -m json.tool assets/eval-cases.json`: pass.

Snapshot hashes:

- `SKILL.md`: `6356fbd8a123f3e4c878bedd44afaec63f85d487687b216d302273570172a5b4`
- `references/cycle-prompts.md`: `070a86987395800ace21f488881e49972ab3a79484a16e28fe9f2971535191e3`
- `references/recovery.md`: `f7140efce93f4190df264410acc6ad7c10ade5d1b6549a4d9d5d6386409879c4`
- `assets/eval-cases.json`: `85b611c79a5d264581e5b213c0d08aa1fe78a46444b097a637652885211a9a24`

## Consumer evaluation gap

Two independent DSH generic sub-agent consumer attempts were made: one skill-aware and one no-skill baseline. Both failed before creating a child because the generic delegation surface tried to restrict unavailable `schedule_create`, `schedule_delete`, `schedule_list`, and `schedule_update` tools.

No consumer tokens or workspace writes resulted. This does not invalidate structural validation, but behavior comparison remains unverified until a session exposes the provider-specific `subagent_claude_code` and `subagent_codex` tools or the generic restriction mismatch is repaired.

The skill explicitly treats missing provider-specific tools as a stop condition and forbids fallback to generic sub-agents, workflow routing, Paseo, or direct CLI execution.
