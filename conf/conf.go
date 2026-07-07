package conf

import (
	"os"

	"github.com/spf13/viper"
)

var GlobalConfig *Config

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	MySQL      MySQLConfig      `mapstructure:"mysql"`
	Redis      RedisConfig      `mapstructure:"redis"`
	OSS        OSSConfig        `mapstructure:"oss"`
	JWT        JWTConfig        `mapstructure:"jwt"`
	SMS        SMSConfig        `mapstructure:"sms"`
	WeChat     WeChatConfig     `mapstructure:"wechat"`
	Apple      AppleConfig      `mapstructure:"apple"`
	Log        LogConfig        `mapstructure:"log"`
	Payment    PaymentConfig    `mapstructure:"payment"`
	Generation GenerationConfig `mapstructure:"generation"`
	Pattern    PatternConfig    `mapstructure:"pattern"`
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

type OSSConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	BucketName      string `mapstructure:"bucket_name"`
	CDNDomain       string `mapstructure:"cdn_domain"`
}

type JWTConfig struct {
	Secret          string `mapstructure:"secret"`
	AccessExpireH   int    `mapstructure:"access_expire_h"`
	RefreshExpireH  int    `mapstructure:"refresh_expire_h"`
}

type SMSConfig struct {
	SecretID  string `mapstructure:"secret_id"`
	SecretKey string `mapstructure:"secret_key"`
	AppID     string `mapstructure:"app_id"`
	SignName  string `mapstructure:"sign_name"`
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
	Level    string `mapstructure:"level"`
	Path     string `mapstructure:"path"`
	MaxSize  int    `mapstructure:"max_size"`
	MaxAge   int    `mapstructure:"max_age"`
}

type PaymentConfig struct {
	WechatPay WechatPayConfig `mapstructure:"wechat_pay"`
}

type WechatPayConfig struct {
	MchID      string `mapstructure:"mch_id"`
	APIKey     string `mapstructure:"api_key"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	NotifyURL  string `mapstructure:"notify_url"`
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

func Init(configPath string) error {
	if configPath == "" {
		configPath = "conf/server.yaml"
	}

	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	GlobalConfig = &Config{}
	if err := viper.Unmarshal(GlobalConfig); err != nil {
		return err
	}

	return nil
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
