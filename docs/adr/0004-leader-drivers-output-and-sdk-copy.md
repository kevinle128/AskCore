---
status: accepted
---

# Leader drivers, output queues and the ACP SDK copy

The local leader (`ask leader`, phase H13b) shares one agent host between many clients. These decisions are durable. The user decided the take rule, the output queues, the version gate and the proof of the other-user rule on 2026-10-08. The host-owned generation and the SDK copy are design choices that follow from them.

**The host owns the driver of a session.** A session has one driver and any number of observers. The host holds the driver generation and is the only place that changes it. Every change of a session carries the generation, and an old generation is refused under the same lock as the change. The router copies the generation that the host returns and never invents one.

**A take works when no driver is live, or when the session is idle.** The creator of a session drives it. Another client takes it with `_ask/session/take`. If the driver is gone, the take works even while a run is active, because otherwise a stuck run could be stopped only by stopping the whole leader. If a live driver runs a busy session, the take fails with `busy`. There is no automatic promotion of an observer. An observer can read, follow and answer shared questions, but cannot change the session.

**Each client has an unbounded output queue.** As in Grok, a client that does not read keeps its place and later gets every frame in order. No other client is slowed and no client is closed because it is slow. The accepted cost is that the memory of the leader grows while a connected client does not read. A client is closed alone when its write fails or a frame is over 64 MiB. The leader also sets no limit for clients, sessions or pending requests; a session is never evicted, because the agent has no way to close one before the durable session work (H8).

**The version gate is the outer protocol number.** A different build prints a hint and shows in `ask leader status`. A leader that a client started is replaced by a newer client only when it reports idle and agrees to stop in one step. A busy, supervised or newer leader stays. The control frames `status` and `shutdown` must stay the same in every protocol version.

**Proof of the "other user" rule.** The leader reads the user of the peer from the kernel and compares it to the owner. The test injects a different owner, so the real system call runs and the mismatch is real. A manual check as root is documented. There is no automated test with a second real user.

**The repository holds a copy of the ACP SDK.** The leader carries messages of up to 64 MiB, and the upstream SDK stops a line at 10 MiB. `third_party/acp-go-sdk` is upstream v0.13.5 with that one limit raised, selected by a relative `replace`. `third_party/acp-go-sdk/PATCH.txt` records the source, the licence, the change and the update steps. A test fails if the patch is lost. Drop the copy when upstream makes the limit configurable.

Consequences: `ask acp` over stdio keeps its stricter rule that a prompt result waits for the write, and its 8 MiB input guard. The line client `ask connect` is a stand-in for the TUI of phase T1a and never falls back to an agent of its own. Evidence and the full plan: [plans/261008-1033-h13b-leader-unix-socket](../../plans/261008-1033-h13b-leader-unix-socket/plan.md).
