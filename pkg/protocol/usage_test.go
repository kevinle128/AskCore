package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZeroUsageEncodesAsObject(t *testing.T) {
	got, err := json.Marshal(Usage{})
	require.NoError(t, err)
	assert.Equal(t, `{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`, string(got))
}

func TestUsageOptionalFieldsKeepZero(t *testing.T) {
	u := Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, CacheWrite1h: ip(0), Reasoning: ip(0), TotalTokens: 10}
	got, err := json.Marshal(u)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"cacheWrite1h":0`)
	assert.Contains(t, string(got), `"reasoning":0`)
	var back Usage
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Equal(t, u, back)
}

func TestCostMicroUSDRoundTrip(t *testing.T) {
	// 1 USD = 1_000_000; 0.000003 USD per token times 1000 tokens = 3000 micro-USD.
	u := Usage{Cost: Cost{Input: 3000, Output: 15_000_000, CacheRead: 1, CacheWrite: 7, Total: 15_003_008}}
	got, err := json.Marshal(u)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"total":15003008`)
	var back Usage
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Equal(t, u.Cost, back.Cost)
}

func TestCostRejectsFractionalValue(t *testing.T) {
	// Pi writes USD floats under the same names; reading them as micro-USD must fail loudly.
	var u Usage
	require.Error(t, json.Unmarshal([]byte(`{"cost":{"input":0.003}}`), &u))
}

func TestUsageCloneSharesNoPointer(t *testing.T) {
	u := Usage{Input: 1, CacheWrite1h: ip(2), Reasoning: ip(3)}
	c := u.Clone()
	assert.Equal(t, u, c)
	*c.CacheWrite1h = 9
	*c.Reasoning = 9
	assert.EqualValues(t, 2, *u.CacheWrite1h)
	assert.EqualValues(t, 3, *u.Reasoning)
}
