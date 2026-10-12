package tools

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeForFuzzyMatch(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"trailing spaces per line", "a  \nb\t\n", "a\nb\n"},
		{"smart single quotes", "it’s ‘x’", "it's 'x'"},
		{"smart double quotes", "“hi”", `"hi"`},
		{"dashes", "a–b—c−d", "a-b-c-d"},
		{"special spaces", "a b c d　e", "a b c d e"},
		{"compatibility forms", "ﬁle …", "file ..."},
		{"last line without newline", "a   ", "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, normalizeForFuzzyMatch(c.in))
		})
	}
}

func TestLineScore(t *testing.T) {
	assert.InDelta(t, 1.0, lineScore("abc", "abc"), 1e-9)
	assert.InDelta(t, 1.0, lineScore("", ""), 1e-9)
	assert.InDelta(t, 0.0, lineScore("abc", "xyz"), 1e-9)
	assert.InDelta(t, 0.8, lineScore("abcde", "abcdx"), 1e-9)
	assert.InDelta(t, 0.0, lineScore("abc", ""), 1e-9)
}

func TestFindMatchExactIsUnique(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	start, end, rung, err := findMatch(content, "beta\n")
	require.NoError(t, err)
	assert.Equal(t, rungExact, rung)
	assert.Equal(t, "beta\n", content[start:end])
}

func TestFindMatchExactFragmentInsideLine(t *testing.T) {
	content := "call(a, b)\n"
	start, end, rung, err := findMatch(content, "a, b")
	require.NoError(t, err)
	assert.Equal(t, rungExact, rung)
	assert.Equal(t, "a, b", content[start:end])
}

func TestFindMatchMultipleListsLines(t *testing.T) {
	content := "x := 1\ny := 2\nx := 1\nz := 3\nx := 1\n"
	_, _, _, err := findMatch(content, "x := 1")
	var me *matchError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, matchMultiple, me.kind)
	assert.Equal(t, 3, me.count)
	require.Len(t, me.previews, 3)
	assert.Equal(t, []int{1, 3, 5}, []int{me.previews[0].line, me.previews[1].line, me.previews[2].line})
	assert.Equal(t, "x := 1", me.previews[0].text)
}

func TestFindMatchMultipleCapsPreviews(t *testing.T) {
	content := strings.Repeat("dup\n", 9)
	_, _, _, err := findMatch(content, "dup")
	var me *matchError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, 9, me.count)
	assert.Len(t, me.previews, maxPreviews)
}

func TestFindMatchNormalizedRungMapsBackToOriginalBytes(t *testing.T) {
	content := "say(“hi”)   \nnext\n"
	start, end, rung, err := findMatch(content, "say(\"hi\")\nnext")
	require.NoError(t, err)
	assert.Equal(t, rungNormalized, rung)
	assert.Equal(t, "say(“hi”)   \nnext", content[start:end])
}

func TestFindMatchNormalizedRungMatchesInsideLine(t *testing.T) {
	content := "x = f(‘a’, b)\n"
	start, end, rung, err := findMatch(content, "f('a', b)")
	require.NoError(t, err)
	assert.Equal(t, rungNormalized, rung)
	assert.Equal(t, "f(‘a’, b)", content[start:end])
}

func TestFindMatchTrimmedRungIgnoresIndentation(t *testing.T) {
	content := "func f() {\n\tif ok {\n\t\treturn 1\n\t}\n}\n"
	start, end, rung, err := findMatch(content, "if ok {\n\treturn 1\n}")
	require.NoError(t, err)
	assert.Equal(t, rungTrimmed, rung)
	assert.Equal(t, "\tif ok {\n\t\treturn 1\n\t}", content[start:end])
}

func TestFindMatchTrimmedRungKeepsTrailingNewlineOfOldText(t *testing.T) {
	content := "a\n\tb\nc\n"
	start, end, rung, err := findMatch(content, "  b\n")
	require.NoError(t, err)
	assert.Equal(t, rungTrimmed, rung)
	assert.Equal(t, "\tb\n", content[start:end])
}

func TestFindMatchFuzzyRungAcceptsSmallTypo(t *testing.T) {
	content := "package main\n\nfunc compute(value int) int {\n\treturn value * 2 + offset\n}\n"
	old := "func compute(value int) int {\n\treturn value * 2 + offsets\n}"
	start, end, rung, err := findMatch(content, old)
	require.NoError(t, err)
	assert.Equal(t, rungFuzzy, rung)
	assert.Equal(t, "func compute(value int) int {\n\treturn value * 2 + offset\n}", content[start:end])
}

func TestFindMatchFuzzyLooseRungIgnoresIndentation(t *testing.T) {
	content := "start\n\t\tresult := compute(alpha, beta)\n\t\tuse(result)\nend\n"
	old := "result := compute(alpha, bet)\nuse(result)"
	start, end, rung, err := findMatch(content, old)
	require.NoError(t, err)
	assert.Equal(t, rungFuzzyLoose, rung)
	assert.Equal(t, "\t\tresult := compute(alpha, beta)\n\t\tuse(result)", content[start:end])
}

func TestFindMatchFuzzyNearDuplicatesAreAmbiguous(t *testing.T) {
	block := func(name string) string {
		return fmt.Sprintf("func %s() {\n\tvalue := loadConfiguration(path)\n\tprocess(value)\n\treturn\n}\n", name)
	}
	content := block("alpha") + "\n" + block("alphb")
	old := "func alphc() {\n\tvalue := loadConfiguration(path)\n\tprocess(value)\n\treturn\n}"
	_, _, _, err := findMatch(content, old)
	var me *matchError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, matchMultiple, me.kind)
	assert.Equal(t, 2, me.count)
	assert.Equal(t, []int{1, 7}, []int{me.previews[0].line, me.previews[1].line})
}

func TestFindMatchNotFoundNamesClosest(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nconfig.timeout = 30\nsix\n"
	_, _, _, err := findMatch(content, "config.timeout = 300000000")
	var me *matchError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, matchNotFound, me.kind)
	require.NotNil(t, me.closest)
	assert.Equal(t, 5, me.closest.line)
	assert.Equal(t, "config.timeout = 300000000", me.closest.expected)
	assert.Equal(t, "config.timeout = 30", me.closest.actual)
	assert.GreaterOrEqual(t, me.closest.percent, 50)
}

func TestFindMatchNotFoundWithoutSimilarText(t *testing.T) {
	_, _, _, err := findMatch("alpha\nbeta\n", "zzzzzzzzzzzzzzzz")
	var me *matchError
	require.True(t, errors.As(err, &me))
	assert.Equal(t, matchNotFound, me.kind)
	assert.Nil(t, me.closest)
}

func TestReindentAddsMissingIndentation(t *testing.T) {
	got := reindent("if x {\n\ty()\n}", "if x {\n\tz()\n}", "\t\tif x {\n\t\t\tz()\n\t\t}")
	assert.Equal(t, "\t\tif x {\n\t\t\ty()\n\t\t}", got)
}

func TestReindentReplacesIndentPrefix(t *testing.T) {
	got := reindent("    a\n      b\n\n    c", "    a", "\t\ta")
	assert.Equal(t, "\t\ta\n\t\t  b\n\n\t\tc", got)
}

func TestReindentKeepsTextWithSameIndent(t *testing.T) {
	assert.Equal(t, "  x\n  y", reindent("  x\n  y", "  x", "  x"))
}
