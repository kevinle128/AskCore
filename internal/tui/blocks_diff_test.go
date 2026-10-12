package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func rowsText(rows []diffRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%c%d:%s", r.kind, r.num, r.text)
	}
	return out
}

func diffNumbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("l%d", i+1)
	}
	return out
}

func TestUnifiedDiffKeepsThreeLinesOfContext(t *testing.T) {
	old := diffNumbered(20)
	cur := append([]string(nil), old...)
	cur[9] = "changed"
	rows, add, del := unifiedDiff(strings.Join(old, "\n"), strings.Join(cur, "\n"))
	require.Equal(t, [2]int{1, 1}, [2]int{add, del})
	require.Equal(t, []string{" 7:l7", " 8:l8", " 9:l9", "-10:l10", "+10:changed", " 11:l11", " 12:l12", " 13:l13"}, rowsText(rows))
}

func TestUnifiedDiffNumbersAreNewFileLines(t *testing.T) {
	rows, add, del := unifiedDiff("a\nb\nc\nd\n", "a\nX\nY\nd\n")
	require.Equal(t, [2]int{2, 2}, [2]int{add, del})
	require.Equal(t, []string{" 1:a", "-2:b", "-2:c", "+2:X", "+3:Y", " 4:d"}, rowsText(rows))
}

func TestUnifiedDiffWithTwoFarChanges(t *testing.T) {
	old := diffNumbered(30)
	cur := append([]string(nil), old...)
	cur[2], cur[26] = "first", "second"
	rows, add, del := unifiedDiff(strings.Join(old, "\n"), strings.Join(cur, "\n"))
	require.Equal(t, [2]int{2, 2}, [2]int{add, del})
	nums := []int{}
	for _, r := range rows {
		nums = append(nums, r.num)
	}
	require.Equal(t, []int{1, 2, 3, 3, 4, 5, 6, 24, 25, 26, 27, 27, 28, 29, 30}, nums)
}

func TestUnifiedDiffOfANewFileAddsEveryLine(t *testing.T) {
	rows, add, del := unifiedDiff("", "a\nb\n")
	require.Equal(t, []string{"+1:a", "+2:b"}, rowsText(rows))
	require.Equal(t, [2]int{2, 0}, [2]int{add, del})
}

func TestUnifiedDiffOfEqualTextsHasNoRows(t *testing.T) {
	rows, add, del := unifiedDiff("a\nb\n", "a\r\nb\r\n")
	require.Empty(t, rows)
	require.Zero(t, add+del)
}

func TestUnifiedDiffReplacesTheMiddleWhenTooManyEdits(t *testing.T) {
	var a, b []string
	for i := 0; i < 3*maxDiffEdits; i++ {
		a = append(a, fmt.Sprintf("old %d", i))
		b = append(b, fmt.Sprintf("new %d", i))
	}
	a = append([]string{"head"}, a...)
	b = append([]string{"head"}, b...)
	rows, add, del := unifiedDiff(strings.Join(a, "\n"), strings.Join(b, "\n"))
	require.Equal(t, [2]int{3 * maxDiffEdits, 3 * maxDiffEdits}, [2]int{add, del})
	require.Equal(t, " 1:head", rowsText(rows)[0])
}

// The rows of a script, applied to the old lines, give the new lines, and the
// counts on the tool line are the counts of the rows.
func TestDiffOpsReproduceTheNewTextForRandomChanges(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 300; iter++ {
		a := make([]string, rng.Intn(25))
		for i := range a {
			a[i] = fmt.Sprintf("w%d", rng.Intn(6))
		}
		b := make([]string, rng.Intn(25))
		for i := range b {
			b[i] = fmt.Sprintf("w%d", rng.Intn(6))
		}
		ops := diffOps(a, b)
		var got []string
		ai, add, del := 0, 0, 0
		for _, op := range ops {
			switch op {
			case diffSame:
				require.Equal(t, a[ai], b[len(got)])
				got = append(got, a[ai])
				ai++
			case diffRemoved:
				ai++
				del++
			default:
				got = append(got, b[len(got)])
				add++
			}
		}
		require.Equal(t, b, append([]string{}, got...), "iteration %d", iter)
		require.Equal(t, len(a), ai)
		// The script is a shortest one: it removes and adds no more than the
		// lines that are not in a longest common subsequence.
		common := lcsLen(a, b)
		require.Equal(t, [2]int{len(b) - common, len(a) - common}, [2]int{add, del}, "iteration %d: %v -> %v", iter, a, b)
	}
}

func lcsLen(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}
	return dp[len(a)][len(b)]
}

func TestToolLineCountsMatchTheDiffRows(t *testing.T) {
	d := protocol.ToolDiff{OldText: "a\nb\nc\nd\ne\n", NewText: "a\nB\nc\nd\nE\nF\n"}
	out := newToolOutput("", protocol.ToolResultDetails{Diff: &d})
	var plus, minus int
	for _, r := range out.diffRows {
		switch r.kind {
		case diffAdded:
			plus++
		case diffRemoved:
			minus++
		}
	}
	require.Equal(t, [2]int{out.added, out.removed}, [2]int{plus, minus})
	require.Equal(t, [2]int{3, 2}, [2]int{plus, minus})
}

func TestFullRowsShowTheDiffInsteadOfTheText(t *testing.T) {
	rc := RenderContext{Width: 80, Theme: NewTheme(0), Fold: FoldExpanded}
	d := protocol.ToolDiff{OldText: "a\n\tb\nc\n", NewText: "a\n\tB\nc\n"}
	b := newToolBlock("edit", "x.go", ToolSucceeded, "0.1s", "", newToolOutput("Edited x.go.", protocol.ToolResultDetails{Diff: &d}), nil)
	var rows []string
	for _, r := range b.Lines(rc)[1:] {
		rows = append(rows, ansi.Strip(r))
	}
	require.Equal(t, []string{
		"       1   a",
		"       2 -     b",
		"       2 +     B",
		"       3   c",
	}, rows)
}

func TestFullRowsWrapALongDiffLineUnderItsTextColumn(t *testing.T) {
	rc := RenderContext{Width: 30, Theme: NewTheme(0), Fold: FoldExpanded}
	d := protocol.ToolDiff{NewText: strings.Repeat("x", 40) + "\n"}
	b := newToolBlock("write", "x.go", ToolSucceeded, "0.1s", "", newToolOutput("", protocol.ToolResultDetails{Diff: &d}), nil)
	rows := b.Lines(rc)[1:]
	require.Len(t, rows, 3)
	first := ansi.Strip(rows[0])
	require.Equal(t, "       1 + "+strings.Repeat("x", 19), first)
	require.Equal(t, strings.Repeat(" ", 11)+strings.Repeat("x", 19), ansi.Strip(rows[1]))
	require.Equal(t, strings.Repeat(" ", 11)+strings.Repeat("x", 2), ansi.Strip(rows[2]))
}

func TestFullRowsOfAFailedEditShowTheErrorText(t *testing.T) {
	rc := RenderContext{Width: 80, Theme: NewTheme(0), Fold: FoldExpanded}
	b := newToolBlock("edit", "x.go", ToolFailed, "", "no match", newToolOutput("no match for old text\nsecond line", protocol.ToolResultDetails{}), nil)
	rows := b.Lines(rc)[1:]
	require.Equal(t, "    no match for old text", ansi.Strip(rows[0]))
	require.Equal(t, "    second line", ansi.Strip(rows[1]))
}

func TestFullRowsKeepTheTruncationLineAndShowEveryLine(t *testing.T) {
	rc := RenderContext{Width: 80, Theme: NewTheme(0), Fold: FoldExpanded}
	d := protocol.ToolResultDetails{Truncation: &protocol.ToolTruncation{TruncatedBy: "lines", TotalLines: 9, OutputLines: 3}}
	b := newToolBlock("read", "x", ToolSucceeded, "0.1s", "", newToolOutput("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12", d), nil)
	rows := b.Lines(rc)
	require.Len(t, rows, 1+12+1)
	for _, r := range rows {
		require.NotContains(t, r, "more lines")
	}
	require.Equal(t, "    model saw 3 of 9 lines", ansi.Strip(rows[13]))
}
