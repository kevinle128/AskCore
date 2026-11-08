package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// projectDir is the name of the project settings folder in a project root.
const projectDir = ".ask"

// gatedProjectNames are the entries in <root>/.ask that need a trust
// decision. A file and a folder are equal here: any entry with the name
// counts, so that an odd type cannot hide a resource. This list is the only
// source for the check and for the docs.
var gatedProjectNames = []string{
	"settings.json",
	"SYSTEM.md",
	"APPEND_SYSTEM.md",
	"mcp.json",
	"models.json",
	"skills",
	"prompts",
	"extensions",
}

// gatedAncestorEntry is the entry that needs a trust decision in the root and
// in each ancestor of the root, except in the user home.
var gatedAncestorEntry = filepath.Join(".agents", "skills")

// canonicalPath returns the absolute, clean form of p with every symbolic
// link resolved. When the links cannot be resolved (for example, the path does
// not exist), it returns the clean absolute path.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// ProjectRoot returns the canonical project root of a session cwd: the
// absolute cwd with every symbolic link resolved, or the clean absolute cwd
// when it cannot be resolved. It does not search the parent folders for a
// .ask folder.
//
// hasLayer is false when <root>/.ask is the Ask home askHome (compared after
// both resolve). The user files of the Ask home are then not a project layer,
// for example when the cwd is the user home and the Ask home is ~/.ask.
// An empty askHome is never equal to <root>/.ask.
func ProjectRoot(cwd, askHome string) (root string, hasLayer bool, err error) {
	if cwd == "" {
		return "", false, errors.New("workspace: project root needs a working directory")
	}
	root, err = canonicalPath(cwd)
	if err != nil {
		return "", false, fmt.Errorf("workspace: project root of %q: %w", cwd, err)
	}
	if askHome == "" {
		return root, true, nil
	}
	home, err := canonicalPath(askHome)
	if err != nil {
		return "", false, fmt.Errorf("workspace: Ask home %q: %w", askHome, err)
	}
	layer, err := canonicalPath(filepath.Join(root, projectDir))
	if err != nil {
		return "", false, fmt.Errorf("workspace: project folder of %q: %w", root, err)
	}
	return root, layer != home, nil
}

// GatedFiles returns the gated entries that exist for a canonical project
// root, in sorted order. An entry in the root is relative to the root; an
// entry in an ancestor is absolute. The entries in <root>/.ask count only when
// hasLayer is true (see ProjectRoot). The .agents/skills folder of userHome is
// not a project resource and does not count.
//
// It uses Lstat only, so it never opens a file and never follows a link at
// the end of a path. A "not exist" result, also through a parent that is not
// a folder, means that the entry is absent. Any other Lstat error is
// returned with the entries found; the caller must then treat the project as
// gated, because an entry that cannot be checked can exist.
func GatedFiles(root string, hasLayer bool, userHome string) ([]string, error) {
	var found []string
	var errs []error
	check := func(abs, show string) {
		_, err := os.Lstat(abs)
		switch {
		case err == nil:
			found = append(found, show)
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		default:
			errs = append(errs, err)
		}
	}
	if hasLayer {
		for _, name := range gatedProjectNames {
			rel := filepath.Join(projectDir, name)
			check(filepath.Join(root, rel), rel)
		}
	}
	skipSkills := ""
	if userHome != "" {
		home, err := canonicalPath(userHome)
		if err != nil {
			errs = append(errs, err)
		} else {
			skipSkills = filepath.Join(home, gatedAncestorEntry)
		}
	}
	for dir := root; ; {
		abs := filepath.Join(dir, gatedAncestorEntry)
		if abs != skipSkills {
			show := abs
			if dir == root {
				show = gatedAncestorEntry
			}
			check(abs, show)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	sort.Strings(found)
	return found, errors.Join(errs...)
}
