package providers

import "time"

// RetryPolicy is the retry budget and backoff of one provider registration.
type RetryPolicy struct {
	Key        string        `json:"key"`
	MaxRetries int           `json:"maxRetries"`
	BaseDelay  time.Duration `json:"baseDelay"`
	MaxDelay   time.Duration `json:"maxDelay"`
}

// DefaultRetryPolicy allows five retries with bounded exponential backoff.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Key: "default", MaxRetries: 5, BaseDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second}
}
