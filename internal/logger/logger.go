package logger

import (
	"context"

	"github.com/zhaojiabo/bobobeads_server/conf"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type contextKey struct{}

// Init builds the global logger from config. Production emits JSON to stdout so
// that a log collector can parse it; development keeps the console encoder.
func Init() {
	cfg := conf.GlobalConfig.Log

	var zc zap.Config
	if conf.IsProd() {
		zc = zap.NewProductionConfig()
	} else {
		zc = zap.NewDevelopmentConfig()
		zc.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zc.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zc.OutputPaths = []string{"stdout"}
	zc.ErrorOutputPaths = []string{"stderr"}

	if level, err := zapcore.ParseLevel(cfg.Level); err == nil {
		zc.Level = zap.NewAtomicLevelAt(level)
	}

	l, err := zc.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		panic("failed to build logger: " + err.Error())
	}
	zap.ReplaceGlobals(l)
}

func Sync() {
	_ = zap.L().Sync()
}

// NewContext attaches a request-scoped logger so downstream code logs with the
// same trace_id without threading fields through every call.
func NewContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext returns the request-scoped logger, falling back to the global one
// for background tasks and startup code.
func FromContext(ctx context.Context) *zap.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(contextKey{}).(*zap.Logger); ok && l != nil {
			return l
		}
	}
	return zap.L()
}
