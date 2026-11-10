# Triage fixes compared with oh-my-pi and Grok CLI (2026-11-10)

Inputs: [triage summary](./triage-261110-1809-ocr-summary.md), [omp report](./researcher-261110-1819-omp-triage-issues.md) (`oh-my-pi` `efbc171`), [Grok report](./researcher-261110-1819-grok-triage-issues.md) (`grok-build` `2bdd1d6a`). Every claim about omp and Grok has a file:line citation in those reports.

| # | Ask defect | omp | Grok CLI | Recommended fix for Ask |
|---|---|---|---|---|
| R1 | `Continue` takes the run slot, returns early, strands queued input | Takes the slot, then runs queued steer/follow-up instead of failing; puts back undelivered input; drains again after each run (an early throw once caused an OOM loop) | Checks "running or empty" under the lock, checks again before pop; late steer moves to the front of the queue at turn end | Follow omp: when there is nothing to continue but input is queued, run that input; on every early return set `wake` so `end` starts the next run. Race test first |
| R2 | Project `AGENTS.md` symlink to a file outside the repo is sent with no trust | Follows symlinks, no containment, no trust gate | Follows symlinks, but drops all project context files in an untrusted folder | Keep Ask's rule that context files need no trust (H6a); add a containment check: a project context file whose target is outside the git root (or cwd) is skipped with a reason. In-repo symlinks keep working |
| R3 | `read` loads the whole file into the leader | Whole read only up to 4 MiB; above that, windowed read with an 8 KiB buffer; binary sniff first | Reads the whole file (same gap); has an unused bounded-read helper | Follow omp: whole read up to a cap (4 MiB), windowed line read above it, binary sniff before decode |
| R4 | No case-fold duplicate check on top-level JSON keys | No check (JS keys are case-sensitive) | No check | Add `hasFoldDuplicate` on top-level keys; the risk is specific to Go's case-insensitive decoding |
| R5 | Comment says "not writable by others", mask is `0002` | Checks only the executable bit | No check on the binary | Fix the comment; keep the mask (Ask already checks more than both) |
| R6 | models.json `Authorization` header replaces the key on Completions only; Responses filters it | User `Authorization` wins on purpose, same on all OpenAI paths | User header wins for a static key (documented as proxy auth); a refreshing credential replaces it on each request | Follow Grok: a user credential header wins over a static API key (file, env, `--api-key`); a saved OAuth credential wins over the header. One rule for Completions and Responses; document it |
| R7 | `edit` race with another writer between read and rename | No lock or re-stat; detects change by content tags; compares after write | No check for ordinary files; memory files use a hash taken at read and an atomic write | Stat after read and again before the rename; fail when mtime or size changed (Ask already writes atomically) |
| R8 | Typed-nil message other than `*CustomMessage` passes the notice check (latent) | Checks only at the RPC edge | No typed nil in Rust; checks empty text where a notice is made | One generic nil/empty check at the single injection point + test |
| R9/R10 | In-place handler edits of `Context.Messages` / `AfterTool` result are lost (latent) | Handlers get a `structuredClone`; the clone is returned, so in-place edits count | Hooks return replacement values; nothing is edited in place | Copy before dispatch and compare against the original copy (omp shape) |
| R11 | `ASK_CAPTURE` dir 0755, cassette 0644 | Umask defaults for logs and dumps | Session dirs owner-only; lock/profile files 0600 | Follow Grok: dir 0700, cassette 0600 |
| Q-write | Should `write` refuse to overwrite a file changed since the session read it? | No guard | No guard for ordinary files | Keep the current contract (no guard), as Pi, omp and Grok do |

## Decisions that change because of this research

- R6: the triage proposal was to filter the header. omp and Grok both let the user header win. The recommendation is now the Grok rule (header wins over a static key, OAuth wins over the header), applied to both paths.
- Q-write: no reference harness has the guard; keep the current behavior.
- R2: Grok's answer is a trust gate, but Ask decided in H6a that context files need no trust, so containment is the smaller change that keeps that decision.

## Open questions

1. R6: accept the Grok rule?
2. R2: containment check (in-repo symlinks load) or block every symlink?
3. R3: cap value 4 MiB (omp) or another value?

## User decisions (2026-11-10)

- R6: Grok rule. A user credential header wins over a static API key; a saved OAuth credential wins over the header; one rule for Completions and Responses; documented.
- R2: skip only targets outside the repo (git root, or cwd without one); in-repo symlinks load.
- R3: 4 MiB whole-read cap, windowed read above it.
- Next: triage the 117 Medium and Low findings first, then one fix plan.

## Round 2: 35 Medium/Low defects (N1-N35)

Inputs: [list](./triage-261110-1832-round2-real-list.md) built from the five `triage-261110-1826-m*.md` tables; [omp report](./researcher-261110-1832-omp-35-defects.md); [Grok report](./researcher-261110-1832-grok-35-defects.md).

### Items with a design choice

| ID | Ask defect | omp | Grok | Recommendation |
|---|---|---|---|---|
| N1 (Medium) | An empty reply (start events only) counts as success | Non-empty only with an image, a tool call or non-whitespace text; empty is retried | Empty is judged on the final response; a content-filter stop is exempt | Count only non-whitespace text, a complete tool call or an image; exempt a content-filter stop (both rules) |
| N3/N4 | Base URL with a trailing `/` fails the Completions and Responses guards | Trims trailing slashes in both paths | Trims once at client construction | Trim trailing `/` once where the row is built, for both paths |
| N5 | `RuneCount/4` undercounts CJK for `max_tokens` | UTF-8 bytes, plus 10% and 64 tokens | Bytes / 4 | omp rule: bytes based with the 10% + 64 pad |
| N9 | Partial JSON skips the colon unchecked | Checks the colon; stops at the last good key when streaming, errors when final | Does not repair; replays `{}` and keeps the raw text | omp rule: check the colon; keep the existing repair path |
| N10/N11 | Same model id on another provider keeps the item id; empty reasoning signature accepted | Compares provider, api and id; rejects a signature without type and `id` | No multi-provider replay | omp rule for both |
| N12 | Duplicate JSON-RPC ids release the shutdown barrier early | Same flaw (ids in a Set) | Not in the repo | Count per id (`map[string]int`); no reference, the fix is local |
| N21 | `Logout` of an unknown method sends a removal notice | Same flaw | Idempotent, no check | Reject an unknown method; no reference, the fix is local and small |
| N25 | Image decoded with no pixel cap | 20 MiB and `maxPixels` on terminal paths, not on the model path | 16 M pixels after a header probe | `image.DecodeConfig` first, refuse above 16 M pixels on every path |
| N26 | Integer strings pass through float64 | Same flaw (`Number()`) | Parses as integer first | Grok rule: parse integer exactly first |
| N27 | Truncation label inferred | Records the limit that stopped output | Sets the reason at the cap site | Record the limit in `sandbox.Truncation` (both) |
| N29/N34 | `ask connect` ignores signals during setup; exits 0 when the leader closes with a prompt in flight | 130/143/129 handlers; drains then exits; client rejects pending on close | Handlers before `initialize`; 130/143/129 | Install the signal context before setup; exit 1 on a lost leader with a prompt in flight |

### Internal items (no design choice)

N2, N6, N7, N8, N13-N20, N22-N24, N28, N30-N33, N35: local fixes as the triage tables give them. The references have no equivalent (Rust has no unclosed body or typed nil) or the same flaw (omp N18: no disposed check). N20 matches omp: check disposed before allocating an id.

### Totals after both rounds

46 real defects: 4 Medium (R1, R2, R3, N1), 42 Low. 0 High or Critical.
