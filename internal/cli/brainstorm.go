package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// BrainstormCmd represents the signalforge brainstorm command (stub).
var BrainstormCmd = &cobra.Command{
	Use:   "brainstorm",
	Short: "Brainstorm product ideas (not implemented in MVP)",
	Long: `Brainstorms product ideas based on collected signals and clusters.

This command is not yet implemented in the MVP. It will be available in a future milestone.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "Not implemented in MVP. The brainstorm command will be available in a future milestone.")
		return nil
	},
}