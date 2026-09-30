# `internal/validation`

Shared validator instance.

## What belongs here

- validator setup and custom rules

## What does not belong here

| Code | Put it in |
|---|---|
| Handler-specific checks | `internal/http` |

## File names

`validation.go`

## Imports

- Allowed: validator
- Denied: other `AskCore/internal/*` packages

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
