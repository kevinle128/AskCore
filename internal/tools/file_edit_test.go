package tools_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

type editArgs struct {
	Path  string     `json:"path"`
	Edits []editPair `json:"edits"`
}

type editPair struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

func edit(path string, pairs ...string) editArgs {
	a := editArgs{Path: path}
	for i := 0; i+1 < len(pairs); i += 2 {
		a.Edits = append(a.Edits, editPair{pairs[i], pairs[i+1]})
	}
	return a
}

func TestEditExactUniqueMatch(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "alpha\nbeta\ngamma\ndelta\n")

	res := h.mustRun("edit", edit("a.txt", "beta", "BETA", "delta\n", "DELTA\n"))

	assert.Equal(t, "Successfully replaced 2 block(s) in a.txt.", resultText(t, res))
	assert.Equal(t, "alpha\nBETA\ngamma\nDELTA\n", h.content("a.txt"))
}

func TestEditRejectsMultipleMatchesWithPreviews(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "x := 1\ny := 2\nx := 1\nz := 3\nx := 1\n")

	_, err := h.run("edit", edit("a.txt", "x := 1", "x := 9"))

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "Found 3 occurrences of the text in a.txt. The text must be unique. Please provide more context to make it unique.")
	for _, line := range []int{1, 3, 5} {
		assert.Contains(t, msg, fmt.Sprintf("line %d: x := 1", line))
	}
	assert.Equal(t, "x := 1\ny := 2\nx := 1\nz := 3\nx := 1\n", h.content("a.txt"), "a refused edit changes nothing")
}

func TestEditNamesTheEditThatIsAmbiguous(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "one\ntwo\ntwo\n")

	_, err := h.run("edit", edit("a.txt", "one", "1", "two", "2"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Found 2 occurrences of edits[1] in a.txt. Each oldText must be unique.")
}

func TestEditMatchesAfterWhitespaceAndUnicodeNormalization(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "title = “Hello”   \nname = it’s here\t\nkeep\n")

	res := h.mustRun("edit", edit("a.txt",
		"title = \"Hello\"\nname = it's here",
		"title = \"Bye\"\nname = ok"))

	assert.Equal(t, "Successfully replaced 1 block(s) in a.txt.", resultText(t, res))
	assert.Equal(t, "title = \"Bye\"\nname = ok\nkeep\n", h.content("a.txt"))
}

func TestEditFuzzyMatchReindentsReplacement(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.go", "func f() {\n\tif ok {\n\t\treturn 1\n\t}\n}\n")

	// The model sent the block without its indentation.
	h.mustRun("edit", edit("a.go", "if ok {\n\treturn 1\n}", "if ok {\n\treturn 2\n}"))

	assert.Equal(t, "func f() {\n\tif ok {\n\t\treturn 2\n\t}\n}\n", h.content("a.go"))
}

func TestEditNearDuplicateBlocksNeedUniqueMatch(t *testing.T) {
	h := newFileHarness(t, nil)
	block := func(name string) string {
		return fmt.Sprintf("func %s() {\n\tvalue := loadConfiguration(path)\n\tprocess(value)\n\treturn\n}\n", name)
	}
	original := block("alpha") + "\n" + block("alphb")
	h.put("a.go", original)

	_, err := h.run("edit", edit("a.go",
		"func alphc() {\n\tvalue := loadConfiguration(path)\n\tprocess(value)\n\treturn\n}",
		"func renamed() {}"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Found 2 occurrences")
	assert.Equal(t, original, h.content("a.go"))
}

func TestEditNotFoundNamesClosestMatch(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "one\ntwo\nthree\nfour\nconfig.timeout = 30\nsix\n")

	_, err := h.run("edit", edit("a.txt", "config.timeout = 300000000", "config.timeout = 60"))

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "Could not find the exact text in a.txt. The old text must match exactly including all whitespace and newlines.")
	assert.Regexp(t, `Closest match \(\d+% similar\) at line 5:`, msg)
	assert.Contains(t, msg, "config.timeout = 30")
}

func TestEditRejectsOverlappingEdits(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "abcdefgh\n")

	_, err := h.run("edit", edit("a.txt", "abcdef", "X", "cdefgh", "Y"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "edits[0] and edits[1] overlap in a.txt. Merge them into one edit or target disjoint regions.")
	assert.Equal(t, "abcdefgh\n", h.content("a.txt"))
}

func TestEditRejectsEmptyOldText(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "abc\n")

	_, err := h.run("edit", edit("a.txt", "", "X"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "oldText must not be empty in a.txt.")
}

func TestEditRejectsNoOpAndStopsAfterThree(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "abc\n")
	noop := edit("a.txt", "abc", "abc")

	for i := 0; i < 2; i++ {
		_, err := h.run("edit", noop)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "identical")
		assert.NotContains(t, err.Error(), "Read a.txt again")
	}
	_, err := h.run("edit", noop)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "third time")
	assert.Contains(t, err.Error(), "Read a.txt again")

	// A real change of the file clears the count.
	h.mustRun("edit", edit("a.txt", "abc", "abd"))
	_, err = h.run("edit", edit("a.txt", "abd", "abd"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "third time")
}

func TestEditKeepsCRLFAndBOM(t *testing.T) {
	h := newFileHarness(t, nil)
	original := "\xef\xbb\xbfone\r\ntwo\r\nthree\r\n"
	h.put("a.txt", original)

	h.mustRun("edit", edit("a.txt", "two\nthree", "2\n3\n4"))

	assert.Equal(t, "\xef\xbb\xbfone\r\n2\r\n3\r\n4\r\n", h.content("a.txt"))
}

func TestEditKeepsLFWhenFileHasNoCRLF(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "one\ntwo\n")

	h.mustRun("edit", edit("a.txt", "one", "1"))

	assert.Equal(t, "1\ntwo\n", h.content("a.txt"))
}

func TestEditRejectsStaleFile(t *testing.T) {
	h := newFileHarness(t, nil)
	p := h.put("a.txt", "alpha\nbeta\n")
	h.mustRun("read", map[string]any{"path": "a.txt"})

	// Something else changes the file after the read.
	require.NoError(t, os.WriteFile(p, []byte("alpha\nbeta\nmore\n"), 0o644))
	_, err := h.run("edit", edit("a.txt", "beta", "BETA"))

	require.Error(t, err)
	assert.EqualError(t, err, "a.txt changed on disk since you last read it. Read it again before you edit.")
	assert.Equal(t, "alpha\nbeta\nmore\n", h.content("a.txt"))

	// Reading again clears the error.
	h.mustRun("read", map[string]any{"path": "a.txt"})
	h.mustRun("edit", edit("a.txt", "beta", "BETA"))
	assert.Equal(t, "alpha\nBETA\nmore\n", h.content("a.txt"))
}

func TestEditRejectsFileThatChangedOnlyInModificationTime(t *testing.T) {
	h := newFileHarness(t, nil)
	p := h.put("a.txt", "alpha\n")
	h.mustRun("read", map[string]any{"path": "a.txt"})

	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(p, future, future))
	_, err := h.run("edit", edit("a.txt", "alpha", "beta"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed on disk since you last read it")
}

func TestEditAllowsFileNeverRead(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "alpha\n")

	h.mustRun("edit", edit("a.txt", "alpha", "beta"))

	assert.Equal(t, "beta\n", h.content("a.txt"))
}

func TestEditRecordsItsOwnWriteSoTheNextEditPasses(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "alpha\nbeta\n")
	h.mustRun("read", map[string]any{"path": "a.txt"})

	h.mustRun("edit", edit("a.txt", "alpha", "ALPHA"))
	h.mustRun("edit", edit("a.txt", "beta", "BETA"))

	assert.Equal(t, "ALPHA\nBETA\n", h.content("a.txt"))
}

func TestEditMissingFileGivesPathInError(t *testing.T) {
	h := newFileHarness(t, nil)

	_, err := h.run("edit", edit("nope.txt", "a", "b"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not edit file: nope.txt.")
}

func TestEditPreparerAcceptsLegacyShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"top-level oldText and newText", `{"path":"a.txt","oldText":"beta","newText":"BETA"}`},
		{"edits as a JSON string", `{"path":"a.txt","edits":"[{\"oldText\":\"beta\",\"newText\":\"BETA\"}]"}`},
		{"edits as one object", `{"path":"a.txt","edits":{"oldText":"beta","newText":"BETA"}}`},
		{"edits as a string holding one object", `{"path":"a.txt","edits":"{\"oldText\":\"beta\",\"newText\":\"BETA\"}"}`},
		{"top-level pair next to edits", `{"path":"a.txt","edits":[],"oldText":"beta","newText":"BETA"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFileHarness(t, nil)
			h.put("a.txt", "alpha\nbeta\n")

			res := h.mustRun("edit", c.raw)

			assert.Equal(t, "Successfully replaced 1 block(s) in a.txt.", resultText(t, res))
			assert.Equal(t, "alpha\nBETA\n", h.content("a.txt"))
		})
	}
}

func TestEditPreparerKeepsCurrentShapeAndLeavesBadInput(t *testing.T) {
	h := newFileHarness(t, nil)
	p, ok := h.set["edit"].(tools.ArgumentPreparer)
	require.True(t, ok)

	cur := `{"path":"a","edits":[{"oldText":"x","newText":"y"}]}`
	got, err := p.PrepareArguments(json.RawMessage(cur))
	require.NoError(t, err)
	assert.JSONEq(t, cur, string(got))

	// A string that is not JSON is left for validation to report.
	got, err = p.PrepareArguments(json.RawMessage(`{"path":"a","edits":"not json"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"path":"a","edits":"not json"}`, string(got))
}

func TestEditDetailsCarryWholeFileDiffAndLocations(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "l1\nl2\nl3\nl4\nl5\n")

	res := h.mustRun("edit", edit("a.txt", "l2", "L2\nextra", "l5", "L5"))

	d := resultDetails(t, res)
	assert.Equal(t, protocol.ToolKindEdit, d.Kind)
	assert.Equal(t, h.path("a.txt"), d.Path)
	require.NotNil(t, d.Diff)
	assert.Equal(t, h.path("a.txt"), d.Diff.Path)
	assert.Equal(t, "l1\nl2\nl3\nl4\nl5\n", d.Diff.OldText)
	assert.Equal(t, "l1\nL2\nextra\nl3\nl4\nL5\n", d.Diff.NewText)
	assert.Equal(t, []protocol.ToolLocation{{Path: h.path("a.txt"), Line: 2}, {Path: h.path("a.txt"), Line: 6}}, d.Locations)
}

func TestEditDeclDescribesPiSchema(t *testing.T) {
	h := newFileHarness(t, nil)
	var decl struct {
		Parameters struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"parameters"`
	}
	b, err := json.Marshal(h.set["edit"].Decl())
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &decl))
	assert.ElementsMatch(t, []string{"path", "edits"}, decl.Parameters.Required)
	assert.Contains(t, decl.Parameters.Properties, "edits")
}

func TestEditHasPromptContribution(t *testing.T) {
	h := newFileHarness(t, nil)
	pc, ok := h.set["edit"].(tools.PromptContributor)
	require.True(t, ok)
	assert.NotEmpty(t, pc.PromptSnippet())
	assert.NotEmpty(t, pc.PromptGuidelines())
	assert.False(t, strings.Contains(pc.PromptSnippet(), "\n"))
}
