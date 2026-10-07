# Inline replay plan review

Status: DONE

The updated plan is ready for implementation.
The user accepted resize-only screen and scrollback purge followed by transcript replay after 120 ms.
This replaces the earlier resize no-purge and no-replay requirements.
The plan states the loss of terminal output from before startup and retains the full bounded T0 transcript without the Grok 4000-row limit.

The application transcript is the source of truth.
One renderer owner must serialize ordinary commits, purge, transcript replay, and live-frame output.
Replay must not advance the commit frontier or emit a commit acknowledgement.
The implementation must take a coherent replay snapshot after any in-flight write completes.
Confirmed entries in that snapshot must appear once in the replay generation.
Pending entries outside the snapshot must retain their normal commit path and acknowledgement.

Generation-tagged debounce must reject stale timers and use the latest width and height, including height-only changes.
The final frame must use the same adopted size as the replay.
A failed or short write during purge or replay must remain a failed generation and follow the existing output-error stop and terminal-reset path.
Do not advance replay success state after an incomplete write.

The output oracle must divide ordinary commits from resize replay generations using observed output boundaries.
It must check every expected entry, order, uniqueness within each generation, and the complete 2000-marker case.
Final screen and history assertions must also reject duplicate live frames and verify cursor, Unicode cells, selector state, and continued input.
The accepted native matrix, independent verification, immutable old candidate, dependency provenance, and root hash boundaries remain in scope.

The Grok source report supplies a design reference, not runtime proof for this candidate.
No implementation or runtime test was performed by this reviewer.
Real iTerm2 and D14 acceptance remain pending.
