package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Operating modes for Reelm
const (
	ModeRecord = "record" // Record all requests and upstream responses to cassettes
	ModeReplay = "replay" // Replay matching requests from cassettes; fail on cache miss
	ModeAuto   = "auto"   // Replay if cassette exists; record and proxy if not
)

// Cassette serialization formats
const (
	FormatYAML = "yaml"
	FormatJSON = "json"
)

// Config represents the complete runtime configuration for Reelm.
type Config struct {
	Mode           string          `mapstructure:"mode"`
	ListenAddr     string          `mapstructure:"listen_addr"`
	AdminAddr      string          `mapstructure:"admin_addr"`
	CassetteDir    string          `mapstructure:"cassette_dir"`
	CassetteFormat string          `mapstructure:"cassette_format"` // "yaml" or "json"
	DefaultTimeout time.Duration   `mapstructure:"default_timeout"`
	Providers      []Provider      `mapstructure:"providers"`
	Matching       MatchConfig     `mapstructure:"matching"`
	Streaming      StreamingConfig `mapstructure:"streaming"`
	TLS            TLSConfig       `mapstructure:"tls"`
	Log            LogConfig       `mapstructure:"log"`
}

// Provider defines upstream API provider endpoints and behavior.
type Provider struct {
	Name        string            `mapstructure:"name"`        // "openai", "anthropic", "gemini", "azure"
	BaseURL     string            `mapstructure:"base_url"`    // e.g. "https://api.openai.com"
	PathPrefix  string            `mapstructure:"path_prefix"` // optional routing prefix, e.g. "/v1"
	Headers     []string          `mapstructure:"headers"`     // headers to pass through to upstream
	StripAuth   bool              `mapstructure:"strip_auth"`  // remove Authorization/API keys in recorded cassettes
	ExtraConfig map[string]string `mapstructure:"extra_config"`
}

// MatchConfig controls request matching and fuzzy scoring heuristics.
type MatchConfig struct {
	FuzzyEnabled        bool     `mapstructure:"fuzzy_enabled"`
	SimilarityThreshold float64  `mapstructure:"similarity_threshold"` // 0.0 to 1.0 (default: 0.92)
	MaxCandidates       int      `mapstructure:"max_candidates"`       // default: 100
	StopWords           []string `mapstructure:"stop_words"`
}

// StreamingConfig controls SSE streaming behavior.
type StreamingConfig struct {
	ReplayMode string `mapstructure:"replay_mode"` // "instant" or "timed" (default: "instant")
}

// TLSConfig controls transparent HTTPS interception and certificate authority generation.
type TLSConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	CADir   string `mapstructure:"ca_dir"`
}

// LogConfig controls logging verbosity and format.
type LogConfig struct {
	Level  string `mapstructure:"level"`  // "debug", "info", "warn", "error"
	Format string `mapstructure:"format"` // "json", "console"
}

// DefaultConfig returns a production-ready default configuration.
func DefaultConfig() *Config {
	return &Config{
		Mode:           ModeAuto,
		ListenAddr:     ":8080",
		AdminAddr:      ":8081",
		CassetteDir:    "./cassettes",
		CassetteFormat: FormatYAML,
		DefaultTimeout: 120 * time.Second,
		Providers: []Provider{
			{
				Name:       "openai",
				BaseURL:    "https://api.openai.com",
				PathPrefix: "/v1",
				Headers: []string{
					"Authorization",
					"OpenAI-Organization",
					"OpenAI-Project",
				},
				StripAuth: true,
			},
			{
				Name:       "anthropic",
				BaseURL:    "https://api.anthropic.com",
				PathPrefix: "/v1/messages",
				Headers: []string{
					"x-api-key",
					"anthropic-version",
					"anthropic-beta",
				},
				StripAuth: true,
			},
			{
				Name:       "gemini",
				BaseURL:    "https://generativelanguage.googleapis.com",
				PathPrefix: "/v1beta",
				Headers: []string{
					"x-goog-api-key",
				},
				StripAuth: true,
			},
		},
		Matching: MatchConfig{
			FuzzyEnabled:        false,
			SimilarityThreshold: 0.92,
			MaxCandidates:       100,
		},
		Streaming: StreamingConfig{
			ReplayMode: "instant",
		},
		TLS: TLSConfig{
			Enabled: true,
			CADir:   "",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "console",
		},
	}
}

// Load loads configuration from an optional file path, environment variables, and defaults.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	cfg := DefaultConfig()

	v.SetDefault("mode", cfg.Mode)
	v.SetDefault("listen_addr", cfg.ListenAddr)
	v.SetDefault("admin_addr", cfg.AdminAddr)
	v.SetDefault("cassette_dir", cfg.CassetteDir)
	v.SetDefault("cassette_format", cfg.CassetteFormat)
	v.SetDefault("default_timeout", cfg.DefaultTimeout)
	v.SetDefault("matching.fuzzy_enabled", cfg.Matching.FuzzyEnabled)
	v.SetDefault("matching.similarity_threshold", cfg.Matching.SimilarityThreshold)
	v.SetDefault("matching.max_candidates", cfg.Matching.MaxCandidates)
	v.SetDefault("streaming.replay_mode", cfg.Streaming.ReplayMode)
	v.SetDefault("tls.enabled", cfg.TLS.Enabled)
	v.SetDefault("tls.ca_dir", cfg.TLS.CADir)
	v.SetDefault("log.level", cfg.Log.Level)
	v.SetDefault("log.format", cfg.Log.Format)

	v.SetEnvPrefix("REELM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
		v.SetConfigName("default")
		v.SetConfigType("yaml")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && cfgFile != "" {
			return nil, fmt.Errorf("failed to read config file %q: %w", cfgFile, err)
		}
	}

	var loaded Config
	if err := v.Unmarshal(&loaded); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	// Preserve default providers if none specified
	if len(loaded.Providers) == 0 {
		loaded.Providers = cfg.Providers
	}

	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &loaded, nil
}
