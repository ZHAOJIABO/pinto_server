package model

import "time"

type AIStyle struct {
	BaseModel
	StyleKey       string `gorm:"type:varchar(64);uniqueIndex;not null" json:"style_key"`
	Name           string `gorm:"type:varchar(128);not null" json:"name"`
	Description    string `gorm:"type:varchar(512)" json:"description"`
	CoverURL       string `gorm:"type:varchar(512)" json:"cover_url"`
	ExampleURL     string `gorm:"type:varchar(512)" json:"example_url"`
	CostCredits    int    `gorm:"default:0" json:"cost_credits"`
	SortOrder      int    `gorm:"default:0" json:"sort_order"`
	Status         int8   `gorm:"type:tinyint;default:1" json:"status"` // 1:active 0:inactive
	Provider       string `gorm:"type:varchar(32)" json:"provider"`
	ModelName      string `gorm:"type:varchar(64)" json:"model_name"`
	PromptTemplate string `gorm:"type:text" json:"prompt_template"`
	NegativePrompt string `gorm:"type:text" json:"negative_prompt"`
	Config         string `gorm:"type:text" json:"config"`
}

func (AIStyle) TableName() string { return "bb_ai_style" }

type AIGeneration struct {
	BaseModel
	TaskID          string     `gorm:"type:varchar(36);uniqueIndex;not null" json:"task_id"`
	UserID          uint64     `gorm:"not null;uniqueIndex:uk_ai_gen_user_request;index:idx_ai_gen_user_created" json:"user_id"`
	ClientRequestID string     `gorm:"type:varchar(64);not null;uniqueIndex:uk_ai_gen_user_request" json:"client_request_id"`
	StyleID         uint64     `gorm:"not null" json:"style_id"`
	InputFileKey    string     `gorm:"type:varchar(512)" json:"input_file_key"`
	InputImageURL   string     `gorm:"type:varchar(1024)" json:"input_image_url"`
	OutputFileKey   string     `gorm:"type:varchar(512)" json:"output_file_key"`
	OutputImageURL  string     `gorm:"type:varchar(1024)" json:"output_image_url"`
	Provider        string     `gorm:"type:varchar(32)" json:"provider"`
	ProviderJobID   string     `gorm:"type:varchar(128);index:idx_ai_gen_provider_job" json:"provider_job_id"`
	CreditsDeducted int        `gorm:"not null;default:0" json:"credits_deducted"`
	Status          int8       `gorm:"type:tinyint;default:0;index:idx_ai_gen_status_created" json:"status"`
	ErrorCode       string     `gorm:"type:varchar(64)" json:"error_code"`
	ErrorMessage    string     `gorm:"type:varchar(512)" json:"error_message"`
	ExpiredAt       *time.Time `gorm:"index" json:"expired_at"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

func (AIGeneration) TableName() string { return "bb_ai_generation" }

const (
	AIGenStatusPending   int8 = 0
	AIGenStatusRunning   int8 = 1
	AIGenStatusSucceeded int8 = 2
	AIGenStatusFailed    int8 = 3
	AIGenStatusCancelled int8 = 4
	AIGenStatusExpired   int8 = 5
)
