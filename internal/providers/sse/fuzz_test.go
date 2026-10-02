package sse

import (
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

// FuzzSplitEqualsUnsplit checks that where the input is cut does not change
// the events or the final error.
func FuzzSplitEqualsUnsplit(f *testing.F) {
	for _, s := range []string{
		"data: a\r\n\r\n",
		"\xEF\xBB\xBFevent: x\ndata: é\r\r",
		": c\ndata\nevent\n\n",
		"data: x",
		"\xEF\xBBdata: y\n\n",
	} {
		f.Add(s, 3)
	}
	f.Fuzz(func(t *testing.T, in string, at int) {
		opts := []Option{WithMaxLineBytes(64), WithMaxEventBytes(128)}
		want, werr := readAll(strings.NewReader(in), opts...)
		if at < 0 {
			at = -at
		}
		if at < 0 || len(in) == 0 {
			at = 0
		} else {
			at %= len(in) + 1
		}
		got, err := readAll(&splitReader{data: in, at: at}, opts...)
		require.Equal(t, werr, err)
		require.Equal(t, want, got)
		got, err = readAll(iotest.OneByteReader(strings.NewReader(in)), opts...)
		require.Equal(t, werr, err)
		require.Equal(t, want, got)
	})
}
