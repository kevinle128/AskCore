# AskCore

This project was created with [Better Fullstack](https://github.com/Marve10s/Better-Fullstack), a high-performance Go stack.

## Features

- **Go** - Fast, reliable, and efficient programming language
- **Echo** - High performance, minimalist Go web framework
- **GORM** - Full-featured ORM for Go
- **gRPC** - High-performance RPC framework
- **Bubble Tea** - Terminal UI framework
- **Zap** - Blazing fast, structured logging
- **Testing** - gomock, testcontainers, testify
- **Messaging** - asynq
- **Observability** - opentelemetry
- **Validation** - validator
- **Code Quality** - golangci-lint
- **Migrations** - golang-migrate
- **Protobuf Tooling** - buf
- **Dependency Injection** - fx

## Prerequisites

- [Go](https://go.dev/) 1.25 or higher
- [Protocol Buffers](https://protobuf.dev/) compiler (`protoc`)
- [Go gRPC plugins](https://grpc.io/docs/languages/go/quickstart/)

## Getting Started

First, copy the environment file:

```bash
cp .env.example .env
```

Then, install dependencies and run the server:

```bash
go mod tidy
go run ./cmd/server
```

The server applies pending database migrations before it starts.

The server will be running at [http://localhost:8080](http://localhost:8080).

## Native auth and headless prompts

Build the `ask` binary from `cmd/tui` for these examples.
Auth commands and headless prompts run in process without a database, daemon, or leader.
Run login on the inference host with the same `ASK_HOME` that will run the prompt.
Use an isolated home for live checks so that a test does not replace your normal credentials.

```sh
go build -o ./ask ./cmd/tui
export ASK_HOME="$(mktemp -d)"
./ask auth --help
```

Anthropic requires an explicit browser or copy-code interaction.
Open the URL that the command prints and complete sign-in.
Use copy-code when the browser cannot reach the inference host's loopback callback.
Paste the returned code into private stdin.

```sh
./ask auth login --provider anthropic --method anthropic-oauth --interaction browser
# Or use the hosted copy-code flow:
./ask auth login --provider anthropic --method anthropic-oauth --interaction copy-code
./ask -p "Say hello." --provider anthropic
```

ChatGPT uses browser interaction by default.
Ordinary login reuses the issued client and verified account while a saved ChatGPT record exists.
Use `--new-account` to replace that account after a new verified sign-in.
Model access varies by account.
Select an available model from the [compiled catalog](internal/providers/catalog.go); the example below selects `gpt-5.6-sol`.
A known account access denial stops the request without selecting another model or API-key route.

```sh
./ask auth login --provider openai --method openai-chatgpt
./ask -p "Say hello." --provider openai --model gpt-5.6-sol
./ask auth login --provider openai --method openai-chatgpt --new-account
```

xAI prints a verification URL and device code, then waits for approval.
Complete approval in a browser before the code expires.

```sh
./ask auth login --provider xai --method xai-oauth
./ask -p "Say hello." --provider xai
```

Browser callbacks bind only to loopback at fixed ports: Anthropic uses `53692` and ChatGPT uses `1455`.
For a remote inference host, forward the selected port from your browser host to the inference host's loopback address before login.
Keep both ends of the forward on loopback; do not expose the callback listener on an external address.
The command fails if its callback port is busy.
It prints the sign-in URL; it does not start a browser.
A callback page confirms receipt only; wait for `Credential saved.` in the command.

For API-key login, select `api-key` and enter the key on private stdin.
Do not put keys or callback input in command arguments.
Private input has a 16 KiB limit.
Noninteractive login requires `--method`.
See [command help](cmd/tui/auth_args.go) for the current options.

```sh
./ask auth login --provider anthropic --method api-key
./ask auth logout --provider anthropic
```

Each provider has one saved credential and account.
Saving an API key replaces its OAuth record, and saving OAuth replaces its API key.
Logout deletes the local record only; it does not revoke a remote grant.
An environment key remains eligible after logout.
A deleted ChatGPT record retains no issued client registration for later login.
These choices do not establish compliance with all OpenAI account or session guidance.

A saved OAuth failure stops the request instead of changing to an environment key or a billed API-key route.
If refresh is uncertain, sign in again or use local logout to clear the pending record.
Do not manually remove its pending fence or retry the same rotating grant.
See [credential storage and recovery](internal/settings/README.md#credential-transaction) for filesystem and shutdown limits, and [capture guidance](docs/testing-llm-cassettes.md) before recording inference.

## Database Setup

This project uses GORM with SQLite, for both daemon and cloud mode. `DATABASE_URL` is the SQLite file path (default `app.db`).

The schema comes only from the SQL files in `migrations/` (golang-migrate). They are embedded into the server binary.

- The server applies pending migrations at start.
- `go run ./cmd/server -migrate` applies them and exits.
- A new migration is a pair `NNNNNN_<slug>.up.sql` / `NNNNNN_<slug>.down.sql` in `migrations/`.

## gRPC Setup

This project includes gRPC support. The server runs both HTTP and gRPC concurrently.

- HTTP server: `http://localhost:8080`
- gRPC server: `localhost:50051`

To regenerate protobuf code after modifying `.proto` files:

```bash
protoc --go_out=. --go-grpc_out=. proto/*.proto
```

## Architecture

Ask is an agent harness that runs as a local daemon or as a remote agent in the cloud. It follows the dewee package model (one package for each capability) with extra import rules that lint enforces. See [docs/ask-architecture-reference.md](docs/ask-architecture-reference.md) and [internal/README.md](internal/README.md). Each package has a `README.md` that says what belongs there.

## Project Structure

```
AskCore/
├── cmd/
│   ├── server/            # Daemon: HTTP + gRPC
│   └── tui/               # Terminal client
├── internal/
│   ├── app/               # Composition root (fx)
│   ├── config/  logs/     # Config and logger
│   ├── gateway/           # HTTP and gRPC servers
│   ├── http/              # REST handlers
│   ├── store/             # Models + store interfaces; gormstore/ = GORM implementation
│   ├── migrations/        # Migration runner
│   └── ...                # Agent runtime and capabilities; see internal/README.md
├── migrations/            # SQL files (SQLite)
├── pkg/protocol/          # WS wire contract
├── proto/                 # gRPC definitions
└── .env.example           # Environment variables template
```

## Available Commands

- `go build ./...`: Build all packages
- `go run ./cmd/server`: Run the server (applies pending migrations first)
- `go run ./cmd/server -migrate`: Apply migrations and exit
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`: Lint, including the import rules
- `go test ./...`: Run all tests
- `go fmt ./...`: Format code
- `go vet ./...`: Run static analysis
- `go run ./cmd/tui`: Run the TUI application
- `go run ./cmd/tui -p "hello"`: Run one prompt headless and print the reply (`--mode json` streams every event as JSONL)
- `protoc --go_out=. --go-grpc_out=. proto/*.proto`: Regenerate protobuf code
- `go run github.com/bufbuild/buf/cmd/buf generate`: Generate protobuf code
