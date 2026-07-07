package subscribe

import (
	"context"
	"fmt"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
)

type Service struct {
	orderDAO        *dao.OrderDAO
	productDAO      *dao.ProductDAO
	subscriptionDAO *dao.SubscriptionDAO
}

func NewService(orderDAO *dao.OrderDAO, productDAO *dao.ProductDAO, subscriptionDAO *dao.SubscriptionDAO) *Service {
	return &Service{
		orderDAO:        orderDAO,
		productDAO:      productDAO,
		subscriptionDAO: subscriptionDAO,
	}
}

func (s *Service) ListProducts(ctx context.Context, platform string) ([]*model.Product, error) {
	return s.productDAO.List(ctx, platform)
}

func (s *Service) CreateOrder(ctx context.Context, userID uint64, productID int, paymentMethod, platform string) (string, error) {
	product, err := s.productDAO.GetByID(ctx, productID)
	if err != nil {
		return "", fmt.Errorf("product not found: %w", err)
	}

	orderNo := fmt.Sprintf("BB%d%d", time.Now().UnixMilli(), userID%1000)
	order := &model.Order{
		OrderNo:       orderNo,
		UserID:        userID,
		ProductID:     productID,
		Amount:        product.Price,
		Currency:      product.Currency,
		PaymentMethod: paymentMethod,
		Platform:      platform,
		Status:        0,
	}
	if err := s.orderDAO.Create(ctx, order); err != nil {
		return "", err
	}
	return orderNo, nil
}

func (s *Service) GetPaymentParams(ctx context.Context, orderNo string) (string, map[string]string, error) {
	order, err := s.orderDAO.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return "", nil, err
	}
	// TODO: generate actual payment params based on payment method
	params := map[string]string{
		"order_no": order.OrderNo,
		"amount":   fmt.Sprintf("%d", order.Amount),
	}
	return order.PaymentMethod, params, nil
}

func (s *Service) HandlePaymentCallback(ctx context.Context, paymentMethod, rawData string) error {
	// TODO: verify payment and update order status
	return fmt.Errorf("not implemented")
}

func (s *Service) GetSubscription(ctx context.Context, userID uint64) (*model.Subscription, error) {
	return s.subscriptionDAO.GetActiveByUserID(ctx, userID)
}

func (s *Service) ListOrders(ctx context.Context, userID uint64, page, pageSize int) ([]*model.Order, int64, error) {
	offset := (page - 1) * pageSize
	return s.orderDAO.ListByUserID(ctx, userID, offset, pageSize)
}

func (s *Service) IsVIP(ctx context.Context, userID uint64) (bool, error) {
	sub, err := s.subscriptionDAO.GetActiveByUserID(ctx, userID)
	if err != nil {
		return false, nil
	}
	return sub.Status == 1 && sub.ExpireAt.After(time.Now()), nil
}
