package protocol

// Usage is the token and cost record of one model response.
//
// CacheWrite1h is a subset of CacheWrite and Reasoning is a subset of Output.
// A nil pointer means the provider did not report the value; a pointer to 0
// means it reported zero. A zero Usage encodes as an object, never as null.
type Usage struct {
	Input        int64  `json:"input"`
	Output       int64  `json:"output"`
	CacheRead    int64  `json:"cacheRead"`
	CacheWrite   int64  `json:"cacheWrite"`
	CacheWrite1h *int64 `json:"cacheWrite1h,omitempty"`
	Reasoning    *int64 `json:"reasoning,omitempty"`
	TotalTokens  int64  `json:"totalTokens"`
	Cost         Cost   `json:"cost"`
}

// Cost holds money values in micro-USD (1 USD = 1_000_000). Pi stores USD as
// floating point numbers under the same field names, so a Pi value cannot be
// read as an Ask value without an explicit conversion.
type Cost struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Total      int64 `json:"total"`
}

// Clone returns a copy that shares no pointer with u.
func (u Usage) Clone() Usage {
	u.CacheWrite1h = clonePtr(u.CacheWrite1h)
	u.Reasoning = clonePtr(u.Reasoning)
	return u
}

// UsageTotals is the sum of the usage of every model attempt of a session,
// with the facts that tell how far the sum can be trusted.
type UsageTotals struct {
	// Total adds the usage of every attempt that reported usage, also a failed
	// or retried attempt.
	Total Usage `json:"total"`
	// Complete is true when every attempt either reported usage or was refused
	// before any output (and so cost nothing).
	Complete bool `json:"complete"`
	// CostComplete is true when Complete is true and every attempt that
	// reported usage had a known price.
	CostComplete bool `json:"costComplete"`
	// ContextTokens is the estimate of the size of the next request. 0 means
	// unknown.
	ContextTokens int64 `json:"contextTokens,omitempty"`
	// ContextWindow is the window of the current model. 0 means unknown.
	ContextWindow int `json:"contextWindow,omitempty"`
}
