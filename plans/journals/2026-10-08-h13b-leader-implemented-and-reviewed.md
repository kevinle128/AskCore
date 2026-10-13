---
title: H13b leader implemented and reviewed
date: 2026-10-08
summary: "Leader over the Unix socket, ask connect and ask version done; independent review found and fixed a session spoofing hole"
---

# H13b leader implemented and reviewed

## What happened
All six phases of the H13b plan are implemented: frames and handshake, the router, the shared host with the route context, spawn and version handling, `ask connect`, and the reverse-call rules. The suite for the whole repository passes, lint and vet are clean, and the leader tests also pass in a Linux container.

## What the independent review found
- A client could act on another client's session by adding a second spelling of `sessionId` (`sessionid`). Go decoders match keys without case and take the last one, so the router checked one session while the host used another. The same class reached `$/cancel_request` through a separate path. Fixed with a duplicate-key check at the router, the checked session in the route context, and a check in the host.
- A client queue was never closed when the handshake ended after the router stopped, so a stop could hang.
- The internal unfollow for an orphan follow had no client id, so the real host refused it and the error was dropped. The scripted agent now applies the host's route checks, so this class shows up in tests.
- Other fixes: detach scope for open questions, questions with no recipient, bounded client capabilities, an atomic idle check, terminal keys by session, and small leaks.

## Lessons
- A scripted agent that accepts anything hides bugs. Give it the checks of the real host.
- A rare test hang was a fixture race, found only because the Linux container run was repeated.
- macOS drops a SIGTERM sent to a stopped process, so a built-binary test of the signal path asserts the exit on Linux only.

## Next steps
The T1a terminal UI replaces `ask connect`. H13c adds the network gateway on top of the leader. Nothing is committed.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
