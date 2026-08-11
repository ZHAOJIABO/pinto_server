package model

import "time"

// 投稿审核状态。与 bb_template.status(0 下架 / 1 上架) 无关，两者独立演进。
const (
	TemplateSubmissionStatusPending  int8 = 0
	TemplateSubmissionStatusApproved int8 = 1
	TemplateSubmissionStatusRejected int8 = 2
)

// TemplateSubmission 是用户把自己的作品投稿为官方图纸的一次申请。
// 图纸内容在投稿时被快照下来，此后用户改图或删作品都不影响审核与发布结果。
type TemplateSubmission struct {
	BaseModel
	// 单列索引在 InnoDB 中物理上就是 (user_id, id)，「我的投稿」游标分页据此免排序。
	UserID uint64 `gorm:"not null;index;uniqueIndex:uk_tpl_sub_user_client_req,priority:1;uniqueIndex:uk_tpl_sub_user_active_work,priority:1" json:"user_id"`
	// 来源作品 ID，仅用于溯源。bb_work 是硬删除，此 ID 可能悬空，读取路径不得 join。
	WorkID uint64 `gorm:"not null;index" json:"work_id"`
	// 活跃占位键：待审核/已通过时为 work_id 的十进制字符串，驳回时置 NULL。
	// MySQL 与 SQLite 的唯一索引都允许重复 NULL，所以「同一作品只能有一条未驳回投稿」
	// 由数据库保证，而驳回后仍可重新投稿。
	ActiveWorkKey *string `gorm:"type:varchar(20);uniqueIndex:uk_tpl_sub_user_active_work,priority:2" json:"active_work_key,omitempty"`

	Title       string `gorm:"type:varchar(128);not null" json:"title"`
	Description string `gorm:"type:varchar(512)" json:"description"`

	// 投稿时刻的作品快照，审核与发布只读这里。
	PatternData JSONMap `gorm:"type:json" json:"pattern_data"`
	BoardSpec   string  `gorm:"type:varchar(32)" json:"board_spec"`
	Width       int     `gorm:"default:0" json:"width"`
	Height      int     `gorm:"default:0" json:"height"`
	BeadCount   int     `gorm:"default:0" json:"bead_count"`
	ColorCount  int     `gorm:"default:0" json:"color_count"`
	// 候选预览图，仅在作品图确属本站对象存储时才有值，否则留空等管理员上传。
	PreviewURL       string `gorm:"type:varchar(1024)" json:"preview_url"`
	ThumbnailURL     string `gorm:"type:varchar(1024)" json:"thumbnail_url"`
	OriginalImageURL string `gorm:"type:varchar(1024)" json:"original_image_url"`

	Status        int8       `gorm:"type:tinyint;not null;default:0;index:idx_tpl_sub_status_id,priority:1" json:"status"`
	ReviewerActor string     `gorm:"type:varchar(64)" json:"reviewer_actor"`
	ReviewReason  string     `gorm:"type:varchar(512)" json:"review_reason"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	// 审核通过后生成的官方图纸 ID；0 表示尚未发布。
	TemplateID uint64 `gorm:"not null;default:0;index" json:"template_id"`

	ClientRequestID string `gorm:"type:varchar(64);not null;uniqueIndex:uk_tpl_sub_user_client_req,priority:2" json:"client_request_id"`
}

func (TemplateSubmission) TableName() string { return "bb_template_submission" }
