package model

// FinishedProduct 是用户拍照上传后确认保存的一件成品。
type FinishedProduct struct {
	BaseModel
	// 单列索引在 InnoDB 中物理上就是 (user_id, id)，游标分页据此免排序。
	UserID       uint64 `gorm:"not null;index;uniqueIndex:uk_fp_user_client_req,priority:1;uniqueIndex:uk_fp_user_media,priority:1" json:"user_id"`
	MediaFileKey string `gorm:"type:varchar(512);not null;uniqueIndex:uk_fp_user_media,priority:2" json:"media_file_key"`
	ImageURL     string `gorm:"type:varchar(1024);not null" json:"image_url"`
	// 服务端生成的缩略图。为空表示生成失败，客户端降级用原图。
	ThumbnailURL    string `gorm:"type:varchar(1024)" json:"thumbnail_url"`
	ClientRequestID string `gorm:"type:varchar(64);not null;uniqueIndex:uk_fp_user_client_req,priority:2" json:"client_request_id"`
}

func (FinishedProduct) TableName() string { return "bb_finished_product" }
