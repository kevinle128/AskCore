# OPENAI_API_KEY ignored: diagnosis

Date: 2026-11-09. Base: origin/develop `6001287`.

## Result

The behavior is intended and documented. No code change.
The env key is ignored because `~/.ask/auth.json` holds a saved `openai` record
(method `openai-chatgpt`) with `refreshState.status = "pending"`. The record
has been in that state since a refresh of 2026-11-01 that did not commit.
The claim "auth.json has no credential for it" is not correct: the record
exists, it only waits for recovery.

## Evidence

- Real home: `ask settings show` gives `openai  none`;
  `ask -p --provider openai --model gpt-5.5` gives `unknown model "openai/gpt-5.5"`.
- Empty `ASK_HOME`, same environment: `openai  env:OPENAI_API_KEY`, and the
  live prompt replied `pong` (exit 0). The env path works.
- Synthetic home with only a fake `openai-chatgpt` record in `pending`:
  `settings show` gives `none`; `-p` gives
  `auth: refresh result uncertain; sign in again`.
  After `ask auth logout --provider openai` in that home: `env:OPENAI_API_KEY`
  and `pong`. The recovery path works.

## Cause chain

1. `internal/auth/service.go:233-234` (`selectSource`): a record with a method
   goes to `savedSource`. The env key (`service.go:239-245`) is read only when
   no saved record exists.
2. `internal/auth/service.go:287-289` (`savedSource`): any `RefreshState`
   gives `ErrRecovery`.
3. This is the documented design: `internal/auth/README.md:27` ("An uncertain
   saved refresh requires explicit recovery and cannot select a different
   billing route") and `:37` (env key "only when no saved record exists").
   `internal/settings/README.md:44-49`: only the matching `CommitRefresh`, a
   login, or a logout clears a pending fence; do not clear it by hand.
4. The `unknown model` text: `CurrentAccountKey`
   (`internal/auth/model_list.go:186-191`) uses the saved record also in
   recovery, so the cached `known` ChatGPT list (8 ids, fetched 2026-11-01)
   still filters the `openai` rows, and `NewModelSet`
   (`internal/providers/model_set.go:66-69`) drops `gpt-5.5`. The model
   resolution fails before the auth error can show.

## Recovery for the user (not run on the real home)

- `ask auth logout --provider openai`: then the env key applies.
- Or `ask auth login --provider openai --method openai-chatgpt`: keeps the
  subscription route.

## Proposed changes (not applied)

1. Message: `KeySources` (`internal/app/session_models.go:589-599`) maps
   `ErrRecovery` to `none`, which conflicts with its own comment ("none when
   no source has a key"). Propose `saved:openai-chatgpt` with Problem
   `refresh result uncertain; sign in again`.
2. Design question: an account in recovery could count as `unknown` for the
   model set, so the catalog rows stay and the user sees the "sign in again"
   error instead of `unknown model`.
3. Doc: `docs/cli.md:641` says "API key when no saved sign-in applies". Make
   explicit that a saved record that waits for recovery also blocks the env
   key, and name `ask auth logout` and `ask auth login` as the recovery.

## Unresolved questions

- Should the owner apply proposals 1-3? Proposal 2 changes model-set rules.
- Why did the 2026-11-01 refresh not commit? A crash between `FenceRefresh`
  and `CommitRefresh` leaves this state; nothing clears it automatically, by design.
