package providers

import "AskCore/pkg/protocol"

const tokensPerPrice = 1_000_000

// RawUsage is the provider token report before the Pi split.
type RawUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CachedTokens     int64
	CacheWriteTokens int64
	CacheWrite1h     *int64
	ReasoningTokens  *int64
	TotalTokens      int64
}

// NormalizeUsage sets Input = max(0, InputTokens-Cached-CacheWrite).
// Output keeps OutputTokens. Reasoning stays a subset of Output.
// CacheWrite1h stays a subset of CacheWrite, clamped into range.
func NormalizeUsage(raw RawUsage) protocol.Usage {
	cached := atLeastZero(raw.CachedTokens)
	write := atLeastZero(raw.CacheWriteTokens)
	out := atLeastZero(raw.OutputTokens)
	in := raw.InputTokens - cached - write
	if in < 0 {
		in = 0
	}
	u := protocol.Usage{
		Input:       in,
		Output:      out,
		CacheRead:   cached,
		CacheWrite:  write,
		TotalTokens: raw.TotalTokens,
	}
	if raw.CacheWrite1h != nil {
		h := *raw.CacheWrite1h
		if h < 0 {
			h = 0
		}
		if h > write {
			h = write
		}
		u.CacheWrite1h = &h
	}
	if raw.ReasoningTokens != nil {
		r := *raw.ReasoningTokens
		if r < 0 {
			r = 0
		}
		if r > out {
			r = out
		}
		u.Reasoning = &r
	}
	return u
}

// CalculateCost picks the highest tier whose Above is strictly less than
// input+cacheRead+cacheWrite, prices that tier, then applies the service
// tier. serviceTier empty leaves the cost unscaled.
//
// CacheWrite1h tokens are priced at 2x the tier input price. The rest of
// CacheWrite uses the tier cache-write price. This 2x path runs only when
// CacheWrite1h is non-nil.
//
// Service tier scale, integer n/d, toward zero:
//
//	flex              1/2
//	priority or fast  2/1, except model id gpt-5.5 which is 5/2
//	anything else     1/1
func CalculateCost(m Model, u protocol.Usage, tier ServiceTier) protocol.Cost {
	in := atLeastZero(u.Input)
	out := atLeastZero(u.Output)
	read := atLeastZero(u.CacheRead)
	write := atLeastZero(u.CacheWrite)
	price := pickTier(m.Cost.Tiers, in+read+write)

	var write1h int64
	pricedWrite := write
	if u.CacheWrite1h != nil {
		write1h = *u.CacheWrite1h
		if write1h < 0 {
			write1h = 0
		}
		if write1h > write {
			write1h = write
		}
		pricedWrite = write - write1h
	}

	c := protocol.Cost{
		Input:      tokenCost(in, price.Input),
		Output:     tokenCost(out, price.Output),
		CacheRead:  tokenCost(read, price.CacheRead),
		CacheWrite: tokenCost(pricedWrite, price.CacheWrite) + tokenCost(write1h, price.Input*2),
	}
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return applyServiceTier(c, m.ID, tier)
}

func pickTier(tiers []Tier, tokens int64) Price {
	bestAbove := int64(-1)
	var price Price
	for _, t := range tiers {
		if t.Above < tokens && t.Above > bestAbove {
			bestAbove = t.Above
			price = t.Price
		}
	}
	return price
}

func tokenCost(tokens, pricePerMillion int64) int64 {
	if tokens <= 0 || pricePerMillion == 0 {
		return 0
	}
	return tokens * pricePerMillion / tokensPerPrice
}

func applyServiceTier(c protocol.Cost, modelID string, tier ServiceTier) protocol.Cost {
	n, d := int64(1), int64(1)
	switch tier {
	case TierFlex:
		n, d = 1, 2
	case TierPriority, TierFast:
		if modelID == ModelGPT55 {
			n, d = 5, 2
		} else {
			n, d = 2, 1
		}
	}
	scale := func(v int64) int64 { return v * n / d }
	c.Input = scale(c.Input)
	c.Output = scale(c.Output)
	c.CacheRead = scale(c.CacheRead)
	c.CacheWrite = scale(c.CacheWrite)
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return c
}

func atLeastZero(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
