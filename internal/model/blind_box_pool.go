package model

// BlindBoxPoolItem 是盲盒奖池的一个条目。奖池只有一个（单池语义），所以
// TemplateID 上的唯一索引既表达业务约束，也是并发重复入池的唯一防线——
// 服务层的"先查再插"挡不住并发。
type BlindBoxPoolItem struct {
	BaseModel
	TemplateID uint64 `gorm:"not null;uniqueIndex:uk_bbp_template" json:"template_id"`
	Weight     int    `gorm:"not null;default:1" json:"weight"`
	SortOrder  int    `gorm:"default:0" json:"sort_order"`
	Status     int8   `gorm:"type:tinyint;default:1;index:idx_bbp_status" json:"status"`
}

func (BlindBoxPoolItem) TableName() string { return "bb_blind_box_pool" }
