package model

import "time"

type Order struct {
	BaseModel
	OrderNo       string  `gorm:"type:varchar(64);uniqueIndex;not null" json:"order_no"`
	UserID        uint64  `gorm:"not null;index" json:"user_id"`
	ProductID     int     `gorm:"not null" json:"product_id"`
	Amount        int     `gorm:"not null" json:"amount"` // 金额，单位分
	Currency      string  `gorm:"type:varchar(8);default:CNY" json:"currency"`
	PaymentMethod string  `gorm:"type:varchar(32)" json:"payment_method"`
	Platform      string  `gorm:"type:varchar(16)" json:"platform"`
	Status        int8    `gorm:"type:tinyint;default:0" json:"status"` // 0:待支付 1:已支付 2:已退款 3:已关闭
	PaidAt        *time.Time `json:"paid_at"`
	TransactionID string  `gorm:"type:varchar(128)" json:"transaction_id"`
}

func (Order) TableName() string { return "bb_order" }

type Product struct {
	ID             int    `gorm:"primaryKey;autoIncrement" json:"id"`
	SKU            string `gorm:"type:varchar(64);uniqueIndex;not null" json:"sku"`
	Name           string `gorm:"type:varchar(128)" json:"name"`
	Description    string `gorm:"type:varchar(512)" json:"description"`
	Price          int    `gorm:"not null" json:"price"` // 单位分
	Currency       string `gorm:"type:varchar(8);default:CNY" json:"currency"`
	DurationDays   int    `gorm:"type:int" json:"duration_days"`
	Platform       string `gorm:"type:varchar(16)" json:"platform"`
	AppleProductID string `gorm:"type:varchar(128)" json:"apple_product_id"`
	Status         int8   `gorm:"type:tinyint;default:1" json:"status"`
	SortOrder      int    `gorm:"default:0" json:"sort_order"`
}

func (Product) TableName() string { return "bb_product" }

type Subscription struct {
	BaseModel
	UserID    uint64    `gorm:"not null;index" json:"user_id"`
	ProductID int       `gorm:"not null" json:"product_id"`
	OrderID   uint64    `gorm:"not null" json:"order_id"`
	StartAt   time.Time `gorm:"not null" json:"start_at"`
	ExpireAt  time.Time `gorm:"not null;index" json:"expire_at"`
	Status    int8      `gorm:"type:tinyint;default:1" json:"status"` // 1:生效中 2:已过期 3:已取消
}

func (Subscription) TableName() string { return "bb_subscription" }

type CreditTransaction struct {
	BaseModel
	UserID      uint64 `gorm:"not null;index" json:"user_id"`
	Amount      int    `gorm:"not null" json:"amount"` // 正数增加，负数消耗
	Balance     int    `gorm:"not null" json:"balance"`
	Type        string `gorm:"type:varchar(32)" json:"type"`
	RefType     string `gorm:"type:varchar(32)" json:"ref_type"`
	RefID       string `gorm:"type:varchar(64)" json:"ref_id"`
	RequestID   string `gorm:"type:varchar(64);index" json:"request_id"`
	Description string `gorm:"type:varchar(256)" json:"description"`
}

func (CreditTransaction) TableName() string { return "bb_credit_transaction" }

type Invite struct {
	BaseModel
	InviterID    uint64 `gorm:"not null;index" json:"inviter_id"`
	InviteeID    uint64 `gorm:"not null" json:"invitee_id"`
	InviteCode   string `gorm:"type:varchar(16);not null;index" json:"invite_code"`
	RewardGranted bool  `gorm:"default:false" json:"reward_granted"`
}

func (Invite) TableName() string { return "bb_invite" }
