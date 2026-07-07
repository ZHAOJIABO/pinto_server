package api

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/report"
)

type ReportHandler struct {
	pb.UnimplementedReportServiceServer
	reportService *report.Service
}

func NewReportHandler(reportService *report.Service) *ReportHandler {
	return &ReportHandler{reportService: reportService}
}

func (h *ReportHandler) ReportEvent(ctx context.Context, req *pb.ReportEventRequest) (*pb.ReportEventResponse, error) {
	userID := middleware.GetUserID(ctx)
	var events []report.Event
	for _, e := range req.Events {
		events = append(events, report.Event{
			Name:      e.EventName,
			Params:    e.Params,
			Timestamp: e.Timestamp,
		})
	}
	h.reportService.ReportEvents(ctx, userID, events)
	return &pb.ReportEventResponse{Header: okHeader()}, nil
}

func (h *ReportHandler) ReportError(ctx context.Context, req *pb.ReportErrorRequest) (*pb.ReportErrorResponse, error) {
	userID := middleware.GetUserID(ctx)
	h.reportService.ReportError(ctx, userID, req.ErrorType, req.Message, req.StackTrace)
	return &pb.ReportErrorResponse{Header: okHeader()}, nil
}

func (h *ReportHandler) SubmitFeedback(ctx context.Context, req *pb.SubmitFeedbackRequest) (*pb.SubmitFeedbackResponse, error) {
	userID := middleware.GetUserID(ctx)
	platform := middleware.GetPlatform(ctx)
	if err := h.reportService.SubmitFeedback(ctx, userID, req.Content, req.Contact, platform, ""); err != nil {
		return &pb.SubmitFeedbackResponse{Header: errHeader(err)}, nil
	}
	return &pb.SubmitFeedbackResponse{Header: okHeader()}, nil
}
