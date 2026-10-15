# Contributing to Ask

Thank you for your help. This page tells you how to set up, test, and send a change.

## Prerequisites

- [Go](https://go.dev/) 1.27 or later (see `go.mod`).
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) on the `PATH`. The `grep` tool and its tests need it.
- Python 3 and `tui-test`, only to run the terminal E2E checks (see [e2e/tui/README.md](e2e/tui/README.md)).
- Docker, only for the optional Linux filesystem fault tests.

## Set up

```sh
git clone <repository-url>
cd AskCore
go mod download
go build -o ask ./cmd/tui
```

The binary must be named `ask` for the TUI, `ask leader`, and `ask connect`. See [README.md](README.md#install).

## Test

- `go test ./...` runs all unit and integration tests. Tests that call a model replay recorded cassettes. They need no network and no key.
- To re-record the cassette of one test, set the key in the environment and run `ASK_RECORD=1 go test -run '^TestX$' ./cmd/tui`. See [docs/testing-llm-cassettes.md](docs/testing-llm-cassettes.md). Never commit a key.
- `python3 e2e/tui/run.py` runs the built `ask` on real terminal engines. See [e2e/tui/README.md](e2e/tui/README.md).
- `FSFAULT_ENGINE=docker internal/testsupport/fsfault/run-tests.sh` runs the optional filesystem fault tests. See [internal/testsupport/README.md](internal/testsupport/README.md#filesystem-fault-tests).

Write tests that run the real code path. Do not build a test-only harness that bypasses the real entry point.

## Lint and format

```sh
go fmt ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

The lint run includes the `depguard` import rules from `.golangci.yml`. The rules are explained in [internal/README.md](internal/README.md#import-rules). Read the README of a package before you add a file to it.

## Continuous integration

GitHub Actions runs [`.github/workflows/ci.yml`](.github/workflows/ci.yml) on every pull request and on every push to `master`. The job runs on a GitHub-hosted `ubuntu-latest` runner with the Go version from `go.mod`. It runs `go vet ./...`, `go test -count=1 ./...`, and the lint command above. The test step includes `TestReferenceDocs`, so a pull request fails when `docs/reference/` does not match the code.

CI does not run the terminal E2E checks (`e2e/tui`), the live provider tests (they need keys), or the container tests (`RUN_TESTCONTAINERS=1`). Run them on your machine when your change touches their area.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): subject`.

- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `chore`, `perf`.
- Scope: the package or area, for example `feat(tui): ...` or `fix(leader): ...`.
- Write the subject in the imperative mood. Describe the change, not a plan step or a finding code.
- Do not add AI references.

## Changelog

[CHANGELOG.md](CHANGELOG.md) follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

- Every user-visible change adds one line under `## [Unreleased]`. Use the sections Added, Changed, Deprecated, Removed, Fixed, and Security.
- Write for a user, not for a developer. Say what the user can do or sees. Do not write commit hashes or plan names.
- A change that a user cannot see (a refactor, a test, an internal rename) needs no line.

## Documentation rules

- The owner README of a package says what belongs in the package. Update it when the package changes.
- `docs/reference/` is generated. Do not edit it by hand. Run `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui` and commit the result. `go test` fails when the files are out of date.
- [docs/cli.md](docs/cli.md) is hand-written. Update it when a command, flag, environment variable, or exit code changes.
- Docs own the why and the where. The code owns the what and the how. Link to the source; do not copy it.
- Write in ASD-STE100 Simplified Technical English: short sentences, active voice, one instruction in each sentence.
- Do not put plan, phase, or finding codes in code comments, file names, test names, or docs.
- [docs/README.md](docs/README.md) lists all documents. Add a new document there.

## License

By contributing, you agree that your contribution is released under the [MIT License](LICENSE).

## Pull request checklist

- [ ] CI passes (`go vet`, `go test`, lint).
- [ ] `go fmt ./...` leaves no change.
- [ ] The commit messages follow Conventional Commits.
- [ ] A user-visible change has a line under `[Unreleased]` in `CHANGELOG.md`.
- [ ] The owner README, `docs/cli.md`, and the generated reference are up to date.
- [ ] No key, token, or `.env` file is in the change.
