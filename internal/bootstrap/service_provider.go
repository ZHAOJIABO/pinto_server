package bootstrap

import (
	"fmt"
	"time"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"github.com/zhaojiabo/bobobeads_server/internal/api"
	"github.com/zhaojiabo/bobobeads_server/internal/dao"
	admin "github.com/zhaojiabo/bobobeads_server/internal/service/admin"
	ai_generation "github.com/zhaojiabo/bobobeads_server/internal/service/ai_generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/auth"
	"github.com/zhaojiabo/bobobeads_server/internal/service/community"
	"github.com/zhaojiabo/bobobeads_server/internal/service/credit"
	"github.com/zhaojiabo/bobobeads_server/internal/service/finishedproduct"
	"github.com/zhaojiabo/bobobeads_server/internal/service/generation"
	"github.com/zhaojiabo/bobobeads_server/internal/service/invite"
	"github.com/zhaojiabo/bobobeads_server/internal/service/media"
	"github.com/zhaojiabo/bobobeads_server/internal/service/report"
	"github.com/zhaojiabo/bobobeads_server/internal/service/subscribe"
	"github.com/zhaojiabo/bobobeads_server/internal/service/system"
	"github.com/zhaojiabo/bobobeads_server/internal/service/template"
	"github.com/zhaojiabo/bobobeads_server/internal/service/templatesubmission"
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
	FinishedProductDAO *dao.FinishedProductDAO
	SubmissionDAO      *dao.TemplateSubmissionDAO
	TemplateDraftDAO   *dao.TemplateDraftDAO

	// Services
	AuthService            *auth.Service
	AdminAuthService       *admin.AuthService
	UserService            *user.Service
	WorkService            *work.Service
	MediaService           *media.Service
	FinishedProductService *finishedproduct.Service
	CommunityService       *community.Service
	TemplateService        *template.Service
	TemplateAdminService   *template.AdminService
	SubmissionService      *templatesubmission.Service
	TemplateDraftService   *template.DraftService
	SubscribeService       *subscribe.Service
	CreditService          *credit.Service
	InviteService          *invite.Service
	SystemService          *system.Service
	ReportService          *report.Service
	GenerationService      *generation.Service
	AIGenerationService    *ai_generation.Service

	// AIDispatcher owns the worker pool that executes AI tasks; main starts and
	// stops it alongside the servers.
	AIDispatcher *ai_generation.Dispatcher

	// Handlers
	AuthHandler            *api.AuthHandler
	UserHandler            *api.UserHandler
	WorkHandler            *api.WorkHandler
	MediaHandler           *api.MediaHandler
	FinishedProductHandler *api.FinishedProductHandler
	CommunityHandler       *api.CommunityHandler
	TemplateHandler        *api.TemplateHandler
	AdminTemplateHandler   *api.AdminTemplateHandler
	SubmissionHandler      *api.TemplateSubmissionHandler
	AdminPortalHandler     *api.AdminPortalHTTPHandler
	SubscribeHandler       *api.SubscribeHandler
	CreditHandler          *api.CreditHandler
	InviteHandler          *api.InviteHandler
	SystemHandler          *api.SystemHandler
	ReportHandler          *api.ReportHandler
	GenerationHandler      *api.GenerationHandler
	AIGenerationHandler    *api.AIGenerationHandler
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
	sp.FinishedProductDAO = dao.NewFinishedProductDAO()
	sp.SubmissionDAO = dao.NewTemplateSubmissionDAO()
	sp.TemplateDraftDAO = dao.NewTemplateDraftDAO()
}

func (sp *ServiceProvider) initServices() {
	sp.AuthService = auth.NewService(sp.UserDAO)
	sp.AdminAuthService = admin.NewAuthService(conf.GlobalConfig.Admin)
	sp.UserService = user.NewService(sp.UserDAO)
	sp.MediaService = media.NewService(sp.MediaDAO)
	sp.WorkService = work.NewService(sp.WorkDAO, sp.MediaService, sp.SubmissionDAO)
	sp.FinishedProductService = finishedproduct.NewService(sp.FinishedProductDAO, sp.MediaService)
	sp.CommunityService = community.NewService(sp.CommunityDAO)
	sp.TemplateService = template.NewService(sp.TemplateDAO, sp.BlindBoxRecordDAO)
	sp.TemplateAdminService = template.NewAdminService(sp.TemplateDAO)
	sp.SubmissionService = templatesubmission.NewService(
		sp.SubmissionDAO, sp.WorkDAO, sp.UserDAO, sp.MediaService, sp.TemplateAdminService,
		conf.GlobalConfig.TemplateSubmission.DailyLimit,
	)
	sp.TemplateDraftService = template.NewDraftService(
		sp.TemplateDraftDAO, sp.TemplateDAO, sp.MediaService, sp.TemplateAdminService,
		conf.GlobalConfig.TemplateDraft.MaxCount,
	)
	sp.SubscribeService = subscribe.NewService(sp.OrderDAO, sp.ProductDAO, sp.SubscriptionDAO)
	sp.CreditService = credit.NewService(sp.CreditDAO)
	sp.InviteService = invite.NewService(sp.InviteDAO)
	sp.SystemService = system.NewService(sp.SystemDAO, conf.GlobalConfig.ExportWatermark)
	sp.ReportService = report.NewService(sp.SystemDAO)
	sp.GenerationService = generation.NewService(sp.GenerationDAO, sp.CreditService, sp.SubscribeService, sp.WorkService, sp.MediaService)

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
	if len(cfg.Models) == 0 {
		zap.L().Fatal("ai_generation.models must configure at least one model")
	}
	// The default is the end of the resolution chain, so a typo here would only
	// surface as a failed task at runtime.
	if _, ok := cfg.Models[cfg.DefaultModel]; !ok {
		zap.L().Fatal("ai_generation.default_model must name a configured model",
			zap.String("default_model", cfg.DefaultModel))
	}
	// Empty is legal and means retries reuse the first-attempt chain, but a
	// non-empty typo would silently degrade every retry back to that chain.
	if cfg.RetryModel != "" {
		if _, ok := cfg.Models[cfg.RetryModel]; !ok {
			zap.L().Fatal("ai_generation.retry_model must name a configured model",
				zap.String("retry_model", cfg.RetryModel))
		}
	}

	providers := make([]ai_generation.Provider, 0, len(cfg.Models))
	for key, modelCfg := range cfg.Models {
		provider, err := buildAIProvider(key, modelCfg, cfg)
		if err != nil {
			zap.L().Fatal("build ai provider failed", zap.String("model", key), zap.Error(err))
		}
		providers = append(providers, provider)
	}
	registry := ai_generation.NewRegistry(providers...)

	sp.AIGenerationService = ai_generation.NewService(
		sp.AIGenerationDAO,
		sp.MediaDAO,
		sp.CreditService,
		sp.MediaService,
		sp.SubscribeService,
		sp.SystemDAO,
		registry,
		ai_generation.Config{
			TaskExpireMinutes:   cfg.TaskExpireMinutes,
			StuckRunningMinutes: cfg.StuckRunningMinutes,
			FreeConcurrent:      cfg.FreeConcurrent,
			FreeQueueSize:       cfg.FreeQueueSize,
			VIPConcurrent:       cfg.VIPConcurrent,
			VIPQueueSize:        cfg.VIPQueueSize,
			DefaultModel:        cfg.DefaultModel,
			RetryModel:          cfg.RetryModel,
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
		zap.Strings("models", registry.Names()),
		zap.String("default_model", cfg.DefaultModel),
		zap.String("retry_model", cfg.RetryModel),
		zap.Bool("fake_provider", cfg.FakeProvider))
}

// buildAIProvider maps one configured model onto the adapter that speaks its
// protocol. In fake mode every model key is served by the placeholder provider,
// so a local setup can run any style without third-party credentials.
func buildAIProvider(key string, modelCfg conf.AIModelConfig, cfg conf.AIGenerationConfig) (ai_generation.Provider, error) {
	if cfg.FakeProvider {
		return ai_generation.NewFakeProvider(key), nil
	}
	// The client timeout is only a backstop behind the per-attempt context
	// deadline, so it must sit above it.
	timeout := time.Duration(cfg.ProviderTimeoutSec+20) * time.Second
	switch modelCfg.Adapter {
	case ai_generation.AdapterOpenAIImageEdit:
		return ai_generation.NewOpenAIImageEditProvider(key, modelCfg, timeout)
	case ai_generation.AdapterGeminiGenerateContent:
		return ai_generation.NewGeminiProvider(key, modelCfg, timeout)
	default:
		return nil, fmt.Errorf("unknown adapter %q", modelCfg.Adapter)
	}
}

func (sp *ServiceProvider) initHandlers() {
	sp.AuthHandler = api.NewAuthHandler(sp.AuthService)
	sp.UserHandler = api.NewUserHandler(sp.UserService)
	sp.WorkHandler = api.NewWorkHandler(sp.WorkService)
	sp.MediaHandler = api.NewMediaHandler(sp.MediaService)
	sp.FinishedProductHandler = api.NewFinishedProductHandler(sp.FinishedProductService)
	sp.CommunityHandler = api.NewCommunityHandler(sp.CommunityService, sp.UserService)
	sp.TemplateHandler = api.NewTemplateHandler(sp.TemplateService)
	sp.AdminTemplateHandler = api.NewAdminTemplateHandler(sp.TemplateAdminService)
	sp.SubmissionHandler = api.NewTemplateSubmissionHandler(sp.SubmissionService)
	sp.AdminPortalHandler = api.NewAdminPortalHTTPHandler(sp.AdminAuthService, sp.MediaService, sp.TemplateService, sp.TemplateAdminService, sp.SubmissionService, sp.TemplateDraftService)
	sp.SubscribeHandler = api.NewSubscribeHandler(sp.SubscribeService)
	sp.CreditHandler = api.NewCreditHandler(sp.CreditService)
	sp.InviteHandler = api.NewInviteHandler(sp.InviteService)
	sp.SystemHandler = api.NewSystemHandler(sp.SystemService)
	sp.ReportHandler = api.NewReportHandler(sp.ReportService)
	sp.GenerationHandler = api.NewGenerationHandler(sp.GenerationService)
	sp.AIGenerationHandler = api.NewAIGenerationHandler(sp.AIGenerationService, conf.GlobalConfig.AIGeneration.AvgDurationSec)
}
