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
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "Not implemented in MVP. The analyze command will be available in a future milestone.")
		return nil
	},
}