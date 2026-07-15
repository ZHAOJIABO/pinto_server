package conf

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

var GlobalConfig *Config

type Config struct {
	Server       ServerConfig       `mapstructure:"server"`
	MySQL        MySQLConfig        `mapstructure:"mysql"`
	Redis        RedisConfig        `mapstructure:"redis"`
	OSS          OSSConfig          `mapstructure:"oss"`
	JWT          JWTConfig          `mapstructure:"jwt"`
	SMS          SMSConfig          `mapstructure:"sms"`
	WeChat       WeChatConfig       `mapstructure:"wechat"`
	Apple        AppleConfig        `mapstructure:"apple"`
	Log          LogConfig          `mapstructure:"log"`
	Payment      PaymentConfig      `mapstructure:"payment"`
	Generation   GenerationConfig   `mapstructure:"generation"`
	Pattern      PatternConfig      `mapstructure:"pattern"`
	AIGeneration AIGenerationConfig `mapstructure:"ai_generation"`
	AdminService AdminServiceConfig `mapstructure:"admin_service"`
	Admin        AdminConfig        `mapstructure:"admin"`
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
	Name     string `mapstructure:"name"`
	Mode     string `mapstructure:"mode"` // local, dev, prod
	GRPCPort int    `mapstructure:"grpc_port"`
	HTTPPort int    `mapstructure:"http_port"`
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

type AIGenerationConfig struct {
	TaskExpireMinutes int    `mapstructure:"task_expire_minutes"`
	WorkerInterval    int    `mapstructure:"worker_interval"`
	FakeProvider      bool   `mapstructure:"fake_provider"`
	ProviderName      string `mapstructure:"provider_name"`
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
