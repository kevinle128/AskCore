package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleteStepEndBeatsContinueBeatsProceed(t *testing.T) {
	tests := []struct {
		name string
		in   []TurnDecision
		want TurnDecision
	}{
		{"none", nil, Proceed},
		{"proceed proceed", []TurnDecision{Proceed, Proceed}, Proceed},
		{"proceed continue", []TurnDecision{Proceed, Continue}, Continue},
		{"continue proceed", []TurnDecision{Continue, Proceed}, Continue},
		{"continue end", []TurnDecision{Continue, End}, End},
		{"end continue", []TurnDecision{End, Continue}, End},
		{"end proceed", []TurnDecision{End, Proceed}, End},
		{"proceed end continue", []TurnDecision{Proceed, End, Continue}, End},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			calls := 0
			for _, d := range tt.in {
				r.OnCompleteStep(func(context.Context, Turn) (TurnDecision, error) { calls++; return d, nil })
			}
			got, err := r.CompleteStep(context.Background(), Turn{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, len(tt.in), calls, "every handler runs, even after End")
		})
	}
}

func TestCompleteStepErrorStopsChain(t *testing.T) {
	r := NewRegistry()
	r.OnCompleteStep(func(context.Context, Turn) (TurnDecision, error) { return End, errHandler })
	called := false
	r.OnCompleteStep(func(context.Context, Turn) (TurnDecision, error) { called = true; return Proceed, nil })
	got, err := r.CompleteStep(context.Background(), Turn{})
	assert.ErrorIs(t, err, errHandler)
	assert.Equal(t, Proceed, got, "an error carries no decision")
	assert.False(t, called)
}

func TestStopTurnEndBeatsContinueBeatsProceed(t *testing.T) {
	r := NewRegistry()
	for _, d := range []TurnDecision{Continue, End, Proceed} {
		r.OnStopTurn(func(context.Context, StopInput) (TurnDecision, error) { return d, nil })
	}
	got, err := r.StopTurn(context.Background(), StopInput{})
	require.NoError(t, err)
	assert.Equal(t, End, got)
}

func TestCompleteStepEveryOrderGivesTheSameDecision(t *testing.T) {
	all := []TurnDecision{Proceed, Continue, End}
	for _, a := range all {
		for _, b := range all {
			for _, c := range all {
				in := []TurnDecision{a, b, c}
				want := max(a, b, c)
				r := NewRegistry()
				for _, d := range in {
					r.OnCompleteStep(func(context.Context, Turn) (TurnDecision, error) { return d, nil })
				}
				got, err := r.CompleteStep(context.Background(), Turn{})
				require.NoError(t, err)
				assert.Equal(t, want, got, "%v", in)
			}
		}
	}
}
