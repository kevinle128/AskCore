# H5 phase 2 report: file tools

Status: completed. `go test -race ./internal/tools/... ./internal/workspace/...`, `go vet ./internal/tools/...` and golangci-lint on `./internal/tools/...` pass.

## Deviations and decisions for the orchestrator

1. `NewFileTools` takes a second argument: `NewFileTools(fs *FileState, modelInput func() []string) []Tool`. The phase text gave `NewFileTools(fs)`, but `Read.modelInput` is unexported and phase 7 lives in `cmd/tui`, so the closure has to enter through the constructor. A nil closure means "always return the image" (Pi: no model, no note). Phase 7 passes `func() []string { return ag.State().Model.Input }`.
2. `write` does not try `workspace.PathVariants`. For `read`, `edit` and `ls` a missing path falls back to the first variant that exists. For `write` a missing path is the normal create case, so a variant fallback would silently overwrite a different file. Pi does the same (variants only on read).
3. `golang.org/x/image` is pinned to v0.45.0. `go get golang.org/x/image` (latest, v0.47.0) also upgraded `x/sys`, `x/text`, `x/sync`, `x/tools`, `x/net`, `x/mod`, `x/crypto`. v0.45.0 adds only its own line, so the go.mod diff is one `require` line and two go.sum lines.
4. Non-vision model: the image is not decoded or resized at all. The result is `Read image file [<mime>]` plus the note, no block.
5. `images.blockImages` has no code path in H5 (false is the only value, so there is nothing to branch on); `images.autoResize` is `imageLimits.autoResize` in `file_image.go`.
6. `edit` `locations` are line numbers in the new file (Pi's `firstChangedLine` is also new-file based).
7. Matcher ladder: exact; normalized substring (Pi's `normalizeForFuzzyMatch`, with an offset map back to the original bytes); whole lines with indentation ignored; per-line score 0.95; per-line score 0.8 with indentation ignored. A rung with more than one place is an error and never falls to a looser rung. Re-indent runs only for the three whole-line rungs.
8. `read` drops the final empty element of a file that ends with a newline, so `a\nb\n` has 2 lines (Pi's `split("\n")` shows a third empty line).
9. Another agent's phase 4 test files appeared in `internal/tools` while this phase ran; the package compiled again once their `Find` landed. No file of theirs was touched.

## Exported API for later phases

- `tools.NewFileTools(state *FileState, modelInput func() []string) []Tool` returns `*Read`, `*Write`, `*Edit`, `*Ls` (names `read`, `write`, `edit`, `ls`).
- Types `tools.Read`, `tools.Write`, `tools.Edit`, `tools.Ls`; `Edit.PrepareArguments` (ArgumentPreparer); `Read` and `Ls` implement `ConcurrencySafe` (true); all four implement `PromptContributor`.
- Helpers in package scope that other `tools` files can reuse: `failf` (model-facing error text), `mustDetails`, `locateExisting`, `formatSize`.
