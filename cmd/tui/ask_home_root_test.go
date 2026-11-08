package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askDirOwners are the only non-test source files that may hold the string
// literal ".ask". Any other file must get the Ask home from
// settings.ResolveHome, so that ASK_HOME applies everywhere.
var askDirOwners = map[string]string{
	"internal/settings/paths.go":    "ResolveHome: the one root function of the Ask home",
	"internal/leader/paths.go":      "the leader may not import internal/settings, so it keeps its own copy of the home rule",
	"internal/settings/project.go":  "the name of the project settings folder <root>/.ask",
	"internal/workspace/project.go": "the name of the project folder for the trust check and the gated files",
	"internal/app/session_start.go": "the project prompt folder and the gated skills entry of a trusted project, matched against the workspace gated list",
}

// TestAskHomeHasOneRootFunction scans the non-test Go sources of the module
// for the string literal ".ask" and allows only the listed owners.
func TestAskHomeHasOneRootFunction(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join(packageDir, "..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(moduleRoot, "go.mod"))
	require.NoError(t, err, "the module root must hold go.mod")

	var found []string
	fset := token.NewFileSet()
	err = filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == moduleRoot {
				return nil
			}
			name := d.Name()
			if name == "testdata" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			// A nested module (third_party) is not part of this module.
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err == nil && (value == ".ask" || strings.HasSuffix(value, "/.ask")) {
				found = append(found, filepath.ToSlash(rel))
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	sort.Strings(found)
	for _, rel := range found {
		_, ok := askDirOwners[rel]
		assert.True(t, ok, "%s holds the literal \".ask\"; use settings.ResolveHome or add the file to askDirOwners with a reason", rel)
	}
	for owner := range askDirOwners {
		assert.Contains(t, found, owner, "a listed owner no longer holds the literal; remove it from the list")
	}
}
