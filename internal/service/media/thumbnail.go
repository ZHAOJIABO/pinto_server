package media

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"

	// Register the WebP decoder so clients may upload image/webp originals.
	_ "golang.org/x/image/webp"
)

const (
	// ThumbnailMaxSide caps the long edge. imaging.Fit never upscales, so a
	// smaller original is emitted at its own size.
	ThumbnailMaxSide = 600
	// ThumbnailQuality is lossy WebP quality.
	ThumbnailQuality = 80
	// ThumbnailContentType is the stored object's content type.
	ThumbnailContentType = "image/webp"

	thumbnailSuffix = "-low.webp"
)

// GenerateThumbnail resizes raw to a long edge of at most ThumbnailMaxSide and
// encodes it as lossy WebP.
//
// Scaling happens in one Lanczos pass straight into the WebP encoder rather
// than through an intermediate JPEG, which would compress the image twice.
//
// JPEG, PNG, GIF, TIFF, BMP and WebP decode; HEIC does not, so HEIC uploads
// (accepted for the "original" and "style_input" purposes) get no thumbnail.
func GenerateThumbnail(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("source image is empty")
	}

	// AutoOrientation applies the EXIF orientation tag; phone photos are
	// frequently stored rotated and would otherwise be thumbnailed sideways.
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode source image: %w", err)
	}

	resized := imaging.Fit(img, ThumbnailMaxSide, ThumbnailMaxSide, imaging.Lanczos)

	buf := new(bytes.Buffer)
	if err := webp.Encode(buf, resized, &webp.Options{Quality: ThumbnailQuality}); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}
	return buf.Bytes(), nil
}

// ThumbnailFileKey derives the thumbnail object key for an original object key,
// e.g. "ai_output/2026/08/06/7/abc.png" -> "ai_output/2026/08/06/7/abc-low.webp".
func ThumbnailFileKey(fileKey string) string {
	fileKey = strings.TrimSpace(fileKey)
	if fileKey == "" {
		return ""
	}
	return strings.TrimSuffix(fileKey, path.Ext(fileKey)) + thumbnailSuffix
}
