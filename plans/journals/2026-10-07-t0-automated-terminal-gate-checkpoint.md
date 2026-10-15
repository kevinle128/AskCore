---
title: T0 automated terminal gate checkpoint
date: 2026-10-07
summary: Automated terminal evidence passes; iTerm2 and D14 remain pending.
---

# T0 automated terminal gate checkpoint

## Outcome

The isolated prototype passes 39 tests in the final clean race run, plus vet, compilation, formatting, and evidence validation.
Final source review and independent Phase 5 verification are pending at this checkpoint.
Automated G1/G2/G3/G6 results pass; iTerm2 3.7.3 observations remain pending because app access was denied.
The manifest keeps D14Ready false.
G4/G5 and optional probes are explicitly not run.

## Technical findings

Stock Println placement passed with zero kiln renderer fix classes.
The output owner required explicit WriteString observation preserving term.File, a complete-write frontier and failure latch, and owned mode 2026 reset after renderer shutdown.
Partial writes stop content without replay; permanent output failure reports terminal-byte restoration as unavailable.
Pinned x/vt truncates visible rows on height shrink, so that failure does not establish a renderer defect or prove native history reflow.
Termios is measured after tea.Run while the child lives; final ANSI state follows exit and drain.

## Scope and next evidence

All 27 protected product hashes remain unchanged, as verified by the controller.
No root dependency migration, fork, custom committer, roadmap decision, commit, or push was performed.
All owned children were waited; no known owned process remains.
The portable prototype archive and public manual checklist preserve the next steps.
Record the real iTerm2 checks before full T0 or D14 acceptance.
AgentWiki publish skipped.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
