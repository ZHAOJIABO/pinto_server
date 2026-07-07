package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
)

type CreditHandler struct {
	pb.UnimplementedCreditServiceServer
	creditService *credit.Service
}

func NewCreditHandler(creditService *credit.Service) *CreditHandler {
	return &CreditHandler{creditService: creditService}
}

func (h *CreditHandler) GetBalance(ctx context.Context, req *pb.GetBalanceRequest) (*pb.GetBalanceResponse, error) {
	userID := middleware.GetUserID(ctx)
	balance, err := h.creditService.GetBalance(ctx, userID)
	if err != nil {
		return &pb.GetBalanceResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetBalanceResponse{
		Header:  okHeader(),
		Balance: int32(balance),
		DailyFreeTotal: 3,
	}, nil
}

func (h *CreditHandler) ListTransactions(ctx context.Context, req *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	userID := middleware.GetUserID(ctx)
	page, pageSize := getPage(req.Page)
	txs, total, err := h.creditService.ListTransactions(ctx, userID, page, pageSize)
	if err != nil {
		return &pb.ListTransactionsResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.TransactionItem
	for _, tx := range txs {
		items = append(items, &pb.TransactionItem{
			TransactionId: fmt.Sprintf("%d", tx.ID),
			Amount:        int32(tx.Amount),
			BalanceAfter:  int32(tx.Balance),
			Type:          tx.Type,
			Description:   tx.Description,
			CreatedAt:     tx.CreatedAt.Unix(),
		})
	}
	return &pb.ListTransactionsResponse{
		Header:       okHeader(),
		Transactions: items,
		Page:         pageResp(total, page, pageSize),
	}, nil
}
