# Greeter

Greeter answers a name over gRPC. It does not read or write SQLite. An empty name is answered as World.

## Sub-features

- `greet-named` returns `Hello, <name>!` for a non-empty name.
- `greet-empty` returns `Hello, World!` when name is empty.

## How to get to it (user POV)

- Unary RPC `proto.Greeter/SayHello` with `{"name":"<name>"}` on `$GRPC_ADDR`, plaintext.
- The same RPC with `{}` or `{"name":""}`.

`SayHelloStream` exists on the service and sends five greetings. This map does not treat the stream as a required proof. Driving it needs a stream client and is not a substitute for the unary calls above.

## Driving it with verify-ask-server

Preconditions:

- `doctor` printed `doctor ok` for this `RUN_ID`.
- `$EVIDENCE/launch.env` is sourced.
- `grpcurl` is on `PATH`. If it is missing, say this feature is unreachable and do not mark it verified. Do not replace it with the HTTP health route.

- **Named.** Run `mkdir -p "$EVIDENCE/greeter" && grpcurl -plaintext -import-path "$REPO/proto" -proto greeter.proto -d '{"name":"Ada"}' "$GRPC_ADDR" proto.Greeter/SayHello | tee "$EVIDENCE/greeter/named.body"`. The exit code is `0`. `named.body` contains `"message": "Hello, Ada!"`.
- **Empty.** Run `grpcurl -plaintext -import-path "$REPO/proto" -proto greeter.proto -d '{}' "$GRPC_ADDR" proto.Greeter/SayHello | tee "$EVIDENCE/greeter/empty.body"`. The exit code is `0`. `empty.body` contains `"message": "Hello, World!"`.
- **No row written.** Run `python3 -c 'import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); print(c.execute("select count(*) from users").fetchone()[0])' "$DATABASE_URL"`. The count is unchanged by these two RPCs.

## Gotchas

- The RPC is plaintext on the loopback port from `launch.env`. Do not pass `-insecure` to a TLS endpoint and do not use port 50051 unless doctor says this run bound it.
- `grpcurl` prints a space after the colon (`"message": "Hello, Ada!"`). Match that string, not the compact HTTP JSON form.
- The proto package is `proto`, so the method is `proto.Greeter/SayHello`. `Greeter/SayHello` alone fails.
- Streaming is not proof of the unary call. A stream that prints five lines does not cover `greet-empty`.
