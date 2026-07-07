package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/bootstrap"
	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/middleware"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"github.com/zhaojiabo/bobobeads_server/internal/task"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	configPath := flag.String("config", "conf/server.yaml", "config file path")
	flag.Parse()

	if err := conf.Init(*configPath); err != nil {
		panic(fmt.Sprintf("failed to init config: %v", err))
	}

	logger, _ := zap.NewDevelopment()
	if conf.IsProd() {
		logger, _ = zap.NewProduction()
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	if err := db.InitMySQL(); err != nil {
		zap.L().Fatal("failed to init mysql", zap.Error(err))
	}
	defer db.CloseMySQL()

	if err := db.AutoMigrate(
		&model.User{},
		&model.Work{},
		&model.CommunityPost{},
		&model.Like{},
		&model.Favorite{},
		&model.Comment{},
		&model.Follow{},
		&model.Template{},
		&model.TemplateCategory{},
		&model.Order{},
		&model.Product{},
		&model.Subscription{},
		&model.CreditTransaction{},
		&model.CreditAccount{},
		&model.Invite{},
		&model.BeadColor{},
		&model.BoardSpec{},
		&model.Config{},
		&model.Feedback{},
		&model.Generation{},
		&model.MediaAsset{},
	); err != nil {
		zap.L().Fatal("failed to auto migrate", zap.Error(err))
	}

	if err := db.InitRedis(); err != nil {
		zap.L().Fatal("failed to init redis", zap.Error(err))
	}
	defer db.CloseRedis()

	sp := bootstrap.NewServiceProvider()

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.TraceInterceptor(),
			middleware.PlatformInterceptor(),
			middleware.AuthInterceptor(sp.AuthService),
			middleware.RateLimitInterceptor(30),
		),
	)

	pb.RegisterAuthServiceServer(grpcServer, sp.AuthHandler)
	pb.RegisterUserServiceServer(grpcServer, sp.UserHandler)
	pb.RegisterWorkServiceServer(grpcServer, sp.WorkHandler)
	pb.RegisterMediaServiceServer(grpcServer, sp.MediaHandler)
	pb.RegisterCommunityServiceServer(grpcServer, sp.CommunityHandler)
	pb.RegisterTemplateServiceServer(grpcServer, sp.TemplateHandler)
	pb.RegisterSubscribeServiceServer(grpcServer, sp.SubscribeHandler)
	pb.RegisterCreditServiceServer(grpcServer, sp.CreditHandler)
	pb.RegisterInviteServiceServer(grpcServer, sp.InviteHandler)
	pb.RegisterSystemServiceServer(grpcServer, sp.SystemHandler)
	pb.RegisterReportServiceServer(grpcServer, sp.ReportHandler)
	pb.RegisterGenerationServiceServer(grpcServer, sp.GenerationHandler)

	if !conf.IsProd() {
		reflection.Register(grpcServer)
	}

	grpcPort := conf.GlobalConfig.Server.GRPCPort
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		zap.L().Fatal("failed to listen", zap.Error(err))
	}

	go func() {
		zap.L().Info("gRPC server started", zap.Int("port", grpcPort))
		if err := grpcServer.Serve(lis); err != nil {
			zap.L().Fatal("grpc serve failed", zap.Error(err))
		}
	}()

	// HTTP Gateway
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	grpcAddr := fmt.Sprintf("localhost:%d", grpcPort)

	pb.RegisterAuthServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterUserServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterWorkServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterMediaServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterCommunityServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterTemplateServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterSubscribeServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterCreditServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterInviteServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterSystemServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterReportServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)
	pb.RegisterGenerationServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts)

	httpPort := conf.GlobalConfig.Server.HTTPPort
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: corsMiddleware(mux),
	}

	go func() {
		zap.L().Info("HTTP gateway started", zap.Int("port", httpPort))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("http serve failed", zap.Error(err))
		}
	}()

	// Background tasks
	genTimeoutProcessor := task.NewGenerationTimeoutProcessor(sp.GenerationService)
	genTimeoutProcessor.Start()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zap.L().Info("shutting down...")
	genTimeoutProcessor.Stop()
	grpcServer.GracefulStop()
	httpServer.Shutdown(ctx)
	zap.L().Info("server stopped")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Platform, X-App-Version, X-Device-Id")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
