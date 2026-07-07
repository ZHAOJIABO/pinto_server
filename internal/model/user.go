package model

import "time"

type BaseModel struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

type User struct {
	BaseModel
	UUID            string `gorm:"type:varchar(36);uniqueIndex;not null" json:"uuid"`
	Nickname        string `gorm:"type:varchar(64)" json:"nickname"`
	AvatarURL       string `gorm:"type:varchar(512)" json:"avatar_url"`
	Phone           string `gorm:"type:varchar(20);index" json:"phone"`
	DeviceID        string `gorm:"type:varchar(64);index" json:"device_id"`
	LoginType       string `gorm:"type:varchar(16)" json:"login_type"`
	WechatUnionID   string `gorm:"type:varchar(64);index" json:"wechat_unionid"`
	WechatOpenIDApp string `gorm:"type:varchar(64)" json:"wechat_openid_app"`
	WechatOpenIDMp  string `gorm:"type:varchar(64)" json:"wechat_openid_mp"`
	WechatOpenIDWeb string `gorm:"type:varchar(64)" json:"wechat_openid_web"`
	AppleID         string `gorm:"type:varchar(128)" json:"apple_id"`
	Status          int8   `gorm:"type:tinyint;default:1" json:"status"` // 1:正常 2:禁用 3:注销
}

func (User) TableName() string { return "bb_user" }
