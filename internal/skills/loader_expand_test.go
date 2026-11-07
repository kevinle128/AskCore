package skills

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadOne(t *testing.T, dir, content string) (Result, string) {
	t.Helper()
	root := t.TempDir()
	file := write(t, filepath.Join(root, dir, "SKILL.md"), content)
	res := Load([]Root{{Dir: root, Scope: ScopeUser}})
	require.Len(t, res.Skills, 1)
	return res, file
}

func TestExpandLoadsTheSkillBodyWithoutFrontmatter(t *testing.T) {
	t.Parallel()
	res, file := loadOne(t, "demo", "---\nname: demo\ndescription: A demo\n---\n\nDo the demo steps.\nSecond line.\n")
	got, ok := Expand("/skill:demo fix the parser\nthen test it", res)
	require.True(t, ok)
	want := "<skill name=\"demo\" location=\"" + file + "\">\nReferences are relative to " + filepath.Dir(file) + ".\n\nDo the demo steps.\nSecond line.\n</skill>\n\nfix the parser\nthen test it"
	assert.Equal(t, want, got)
	assert.NotContains(t, got, "description: A demo")
}

func TestExpandWithoutArgumentsEndsAtTheBlock(t *testing.T) {
	t.Parallel()
	res, _ := loadOne(t, "demo", skillMD("demo", "d"))
	got, ok := Expand("/skill:demo", res)
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(got, "body\n</skill>"), got)
	got, ok = Expand("/skill:demo   \n", res)
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(got, "body\n</skill>"), got)
}

func TestExpandLeavesOtherTextAlone(t *testing.T) {
	t.Parallel()
	res, _ := loadOne(t, "demo", skillMD("demo", "d"))
	for _, text := range []string{"/skill:nope hi", "/skill:", "/skill: demo", "/skill:dem", "/skill:Demo", "hello /skill:demo", " /skill:demo", "/skills:demo", "plain text", ""} {
		got, ok := Expand(text, res)
		assert.False(t, ok, text)
		assert.Empty(t, got, text)
	}
}

func TestExpandLoadsASkillThatTheModelCannotSee(t *testing.T) {
	t.Parallel()
	res, _ := loadOne(t, "quiet", "---\ndescription: manual only\ndisable-model-invocation: true\n---\nManual steps.\n")
	require.False(t, res.Skills[0].ModelInvocable)
	got, ok := Expand("/skill:quiet go", res)
	require.True(t, ok)
	assert.Contains(t, got, "Manual steps.")
}

func TestExpandCutsALongBodyOnARuneBoundary(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("€", 60<<10)
	res, _ := loadOne(t, "big", "---\ndescription: big\n---\n"+body)
	got, ok := Expand("/skill:big go", res)
	require.True(t, ok)
	require.True(t, utf8.ValidString(got))
	start := strings.Index(got, "\n\n") + 2
	end := strings.Index(got, "\n\n[The skill text is cut here at 100 KB")
	require.Positive(t, end, "the note is visible")
	assert.Equal(t, 100<<10-1, end-start, "102400 is not a multiple of 3, so the cut moves back one byte")
	assert.True(t, strings.HasSuffix(got, "\n</skill>\n\ngo"))
}

func TestExpandCapsAThreeHundredKilobyteBody(t *testing.T) {
	t.Parallel()
	res, _ := loadOne(t, "big", "---\ndescription: big\n---\n"+strings.Repeat("0123456789", 30<<10))
	got, ok := Expand("/skill:big", res)
	require.True(t, ok)
	assert.Less(t, len(got), 101<<10+1024)
	assert.Contains(t, got, "cut here at 100 KB")
}

func TestExpandKeepsABodyAtTheCapWhole(t *testing.T) {
	t.Parallel()
	res, _ := loadOne(t, "fit", "---\ndescription: fit\n---\n"+strings.Repeat("a", 100<<10))
	got, ok := Expand("/skill:fit", res)
	require.True(t, ok)
	assert.NotContains(t, got, "cut here")
}

func TestExpandPrefersTheFirstSkillOfAName(t *testing.T) {
	t.Parallel()
	user := t.TempDir()
	project := t.TempDir()
	write(t, filepath.Join(user, "same", "SKILL.md"), "---\ndescription: user\n---\nUSER BODY\n")
	write(t, filepath.Join(project, "same", "SKILL.md"), "---\ndescription: project\n---\nPROJECT BODY\n")
	res := Load([]Root{{Dir: user, Scope: ScopeUser}, {Dir: project, Scope: ScopeProject}})
	got, ok := Expand("/skill:same", res)
	require.True(t, ok)
	assert.Contains(t, got, "PROJECT BODY")
}

func TestExpandKeepsTheTextWhenTheFileIsGone(t *testing.T) {
	t.Parallel()
	res, file := loadOne(t, "demo", skillMD("demo", "d"))
	write(t, file, "no frontmatter now\n")
	_, ok := Expand("/skill:demo", res)
	assert.False(t, ok)
}
