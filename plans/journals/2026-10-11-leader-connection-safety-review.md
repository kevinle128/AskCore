---
title: Leader connection safety review
date: 2026-10-11
summary: Checked the leader port requirements and fixed four connection and signal safety defects.
---

# Leader connection safety review

## What happened

Reviewed all phases of the leader connect-or-spawn plan against source and fresh tests.
Four built-binary E2E tests exposed read-only replacement, concurrent replacement, expired deadline and cancelled eviction defects.
The repairs passed macOS and Linux checks; the requirement report records the commands and evidence limits.

## Decision

Keep the accepted replacement and platform rules.
Use the original registered instance for the replacement boundary, and respect both cancellation and the wall-clock deadline before signals.

## Next steps

No open repair remains from this review.
See plans/reports/review-261012-0120-leader-connect-or-spawn-port.md.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
