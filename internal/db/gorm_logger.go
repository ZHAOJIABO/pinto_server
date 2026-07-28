package db

import (
	"context"
	"errors"
	"time"

	"github.com/zhaojiabo/bobobeads_server/internal/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// zapGormLogger routes GORM output through zap so SQL lines are structured and
// carry the trace_id of the request that issued them.
type zapGormLogger struct {
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

func newGormLogger(level gormlogger.LogLevel, slowThreshold time.Duration) gormlogger.Interface {
	return &zapGormLogger{level: level, slowThreshold: slowThreshold}
}

func (l *zapGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *zapGormLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= gormlogger.Info {
		logger.FromContext(ctx).Sugar().Infof(msg, args...)
	}
}

func (l *zapGormLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= gormlogger.Warn {
		logger.FromContext(ctx).Sugar().Warnf(msg, args...)
	}
}

func (l *zapGormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= gormlogger.Error {
		logger.FromContext(ctx).Sugar().Errorf(msg, args...)
	}
}

func (l *zapGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("elapsed", elapsed),
	}
	log := logger.FromContext(ctx)

	switch {
	// ErrRecordNotFound is how the DAOs probe for existence, so it is not an error.
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error:
		log.Error("sql failed", append(fields, zap.Error(err))...)
	case l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		log.Warn("slow sql", fields...)
	case l.level >= gormlogger.Info:
		log.Debug("sql", fields...)
	}
}
