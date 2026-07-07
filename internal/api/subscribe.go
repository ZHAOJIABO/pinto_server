package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
)

type SubscribeHandler struct {
	pb.UnimplementedSubscribeServiceServer
	subscribeService *subscribe.Service
}

func NewSubscribeHandler(subscribeService *subscribe.Service) *SubscribeHandler {
	return &SubscribeHandler{subscribeService: subscribeService}
}

func (h *SubscribeHandler) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
	platform := middleware.GetPlatform(ctx)
	products, err := h.subscribeService.ListProducts(ctx, platform)
	if err != nil {
		return &pb.ListProductsResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.ProductItem
	for _, p := range products {
		items = append(items, &pb.ProductItem{
			ProductId:      fmt.Sprintf("%d", p.ID),
			Sku:            p.SKU,
			Name:           p.Name,
			Description:    p.Description,
			PriceCents:     int32(p.Price),
			Currency:       p.Currency,
			DurationDays:   int32(p.DurationDays),
			AppleProductId: p.AppleProductID,
		})
	}
	return &pb.ListProductsResponse{Header: okHeader(), Products: items}, nil
}

func (h *SubscribeHandler) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	userID := middleware.GetUserID(ctx)
	platform := middleware.GetPlatform(ctx)
	productID := 0
	fmt.Sscanf(req.ProductId, "%d", &productID)
	orderNo, err := h.subscribeService.CreateOrder(ctx, userID, productID, req.PaymentMethod, platform)
	if err != nil {
		return &pb.CreateOrderResponse{Header: errHeader(err)}, nil
	}
	return &pb.CreateOrderResponse{Header: okHeader(), OrderNo: orderNo}, nil
}

func (h *SubscribeHandler) GetPaymentParams(ctx context.Context, req *pb.GetPaymentParamsRequest) (*pb.GetPaymentParamsResponse, error) {
	method, params, err := h.subscribeService.GetPaymentParams(ctx, req.OrderNo)
	if err != nil {
		return &pb.GetPaymentParamsResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetPaymentParamsResponse{
		Header:        okHeader(),
		PaymentMethod: method,
		Params:        params,
	}, nil
}

func (h *SubscribeHandler) PaymentCallback(ctx context.Context, req *pb.PaymentCallbackRequest) (*pb.PaymentCallbackResponse, error) {
	err := h.subscribeService.HandlePaymentCallback(ctx, req.PaymentMethod, req.RawData)
	if err != nil {
		return &pb.PaymentCallbackResponse{Header: errHeader(err), Success: false}, nil
	}
	return &pb.PaymentCallbackResponse{Header: okHeader(), Success: true}, nil
}

func (h *SubscribeHandler) RestorePurchase(ctx context.Context, req *pb.RestorePurchaseRequest) (*pb.RestorePurchaseResponse, error) {
	// TODO: verify Apple receipt
	return &pb.RestorePurchaseResponse{Header: okHeader()}, nil
}

func (h *SubscribeHandler) GetSubscription(ctx context.Context, req *pb.GetSubscriptionRequest) (*pb.GetSubscriptionResponse, error) {
	userID := middleware.GetUserID(ctx)
	sub, err := h.subscribeService.GetSubscription(ctx, userID)
	if err != nil {
		return &pb.GetSubscriptionResponse{Header: okHeader(), IsVip: false}, nil
	}
	return &pb.GetSubscriptionResponse{
		Header:   okHeader(),
		IsVip:    true,
		ExpireAt: sub.ExpireAt.Unix(),
	}, nil
}

func (h *SubscribeHandler) ListOrders(ctx context.Context, req *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	orders, total, err := h.subscribeService.ListOrders(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListOrdersResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.OrderItem
	for _, o := range orders {
		items = append(items, &pb.OrderItem{
			OrderNo:       o.OrderNo,
			AmountCents:   int32(o.Amount),
			Currency:      o.Currency,
			PaymentMethod: o.PaymentMethod,
			Status:        int32(o.Status),
			CreatedAt:     o.CreatedAt.Unix(),
		})
	}
	return &pb.ListOrdersResponse{
		Header: okHeader(),
		Orders: items,
		Page:   pageResp(total, page, pageSize),
	}, nil
}
