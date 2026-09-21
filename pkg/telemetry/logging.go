package telemetry

import (
	"os"
	"strings"
	"time"

	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// RootLogger is the package-level logger.
	RootLogger = log.Logger
)

// InitLogger configures the global logger based on configuration settings.
func InitLogger(cfg config.LogConfig) zerolog.Logger {
	var level zerolog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn", "warning":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	default:
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano

	if strings.ToLower(cfg.Format) == "console" {
		output := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05.000",
		}
		RootLogger = zerolog.New(output).With().Timestamp().Logger()
	} else {
		RootLogger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	log.Logger = RootLogger
	return RootLogger
}

// RequestLogger creates a contextualized child logger for an in-flight proxy request.
func RequestLogger(reqID, provider, endpoint, mode string) zerolog.Logger {
	return RootLogger.With().
		Str("req_id", reqID).
		Str("provider", provider).
		Str("endpoint", endpoint).
		Str("mode", mode).
		Logger()
}
