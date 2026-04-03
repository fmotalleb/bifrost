/*
Copyright © 2025 Motalleb Fallahnezhad (fmotalleb@gmail.com)

This program is free software; you can redistribute it and/or
modify it under the terms of the GNU General Public License
as published by the Free Software Foundation; either version 2
of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/

package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/fmotalleb/go-tools/git"
	"github.com/fmotalleb/go-tools/log"
	"github.com/fmotalleb/go-tools/reloader"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fmotalleb/bifrost/config"
	"github.com/fmotalleb/bifrost/internal/metrics"
	"github.com/fmotalleb/bifrost/internal/proxy"
)

var debug = false

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:     "bifrost",
	Short:   "Bifrost combines multiple network interfaces with weighting to distribute TCP traffic, effectively overriding default routing behavior, to achieve maximum speed on multi connection scenarios.",
	Version: git.String(),
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if debug {
			log.SetDebugDefaults()
		}
	},
	RunE: func(cmd *cobra.Command, _ []string) error {
		var configFile string
		var err error
		var cfg config.Config
		if configFile, err = cmd.Flags().GetString("config"); err != nil {
			return err
		}
		ctx := context.Background()
		ctx, err = log.WithNewEnvLogger(ctx)
		if err != nil {
			return err
		}
		// defer cancel()
		err = reloader.WithOsSignal(
			ctx,
			func(ctx context.Context) error {
				logger := log.Of(ctx)
				if pErr := config.Parse(ctx, &cfg, configFile); pErr != nil {
					return pErr
				}
				if vErr := config.Validate(cfg); vErr != nil {
					return fmt.Errorf("validate config: %w", vErr)
				}
				var telemetry proxy.Telemetry
				if telemetry, err = runMetricsServer(ctx, cfg, logger); err != nil {
					return err
				}

				srv, sErr := proxy.NewServer(cfg, telemetry)
				if sErr != nil {
					return fmt.Errorf("create server: %w", sErr)
				}

				return srv.Serve(ctx)
			},
			time.Minute,
		)
		return err
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// init initializes command-line flags for the root command, including configuration file path, format, debug mode, and dry-run options.
func init() {
	rootCmd.Flags().StringP("config", "c", "", "config file (default: reading config from stdin)")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "enable debug mode")
}

func runMetricsServer(ctx context.Context, cfg config.Config, logger *zap.Logger) (proxy.Telemetry, error) {
	telemetry := proxy.NoopTelemetry
	if cfg.Metrics.IsValid() {
		ifaceNames := slices.Sorted(maps.Keys(cfg.IFaces))
		metricsServer, mErr := metrics.NewServer(cfg.Metrics, ifaceNames)
		if mErr != nil {
			return nil, fmt.Errorf("create metrics server: %w", mErr)
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
	return telemetry, nil
}
