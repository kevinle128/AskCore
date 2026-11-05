package bootstrap

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

const (
	systemFile       = "SYSTEM.md"
	appendSystemFile = "APPEND_SYSTEM.md"
	// FlagSource is PromptFiles.CustomFrom when the replacement came from the
	// --system-prompt flag.
	FlagSource = "--system-prompt"
)

// PromptInput names the prompt files and the flags of one session.
// ProjectDir is <root>/.ask. The caller passes "" when the project is not
// trusted: that is the whole trust gate for SYSTEM.md and APPEND_SYSTEM.md.
type PromptInput struct {
	UserDir    string
	ProjectDir string
	Replace    string
	Append     []string
}

// PromptFiles is the result after precedence. Text is trimmed.
type PromptFiles struct {
	// Custom replaces the default prompt; "" means keep the default.
	Custom string
	// CustomFrom is the path of the winning file or FlagSource.
	CustomFrom string
	Append     []string
}

// LoadPromptFiles picks one replacement (flag, then project SYSTEM.md, then
// user SYSTEM.md; never merged) and one APPEND_SYSTEM.md (project, then user),
// then adds the flag appends. Blank text counts as absent. A file that exists
// but cannot be read, or is over MaxFileBytes, is an error: it is explicit
// configuration, so falling back to the default prompt would hide the problem.
func LoadPromptFiles(in PromptInput) (PromptFiles, error) {
	var out PromptFiles
	dirs := promptDirs(in)

	if text := strings.TrimSpace(in.Replace); text != "" {
		out.Custom, out.CustomFrom = text, FlagSource
	} else {
		text, from, err := firstPromptFile(dirs, systemFile)
		if err != nil {
			return PromptFiles{}, err
		}
		out.Custom, out.CustomFrom = text, from
	}

	text, _, err := firstPromptFile(dirs, appendSystemFile)
	if err != nil {
		return PromptFiles{}, err
	}
	if text != "" {
		out.Append = append(out.Append, text)
	}
	for _, a := range in.Append {
		if a = strings.TrimSpace(a); a != "" {
			out.Append = append(out.Append, a)
		}
	}
	return out, nil
}

func promptDirs(in PromptInput) []string {
	var dirs []string
	for _, d := range []string{in.ProjectDir, in.UserDir} {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func firstPromptFile(dirs []string, name string) (text, from string, err error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		data, _, err := readRegular(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return "", "", fmt.Errorf("read %s: %w", path, err)
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text, path, nil
		}
	}
	return "", "", nil
}
