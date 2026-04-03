package cmd

import (
	"context"
	"fmt"

	"github.com/fmotalleb/go-tools/log"
	"github.com/spf13/cobra"

	"github.com/fmotalleb/bifrost/config"
)

const configFlagName = "config"

const configFlagDescription = "config file (default: reading config from stdin)"

func addConfigFlag(cmd *cobra.Command) {
	cmd.Flags().StringP(configFlagName, "c", "", configFlagDescription)
}

func commandContext() (context.Context, error) {
	return log.WithNewEnvLogger(context.Background())
}

func loadValidatedConfig(ctx context.Context, cmd *cobra.Command) (config.Config, error) {
	configFile, err := cmd.Flags().GetString(configFlagName)
	if err != nil {
		return config.Config{}, err
	}

	var cfg config.Config
	if err := config.Parse(ctx, &cfg, configFile); err != nil {
		return config.Config{}, err
	}
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}
