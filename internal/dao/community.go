package dao

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/gorm"
)

type CommunityDAO struct{}

func NewCommunityDAO() *CommunityDAO { return &CommunityDAO{} }

func (d *CommunityDAO) DB(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx)
}

func (d *CommunityDAO) CreatePost(ctx context.Context, post *model.CommunityPost) error {
	return d.DB(ctx).Create(post).Error
}

func (d *CommunityDAO) GetPostByID(ctx context.Context, id uint64) (*model.CommunityPost, error) {
	var post model.CommunityPost
	err := d.DB(ctx).Where("id = ? AND status = 1", id).First(&post).Error
	return &post, err
}

func (d *CommunityDAO) ListPosts(ctx context.Context, orderBy string, offset, limit int) ([]*model.CommunityPost, int64, error) {
	var posts []*model.CommunityPost
	var total int64
	query := d.DB(ctx).Where("status = 1")
	query.Model(&model.CommunityPost{}).Count(&total)
	err := query.Order(orderBy).Offset(offset).Limit(limit).Find(&posts).Error
	return posts, total, err
}

func (d *CommunityDAO) DeletePost(ctx context.Context, id uint64, userID uint64) error {
	return d.DB(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.CommunityPost{}).Error
}

func (d *CommunityDAO) IncrementCounter(ctx context.Context, postID uint64, field string, delta int) error {
	return d.DB(ctx).Model(&model.CommunityPost{}).Where("id = ?", postID).
		Update(field, gorm.Expr(field+" + ?", delta)).Error
}

func (d *CommunityDAO) CreateLike(ctx context.Context, like *model.Like) error {
	return d.DB(ctx).Create(like).Error
}

func (d *CommunityDAO) DeleteLike(ctx context.Context, userID, postID uint64) error {
	return d.DB(ctx).Where("user_id = ? AND post_id = ?", userID, postID).Delete(&model.Like{}).Error
}

func (d *CommunityDAO) IsLiked(ctx context.Context, userID, postID uint64) (bool, error) {
	var count int64
	err := d.DB(ctx).Model(&model.Like{}).Where("user_id = ? AND post_id = ?", userID, postID).Count(&count).Error
	return count > 0, err
}

func (d *CommunityDAO) CreateFavorite(ctx context.Context, fav *model.Favorite) error {
	return d.DB(ctx).Create(fav).Error
}

func (d *CommunityDAO) DeleteFavorite(ctx context.Context, userID, postID uint64) error {
	return d.DB(ctx).Where("user_id = ? AND post_id = ?", userID, postID).Delete(&model.Favorite{}).Error
}

func (d *CommunityDAO) CreateComment(ctx context.Context, comment *model.Comment) error {
	return d.DB(ctx).Create(comment).Error
}

func (d *CommunityDAO) ListComments(ctx context.Context, postID uint64, offset, limit int) ([]*model.Comment, int64, error) {
	var comments []*model.Comment
	var total int64
	query := d.DB(ctx).Where("post_id = ? AND status = 1", postID)
	query.Model(&model.Comment{}).Count(&total)
	err := query.Order("created_at ASC").Offset(offset).Limit(limit).Find(&comments).Error
	return comments, total, err
}

func (d *CommunityDAO) CreateFollow(ctx context.Context, follow *model.Follow) error {
	return d.DB(ctx).Create(follow).Error
}

func (d *CommunityDAO) DeleteFollow(ctx context.Context, followerID, followingID uint64) error {
	return d.DB(ctx).Where("follower_id = ? AND following_id = ?", followerID, followingID).Delete(&model.Follow{}).Error
}
