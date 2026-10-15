package app

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/auth"
)

func TestMarkModelAccessTagsOnlyAccessDenial(t *testing.T) {
	denied := fmt.Errorf("%w: %v", auth.ErrAccess, errors.New("auth: model access denied"))
	got := markModelAccess(denied)
	require.ErrorIs(t, got, acp.ErrModelAccess)
	require.ErrorIs(t, got, auth.ErrAccess)

	require.NoError(t, markModelAccess(nil))
	for _, err := range []error{auth.ErrNoCredential, auth.ErrMethod, auth.ErrRecovery} {
		out := markModelAccess(err)
		require.ErrorIs(t, out, err)
		require.NotErrorIs(t, out, acp.ErrModelAccess)
	}
}
