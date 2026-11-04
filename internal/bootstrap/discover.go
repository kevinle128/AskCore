package bootstrap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

var contextNames = [...]string{"AGENTS.md", "CLAUDE.md"}

// contextDirs returns the folders to read, from the git root down to cwd. The
// root is the nearest folder that holds .git as a file (a worktree) or a
// folder. A root equal to userHome counts as no root, so only cwd is read. A
// probe error other than "not exist" is returned: the walk must not climb past
// a folder it could not check.
func contextDirs(cwd, userHome string) ([]string, error) {
	cwd = filepath.Clean(cwd)
	chain := []string{cwd}
	for dir := cwd; ; {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			if userHome != "" && dir == filepath.Clean(userHome) {
				return []string{cwd}, nil
			}
			slices.Reverse(chain)
			return chain, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("probe git root at %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return []string{cwd}, nil
		}
		dir = parent
		chain = append(chain, dir)
	}
}
