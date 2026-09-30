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
go run cmd/server/main.go
```

The server will be running at [http://localhost:8080](http://localhost:8080).

## Database Setup

This project uses GORM with SQLite by default. To configure the database:

1. Copy the environment file:

```bash
cp .env.example .env
```

2. Update `DATABASE_URL` in `.env` with your database connection string.

Supported databases:

- SQLite (default): `DATABASE_URL=./data.db`
- PostgreSQL: `DATABASE_URL=postgres://user:pass@localhost:5432/dbname`

## gRPC Setup

This project includes gRPC support. The server runs both HTTP and gRPC concurrently.

- HTTP server: `http://localhost:8080`
- gRPC server: `localhost:50051`

To regenerate protobuf code after modifying `.proto` files:

```bash
protoc --go_out=. --go-grpc_out=. proto/*.proto
```

## Project Structure

```
AskCore/
├── go.mod                # Module definition
├── cmd/
│   └── server/           # HTTP server entry point
│       └── main.go
│   └── tui/              # Terminal UI application
│       └── main.go
├── internal/
│   ├── database/         # Database configuration
│   │   └── database.go
│   ├── models/           # GORM models
│   │   └── models.go
│   └── handlers/         # HTTP handlers
│       └── handlers.go
├── proto/                # Protocol buffer definitions
│   ├── greeter.proto
│   ├── greeter.pb.go
│   └── greeter_grpc.pb.go
├── .env.example          # Environment variables template
└── .gitignore
```

## Available Commands

- `go build ./...`: Build all packages
- `go run cmd/server/main.go`: Run the server
- `go test ./...`: Run all tests
- `go fmt ./...`: Format code
- `go vet ./...`: Run static analysis
- `go run cmd/tui/main.go`: Run the TUI application
- `protoc --go_out=. --go-grpc_out=. proto/*.proto`: Regenerate protobuf code
- `go run github.com/bufbuild/buf/cmd/buf generate`: Generate protobuf code
- `migrate -path migrations -database "$DATABASE_URL" up`: Run migrations
