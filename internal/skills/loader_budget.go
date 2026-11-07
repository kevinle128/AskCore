package skills

import (
	"unicode/utf8"
)

const (
	maxBudgetTokens   = 10000
	fallbackBudgetChs = 8000
)

// Budget is the room for the skill entries of the listing. Set Tokens or
// Chars, not both. Tokens counts ceil(bytes/4); Chars counts runes. The
// header and the footer are not charged.
type Budget struct {
	Tokens int
	Chars  int
}

// BudgetFor picks the budget: the positive override (the skillsMaxContextTokens
// setting) capped at 10000 tokens; else 2% of the context window in tokens;
// else 8000 characters, when the window is not known.
func BudgetFor(contextWindow, maxContextTokens int) Budget {
	switch {
	case maxContextTokens > 0:
		return Budget{Tokens: min(maxContextTokens, maxBudgetTokens)}
	case contextWindow > 0:
		return Budget{Tokens: max(contextWindow*2/100, 1)}
	default:
		return Budget{Chars: fallbackBudgetChs}
	}
}

func (b Budget) limit() int {
	if b.Tokens > 0 {
		return b.Tokens
	}
	return b.Chars
}

// lineCost is the cost of one line of text, its newline included.
func (b Budget) lineCost(text string) int {
	if b.Tokens > 0 {
		return (len(text)+3)/4 + 1
	}
	return utf8.RuneCountInString(text) + 1
}

// cost is the cost of an entry whose description has runes runes and bytes
// bytes.
func (b Budget) cost(e *entry, runes, bytes int) int {
	if b.Tokens > 0 {
		return (e.fixedBytes+bytes+3)/4 + 1
	}
	return e.fixedRunes + runes + 1
}

// shareDescriptions gives each entry description runes round-robin, one rune
// at a time, from the room that the empty descriptions leave. It returns the
// total cost.
func shareDescriptions(es []entry, b Budget) int {
	used := 0
	for i := range es {
		es[i].shown = 0
		used += b.cost(&es[i], 0, 0)
	}
	bytes := make([]int, len(es))
	for changed := true; changed; {
		changed = false
		for i := range es {
			e := &es[i]
			if e.shown >= len(e.desc) {
				continue
			}
			nb := bytes[i] + utf8.RuneLen(e.desc[e.shown])
			delta := b.cost(e, e.shown+1, nb) - b.cost(e, e.shown, bytes[i])
			if used+delta <= b.limit() {
				used += delta
				e.shown++
				bytes[i] = nb
				changed = true
			}
		}
	}
	return used
}
