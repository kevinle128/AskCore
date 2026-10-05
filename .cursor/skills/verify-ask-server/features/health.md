# Health

Health tells a caller that this daemon process is serving HTTP, and the root route identifies the process as AskCore. Neither route reads or writes the database.

## Sub-features

- `health-ok` returns the running status.
- `root-welcome` returns the AskCore welcome message.

## How to get to it (user POV)

- `GET /health`
- `GET /`

## Driving it with verify-ask-server

Preconditions:

- `doctor` printed `doctor ok` for this `RUN_ID`.
- `$EVIDENCE/launch.env` is sourced.

- **Health.** Request the status. Run `mkdir -p "$EVIDENCE/health" && curl -sS -D "$EVIDENCE/health/health.headers" -o "$EVIDENCE/health/health.body" -w "%{http_code}" "$BASE_URL/health"`. The status is `200`. `health.body` is `{"message":"Server is running","status":"ok"}` with those two fields, in either order.
- **Welcome.** Request the root. Run `curl -sS -D "$EVIDENCE/health/root.headers" -o "$EVIDENCE/health/root.body" -w "%{http_code}" "$BASE_URL/"`. The status is `200`. `root.body` is `{"message":"Welcome to AskCore!"}`.
- **No row written.** Confirm the scratch database gained no user. Run `python3 -c 'import os,sqlite3,sys; c=sqlite3.connect(sys.argv[1]); print(c.execute("select count(*) from users").fetchone()[0])' "$DATABASE_URL"`. The printed count is unchanged from before these two GETs (on a fresh launch it is `0`).

## Gotchas

- `/health` stays `ok` when the SQLite file is empty. It does not prove that users or posts work.
- A process on port 8080 can also answer `/health`. Doctor must match the pid from this run before the body counts as proof.
- Field order in the JSON object is not significant. Both required strings must be present.
