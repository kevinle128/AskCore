# Inline resize plan review

Status: DONE_WITH_CONCERNS.
The plan is approved for isolated investigation and a bounded cause-aligned correction.
No user decision blocks this work.

The diagnosis places the defect before renderer erase, where stored relative cursor state differs from native reflow.
The plan keeps the fix at that owner, preserves one output path, and rejects model-only cursor changes.
It requires old-candidate failure, corrected-candidate success, both native engines, retained Go checks, and immutable product and archive hashes.
Local dependency provenance, licensing, pins, and patch records are required without changing the dependency cache.
Headless evidence does not complete iTerm2 or D14 acceptance.

Before implementation, name the measured renderer fix class in the ledger.
An origin reconciliation that enables correct full live redraw should be recorded as that bounded class.
It must not silently become a raw committer or a different renderer route.
The original five-class threshold and user fallback decision remain in force.

Name native mid-draft cursor and multiline hard-break checks in the validation set.
Include wide glyphs at widths 12 and 13, shrink and grow, selector resize, and resize while insertion is pending.
The existing eight E2E cases alone do not prove a general reflow algorithm because the main Unicode case uses an end-of-draft cursor.
These checks are discoverable implementation work, not a reason for another user approval pause.

This reviewer changed no implementation and started no terminal session.
