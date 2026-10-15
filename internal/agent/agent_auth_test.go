package agent_test

import (
	"context"
	"errors"
	"testing"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestSetModelUsesInjectedReadinessAndRetainsStateOnFailure(t *testing.T) {
	first := providers.TokenPlanMessages()
	next := providers.OpenAIGPT55()
	var checked providers.Model
	allowed := false
	a, err := agent.New(agent.Config{LoopConfig: agent.LoopConfig{
		Model: first,
		Stream: func(ctx context.Context, m providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) *providers.Stream {
			return providers.NewStream(ctx, 0, protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID}, func(a *providers.Assembler) { a.Start(); a.Done(protocol.StopStop) })
		},
		Ready: func(_ context.Context, m providers.Model, key string) error {
			checked = m
			if key != "" {
				t.Fatalf("unexpected key %q", key)
			}
			if !allowed {
				return errors.New("access denied")
			}
			return nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetModel(context.Background(), next); err == nil {
		t.Fatal("expected denied readiness")
	}
	if st := a.State(); st.Model.Provider != first.Provider {
		t.Fatalf("changed state: %v", st.Model)
	}
	allowed = true
	if err := a.SetModel(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if checked.Provider != next.Provider || a.State().Model.Provider != next.Provider {
		t.Fatal("readiness was not applied to target")
	}
}
