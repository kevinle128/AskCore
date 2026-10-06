package pipeline

import "context"

// Decider is a handler of a decision point. It returns a decision and
// does not call next: the driver applies the rule of the point to all answers.
type Decider[In any] func(ctx context.Context, in In) (TurnDecision, error)

// Handler types of the decision points.
type (
	CompleteStepHandler = Decider[Turn]
	StopTurnHandler     = Decider[StopInput]
)

// decide runs every handler in order, even after an End, because a handler
// may record the turn when another one ends the run. The strongest decision
// wins: End, then Continue, then Proceed. The first error stops the chain and
// is returned with no decision. hs is a snapshot.
func decide[In any](ctx context.Context, hs []Decider[In], in In) (TurnDecision, error) {
	decision := Proceed
	for _, h := range hs {
		d, err := h(ctx, in)
		if err != nil {
			return Proceed, err
		}
		// The constants are ordered Proceed < Continue < End.
		decision = max(decision, d)
	}
	return decision, nil
}
