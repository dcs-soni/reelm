//go:build integration

package integration_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dcs-soni/reelm/internal/testutil"
	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/dcs-soni/reelm/pkg/proxy"
	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMultiProviderTestProxy(t *testing.T, mode string, mockURL string) (*proxy.Handler, store.Store) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.Mode = mode
	cfg.CassetteDir = tempDir
	cfg.Providers = []config.Provider{
		{Name: "openai", BaseURL: mockURL},
		{Name: "anthropic", BaseURL: mockURL},
		{Name: "gemini", BaseURL: mockURL},
	}

	h := hasher.NewDefaultHasher()
	reg := providers.DefaultRegistry()
	handler := proxy.NewHandler(cfg, s, h, reg)

	return handler, s
}

func TestAnthropicRecordAndReplay(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupMultiProviderTestProxy(t, config.ModeAuto, mockUpstream.URL())
	defer s.Close()

	reqBody := []byte(`{
		"model": "claude-3-5-sonnet",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "Hello Anthropic Claude"}]
	}`)

	// 1. Record
	req1 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(reqBody))
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "RECORDED", rec1.Header().Get("X-Reelm-Cache"))
	assert.Contains(t, rec1.Body.String(), "Hello from mock Anthropic Claude!")
	assert.Equal(t, int64(1), mockUpstream.CallCount())

	// 2. Replay
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(reqBody))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Reelm-Cache"))
	assert.Contains(t, rec2.Body.String(), "Hello from mock Anthropic Claude!")
	assert.Equal(t, int64(1), mockUpstream.CallCount()) // Upstream call count remains 1
}

func TestGeminiRecordAndReplay(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupMultiProviderTestProxy(t, config.ModeAuto, mockUpstream.URL())
	defer s.Close()

	reqBody := []byte(`{
		"contents": [{"parts": [{"text": "Hello Gemini"}]}]
	}`)

	// 1. Record
	req1 := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-1.5-flash:generateContent", bytes.NewReader(reqBody))
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "RECORDED", rec1.Header().Get("X-Reelm-Cache"))
	assert.Contains(t, rec1.Body.String(), "Hello from mock Google Gemini!")
	assert.Equal(t, int64(1), mockUpstream.CallCount())

	// 2. Replay
	req2 := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-1.5-flash:generateContent", bytes.NewReader(reqBody))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Reelm-Cache"))
	assert.Equal(t, int64(1), mockUpstream.CallCount())
}

func TestStreamingRecordAndReplay(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupMultiProviderTestProxy(t, config.ModeAuto, mockUpstream.URL())
	defer s.Close()

	reqBody := []byte(`{
		"model": "gpt-4o",
		"stream": true,
		"messages": [{"role": "user", "content": "Stream me a response"}]
	}`)

	// 1. First call: Record SSE stream
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "RECORDED", rec1.Header().Get("X-Reelm-Cache"))
	assert.Contains(t, rec1.Header().Get("Content-Type"), "text/event-stream")
	assert.Contains(t, rec1.Body.String(), "chatcmpl-stream-1")
	assert.Contains(t, rec1.Body.String(), "[DONE]")
	assert.Equal(t, int64(1), mockUpstream.CallCount())

	// Verify cassette was saved as streaming with chunks
	hash := rec1.Header().Get("X-Reelm-Hash")
	cassette, err := s.LoadByHash(hash)
	require.NoError(t, err)
	assert.True(t, cassette.Response.IsStream)
	assert.NotEmpty(t, cassette.Response.Chunks)

	// 2. Second call: Replay SSE stream
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Reelm-Cache"))
	assert.Contains(t, rec2.Header().Get("Content-Type"), "text/event-stream")
	assert.Contains(t, rec2.Body.String(), "chatcmpl-stream-1")
	assert.Contains(t, rec2.Body.String(), "[DONE]")
	assert.Equal(t, int64(1), mockUpstream.CallCount()) // Upstream was not called!
}
