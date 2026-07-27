package model

type BlindBoxRecord struct {
	BaseModel
	UserID     uint64 `gorm:"not null;index:idx_bbr_user_created,priority:1" json:"user_id"`
	TemplateID uint64 `gorm:"not null;index" json:"template_id"`
}

func (BlindBoxRecord) TableName() string { return "bb_blind_box_record" }
