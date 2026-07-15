package ai_generation

type ProviderResult struct {
	JobID      string
	Status     string // "pending", "running", "succeeded", "failed"
	OutputURL  string
	ErrorCode  string
	ErrorMsg   string
}

type Provider interface {
	Submit(styleKey, prompt, inputImageURL string) (*ProviderResult, error)
	Query(jobID string) (*ProviderResult, error)
}
