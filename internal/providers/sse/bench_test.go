package sse

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// BenchmarkLongLine reads one long line in small chunks. Time per byte must
// stay the same at both sizes.
func BenchmarkLongLine(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20} {
		in := "data: " + strings.Repeat("x", size) + "\n\n"
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				rd := NewReader(&chunked{data: in, n: 1024})
				if _, err := rd.Next(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkManyEvents reads many small events.
func BenchmarkManyEvents(b *testing.B) {
	for _, count := range []int{1000, 100000} {
		in := strings.Repeat("event: delta\r\ndata: {\"a\":1}\r\n\r\n", count)
		b.Run(fmt.Sprintf("%d", count), func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				rd := NewReader(strings.NewReader(in))
				for {
					if _, err := rd.Next(context.Background()); err != nil {
						if err != io.EOF {
							b.Fatal(err)
						}
						break
					}
				}
			}
		})
	}
}

// chunked returns at most n bytes for each Read.
type chunked struct {
	data string
	n    int
}

func (c *chunked) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(c.n, len(p), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}
