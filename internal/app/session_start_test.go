package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/bootstrap"
	"AskCore/internal/tools"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

type sessionTree struct {
	home, userHome, root string
}

// newSessionTree makes an Ask home, a user home and a project folder with the
// given files. A name that ends in "/" is a folder.
func newSessionTree(t *testing.T, project, user map[string]string) sessionTree {
	t.Helper()
	canon := func() string {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		return dir
	}
	tree := sessionTree{home: filepath.Join(canon(), "ask-home"), userHome: canon(), root: canon()}
	require.NoError(t, os.Mkdir(tree.home, 0o700))
	put := func(base string, files map[string]string, mode os.FileMode) {
		for name, content := range files {
			path := filepath.Join(base, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte(content), mode))
		}
	}
	put(tree.root, project, 0o644)
	put(tree.home, user, 0o600)
	return tree
}

func (s sessionTree) prepare(t *testing.T, trust workspace.Override, flags protocol.ACPSessionOptions) SessionStart {
	t.Helper()
	start, err := PrepareSession(context.Background(), SessionInput{
		Home: s.home, UserHome: s.userHome, Cwd: s.root, Trust: trust, Flags: flags,
		Now: func() time.Time { return time.Date(2026, 11, 4, 23, 59, 0, 0, time.Local) },
	})
	require.NoError(t, err)
	return start
}

func fileAndEcho() []tools.Tool {
	return append(tools.NewFileTools(tools.NewFileState(), nil), tools.Echo{})
}

func toolNames(ts []tools.Tool) []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Decl().Name
	}
	return names
}

func skillFile(name string) string {
	return "---\nname: " + name + "\ndescription: " + name + " skill\n---\n"
}

func registryOf(t *testing.T, ts ...tools.Tool) *tools.Snapshot {
	t.Helper()
	var reg tools.Registry
	for _, tool := range ts {
		require.NoError(t, reg.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	}
	return reg.Snapshot()
}

func TestPrepareSessionGatesProjectFilesOnTrustButNotContextFiles(t *testing.T) {
	tree := newSessionTree(t, map[string]string{
		"AGENTS.md":                         "agents",
		".ask/SYSTEM.md":                    "project system",
		".ask/skills/proj/SKILL.md":         skillFile("proj"),
		".agents/skills/dotagents/SKILL.md": skillFile("dotagents"),
	}, map[string]string{"skills/home/SKILL.md": skillFile("home")})

	untrusted := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	assert.Empty(t, untrusted.Prompt.Custom)
	require.Len(t, untrusted.Context.Files, 1)
	assert.Equal(t, filepath.Join(tree.root, "AGENTS.md"), untrusted.Context.Files[0].Path)
	require.Len(t, untrusted.Skills.Skills, 1)
	assert.Equal(t, "home", untrusted.Skills.Skills[0].Name)
	assert.Equal(t, "2026-11-04", untrusted.Date)

	trusted := tree.prepare(t, workspace.OverrideApprove, protocol.ACPSessionOptions{})

	assert.Equal(t, "project system", trusted.Prompt.Custom)
	var names []string
	for _, s := range trusted.Skills.Skills {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"dotagents", "proj", "home"}, names)
}

func TestSessionSkillsFollowTheTrustDecisionForRowsAndExpansion(t *testing.T) {
	tree := newSessionTree(t, map[string]string{
		".ask/skills/proj/SKILL.md": "---\ndescription: project skill\n---\nPROJECT BODY\n",
	}, map[string]string{"skills/home/SKILL.md": "---\ndescription: home skill\ndisable-model-invocation: true\n---\nHOME BODY\n"})

	untrusted := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})
	assert.Equal(t, []protocol.ACPSkill{
		{Name: "home", Description: "home skill", Path: filepath.Join(tree.home, "skills", "home", "SKILL.md"), Scope: protocol.ACPSkillUser},
	}, untrusted.SkillRows())
	_, ok := untrusted.ExpandInput("/skill:proj go")
	assert.False(t, ok, "an untrusted project loads no skill, so its name is plain text")
	got, ok := untrusted.ExpandInput("/skill:home go")
	require.True(t, ok)
	assert.Contains(t, got, "HOME BODY", "a skill that the model cannot invoke expands")

	trusted := tree.prepare(t, workspace.OverrideApprove, protocol.ACPSessionOptions{})
	rows := trusted.SkillRows()
	require.Len(t, rows, 2)
	assert.Equal(t, protocol.ACPSkillProject, rows[0].Scope)
	assert.Equal(t, "proj", rows[0].Name)
	assert.True(t, rows[0].ModelInvocable)
	got, ok = trusted.ExpandInput("/skill:proj go")
	require.True(t, ok)
	assert.Contains(t, got, "PROJECT BODY")

	assert.Empty(t, SessionStart{}.SkillRows())
}

func TestPrepareSessionFlagsBeatDefaultToolsAndReplacePromptFiles(t *testing.T) {
	tree := newSessionTree(t, nil, map[string]string{
		"settings.json":    `{"defaultTools":["read","ls"]}`,
		"SYSTEM.md":        "user system",
		"APPEND_SYSTEM.md": "user append",
	})

	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})
	assert.Equal(t, "user system", start.Prompt.Custom)
	assert.Equal(t, []string{"user append"}, start.Prompt.Append)
	kept, err := start.Tools.Resolve(fileAndEcho())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"read", "ls"}, toolNames(kept))

	start = tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{SystemPrompt: "flag system", AppendSystemPrompt: []string{"flag append"}, Tools: []string{"echo"}})
	assert.Equal(t, "flag system", start.Prompt.Custom)
	assert.Equal(t, bootstrap.FlagSource, start.Prompt.CustomFrom)
	assert.Equal(t, []string{"user append", "flag append"}, start.Prompt.Append)
	kept, err = start.Tools.Resolve(fileAndEcho())
	require.NoError(t, err)
	assert.Equal(t, []string{"echo"}, toolNames(kept))

	_, err = PrepareSession(context.Background(), SessionInput{Home: tree.home, UserHome: tree.userHome, Cwd: tree.root, Flags: protocol.ACPSessionOptions{Tools: []string{"read"}, NoTools: true}})
	require.ErrorIs(t, err, ErrSessionFlags)
}

func TestSessionRenderSeedsContextAndSkillsOnlyWhenTheyCanBeUsed(t *testing.T) {
	tree := newSessionTree(t, map[string]string{"AGENTS.md": "agents"}, map[string]string{"skills/home/SKILL.md": skillFile("home")})
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{AppendSystemPrompt: []string{"extra"}})
	fileTools := tools.NewFileTools(tools.NewFileState(), nil)

	res := start.Render(registryOf(t, fileTools...), 0)
	prompt, seed := res.SystemPrompt, res.Seed

	require.Len(t, seed, 2)
	assert.Equal(t, protocol.CustomContextFiles, seed[0].CustomType)
	assert.Equal(t, protocol.CustomSkillList, seed[1].CustomType)
	for _, item := range seed {
		assert.False(t, item.Display)
		assert.Zero(t, item.Timestamp)
		require.Len(t, item.Content, 1)
	}
	assert.Contains(t, prompt, "Available tools:")
	assert.Contains(t, prompt, "extra")
	assert.True(t, strings.HasSuffix(prompt, "Current working directory: "+tree.root+"\n\nCurrent date: 2026-11-04"), prompt)

	res = start.Render(registryOf(t, tools.Echo{}), 0)

	require.Len(t, res.Seed, 1, "no read or bash tool, so no skill list")
	assert.Equal(t, protocol.CustomContextFiles, res.Seed[0].CustomType)
	again := start.Render(registryOf(t, tools.Echo{}), 0)
	assert.Equal(t, res, again, "Render is pure")
}

func TestSessionWarningsSummarizeSkippedContextFilesAndCapSkillProblems(t *testing.T) {
	user := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		user["skills/"+n+"/SKILL.md"] = "no frontmatter"
	}
	tree := newSessionTree(t, map[string]string{"AGENTS.md": strings.Repeat("x", bootstrap.MaxFileBytes+1)}, user)
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	lines := start.Warnings()

	require.Len(t, lines, 6)
	assert.Equal(t, "Warning: 1 context file was skipped; run ask settings show for details", lines[0])
	for i, n := range []string{"a", "b", "c", "d"} {
		assert.Equal(t, "Warning: skills: "+filepath.Join(tree.home, "skills", n, "SKILL.md")+": no frontmatter", lines[1+i])
	}
	assert.Equal(t, "Warning: skills: 2 more skill problems; run ask settings show for details", lines[5])
	assert.Len(t, start.SkillDiagnostics(), 6, "the full list stays available")

	diags := sessionTrust(start).Diagnostics
	require.Len(t, diags, 6, "one context row, four skill rows, one more row")
	assert.Equal(t, protocol.ACPDiagnostic{Layer: "project", Path: filepath.Join(tree.root, "AGENTS.md"), Key: "context", Text: "left out: the file is over 1 MiB"}, diags[0])
	assert.Contains(t, diags, protocol.ACPDiagnostic{Layer: "user", Path: filepath.Join(tree.home, "skills", "a", "SKILL.md"), Key: "skills", Text: "no frontmatter"})
	assert.Equal(t, protocol.ACPDiagnostic{Layer: "user", Key: "skills", Text: "2 more skill problems; run ask settings show for details"}, diags[5])
}

func TestSessionWarningsCutALongSkillPathAndCountOneMore(t *testing.T) {
	user := map[string]string{}
	for _, n := range []string{strings.Repeat("é", 120), "b", "c", "d", "e"} {
		user["skills/"+n+"/SKILL.md"] = "no frontmatter"
	}
	tree := newSessionTree(t, nil, user)
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	lines := start.Warnings()

	require.Len(t, lines, 5)
	for _, line := range lines[:4] {
		assert.LessOrEqual(t, len(line), 256, "the prefix counts")
		assert.True(t, utf8.ValidString(line))
	}
	assert.Equal(t, "Warning: skills: 1 more skill problem; run ask settings show for details", lines[4])
}

func TestSessionRenderBudgetsTheSkillListByTheModelWindow(t *testing.T) {
	user := map[string]string{}
	for i := range 100 {
		user[fmt.Sprintf("skills/skill-%03d/SKILL.md", i)] = "---\ndescription: " + strings.Repeat("use it when needed ", 20) + "\n---\n"
	}
	tree := newSessionTree(t, nil, user)
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})
	reg := registryOf(t, tools.NewFileTools(tools.NewFileState(), nil)...)

	roomy := start.Render(reg, 4_000_000)
	assert.Empty(t, roomy.Warnings)
	assert.NotContains(t, roomy.Seed[0].Content[0].(protocol.Text).Text, "omitted from this bounded skills list")

	tiny := start.Render(reg, 10_000)
	require.Len(t, tiny.Warnings, 1)
	assert.Regexp(t, `^Warning: Exceeded skills context budget\. All skill descriptions were removed and \d+ additional skills were not included in the model-visible skills list\.$`, tiny.Warnings[0])
	assert.Contains(t, tiny.Seed[0].Content[0].(protocol.Text).Text, "omitted from this bounded skills list.")

	start.Settings.SkillsMaxContextTokens = 100_000
	assert.Empty(t, start.Render(reg, 10_000).Warnings, "the setting replaces the window percentage, capped at 10000 tokens")
}

func TestStartErrorMapsClientCausedFailuresToInvalidParams(t *testing.T) {
	err := startError(&tools.SelectionError{Origin: "--tools", Name: "nope", Known: []string{"read", "ls"}})
	assert.EqualError(t, err, "invalid_params: --tools: unknown tool nope; known tools: read, ls")

	err = startError(ErrSessionFlags)
	assert.ErrorContains(t, err, "invalid_params")

	plain := os.ErrPermission
	assert.Equal(t, plain, startError(plain), "another error keeps its kind")
}

func TestSessionWarningsKeepEachSkillLineWithinTheByteLimitIncludingThePrefix(t *testing.T) {
	// A multibyte name makes a cut inside a rune possible at any byte offset.
	user := map[string]string{"skills/" + strings.Repeat("ü", 200) + "/SKILL.md": "no frontmatter"}
	tree := newSessionTree(t, nil, user)
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	lines := start.Warnings()

	require.Len(t, lines, 1)
	assert.LessOrEqual(t, len(lines[0]), 256)
	assert.True(t, utf8.ValidString(lines[0]), "the cut falls on a rune boundary")
	assert.True(t, strings.HasSuffix(lines[0], ": no frontmatter"))
	rows := start.cappedSkillDiagnostics()
	require.Len(t, rows, 1)
	assert.LessOrEqual(t, len(rows[0].Path)+len(": ")+len(rows[0].Text), 256, "a row without a prefix keeps its larger room")
}

func TestContextLayerIsUserOnlyForAFileDirectlyInTheAskHome(t *testing.T) {
	tree := newSessionTree(t, nil, map[string]string{"AGENTS.md": "user rules"})
	work := filepath.Join(tree.home, "work")
	require.NoError(t, os.Mkdir(work, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "AGENTS.md"), []byte(strings.Repeat("x", bootstrap.MaxFileBytes+1)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tree.home, "CLAUDE.md"), []byte(strings.Repeat("x", bootstrap.MaxFileBytes+1)), 0o600))
	tree.root = work

	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	layers := map[string]string{}
	for _, d := range start.contextDiagnostics() {
		layers[d.Path] = d.Layer
	}
	assert.Equal(t, "project", layers[filepath.Join(work, "AGENTS.md")], "a project below the Ask home is a project")
	assert.Equal(t, "user", layers[filepath.Join(tree.home, "CLAUDE.md")])
}

func TestContextDiagnosticsReportAFileLinkedOutsideTheProject(t *testing.T) {
	start := SessionStart{Context: bootstrap.Context{Skipped: []bootstrap.Skip{{Path: "/p/AGENTS.md", Reason: bootstrap.SkipOutsideProject}}}}

	diags := start.contextDiagnostics()

	require.Equal(t, []protocol.ACPDiagnostic{{Layer: "project", Path: "/p/AGENTS.md", Key: "context", Text: "left out: the file links to a path outside the project"}}, diags)
	assert.Equal(t, []string{"Warning: 1 context file was skipped; run ask settings show for details"}, start.Warnings())
}
