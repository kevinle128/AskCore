---
name: h1-go-decision
description: H1 (messages/events/faux) got a GO on 2026-10-01; records the two lead decisions that depart from plan wording and the one latent trap left for H2 (RawEvent envelope write-back)
metadata:
  type: project
---

H1 was declared complete on 2026-10-01 (Kongming go/no-go: GO). Verified by overlay probes that
the M1 regression test (`faux.TestFuncStepDoesNotChangeLaterReplyChunksAndIds`, chunk assertion)
and the M2 tests (`protocol.TestIndentedUnknown*`) fail on the old code and pass on the tree.

Decisions kept against literal plan wording:
- Faux generated tool ids are `tool:<call>:<n>` (plan 5.2 item 5 said "counter ids like tool:1").
  Kept because plan 4.7 demands per-call allocation independent of scheduling; the per-call
  namespace is the direct realization. Only the researcher faux report mentions `tool:1`.
- `faux.Raw` passes script identity (api/provider/model) through; documented on `Raw`.

Latent trap handed to H2: `RawEvent` ignores `Envelope` mutations on encode (`pkg/protocol/codec.go`
`encodeRawEvent`, documented at `events.go` RawEvent doc). `Env().Seq = n` is a silent no-op for
that one Event implementor. Recommended fix before H2 emitter work: merge the five envelope keys
from the struct into Data on encode.

**Why:** H2 fills `seq`/`runId` through `Event.Env()`; a relay of unknown events would keep stale
envelopes on the wire.
**How to apply:** when consulted on H2 emitter/sequencing, check whether this was fixed before
assuming `Env()` writes reach the wire for every Event type. Related: [[h1-build-blocker-internal-logs]].
