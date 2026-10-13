package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Text formats of transcript blocks. These helpers return plain text; blocks
// style the result with theme roles.

// formatTokens writes a token count: whole numbers below 1,000, then "k" with
// one decimal place below 100k, then whole "k", then "M" with one decimal.
func formatTokens(n int64) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 99_950:
		return trimTenth(float64(n)/1000) + "k"
	case n < 999_500:
		return fmt.Sprintf("%dk", (n+500)/1000)
	default:
		return trimTenth(float64(n)/1_000_000) + "M"
	}
}

// trimTenth writes a number with one decimal place, for example 12.4.
func trimTenth(v float64) string { return fmt.Sprintf("%.1f", v) }

// formatCost writes a cost in micro-USD: three decimal places below one
// dollar, two decimal places from one dollar.
func formatCost(microUSD int64) string {
	if microUSD < 0 {
		microUSD = 0
	}
	usd := float64(microUSD) / 1_000_000
	if microUSD < 999_500 {
		return fmt.Sprintf("$%.3f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// formatDuration writes an elapsed time with the least digits that stay
// useful: "0.1s", "4.2s", "12s", "1m05s".
func formatDuration(d time.Duration) string {
	switch {
	case d < 0:
		d = 0
		fallthrough
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()+0.5))
	default:
		secs := int(d.Seconds() + 0.5)
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	}
}

// argKeys are the argument names that describe a tool call best, in the order
// the tool line looks for them.
var argKeys = []string{"command", "path", "file_path", "filePath", "pattern", "query", "url", "text", "prompt", "name"}

// shortArgs makes the one-line summary of tool arguments. It prefers a known
// key, then the first string value, then the compact JSON text. Line breaks
// and runs of spaces collapse so the summary always fits one row.
func shortArgs(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return oneLine(string(raw))
	}
	asString := func(v json.RawMessage) (string, bool) {
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", false
		}
		return s, true
	}
	for _, k := range argKeys {
		if v, ok := obj[k]; ok {
			if s, ok := asString(v); ok && strings.TrimSpace(s) != "" {
				return oneLine(s)
			}
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := asString(obj[k]); ok && strings.TrimSpace(s) != "" {
			return oneLine(s)
		}
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		return oneLine(compact.String())
	}
	return oneLine(string(raw))
}

// oneLine turns any run of white space and control characters into one space.
// Escape sequences are removed first, so that their text does not stay behind.
func oneLine(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// clockTime writes a Unix time in milliseconds as hours and minutes. A zero
// time gives an empty string, so a block without a send time shows none.
func clockTime(unixMs int64, loc *time.Location) string {
	if unixMs <= 0 {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	return time.UnixMilli(unixMs).In(loc).Format("15:04")
}
