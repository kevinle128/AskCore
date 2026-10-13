package tools

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Image limits of the inline images that read returns. They are Pi's
// defaults: the longest side fits 2000 pixels, and the base64 text stays
// below 4.5 MiB, which leaves room under a 5 MiB provider limit.
const (
	inlineMaxSide   = 2000
	inlineMaxBase64 = 4*1024*1024 + 512*1024
	// imageSniffBytes is the number of leading bytes that identify an image.
	imageSniffBytes = 4100
)

// jpegQualities is the order in which the resize ladder tries JPEG
// qualities after the PNG attempt of a size.
var jpegQualities = []int{80, 85, 70, 55, 40}

// Texts of the two ways an image cannot be returned inline.
const (
	imageNotConvertible = "[Image omitted: could not be converted to a supported inline image format.]"
	imageNotResizable   = "[Image omitted: could not be resized below the inline image size limit.]"
)

// imageLimits holds the resize settings of read. The zero value is not
// usable; defaultImageLimits has the defaults. Automatic resizing is on and
// images are never blocked, as in Pi; a later settings layer can change them.
type imageLimits struct {
	autoResize bool
	maxWidth   int
	maxHeight  int
	// maxBase64 is the exclusive upper bound of the base64 length.
	maxBase64 int
}

var defaultImageLimits = imageLimits{
	autoResize: true,
	maxWidth:   inlineMaxSide,
	maxHeight:  inlineMaxSide,
	maxBase64:  inlineMaxBase64,
}

// inlineImage is an image that is ready to go to the model.
type inlineImage struct {
	data []byte
	mime string
	// hints are lines for the model text, for example a conversion note.
	hints []string
}

// imageError is a failure that read shows to the model as a normal result.
type imageError struct{ msg string }

func (e *imageError) Error() string { return e.msg }

// detectImage returns the MIME type of the image that head starts, or "" when
// it is no supported image. It looks at magic bytes only. head needs at most
// imageSniffBytes bytes.
func detectImage(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte{0xff, 0xd8, 0xff}):
		if len(head) > 3 && head[3] == 0xf7 {
			return "" // JPEG-LS is not a JPEG that browsers and models decode
		}
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		if isPNG(head) && !isAnimatedPNG(head) {
			return "image/png"
		}
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif"
	case len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(head, []byte("BM")) && isBMP(head):
		return "image/bmp"
	}
	return ""
}

func isPNG(b []byte) bool {
	return len(b) >= 16 && be32(b, 8) == 13 && bytes.Equal(b[12:16], []byte("IHDR"))
}

// isAnimatedPNG reports an acTL chunk before the first IDAT chunk.
func isAnimatedPNG(b []byte) bool {
	for off := 8; off+8 <= len(b); {
		size := int(be32(b, off))
		switch string(b[off+4 : off+8]) {
		case "acTL":
			return true
		case "IDAT":
			return false
		}
		next := off + 8 + size + 4
		if next <= off || next > len(b) {
			return false
		}
		off = next
	}
	return false
}

func isBMP(b []byte) bool {
	if len(b) < 26 {
		return false
	}
	fileSize, pixelOffset, dibSize := le32(b, 2), le32(b, 10), le32(b, 14)
	if fileSize != 0 && fileSize < 26 {
		return false
	}
	if pixelOffset < 14+dibSize {
		return false
	}
	if fileSize != 0 && pixelOffset >= fileSize {
		return false
	}
	var planes, bits uint32
	switch {
	case dibSize == 12:
		planes, bits = le16(b, 22), le16(b, 24)
	case dibSize >= 40 && dibSize <= 124:
		if len(b) < 30 {
			return false
		}
		planes, bits = le16(b, 26), le16(b, 28)
	default:
		return false
	}
	switch bits {
	case 1, 4, 8, 16, 24, 32:
		return planes == 1
	}
	return false
}

func be32(b []byte, off int) uint32 {
	return uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3])
}

func le16(b []byte, off int) uint32 { return uint32(b[off]) | uint32(b[off+1])<<8 }

func le32(b []byte, off int) uint32 {
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

// exifOrientation returns the EXIF orientation (1 to 8) of a JPEG, read from
// the APP1 segment. It returns 1 for data without a valid orientation tag.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	tiff := -1
	for off := 2; off+1 < len(data); {
		if data[off] != 0xff {
			break
		}
		marker := data[off+1]
		if marker == 0xff {
			off++
			continue
		}
		if off+4 > len(data) {
			break
		}
		if start := off + 4; marker == 0xe1 && start+6 <= len(data) && bytes.Equal(data[start:start+6], []byte("Exif\x00\x00")) {
			tiff = start + 6
			break
		}
		off += 2 + int(data[off+2])<<8 + int(data[off+3])
	}
	if tiff < 0 || tiff+8 > len(data) {
		return 1
	}
	var u16 func(int) int
	var u32 func(int) int
	switch {
	case data[tiff] == 'I' && data[tiff+1] == 'I':
		u16 = func(p int) int { return int(data[p]) | int(data[p+1])<<8 }
		u32 = func(p int) int { return int(le32(data, p)) }
	case data[tiff] == 'M' && data[tiff+1] == 'M':
		u16 = func(p int) int { return int(data[p])<<8 | int(data[p+1]) }
		u32 = func(p int) int { return int(be32(data, p)) }
	default:
		return 1
	}
	ifd := tiff + u32(tiff+4)
	if ifd < tiff || ifd+2 > len(data) {
		return 1
	}
	count := u16(ifd)
	for i := 0; i < count; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(data) {
			return 1
		}
		if u16(entry) == 0x0112 {
			if v := u16(entry + 8); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// toInline prepares an image for the model. BMP is converted to PNG. With
// automatic resizing, an image over the size limits is scaled and re-encoded
// until it fits. The returned error is an *imageError whose text is shown to
// the model.
func toInline(data []byte, mime string, lim imageLimits) (inlineImage, error) {
	out := inlineImage{data: data, mime: mime}
	var decoded image.Image

	if mime == "image/bmp" {
		im, err := bmp.Decode(bytes.NewReader(data))
		if err != nil {
			return inlineImage{}, &imageError{imageNotConvertible}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, im); err != nil {
			return inlineImage{}, &imageError{imageNotConvertible}
		}
		out = inlineImage{data: buf.Bytes(), mime: "image/png", hints: []string{"[Image converted from image/bmp to image/png.]"}}
		decoded = im
	}
	if !lim.autoResize {
		return out, nil
	}

	if decoded == nil {
		im, err := decodeImage(out.data, out.mime)
		if err != nil {
			return inlineImage{}, &imageError{imageNotResizable}
		}
		decoded = im
	}
	orientation := 1
	if out.mime == "image/jpeg" {
		orientation = exifOrientation(out.data)
	}
	b := decoded.Bounds()
	w, h := b.Dx(), b.Dy()
	if orientation >= 5 {
		w, h = h, w
	}
	if w <= lim.maxWidth && h <= lim.maxHeight && base64.StdEncoding.EncodedLen(len(out.data)) < lim.maxBase64 {
		return out, nil
	}

	src := toNRGBA(decoded)
	if orientation > 1 {
		src = orient(src, orientation)
	}
	resized, err := shrinkToFit(src, lim)
	if err != nil {
		return inlineImage{}, err
	}
	scale := float64(w) / float64(resized.width)
	note := fmt.Sprintf("[Image: original %dx%d, displayed at %dx%d. Multiply coordinates by %.2f to map to original image.]",
		w, h, resized.width, resized.height, scale)
	return inlineImage{data: resized.data, mime: resized.mime, hints: append(out.hints, note)}, nil
}

type shrunkImage struct {
	data          []byte
	mime          string
	width, height int
}

// shrinkToFit scales src into the size limits, then encodes it as PNG and as
// JPEG at falling qualities, and scales down by a quarter again until the
// base64 text fits.
func shrinkToFit(src *image.NRGBA, lim imageLimits) (shrunkImage, error) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w > lim.maxWidth {
		h = int(math.Round(float64(h) * float64(lim.maxWidth) / float64(w)))
		w = lim.maxWidth
	}
	if h > lim.maxHeight {
		w = int(math.Round(float64(w) * float64(lim.maxHeight) / float64(h)))
		h = lim.maxHeight
	}
	w, h = max(w, 1), max(h, 1)

	for {
		scaled := src
		if sb := src.Bounds(); sb.Dx() != w || sb.Dy() != h {
			scaled = image.NewNRGBA(image.Rect(0, 0, w, h))
			draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
		}
		fits := func(b []byte) bool { return base64.StdEncoding.EncodedLen(len(b)) < lim.maxBase64 }

		var buf bytes.Buffer
		if err := png.Encode(&buf, scaled); err == nil && fits(buf.Bytes()) {
			return shrunkImage{data: buf.Bytes(), mime: "image/png", width: w, height: h}, nil
		}
		for _, q := range jpegQualities {
			buf.Reset()
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: q}); err == nil && fits(buf.Bytes()) {
				return shrunkImage{data: buf.Bytes(), mime: "image/jpeg", width: w, height: h}, nil
			}
		}

		nw, nh := max(1, w*3/4), max(1, h*3/4)
		if nw == w && nh == h {
			return shrunkImage{}, &imageError{imageNotResizable}
		}
		w, h = nw, nh
	}
}

func decodeImage(data []byte, mime string) (image.Image, error) {
	r := bytes.NewReader(data)
	switch mime {
	case "image/png":
		return png.Decode(r)
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/gif":
		return gif.Decode(r)
	case "image/webp":
		return webp.Decode(r)
	case "image/bmp":
		return bmp.Decode(r)
	}
	return nil, errors.New("unsupported image type " + mime)
}

func toNRGBA(im image.Image) *image.NRGBA {
	if n, ok := im.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := im.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), im, b.Min, draw.Src)
	return out
}

// orient applies an EXIF orientation (2 to 8) to src and returns the upright
// image.
func orient(src *image.NRGBA, orientation int) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			s, d := src.PixOffset(x, y), dst.PixOffset(dx, dy)
			copy(dst.Pix[d:d+4], src.Pix[s:s+4])
		}
	}
	return dst
}
