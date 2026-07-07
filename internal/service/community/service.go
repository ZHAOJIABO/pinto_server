package community

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	communityDAO *dao.CommunityDAO
}

func NewService(communityDAO *dao.CommunityDAO) *Service {
	return &Service{communityDAO: communityDAO}
}

func (s *Service) PublishWork(ctx context.Context, userID, workID uint64, description string) (uint64, error) {
	post := &model.CommunityPost{
		UserID:      userID,
		WorkID:      workID,
		Description: description,
		Status:      1,
	}
	if err := s.communityDAO.CreatePost(ctx, post); err != nil {
		return 0, err
	}
	return post.ID, nil
}

func (s *Service) UnpublishWork(ctx context.Context, userID, postID uint64) error {
	return s.communityDAO.DeletePost(ctx, postID, userID)
}

func (s *Service) GetFeed(ctx context.Context, feedType string, page, pageSize int) ([]*model.CommunityPost, int64, error) {
	offset := (page - 1) * pageSize
	orderBy := "created_at DESC"
	if feedType == "hot" {
		orderBy = "like_count DESC, created_at DESC"
	}
	return s.communityDAO.ListPosts(ctx, orderBy, offset, pageSize)
}

func (s *Service) GetPostDetail(ctx context.Context, postID uint64) (*model.CommunityPost, error) {
	return s.communityDAO.GetPostByID(ctx, postID)
}

func (s *Service) LikePost(ctx context.Context, userID, postID uint64) error {
	like := &model.Like{UserID: userID, PostID: postID}
	if err := s.communityDAO.CreateLike(ctx, like); err != nil {
		return err
	}
	return s.communityDAO.IncrementCounter(ctx, postID, "like_count", 1)
}

func (s *Service) UnlikePost(ctx context.Context, userID, postID uint64) error {
	if err := s.communityDAO.DeleteLike(ctx, userID, postID); err != nil {
		return err
	}
	return s.communityDAO.IncrementCounter(ctx, postID, "like_count", -1)
}

func (s *Service) FavoritePost(ctx context.Context, userID, postID uint64) error {
	fav := &model.Favorite{UserID: userID, PostID: postID}
	if err := s.communityDAO.CreateFavorite(ctx, fav); err != nil {
		return err
	}
	return s.communityDAO.IncrementCounter(ctx, postID, "favorite_count", 1)
}

func (s *Service) UnfavoritePost(ctx context.Context, userID, postID uint64) error {
	if err := s.communityDAO.DeleteFavorite(ctx, userID, postID); err != nil {
		return err
	}
	return s.communityDAO.IncrementCounter(ctx, postID, "favorite_count", -1)
}

func (s *Service) AddComment(ctx context.Context, userID, postID uint64, content string, parentID uint64) (uint64, error) {
	comment := &model.Comment{
		UserID:   userID,
		PostID:   postID,
		Content:  content,
		ParentID: parentID,
		Status:   1,
	}
	if err := s.communityDAO.CreateComment(ctx, comment); err != nil {
		return 0, err
	}
	s.communityDAO.IncrementCounter(ctx, postID, "comment_count", 1)
	return comment.ID, nil
}

func (s *Service) ListComments(ctx context.Context, postID uint64, page, pageSize int) ([]*model.Comment, int64, error) {
	offset := (page - 1) * pageSize
	return s.communityDAO.ListComments(ctx, postID, offset, pageSize)
}

func (s *Service) FollowUser(ctx context.Context, followerID, followingID uint64) error {
	follow := &model.Follow{FollowerID: followerID, FollowingID: followingID}
	return s.communityDAO.CreateFollow(ctx, follow)
}

func (s *Service) UnfollowUser(ctx context.Context, followerID, followingID uint64) error {
	return s.communityDAO.DeleteFollow(ctx, followerID, followingID)
}
