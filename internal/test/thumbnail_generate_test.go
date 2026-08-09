package test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"golang.org/x/image/webp"
)

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("encode source png: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateThumbnail_CapsLongEdgeAndKeepsAspectRatio(t *testing.T) {
	thumbnail, err := media.GenerateThumbnail(pngBytes(t, 1600, 800))
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}

	// Decoding proves the output is real WebP rather than just checking the
	// RIFF magic bytes.
	img, err := webp.Decode(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("decode thumbnail as webp: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != media.ThumbnailMaxSide || bounds.Dy() != media.ThumbnailMaxSide/2 {
		t.Errorf("thumbnail size = %dx%d, want %dx%d",
			bounds.Dx(), bounds.Dy(), media.ThumbnailMaxSide, media.ThumbnailMaxSide/2)
	}
}

func TestGenerateThumbnail_DoesNotUpscaleSmallImages(t *testing.T) {
	thumbnail, err := media.GenerateThumbnail(pngBytes(t, 120, 90))
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	img, err := webp.Decode(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("decode thumbnail as webp: %v", err)
	}
	if bounds := img.Bounds(); bounds.Dx() != 120 || bounds.Dy() != 90 {
		t.Errorf("thumbnail size = %dx%d, want 120x90", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerateThumbnail_RejectsUndecodableInput(t *testing.T) {
	if _, err := media.GenerateThumbnail(nil); err == nil {
		t.Error("GenerateThumbnail(nil) succeeded, want error")
	}
	if _, err := media.GenerateThumbnail([]byte("not an image")); err == nil {
		t.Error("GenerateThumbnail on garbage succeeded, want error")
	}
}

func TestThumbnailFileKey(t *testing.T) {
	cases := map[string]string{
		"ai_output/2026/08/06/7/abc.png": "ai_output/2026/08/06/7/abc-low.webp",
		"finished_product/1/photo.jpeg":  "finished_product/1/photo-low.webp",
		"admin_preview/no-extension":     "admin_preview/no-extension-low.webp",
		"style_input/already-a.webp":     "style_input/already-a-low.webp",
		"":                               "",
		"   ":                            "",
	}
	for fileKey, want := range cases {
		if got := media.ThumbnailFileKey(fileKey); got != want {
			t.Errorf("ThumbnailFileKey(%q) = %q, want %q", fileKey, got, want)
		}
	}
}
