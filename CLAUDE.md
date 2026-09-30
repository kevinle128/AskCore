# AskCore

This file provides context about the project for AI assistants.

## Project Overview

- **Ecosystem**: Go

## Tech Stack

- Web Framework: echo
- Database: gorm
- API: grpc-go
- CLI: bubbletea
- Logging: zap
- Testing: gomock, testcontainers, testify
- Messaging: asynq
- Observability: opentelemetry
- Validation: validator
- Code Quality: golangci-lint
- Migrations: golang-migrate
- Protobuf Tooling: buf
- Dependency Injection: fx

## Project Structure

```
AskCore/
├── go.mod           # Module definition
├── cmd/
│   └── server/      # Server entry point
├── internal/        # Internal packages
├── proto/           # Protocol buffers
```

## Common Commands

- `go mod tidy` - Install dependencies
- `go run cmd/server/main.go` - Start server
- `go test ./...` - Run tests
- `go fmt ./...` - Format code

## Better Fullstack project context

`bts.jsonc` is the authority for the current Stack Graph. Its `stackParts` array owns role selection and `ownerPartId` bindings. Top-level option fields are a compatibility projection and must not become a second mutation path.

### Stack Parts, ownership, and evidence

- `backend.api:go:grpc-go`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.buildTool:go:buf`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.caching:go:redis`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.cli:go:bubbletea`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.codeQuality:go:golangci-lint`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.config:go:viper`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.jobQueue:go:asynq`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.libraries:go:fx`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.logging:go:zap`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.migrations:go:golang-migrate`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.observability:go:opentelemetry`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.orm:go:gorm`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.realtime:go:centrifuge`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:gomock`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:testcontainers`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.testing:go:testify`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend.validation:go:validator`. It belongs to `backend:go:echo`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `backend:go:echo`. Its generated target is `apps/server`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.
- `database:universal:sqlite`. Its generated target is `packages/db`. Evidence is `listed` with `unverified` freshness. Verification maintainer: @Marve10s.

### Installed-version authority

Use `bts.jsonc` for the generator and schema version. Use local package manifests and lockfiles for installed dependency versions. Do not assume that documentation for a newer Better Fullstack release matches this project.

### Compatibility and lifecycle safety

Run `create-better-fullstack context --json` for bounded roles, capabilities, evidence, compatibility issues, and safe next actions. Run `create-better-fullstack doctor --json` before repairing graph drift. Existing-project writes must start with a plan and use the exact review token. Use `create-better-fullstack recipes check --json` before editing recipe-owned paths or managed regions, and use recipe history plus project recovery commands to undo a reviewed operation.

User code outside an explicit Better Fullstack managed region is not generator-owned. Missing or changed managed-region hashes stop recipe planning for manual review.

<!-- <better-fullstack:recipes sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855> -->

<!-- </better-fullstack:recipes> -->

## Maintenance

Keep CLAUDE.md updated when:

- Adding/removing dependencies
- Changing project structure
- Adding new features or services
- Modifying build/dev workflows

AI assistants should suggest updates to this file when they notice relevant changes.
