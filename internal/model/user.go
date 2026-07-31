package model

import (
	"fmt"
	"time"
)

type BaseModel struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

type User struct {
	BaseModel
	// UserID 是对外暴露的业务用户 ID；内部关联仍使用 BaseModel.ID。
	UserID    *string `gorm:"column:user_id;type:varchar(13);uniqueIndex" json:"user_id,omitempty"`
	UUID      string  `gorm:"type:varchar(36);uniqueIndex;not null" json:"uuid"`
	Nickname  string  `gorm:"type:varchar(64)" json:"nickname"`
	AvatarURL string  `gorm:"type:varchar(512)" json:"avatar_url"`
	Phone     string  `gorm:"type:varchar(20);index" json:"phone"`
	// DeviceID 和 GuestIdentity 均只保存旧设备标识的 HMAC，用于游客账号迁移。
	DeviceID      string  `gorm:"type:varchar(64);index" json:"device_id"`
	GuestIdentity *string `gorm:"type:varchar(64);uniqueIndex" json:"-"`
	// GuestCredentialHash 为游客账号的主恢复凭证 HMAC；NULL 仅兼容存量游客数据。
	GuestCredentialHash *string `gorm:"type:varchar(64);uniqueIndex" json:"-"`
	LoginType           string  `gorm:"type:varchar(16)" json:"login_type"`
	WechatUnionID       string  `gorm:"type:varchar(64);index" json:"wechat_unionid"`
	WechatOpenIDApp     string  `gorm:"type:varchar(64)" json:"wechat_openid_app"`
	WechatOpenIDMp      string  `gorm:"type:varchar(64)" json:"wechat_openid_mp"`
	WechatOpenIDWeb     string  `gorm:"type:varchar(64)" json:"wechat_openid_web"`
	AppleID             string  `gorm:"type:varchar(128)" json:"apple_id"`
	Status              int8    `gorm:"type:tinyint;default:1" json:"status"` // 1:正常 2:禁用 3:注销
	// 最近一次登录时上报的完整设备资料。设备身份 HMAC 字段仍用于游客账号迁移。
	DeviceIP            string  `gorm:"type:varchar(45)" json:"-"`
	DeviceUserAgent     string  `gorm:"type:varchar(2048)" json:"-"`
	DeviceIDFA          string  `gorm:"type:varchar(255)" json:"-"`
	DeviceIDFAMD5       string  `gorm:"type:varchar(64)" json:"-"`
	DeviceIMEI          string  `gorm:"type:varchar(255)" json:"-"`
	DeviceIMEIMD5       string  `gorm:"type:varchar(64)" json:"-"`
	DeviceOAID          string  `gorm:"type:varchar(255)" json:"-"`
	DeviceOAIDMD5       string  `gorm:"type:varchar(64)" json:"-"`
	DeviceAndroidID     string  `gorm:"type:varchar(255)" json:"-"`
	DeviceType          int32   `json:"-"`
	DeviceBrand         string  `gorm:"type:varchar(255)" json:"-"`
	DeviceModel         string  `gorm:"type:varchar(255)" json:"-"`
	DeviceOS            int32   `json:"-"`
	DeviceOSVersion     string  `gorm:"type:varchar(64)" json:"-"`
	DeviceNetwork       int32   `json:"-"`
	DeviceOperator      int32   `json:"-"`
	DeviceWidth         int32   `json:"-"`
	DeviceHeight        int32   `json:"-"`
	DeviceOrientation   int32   `json:"-"`
	DeviceGeoLatitude   float64 `json:"-"`
	DeviceGeoLongitude  float64 `json:"-"`
	DeviceInstalledApps string  `gorm:"type:text" json:"-"`
	DeviceCAIDs         string  `gorm:"type:text" json:"-"`
	DeviceBootMark      string  `gorm:"type:varchar(255)" json:"-"`
	DeviceUpdateMark    string  `gorm:"type:varchar(255)" json:"-"`
	DeviceMAC           string  `gorm:"type:varchar(64)" json:"-"`
	DeviceAndroidIDMD5  string  `gorm:"type:varchar(64)" json:"-"`
	DeviceIPv6          string  `gorm:"type:varchar(45)" json:"-"`
	DeviceUserAge       int32   `json:"-"`
	DeviceUserGender    int32   `json:"-"`
	DeviceBirthTime     string  `gorm:"type:varchar(64)" json:"-"`
	DeviceBootTime      string  `gorm:"type:varchar(64)" json:"-"`
	DeviceUpdateTime    string  `gorm:"type:varchar(64)" json:"-"`
	DeviceIDFV          string  `gorm:"type:varchar(255)" json:"-"`
	DeviceIDFVMD5       string  `gorm:"type:varchar(64)" json:"-"`
	DeviceLanguage      int32   `json:"-"`
	DeviceTimezone      string  `gorm:"type:varchar(64)" json:"-"`
}

func (u *User) PublicUserID() string {
	if u != nil && u.UserID != nil {
		return *u.UserID
	}
	if u == nil {
		return ""
	}
	return fmt.Sprintf("%d", u.ID)
}

func (User) TableName() string { return "bb_user" }
