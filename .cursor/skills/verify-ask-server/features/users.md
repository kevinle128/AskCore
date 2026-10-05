# Users

Users lets a caller create a person, read them back, change a name, and soft-delete them. The email is unique. A deleted user disappears from GET and from the list, and the scratch database keeps the row with `deleted_at` set.

## Sub-features

- `user-create` stores a name and email and returns an id.
- `user-list` returns every user that is not soft-deleted.
- `user-get` returns one user, or 404 when the id is missing or deleted.
- `user-update` changes only the fields present in the body.
- `user-delete` returns 204 and hides the user from later reads.

## How to get to it (user POV)

- `POST /api/users` with JSON `name` and `email`
- `GET /api/users`
- `GET /api/users/:id`
- `PUT /api/users/:id` with JSON `name` and/or `email`
- `DELETE /api/users/:id`

## Driving it with verify-ask-server

Preconditions:

- `doctor` printed `doctor ok` for this `RUN_ID`.
- `$EVIDENCE/launch.env` is sourced.
- No row in this scratch database uses email `verify-user-$RUN_ID@example.com`. A fresh launch satisfies this.

- **Create.** Send the user. Run `mkdir -p "$EVIDENCE/users" && curl -sS -D "$EVIDENCE/users/create.headers" -o "$EVIDENCE/users/create.body" -w "%{http_code}" -H "Content-Type: application/json" -d "{\"name\":\"Verify User\",\"email\":\"verify-user-$RUN_ID@example.com\"}" "$BASE_URL/api/users"`. The status is `201`. `create.body` contains `"name":"Verify User"` and `"email":"verify-user-$RUN_ID@example.com"` and a numeric `"id"`.
- **Read the id.** Set `USER_ID` from `create.body`. Run `USER_ID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$EVIDENCE/users/create.body")`.
- **Get.** Read that id. Run `curl -sS -D "$EVIDENCE/users/get.headers" -o "$EVIDENCE/users/get.body" -w "%{http_code}" "$BASE_URL/api/users/$USER_ID"`. The status is `200`. `get.body` repeats the same `id`, `name`, and `email`.
- **List.** Read the collection. Run `curl -sS -D "$EVIDENCE/users/list.headers" -o "$EVIDENCE/users/list.body" -w "%{http_code}" "$BASE_URL/api/users"`. The status is `200`. `list.body` contains `verify-user-$RUN_ID@example.com`.
- **Stored row.** Read SQLite without going through the HTTP handler. Run `python3 -c 'import sqlite3,sys; print(sqlite3.connect(sys.argv[1]).execute("select id,name,email,deleted_at from users where email=?", (sys.argv[2],)).fetchall())' "$DATABASE_URL" "verify-user-$RUN_ID@example.com"`. One row prints, `deleted_at` is `None`, and `id` equals `USER_ID`.
- **Update name only.** Run `curl -sS -D "$EVIDENCE/users/update.headers" -o "$EVIDENCE/users/update.body" -w "%{http_code}" -H "Content-Type: application/json" -d '{"name":"Verify User Renamed"}' -X PUT "$BASE_URL/api/users/$USER_ID"`. The status is `200`. `update.body` has `"name":"Verify User Renamed"` and the original email.
- **Delete.** Run `curl -sS -D "$EVIDENCE/users/delete.headers" -o "$EVIDENCE/users/delete.body" -w "%{http_code}" -X DELETE "$BASE_URL/api/users/$USER_ID"`. The status is `204`. `delete.body` is empty.
- **Hidden after delete.** Run `curl -sS -D "$EVIDENCE/users/get-deleted.headers" -o "$EVIDENCE/users/get-deleted.body" -w "%{http_code}" "$BASE_URL/api/users/$USER_ID"`. The status is `404`. `get-deleted.body` is `{"error":"User not found"}`. A second list, saved as `$EVIDENCE/users/list-after-delete.body`, does not contain `verify-user-$RUN_ID@example.com`. The SQLite query from **Stored row** now shows a non-null `deleted_at`.

## Gotchas

- A second POST with the same email returns 500 and a SQLite unique error in `{"error":...}`. That is not a 409.
- DELETE of an id that was never created still returns 204. Proof of delete is the following GET 404 plus `deleted_at` on the row that was created, not the 204 alone.
- PUT with an empty JSON object leaves `name` and `email` as they were. Assert both fields after the call.
- `GET /api/users/:id` with a non-numeric id returns 400 `{"error":"Invalid ID"}`.
- Emails are not trimmed by the handler. Assert the email you sent.
