package tools

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// matchRung names the step of the matching ladder that found a match. The
// steps run from the strictest to the loosest, and the first step that finds
// exactly one match wins.
type matchRung int

const (
	// rungExact is a plain substring match.
	rungExact matchRung = iota
	// rungNormalized is a substring match after trailing whitespace, quotes,
	// dashes and special spaces are normalized on both sides.
	rungNormalized
	// rungTrimmed compares whole lines with their indentation ignored.
	rungTrimmed
	// rungFuzzy scores whole lines and needs a mean similarity of 0.95.
	rungFuzzy
	// rungFuzzyLoose scores whole lines with indentation ignored and needs a
	// mean similarity of 0.8.
	rungFuzzyLoose
)

const (
	fuzzyThreshold      = 0.95
	fuzzyLooseThreshold = 0.8
	// closestFloor is the lowest similarity that is worth a "closest match"
	// hint in a not-found error.
	closestFloor = 0.5
	// maxPreviews caps the match previews of an ambiguity error.
	maxPreviews = 5
	// maxScoredLine is the longest line (in bytes) that the similarity score
	// compares character by character. Longer lines match only when equal.
	maxScoredLine = 2000
	// maxMemoCells bounds the per-search similarity cache.
	maxMemoCells = 4 << 20
	previewRunes = 100
)

type matchErrKind int

const (
	matchNotFound matchErrKind = iota
	matchMultiple
)

// matchError is returned by findMatch when no unique match exists. The edit
// tool turns it into a message that names the file and the edit.
type matchError struct {
	kind matchErrKind
	// count is the number of matches of an ambiguity.
	count int
	// previews hold at most maxPreviews of the matches, in file order.
	previews []matchPreview
	// closest is the most similar block of a not-found error. It is nil when
	// nothing in the file is similar enough.
	closest *closestMatch
}

type matchPreview struct {
	line int // 1-based
	text string
}

type closestMatch struct {
	percent  int
	line     int // 1-based first line of the similar block
	expected string
	actual   string
}

func (e *matchError) Error() string {
	if e.kind == matchMultiple {
		return "text matches more than one place"
	}
	return "text not found"
}

// findMatch locates oldText in content, which must use "\n" line ends. It
// returns the byte range of the match. The ladder is: exact substring;
// normalized substring (Pi's normalizeForFuzzyMatch); whole lines with
// indentation ignored; a per-line similarity score of 0.95; the same score at
// 0.8 with indentation ignored. A rung that finds more than one place is an
// error, and the search never falls to a looser rung then. The rungs after
// the first two compare whole lines, so the range they return holds whole
// lines, and holds the final line end only when oldText ends with one.
func findMatch(content, oldText string) (start, end int, rung matchRung, err error) {
	var offsets []int
	for pos := 0; pos < len(content); {
		i := strings.Index(content[pos:], oldText)
		if i < 0 {
			break
		}
		offsets = append(offsets, pos+i)
		pos += i + 1
	}
	if len(offsets) > 0 {
		if len(offsets) == 1 {
			return offsets[0], offsets[0] + len(oldText), rungExact, nil
		}
		return 0, 0, rungExact, multipleError(content, offsets)
	}

	if spans := normalizedMatches(content, oldText); len(spans) == 1 {
		return spans[0][0], spans[0][1], rungNormalized, nil
	} else if len(spans) > 1 {
		offsets := make([]int, len(spans))
		for i, s := range spans {
			offsets[i] = s[0]
		}
		return 0, 0, rungNormalized, multipleError(content, offsets)
	}

	lines := lineSpans(content)
	oldLines, trailingNL := splitOldLines(oldText)
	n := len(oldLines)
	if n > 0 && n <= len(lines) {
		contentLines := make([]string, len(lines))
		for i, sp := range lines {
			contentLines[i] = content[sp.start:sp.end]
		}
		rungs := []struct {
			rung      matchRung
			key       func(string) string
			threshold float64 // 0 means equality
		}{
			{rungTrimmed, trimmedKey, 0},
			{rungFuzzy, normalizeLineKey, fuzzyThreshold},
			{rungFuzzyLoose, trimmedKey, fuzzyLooseThreshold},
		}
		for _, r := range rungs {
			ck, ok := keyed(contentLines, r.key), keyed(oldLines, r.key)
			var starts []int
			if r.threshold == 0 {
				starts = equalWindows(ck, ok)
			} else {
				starts = scoredWindows(ck, ok, r.threshold)
			}
			switch {
			case len(starts) == 1:
				s, e := blockRange(content, lines, starts[0], n, trailingNL)
				return s, e, r.rung, nil
			case len(starts) > 1:
				offsets := make([]int, len(starts))
				for i, s := range starts {
					offsets[i] = lines[s].start
				}
				return 0, 0, r.rung, multipleError(content, offsets)
			}
		}
		return 0, 0, rungFuzzyLoose, notFoundError(contentLines, oldLines)
	}
	return 0, 0, rungExact, &matchError{kind: matchNotFound}
}

// blockRange returns the byte range of n whole lines that start at line s.
func blockRange(content string, lines []lineSpan, s, n int, trailingNL bool) (start, end int) {
	start, end = lines[s].start, lines[s+n-1].end
	if trailingNL && end < len(content) {
		end++ // the line end of the last matched line
	}
	return start, end
}

func multipleError(content string, offsets []int) *matchError {
	e := &matchError{kind: matchMultiple, count: len(offsets)}
	for _, off := range offsets {
		if len(e.previews) == maxPreviews {
			break
		}
		line := strings.Count(content[:off], "\n") + 1
		ls := strings.LastIndexByte(content[:off], '\n') + 1
		le := len(content)
		if i := strings.IndexByte(content[off:], '\n'); i >= 0 {
			le = off + i
		}
		e.previews = append(e.previews, matchPreview{line: line, text: cutRunes(strings.TrimSpace(content[ls:le]), previewRunes)})
	}
	return e
}

func notFoundError(contentLines, oldLines []string) *matchError {
	e := &matchError{kind: matchNotFound}
	ck, ok := keyed(contentLines, trimmedKey), keyed(oldLines, trimmedKey)
	sc := newWindowScorer(ck, ok)
	best, bestStart := closestFloor-1e-9, -1
	for s := 0; s+len(ok) <= len(ck); s++ {
		if score, found := sc.window(s, best); found && score > best {
			best, bestStart = score, s
		}
	}
	if bestStart < 0 {
		return e
	}
	c := &closestMatch{percent: int(math.Round(best * 100)), line: bestStart + 1}
	for j := range ok {
		if ok[j] != ck[bestStart+j] {
			c.line = bestStart + j + 1
			c.expected = cutRunes(strings.TrimSpace(oldLines[j]), previewRunes)
			c.actual = cutRunes(strings.TrimSpace(contentLines[bestStart+j]), previewRunes)
			break
		}
	}
	e.closest = c
	return e
}

type lineSpan struct{ start, end int } // end excludes the line end

// lineSpans splits content into line ranges. A final line end does not start
// another line.
func lineSpans(content string) []lineSpan {
	var out []lineSpan
	for pos := 0; pos < len(content); {
		end := len(content)
		next := end
		if i := strings.IndexByte(content[pos:], '\n'); i >= 0 {
			end = pos + i
			next = end + 1
		}
		out = append(out, lineSpan{pos, end})
		pos = next
	}
	return out
}

// splitOldLines splits oldText into lines. A final line end is reported
// separately, so "a\nb\n" and "a\nb" have the same lines.
func splitOldLines(oldText string) (lines []string, trailingNL bool) {
	lines = strings.Split(oldText, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		return lines[:len(lines)-1], true
	}
	return lines, false
}

func keyed(lines []string, key func(string) string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = key(l)
	}
	return out
}

func trimmedKey(line string) string { return strings.TrimSpace(normalizeForFuzzyMatch(line)) }

func normalizeLineKey(line string) string { return normalizeForFuzzyMatch(line) }

// equalWindows returns the start of every window of len(old) lines of
// content that equals old.
func equalWindows(content, old []string) []int {
	var out []int
	for s := 0; s+len(old) <= len(content); s++ {
		if slices.Equal(content[s:s+len(old)], old) {
			out = append(out, s)
		}
	}
	return out
}

// scoredWindows returns the start of each window whose mean line similarity
// reaches threshold. Windows that overlap a better window are dropped, so a
// block that matches at a shifted position is reported once.
func scoredWindows(content, old []string, threshold float64) []int {
	type cand struct {
		start int
		score float64
	}
	sc := newWindowScorer(content, old)
	var cands []cand
	for s := 0; s+len(old) <= len(content); s++ {
		if score, ok := sc.window(s, threshold); ok {
			cands = append(cands, cand{s, score})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return a.start - b.start
	})
	var kept []int
	for _, c := range cands {
		overlaps := false
		for _, k := range kept {
			if abs(k-c.start) < len(old) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, c.start)
		}
	}
	slices.Sort(kept)
	return kept
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// windowScorer scores windows of old lines laid over content lines. It caches
// the similarity of each line pair, because neighbouring windows share pairs.
type windowScorer struct {
	content, old []string
	memo         []float32 // -1 = not computed; nil when the matrix is too large
}

func newWindowScorer(content, old []string) *windowScorer {
	s := &windowScorer{content: content, old: old}
	if cells := len(content) * len(old); cells > 0 && cells <= maxMemoCells {
		s.memo = make([]float32, cells)
		for i := range s.memo {
			s.memo[i] = -1
		}
	}
	return s
}

func (s *windowScorer) lineSim(i, j int) float64 {
	if s.memo == nil {
		return lineScore(s.content[i], s.old[j])
	}
	k := i*len(s.old) + j
	if v := s.memo[k]; v >= 0 {
		return float64(v)
	}
	v := lineScore(s.content[i], s.old[j])
	s.memo[k] = float32(v)
	return v
}

// window returns the mean similarity of the window that starts at line start.
// It gives up, and reports false, as soon as the mean cannot reach floor.
func (s *windowScorer) window(start int, floor float64) (float64, bool) {
	n := len(s.old)
	need := floor*float64(n) - 1e-9
	sum := 0.0
	for j := 0; j < n; j++ {
		sum += s.lineSim(start+j, j)
		if sum+float64(n-j-1) < need {
			return 0, false
		}
	}
	return sum / float64(n), true
}

// lineScore returns the similarity of two lines from 0 to 1: one minus the
// edit distance divided by the length of the longer line.
func lineScore(a, b string) float64 {
	switch {
	case a == b:
		return 1
	case a == "" || b == "":
		return 0
	case len(a) > maxScoredLine || len(b) > maxScoredLine:
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	return 1 - float64(editDistance(ra, rb))/float64(max(len(ra), len(rb)))
}

func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// normalizedMatches returns the byte ranges of content that equal oldText
// after both are normalized. The ranges are in the original content. A range
// that ends at a line end also holds the trailing whitespace of that line.
func normalizedMatches(content, oldText string) [][2]int {
	want := normalizeForFuzzyMatch(oldText)
	if want == "" {
		return nil
	}
	have, starts, ends := normalizeWithMap(content)
	var out [][2]int
	for pos := 0; ; {
		i := strings.Index(have[pos:], want)
		if i < 0 {
			return out
		}
		i += pos
		end := i + len(want)
		pos = i + 1
		first, last := int(starts[i]), int(ends[end-1])
		if end == len(have) || have[end] == '\n' {
			// The match ends at a line end, so everything up to the original
			// line end is whitespace that normalization dropped. It belongs to
			// the matched line.
			if nl := strings.IndexByte(content[last:], '\n'); nl >= 0 {
				last += nl
			} else {
				last = len(content)
			}
		}
		out = append(out, [2]int{first, last})
	}
}

// normalizeForFuzzyMatch applies the normalization that Pi uses to compare
// text that differs only in invisible or look-alike characters: NFKC, no
// trailing whitespace on a line, ASCII quotes and hyphens, and plain spaces.
func normalizeForFuzzyMatch(text string) string {
	s, _, _ := normalizeWithMap(text)
	return s
}

// normalizeWithMap normalizes text like normalizeForFuzzyMatch and also
// returns, for each byte of the result, the byte range of the input segment
// it came from.
func normalizeWithMap(text string) (out string, starts, ends []int32) {
	buf := make([]byte, 0, len(text))
	starts = make([]int32, 0, len(text))
	ends = make([]int32, 0, len(text))
	trimTrailing := func() {
		for n := len(buf); n > 0; n-- {
			switch buf[n-1] {
			case ' ', '\t', '\r', '\v', '\f':
				buf, starts, ends = buf[:n-1], starts[:n-1], ends[:n-1]
				continue
			}
			break
		}
	}
	var it norm.Iter
	it.InitString(norm.NFKC, text)
	from := 0
	for !it.Done() {
		seg := it.Next()
		to := it.Pos()
		for _, r := range string(seg) {
			if r == '\n' {
				trimTrailing()
			}
			r = foldRune(r)
			l := len(buf)
			buf = utf8.AppendRune(buf, r)
			for k := l; k < len(buf); k++ {
				starts = append(starts, int32(from))
				ends = append(ends, int32(to))
			}
		}
		from = to
	}
	trimTrailing()
	return string(buf), starts, ends
}

// foldRune maps the quote, dash and space characters that Pi treats as equal
// to their ASCII forms.
func foldRune(r rune) rune {
	switch r {
	case '‘', '’', '‚', '‛':
		return '\''
	case '“', '”', '„', '‟':
		return '"'
	case '‐', '‑', '‒', '–', '—', '―', '−':
		return '-'
	case ' ', ' ', ' ', '　':
		return ' '
	}
	if r >= ' ' && r <= ' ' {
		return ' '
	}
	return r
}

// reindent shifts the indentation of newText when the block that oldText
// matched had another indentation than oldText. block is the matched text of
// the file. It compares the indentation of the first non-blank line of
// oldText with that of block and replaces that prefix on every non-blank line
// of newText that starts with it. A line without the prefix stays as it is.
func reindent(newText, oldText, block string) string {
	want, have := firstIndent(oldText), firstIndent(block)
	if want == have {
		return newText
	}
	lines := strings.Split(newText, "\n")
	for i, l := range lines {
		switch {
		case strings.TrimSpace(l) == "":
		case strings.HasPrefix(l, want):
			lines[i] = have + l[len(want):]
		}
	}
	return strings.Join(lines, "\n")
}

// firstIndent returns the leading whitespace of the first non-blank line.
func firstIndent(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
	}
	return ""
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}
