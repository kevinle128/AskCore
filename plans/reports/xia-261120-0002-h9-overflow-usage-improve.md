# Xia (--improve): overflow detection and usage accounting for Ask (roadmap H9)

Date: 2026-11-20. Mode: `--improve`. This report records the source manifest, the decision matrix and the user decisions. The plan is written from it.

## 1. Source manifest

| Source | Path | Commit | License | Report |
|---|---|---|---|---|
| DeepSeek harness | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` | `ca33fd6843` | MIT | `researcher-261119-2337-deepseek-overflow-usage.md` |
| oh-my-pi (omp) | `/Users/dale/Desktop/workspace/opensources/oh-my-pi` | `eb6132a38b` | MIT | `researcher-261119-2337-omp-pi-overflow-usage.md` |
| Pi upstream | `/Users/dale/Desktop/workspace/opensources/pi` | `4c6fb7cfe` | MIT | same as omp |
| AskCore | branch `task/h9-plan` at `7f1b4a0` | | | `scout-261119-2337-h9-askcore-integration-surface.md` |

All three sources are MIT. Ask ports ideas and patterns, not files; regex lists copied from Pi need an attribution comment in the source file that holds them.

## 2. What Ask already has

- The failure code `CONTEXT_WINDOW_EXCEEDED` and a classifier (`internal/providers/failure.go:180-199`); the fantasy fork flags Anthropic and OpenAI overflow (`fantasykit/errors.go:103`). Nothing acts on it: it ends the cycle as an assistant error, and `ask -p` exits 1.
- Usage on every attempt, failed ones included, in `AttemptSettled.Usage` in the session log; ACP `_ask/session/usage` sums it from the log, so totals survive resume.
- Prices in micro-USD in the catalog, models.dev and `models.json`.

## 3. Gaps

1. Overflow detection misses wording that the fork does not flag, and misses silent overflow (a normal or length stop with input at or over the window).
2. Overflow has no distinct cause or user text.
3. Silent zero cost: `pickTier` returns a zero price when no tier matches (`internal/providers/cost.go:102-112`), and totals ignore `CostUnknown` (`internal/acp/ask_methods.go:499-523`).
4. No estimate of the next request's context size; the TUI shows the last reply's usage.
5. The TUI footer shows no session cost; `--mode json` has no totals.

## 4. Decision matrix (user approved 2026-11-20: 1A 2A 3A 4A)

| # | Decision | DeepSeek | omp / Pi | Ask (approved) |
|---|---|---|---|---|
| 1 | Action on overflow | 5 regexes + status order; one forced compaction, retry only if the context shrank | Pi: 25 patterns, rate-limit veto, silent overflow, compact + retry once. omp: 429/402 status overrules overflow text, 413 bytes vs tokens, larger model before compaction | **A.** H9: better detection (Pi patterns, omp 429/402 veto and 413 split, silent overflow), a distinct cause and clear user text, no retry. H10 adds one compact-and-retry that retries only on real shrink (DeepSeek). No model switch. |
| 2 | Unknown usage or price | Tokens only; a turn total hides when any attempt lacks usage | Priced totals | **A.** Show the known sum marked partial (for example `≥ $0.42`); fix the silent zero price. |
| 3 | Usage entry | Usage inside assistant and attempt events; totals recomputed; three folds that drift | Usage on messages; totals recomputed | **A.** No new entry. `AttemptSettled.Usage` is the single source of truth; one shared fold; message usage is display only (no double count). H-RETRY-10 is met by the existing entries plus tests. This changes the roadmap wording on purpose. |
| 4 | Context estimate | Last usage + chars/4 of later messages, only when model and tools are unchanged; UI meter uses a different number | Pi: same. omp: real tokenizer, 15% reserve | **A.** Last valid usage + chars/4 of later messages. One number for the UI and for the later compaction trigger. No tokenizer dependency. |
| 5 | Out of scope | | omp credential switch and quota wait | M2. |

## 5. Weaknesses in the sources that Ask avoids

- DeepSeek: one retried 429 hides the whole turn total; compaction and sub-agent usage not in the session total; three separate usage folds; overflow retry counter in memory only.
- omp: no visible limit on overflow retries; two totals with different scopes.
- Pi: no test that failed-attempt usage reaches totals.

## 6. Risk

Medium-low. The change is mostly classification, a fold and display. The risks are misclassification (a quota or rate-limit error read as overflow) and a cost total that looks complete when it is not; both get table tests.

## Unresolved questions

None for planning. The planner may raise smaller defaults (cause word, exit code, footer text) for approval.
