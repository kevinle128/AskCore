# Ask

Ask is an agent harness written in Go. It runs a model in a loop with tools (read, write, edit, shell, search) and shows the work in a terminal UI, in a headless mode, or to an editor over the Agent Client Protocol (ACP).

Status: early development. The `ask` binary, its providers, tools, leader, and TUI work today. The `cmd/server` daemon is a small demo (health, users, posts, one gRPC service). Many packages are scaffolds; see [internal/README.md](internal/README.md).

## Requirements

- [Go](https://go.dev/) 1.27 or later (see `go.mod`).
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) on the `PATH`. The `grep` tool needs it.
- Python 3 and `tui-test` only for the terminal E2E checks (see [e2e/tui/README.md](e2e/tui/README.md)).

## Install

The `ask` binary is built from `cmd/tui`. The TUI, `ask connect`, `ask leader`, and `/providers` need a binary that is named `ask`. A client starts the leader from a binary named `ask` that is next to the caller or on the `PATH` (see [internal/leader/spawn.go](internal/leader/spawn.go)). `go run ./cmd/tui` and `go install ./cmd/tui` make a binary named `tui`, so they do not give you these features.

Build the binary with the name `ask` and put it on your `PATH`:

```sh
go build -o ask ./cmd/tui
mv ask "$(go env GOPATH)/bin/ask"   # or any other directory on the PATH
```

`ask -p`, `ask auth`, `ask version`, and `ask acp` need no leader. They also work with `go run ./cmd/tui`.

## Quick start

```sh
ask                       # full-screen TUI; connects to the local leader, starts one when none runs
ask -p "Say hello."       # one prompt, headless; the default provider is the demo provider faux
ask auth login --provider anthropic --method api-key   # save a provider sign-in
ask -p "Say hello." --provider anthropic
```

With no usable provider, the TUI starts with no model. Type `/providers` to sign in. Details are in [docs/cli.md](docs/cli.md).

## Documentation

- [docs/README.md](docs/README.md): index of all documents.
- [docs/cli.md](docs/cli.md): subcommands, flags, environment variables, exit codes, TUI commands.
- [docs/reference/](docs/reference/): built-in tools and providers (generated from code).
- [CONTRIBUTING.md](CONTRIBUTING.md): how to build, test, and send a change.
- [CHANGELOG.md](CHANGELOG.md): user-visible changes.

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

## Server (daemon)

`cmd/server` is a demo daemon. It serves HTTP (Echo) and gRPC on SQLite. It has no WebSocket endpoint, no dashboard, and no leader client yet; the design in [docs/ask-architecture-reference.md](docs/ask-architecture-reference.md) describes them as targets.

```sh
cp .env.example .env     # optional; the server also reads the environment
go run ./cmd/server
```

- HTTP: `http://localhost:8080` (`GET /health`, `/api/users`, `/api/posts`).
- gRPC: `localhost:50051` (`Greeter` service in [proto/greeter.proto](proto/greeter.proto)).
- `DATABASE_URL` is the SQLite file path (default `app.db`).

### Database

The schema comes only from the SQL files in `migrations/` (golang-migrate). They are embedded into the server binary.

- The server applies pending migrations at start.
- `go run ./cmd/server -migrate` applies them and exits.
- A new migration is a pair `NNNNNN_<slug>.up.sql` and `NNNNNN_<slug>.down.sql` in `migrations/`.

### Protobuf

The generated files are committed. Building and running need no protobuf tools. After you change a `.proto` file, regenerate the code (needs network access for the remote plugins in `buf.gen.yaml`):

```sh
go run github.com/bufbuild/buf/cmd/buf generate
```

## Architecture

Ask follows the dewee package model (one package for each capability) with import rules that lint enforces. See [docs/ask-architecture-reference.md](docs/ask-architecture-reference.md) and [internal/README.md](internal/README.md). Each package README says what belongs there.

The project was generated with [Better Fullstack](https://github.com/Marve10s/Better-Fullstack); `bts.jsonc` records the generator choices.

## License

Ask is released under the [MIT License](LICENSE).

The patched copies in `third_party/` keep their own licenses: `third_party/bubbletea` is MIT ([LICENSE](third_party/bubbletea/LICENSE)) and `third_party/acp-go-sdk` is Apache-2.0 ([LICENSE](third_party/acp-go-sdk/LICENSE)). Each `PATCH.txt` records the local changes.
