package sessions

import "AskCore/pkg/protocol"

// AttemptUsage is one row of the usage fold: how one model attempt ended and
// what it used. A row holds no failure text.
type AttemptUsage struct {
	AttemptID string
	CycleID   string
	Outcome   string
	// Usage is nil when the attempt reported none.
	Usage *protocol.Usage
	// CostUnknown is true when the model of the attempt had no known price.
	CostUnknown bool
}

// FoldUsage folds the AttemptSettled entries of a session log into one row per
// attempt, in log order, and the totals of all rows. It is the only
// accumulation of usage: every reader (ACP, headless, TUI) calls it, so the
// figures agree. Message usage is never read, because the settlement of the
// attempt already holds it and a second source would count it twice.
//
// The fold covers the whole log, so failed and retried attempts count and the
// totals survive a resume. Complete is false when an attempt has no usage,
// unless the attempt failed before the provider streamed any output (NoOutput):
// a refused request is not billed and counts as zero. CostComplete is also
// false when an attempt that reported usage had no known price; its tokens
// still add. ContextTokens and ContextWindow stay 0, because they depend on the
// current model; EstimateContext computes the first.
func FoldUsage(entries []Entry) ([]AttemptUsage, protocol.UsageTotals) {
	rows := []AttemptUsage{}
	totals := protocol.UsageTotals{Complete: true, CostComplete: true}
	cycle := ""
	for _, e := range entries {
		switch v := e.(type) {
		case CycleOpened:
			cycle = v.CycleID
		case CycleClosed:
			cycle = ""
		case AttemptSettled:
			row := AttemptUsage{AttemptID: v.AttemptID, CycleID: cycle, Outcome: v.Outcome, CostUnknown: v.CostUnknown}
			switch {
			case v.Usage != nil:
				u := v.Usage.Clone()
				row.Usage = &u
				addUsage(&totals.Total, u)
				if v.CostUnknown {
					totals.CostComplete = false
				}
			case v.Outcome == "failed" && v.NoOutput:
				// Refused before any output: zero tokens, zero cost.
			default:
				totals.Complete = false
				totals.CostComplete = false
			}
			rows = append(rows, row)
		}
	}
	return rows, totals
}

// addUsage adds u to dst. An optional counter stays unset until one attempt
// reports it.
func addUsage(dst *protocol.Usage, u protocol.Usage) {
	dst.Input += u.Input
	dst.Output += u.Output
	dst.CacheRead += u.CacheRead
	dst.CacheWrite += u.CacheWrite
	dst.TotalTokens += u.TotalTokens
	dst.Cost.Input += u.Cost.Input
	dst.Cost.Output += u.Cost.Output
	dst.Cost.CacheRead += u.Cost.CacheRead
	dst.Cost.CacheWrite += u.Cost.CacheWrite
	dst.Cost.Total += u.Cost.Total
	dst.CacheWrite1h = addOptional(dst.CacheWrite1h, u.CacheWrite1h)
	dst.Reasoning = addOptional(dst.Reasoning, u.Reasoning)
}

func addOptional(dst, v *int64) *int64 {
	if v == nil {
		return dst
	}
	sum := *v
	if dst != nil {
		sum += *dst
	}
	return &sum
}
