---
name: rescout-before-advising
description: Consult prompts in this repo lag the disk; files change mid-consult. Re-read every file named in the prompt before advising.
metadata:
  type: feedback
---

Re-scout the disk before writing counsel; treat the caller's phase description as a hint, not as state.

**Why:** On 2026-10-05 the cassette consult said "phase 2 is next", but phase 2 (`cmd/tui/capture.go`, `options.transport`, `useCassette`, six cassettes) was already on disk, and `internal/providers/cassette/cassette.go` changed while the review was running (a `sent` field and a `capture` hook replaced `redact`).

**How to apply:** Read the named files last, right before the final message. State at the top which version was reviewed and what exists beyond the prompt's description. Run the offline test suite with `-race` to anchor the verdict in current state.
