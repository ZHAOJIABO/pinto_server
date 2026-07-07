package test

import (
	"context"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/service/community"
)

func TestCommunityFlow(t *testing.T) {
	SetupTestDB(t)
	communityDAO := dao.NewCommunityDAO()
	communityService := community.NewService(communityDAO)

	ctx := context.Background()
	userID := uint64(1)
	workID := uint64(100)

	// Publish
	postID, err := communityService.PublishWork(ctx, userID, workID, "我的第一个拼豆作品!")
	if err != nil {
		t.Fatalf("PublishWork failed: %v", err)
	}
	if postID == 0 {
		t.Error("expected post ID > 0")
	}

	// Get feed
	posts, total, err := communityService.GetFeed(ctx, "latest", 1, 10)
	if err != nil {
		t.Fatalf("GetFeed failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 post in feed, got %d", total)
	}
	if posts[0].Description != "我的第一个拼豆作品!" {
		t.Errorf("unexpected description: %s", posts[0].Description)
	}

	// Like
	likerID := uint64(2)
	err = communityService.LikePost(ctx, likerID, postID)
	if err != nil {
		t.Fatalf("LikePost failed: %v", err)
	}

	post, _ := communityService.GetPostDetail(ctx, postID)
	if post.LikeCount != 1 {
		t.Errorf("expected like_count=1, got %d", post.LikeCount)
	}

	// Unlike
	err = communityService.UnlikePost(ctx, likerID, postID)
	if err != nil {
		t.Fatalf("UnlikePost failed: %v", err)
	}

	post, _ = communityService.GetPostDetail(ctx, postID)
	if post.LikeCount != 0 {
		t.Errorf("expected like_count=0 after unlike, got %d", post.LikeCount)
	}

	// Favorite
	err = communityService.FavoritePost(ctx, likerID, postID)
	if err != nil {
		t.Fatalf("FavoritePost failed: %v", err)
	}

	post, _ = communityService.GetPostDetail(ctx, postID)
	if post.FavoriteCount != 1 {
		t.Errorf("expected favorite_count=1, got %d", post.FavoriteCount)
	}

	// Comment
	commentID, err := communityService.AddComment(ctx, likerID, postID, "好漂亮!", 0)
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if commentID == 0 {
		t.Error("expected comment ID > 0")
	}

	post, _ = communityService.GetPostDetail(ctx, postID)
	if post.CommentCount != 1 {
		t.Errorf("expected comment_count=1, got %d", post.CommentCount)
	}

	// List comments
	comments, commentTotal, err := communityService.ListComments(ctx, postID, 1, 10)
	if err != nil {
		t.Fatalf("ListComments failed: %v", err)
	}
	if commentTotal != 1 {
		t.Errorf("expected 1 comment, got %d", commentTotal)
	}
	if comments[0].Content != "好漂亮!" {
		t.Errorf("unexpected comment content: %s", comments[0].Content)
	}

	// Follow
	err = communityService.FollowUser(ctx, likerID, userID)
	if err != nil {
		t.Fatalf("FollowUser failed: %v", err)
	}

	// Unfollow
	err = communityService.UnfollowUser(ctx, likerID, userID)
	if err != nil {
		t.Fatalf("UnfollowUser failed: %v", err)
	}

	t.Logf("Community flow success: post_id=%d, comment_id=%d", postID, commentID)
}

func TestCommunityFeedTypes(t *testing.T) {
	SetupTestDB(t)
	communityDAO := dao.NewCommunityDAO()
	communityService := community.NewService(communityDAO)

	ctx := context.Background()

	// Create multiple posts
	communityService.PublishWork(ctx, 1, 100, "post 1")
	communityService.PublishWork(ctx, 2, 101, "post 2")
	postID3, _ := communityService.PublishWork(ctx, 3, 102, "post 3")

	// Like post 3 multiple times to make it "hot"
	communityService.LikePost(ctx, 10, postID3)
	communityService.LikePost(ctx, 11, postID3)
	communityService.LikePost(ctx, 12, postID3)

	// Hot feed - post 3 should be first
	posts, _, _ := communityService.GetFeed(ctx, "hot", 1, 10)
	if len(posts) != 3 {
		t.Errorf("expected 3 posts, got %d", len(posts))
	}
	if posts[0].ID != postID3 {
		t.Errorf("expected post 3 (most liked) first in hot feed, got post %d", posts[0].ID)
	}

	// Latest feed - post 3 should also be first (most recent)
	posts, _, _ = communityService.GetFeed(ctx, "latest", 1, 10)
	if posts[0].ID != postID3 {
		t.Errorf("expected post 3 (most recent) first in latest feed, got post %d", posts[0].ID)
	}

	t.Log("Feed types success")
}
