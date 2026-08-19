package model

import "time"

// TemplateRevision 是覆盖一个已发布模板之前留下的只写快照。
//
// 没有读接口也没有回滚接口：唯一用途是误覆盖之后由人工从数据库里把旧图纸找回来。
// 将来如果要加读接口，查询必须用显式列投影排除 pattern_data，原因见
// dao.templateListColumns 的注释（几 MB 的 JSON 一排序就撞 MySQL error 1038）。
type TemplateRevision struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// 被覆盖的模板，以及触发这次覆盖的草稿。草稿发布后即删除，所以 DraftID 会悬空，
	// 只作溯源用，读取路径不得 join。
	TemplateID uint64 `gorm:"not null;index:idx_tpl_rev_template_created,priority:1" json:"template_id"`
	DraftID    uint64 `gorm:"not null;default:0" json:"draft_id"`

	// 以下镜像覆盖前 bb_template 的业务列。
	CategoryID   int     `gorm:"not null;default:0" json:"category_id"`
	Title        string  `gorm:"type:varchar(128);not null;default:''" json:"title"`
	Description  string  `gorm:"type:varchar(512);not null;default:''" json:"description"`
	PreviewURL   string  `gorm:"type:varchar(1024);not null;default:''" json:"preview_url"`
	ThumbnailURL string  `gorm:"type:varchar(1024);not null;default:''" json:"thumbnail_url"`
	PatternData  JSONMap `gorm:"type:json" json:"pattern_data"`
	BoardSpec    string  `gorm:"type:varchar(32);not null;default:''" json:"board_spec"`
	Tags         string  `gorm:"type:varchar(512);not null;default:''" json:"tags"`
	Difficulty   int8    `gorm:"type:tinyint;not null;default:0" json:"difficulty"`
	Width        int     `gorm:"not null;default:0" json:"width"`
	Height       int     `gorm:"not null;default:0" json:"height"`
	ColorCount   int     `gorm:"not null;default:0" json:"color_count"`

	ReplacedByActor string    `gorm:"type:varchar(64);not null;default:''" json:"replaced_by_actor"`
	CreatedAt       time.Time `gorm:"not null;index:idx_tpl_rev_template_created,priority:2" json:"created_at"`
}

func (TemplateRevision) TableName() string { return "bb_template_revision" }
