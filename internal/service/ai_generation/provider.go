package ai_generation

import (
	"context"
	"errors"
)

// ErrQueryUnsupported is returned by synchronous providers, whose Submit
// already carries the final result.
var ErrQueryUnsupported = errors.New("provider does not support querying job status")

// Mode describes how a provider delivers its result, which determines whether
// the dispatcher can finish a task inside a single worker slot.
type Mode int

const (
	// ModeSync blocks until the image is ready and returns it inline.
	ModeSync Mode = iota
	// ModePoll returns a job id that must be polled.
	ModePoll
	// ModeWebhook returns a job id and calls back on completion.
	ModeWebhook
)

type Status int

const (
	StatusRunning Status = iota
	StatusSucceeded
	StatusFailed
)

type SubmitRequest struct {
	StyleKey  string
	Prompt    string
	Negative  string
	ModelName string
	// Options carries provider-specific knobs (size, quality, background,
	// moderation) so adding one does not change this struct.
	Options map[string]string

	InputImage []byte
	InputName  string
	InputMIME  string
}

type Result struct {
	JobID  string
	Status Status

	// ImageBytes holds the decoded image for ModeSync providers. It must never
	// be logged or persisted to the database.
	ImageBytes []byte
	ImageMIME  string
	// OutputURL is set instead of ImageBytes by providers that host the result.
	OutputURL string

	ErrorCode string
	ErrorMsg  string
	// Retryable is only true for failures that prove generation never started,
	// so retrying cannot pay for the same image twice.
	Retryable bool
}

type Provider interface {
	Name() string
	Mode() Mode
	Submit(ctx context.Context, req *SubmitRequest) (*Result, error)
	Query(ctx context.Context, jobID string) (*Result, error)
}
