package main

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

const demoSkill = "---\nname: demo\ndescription: A demo skill\n---\n\nDEMO STEPS\n"

func TestPrintSkillCommandSendsTheSkillBlockAndTheArguments(t *testing.T) {
	root := trustProject(t, nil)
	userHome := canonicalDir(t, t.TempDir())
	file := writeRawSkill(t, filepath.Join(userHome, ".agents", "skills"), "demo", demoSkill)

	m, got := sessionRun{cwd: root, userHome: userHome, prompt: "/skill:demo fix the parser"}.run(t)

	require.Equal(t, 0, got.code, got.stderr)
	user := userBlocks(t, m)
	require.Len(t, user, 2, "the skill list and the prompt")
	assert.Equal(t, "<skill name=\"demo\" location=\""+file+"\">\nReferences are relative to "+filepath.Dir(file)+".\n\nDEMO STEPS\n</skill>\n\nfix the parser", user[1])
	assert.NotContains(t, user[1], "description: A demo skill", "the frontmatter is not sent")
}

func TestPrintSkillCommandLoadsASkillThatTheModelDoesNotSee(t *testing.T) {
	root := trustProject(t, nil)
	userHome := canonicalDir(t, t.TempDir())
	writeRawSkill(t, filepath.Join(userHome, ".agents", "skills"), "quiet", "---\ndescription: manual only\ndisable-model-invocation: true\n---\nQUIET STEPS\n")

	m, got := sessionRun{cwd: root, userHome: userHome, prompt: "/skill:quiet now"}.run(t)

	require.Equal(t, 0, got.code, got.stderr)
	user := userBlocks(t, m)
	assert.NotContains(t, user[0], "<name>quiet</name>", "the model list leaves it out")
	assert.Contains(t, user[len(user)-1], "QUIET STEPS")
}

func TestPrintUnknownSkillNameIsSentAsTyped(t *testing.T) {
	root := trustProject(t, nil)
	userHome := canonicalDir(t, t.TempDir())
	writeRawSkill(t, filepath.Join(userHome, ".agents", "skills"), "demo", demoSkill)

	m, got := sessionRun{cwd: root, userHome: userHome, prompt: "/skill:nope hello"}.run(t)

	require.Equal(t, 0, got.code, got.stderr)
	user := userBlocks(t, m)
	assert.Equal(t, "/skill:nope hello", user[len(user)-1])
}

func TestPrintProjectSkillExpandsOnlyForATrustedProject(t *testing.T) {
	root := trustProject(t, map[string]string{".ask/skills/proj/SKILL.md": "---\ndescription: project skill\n---\nPROJECT STEPS\n"})
	userHome := canonicalDir(t, t.TempDir())

	m, got := sessionRun{cwd: root, userHome: userHome, prompt: "/skill:proj go"}.run(t)
	require.Equal(t, 0, got.code, got.stderr)
	user := userBlocks(t, m)
	assert.Equal(t, "/skill:proj go", user[len(user)-1], "an untrusted project has no skill")

	m, got = sessionRun{cwd: root, userHome: userHome, prompt: "/skill:proj go", args: []string{"--approve"}}.run(t)
	require.Equal(t, 0, got.code, got.stderr)
	user = userBlocks(t, m)
	assert.Contains(t, user[len(user)-1], "PROJECT STEPS")
}

func TestInProcessClientListsSkillsAndExpandsThePrompt(t *testing.T) {
	root := trustProject(t, nil)
	userHome := canonicalDir(t, t.TempDir())
	t.Setenv("HOME", userHome)
	file := writeRawSkill(t, filepath.Join(userHome, ".agents", "skills"), "demo", demoSkill)
	m := scriptModel(t, say("done"))
	deps := runDependencies{
		getenv: func(k string) string {
			switch k {
			case "ASK_HOME":
				return leaderHome(t)
			case "ANTHROPIC_API_KEY":
				return "fixture-anthropic-key"
			}
			return ""
		},
		inferenceHTTP: &http.Client{Transport: m},
	}
	c, err := openInProcessClient(context.Background(), deps)
	require.NoError(t, err)
	col := collect(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	_, err = c.NewSession(ctx, root, tui.NewSessionOptions{})
	require.NoError(t, err)

	list, err := c.Skills(ctx)
	require.NoError(t, err)
	assert.Equal(t, []protocol.ACPSkill{{Name: "demo", Description: "A demo skill", Path: file, Scope: protocol.ACPSkillUser, ModelInvocable: true}}, list.Skills)

	_, err = c.SetModel(ctx, anthropicModel, "", false)
	require.NoError(t, err)
	_, err = c.Prompt("/skill:demo go on")
	require.NoError(t, err)
	col.waitFor("the run settles", isType[tui.PromptDoneMsg])

	user := userBlocks(t, m)
	assert.Contains(t, user[len(user)-1], "DEMO STEPS")
	assert.True(t, strings.HasSuffix(user[len(user)-1], "\n</skill>\n\ngo on"), user[len(user)-1])
	require.NoError(t, c.Close())
}

func TestACPSessionListsSkillsAndExpandsThePrompt(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	file := writeRawSkill(t, filepath.Join(p.root, ".agents", "skills"), "demo", demoSkill)
	sid := p.start()

	var list protocol.ACPSkillsResult
	p.ok(protocol.ACPSkills, e2eSession(sid), &list)
	assert.Equal(t, []protocol.ACPSkill{{Name: "demo", Description: "A demo skill", Path: file, Scope: protocol.ACPSkillUser, ModelInvocable: true}}, list.Skills)

	require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "/skill:demo over acp")).Error)
	reply := p.text(sid)
	assert.Contains(t, reply, "DEMO STEPS", "the demo model says back what the agent got")
	assert.Contains(t, reply, "over acp")
	assert.NotContains(t, reply, "/skill:demo")
}
