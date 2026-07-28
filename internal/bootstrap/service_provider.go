package bootstrap

import (
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	admin "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	ai_generation "github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
	"github.com/zhaojiabo/bobobeads_server/internal/service/community"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/invite"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"github.com/zhaojiabo/bobobeads_server/internal/service/report"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/system"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/user"
	"github.com/zhaojiabo/bobobeads_server/internal/service/work"
	"go.uber.org/zap"
)

type ServiceProvider struct {
	// DAOs
	UserDAO            *dao.UserDAO
	WorkDAO            *dao.WorkDAO
	CommunityDAO       *dao.CommunityDAO
	TemplateDAO        *dao.TemplateDAO
	BlindBoxRecordDAO  *dao.BlindBoxRecordDAO
	OrderDAO           *dao.OrderDAO
	ProductDAO         *dao.ProductDAO
	SubscriptionDAO    *dao.SubscriptionDAO
	CreditDAO          *dao.CreditDAO
	InviteDAO          *dao.InviteDAO
	SystemDAO          *dao.SystemDAO
	GenerationDAO      *dao.GenerationDAO
	MediaDAO           *dao.MediaDAO
	AIGenerationDAO    *dao.AIGenerationDAO

	// Services
	AuthService          *auth.Service
	AdminAuthService     *admin.AuthService
	UserService          *user.Service
	WorkService          *work.Service
	MediaService         *media.Service
	CommunityService     *community.Service
	TemplateService      *template.Service
	TemplateAdminService *template.AdminService
	SubscribeService     *subscribe.Service
	CreditService        *credit.Service
	InviteService        *invite.Service
	SystemService        *system.Service
	ReportService        *report.Service
	GenerationService    *generation.Service
	AIGenerationService  *ai_generation.Service

	// AIDispatcher owns the worker pool that executes AI tasks; main starts and
	// stops it alongside the servers.
	AIDispatcher *ai_generation.Dispatcher

	// Handlers
	AuthHandler          *api.AuthHandler
	UserHandler          *api.UserHandler
	WorkHandler          *api.WorkHandler
	MediaHandler         *api.MediaHandler
	CommunityHandler     *api.CommunityHandler
	TemplateHandler      *api.TemplateHandler
	AdminTemplateHandler *api.AdminTemplateHandler
	AdminPortalHandler   *api.AdminPortalHTTPHandler
	SubscribeHandler     *api.SubscribeHandler
	CreditHandler        *api.CreditHandler
	InviteHandler        *api.InviteHandler
	SystemHandler        *api.SystemHandler
	ReportHandler        *api.ReportHandler
	GenerationHandler    *api.GenerationHandler
	AIGenerationHandler  *api.AIGenerationHandler
}

func NewServiceProvider() *ServiceProvider {
	sp := &ServiceProvider{}
	sp.initDAOs()
	sp.initServices()
	sp.initHandlers()
	return sp
}

func (sp *ServiceProvider) initDAOs() {
	sp.UserDAO = dao.NewUserDAO()
	sp.WorkDAO = dao.NewWorkDAO()
	sp.CommunityDAO = dao.NewCommunityDAO()
	sp.TemplateDAO = dao.NewTemplateDAO()
	sp.BlindBoxRecordDAO = dao.NewBlindBoxRecordDAO()
	sp.OrderDAO = dao.NewOrderDAO()
	sp.ProductDAO = dao.NewProductDAO()
	sp.SubscriptionDAO = dao.NewSubscriptionDAO()
	sp.CreditDAO = dao.NewCreditDAO()
	sp.InviteDAO = dao.NewInviteDAO()
	sp.SystemDAO = dao.NewSystemDAO()
	sp.GenerationDAO = dao.NewGenerationDAO()
	sp.MediaDAO = dao.NewMediaDAO()
	sp.AIGenerationDAO = dao.NewAIGenerationDAO()
}

func (sp *ServiceProvider) initServices() {
	sp.AuthService = auth.NewService(sp.UserDAO)
	sp.AdminAuthService = admin.NewAuthService(conf.GlobalConfig.Admin)
	sp.UserService = user.NewService(sp.UserDAO)
	sp.WorkService = work.NewService(sp.WorkDAO)
	sp.MediaService = media.NewService(sp.MediaDAO)
	sp.CommunityService = community.NewService(sp.CommunityDAO)
	sp.TemplateService = template.NewService(sp.TemplateDAO, sp.BlindBoxRecordDAO)
	sp.TemplateAdminService = template.NewAdminService(sp.TemplateDAO)
	sp.SubscribeService = subscribe.NewService(sp.OrderDAO, sp.ProductDAO, sp.SubscriptionDAO)
	sp.CreditService = credit.NewService(sp.CreditDAO)
	sp.InviteService = invite.NewService(sp.InviteDAO)
	sp.SystemService = system.NewService(sp.SystemDAO)
	sp.ReportService = report.NewService(sp.SystemDAO)
	sp.GenerationService = generation.NewService(sp.GenerationDAO, sp.CreditService, sp.SubscribeService, sp.WorkService)

	sp.initAIGeneration()

	sp.GenerationService.SetAIValidator(sp.AIGenerationService)
}

func (sp *ServiceProvider) initAIGeneration() {
	cfg := conf.GlobalConfig.AIGeneration

	// Fail closed: the fake provider still charges credits, so shipping it to
	// production would bill users for a placeholder image.
	if cfg.FakeProvider && conf.IsProd() {
		zap.L().Fatal("ai_generation.fake_provider must be false in production")
	}

	var provider ai_generation.Provider
	if cfg.FakeProvider {
		provider = ai_generation.NewFakeProvider()
	} else {
		// The client timeout is only a backstop behind the per-attempt context
		// deadline, so it must sit above it.
		vectorEngine, err := ai_generation.NewVectorEngineProvider(cfg.VectorEngine,
			time.Duration(cfg.ProviderTimeoutSec+20)*time.Second)
		if err != nil {
			zap.L().Fatal("build ai provider failed", zap.Error(err))
		}
		provider = vectorEngine
	}

	registry := ai_generation.NewRegistry(firstNonEmpty(cfg.ProviderName, provider.Name()), provider)

	sp.AIGenerationService = ai_generation.NewService(
		sp.AIGenerationDAO,
		sp.MediaDAO,
		sp.CreditService,
		sp.MediaService,
		sp.SubscribeService,
		registry,
		ai_generation.Config{
			TaskExpireMinutes:   cfg.TaskExpireMinutes,
			StuckRunningMinutes: cfg.StuckRunningMinutes,
			FreeConcurrent:      cfg.FreeConcurrent,
			FreeQueueSize:       cfg.FreeQueueSize,
			VIPConcurrent:       cfg.VIPConcurrent,
			VIPQueueSize:        cfg.VIPQueueSize,
		},
	)

	sp.AIDispatcher = ai_generation.NewDispatcher(sp.AIGenerationService, ai_generation.DispatcherConfig{
		MaxConcurrency: cfg.MaxConcurrency,
		BatchSize:      cfg.DispatchBatchSize,
		Interval:       time.Duration(cfg.DispatchIntervalMS) * time.Millisecond,
		TaskTimeout:    time.Duration(cfg.ProviderTimeoutSec) * time.Second,
	})
	sp.AIGenerationService.SetDispatchNotifier(sp.AIDispatcher.Notify)

	zap.L().Info("ai generation configured",
		zap.Strings("providers", registry.Names()),
		zap.Bool("fake_provider", cfg.FakeProvider))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (sp *ServiceProvider) initHandlers() {
	sp.AuthHandler = api.NewAuthHandler(sp.AuthService)
	sp.UserHandler = api.NewUserHandler(sp.UserService)
	sp.WorkHandler = api.NewWorkHandler(sp.WorkService)
	sp.MediaHandler = api.NewMediaHandler(sp.MediaService)
	sp.CommunityHandler = api.NewCommunityHandler(sp.CommunityService)
	sp.TemplateHandler = api.NewTemplateHandler(sp.TemplateService)
	sp.AdminTemplateHandler = api.NewAdminTemplateHandler(sp.TemplateAdminService)
	sp.AdminPortalHandler = api.NewAdminPortalHTTPHandler(sp.AdminAuthService, sp.MediaService, sp.TemplateService, sp.TemplateAdminService)
	sp.SubscribeHandler = api.NewSubscribeHandler(sp.SubscribeService)
	sp.CreditHandler = api.NewCreditHandler(sp.CreditService)
	sp.InviteHandler = api.NewInviteHandler(sp.InviteService)
	sp.SystemHandler = api.NewSystemHandler(sp.SystemService)
	sp.ReportHandler = api.NewReportHandler(sp.ReportService)
	sp.GenerationHandler = api.NewGenerationHandler(sp.GenerationService)
	sp.AIGenerationHandler = api.NewAIGenerationHandler(sp.AIGenerationService)
}
