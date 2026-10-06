package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readPipedStdin reads stdin to EOF and trims it, like Pi's readPipedStdin.
// An empty result means no stdin part.
func readPipedStdin(stdin io.Reader) (string, error) {
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("Could not read stdin: %w", err) //nolint:staticcheck // Pi's message text
	}
	return strings.TrimSpace(string(b)), nil
}

// fileText wraps every non-empty file in a <file> tag, like Pi's
// processFileArguments for text files. Images are read as text until H2
// gains image input.
func fileText(paths []string) (string, error) {
	var b strings.Builder
	for _, p := range paths {
		abs, err := resolvePath(p)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("File not found: %s", abs) //nolint:staticcheck // Pi's message text
		}
		if err == nil && info.Size() == 0 {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("Could not read file %s: %w", abs, err) //nolint:staticcheck // Pi's message text
		}
		fmt.Fprintf(&b, "<file name=\"%s\">\n%s\n</file>\n", abs, strings.TrimPrefix(string(data), "\uFEFF"))
	}
	return b.String(), nil
}

func resolvePath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	return filepath.Abs(p)
}

// buildPrompts returns the prompts to run in order. The first joins stdin,
// the file text and the first message with no separator, as Pi's
// buildInitialMessage does; it is dropped when that join is empty. The other
// messages follow as they are.
func buildPrompts(stdin, files string, messages []string) []string {
	first := stdin + files
	if len(messages) > 0 {
		first += messages[0]
		messages = messages[1:]
	}
	var prompts []string
	if first != "" {
		prompts = append(prompts, first)
	}
	return append(prompts, messages...)
}
