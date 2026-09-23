package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.Equal(t, config.ModeAuto, cfg.Mode)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, config.FormatYAML, cfg.CassetteFormat)
	assert.Len(t, cfg.Providers, 3)
	assert.Equal(t, "openai", cfg.Providers[0].Name)
	assert.Equal(t, "anthropic", cfg.Providers[1].Name)
	assert.Equal(t, "gemini", cfg.Providers[2].Name)
	assert.Equal(t, "instant", cfg.Streaming.ReplayMode)
	assert.True(t, cfg.TLS.Enabled)
	assert.NoError(t, cfg.Validate())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(c *config.Config)
		wantErr string
	}{
		{
			name: "invalid mode",
			modify: func(c *config.Config) {
				c.Mode = "invalid"
			},
			wantErr: "invalid mode",
		},
		{
			name: "invalid cassette format",
			modify: func(c *config.Config) {
				c.CassetteFormat = "xml"
			},
			wantErr: "invalid cassette_format",
		},
		{
			name: "empty listen address",
			modify: func(c *config.Config) {
				c.ListenAddr = ""
			},
			wantErr: "listen_addr cannot be empty",
		},
		{
			name: "empty providers",
			modify: func(c *config.Config) {
				c.Providers = nil
			},
			wantErr: "at least one provider must be defined",
		},
		{
			name: "invalid provider URL",
			modify: func(c *config.Config) {
				c.Providers = []config.Provider{
					{Name: "openai", BaseURL: "not-a-valid-url"},
				}
			},
			wantErr: "is not a valid absolute URL",
		},
		{
			name: "invalid similarity threshold",
			modify: func(c *config.Config) {
				c.Matching.SimilarityThreshold = 1.5
			},
			wantErr: "similarity_threshold must be between 0.0 and 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "test_config.yaml")

	yamlContent := `
mode: replay
listen_addr: ":9090"
cassette_dir: "./custom_cassettes"
cassette_format: "json"
matching:
  fuzzy_enabled: true
  similarity_threshold: 0.85
`
	err := os.WriteFile(cfgPath, []byte(yamlContent), 0600)
	require.NoError(t, err)

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, config.ModeReplay, loaded.Mode)
	assert.Equal(t, ":9090", loaded.ListenAddr)
	assert.Equal(t, "./custom_cassettes", loaded.CassetteDir)
	assert.Equal(t, config.FormatJSON, loaded.CassetteFormat)
	assert.True(t, loaded.Matching.FuzzyEnabled)
	assert.Equal(t, 0.85, loaded.Matching.SimilarityThreshold)
	// Default providers preserved
	assert.Len(t, loaded.Providers, 3)
	assert.Equal(t, "openai", loaded.Providers[0].Name)
}
