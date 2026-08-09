package ai_generation

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"

	"github.com/google/uuid"
)

// FakeProvider returns a solid-colour placeholder image so the whole pipeline
// (OSS transcode, status transitions, credit accounting) can run locally
// without a provider account. It must never be enabled in production.
// It is registered under every configured model key so that any style row
// resolves locally.
type FakeProvider struct {
	name string
}

func NewFakeProvider(name string) *FakeProvider {
	if name == "" {
		name = "fake"
	}
	return &FakeProvider{name: name}
}

func (p *FakeProvider) Name() string { return p.name }

func (p *FakeProvider) Mode() Mode { return ModeSync }

func (p *FakeProvider) Submit(_ context.Context, req *SubmitRequest) (*Result, error) {
	content, err := placeholderPNG(req.StyleKey)
	if err != nil {
		return nil, err
	}
	return &Result{
		JobID:      uuid.NewString(),
		Status:     StatusSucceeded,
		ImageBytes: content,
		ImageMIME:  "image/png",
	}, nil
}

func (p *FakeProvider) Query(_ context.Context, _ string) (*Result, error) {
	return nil, ErrQueryUnsupported
}

func placeholderPNG(styleKey string) ([]byte, error) {
	const size = 256
	var hash uint32 = 2166136261
	for _, char := range []byte(styleKey) {
		hash = (hash ^ uint32(char)) * 16777619
	}
	fill := color.RGBA{R: uint8(hash), G: uint8(hash >> 8), B: uint8(hash >> 16), A: 255}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, fill)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
