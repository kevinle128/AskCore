package providers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistryPrepareCapturesRegistrationRetryPolicy(t *testing.T) {
	r := NewRegistry()
	model := Model{API: "custom", Provider: "custom", ID: "model"}
	fn := func(context.Context, Model, TranscriptRequest, StreamOptions) *Stream { return nil }
	r.Register(model.API, fn)
	p, err := r.Prepare(model, TranscriptRequest{}, StreamOptions{})
	require.NoError(t, err)
	require.NotNil(t, p, "a custom provider without a Preparer still captures its policy")
	require.True(t, p.PolicyOnly, "plain stream registrations compute adapter values at Stream")
	require.Equal(t, RetryPolicy{Key: "default", MaxRetries: 5, BaseDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second}, p.RetryPolicy)
	custom := RetryPolicy{Key: "custom", MaxRetries: 2, BaseDelay: time.Second, MaxDelay: 3 * time.Second}
	r.Register(model.API, fn, custom)
	next, err := r.Prepare(model, TranscriptRequest{}, StreamOptions{})
	require.NoError(t, err)
	require.Equal(t, custom, next.RetryPolicy)
	require.Equal(t, DefaultRetryPolicy(), p.RetryPolicy, "the prior attempt keeps its captured policy")
	clone := next.Clone()
	clone.RetryPolicy.Key = "changed"
	require.Equal(t, "custom", next.RetryPolicy.Key)
}

func TestRegistryPrepareWithoutPreparerKeepsRequestOptions(t *testing.T) {
	r := NewRegistry()
	model := Model{API: "custom", Provider: "custom", ID: "model", Reasoning: true}
	temperature := 0.5
	opts := StreamOptions{Reasoning: "high", MaxTokens: 123, Temperature: &temperature, ToolChoice: "tool", CacheRetention: CacheRetentionLong}
	p, err := r.Prepare(model, TranscriptRequest{}, opts)
	require.NoError(t, err)
	require.Equal(t, opts.Reasoning, p.Thinking)
	require.Equal(t, opts.MaxTokens, p.MaxTokens)
	require.Equal(t, opts.ToolChoice, p.ToolChoice)
	require.Equal(t, opts.CacheRetention, p.CacheRetention)
	temperature = 0.9
	require.Equal(t, 0.5, *p.Temperature)
}

// policyProvider returns the same effective values to check registry ownership.
type policyProvider struct{ prepared *Prepared }

func (*policyProvider) API() string { return "prepared-custom" }
func (*policyProvider) Stream(context.Context, Model, TranscriptRequest, StreamOptions) *Stream {
	return nil
}
func (p *policyProvider) Prepare(Model, TranscriptRequest, StreamOptions) (*Prepared, error) {
	return p.prepared, nil
}
func (*policyProvider) Encode(context.Context, Model, *Prepared, TranscriptRequest, AuthBinding) (*Encoded, error) {
	return nil, nil
}

func TestRegistryCapturesPolicyWithoutMutatingPreparerValues(t *testing.T) {
	provider := &policyProvider{prepared: &Prepared{MaxTokens: 123}}
	registry := NewRegistry()
	policy := RetryPolicy{Key: "serving", MaxRetries: 3, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	registry.RegisterProvider(provider, policy)
	prepared, err := registry.Prepare(Model{API: API(provider.API()), Provider: "serving"}, TranscriptRequest{}, StreamOptions{})
	require.NoError(t, err)
	require.Equal(t, policy, prepared.RetryPolicy)
	require.False(t, prepared.PolicyOnly)
	require.Equal(t, 123, prepared.MaxTokens)
	require.Empty(t, provider.prepared.RetryPolicy.Key, "the captured policy belongs to the attempt")
	registry.Register(API(provider.API()), provider.Stream)
	fallback, err := registry.Prepare(Model{API: API(provider.API()), Provider: "serving"}, TranscriptRequest{}, StreamOptions{})
	require.NoError(t, err)
	require.True(t, fallback.PolicyOnly, "replaced stream registration must not retain a stale preparer")
	require.Equal(t, DefaultRetryPolicy(), fallback.RetryPolicy)
}
