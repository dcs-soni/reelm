package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/dcs-soni/reelm/pkg/proxy"
	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/dcs-soni/reelm/pkg/telemetry"
	"github.com/dcs-soni/reelm/pkg/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	modeFlag  string
	portFlag  string
	dirFlag   string
	fmtFlag   string
	levelFlag string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "reelm",
		Short: "Reelm — Record & Replay proxy for LLM API calls",
		Long: `Reelm is a production-grade, language-agnostic HTTP proxy that records
LLM API interactions to cassettes and replays them deterministically in CI/CD pipelines.`,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to configuration file (default: configs/default.yaml)")
	rootCmd.PersistentFlags().StringVar(&levelFlag, "log-level", "", "logging level (debug, info, warn, error)")

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Reelm HTTP proxy server",
		RunE:  runServe,
	}

	serveCmd.Flags().StringVarP(&modeFlag, "mode", "m", "", "operating mode: record, replay, or auto")
	serveCmd.Flags().StringVarP(&portFlag, "port", "p", "", "listen address/port (e.g. :8080)")
	serveCmd.Flags().StringVarP(&dirFlag, "cassette-dir", "d", "", "directory to store cassettes (default: ./cassettes)")
	serveCmd.Flags().StringVar(&fmtFlag, "cassette-format", "", "cassette format: yaml or json")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print Reelm version and build information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Reelm %s (commit: %s, built: %s, go: %s)\n",
				version.Version, version.GitCommit, version.BuildDate, runtime.Version())
		},
	}

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	// 1. Load configuration
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// 2. Apply CLI flag overrides
	if modeFlag != "" {
		cfg.Mode = modeFlag
	}
	if portFlag != "" {
		if portFlag[0] != ':' {
			cfg.ListenAddr = ":" + portFlag
		} else {
			cfg.ListenAddr = portFlag
		}
	}
	if dirFlag != "" {
		cfg.CassetteDir = dirFlag
	}
	if fmtFlag != "" {
		cfg.CassetteFormat = fmtFlag
	}
	if levelFlag != "" {
		cfg.Log.Level = levelFlag
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// 3. Initialize logger
	logger := telemetry.InitLogger(cfg.Log)
	logger.Info().
		Str("version", version.Version).
		Str("mode", cfg.Mode).
		Str("port", cfg.ListenAddr).
		Str("cassette_dir", cfg.CassetteDir).
		Str("format", cfg.CassetteFormat).
		Msg("initializing Reelm proxy")

	// 4. Initialize cassette store
	diskStore, err := store.NewDiskStore(cfg.CassetteDir, cfg.CassetteFormat)
	if err != nil {
		return fmt.Errorf("failed to initialize cassette store: %w", err)
	}
	defer diskStore.Close()

	// 5. Initialize hasher & provider registry
	h := hasher.NewDefaultHasher()
	reg := providers.DefaultRegistry()

	// 6. Build and start proxy server
	srv := proxy.NewServer(cfg, diskStore, h, reg)

	// Channel for OS shutdown signals
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	select {
	case err := <-serverErr:
		return err
	case sig := <-stopChan:
		logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("error during server shutdown")
			return err
		}
		logger.Info().Msg("Reelm stopped gracefully")
		return nil
	}
}
