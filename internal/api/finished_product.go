package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/finishedproduct"
)

type FinishedProductHandler struct {
	pb.UnimplementedFinishedProductServiceServer
	finishedProductService *finishedproduct.Service
}

func NewFinishedProductHandler(finishedProductService *finishedproduct.Service) *FinishedProductHandler {
	return &FinishedProductHandler{finishedProductService: finishedProductService}
}

func (h *FinishedProductHandler) ListFinishedProducts(ctx context.Context, req *pb.ListFinishedProductsRequest) (*pb.ListFinishedProductsResponse, error) {
	userID := middleware.GetUserID(ctx)
	products, nextCursor, err := h.finishedProductService.List(ctx, userID, req.Limit, req.Cursor)
	if err != nil {
		return &pb.ListFinishedProductsResponse{Header: errHeaderCtx(ctx, err)}, nil
	}

	items := make([]*pb.FinishedProductItem, 0, len(products))
	for _, fp := range products {
		items = append(items, h.toProto(fp))
	}
	return &pb.ListFinishedProductsResponse{
		Header:     okHeaderCtx(ctx),
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (h *FinishedProductHandler) CreateFinishedProduct(ctx context.Context, req *pb.CreateFinishedProductRequest) (*pb.CreateFinishedProductResponse, error) {
	userID := middleware.GetUserID(ctx)
	fp, err := h.finishedProductService.Create(ctx, userID, req.MediaFileKey, req.ClientRequestId)
	if err != nil {
		return &pb.CreateFinishedProductResponse{Header: errHeaderCtx(ctx, err)}, nil
	}
	return &pb.CreateFinishedProductResponse{
		Header: okHeaderCtx(ctx),
		Item:   h.toProto(fp),
	}, nil
}

func (h *FinishedProductHandler) toProto(fp *model.FinishedProduct) *pb.FinishedProductItem {
	return &pb.FinishedProductItem{
		FinishedProductId: fmt.Sprintf("%d", fp.ID),
		ImageUrl:          fp.ImageURL,
		ThumbnailUrl:      fp.ThumbnailURL,
		CreatedAt:         fp.CreatedAt.Unix(),
	}
}
