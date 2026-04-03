package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/fmotalleb/go-tools/log"
	"github.com/fmotalleb/go-tools/reloader"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fmotalleb/bifrost/config"
	"github.com/fmotalleb/bifrost/internal/metrics"
	"github.com/fmotalleb/bifrost/internal/proxy"
)

const defaultSOCKSListen = "127.0.0.1:1080"

var socksCmd = &cobra.Command{
	Use:   "socks",
	Short: "Run a SOCKS5 proxy that distributes outbound connections across interfaces",
	RunE: func(cmd *cobra.Command, _ []string) error {
		configFile, err := cmd.Flags().GetString("config")
		if err != nil {
			return err
		}
		socksListenRaw, err := cmd.Flags().GetString("socks")
		if err != nil {
			return err
		}
		socksFlagChanged := cmd.Flags().Changed("socks")

		ctx := context.Background()
		ctx, err = log.WithNewEnvLogger(ctx)
		if err != nil {
			return err
		}

		return reloader.WithOsSignal(
			ctx,
			func(ctx context.Context) error {
				logger := log.Of(ctx)
				var cfg config.Config
				if parseErr := config.Parse(ctx, &cfg, configFile); parseErr != nil {
					return parseErr
				}
				if validateErr := config.Validate(cfg); validateErr != nil {
					return fmt.Errorf("validate config: %w", validateErr)
				}
				switch {
				case socksFlagChanged:
					socksListen, parseErr := netip.ParseAddrPort(socksListenRaw)
					if parseErr != nil {
						return fmt.Errorf("invalid --socks value %q: %w", socksListenRaw, parseErr)
					}
					cfg.Socks.Listen = socksListen
				case cfg.Socks.Listen.IsValid():
					// use config value
				default:
					socksListen, parseErr := netip.ParseAddrPort(defaultSOCKSListen)
					if parseErr != nil {
						return fmt.Errorf("invalid default socks listen %q: %w", defaultSOCKSListen, parseErr)
					}
					cfg.Socks.Listen = socksListen
				}

				telemetry := proxy.NoopTelemetry
				if cfg.Metrics.IsValid() {
					ifaceNames := slices.Sorted(maps.Keys(cfg.IFaces))
					metricsServer, metricsErr := metrics.NewServer(cfg.Metrics, ifaceNames)
					if metricsErr != nil {
						return fmt.Errorf("create metrics server: %w", metricsErr)
					}
					telemetry = metricsServer.Telemetry()

					go func() {
						if serveErr := metricsServer.Serve(ctx); serveErr != nil &&
							!errors.Is(serveErr, context.Canceled) {
							logger.Warn("metrics server stopped with error", zap.Error(serveErr))
						}
					}()

					logger.Info("metrics server listening", zap.String("metrics", cfg.Metrics.String()))
				}

				server, serverErr := proxy.NewSOCKSServer(cfg, telemetry)
				if serverErr != nil {
					return fmt.Errorf("create socks server: %w", serverErr)
				}

				return server.Serve(ctx)
			},
			time.Minute,
		)
	},
}

func init() {
	rootCmd.AddCommand(socksCmd)
	socksCmd.Flags().StringP("config", "c", "", "config file (default: reading config from stdin)")
	socksCmd.Flags().String(
		"socks",
		"",
		"SOCKS5 listen address (host:port)",
	)
}
