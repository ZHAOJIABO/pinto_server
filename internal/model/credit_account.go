package model

type CreditAccount struct {
	BaseModel
	UserID  uint64 `gorm:"not null;uniqueIndex" json:"user_id"`
	Balance int    `gorm:"not null;default:0" json:"balance"`
}

func (CreditAccount) TableName() string { return "bb_credit_account" }
