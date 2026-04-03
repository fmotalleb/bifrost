package cmd

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/fmotalleb/go-tools/log"
	"github.com/fmotalleb/go-tools/reloader"
	"github.com/spf13/cobra"

	"github.com/fmotalleb/bifrost/internal/proxy"
)

const defaultSOCKSListen = "127.0.0.1:1080"

var socksCmd = &cobra.Command{
	Use:   "socks",
	Short: "Run a SOCKS5 proxy that distributes outbound connections across interfaces",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, err := commandContext()
		if err != nil {
			return err
		}

		socksListenRaw, err := cmd.Flags().GetString("socks")
		if err != nil {
			return err
		}
		socksFlagChanged := cmd.Flags().Changed("socks")

		return reloader.WithOsSignal(
			ctx,
			func(ctx context.Context) error {
				logger := log.Of(ctx)
				cfg, err := loadValidatedConfig(ctx, cmd)
				if err != nil {
					return err
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

				var telemetry proxy.Telemetry
				if telemetry, err = runMetricsServer(ctx, cfg, logger); err != nil {
					return err
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
	addConfigFlag(socksCmd)
	socksCmd.Flags().String(
		"socks",
		"",
		"SOCKS5 listen address (host:port)",
	)
}
