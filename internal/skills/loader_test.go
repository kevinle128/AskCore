package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func skillMD(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
}

func TestLoadValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		file   string
		reason string
	}{
		{"no frontmatter", "just text\n", "no frontmatter"},
		{"unterminated", "---\nname: a\ndescription: d\n", "no frontmatter"},
		{"bad yaml", "---\n: : [\n---\n", "bad frontmatter"},
		{"unrepairable yaml", "---\ndescription: d\n  name: [a\n---\n", "bad frontmatter"},
		{"missing description", "---\nname: ok\n---\n", "missing description"},
		{"blank description", "---\nname: ok\ndescription: '  '\n---\n", "missing description"},
		{"yes is not a boolean", "---\ndescription: d\ndisable-model-invocation: yes\n---\n", "bad boolean disable-model-invocation"},
		{"quoted boolean", "---\ndescription: d\ndisable-model-invocation: \"true\"\n---\n", "bad boolean disable-model-invocation"},
		{"oversized frontmatter", "---\ndescription: d\nnote: " + strings.Repeat("x", 70000) + "\n---\n", "frontmatter over 65536 bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			file := write(t, filepath.Join(root, "demo", "SKILL.md"), tc.file)
			got := Load([]Root{{Dir: root, Scope: ScopeUser}})
			assert.Empty(t, got.Skills)
			assert.Equal(t, []Diagnostic{{Path: file, Scope: ScopeUser, Reason: tc.reason}}, got.Diagnostics)
		})
	}
}

func TestLoadAcceptsRealWorldShapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	crlf := write(t, filepath.Join(root, "crlf", "SKILL.md"), "---\r\nname: crlf-skill\r\ndescription: windows\r\n---\r\nbody\r\n")
	folder := write(t, filepath.Join(root, "from-folder", "SKILL.md"), "---\ndescription: no name\n---\n")
	blank := write(t, filepath.Join(root, "blank  name", "SKILL.md"), "---\nname: '  '\ndescription: d\n---\n")
	hidden := write(t, filepath.Join(root, "hidden", "SKILL.md"), "---\ndescription: manual\ndisable-model-invocation: true\n---\n")
	colon := write(t, filepath.Join(root, "kk", "SKILL.md"), "---\nname: kk:agent-browser\ndescription: browser\n---\n")
	spaced := write(t, filepath.Join(root, "spaced", "SKILL.md"), "---\nname:   My   Skill\t Name\ndescription: |\n  line one\n  line two\n---\n")
	long := strings.Repeat("é", 5000)
	huge := write(t, filepath.Join(root, "huge", "SKILL.md"), skillMD("huge", long))
	cut := write(t, filepath.Join(root, "cut", "SKILL.md"), skillMD(strings.Repeat("ü", 70), "d"))
	write(t, filepath.Join(root, "notes.md"), skillMD("not-a-skill", "only SKILL.md counts"))

	got := Load([]Root{{Dir: root, Scope: ScopeProject}})
	assert.Empty(t, got.Diagnostics)
	assert.Equal(t, []Skill{
		{Name: "My Skill Name", Description: "line one line two", File: spaced, Scope: ScopeProject, ModelInvocable: true},
		{Name: "blank name", Description: "d", File: blank, Scope: ScopeProject, ModelInvocable: true},
		{Name: "crlf-skill", Description: "windows", File: crlf, Scope: ScopeProject, ModelInvocable: true},
		{Name: "from-folder", Description: "no name", File: folder, Scope: ScopeProject, ModelInvocable: true},
		{Name: "hidden", Description: "manual", File: hidden, Scope: ScopeProject, ModelInvocable: false},
		{Name: "huge", Description: long, File: huge, Scope: ScopeProject, ModelInvocable: true},
		{Name: "kk:agent-browser", Description: "browser", File: colon, Scope: ScopeProject, ModelInvocable: true},
		{Name: strings.Repeat("ü", 64), Description: "d", File: cut, Scope: ScopeProject, ModelInvocable: true},
	}, got.Skills)
}

func TestLoadRepairsYAML(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Build for AWS: ECS":     "description: Build for AWS: ECS",
		"[WIP] notes":            "description: [WIP] notes",
		"@scope/pkg helper":      "description: @scope/pkg helper",
		"`go` tests":             "description: `go` tests",
		`say "hi" \ there: now`:  `description: say "hi" \ there: now`,
		"plain text stays as is": "description: plain text stays as is",
	}
	for want, line := range cases {
		root := t.TempDir()
		file := write(t, filepath.Join(root, "demo", "SKILL.md"), "---\n"+line+"\nname: demo\n---\n")
		got := Load([]Root{{Dir: root, Scope: ScopeUser}})
		assert.Empty(t, got.Diagnostics, line)
		require.Len(t, got.Skills, 1, line)
		assert.Equal(t, Skill{Name: "demo", Description: want, File: file, Scope: ScopeUser, ModelInvocable: true}, got.Skills[0])
	}
}

func TestLoadKeepsSameNameFromBothRootsProjectFirst(t *testing.T) {
	t.Parallel()
	user, project := t.TempDir(), t.TempDir()
	u := write(t, filepath.Join(user, "one", "SKILL.md"), skillMD("shared", "from user"))
	p := write(t, filepath.Join(project, "two", "SKILL.md"), skillMD("shared", "from project"))

	got := Load([]Root{{Dir: user, Scope: ScopeUser}, {Dir: project, Scope: ScopeProject}})
	assert.Empty(t, got.Diagnostics)
	assert.Equal(t, []Skill{
		{Name: "shared", Description: "from project", File: p, Scope: ScopeProject, ModelInvocable: true},
		{Name: "shared", Description: "from user", File: u, Scope: ScopeUser, ModelInvocable: true},
	}, got.Skills)
}

func TestLoadListsOneSkillReachedByTwoRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "SKILL.md"), skillMD("a", "d"))
	got := Load([]Root{{Dir: root, Scope: ScopeUser}, {Dir: filepath.Join(root, "a"), Scope: ScopeProject}})
	assert.Len(t, got.Skills, 1)
}

func TestLoadSkipsHiddenFoldersBelowTheRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, ".system", "x", "SKILL.md"), skillMD("hidden", "d"))
	write(t, filepath.Join(root, "ok", "SKILL.md"), skillMD("ok", "d"))
	hiddenRoot := filepath.Join(root, ".agents", "skills")
	write(t, filepath.Join(hiddenRoot, "in", "SKILL.md"), skillMD("in", "d"))

	got := Load([]Root{{Dir: root, Scope: ScopeUser}, {Dir: hiddenRoot, Scope: ScopeUser}})
	names := []string{}
	for _, s := range got.Skills {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"in", "ok"}, names)
}

func TestLoadStopsAtTheDirectoryLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 20 {
		require.NoError(t, os.Mkdir(filepath.Join(root, fmt.Sprintf("d%02d", i)), 0o755))
	}
	write(t, filepath.Join(root, "zzz", "SKILL.md"), skillMD("late", "d"))
	write(t, filepath.Join(root, "d00", "SKILL.md"), skillMD("early", "d"))

	got := load([]Root{{Dir: root, Scope: ScopeUser}}, limits{depth: 6, dirs: 10, entries: 1000})
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "early", got.Skills[0].Name)
	assert.Empty(t, got.Diagnostics)
}

func TestLoadStopsAtTheEntryLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 20 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d", i)), nil, 0o644))
	}
	write(t, filepath.Join(root, "zzz", "SKILL.md"), skillMD("late", "d"))

	got := load([]Root{{Dir: root, Scope: ScopeUser}}, limits{depth: 6, dirs: 1000, entries: 10})
	assert.Empty(t, got.Skills)
	assert.Empty(t, got.Diagnostics)
	assert.Len(t, load([]Root{{Dir: root, Scope: ScopeUser}}, walkLimits).Skills, 1)
}

func TestLoadSkillFolderIsNotEnteredFurther(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "outer", "SKILL.md"), skillMD("outer", "d"))
	write(t, filepath.Join(root, "outer", "inner", "SKILL.md"), skillMD("inner", "d"))

	got := Load([]Root{{Dir: root, Scope: ScopeUser}})
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "outer", got.Skills[0].Name)
	assert.Empty(t, got.Diagnostics)
}

func TestLoadDepthLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	deepest := write(t, filepath.Join(root, "a", "b", "c", "d", "e", "f", "SKILL.md"), skillMD("depth-six", "d"))
	write(t, filepath.Join(root, "a", "b", "c", "d", "e", "f", "g", "SKILL.md"), skillMD("depth-seven", "d"))

	got := Load([]Root{{Dir: root, Scope: ScopeUser}})
	require.Len(t, got.Skills, 1)
	assert.Equal(t, deepest, got.Skills[0].File)
}

func TestLoadSymlinks(t *testing.T) {
	t.Parallel()
	root, elsewhere := t.TempDir(), t.TempDir()
	write(t, filepath.Join(elsewhere, "SKILL.md"), skillMD("linked", "via link"))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "link")))
	write(t, filepath.Join(root, "real", "SKILL.md"), skillMD("real", "d"))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "real", "..", "loop")))
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")))

	got := Load([]Root{{Dir: root, Scope: ScopeUser}})
	names := []string{}
	for _, s := range got.Skills {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"linked", "real"}, names)
	assert.Empty(t, got.Diagnostics)
}

func TestLoadMissingRootAndDeterminism(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, n := range []string{"zeta", "alpha", "mid"} {
		write(t, filepath.Join(root, n, "SKILL.md"), skillMD(n, "d"))
	}
	write(t, filepath.Join(root, "bad", "SKILL.md"), "nope")
	roots := []Root{{Dir: filepath.Join(root, "missing"), Scope: ScopeProject}, {Dir: root, Scope: ScopeUser}}

	first, second := Load(roots), Load(roots)
	assert.Equal(t, first, second)
	require.Len(t, first.Skills, 3)
	assert.Equal(t, "alpha", first.Skills[0].Name)
	assert.Len(t, first.Diagnostics, 1)
}
