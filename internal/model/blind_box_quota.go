package model

// BlindBoxDailyQuota 记录用户在某个业务日期已经用掉的盲盒次数。
//
// 不用 COUNT(bb_blind_box_record) 代替这张表，有三个原因：并发的两个请求会同时读到
// "今天 0 次" 然后双抽，而 (user_id, draw_date) 唯一索引配合条件 UPDATE 能原子地挡住；
// 开盒记录只是历史，写失败不该等于白送一次机会；将来加了签到赠送的机会券，还要能分清
// 一次抽奖花掉的是免费次数还是券。
//
// DrawDate 存 varchar(10) 的 "2006-01-02"，而不是 DATE 或时间类型：业务日期由服务端按
// 固定时区算好再写入，字符串不经过驱动的时区转换（MySQL 的 DSN 是 loc=Local），MySQL
// 与测试用的 SQLite 行为一致。
type BlindBoxDailyQuota struct {
	BaseModel
	UserID    uint64 `gorm:"not null;uniqueIndex:uk_bbq_user_date,priority:1" json:"user_id"`
	DrawDate  string `gorm:"type:varchar(10);not null;uniqueIndex:uk_bbq_user_date,priority:2" json:"draw_date"`
	UsedCount int    `gorm:"not null;default:0" json:"used_count"`
}

func (BlindBoxDailyQuota) TableName() string { return "bb_blind_box_daily_quota" }
