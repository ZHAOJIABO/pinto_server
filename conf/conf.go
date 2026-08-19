package conf

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

var GlobalConfig *Config

type Config struct {
	Server             ServerConfig             `mapstructure:"server"`
	MySQL              MySQLConfig              `mapstructure:"mysql"`
	Redis              RedisConfig              `mapstructure:"redis"`
	OSS                OSSConfig                `mapstructure:"oss"`
	JWT                JWTConfig                `mapstructure:"jwt"`
	SMS                SMSConfig                `mapstructure:"sms"`
	WeChat             WeChatConfig             `mapstructure:"wechat"`
	Apple              AppleConfig              `mapstructure:"apple"`
	Log                LogConfig                `mapstructure:"log"`
	Payment            PaymentConfig            `mapstructure:"payment"`
	Generation         GenerationConfig         `mapstructure:"generation"`
	Pattern            PatternConfig            `mapstructure:"pattern"`
	TemplateSubmission TemplateSubmissionConfig `mapstructure:"template_submission"`
	TemplateDraft      TemplateDraftConfig      `mapstructure:"template_draft"`
	ExportWatermark    ExportWatermarkConfig    `mapstructure:"export_watermark"`
	AIGeneration       AIGenerationConfig       `mapstructure:"ai_generation"`
	AdminService       AdminServiceConfig       `mapstructure:"admin_service"`
	Admin              AdminConfig              `mapstructure:"admin"`
}

type AdminServiceConfig struct {
	Token string `mapstructure:"token"`
}

// AdminConfig protects the browser-based template management portal. It is
// deliberately separate from end-user authentication and service credentials:
// browser bundles must never contain the internal service token.
type AdminConfig struct {
	JWTSecret     string               `mapstructure:"jwt_secret"`
	AccessExpireM int                  `mapstructure:"access_expire_m"`
	Accounts      []AdminAccountConfig `mapstructure:"accounts"`
}

type AdminAccountConfig struct {
	Username     string `mapstructure:"username"`
	PasswordHash string `mapstructure:"password_hash"`
}

type ServerConfig struct {
	Name              string   `mapstructure:"name"`
	Mode              string   `mapstructure:"mode"` // local, dev, prod
	GRPCPort          int      `mapstructure:"grpc_port"`
	HTTPPort          int      `mapstructure:"http_port"`
	TrustedProxyCIDRs []string `mapstructure:"trusted_proxy_cidrs"`
}

type MySQLConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// OSSConfig holds the server-side credentials for Alibaba Cloud OSS. Keep
// access keys in a local override or a secret manager, never the shared YAML.
type OSSConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	Region          string `mapstructure:"region"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	Bucket          string `mapstructure:"bucket"`
	PublicBaseURL   string `mapstructure:"public_base_url"`
}

type JWTConfig struct {
	Secret         string `mapstructure:"secret"`
	AccessExpireH  int    `mapstructure:"access_expire_h"`
	RefreshExpireH int    `mapstructure:"refresh_expire_h"`
}

type SMSConfig struct {
	SecretID   string `mapstructure:"secret_id"`
	SecretKey  string `mapstructure:"secret_key"`
	AppID      string `mapstructure:"app_id"`
	SignName   string `mapstructure:"sign_name"`
	TemplateID string `mapstructure:"template_id"`
}

type WeChatConfig struct {
	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
	MpAppID   string `mapstructure:"mp_app_id"`
	MpSecret  string `mapstructure:"mp_secret"`
}

type AppleConfig struct {
	TeamID   string `mapstructure:"team_id"`
	ClientID string `mapstructure:"client_id"`
	KeyID    string `mapstructure:"key_id"`
	KeyFile  string `mapstructure:"key_file"`
}

type LogConfig struct {
	Level   string `mapstructure:"level"`
	Path    string `mapstructure:"path"`
	MaxSize int    `mapstructure:"max_size"`
	MaxAge  int    `mapstructure:"max_age"`
}

type PaymentConfig struct {
	WechatPay WechatPayConfig `mapstructure:"wechat_pay"`
}

type WechatPayConfig struct {
	MchID     string `mapstructure:"mch_id"`
	APIKey    string `mapstructure:"api_key"`
	CertFile  string `mapstructure:"cert_file"`
	KeyFile   string `mapstructure:"key_file"`
	NotifyURL string `mapstructure:"notify_url"`
}

type GenerationConfig struct {
	DailyFreeLimit int `mapstructure:"daily_free_limit"`
	CreditCost     int `mapstructure:"credit_cost"`
	ExpireMinutes  int `mapstructure:"expire_minutes"`
}

type PatternConfig struct {
	MaxWidth  int `mapstructure:"max_width"`
	MaxHeight int `mapstructure:"max_height"`
	MaxPixels int `mapstructure:"max_pixels"`
	MaxColors int `mapstructure:"max_colors"`
}

// TemplateSubmissionConfig 限制用户投稿图纸的频率。RateLimitInterceptor 是
// 每秒粒度的，拦不住一天刷几百条投稿，所以这里另加一个每日配额。
type TemplateSubmissionConfig struct {
	DailyLimit int `mapstructure:"daily_limit"`
}

// TemplateDraftConfig 限制草稿箱规模。草稿对全部管理员共享且带完整 pattern_data，
// 不设上限的话草稿箱会无声地长成一张几 GB 的表。MaxCount 只在创建草稿时校验，
// 且 COUNT→INSERT 不原子，所以它是近似上限而非硬边界。
type TemplateDraftConfig struct {
	MaxCount int `mapstructure:"max_count"`
}

// ExportWatermarkConfig controls the CDN watermark URL the client applies
// when it renders a downloadable pattern. Supported modes are none,
// marketing, and online.
type ExportWatermarkConfig struct {
	Mode         string `mapstructure:"mode"`
	MarketingURL string `mapstructure:"marketing_url"`
	OnlineURL    string `mapstructure:"online_url"`
}

type AIGenerationConfig struct {
	TaskExpireMinutes int  `mapstructure:"task_expire_minutes"`
	WorkerInterval    int  `mapstructure:"worker_interval"`
	FakeProvider      bool `mapstructure:"fake_provider"`

	// DefaultModel is the key in Models used when neither bb_config nor the
	// style row names one. It is the last link in the chain, so it must always
	// point at a model that is actually configured.
	DefaultModel string `mapstructure:"default_model"`

	// RetryModel serves user-initiated retries, so a model that just failed is
	// not handed the same job again. Empty means retries reuse the first-attempt
	// chain.
	RetryModel string `mapstructure:"retry_model"`

	// MaxConcurrency is the ceiling on generation requests in flight towards the
	// provider. The provider itself imposes no limit, so this is our own
	// throttle on burst cost and memory; with more than one server instance,
	// divide it by the instance count because the semaphore is process-local.
	MaxConcurrency      int `mapstructure:"max_concurrency"`
	DispatchIntervalMS  int `mapstructure:"dispatch_interval_ms"`
	DispatchBatchSize   int `mapstructure:"dispatch_batch_size"`
	ProviderTimeoutSec  int `mapstructure:"provider_timeout_sec"`
	StuckRunningMinutes int `mapstructure:"stuck_running_minutes"`

	// AvgDurationSec is the observed average generation time, used only to
	// estimate the progress percentage we report to clients. Providers give us
	// no progress signal, so this is a display knob: retune it from production
	// latency without shipping an app release.
	AvgDurationSec int `mapstructure:"avg_duration_sec"`

	FreeConcurrent int `mapstructure:"free_concurrent"`
	FreeQueueSize  int `mapstructure:"free_queue_size"`
	VIPConcurrent  int `mapstructure:"vip_concurrent"`
	VIPQueueSize   int `mapstructure:"vip_queue_size"`

	// Models maps a logical model key (referenced by bb_config, bb_ai_style or
	// DefaultModel) to the adapter that speaks its protocol plus that model's
	// own defaults. Adding a model that reuses a known protocol is a config
	// change only. Keys must not contain a dot: viper treats it as a nesting
	// separator and would split the key apart.
	Models map[string]AIModelConfig `mapstructure:"models"`
}

// AIModelConfig is one upstream model. Adapter selects the request/response
// protocol, Model is the name the vendor expects on the wire, and Options
// carries the protocol-specific knobs (size, quality, aspect_ratio,
// image_size) so adding one never changes this struct. Keep api_key in an
// untracked local override, never in the shared YAML.
type AIModelConfig struct {
	Adapter string            `mapstructure:"adapter"`
	BaseURL string            `mapstructure:"base_url"`
	APIKey  string            `mapstructure:"api_key"`
	Model   string            `mapstructure:"model"`
	Options map[string]string `mapstructure:"options"`
}

func Init(configPath string) error {
	if configPath == "" {
		configPath = "conf/server.yaml"
	}

	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	if err := mergeLocalOverride(configPath); err != nil {
		return err
	}

	GlobalConfig = &Config{}
	if err := viper.Unmarshal(GlobalConfig); err != nil {
		return err
	}

	return nil
}

// mergeLocalOverride loads a developer-only sibling config when it exists.
// For example, conf/server.yaml can be supplemented by the untracked
// conf/server.local.yaml so administrator credentials never need to be
// committed to the shared base config.
func mergeLocalOverride(configPath string) error {
	ext := filepath.Ext(configPath)
	localConfigPath := strings.TrimSuffix(configPath, ext) + ".local" + ext
	if _, err := os.Stat(localConfigPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	viper.SetConfigFile(localConfigPath)
	return viper.MergeInConfig()
}

func IsLocal() bool {
	return GlobalConfig.Server.Mode == "local"
}

func IsDev() bool {
	return GlobalConfig.Server.Mode == "dev"
}

func IsProd() bool {
	return GlobalConfig.Server.Mode == "prod"
}

func GetEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
