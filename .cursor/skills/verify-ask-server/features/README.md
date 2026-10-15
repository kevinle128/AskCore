# ask-server verification map

This directory is the maintained source for verifying the caller-facing behavior of the AskCore daemon. Read this index, then use the matching feature file as the recipe.

## Baseline preconditions

- Launch with `.cursor/skills/verify-ask-server/scripts/verify-ask-server.sh launch`.
- `doctor` must print `doctor ok` for that `RUN_ID` before any recipe.
- The instance listens on `127.0.0.1` with its own `PORT`, `GRPC_PORT`, and SQLite file. Concurrent runs do not share them.
- Source `$EVIDENCE/launch.env` before `curl` or `grpcurl`.
- Never drive an instance this run did not start. Port 8080 and `app.db` are out of bounds.

## Driving conventions

- Start every recipe from the baseline of a fresh launch unless its preconditions say otherwise.
- HTTP proof is `curl` against `$BASE_URL`. gRPC proof is `grpcurl -plaintext` against `$GRPC_ADDR`.
- Treat every command as literal. Keep paths, JSON fields, and status codes unchanged.
- Save headers and bodies under `$EVIDENCE/<feature>/`.
- Restore nothing in the shared developer database. This run's scratch database is deleted by `cleanup`. Do not remove proof artifacts during cleanup.

## Proof and skip reporting

- Capture the request and the resulting state, not only the last status code.
- Mutation proof includes the response body, a later GET of the same id, and a Python read of `$DATABASE_URL`.
- Record the feature id and the entry point (method and path, or RPC name) next to every artifact.
- Report an unreachable path with the command that failed and the unmet precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the caller-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every caller entry point.
3. `Driving it with verify-ask-server` starts with `Preconditions:` and uses labeled bullets that pair each action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

## Features

- [Health](./health.md) covers `GET /health` and `GET /`.
- [Users](./users.md) covers create, list, get, update, delete, and the scratch-database row.
- [Posts](./posts.md) covers create, list, get, update, and delete for a post that names an author.
- [Greeter](./greeter.md) covers the unary `SayHello` RPC, including an empty name.
