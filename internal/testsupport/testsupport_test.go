package testsupport

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"
)

func TestHelloWithTestify(t *testing.T) {
	require.NotEmpty(t, Hello(""))
	assert.Equal(t, "Hello, world!", Hello(""))
	assert.Equal(t, "Hello, gopher!", Hello("gopher"))
}
func TestGomockControllerLifecycle(t *testing.T) {
	// Generated mocks land in ./mocks after `go generate ./internal/testsupport`.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
}
