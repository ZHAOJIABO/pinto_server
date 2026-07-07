package api

import (
	"context"
	"fmt"

	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/service/system"
)

type SystemHandler struct {
	pb.UnimplementedSystemServiceServer
	systemService *system.Service
}

func NewSystemHandler(systemService *system.Service) *SystemHandler {
	return &SystemHandler{systemService: systemService}
}

func (h *SystemHandler) GetAppConfig(ctx context.Context, req *pb.GetAppConfigRequest) (*pb.GetAppConfigResponse, error) {
	configs, err := h.systemService.GetAppConfig(ctx)
	if err != nil {
		return &pb.GetAppConfigResponse{Header: errHeader(err)}, nil
	}
	return &pb.GetAppConfigResponse{Header: okHeader(), Configs: configs}, nil
}

func (h *SystemHandler) CheckUpdate(ctx context.Context, req *pb.CheckUpdateRequest) (*pb.CheckUpdateResponse, error) {
	// TODO: compare versions
	return &pb.CheckUpdateResponse{
		Header:    okHeader(),
		HasUpdate: false,
	}, nil
}

func (h *SystemHandler) GetBanners(ctx context.Context, req *pb.GetBannersRequest) (*pb.GetBannersResponse, error) {
	// TODO: fetch from DB
	return &pb.GetBannersResponse{Header: okHeader(), Banners: []*pb.BannerItem{}}, nil
}

func (h *SystemHandler) GetBeadColors(ctx context.Context, req *pb.GetBeadColorsRequest) (*pb.GetBeadColorsResponse, error) {
	colors, err := h.systemService.GetBeadColors(ctx, req.Brand)
	if err != nil {
		return &pb.GetBeadColorsResponse{Header: errHeader(err)}, nil
	}

	brandMap := make(map[string]*pb.BeadBrand)
	for _, c := range colors {
		brand, ok := brandMap[c.Brand]
		if !ok {
			brand = &pb.BeadBrand{Brand: c.Brand, DisplayName: c.Brand}
			brandMap[c.Brand] = brand
		}
		brand.Colors = append(brand.Colors, &pb.BeadColor{
			Code: c.Code,
			Name: c.Name,
			Hex:  c.Hex,
		})
	}

	var brands []*pb.BeadBrand
	for _, b := range brandMap {
		brands = append(brands, b)
	}
	return &pb.GetBeadColorsResponse{Header: okHeader(), Brands: brands}, nil
}

func (h *SystemHandler) GetBoardSpecs(ctx context.Context, req *pb.GetBoardSpecsRequest) (*pb.GetBoardSpecsResponse, error) {
	specs, err := h.systemService.GetBoardSpecs(ctx)
	if err != nil {
		return &pb.GetBoardSpecsResponse{Header: errHeader(err)}, nil
	}
	var items []*pb.BoardSpec
	for _, s := range specs {
		items = append(items, &pb.BoardSpec{
			SpecId:   fmt.Sprintf("%d", s.ID),
			Name:     s.Name,
			Shape:    s.Shape,
			Width:    int32(s.Width),
			Height:   int32(s.Height),
			BeadSize: fmt.Sprintf("%.1fmm", s.BeadSize),
		})
	}
	return &pb.GetBoardSpecsResponse{Header: okHeader(), Specs: items}, nil
}
