// Package cli implements the SignalForge CLI commands.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/spf13/cobra"
)

// InitCmd represents the signalforge init command.
var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize SignalForge directory structure",
	Long: `Creates the ~/.signalforge directory structure with default configuration.

This command sets up the data directory, creates all required subdirectories,
and writes a default config.json file.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		force, _ := cmd.Flags().GetBool("force")

		signalForgeDir, err := config.GetSignalForgeDir()
		if err != nil {
			return fmt.Errorf("determine signalforge dir: %w", err)
		}

		// Check if already initialized
		configPath := filepath.Join(signalForgeDir, "config.json")
		if _, err := os.Stat(configPath); err == nil && !force {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "SignalForge is already initialized at %s\n", signalForgeDir); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Use --force to reinitialize (overwrites config.json)\n"); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			return nil
		}

		// Create directory structure
		dirs := config.DefaultDirStructure()
		for dir := range dirs {
			path := filepath.Join(signalForgeDir, dir)
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", dir, err)
			}
		}

		// Write default config
		cfg := config.DefaultConfig()
		if err := config.SaveConfig(signalForgeDir, cfg); err != nil {
			return fmt.Errorf("save default config: %w", err)
		}

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "SignalForge initialized at %s\n", signalForgeDir); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Default configuration written to %s\n", configPath); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

func init() {
	InitCmd.Flags().BoolP("force", "f", false, "Reinitialize even if already initialized")
}
