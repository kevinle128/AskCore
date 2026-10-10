---
phase: 1
title: "TUI design"
status: completed
priority: P1
effort: "done"
dependencies: []
---

# Phase 1: TUI design

## Goal

An approved UI contract for `/providers`, `/model` and the first run, made with the `ask-tui-design` skill before any Go code.

## Result

Completed. Two approvals, both by Kevin Le (user):

1. **First approval, 2026-10-10:** states P1 to P31, criteria 1-50.
2. **Re-approval, 2026-10-10:** after decisions D11 (Claude Pro/Max address or code), D12 (default model), D14 (`TEA_TRACE` warning) and D15 (removal wins over another sign-in) the design went back to `draft`, was amended, and was approved again (`design.md:3-5`; section "Changes since the first approval", `design.md:15-27`). New or redrawn states: P6a, P12c, P15, P15a, P16, P25a, P28b, P28c, P29. Final criteria: 1-58, with the `TEA_TRACE` exception stated in the security criteria.
3. **Third approval, 2026-10-10:** after decision D16 (Alibaba Token Plan key saved without a check: states P6b, P9a, P9b; criteria 59 and 60). `design.md:3-5` reads `approved`, Kevin Le (user), 2026-10-10.

| Evidence | Where |
|---|---|
| Approval fields | `plans/261010-1424-t1c-provider-setup-design/design.md:3-5`: `design-status: approved`, `design-approved-by: "Kevin Le (user)"`, `design-approved-on: "2026-10-10"` |
| States | P1 to P31 plus P6a, P12c, P15a, P25a, P28b, P28c (section 4 sketches, section 5 tables) |
| Keys, widths, text | Sections 6, 7, 8 |
| Acceptance | Section 10: criteria 1-35, security 36-43 (hold when `TEA_TRACE` is not set), model line and default model 44-58, Token Plan key without a check 59-60 |
| Leader needs | Section 3, "What the TUI needs from the leader" (`design.md:134-149`) |
| Amendments to the T1a design | Section 12 (`design.md:1747-1787`) |
| Visual preview | `artifacts/preview.html`, built by `artifacts/make-design.py` (widths checked by `artifacts/verify-widths.py`) |

## Re-approval

- [x] Design amended for D11, D12, D14, D15 (another agent)
- [x] Design approved again (2026-10-10)
- [x] Criterion map synced to criteria 1-58 in [plan.md](plan.md#criterion-to-test-map) (2026-10-10)

## Rules for later phases

- The design folder is read only for this plan.
- A visible change that differs from the design (text, layout, keys) sends the design back to `draft` and needs a new user approval before the golden that shows it is accepted.
  The D11, D12, D14 and D15 texts are now part of the approved design.

## Todo

- [x] Design drafted with sketches at 80×24, 40×14 and 120×40
- [x] Open questions decided by the user (design section 11)
- [x] Design approved (2026-10-10)
- [x] Design approved again after the D11, D12, D14, D15 amendments (2026-10-10)
- [x] D16 variant added to the design (P6b, P9a, P9b; criteria 59, 60), 2026-10-10
