package providers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderDetection(t *testing.T) {
	reg := providers.DefaultRegistry()

	// 1. Path-based detection
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	p1, c1, err := reg.DetectProvider(req1)
	require.NoError(t, err)
	assert.Equal(t, "openai", p1)
	assert.NotNil(t, c1)

	// 2. Header override
	req2 := httptest.NewRequest(http.MethodPost, "/custom/endpoint", nil)
	req2.Header.Set("X-Reelm-Provider", "openai")
	p2, c2, err := reg.DetectProvider(req2)
	require.NoError(t, err)
	assert.Equal(t, "openai", p2)
	assert.NotNil(t, c2)
}
