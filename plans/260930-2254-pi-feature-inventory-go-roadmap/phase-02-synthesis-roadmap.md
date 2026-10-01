# Phase 2: Synthesis

Status: Done (2026-09-30)

## Inputs

The 10 reports from phase 1.

## Steps

1. Merge the inventory. One table: feature | category (harness, extension, TUI, provider, session, config, distribution, other) | Pi status (active, experimental, removed) | source | edge cases (link to lane C rows) | Go note.
   Split into files by category if one file goes over 800 lines.
2. Timeline: Pi development in stages, from the changelogs. Mark the lessons (features that were removed, APIs that broke).
3. Cross-check: every doc from lane A, every hook from lane E, every package from lane G is in the merged table. List gaps.
4. Go library choice: bubbletea version and companion libraries (from H; bubbletea itself is fixed by the user), and extension runtime (from I), each with the recommendation and the trade-off.
5. Roadmap: phases in order. Harness core first, then extension system, then TUI, then the rest. For each phase: goal, inventory items it delivers, AskCore packages it touches, exit criteria, edge cases to test.
6. Save the roadmap as `docs/` only if the user wants it as evergreen doc; else keep it in this plan folder as `roadmap.md`.

## Output

- `plans/260930-2254-pi-feature-inventory-go-roadmap/inventory*.md`
- `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`
- A short summary for the user in Vietnamese, with the decisions that need the user.

## Validation

- Acceptance criteria in `plan.md` all checked.
- Independent review of the roadmap against the inventory (one reviewer agent): no big feature is missing, the phase order has no dependency errors.
