package tools

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/bmp"
)

// noiseImage returns an opaque image of random pixels. It does not compress,
// so its encoded size follows its pixel count.
func noiseImage(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < len(im.Pix); i += 4 {
		v := rng.Uint32()
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = byte(v), byte(v>>8), byte(v>>16), 255
	}
	return im
}

func encodePNG(t *testing.T, im image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	require.NoError(t, enc.Encode(&b, im))
	return b.Bytes()
}

func encodeJPEG(t *testing.T, im image.Image, quality int) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, im, &jpeg.Options{Quality: quality}))
	return b.Bytes()
}

func decodedSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

// jpegWithOrientation inserts an Exif APP1 segment with the given orientation
// after the SOI marker of a JPEG.
func jpegWithOrientation(t *testing.T, data []byte, orientation int, bigEndian bool) []byte {
	t.Helper()
	var tiff []byte
	if bigEndian {
		tiff = []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(orientation), 0, 0, 0, 0, 0, 0}
	} else {
		tiff = []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(orientation), 0, 0, 0, 0, 0, 0, 0}
	}
	body := append([]byte("Exif\x00\x00"), tiff...)
	n := len(body) + 2
	seg := append([]byte{0xff, 0xe1, byte(n >> 8), byte(n)}, body...)
	out := append([]byte{}, data[:2]...)
	out = append(out, seg...)
	return append(out, data[2:]...)
}

func TestDetectImageByMagicBytes(t *testing.T) {
	small := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	var gifBuf, bmpBuf bytes.Buffer
	require.NoError(t, gif.Encode(&gifBuf, small, nil))
	require.NoError(t, bmp.Encode(&bmpBuf, small))
	webpHeader := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 20)...)

	jpegLS := append([]byte{}, encodeJPEG(t, small, 80)...)
	jpegLS[3] = 0xf7

	// An animated PNG has an acTL chunk before its first IDAT chunk.
	pngData := encodePNG(t, small)
	actl := []byte{0, 0, 0, 8, 'a', 'c', 'T', 'L', 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	apng := append(append(append([]byte{}, pngData[:33]...), actl...), pngData[33:]...)

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", encodeJPEG(t, small, 80), "image/jpeg"},
		{"png", pngData, "image/png"},
		{"gif", gifBuf.Bytes(), "image/gif"},
		{"webp", webpHeader, "image/webp"},
		{"bmp", bmpBuf.Bytes(), "image/bmp"},
		{"jpeg with 0xf7 at byte 3", jpegLS, ""},
		{"animated png", apng, ""},
		{"text", []byte("hello, this is text"), ""},
		{"empty", nil, ""},
		{"riff without webp", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), ""},
		{"bm text", []byte("BM is a prefix of this sentence and no bitmap"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, detectImage(c.data))
		})
	}
}

func TestExifOrientation(t *testing.T) {
	data := encodeJPEG(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)), 80)
	assert.Equal(t, 1, exifOrientation(data), "no Exif segment")
	for o := 1; o <= 8; o++ {
		assert.Equal(t, o, exifOrientation(jpegWithOrientation(t, data, o, false)), "little endian %d", o)
		assert.Equal(t, o, exifOrientation(jpegWithOrientation(t, data, o, true)), "big endian %d", o)
	}
	assert.Equal(t, 1, exifOrientation(jpegWithOrientation(t, data, 9, false)), "out of range")
	assert.Equal(t, 1, exifOrientation(nil))
	assert.Equal(t, 1, exifOrientation([]byte{0xff, 0xd8, 0xff, 0xe1, 0x00}), "truncated segment")
	assert.Equal(t, 1, exifOrientation([]byte("not a jpeg")))
}

func TestOrientAppliesAllEightOrientations(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i, v := range []uint8{1, 2, 3, 4} {
		src.SetNRGBA(i%2, i/2, color.NRGBA{R: v, A: 255})
	}
	grid := func(im *image.NRGBA) [][]uint8 {
		var rows [][]uint8
		for y := 0; y < im.Bounds().Dy(); y++ {
			var row []uint8
			for x := 0; x < im.Bounds().Dx(); x++ {
				row = append(row, im.NRGBAAt(x, y).R)
			}
			rows = append(rows, row)
		}
		return rows
	}
	want := map[int][][]uint8{
		2: {{2, 1}, {4, 3}}, // flip horizontal
		3: {{4, 3}, {2, 1}}, // rotate 180
		4: {{3, 4}, {1, 2}}, // flip vertical
		5: {{1, 3}, {2, 4}}, // transpose
		6: {{3, 1}, {4, 2}}, // rotate 90 clockwise
		7: {{4, 2}, {3, 1}}, // anti-transpose
		8: {{2, 4}, {1, 3}}, // rotate 90 counterclockwise
	}
	for o, w := range want {
		assert.Equal(t, w, grid(orient(src, o)), "orientation %d", o)
	}

	// A rotation swaps the sides of a non-square image.
	wide := image.NewNRGBA(image.Rect(0, 0, 5, 2))
	assert.Equal(t, image.Rect(0, 0, 2, 5), orient(wide, 6).Bounds())
}

func TestToInlineKeepsSmallImagesUnchanged(t *testing.T) {
	small := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	var gifBuf bytes.Buffer
	require.NoError(t, gif.Encode(&gifBuf, small, nil))
	// A 1x1 lossless WebP.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	require.NoError(t, err)

	for name, c := range map[string]struct {
		data []byte
		mime string
	}{
		"png":  {encodePNG(t, small), "image/png"},
		"jpeg": {encodeJPEG(t, small, 80), "image/jpeg"},
		"gif":  {gifBuf.Bytes(), "image/gif"},
		"webp": {webp, "image/webp"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := toInline(c.data, c.mime, defaultImageLimits)
			require.NoError(t, err)
			assert.Equal(t, c.data, got.data)
			assert.Equal(t, c.mime, got.mime)
			assert.Empty(t, got.hints)
		})
	}
}

func TestToInlineWithoutAutoResizeKeepsLargeImage(t *testing.T) {
	data := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 3000, 3000)))
	lim := defaultImageLimits
	lim.autoResize = false

	got, err := toInline(data, "image/png", lim)

	require.NoError(t, err)
	assert.Equal(t, data, got.data)
}

func TestReadResizeLadderFallsBackToJpegQualities(t *testing.T) {
	src := noiseImage(600, 400)
	data := encodePNG(t, src)
	q := map[int][]byte{}
	for _, quality := range jpegQualities {
		q[quality] = encodeJPEG(t, src, quality)
	}
	b64 := func(b []byte) int { return base64.StdEncoding.EncodedLen(len(b)) }
	// The qualities are tried in the order 80, 85, 70, 55, 40. Allow only the
	// quality 55 and below.
	require.Greater(t, b64(q[70]), b64(q[55]))
	lim := defaultImageLimits
	lim.maxBase64 = b64(q[55]) + 1
	require.Greater(t, b64(data), lim.maxBase64, "the PNG must not fit")

	got, err := toInline(data, "image/png", lim)

	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", got.mime)
	assert.Equal(t, q[55], got.data, "quality 55 is the first that fits")
	w, h := decodedSize(t, got.data)
	assert.Equal(t, [2]int{600, 400}, [2]int{w, h}, "no scaling is needed")
}

func TestReadResizeLadderScalesDownWhenQualityIsNotEnough(t *testing.T) {
	src := noiseImage(600, 400)
	data := encodePNG(t, src)
	lowest := encodeJPEG(t, src, 40)
	lim := defaultImageLimits
	lim.maxBase64 = base64.StdEncoding.EncodedLen(len(lowest)) * 7 / 10

	got, err := toInline(data, "image/png", lim)

	require.NoError(t, err)
	w, h := decodedSize(t, got.data)
	assert.Equal(t, [2]int{450, 300}, [2]int{w, h}, "one step of 0.75")
	assert.Less(t, base64.StdEncoding.EncodedLen(len(got.data)), lim.maxBase64)
	require.Len(t, got.hints, 1)
	assert.Equal(t, "[Image: original 600x400, displayed at 450x300. Multiply coordinates by 1.33 to map to original image.]", got.hints[0])
}

func TestReadImageTooLargeAfterResizeIsOmitted(t *testing.T) {
	data := encodePNG(t, noiseImage(50, 50))
	lim := defaultImageLimits
	lim.maxBase64 = 10

	_, err := toInline(data, "image/png", lim)

	var ie *imageError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, "[Image omitted: could not be resized below the inline image size limit.]", ie.msg)
}

func TestReadBMPConvertedToPNG(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, bmp.Encode(&b, noiseImage(16, 12)))

	got, err := toInline(b.Bytes(), "image/bmp", defaultImageLimits)

	require.NoError(t, err)
	assert.Equal(t, "image/png", got.mime)
	assert.Equal(t, []string{"[Image converted from image/bmp to image/png.]"}, got.hints)
	im, err := png.Decode(bytes.NewReader(got.data))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 16, 12), im.Bounds())
}

func TestReadUndecodableImageIsOmitted(t *testing.T) {
	// A header that passes detection, with no pixel data behind it.
	hdr := make([]byte, 54)
	copy(hdr, "BM")
	hdr[2], hdr[3] = 0x66, 0x75 // declared file size 30054
	hdr[10] = 54                // pixel data offset
	hdr[14] = 40                // DIB header size
	hdr[18], hdr[22] = 100, 100 // width, height
	hdr[26] = 1                 // planes
	hdr[28] = 24                // bits per pixel
	require.Equal(t, "image/bmp", detectImage(hdr))

	_, err := toInline(hdr, "image/bmp", defaultImageLimits)

	var ie *imageError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, "[Image omitted: could not be converted to a supported inline image format.]", ie.msg)
}

func TestReadJPEGWithExifOrientation(t *testing.T) {
	// 3000x1000 needs a resize; orientation 6 turns it into 1000x3000, which
	// fits 667x2000.
	plain := image.NewNRGBA(image.Rect(0, 0, 3000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 3000; x++ {
			plain.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 12), G: uint8(y / 4), B: 90, A: 255})
		}
	}
	data := jpegWithOrientation(t, encodeJPEG(t, plain, 90), 6, false)

	got, err := toInline(data, "image/jpeg", defaultImageLimits)

	require.NoError(t, err)
	w, h := decodedSize(t, got.data)
	assert.Equal(t, [2]int{667, 2000}, [2]int{w, h})
	require.Len(t, got.hints, 1)
	assert.Contains(t, got.hints[0], "original 1000x3000, displayed at 667x2000")

	// Rotated 90 degrees clockwise: the red gradient that ran left to right
	// now runs top to bottom, and the green gradient that ran top to bottom
	// now runs right to left.
	out, _, err := image.Decode(bytes.NewReader(got.data))
	require.NoError(t, err)
	red := func(x, y int) uint32 { r, _, _, _ := out.At(x, y).RGBA(); return r >> 8 }
	green := func(x, y int) uint32 { _, g, _, _ := out.At(x, y).RGBA(); return g >> 8 }
	assert.Greater(t, red(300, 1900), red(300, 100))
	assert.Greater(t, green(50, 1000), green(600, 1000))
}

func TestReadJPEGWithExifOrientationWithinLimitsIsKept(t *testing.T) {
	data := jpegWithOrientation(t, encodeJPEG(t, image.NewNRGBA(image.Rect(0, 0, 40, 20)), 80), 6, false)

	got, err := toInline(data, "image/jpeg", defaultImageLimits)

	require.NoError(t, err)
	assert.Equal(t, data, got.data, "the tag stays in the file for the consumer")
}
