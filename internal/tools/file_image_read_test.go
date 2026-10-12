package tools_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/bmp"

	"AskCore/pkg/protocol"
)

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))))
	return b.Bytes()
}

func (h *fileHarness) putBytes(rel string, data []byte) {
	h.t.Helper()
	require.NoError(h.t, os.WriteFile(h.path(rel), data, 0o644))
}

func TestReadImageReturnsImageBlock(t *testing.T) {
	h := newFileHarness(t, nil)
	data := solidPNG(t, 12, 8)
	h.putBytes("pixel.png", data)

	res := h.mustRun("read", map[string]any{"path": "pixel.png"})

	assert.Equal(t, "Read image file [image/png]", resultText(t, res))
	imgs := resultImages(res)
	require.Len(t, imgs, 1)
	assert.Equal(t, "image/png", imgs[0].MimeType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(data), imgs[0].Data)
	d := resultDetails(t, res)
	assert.Equal(t, protocol.ToolKindRead, d.Kind)
	assert.Equal(t, h.path("pixel.png"), d.Path)
}

func TestReadDetectsImagesByMagicBytes(t *testing.T) {
	h := newFileHarness(t, nil)
	h.putBytes("photo.txt", solidPNG(t, 4, 4))
	var b bytes.Buffer
	require.NoError(t, bmp.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 4, 4))))
	h.putBytes("scan.dat", b.Bytes())

	res := h.mustRun("read", map[string]any{"path": "photo.txt"})
	assert.Len(t, resultImages(res), 1)
	assert.Equal(t, "image/png", resultImages(res)[0].MimeType)

	res = h.mustRun("read", map[string]any{"path": "scan.dat"})
	require.Len(t, resultImages(res), 1)
	assert.Equal(t, "image/png", resultImages(res)[0].MimeType, "BMP is converted to PNG")
	assert.Contains(t, resultText(t, res), "[Image converted from image/bmp to image/png.]")
}

func TestReadLargeImageIsResizedUnderLimit(t *testing.T) {
	h := newFileHarness(t, nil)
	src := image.NewNRGBA(image.Rect(0, 0, 4000, 3000))
	rng := rand.New(rand.NewPCG(3, 5))
	for i := 0; i < len(src.Pix); i += 4 {
		v := rng.Uint32()
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = byte(v), byte(v>>8), byte(v>>16), 255
	}
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	require.NoError(t, enc.Encode(&b, src))
	h.putBytes("big.png", b.Bytes())

	res := h.mustRun("read", map[string]any{"path": "big.png"})

	imgs := resultImages(res)
	require.Len(t, imgs, 1)
	assert.LessOrEqual(t, len(imgs[0].Data), 4*1024*1024+512*1024)
	raw, err := base64.StdEncoding.DecodeString(imgs[0].Data)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, 2000)
	assert.LessOrEqual(t, cfg.Height, 2000)
	assert.Equal(t, [2]int{2000, 1500}, [2]int{cfg.Width, cfg.Height})
	assert.Contains(t, resultText(t, res),
		"[Image: original 4000x3000, displayed at 2000x1500. Multiply coordinates by 2.00 to map to original image.]")
}

func TestReadImageForNonVisionModelAddsNote(t *testing.T) {
	h := newFileHarness(t, func() []string { return []string{"text"} })
	h.putBytes("pixel.png", solidPNG(t, 4, 4))

	res := h.mustRun("read", map[string]any{"path": "pixel.png"})

	assert.Equal(t, "Read image file [image/png]\n[Current model does not support images. The image will be omitted from this request.]", resultText(t, res))
	assert.Empty(t, resultImages(res))
}

func TestReadImageForVisionModelHasNoNote(t *testing.T) {
	h := newFileHarness(t, func() []string { return []string{"text", "image"} })
	h.putBytes("pixel.png", solidPNG(t, 4, 4))

	res := h.mustRun("read", map[string]any{"path": "pixel.png"})

	assert.Equal(t, "Read image file [image/png]", resultText(t, res))
	assert.Len(t, resultImages(res), 1)
}

func TestReadUndecodableImageGivesTextOnly(t *testing.T) {
	h := newFileHarness(t, nil)
	// A PNG header followed by garbage: it is detected, then it cannot be
	// decoded for the size check.
	data := append(solidPNG(t, 4, 4)[:33], []byte("garbage")...)
	h.putBytes("broken.png", data)

	res := h.mustRun("read", map[string]any{"path": "broken.png"})

	assert.Equal(t, "Read image file [image/png]\n[Image omitted: could not be resized below the inline image size limit.]", resultText(t, res))
	assert.Empty(t, resultImages(res))
}
