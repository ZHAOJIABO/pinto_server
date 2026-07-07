package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/community"
)

type CommunityHandler struct {
	pb.UnimplementedCommunityServiceServer
	communityService *community.Service
}

func NewCommunityHandler(communityService *community.Service) *CommunityHandler {
	return &CommunityHandler{communityService: communityService}
}

func (h *CommunityHandler) PublishWork(ctx context.Context, req *pb.PublishWorkRequest) (*pb.PublishWorkResponse, error) {
	userID := middleware.GetUserID(ctx)
	workID, _ := strconv.ParseUint(req.WorkId, 10, 64)
	postID, err := h.communityService.PublishWork(ctx, userID, workID, req.Description)
	if err != nil {
		return &pb.PublishWorkResponse{Header: errHeader(err)}, nil
	}
	return &pb.PublishWorkResponse{Header: okHeader(), PostId: fmt.Sprintf("%d", postID)}, nil
}

func (h *CommunityHandler) UnpublishWork(ctx context.Context, req *pb.UnpublishWorkRequest) (*pb.UnpublishWorkResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	if err := h.communityService.UnpublishWork(ctx, userID, postID); err != nil {
		return &pb.UnpublishWorkResponse{Header: errHeader(err)}, nil
	}
	return &pb.UnpublishWorkResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) GetFeed(ctx context.Context, req *pb.GetFeedRequest) (*pb.GetFeedResponse, error) {
	page, pageSize := getPage(req.Page)
	posts, total, err := h.communityService.GetFeed(ctx, req.FeedType, page, pageSize)
	if err != nil {
		return &pb.GetFeedResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.PostItem
	for _, p := range posts {
		items = append(items, &pb.PostItem{
			PostId:      fmt.Sprintf("%d", p.ID),
			WorkId:      fmt.Sprintf("%d", p.WorkID),
			Description: p.Description,
			LikeCount:   int32(p.LikeCount),
			FavoriteCount: int32(p.FavoriteCount),
			CommentCount:  int32(p.CommentCount),
			CreatedAt:   p.CreatedAt.Unix(),
		})
	}
	return &pb.GetFeedResponse{
		Header: okHeader(),
		Posts:  items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}

func (h *CommunityHandler) GetPostDetail(ctx context.Context, req *pb.GetPostDetailRequest) (*pb.GetPostDetailResponse, error) {
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	post, err := h.communityService.GetPostDetail(ctx, postID)
	if err != nil {
		return &pb.GetPostDetailResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetPostDetailResponse{
		Header: okHeader(),
		Post: &pb.PostItem{
			PostId:        fmt.Sprintf("%d", post.ID),
			WorkId:        fmt.Sprintf("%d", post.WorkID),
			Description:   post.Description,
			LikeCount:     int32(post.LikeCount),
			FavoriteCount: int32(post.FavoriteCount),
			CommentCount:  int32(post.CommentCount),
			CreatedAt:     post.CreatedAt.Unix(),
		},
	}, nil
}

func (h *CommunityHandler) LikePost(ctx context.Context, req *pb.LikePostRequest) (*pb.LikePostResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	if err := h.communityService.LikePost(ctx, userID, postID); err != nil {
		return &pb.LikePostResponse{Header: errHeader(err)}, nil
	}
	return &pb.LikePostResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) UnlikePost(ctx context.Context, req *pb.UnlikePostRequest) (*pb.UnlikePostResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	if err := h.communityService.UnlikePost(ctx, userID, postID); err != nil {
		return &pb.UnlikePostResponse{Header: errHeader(err)}, nil
	}
	return &pb.UnlikePostResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) FavoritePost(ctx context.Context, req *pb.FavoritePostRequest) (*pb.FavoritePostResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	if err := h.communityService.FavoritePost(ctx, userID, postID); err != nil {
		return &pb.FavoritePostResponse{Header: errHeader(err)}, nil
	}
	return &pb.FavoritePostResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) UnfavoritePost(ctx context.Context, req *pb.UnfavoritePostRequest) (*pb.UnfavoritePostResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	if err := h.communityService.UnfavoritePost(ctx, userID, postID); err != nil {
		return &pb.UnfavoritePostResponse{Header: errHeader(err)}, nil
	}
	return &pb.UnfavoritePostResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) AddComment(ctx context.Context, req *pb.AddCommentRequest) (*pb.AddCommentResponse, error) {
	userID := middleware.GetUserID(ctx)
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	parentID, _ := strconv.ParseUint(req.ParentId, 10, 64)
	commentID, err := h.communityService.AddComment(ctx, userID, postID, req.Content, parentID)
	if err != nil {
		return &pb.AddCommentResponse{Header: errHeader(err)}, nil
	}
	return &pb.AddCommentResponse{Header: okHeader(), CommentId: fmt.Sprintf("%d", commentID)}, nil
}

func (h *CommunityHandler) ListComments(ctx context.Context, req *pb.ListCommentsRequest) (*pb.ListCommentsResponse, error) {
	postID, _ := strconv.ParseUint(req.PostId, 10, 64)
	page, pageSize := getPage(req.Page)
	comments, total, err := h.communityService.ListComments(ctx, postID, page, pageSize)
	if err != nil {
		return &pb.ListCommentsResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.CommentItem
	for _, c := range comments {
		items = append(items, &pb.CommentItem{
			CommentId: fmt.Sprintf("%d", c.ID),
			Content:   c.Content,
			ParentId:  fmt.Sprintf("%d", c.ParentID),
			CreatedAt: c.CreatedAt.Unix(),
		})
	}
	return &pb.ListCommentsResponse{
		Header:   okHeader(),
		Comments: items,
		Page:     pageResp(total, page, pageSize),
	}, nil
}

func (h *CommunityHandler) FollowUser(ctx context.Context, req *pb.FollowUserRequest) (*pb.FollowUserResponse, error) {
	userID := middleware.GetUserID(ctx)
	targetID, _ := strconv.ParseUint(req.UserId, 10, 64)
	if err := h.communityService.FollowUser(ctx, userID, targetID); err != nil {
		return &pb.FollowUserResponse{Header: errHeader(err)}, nil
	}
	return &pb.FollowUserResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) UnfollowUser(ctx context.Context, req *pb.UnfollowUserRequest) (*pb.UnfollowUserResponse, error) {
	userID := middleware.GetUserID(ctx)
	targetID, _ := strconv.ParseUint(req.UserId, 10, 64)
	if err := h.communityService.UnfollowUser(ctx, userID, targetID); err != nil {
		return &pb.UnfollowUserResponse{Header: errHeader(err)}, nil
	}
	return &pb.UnfollowUserResponse{Header: okHeader()}, nil
}

func (h *CommunityHandler) ReportContent(ctx context.Context, req *pb.ReportContentRequest) (*pb.ReportContentResponse, error) {
	// TODO: save report record
	return &pb.ReportContentResponse{Header: okHeader()}, nil
}
