package middleware

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/zhaojiabo/bobobeads_server/internal/logger"
	"go.uber.org/zap"
)

const requestIDHeader = "X-Request-Id"

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// HTTPLogging logs one line per HTTP request and makes sure every request carries
// an X-Request-Id, so the gateway can forward it and the gRPC layer logs the same
// trace_id for the same request.
func HTTPLogging(slowThreshold time.Duration, clientIPResolver ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := r.Header.Get(requestIDHeader)
			if traceID == "" {
				traceID = uuid.NewString()
				r.Header.Set(requestIDHeader, traceID)
			}
			w.Header().Set(requestIDHeader, traceID)

			l := zap.L().With(zap.String("trace_id", traceID))
			r = r.WithContext(logger.NewContext(r.Context(), l))

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)

			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rec.status),
				zap.Int("bytes", rec.bytes),
				zap.Duration("elapsed", elapsed),
				zap.String("ip", clientIPResolver.Resolve(r)),
			}

			switch {
			case rec.status >= http.StatusInternalServerError:
				l.Error("http request failed", fields...)
			case rec.status >= http.StatusBadRequest:
				l.Warn("http request rejected", fields...)
			case slowThreshold > 0 && elapsed > slowThreshold:
				l.Warn("slow http request", fields...)
			default:
				l.Info("http request", fields...)
			}
		})
	}
}
