package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type slowBlock struct{ text string }

func (b slowBlock) Lines(rc RenderContext) []string {
	time.Sleep(time.Millisecond)
	return wrapLines(b.text, rc.Width-4)
}

func TestTallStreamingBlockCommitsEveryWordOnce(t *testing.T) {
	for _, n := range []int{25, 150} {
		for rep := 0; rep < 3; rep++ {
			t.Run(fmt.Sprintf("n=%d/%d", n, rep), func(t *testing.T) {
				h := startScreen(t, newTestModel(sketchSource("")), 60, 14)
				appendBlock(h, "p", textBlock{"prompt"}, true)
				var words []string
				appendBlock(h, "tall", slowBlock{"start"}, false)
				for i := 0; i < n; i++ {
					words = append(words, fmt.Sprintf("w%03d", i))
					h.send(ReplaceBlockMsg{ID: "tall", Block: slowBlock{strings.Join(words, " ")}})
				}
				h.send(FinishBlockMsg{ID: "tall"})
				h.send(AppendBlockMsg{ID: "meta", Block: textBlock{"meta\n"}, Stable: true})
				h.requireEachOnce(append([]string{"prompt"}, words...)...)
			})
		}
	}
}

// A stable block that is taller than the live region commits row by row
// without losing or repeating a row.
func TestTallStableBlockCommitsEveryRowOnce(t *testing.T) {
	h := startScreen(t, newTestModel(sketchSource("")), 80, 24)
	var rows []string
	for i := 0; i < 20; i++ {
		rows = append(rows, fmt.Sprintf("code-line-%02d", i))
	}
	appendBlock(h, "code", textBlock{strings.Join(rows, "\n")}, true)
	appendBlock(h, "meta", textBlock{"meta\n"}, true)
	h.requireEachOnce(append(append([]string{}, rows...), "meta")...)
}
