package pipeline

import (
	"AskCore/internal/providers"
	"context"
	"time"
)

func defaultRecovery(_ context.Context, in RecoverInput) (RecoverAction, error) {
	f, ok := providers.AsFailure(in.Failure)
	if !ok || in.Retry > in.Policy.MaxRetries || in.Retry < 1 {
		return RecoverAction{}, nil
	}
	switch f.Code {
	case providers.CodeEmptyResponse, providers.CodeRateLimit, providers.CodeServer, providers.CodeTimeout, providers.CodeTransport:
	default:
		return RecoverAction{}, nil
	}
	if f.RetryAfter > in.Policy.MaxDelay {
		return RecoverAction{}, nil
	}
	delay := in.Policy.BaseDelay
	for i := 1; i < in.Retry && delay < in.Policy.MaxDelay; i++ {
		delay *= 2
	}
	delay = min(delay, in.Policy.MaxDelay)
	if f.RetryAfter > 0 {
		delay = f.RetryAfter
	}
	return RecoverAction{Retry: true, Delay: time.Duration(delay)}, nil
}
