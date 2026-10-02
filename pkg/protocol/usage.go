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
