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
	"os"
	"time"

	"github.com/fmotalleb/go-tools/git"
	"github.com/fmotalleb/go-tools/log"
	"github.com/fmotalleb/go-tools/reloader"
	"github.com/spf13/cobra"

	"github.com/fmotalleb/bifrost/config"
)

var (
	dump  = false
	debug = false
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:     "bifrost",
	Short:   "Bifrost combines multiple network interfaces with weighting to distribute TCP traffic, effectively overriding default routing behavior, to achive maximum speed on multi connection scenarios.",
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
				if pErr := config.Parse(ctx, &cfg, configFile); pErr != nil {
					return pErr
				}
				// if sErr := server.Serve(ctx, cfg); sErr != nil {
				// 	return sErr
				// }
				return nil
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
