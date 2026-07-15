package model

import "time"

type TemplatePublishRecord struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	IdempotencyKey  string    `gorm:"type:varchar(64);uniqueIndex" json:"idempotency_key"`
	TemplateID      uint64    `gorm:"not null;index" json:"template_id"`
	DraftRevisionID uint64    `gorm:"not null" json:"draft_revision_id"`
	Status          string    `gorm:"type:varchar(32);not null;default:'published'" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (TemplatePublishRecord) TableName() string { return "bb_template_publish_record" }
