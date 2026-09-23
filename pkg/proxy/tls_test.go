package proxy_test

import (
	"testing"

	"github.com/dcs-soni/reelm/pkg/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCAManagerGenerationAndSigning(t *testing.T) {
	tempDir := t.TempDir()

	mgr, err := proxy.NewCAManager(tempDir)
	require.NoError(t, err)
	require.NotNil(t, mgr)

	// 1. Sign host certificate for api.openai.com
	cert1, err := mgr.GetHostCertificate("api.openai.com:443")
	require.NoError(t, err)
	require.NotNil(t, cert1)
	assert.NotEmpty(t, cert1.Certificate)

	// 2. Requesting same host should hit memory cache
	cert2, err := mgr.GetHostCertificate("api.openai.com")
	require.NoError(t, err)
	assert.Same(t, cert1, cert2, "Repeated calls for the same host should return cached certificate pointer")

	// 3. Different host should generate new cert
	certAnthropic, err := mgr.GetHostCertificate("api.anthropic.com")
	require.NoError(t, err)
	assert.NotNil(t, certAnthropic)
	assert.NotSame(t, cert1, certAnthropic)

	// 4. Reload existing CA from disk
	mgr2, err := proxy.NewCAManager(tempDir)
	require.NoError(t, err)
	require.NotNil(t, mgr2)
}
