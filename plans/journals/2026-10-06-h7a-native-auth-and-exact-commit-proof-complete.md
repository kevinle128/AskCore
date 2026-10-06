---
title: H7a native auth and exact commit proof complete
date: 2026-10-06
summary: Three live provider routes and the exact ten-case filesystem race actor passed; all six phases and 22 criteria are complete.
---

# H7a native auth and exact commit proof complete

Native Anthropic, ChatGPT, and xAI login, canonical echo tool loops, and local logout passed through the ordinary command.
The accepted one-account and local-only logout scope is unchanged.

The exact signal proof required a real local file-sync barrier.
A test-only libfuse3 fixture blocks replacement fsync and holds the later directory-sync reply after real backing sync.
The separate observer reads owned backing bytes because Linux serialized a mounted temp-file read behind active fsync.
Product writes, locks, rename, and sync still use the real mount.
No production hook or auth bypass was added.

The isolated Linux environment first exceeded its 2 GiB memory limit during SDK compilation.
Build parallelism was limited, and the owned VM received 6 GiB.
A later repeat exhausted host disk space and damaged the owned build cache.
Space recovery, removal of that cache, and a cold rebuild resolved the failures.
The final race run passed all five cases twice in 38.186 seconds with exit code 0.
SIGINT, SIGTERM, and SIGHUP waited for durable replacement observation; budget expiry and SIGKILL retained the fence and prevented grant reuse.

Final tagged lint reported zero issues, and review closed the proof gap.
The plan CLI reports completed, 6/6 phases, 22/22 criteria, and 100%.
The controller removed the owned container, VM, cache, and test processes.
The original Podman default connection and user Docker processes are unchanged.

See [the completed plan](../261006-0157-h7a-subscription-auth/plan.md), [exact actor evidence](../reports/tester-261006-0855-h7a-fsync-actor.md), and [controller report](../reports/cook-261006-0849-h7a-implementation.md).
The owning roadmap H7a phase records completion; the whole parent roadmap and other phases remain outside this completion.
AgentWiki publish skipped.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
