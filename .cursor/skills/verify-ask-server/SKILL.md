---
name: verify-ask-server
description: "Drive the AskCore daemon (ask-server) over HTTP and gRPC the way a caller does, on an isolated SQLite file and loopback ports. Use when proving users, posts, health, or the Greeter RPC. Do not use it for the agent harness library (pkg/protocol, internal/providers) until ask -p exists."
---

# Verify ask-server

AskCore's runnable user surface today is the daemon in `cmd/server`. It serves HTTP (Echo) and gRPC on loopback. The data a caller can change is users and posts in SQLite. Health and the Greeter RPC do not write rows.

This skill starts a private instance, drives it with `curl` (and `grpcurl` for gRPC), and keeps proof under `.cursor/skills/verify-ask-server/evidence/<RUN_ID>/`.

Other surfaces, not driven here:

- `cmd/tui` is a scaffold Bubble Tea menu (`AskCore TUI`, choices `Build project`, `Run tests`, `Deploy application`, `View logs`, `Exit`). It does not talk to the daemon.
- `ask -p` and the agent loop are not in this tree yet (roadmap H2). A green `go test ./pkg/protocol/... ./internal/providers/...` proves the harness library only. It is not a substitute for this skill when the change is an HTTP or gRPC behavior.

Two verification runs can run at once. Each `launch` picks its own HTTP port, gRPC port, and SQLite file. Never call `localhost:8080` or the developer's `app.db`.

## Launch

From the repo root:

```bash
.cursor/skills/verify-ask-server/scripts/verify-ask-server.sh launch
```

Optional second argument is a `RUN_ID`. When omitted, the script uses a timestamp.

Ready means `GET $BASE_URL/health` returns HTTP 200 and a JSON body containing `"status":"ok"` and `"message":"Server is running"`. The script prints `RUN_ID`, `BASE_URL`, `GRPC_ADDR`, `DATABASE_URL`, and `EVIDENCE`. The same values are in `$EVIDENCE/launch.env`. The process log is `$EVIDENCE/server.log`. A ready log line is zap field `msg` equal to `Starting HTTP server`.

The binary is `go build -o "$SCRATCH/ask-server" ./cmd/server`. The child environment is `HOST=127.0.0.1`, fresh `PORT` and `GRPC_PORT`, `DATABASE_URL` inside the scratch dir, `LOG_LEVEL=debug`, `CORS_ORIGIN=*`. Those values override `.env` because godotenv does not replace variables that are already set. The process is started in its own session (a double fork), so it keeps running after `launch` returns. `cleanup` is what stops it.

Teardown is `cleanup` below. Do not leave the process running after the proof is captured.

## Doctor

Run this before any drive, and again when a call looks wrong:

```bash
.cursor/skills/verify-ask-server/scripts/verify-ask-server.sh doctor "$RUN_ID"
```

It passes only when all of these are true:

- `$EVIDENCE/launch.env` exists and `HOST` is `127.0.0.1`
- `PID` is alive and its command is the scratch `ask-server` binary from this run
- that same pid owns `PORT` in `LISTEN`
- `GET $BASE_URL/health` body contains `"status":"ok"` and `"message":"Server is running"`
- `DATABASE_URL` is a file inside this run's scratch directory

A failure means this instance is not worth driving. Do not fall back to port 8080.

## Drive

Source `$EVIDENCE/launch.env` so `BASE_URL`, `GRPC_ADDR`, `DATABASE_URL`, and `EVIDENCE` are set. Read `.cursor/skills/verify-ask-server/features/README.md`, then the feature file for the behavior under test. Run the commands in that file literally.

HTTP calls are `curl` against `$BASE_URL`. Save response headers and body under `$EVIDENCE/<feature>/`. gRPC calls are `grpcurl -plaintext` against `$GRPC_ADDR` using `proto/greeter.proto`. There is no browser UI and no auth header.

Routes a caller can hit:

| Method | Path | Result |
|---|---|---|
| GET | `/` | 200, `{"message":"Welcome to AskCore!"}` |
| GET | `/health` | 200, `{"status":"ok","message":"Server is running"}` |
| GET | `/api/users` | 200, JSON array |
| POST | `/api/users` | 201, JSON user with `id`, `name`, `email` |
| GET | `/api/users/:id` | 200 user, or 404 `{"error":"User not found"}` |
| PUT | `/api/users/:id` | 200, omitted fields stay unchanged |
| DELETE | `/api/users/:id` | 204, empty body |
| GET | `/api/posts` | 200, JSON array; each post may include `author` |
| POST | `/api/posts` | 201, JSON post with `id`, `title`, `content`, `author_id`, `published` |
| GET | `/api/posts/:id` | 200 post, or 404 `{"error":"Post not found"}` |
| PUT | `/api/posts/:id` | 200; body fields `title`, `content`, `published` are optional |
| DELETE | `/api/posts/:id` | 204, empty body |
| gRPC | `proto.Greeter/SayHello` | `Hello, <name>!`, or `Hello, World!` when name is empty |

## Evidence

Write proof under `$EVIDENCE/<feature>/`. Keep the request, the response status and body, and a second observation of stored state.

Standards:

- Call the HTTP or gRPC route. Do not insert rows through GORM, `store` interfaces, or test helpers.
- Record the action and the resulting state. A 201 body is not enough for a create; follow it with GET and a read of the scratch SQLite file.
- Side effect for a user or post: `python3` opens `$DATABASE_URL` and prints the row. Soft-deleted rows have `deleted_at` set and must disappear from GET and from the default list.
- Greeter and health write no rows. Proof is the response body plus the doctor line. Do not claim a database change.
- No auth, no external network, and no Redis are required. Do not start Redis to make a proof pass.
- `LOG_LEVEL=debug` still serves real routes. It is not a dry-run.

## Cleanup

```bash
.cursor/skills/verify-ask-server/scripts/verify-ask-server.sh cleanup "$RUN_ID"
```

This sends `SIGTERM` to the pid in `$EVIDENCE/server.pid`, then `SIGKILL` if it is still alive, and deletes the scratch directory (binary and SQLite file). It does not delete `$EVIDENCE`. After cleanup, `launch.env`, `server.log`, and the feature files must still be there.

Never kill by process name. Never delete `app.db` in the repo root.

## Helpers

The only helper is `.cursor/skills/verify-ask-server/scripts/verify-ask-server.sh`. Invoke it as shown in Launch, Doctor, and Cleanup. `launch` prints the variables the feature files use. `doctor` prints `doctor ok` or exits non-zero. `cleanup` prints `cleaned RUN_ID=...`.
