package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// AnalyzeCmd represents the signalforge analyze command (stub).
var AnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze collected signals (not implemented in MVP)",
	Long: `Analyzes collected raw signals to classify them as problem signals or noise.

This command is not yet implemented in the MVP. It will be available in a future milestone.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Not implemented in MVP. The analyze command will be available in a future milestone.")
		if err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}
