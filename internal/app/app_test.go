package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// TestModule_GraphIsComplete checks that every constructor has its
// dependencies. It does not run constructors or start servers.
func TestModule_GraphIsComplete(t *testing.T) {
	require.NoError(t, fx.ValidateApp(Module))
}
