# Docs rewrite report

Stateful record. Date 2026-10-13.

## Files changed

- Rewritten: `README.md`, `AGENTS.md`, `CONTRIBUTING.md` (new), `CHANGELOG.md` (new), `docs/README.md` (new), `docs/cli.md` (new).
- `CLAUDE.md` is now `@AGENTS.md`.
- Fixed: `internal/http/README.md` (`/v1/*` to `/api/*`, `/health`), `internal/README.md` (added `tuiclient`; WS marked planned).

## AGENTS/CLAUDE hunks

1. Testing line: CLAUDE (go-vcr) kept.
2. ACP and go-oidc lines: kept. Added fantasy and the patched copies in `third_party`. Added a note on stack items that the server does not wire.
3. Project tree: new rows from the audit. `sandbox`/`jobs` are implemented; `tuiclient`, `acp`, `leader`, `settings`, `pkg/askui`, `third_party`, `.claude/skills` added. `cmd/server` text trimmed to what exists.
4. e2e row: kept.
5. TUI extension section: kept; `tuiclient` added; `tuiext` and `askui` marked scaffold.
6. Test commands: union. `--driver t0|ask` replaced by `--backend` and `--case`. `ASK_RECORD` stays under test commands.
7. Command bullets: union. Long TUI sub-bullets moved to `docs/cli.md` and `internal/tui/README.md`; leader replacement facts kept; `echo` added; "the `ask` binary" paragraph added (`go run` makes `tui`).
8. Maintenance: name changed; changelog bullet added.
Dashboard, WS, and ACP-client-of-leader text is marked as not built. The recipes block is byte for byte identical (checked with diff).

## Left out (follow-ups)

- `ask leader status` exits 1 when no leader runs; its usage text says "0 stopped or nothing to do". Documented in `docs/cli.md`. Code not changed.
- Package READMEs in present tense for scaffold packages (hooks, mcp, channels, tracing, permissions, cron, bootstrap, skills) and `internal/gateway/README.md` WS/dashboard claims.
- 11 Go directories without README (`tuiclient`, `providers/*` adapters).
- `bts.lock.json` hashes of AGENTS.md and CLAUDE.md were already stale.
- `docs/adr/0003` needs a "not implemented" status. `docs/ask-architecture-reference.md` still names `ask-server` and WS.
- `.claude/skills/ask-docs-sync` is referenced in AGENTS.md; this task did not create it.
- No LICENSE; the README names none.
- `go install ./cmd/tui` gives a binary named `tui`, so the README uses `go build -o ask`. I did not run the build (a shell hook blocks it); the name rule comes from `internal/leader/spawn.go`.
- Changelog entry "clients retry a failed leader start" comes from the audit (plan), not a code read.

## Validation

- Relative links and anchors in all touched files resolve (script).
- `go test -count=1 -run '^TestReferenceDocs$' ./cmd/tui` passes.
- Line counts: AGENTS 185, cli 193, others below 160.
