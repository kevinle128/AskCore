# TUI execution allocation

Status: DONE

The next path is T0 → H13a ACP → H13b Leader → T1a.
Revision 16 now gives each remaining M1 backend feature a T1b exit through the real Leader ACP connection.
H13a/b use the completed H4, H7a and lifecycle work; storage, compaction, monitoring and network transport join at their owning exits.

Verification: H7a execution record is complete at 6/6; lifecycle is complete at 12/12.
The sibling H4 verification plan records DONE_WITH_CONCERNS with live vendor limits.
Relative Markdown file links pass, git diff --check passes, and roadmap.md has 799 lines.
No code, dependencies, phase status, commits or processes changed.

## Unresolved questions

D14 remains the T0 terminal gate decision; D17 remains the H13a ACP conformance decision.
Live-provider access limits remain acceptance evidence limits, not missing backend implementation.
