---
title: H13b leader repair gates passed
date: 2026-10-09
summary: "Fixed driver commit, Follow order, shutdown, idle admission, and local leader safety."
---

# H13b leader repair gates passed

# Repair record

The independent review found eight runtime, safety, and test defects after H13b implementation.
The maintainer approved their repair.

A session commit lock now orders driver changes against mutations at their actual commit boundary.
The line client registers Follow ownership on its socket reader.
The forced signal path no longer joins an active runtime drain.
Auth cleanup starts in parallel with that drain.

Directory descriptors protect local leader artifacts.
Linux PID fallback uses a stable kernel handle and checks the lock owner again.
macOS refuses that fallback when it cannot establish a stable target.

The real-host tests now join the large-frame and question/run lifecycle proofs.
All six original regression checks, the repository suite, the selected race gate, build, vet, lint, and Linux leader tests passed.
Two full-suite retries exposed an ENOENT wrapper regression and a test startup timing assumption; both have regression checks.

See [the repair verification](../reports/pm-261009-h13b-repairs.md) for evidence and the accepted manual gaps.
No commit or publication was requested.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
