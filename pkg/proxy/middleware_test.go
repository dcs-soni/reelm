package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dcs-soni/reelm/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestRequestIDMiddleware(t *testing.T) {
	// 1. Without existing request ID -> generates new one
	nextCalled := false
	handler := proxy.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		reqID := r.Header.Get("X-Reelm-Request-Id")
		assert.NotEmpty(t, reqID)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, nextCalled)
	assert.NotEmpty(t, rec.Header().Get("X-Reelm-Request-Id"))

	// 2. With existing request ID -> preserves it
	nextCalled = false
	existingID := "custom-uuid-12345"
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("X-Reelm-Request-Id", existingID)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.True(t, nextCalled)
	assert.Equal(t, existingID, rec2.Header().Get("X-Reelm-Request-Id"))
}

func TestRecoveryMiddleware(t *testing.T) {
	logger := zerolog.Nop()
	panickingHandler := proxy.RecoveryMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("unexpected critical failure")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		panickingHandler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal proxy error")
}
