package proxy

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// RequestIDMiddleware injects or forwards an X-Reelm-Request-Id header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Reelm-Request-Id")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		w.Header().Set("X-Reelm-Request-Id", reqID)
		r.Header.Set("X-Reelm-Request-Id", reqID)
		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware catches any panics in downstream handlers and returns 502.
func RecoveryMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error().Interface("panic", rec).Msg("recovered from panic in proxy handler")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":{"message":"internal proxy error","type":"proxy_error"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
