package model

import "time"

type MediaAsset struct {
	BaseModel
	UserID      uint64     `gorm:"not null;index" json:"user_id"`
	FileKey     string     `gorm:"type:varchar(512);not null;uniqueIndex" json:"file_key"`
	FileURL     string     `gorm:"type:varchar(1024)" json:"file_url"`
	Purpose     string     `gorm:"type:varchar(32);not null;index" json:"purpose"`
	ContentType string     `gorm:"type:varchar(128)" json:"content_type"`
	FileSize    int64      `json:"file_size"`
	Status      int8       `gorm:"type:tinyint;default:0" json:"status"` // 0:pending 1:uploaded 2:failed
	UploadedAt  *time.Time `json:"uploaded_at"`
}

func (MediaAsset) TableName() string { return "bb_media_asset" }

const (
	MediaStatusPending  int8 = 0
	MediaStatusUploaded int8 = 1
	MediaStatusFailed   int8 = 2
)
