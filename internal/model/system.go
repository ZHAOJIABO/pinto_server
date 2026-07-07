package model

type BeadColor struct {
	ID       int    `gorm:"primaryKey;autoIncrement" json:"id"`
	Brand    string `gorm:"type:varchar(32);not null;uniqueIndex:uk_brand_code" json:"brand"`
	Code     string `gorm:"type:varchar(16);not null;uniqueIndex:uk_brand_code" json:"code"`
	Name     string `gorm:"type:varchar(64)" json:"name"`
	Hex      string `gorm:"type:varchar(7);not null" json:"hex"`
	R        uint8  `json:"r"`
	G        uint8  `json:"g"`
	B        uint8  `json:"b"`
	Category string `gorm:"type:varchar(32)" json:"category"`
	Status   int8   `gorm:"type:tinyint;default:1" json:"status"`
}

func (BeadColor) TableName() string { return "bb_bead_color" }

type BoardSpec struct {
	ID       int     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name     string  `gorm:"type:varchar(64)" json:"name"`
	Shape    string  `gorm:"type:varchar(16);default:square" json:"shape"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	BeadSize float64 `gorm:"type:decimal(3,1)" json:"bead_size"`
	Status   int8    `gorm:"type:tinyint;default:1" json:"status"`
}

func (BoardSpec) TableName() string { return "bb_board_spec" }

type Config struct {
	ID          int    `gorm:"primaryKey;autoIncrement" json:"id"`
	ConfigKey   string `gorm:"type:varchar(128);uniqueIndex;not null" json:"config_key"`
	ConfigValue string `gorm:"type:text" json:"config_value"`
	Description string `gorm:"type:varchar(256)" json:"description"`
}

func (Config) TableName() string { return "bb_config" }

type Feedback struct {
	BaseModel
	UserID     uint64 `gorm:"not null" json:"user_id"`
	Content    string `gorm:"type:text" json:"content"`
	Contact    string `gorm:"type:varchar(128)" json:"contact"`
	Platform   string `gorm:"type:varchar(16)" json:"platform"`
	AppVersion string `gorm:"type:varchar(16)" json:"app_version"`
	Status     int8   `gorm:"type:tinyint;default:0" json:"status"` // 0:待处理 1:已处理
}

func (Feedback) TableName() string { return "bb_feedback" }
