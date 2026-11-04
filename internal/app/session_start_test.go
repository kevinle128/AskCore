package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	assert.Equal(t, []string{"dotagents", "home", "proj"}, names)
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

	prompt, seed := start.Render(registryOf(t, fileTools...))

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

	prompt, seed = start.Render(registryOf(t, tools.Echo{}))

	require.Len(t, seed, 1, "no read or bash tool, so no skill list")
	assert.Equal(t, protocol.CustomContextFiles, seed[0].CustomType)
	again, againSeed := start.Render(registryOf(t, tools.Echo{}))
	assert.Equal(t, prompt, again, "Render is pure")
	assert.Equal(t, seed, againSeed)
}

func TestSessionWarningsSummarizeSkippedContextFilesAndBadSkills(t *testing.T) {
	tree := newSessionTree(t, map[string]string{"AGENTS.md": strings.Repeat("x", bootstrap.MaxFileBytes+1)},
		map[string]string{"skills/broken/SKILL.md": "no frontmatter", "skills/worse/SKILL.md": "no frontmatter"})
	start := tree.prepare(t, workspace.OverrideDeny, protocol.ACPSessionOptions{})

	lines := start.Warnings()

	assert.Equal(t, []string{
		"Warning: 1 context file was skipped; run ask settings show for details",
		"Warning: 2 skills were not loaded; run ask settings show for details",
	}, lines)
	diags := sessionTrust(start).Diagnostics
	require.Len(t, diags, 3)
	assert.Equal(t, protocol.ACPDiagnostic{Layer: "project", Path: filepath.Join(tree.root, "AGENTS.md"), Key: "context", Text: "left out: the file is over 1 MiB"}, diags[0])
	assert.Contains(t, diags, protocol.ACPDiagnostic{Layer: "user", Path: filepath.Join(tree.home, "skills", "broken", "SKILL.md"), Key: "skills", Text: "no frontmatter"})
}

func TestStartErrorMapsClientCausedFailuresToInvalidParams(t *testing.T) {
	err := startError(&tools.SelectionError{Origin: "--tools", Name: "nope", Known: []string{"read", "ls"}})
	assert.EqualError(t, err, "invalid_params: --tools: unknown tool nope; known tools: read, ls")

	err = startError(ErrSessionFlags)
	assert.ErrorContains(t, err, "invalid_params")

	plain := os.ErrPermission
	assert.Equal(t, plain, startError(plain), "another error keeps its kind")
}
