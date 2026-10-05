package pipeline

import (
	"context"
	"encoding/json"
	"errors"

	"AskCore/pkg/protocol"
)

// ErrMultipleConvertToLLM is returned by Compose when more than one input sets
// ConvertToLLM. A second converter would get model messages and not the log,
// which is an easy silent bug.
var ErrMultipleConvertToLLM = errors.New("pipeline: more than one ConvertToLLM hook")

// Compose returns one Hooks that runs hs in argument order at each hook point.
// A nil field is skipped. When no input sets a point, the field stays nil and
// the loop keeps its default. When one input sets it, that function is used
// as it is. Only two or more functions get a chain with the rule of that
// point. The first error stops a chain and is returned unchanged.
//
// When a later queue source fails, the messages that earlier sources already
// gave in that poll are dropped with the error. The run ends with that error
// in any case, so no turn could use them.
//
// Compose does not recover panics. A panic in a tool hook still becomes an
// error result in the loop, and any other panic reaches the run guard.
func Compose(hs ...Hooks) (Hooks, error) {
	var out Hooks
	var converters int
	for _, h := range hs {
		if h.ConvertToLLM != nil {
			out.ConvertToLLM = h.ConvertToLLM
			converters++
		}
	}
	if converters > 1 {
		return Hooks{}, ErrMultipleConvertToLLM
	}

	var transforms []func(context.Context, []protocol.Message) ([]protocol.Message, error)
	var keys []func(context.Context, string) (string, error)
	var prepares []func(context.Context, Request) (*RequestUpdate, error)
	var finishes []func(context.Context, Turn) (TurnDecision, error)
	var befores []func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error)
	var afters []func(context.Context, ToolResultInfo) (*AfterToolCallResult, error)
	var steers, followUps []func(context.Context) ([]protocol.Message, error)
	for _, h := range hs {
		if h.TransformContext != nil {
			transforms = append(transforms, h.TransformContext)
		}
		if h.GetAPIKey != nil {
			keys = append(keys, h.GetAPIKey)
		}
		if h.PrepareRequest != nil {
			prepares = append(prepares, h.PrepareRequest)
		}
		if h.FinishTurn != nil {
			finishes = append(finishes, h.FinishTurn)
		}
		if h.BeforeToolCall != nil {
			befores = append(befores, h.BeforeToolCall)
		}
		if h.AfterToolCall != nil {
			afters = append(afters, h.AfterToolCall)
		}
		if h.GetSteeringMessages != nil {
			steers = append(steers, h.GetSteeringMessages)
		}
		if h.GetFollowUpMessages != nil {
			followUps = append(followUps, h.GetFollowUpMessages)
		}
	}

	if len(transforms) == 1 {
		out.TransformContext = transforms[0]
	} else if len(transforms) > 1 {
		out.TransformContext = chainTransform(transforms)
	}
	if len(keys) == 1 {
		out.GetAPIKey = keys[0]
	} else if len(keys) > 1 {
		out.GetAPIKey = firstKey(keys)
	}
	if len(prepares) == 1 {
		out.PrepareRequest = prepares[0]
	} else if len(prepares) > 1 {
		out.PrepareRequest = chainPrepare(prepares)
	}
	if len(finishes) == 1 {
		out.FinishTurn = finishes[0]
	} else if len(finishes) > 1 {
		out.FinishTurn = allFinish(finishes)
	}
	if len(befores) == 1 {
		out.BeforeToolCall = befores[0]
	} else if len(befores) > 1 {
		out.BeforeToolCall = chainBefore(befores)
	}
	if len(afters) == 1 {
		out.AfterToolCall = afters[0]
	} else if len(afters) > 1 {
		out.AfterToolCall = chainAfter(afters)
	}
	if len(steers) == 1 {
		out.GetSteeringMessages = steers[0]
	} else if len(steers) > 1 {
		out.GetSteeringMessages = concatQueues(steers)
	}
	if len(followUps) == 1 {
		out.GetFollowUpMessages = followUps[0]
	} else if len(followUps) > 1 {
		out.GetFollowUpMessages = concatQueues(followUps)
	}
	return out, nil
}

// chainTransform feeds the output of each function into the next.
func chainTransform(fs []func(context.Context, []protocol.Message) ([]protocol.Message, error)) func(context.Context, []protocol.Message) ([]protocol.Message, error) {
	return func(ctx context.Context, msgs []protocol.Message) ([]protocol.Message, error) {
		var err error
		for _, f := range fs {
			if msgs, err = f(ctx, msgs); err != nil {
				return nil, err
			}
		}
		return msgs, nil
	}
}

// firstKey returns the first non-empty key. An empty key means "no opinion",
// so later functions only run while no key was found. All empty gives "" and
// the loop falls back to the configured key.
func firstKey(fs []func(context.Context, string) (string, error)) func(context.Context, string) (string, error) {
	return func(ctx context.Context, provider string) (string, error) {
		for _, f := range fs {
			key, err := f(ctx, provider)
			if err != nil || key != "" {
				return key, err
			}
		}
		return "", nil
	}
}

// chainPrepare shows each function the request with all earlier updates
// applied. The merged update takes the latest non-nil (non-empty for the
// thinking level) value of each field. A nil update keeps the earlier ones.
func chainPrepare(fs []func(context.Context, Request) (*RequestUpdate, error)) func(context.Context, Request) (*RequestUpdate, error) {
	return func(ctx context.Context, r Request) (*RequestUpdate, error) {
		var merged *RequestUpdate
		for _, f := range fs {
			upd, err := f(ctx, r)
			if err != nil {
				return nil, err
			}
			if upd == nil {
				continue
			}
			if merged == nil {
				merged = &RequestUpdate{}
			}
			if upd.Context != nil {
				merged.Context, r.Context = upd.Context, *upd.Context
			}
			if upd.Model != nil {
				merged.Model, r.Model = upd.Model, *upd.Model
			}
			if upd.ThinkingLevel != "" {
				merged.ThinkingLevel, r.ThinkingLevel = upd.ThinkingLevel, upd.ThinkingLevel
			}
		}
		return merged, nil
	}
}

// allFinish runs every function, even after an End, because a hook may record
// the turn when another one ends the run. The strongest decision wins:
// End, then Continue, then Proceed.
func allFinish(fs []func(context.Context, Turn) (TurnDecision, error)) func(context.Context, Turn) (TurnDecision, error) {
	return func(ctx context.Context, t Turn) (TurnDecision, error) {
		decision := Proceed
		for _, f := range fs {
			d, err := f(ctx, t)
			if err != nil {
				return Proceed, err
			}
			// The constants are ordered Proceed < Continue < End.
			decision = max(decision, d)
		}
		return decision, nil
	}
}

// chainBefore passes replaced arguments to the next function. The first Block
// ends the chain and its result is returned as it is. Without a block, the
// result is nil unless some function replaced the arguments.
func chainBefore(fs []func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error)) func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error) {
	return func(ctx context.Context, c ToolCallInfo) (*BeforeToolCallResult, error) {
		var args json.RawMessage
		for _, f := range fs {
			r, err := f(ctx, c)
			if err != nil {
				return nil, err
			}
			if r == nil {
				continue
			}
			if r.Block {
				return r, nil
			}
			if r.Args != nil {
				args, c.Args = r.Args, r.Args
			}
		}
		if args == nil {
			return nil, nil
		}
		return &BeforeToolCallResult{Args: args}, nil
	}
}

// chainAfter shows each function the result with all earlier overrides
// applied. The merged override takes the latest non-nil value of each field.
// A later Content without StructuredContent clears an earlier
// StructuredContent, as AfterToolCallResult.Apply does.
func chainAfter(fs []func(context.Context, ToolResultInfo) (*AfterToolCallResult, error)) func(context.Context, ToolResultInfo) (*AfterToolCallResult, error) {
	return func(ctx context.Context, c ToolResultInfo) (*AfterToolCallResult, error) {
		var merged *AfterToolCallResult
		for _, f := range fs {
			r, err := f(ctx, c)
			if err != nil {
				return nil, err
			}
			if r == nil {
				continue
			}
			if merged == nil {
				merged = &AfterToolCallResult{}
			}
			if r.StructuredContent != nil {
				merged.StructuredContent = r.StructuredContent
			} else if r.Content != nil {
				merged.StructuredContent = nil
			}
			if r.Content != nil {
				merged.Content = r.Content
			}
			if r.Details != nil {
				merged.Details = r.Details
			}
			if r.IsError != nil {
				merged.IsError = r.IsError
			}
			if r.Usage != nil {
				merged.Usage = r.Usage
			}
			if r.Terminate != nil {
				merged.Terminate = r.Terminate
			}
			c.Result, c.IsError = r.Apply(c.Result, c.IsError)
		}
		return merged, nil
	}
}

// concatQueues polls every source in order and joins the messages. Each
// source keeps its own delivery rule, so two sources can give two messages in
// one poll.
func concatQueues(fs []func(context.Context) ([]protocol.Message, error)) func(context.Context) ([]protocol.Message, error) {
	return func(ctx context.Context) ([]protocol.Message, error) {
		var all []protocol.Message
		for _, f := range fs {
			msgs, err := f(ctx)
			if err != nil {
				return nil, err
			}
			all = append(all, msgs...)
		}
		return all, nil
	}
}
