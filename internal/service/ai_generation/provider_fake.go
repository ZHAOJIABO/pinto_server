package ai_generation

import "github.com/google/uuid"

type FakeProvider struct{}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

func (p *FakeProvider) Submit(styleKey, prompt, inputImageURL string) (*ProviderResult, error) {
	return &ProviderResult{
		JobID:     uuid.NewString(),
		Status:    "succeeded",
		OutputURL: "https://fake-ai-output.example.com/" + uuid.NewString() + ".png",
	}, nil
}

func (p *FakeProvider) Query(jobID string) (*ProviderResult, error) {
	return &ProviderResult{
		JobID:     jobID,
		Status:    "succeeded",
		OutputURL: "https://fake-ai-output.example.com/" + jobID + ".png",
	}, nil
}
