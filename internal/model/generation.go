package model

import "time"

type Generation struct {
	BaseModel
	UserID          uint64     `gorm:"not null;uniqueIndex:uk_generation_user_request;index" json:"user_id"`
	GenerationID    string     `gorm:"type:varchar(36);uniqueIndex;not null" json:"generation_id"`
	ClientRequestID string     `gorm:"type:varchar(64);not null;uniqueIndex:uk_generation_user_request" json:"client_request_id"`
	BoardSpec       string     `gorm:"type:varchar(32)" json:"board_spec"`
	SourceType      string     `gorm:"type:varchar(16)" json:"source_type"`
	SourceID        string     `gorm:"type:varchar(64)" json:"source_id"`
	CreditsDeducted int        `gorm:"not null;default:0" json:"credits_deducted"`
	Status          int8       `gorm:"type:tinyint;default:0;index" json:"status"` // 0:pending 1:completed 2:cancelled 3:expired
	WorkID          uint64     `gorm:"default:0" json:"work_id"`
	CancelReason    string     `gorm:"type:varchar(255)" json:"cancel_reason"`
	ExpiredAt       time.Time  `gorm:"not null;index" json:"expired_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

func (Generation) TableName() string { return "bb_generation" }

const (
	GenerationStatusPending   int8 = 0
	GenerationStatusCompleted int8 = 1
	GenerationStatusCancelled int8 = 2
	GenerationStatusExpired   int8 = 3
)
