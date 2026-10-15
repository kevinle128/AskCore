package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patternBytes returns n deterministic bytes so that a wrong slice is visible.
func patternBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func fillBuffer(b *buffer, data []byte, chunk int) {
	for len(data) > 0 {
		n := min(chunk, len(data))
		b.write(data[:n])
		data = data[n:]
	}
}

func TestBufferKeepsHeadAndTailAndCountsDropped(t *testing.T) {
	const size = 1 << 20
	data := patternBytes(size)
	var b buffer
	fillBuffer(&b, data, 7*1024)

	head, tail, total := b.snapshot()
	require.Equal(t, int64(size), total)
	assert.Equal(t, data[:headSize], head)
	assert.Equal(t, data[size-tailSize:], tail)
	dropped := total - int64(len(head)) - int64(len(tail))
	assert.Equal(t, int64(size-headSize-tailSize), dropped)
}

func TestBufferSmallOutputIsKeptWhole(t *testing.T) {
	data := patternBytes(headSize + 1000)
	var b buffer
	fillBuffer(&b, data, 333)

	head, tail, total := b.snapshot()
	assert.Equal(t, int64(len(data)), total)
	assert.Equal(t, data, append(append([]byte{}, head...), tail...))
}

func TestBufferOversizedChunkKeepsLastBytes(t *testing.T) {
	data := patternBytes(3 * tailSize)
	var b buffer
	b.write(data)

	head, tail, total := b.snapshot()
	assert.Equal(t, int64(len(data)), total)
	assert.Equal(t, data[:headSize], head)
	assert.Equal(t, data[len(data)-tailSize:], tail)
}

func TestBufferReadFromReportsSkippedBytes(t *testing.T) {
	const size = 1 << 20
	data := patternBytes(size)
	var b buffer
	fillBuffer(&b, data, 7*1024)
	tailStart := int64(size - tailSize)

	t.Run("cursor inside head returns head rest and tail", func(t *testing.T) {
		got, next, skipped := b.readFrom(100)
		want := append(append([]byte{}, data[100:headSize]...), data[size-tailSize:]...)
		assert.Equal(t, want, got)
		assert.Equal(t, int64(size), next)
		assert.Equal(t, tailStart-headSize, skipped)
	})
	t.Run("cursor inside dropped middle", func(t *testing.T) {
		got, next, skipped := b.readFrom(500_000)
		assert.Equal(t, data[size-tailSize:], got)
		assert.Equal(t, int64(size), next)
		assert.Equal(t, tailStart-500_000, skipped)
	})
	t.Run("cursor inside tail", func(t *testing.T) {
		cursor := tailStart + 10
		got, next, skipped := b.readFrom(cursor)
		assert.Equal(t, data[cursor:], got)
		assert.Equal(t, int64(size), next)
		assert.Zero(t, skipped)
	})
	t.Run("cursor at end", func(t *testing.T) {
		got, next, skipped := b.readFrom(int64(size))
		assert.Empty(t, got)
		assert.Equal(t, int64(size), next)
		assert.Zero(t, skipped)
	})
	t.Run("cursor past end and negative cursor", func(t *testing.T) {
		got, next, _ := b.readFrom(int64(size) + 5)
		assert.Empty(t, got)
		assert.Equal(t, int64(size), next)
		got, _, _ = b.readFrom(-4)
		assert.Equal(t, data[:headSize], got[:headSize])
	})
}

func TestBufferReadFromSmallOutput(t *testing.T) {
	var b buffer
	b.write([]byte("0123456789"))
	got, next, skipped := b.readFrom(3)
	assert.Equal(t, "3456789", string(got))
	assert.Equal(t, int64(10), next)
	assert.Zero(t, skipped)
}
