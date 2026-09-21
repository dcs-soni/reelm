package proxy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/dcs-soni/reelm/pkg/telemetry"
	"github.com/rs/zerolog"
)

// Server wraps http.Server with graceful lifecycle management.
type Server struct {
	cfg        *config.Config
	httpServer *http.Server
	store      store.Store
	logger     zerolog.Logger
}

// NewServer configures a new Server instance.
func NewServer(cfg *config.Config, s store.Store, h hasher.Hasher, reg *providers.Registry) *Server {
	logger := telemetry.RootLogger.With().Str("component", "server").Logger()

	handler := NewHandler(cfg, s, h, reg)

	// Build middleware chain
	var rootHandler http.Handler = handler
	rootHandler = RequestIDMiddleware(rootHandler)
	rootHandler = RecoveryMiddleware(logger, rootHandler)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Server{
		cfg:        cfg,
		httpServer: srv,
		store:      s,
		logger:     logger,
	}
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	s.logger.Info().
		Str("addr", s.cfg.ListenAddr).
		Str("mode", s.cfg.Mode).
		Str("cassette_dir", s.cfg.CassetteDir).
		Msg("Reelm proxy server starting")

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("proxy server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down proxy server...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("proxy server shutdown error: %w", err)
	}
	return nil
}
