package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

type Work struct {
	BaseModel
	UserID           uint64  `gorm:"not null;index" json:"user_id"`
	Title            string  `gorm:"type:varchar(128)" json:"title"`
	OriginalImageURL string  `gorm:"type:varchar(512)" json:"original_image_url"`
	PatternImageURL  string  `gorm:"type:varchar(512)" json:"pattern_image_url"`
	PatternData      JSONMap `gorm:"type:json" json:"pattern_data"`
	BoardSpec        string  `gorm:"type:varchar(32)" json:"board_spec"`
	Width            int     `gorm:"type:int" json:"width"`
	Height           int     `gorm:"type:int" json:"height"`
	BeadCount        int     `gorm:"type:int" json:"bead_count"`
	ColorCount       int     `gorm:"type:int" json:"color_count"`
	SourceType       string  `gorm:"type:varchar(16)" json:"source_type"`
	SourceID         string  `gorm:"type:varchar(64)" json:"source_id"`
	Status           int8    `gorm:"type:tinyint;default:1" json:"status"` // 1:草稿 2:已完成
}

func (Work) TableName() string { return "bb_work" }

type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

type CommunityPost struct {
	BaseModel
	UserID        uint64 `gorm:"not null;index" json:"user_id"`
	WorkID        uint64 `gorm:"not null" json:"work_id"`
	Description   string `gorm:"type:text" json:"description"`
	LikeCount     int    `gorm:"type:int;default:0" json:"like_count"`
	FavoriteCount int    `gorm:"type:int;default:0" json:"favorite_count"`
	CommentCount  int    `gorm:"type:int;default:0" json:"comment_count"`
	Status        int8   `gorm:"type:tinyint;default:1" json:"status"`
}

func (CommunityPost) TableName() string { return "bb_community_post" }

type Like struct {
	BaseModel
	UserID uint64 `gorm:"not null;uniqueIndex:uk_like_user_post" json:"user_id"`
	PostID uint64 `gorm:"not null;uniqueIndex:uk_like_user_post" json:"post_id"`
}

func (Like) TableName() string { return "bb_like" }

type Favorite struct {
	BaseModel
	UserID uint64 `gorm:"not null;uniqueIndex:uk_fav_user_post" json:"user_id"`
	PostID uint64 `gorm:"not null;uniqueIndex:uk_fav_user_post" json:"post_id"`
}

func (Favorite) TableName() string { return "bb_favorite" }

type Comment struct {
	BaseModel
	UserID   uint64 `gorm:"not null" json:"user_id"`
	PostID   uint64 `gorm:"not null;index" json:"post_id"`
	ParentID uint64 `gorm:"default:0" json:"parent_id"`
	Content  string `gorm:"type:varchar(500)" json:"content"`
	Status   int8   `gorm:"type:tinyint;default:1" json:"status"`
}

func (Comment) TableName() string { return "bb_comment" }

type Follow struct {
	BaseModel
	FollowerID  uint64 `gorm:"not null;uniqueIndex:uk_follow" json:"follower_id"`
	FollowingID uint64 `gorm:"not null;uniqueIndex:uk_follow" json:"following_id"`
}

func (Follow) TableName() string { return "bb_follow" }

type Template struct {
	BaseModel
	CategoryID    int     `gorm:"not null" json:"category_id"`
	Title         string  `gorm:"type:varchar(128)" json:"title"`
	PreviewURL    string  `gorm:"type:varchar(512)" json:"preview_url"`
	ThumbnailURL  string  `gorm:"type:varchar(512)" json:"thumbnail_url"`
	Description   string  `gorm:"type:varchar(512)" json:"description"`
	PatternData   JSONMap `gorm:"type:json" json:"pattern_data"`
	BoardSpec     string  `gorm:"type:varchar(32)" json:"board_spec"`
	Tags          string  `gorm:"type:varchar(512)" json:"tags"`
	Difficulty    int8    `gorm:"type:tinyint;default:1" json:"difficulty"`
	Width         int     `gorm:"default:0" json:"width"`
	Height        int     `gorm:"default:0" json:"height"`
	ColorCount    int     `gorm:"default:0" json:"color_count"`
	IsFree        bool    `gorm:"default:true" json:"is_free"`
	CreditCost    int     `gorm:"default:0" json:"credit_cost"`
	DownloadCount int     `gorm:"default:0" json:"download_count"`
	FavoriteCount int     `gorm:"default:0" json:"favorite_count"`
	SortOrder     int     `gorm:"default:0" json:"sort_order"`
	Status        int8    `gorm:"type:tinyint;default:1" json:"status"`
}

func (Template) TableName() string { return "bb_template" }

type TemplateFavorite struct {
	BaseModel
	UserID     uint64 `gorm:"not null;uniqueIndex:uk_template_fav_user_tpl;index:idx_template_fav_user_created" json:"user_id"`
	TemplateID uint64 `gorm:"not null;uniqueIndex:uk_template_fav_user_tpl;index:idx_template_fav_template" json:"template_id"`
}

func (TemplateFavorite) TableName() string { return "bb_template_favorite" }

type TemplateCategory struct {
	ID        int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"type:varchar(64);uniqueIndex:uk_template_category_name" json:"name"`
	IconURL   string    `gorm:"type:varchar(512)" json:"icon_url"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	Status    int8      `gorm:"type:tinyint;default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (TemplateCategory) TableName() string { return "bb_template_category" }
