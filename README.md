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
│   └── ...                # Agent runtime packages (scaffold): agent, pipeline, providers, tools, ...
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
