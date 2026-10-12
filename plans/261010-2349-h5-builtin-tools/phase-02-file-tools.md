---
phase: 2
title: "File tools: read, write, edit, ls"
status: completed
priority: P1
effort: "4d"
dependencies: [1]
---

# Phase 2: File tools: read, write, edit, ls

## Context

- Architecture: [Tool contracts (`tools`)](../architectures/h5-builtin-tools-architecture.md#tool-contracts-tools) (edit ladder, freshness, atomic writes, read, write bullets), [Other](../architectures/h5-builtin-tools-architecture.md#other) (CRLF, BOM, multi-match and stale-edit test cases).
- Decisions: [Q1 edit schema](../architectures/h5-builtin-tools.md#q1-edit-tool-schema).
- xia H5: ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) rows `read`, `write`, `edit`, `edit freshness`, `tests`; ["Skip"](../reports/xia-261010-1727-h5-builtin-tools.md#skip) (no read selectors, no hashline anchors, no apply_patch); [Risks](../reports/xia-261010-1727-h5-builtin-tools.md#risks) (fuzzy edits change the wrong place).
- Inventory rows H-TOOL-01 `read`, H-TOOL-02 `write`, H-TOOL-03 `edit`, H-TOOL-07 `ls`, H-TOOL-11 snippets ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) lines 51-53, 57, 61).
- Pi source (contract texts): `opensources/pi` commit `4c6fb7cfe`, `packages/coding-agent/src/core/tools/read.ts:76,143,161,169-178`, `write.ts:86`, `edit.ts:25,207`, `edit-diff.ts:256-271`, `ls.ts:62,88,109,135,147`.

## Overview

Add the four tools that need no external process. They follow Pi's parameters, limits and texts, with the omp and Grok improvements that the architecture lists.

## Requirements

- Functional:
  - `read(path, offset?, limit?)`: `offset` 1-indexed, `null` treated as omitted (H-TOOL-01). Output: every line as `N|text`; cut by `TruncateHead` (2000 lines / 50 KB); Pi's continuation text `[Showing lines A-B of T. Use offset=N to continue.]` (`read.ts:169-178`); a line over 768 chars is cut only in the model text with a ` … [line cut]` marker; a first line over 50 KB gives Pi's `sed -n` hint (`read.ts:161`); an offset past the end gives the hint `Offset N is beyond end of file (T lines total)` as a normal result, not an error (architecture: past-end hint). Binary detection: a NUL byte or more than 30 % control bytes in the first 8 KB returns an error naming the file as binary. Images: see the next item. Records `FileState`. Details: `kind=read`, `path`, `locations[{path, line=offset}]`, `truncation`, `nextOffset`.
  - `read` and images (user decision 2026-10-11: exactly like Pi; this pulls H-TOOL-20 image resize into H5). Pi source: `opensources/pi/packages/coding-agent/src/core/tools/read.ts:59-135`, `utils/mime.ts`, `utils/image-process.ts`, `utils/image-resize-core.ts:5-28,59-130`, `core/settings-manager.ts:68-69`.
    - Detection by magic bytes (`utils/mime.ts`): JPEG, PNG, GIF, WebP, BMP; a JPEG whose byte 3 is `0xf7` is not treated as an image. Other files go to the text path (binary check above).
    - Conversion (`image-process.ts`): PNG, JPEG, GIF and WebP are kept as they are; other formats (BMP) are decoded and re-encoded as PNG, with the hint `[Image converted from <X> to <Y>.]`. For JPEG, the EXIF orientation tag (0x0112 in the APP1 TIFF IFD0) is read by a small in-house parser and applied when the image is decoded for conversion or resize.
    - Resize (`image-resize-core.ts`), on by default: when the image is larger than 2000x2000 px or its base64 is larger than 4.5 MiB, scale to fit 2000x2000 (aspect kept), then try PNG, then JPEG at qualities 80, 85, 70, 55, 40, then scale down step by step and try again until the base64 fits 4.5 MiB; add Pi's dimension note hint when the image was resized.
    - Result (`read.ts`): text `Read image file [<mime>]` plus the hint lines, plus one `protocol.Image{Data, MimeType}` block. Failures give text only: `[Image omitted: could not be converted to a supported inline image format.]` or `[Image omitted: could not be resized below the inline image size limit.]`.
    - Non-vision model: when the session model's `Input` (`internal/providers/model.go:55`, catalog rows such as `internal/providers/catalog.go:44` list `"image"`) does not contain `image`, the result adds `[Current model does not support images. The image will be omitted from this request.]` and has no image block. `tools.Context` has no model, so `Read` gets a `modelInput func() []string` closure from the composition root (phase 7, same pattern as P7).
    - Settings: `images.autoResize` = true and `images.blockImages` = false are fixed at Pi's defaults in H5 (constants in `file_image.go`); H6 makes them settings.
    - Description: Pi's `read.ts:76` text ("Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments...").
    - Libraries: standard `image`, `image/png`, `image/jpeg`, `image/gif`, plus `golang.org/x/image` (`webp` and `bmp` decode, `draw` for scaling). It is pure Go, not a native image library, so the roadmap rule "do not rebuild native image libraries" stays satisfied.
  - `write(path, content)`: creates parent dirs (0755); `PathLock`; refuses to overwrite a file whose first 5 lines hold `@generated`, `Code generated` + `DO NOT EDIT`, a protoc or sqlc header, with an error that names the marker; `WriteFileAtomic`; when content starts with `#!`, sets the owner, group and other execute bits that read bits allow; result `Successfully wrote to <path>` (`write.ts:86`, no byte count); records `FileState`. Details: `kind=edit`, `path`, `diff{oldText: previous or "", newText}`, `locations[{path, line:1}]`.
  - `edit(path, edits[{oldText,newText}])`: `ArgumentPreparer` accepts top-level `oldText`/`newText` and `edits` as a JSON string or one object (H-TOOL-03). `PathLock`; `FileState.Check` first (stale error: `<path> changed on disk since you last read it. Read it again before you edit.`). Each `oldText` is matched against the original content; matching ladder per edit: exact; then right-trim per line, trim, and Pi's `normalizeForFuzzyMatch` (NFKC, smart quotes, dashes, special spaces); then a fuzzy per-line score at 0.95, retried at 0.8 with indentation ignored. A match must be unique; matches must not overlap. The replacement is re-indented to the matched block's indentation when a fuzzy rung matched. Errors keep Pi's text (`edit-diff.ts:256-271`) and add omp's hint `Closest match (N% similar) at line L:` with the first differing line; multiple matches list up to 5 previews with line numbers. A no-op edit (oldText equals newText) is rejected; a third identical no-op call on the same path in a session is a hard error that tells the model to read the file again. BOM is stripped and restored; CRLF or LF is kept as found. `WriteFileAtomic`; result `Successfully replaced K block(s) in <path>.` (`edit.ts:207`). Details: `kind=edit`, whole-file `diff{oldText,newText}`, `locations` of each changed block (first line).
  - `ls(path?, limit=500)`: entries sorted case-insensitively (`ls.ts:109`), `/` suffix for dirs, dotfiles included, 50 KB cap; `(empty directory)`; `Path not found: <path>`; limit notice `500 entries limit reached. Use limit=1000 for more` (`ls.ts:147`). Details: `kind=search`.
  - Every tool resolves paths with `workspace.ResolvePath(tc.Cwd, p)` and tries `PathVariants` when the path does not exist.
  - Snippets and guidelines (H-TOOL-11): port the text of Pi's `*ToolSystemPromptContribution` for each tool (`read.ts:77-78`, `write.ts:54-55`, `edit.ts:153-154`, `ls.ts:63`).
- Non-functional: `read` and `ls` implement `ConcurrencySafe` = true; `write` and `edit` do not (P9). Bodies check `ctx` before the write and return promptly on cancel.

## Architecture

```
model call → coordinator Prepare (coercion, edit shim) → Execute
  read:  ResolvePath → open → sniff → number lines → TruncateHead → FileState.Record → result + details
  write: ResolvePath → PathLock → generated guard → WriteFileAtomic → chmod → FileState.Record
  edit:  ResolvePath → PathLock → FileState.Check → read → BOM/EOL split → ladder per edit → overlap check → apply → restore BOM/EOL → WriteFileAtomic → FileState.Record
  ls:    ResolvePath → ReadDir → sort → cap
```

Per-session state: one `tools.FileState` and one no-op counter, both created by the session's tool constructor (phase 7). Lifetime = one Agent (`newNativeAgentOn` runs once per session, `cmd/tui/headless.go:242`).

## Runtime Flow

- Actor and trigger: the model calls the tool.
- Registered entry point: `ask -p` and ACP sessions register the tools in `newNativeAgentOn` (phase 7).
- Internal path: coordinator (`internal/agent/tool_coordinator.go:204,264`) → tool body → helpers of phase 1.
- Prepared data and external mocks: temp repo files; scripted model HTTP in E2E.
- Observable result: file content on disk, result text, details JSON.
- End-to-end proof: `TestPrintReadsFileWithLineNumbers`, `TestPrintReadPagesWithOffset`, `TestPrintWritesFileAtomically`, `TestPrintWriteRefusesGeneratedFile`, `TestPrintEditsFileAfterRead`, `TestPrintEditRejectsStaleFile`, `TestPrintListsDirectory`, `TestPrintEditAcceptsLegacyShape`, `TestPrintReadImageReturnsImageBlock`, `TestPrintReadLargeImageIsResized`, `TestPrintReadImageForNonVisionModelAddsNote` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/tools/file_read.go` | create | ~220 | `file_read_test.go` |
| `internal/tools/file_write.go` | create | ~110 | `file_write_test.go` |
| `internal/tools/file_generated.go` | create | ~40 | in `file_write_test.go` |
| `internal/tools/file_edit.go` | create | ~200 | `file_edit_test.go` |
| `internal/tools/file_edit_match.go` | create | ~260 | `file_edit_match_test.go` |
| `internal/tools/file_ls.go` | create | ~90 | `file_ls_test.go` |
| `internal/tools/file_image.go` | create (detect, EXIF orientation, convert, resize ladder) | ~260 | `file_image_test.go` |
| `go.mod`, `go.sum` | modify (add `golang.org/x/image`) | +2 | none |

Existing coverage: none for these tools (new). `internal/tools` regression tests from phase 1 must stay green.

## Tests Before

`go test ./internal/tools` passes (phase 1 state).

## Tests After (write first)

| Test | Scenario | Priority |
|---|---|---|
| `TestReadNumbersEveryLine` | 3 lines → `1\|a` … | critical |
| `TestReadTruncatesAndGivesNextOffset` | 3000 lines | critical |
| `TestReadOffsetPastEndGivesHint` | offset 50 on 10 lines | high |
| `TestReadHugeFirstLineGivesSedHint` | 60 KB first line | medium |
| `TestReadCutsLongLineInModelViewOnly` | 2000-char line; details keep counts | medium |
| `TestReadRejectsBinaryByContent` | NUL bytes; 40 % control bytes | critical |
| `TestReadImageReturnsImageBlock` | small PNG → `Read image file [image/png]` + image block | critical |
| `TestReadDetectsImagesByMagicBytes` | jpeg, png, gif, webp, bmp with wrong extensions; JPEG with byte 3 `0xf7` not an image | high |
| `TestReadLargeImageIsResizedUnderLimit` | 4000x3000 PNG noise → ≤ 2000x2000, base64 ≤ 4.5 MiB, dimension note | critical |
| `TestReadResizeLadderFallsBackToJpegQualities` | PNG too large after scale → JPEG 80…40, then scale down | high |
| `TestReadBMPConvertedToPNG` | BMP → PNG block, `[Image converted from image/bmp to image/png.]` | high |
| `TestReadJPEGWithExifOrientation` | JPEG with orientation 6 that needs a resize → output rotated (width and height swapped) | high |
| `TestReadImageTooLargeAfterResizeIsOmitted` | resize limit lowered by a test hook → `[Image omitted: could not be resized below the inline image size limit.]`, no block | high |
| `TestReadUndecodableImageIsOmitted` | BMP header with broken data → `[Image omitted: could not be converted ...]` | medium |
| `TestReadImageForNonVisionModelAddsNote` | `modelInput` returns `["text"]` → note, no image block | critical |
| `TestReadTriesScreenshotVariant` | file with U+202F, path with space | medium |
| `TestWriteCreatesParentsAtomically` | nested new path; temp file absent | critical |
| `TestWriteSetsExecutableBitForShebang` | `#!/bin/sh` | high |
| `TestWriteRefusesGeneratedFile` | `// Code generated ... DO NOT EDIT.` | high |
| `TestEditExactUniqueMatch` | two edits | critical |
| `TestEditRejectsMultipleMatchesWithPreviews` | 3 matches → 3 previews with lines | critical |
| `TestEditMatchesAfterWhitespaceAndUnicodeNormalization` | trailing spaces, smart quotes, NBSP | critical |
| `TestEditFuzzyMatchReindentsReplacement` | indented block, model sent no indent | high |
| `TestEditNearDuplicateBlocksNeedUniqueMatch` | two blocks 96 % similar | critical |
| `TestEditNotFoundNamesClosestMatch` | `Closest match (N% similar) at line L` | high |
| `TestEditRejectsOverlappingEdits` | overlapping oldText | high |
| `TestEditRejectsNoOpAndStopsAfterThree` | same no-op 3 times | high |
| `TestEditKeepsCRLFAndBOM` | CRLF + BOM round trip, byte compare | critical |
| `TestEditRejectsStaleFile` | read, outside write, edit | critical |
| `TestEditAllowsFileNeverRead` | no read before edit | high |
| `TestEditPreparerAcceptsLegacyShapes` | top-level oldText; edits as string; one object | high |
| `TestEditDetailsCarryWholeFileDiffAndLocations` | details JSON | high |
| `TestLsSortsAndMarksDirectories` | mixed case, dotfiles, dir | critical |
| `TestLsLimitNotice` | 600 entries | medium |
| `TestFileToolsReturnOnCancelledContext` | cancelled ctx | high |

## Function and interface checklist

- [x] `tools.Read`, `tools.Write`, `tools.Edit`, `tools.Ls` (each: `Decl`, `Execute`, `PromptSnippet`, `PromptGuidelines`; `ConcurrencySafe` for read and ls)
- [x] `tools.NewFileTools(fs *FileState, modelInput func() []string) []Tool` constructor used by phase 7 (`Edit` keeps its no-op counter inside the instance; a nil `modelInput` means images are always returned)
- [x] `Edit.PrepareArguments` (`tools.ArgumentPreparer`)
- [x] matcher: `findMatch(content, oldText) (start, end, rung, err)`, `normalizeForFuzzyMatch`, `lineScore`, `reindent`
- [x] generated-file guard `isGenerated(head []byte) (marker string, ok bool)`
- [x] images: `detectImage`, `exifOrientation`, `toInline(data, mime, lim) (inlineImage, error)`, resize ladder; `Read` field `modelInput func() []string`

## Dependency map

Needs phase 1 (`ResolvePath`, `TruncateHead`, `PathLock`, `FileState`, `WriteFileAtomic`, `ToolResultDetails`). Used by phase 7 (registration) and phase 8 (details decode). No overlap with phases 3-6.

## Related Code Files

- Create: the seven files above and their tests.
- Modify: `go.mod`, `go.sum` (add `golang.org/x/image`).

## Implementation Steps

1. Port Pi's `normalizeForFuzzyMatch` exactly (H-TOOL-03 note) from `pi/.../edit-diff.ts`; port behavior only for omp's scorer (MIT; no code copy needed).
2. Write the tests; run; they fail.
3. Implement the matcher, then `edit`, then `read`, `write`, `ls`.
4. Run the regression gate.

## Regression Gate

`go test ./internal/tools ./internal/workspace -race && go vet ./internal/tools`

## Success Criteria

- [x] All listed tests pass with `-race`.
- [x] Error and success texts match the Pi sources cited above, plus the named omp additions.
- [x] No tool writes outside `WriteFileAtomic`.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Fuzzy rung edits the wrong block | M x H | Unique match at each rung, no rung below 0.8, near-duplicate test; signal: `TestEditNearDuplicateBlocksNeedUniqueMatch` fails or a real run edits the wrong place; response: raise the threshold or drop the 0.8 retry (replan with the user, it changes the architecture's ladder). |
| Stale check rejects a file the session itself changed through `bash` | M x M | Intended: the model must read again; the error says so. |
| Atomic rename changes inode (hard links, ownership) | L x M | Keep mode; document that hard links are broken (as every atomic writer); omp and Grok write in place, Ask chose atomic (architecture). |
| Resized image still exceeds a provider's own limit | L x M | Pi's limits (2000x2000, 4.5 MiB base64); signal: a provider rejects a request with an image in the phase 10 run; response: lower the constants (H6 makes them settings). |
| Go JPEG and PNG encoders give larger files than Pi's encoder | M x L | The quality ladder and the scale-down loop end below the limit or omit the image with Pi's text. |

## Security Considerations

- No path sandboxing (D3). Generated-file guard is a safety net, not a policy.
- `read` of a secret file returns it to the model; this is Pi's stance (H-SEC-03).
