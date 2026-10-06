# Posts

Posts lets a caller save a title and content for an author, read the post back with that author, mark it published, and soft-delete it. A new post is not published.

## Sub-features

- `post-create` stores title, content, and author id. `published` is false.
- `post-get` returns the post and a nested `author`.
- `post-list` returns posts that are not soft-deleted, each with `author`.
- `post-publish` sets `published` without clearing the title.
- `post-delete` returns 204 and hides the post from later reads.

## How to get to it (user POV)

- `POST /api/posts` with JSON `title`, `content`, and `author_id`
- `GET /api/posts`
- `GET /api/posts/:id`
- `PUT /api/posts/:id` with any of `title`, `content`, `published`
- `DELETE /api/posts/:id`

## Driving it with verify-ask-server

Preconditions:

- `doctor` printed `doctor ok` for this `RUN_ID`.
- `$EVIDENCE/launch.env` is sourced.
- A user exists. Create one with the Users recipe, or run `curl -sS -H "Content-Type: application/json" -d "{\"name\":\"Post Author\",\"email\":\"post-author-$RUN_ID@example.com\"}" "$BASE_URL/api/users"` and set `AUTHOR_ID` from the `id` field. The status of that call is `201`.

- **Create.** Send the post. Run `mkdir -p "$EVIDENCE/posts" && curl -sS -D "$EVIDENCE/posts/create.headers" -o "$EVIDENCE/posts/create.body" -w "%{http_code}" -H "Content-Type: application/json" -d "{\"title\":\"Verify Post\",\"content\":\"Stored by verification\",\"author_id\":$AUTHOR_ID}" "$BASE_URL/api/posts"`. The status is `201`. `create.body` contains `"title":"Verify Post"`, `"content":"Stored by verification"`, `"published":false`, and `"author_id":` equal to `AUTHOR_ID`.
- **Read the id.** Run `POST_ID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$EVIDENCE/posts/create.body")`.
- **Get.** Run `curl -sS -D "$EVIDENCE/posts/get.headers" -o "$EVIDENCE/posts/get.body" -w "%{http_code}" "$BASE_URL/api/posts/$POST_ID"`. The status is `200`. `get.body` includes `"author"` with `"email":"post-author-$RUN_ID@example.com"`.
- **Stored row.** Run `python3 -c 'import sqlite3,sys; print(sqlite3.connect(sys.argv[1]).execute("select id,title,published,author_id,deleted_at from posts where id=?", (sys.argv[2],)).fetchall())' "$DATABASE_URL" "$POST_ID"`. One row prints, `published` is `0`, `deleted_at` is `None`, and `author_id` equals `AUTHOR_ID`.
- **Publish.** Run `curl -sS -D "$EVIDENCE/posts/update.headers" -o "$EVIDENCE/posts/update.body" -w "%{http_code}" -H "Content-Type: application/json" -d '{"published":true}' -X PUT "$BASE_URL/api/posts/$POST_ID"`. The status is `200`. `update.body` has `"published":true` and `"title":"Verify Post"`.
- **List.** Run `curl -sS -o "$EVIDENCE/posts/list.body" -w "%{http_code}" "$BASE_URL/api/posts"`. The status is `200`. `list.body` contains `Verify Post`.
- **Delete.** Run `curl -sS -D "$EVIDENCE/posts/delete.headers" -o "$EVIDENCE/posts/delete.body" -w "%{http_code}" -X DELETE "$BASE_URL/api/posts/$POST_ID"`. The status is `204`. A following GET of `$POST_ID` returns `404` and `{"error":"Post not found"}`. The SQLite query shows a non-null `deleted_at`.

## Gotchas

- `Open` does not set `PRAGMA foreign_keys`. A post whose `author_id` matches no user can still return 201. Proof that the author is real is the nested `author` on GET, which is empty of a user when the id does not exist. Create the author first when the recipe needs `author.email`.
- A new post is unpublished even when the body omits `published`. Assert `"published":false` on the 201 body.
- PUT that sends only `published` must leave `title` and `content` in place.
- DELETE of an unknown id still returns 204. The following GET 404 is the proof that a created post is gone from the API.
- `author_id` is required by the table. Omitting it makes POST return 500, not 400.
