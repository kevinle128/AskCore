package fantasykit

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWitnessDoneMarkerAcrossReads(t *testing.T) {
	w := NewWitness("finish_reason")
	body := witnessBody{ReadCloser: io.NopCloser(strings.NewReader("data: {}\n\ndata: [DONE]\n\n")), w: w}
	buf := make([]byte, 3)
	for {
		_, err := body.Read(buf)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	assert.True(t, w.SawDone())
}
