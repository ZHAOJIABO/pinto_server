package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/invite"
)

type InviteHandler struct {
	pb.UnimplementedInviteServiceServer
	inviteService *invite.Service
}

func NewInviteHandler(inviteService *invite.Service) *InviteHandler {
	return &InviteHandler{inviteService: inviteService}
}

func (h *InviteHandler) GetInviteCode(ctx context.Context, req *pb.GetInviteCodeRequest) (*pb.GetInviteCodeResponse, error) {
	userID := middleware.GetUserID(ctx)
	code := h.inviteService.GetOrCreateInviteCode(ctx, userID)
	return &pb.GetInviteCodeResponse{
		Header:     okHeader(),
		InviteCode: code,
		ShareUrl:   fmt.Sprintf("https://bobobeads.com/invite/%s", code),
	}, nil
}

func (h *InviteHandler) BindInviteCode(ctx context.Context, req *pb.BindInviteCodeRequest) (*pb.BindInviteCodeResponse, error) {
	userID := middleware.GetUserID(ctx)
	reward, err := h.inviteService.BindInviteCode(ctx, userID, req.InviteCode)
	if err != nil {
		return &pb.BindInviteCodeResponse{Header: errHeader(err)}, nil
	}
	return &pb.BindInviteCodeResponse{Header: okHeader(), RewardCredits: int32(reward)}, nil
}

func (h *InviteHandler) GetInviteStats(ctx context.Context, req *pb.GetInviteStatsRequest) (*pb.GetInviteStatsResponse, error) {
	userID := middleware.GetUserID(ctx)
	totalInvites, totalRewards, err := h.inviteService.GetStats(ctx, userID)
	if err != nil {
		return &pb.GetInviteStatsResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetInviteStatsResponse{
		Header:       okHeader(),
		TotalInvites: int32(totalInvites),
		TotalRewards: int32(totalRewards),
	}, nil
}

func (h *InviteHandler) ListInviteRecords(ctx context.Context, req *pb.ListInviteRecordsRequest) (*pb.ListInviteRecordsResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	records, total, err := h.inviteService.ListRecords(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListInviteRecordsResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.InviteRecord
	for _, r := range records {
		items = append(items, &pb.InviteRecord{
			RewardCredits: 5,
			CreatedAt:     r.CreatedAt.Unix(),
		})
	}
	return &pb.ListInviteRecordsResponse{
		Header:  okHeader(),
		Records: items,
		Page:    pageResp(total, page, pageSize),
	}, nil
}
