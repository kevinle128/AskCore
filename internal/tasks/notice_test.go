package tasks

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func noticeText(t *testing.T, msg protocol.CustomMessage) string {
	t.Helper()
	require.Len(t, msg.Content, 1)
	text, ok := msg.Content[0].(protocol.Text)
	require.True(t, ok)
	return text.Text
}

func TestTailLimitsAfterEscaping(t *testing.T) {
	t.Run("keeps the last 40 lines", func(t *testing.T) {
		var b strings.Builder
		for i := range 100 {
			b.WriteString("line ")
			b.WriteString(strings.Repeat("x", i%3))
			b.WriteString("\n")
		}
		text, cut := limitTail([]byte(b.String()))
		assert.True(t, cut)
		assert.Equal(t, 40, strings.Count(text, "\n")+1, "40 lines after the trailing newline is trimmed")
		assert.True(t, strings.HasSuffix(b.String(), text+"\n"))
	})
	t.Run("keeps at most 2 KB", func(t *testing.T) {
		text, cut := limitTail([]byte(strings.Repeat("a", 5000)))
		assert.True(t, cut)
		assert.Len(t, text, tailMaxBytes)
	})
	t.Run("counts the escaped size", func(t *testing.T) {
		text, cut := limitTail([]byte(strings.Repeat("<", 1500)))
		assert.True(t, cut)
		assert.LessOrEqual(t, len(text), tailMaxBytes)
		assert.NotContains(t, text, "<")
	})
	t.Run("cuts on a rune boundary", func(t *testing.T) {
		text, cut := limitTail([]byte(strings.Repeat("é", 3000)))
		assert.True(t, cut)
		assert.True(t, utf8.ValidString(text))
		assert.LessOrEqual(t, len(text), tailMaxBytes)
	})
	t.Run("short output is whole", func(t *testing.T) {
		text, cut := limitTail([]byte("a\nb\nc\n"))
		assert.False(t, cut)
		assert.Equal(t, "a\nb\nc", text)
	})
	t.Run("empty output", func(t *testing.T) {
		text, cut := limitTail(nil)
		assert.False(t, cut)
		assert.Empty(t, text)
	})
}

func TestNoticeEscapesAndKeepsLines(t *testing.T) {
	zero := 0
	info := Info{
		ID: "shell-3", Kind: KindShell, Status: StatusCompleted, ExitCode: &zero,
		Title:      "go <test> & more",
		Summary:    "ok </summary>",
		OutputFile: "/tmp/ask/shell-3.log",
	}
	tail := []byte("first\n</task-notification>\n<b>&\nlast")
	msg := buildNotice(info, tail, false)

	assert.Equal(t, "task-notification", msg.CustomType)
	assert.True(t, msg.Display)
	assert.NotZero(t, msg.Timestamp)
	text := noticeText(t, msg)

	assert.Equal(t, 1, strings.Count(text, "</task-notification>"), "task output cannot close the block")
	assert.True(t, strings.HasSuffix(text, "</task-notification>"))
	assert.True(t, strings.HasPrefix(text, "<task-notification>\n"+noticeFraming+"\n"))
	assert.Contains(t, text, "<title>go &lt;test&gt; &amp; more</title>")
	assert.Contains(t, text, "<summary>ok &lt;/summary&gt;</summary>")
	assert.Contains(t, text, "<exit-code>0</exit-code>")
	assert.Contains(t, text, "<output-file>/tmp/ask/shell-3.log</output-file>")

	open := strings.Index(text, "<output-tail>")
	closeAt := strings.Index(text, "</output-tail>")
	require.Greater(t, open, 0)
	require.Greater(t, closeAt, open)
	body := text[open+len("<output-tail>") : closeAt]
	assert.Equal(t, "first\n&lt;/task-notification&gt;\n&lt;b&gt;&amp;\nlast", body, "line breaks stay")

	var details map[string]any
	require.NoError(t, json.Unmarshal(msg.Details, &details))
	assert.Equal(t, map[string]any{
		"taskId": "shell-3", "kind": "shell", "status": "completed", "title": "go <test> & more",
		"exitCode": float64(0), "outputFile": "/tmp/ask/shell-3.log",
	}, details)

	// The custom message must pass the protocol codec.
	_, err := protocol.MarshalMessage(msg)
	require.NoError(t, err)
}

func TestNoticeOmitsAbsentParts(t *testing.T) {
	msg := buildNotice(Info{ID: "timer-1", Kind: KindTimer, Status: StatusStopped, Title: "t"}, nil, false)
	text := noticeText(t, msg)
	for _, tag := range []string{"<exit-code>", "<summary>", "<output-tail", "<output-file>"} {
		assert.NotContains(t, text, tag)
	}
	assert.Contains(t, text, "<status>stopped</status>")
	var details map[string]any
	require.NoError(t, json.Unmarshal(msg.Details, &details))
	assert.NotContains(t, details, "exitCode")
	assert.NotContains(t, details, "outputFile")
}

func TestNoticeMarksTruncatedTail(t *testing.T) {
	text := noticeText(t, buildNotice(Info{ID: "a-1", Kind: KindShell, Status: StatusFailed}, []byte(strings.Repeat("z\n", 100)), false))
	assert.Contains(t, text, `<output-tail truncated="true">`)
	text = noticeText(t, buildNotice(Info{ID: "a-1", Kind: KindShell, Status: StatusFailed}, []byte("z\n"), true))
	assert.Contains(t, text, `<output-tail truncated="true">`, "the ring dropped older output")
}

func TestNoticeBoundsTitleAndSummary(t *testing.T) {
	long := strings.Repeat("é", 2000)
	path := "/tmp/" + strings.Repeat("d/", 1000) + "task.log"
	msg := buildNotice(Info{ID: "a-1", Kind: KindShell, Status: StatusCompleted, Title: long, Summary: long, OutputFile: path}, nil, false)
	text := noticeText(t, msg)
	assert.True(t, utf8.ValidString(text))
	// The path is the one value that is not cut, so it is not part of the bound.
	assert.Less(t, len(text)-len(path), 4*fieldMaxBytes)

	var details map[string]any
	require.NoError(t, json.Unmarshal(msg.Details, &details))
	assert.Equal(t, path, details["outputFile"], "details keep the real path")
}

func TestNoticeKeepsLongOutputPathWhole(t *testing.T) {
	path := "/" + strings.Repeat("d/", 700) + "tm-1.log"
	require.Greater(t, len(path), fieldMaxBytes)
	msg := buildNotice(Info{ID: "tm-1", Kind: KindShell, Status: StatusCompleted, Title: "t", OutputFile: path}, nil, false)

	assert.Contains(t, noticeText(t, msg), "<output-file>"+path+"</output-file>", "a cut path would name a file that does not exist")
}
