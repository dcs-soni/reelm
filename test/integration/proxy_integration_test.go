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

func setupTestProxy(t *testing.T, mode string, mockURL string) (*proxy.Handler, store.Store) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.Mode = mode
	cfg.CassetteDir = tempDir
	cfg.Providers = []config.Provider{
		{
			Name:       "openai",
			BaseURL:    mockURL,
			PathPrefix: "/v1",
		},
	}

	h := hasher.NewDefaultHasher()
	reg := providers.DefaultRegistry()
	handler := proxy.NewHandler(cfg, s, h, reg)

	return handler, s
}

func TestProxyRecordMode(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupTestProxy(t, config.ModeRecord, mockUpstream.URL())
	defer s.Close()

	reqBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Tell me a joke"}]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "RECORDED", rec.Header().Get("X-Reelm-Cache"))
	assert.NotEmpty(t, rec.Header().Get("X-Reelm-Hash"))
	assert.Equal(t, int64(1), mockUpstream.CallCount())

	// Verify cassette was saved to disk
	hash := rec.Header().Get("X-Reelm-Hash")
	cassette, err := s.LoadByHash(hash)
	require.NoError(t, err)
	assert.Equal(t, hash, cassette.Hash)
	assert.Contains(t, cassette.Response.Body, "deterministic mock completion")
}

func TestProxyReplayModeHitAndMiss(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupTestProxy(t, config.ModeReplay, mockUpstream.URL())
	defer s.Close()

	// 1. Replay Miss on unrecorded prompt
	missBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Unrecorded prompt"}]
	}`)
	reqMiss := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(missBody))
	recMiss := httptest.NewRecorder()
	handler.ServeHTTP(recMiss, reqMiss)

	assert.Equal(t, http.StatusNotFound, recMiss.Code)
	assert.Equal(t, "MISS", recMiss.Header().Get("X-Reelm-Cache"))
	assert.Equal(t, int64(0), mockUpstream.CallCount()) // Upstream must NOT be called

	// 2. Pre-seed cassette into store
	h := hasher.NewDefaultHasher()
	canon := providers.NewOpenAICanonicalizer()
	hitBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Seeded prompt"}]
	}`)
	reqHit := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(hitBody))
	canonic, err := canon.Canonicalize(reqHit, hitBody)
	require.NoError(t, err)
	hash, err := h.Hash(canonic)
	require.NoError(t, err)

	seedCassette := &store.Cassette{
		Version:   store.CurrentCassetteVersion,
		Hash:      hash,
		Provider:  "openai",
		Endpoint:  "/v1/chat/completions",
		Response: store.CassetteResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"choices":[{"message":{"content":"Seeded replay content"}}]}`,
		},
	}
	require.NoError(t, s.Save(seedCassette))

	// 3. Replay Hit
	reqHit = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(hitBody))
	recHit := httptest.NewRecorder()
	handler.ServeHTTP(recHit, reqHit)

	assert.Equal(t, http.StatusOK, recHit.Code)
	assert.Equal(t, "HIT", recHit.Header().Get("X-Reelm-Cache"))
	assert.Equal(t, hash, recHit.Header().Get("X-Reelm-Hash"))
	assert.Contains(t, recHit.Body.String(), "Seeded replay content")
	assert.Equal(t, int64(0), mockUpstream.CallCount()) // Still 0 calls to upstream!
}

func TestProxyAutoMode(t *testing.T) {
	mockUpstream := testutil.NewMockOpenAI()
	defer mockUpstream.Close()

	handler, s := setupTestProxy(t, config.ModeAuto, mockUpstream.URL())
	defer s.Close()

	promptBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Auto mode test question"}]
	}`)

	// Call 1: Misses cache -> proxies and records
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(promptBody))
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "RECORDED", rec1.Header().Get("X-Reelm-Cache"))
	assert.Equal(t, int64(1), mockUpstream.CallCount())

	// Call 2: Hits cache -> serves recorded cassette instantly without upstream call
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(promptBody))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "HIT", rec2.Header().Get("X-Reelm-Cache"))
	assert.Equal(t, int64(1), mockUpstream.CallCount()) // Upstream call count remains 1!
}

func TestProxyHealthCheck(t *testing.T) {
	handler, s := setupTestProxy(t, config.ModeAuto, "http://localhost")
	defer s.Close()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}
