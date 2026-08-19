package model

import "time"

// TemplateDraft 是管理后台的图纸草稿，一个独立资源。草稿存活期间 bb_template
// 完全不变，只有「发布草稿」才替换线上内容。草稿对全部管理员可见可编辑，不做归属
// 隔离，因此并发保护靠 UpdatedAt 的乐观锁。
//
// 刻意不嵌 BaseModel：UpdatedAt 在这里既是乐观锁令牌又是列表排序键，需要自己的
// 复合索引，而且每次写入都必须显式赋值（见 dao.nowMillis 的注释）。嵌了 BaseModel
// 就会被 GORM 的 AutoUpdateTime 接管，锁令牌会在写入时被悄悄改掉。
type TemplateDraft struct {
	ID uint64 `gorm:"primaryKey;autoIncrement;index:idx_tpl_draft_updated_id,priority:2" json:"id"`

	// 关联的已发布模板；nil 表示这是一份还没上过线的新图纸草稿。
	// MySQL 与 SQLite 的唯一索引都允许重复 NULL，所以「一个模板最多挂一份草稿」由
	// 数据库保证，同时任意多份新图纸草稿可以共存。手法同
	// TemplateSubmission.ActiveWorkKey。
	TemplateID *uint64 `gorm:"uniqueIndex:uk_tpl_draft_template" json:"template_id,omitempty"`

	// 创建时必填非空。列上有唯一索引，允许空串会让第二份草稿撞索引并变成 500；
	// 改成可空则 NULL 使唯一索引失效，丢掉防重复点击的唯一保护。
	IdempotencyKey string `gorm:"type:varchar(64);not null;uniqueIndex:uk_tpl_draft_idem" json:"idempotency_key"`

	// 业务字段全部允许「空着存」——草稿的用途就是承载半成品。CategoryID 为 0 表示
	// 还没选分类，Difficulty 为 0 表示还没定难度。完整校验只在发布时刻执行。
	Title       string `gorm:"type:varchar(128);not null;default:''" json:"title"`
	Description string `gorm:"type:varchar(512);not null;default:''" json:"description"`
	CategoryID  int    `gorm:"not null;default:0" json:"category_id"`
	Tags        string `gorm:"type:varchar(512);not null;default:''" json:"tags"`
	Difficulty  int8   `gorm:"type:tinyint;not null;default:0" json:"difficulty"`

	// PreviewFileKey 是可再发布的真相来源；PreviewURL/ThumbnailURL 是它的派生缓存，
	// 存下来才能让草稿列表逐行显示缩略图而不必回查对象存储。
	PreviewFileKey string `gorm:"type:varchar(512);not null;default:''" json:"preview_file_key"`
	PreviewURL     string `gorm:"type:varchar(1024);not null;default:''" json:"preview_url"`
	ThumbnailURL   string `gorm:"type:varchar(1024);not null;default:''" json:"thumbnail_url"`

	PatternData JSONMap `gorm:"type:json" json:"pattern_data"`
	BoardSpec   string  `gorm:"type:varchar(32);not null;default:''" json:"board_spec"`
	// 保存时由 pattern 校验派生并落列，好让草稿列表显示尺寸与配色数而完全不
	// SELECT pattern_data。
	Width      int `gorm:"not null;default:0" json:"width"`
	Height     int `gorm:"not null;default:0" json:"height"`
	BeadCount  int `gorm:"not null;default:0" json:"bead_count"`
	ColorCount int `gorm:"not null;default:0" json:"color_count"`

	UpdatedByActor string `gorm:"type:varchar(64);not null;default:''" json:"updated_by_actor"`

	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	// idx_tpl_draft_updated_id 让 ORDER BY updated_at DESC, id DESC 的 offset 分页
	// 确定且免排序，同时让草稿上限的 COUNT(*) 走这条窄索引而不是扫聚簇索引——后者
	// 会碰到每一行的 pattern_data 页。不要「简化」掉这个索引。
	UpdatedAt time.Time `gorm:"not null;index:idx_tpl_draft_updated_id,priority:1" json:"updated_at"`
}

func (TemplateDraft) TableName() string { return "bb_template_draft" }
