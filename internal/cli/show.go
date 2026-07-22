package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/moontechs/signalforge/internal/config"
)

// ShowCmd represents the signalforge show command.
var ShowCmd = &cobra.Command{
	Use:   "show <type> <id>",
	Short: "Show details of a specific item",
	Long: `Displays the full details of a stored item.

Supported types: signals, clusters, jobs, ideas, runs

Example:
  signalforge show signals abc123
  signalforge show clusters def456 --json`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		itemType := args[0]
		itemID := args[1]
		asJSON, _ := cmd.Flags().GetBool("json")

		subDir, ok := validTypes[itemType]
		if !ok {
			return fmt.Errorf("unsupported type: %s (supported: signals, clusters, jobs, ideas, runs)", itemType)
		}

		dir, err := config.GetSignalForgeDir()
		if err != nil {
			return fmt.Errorf("determine signalforge dir: %w", err)
		}

		// Look for the file
		searchDir := filepath.Join(dir, subDir)
		var foundFile string

		entries, err := os.ReadDir(searchDir)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("item not found: %s/%s", itemType, itemID)
			}
			return fmt.Errorf("read directory: %w", err)
		}

		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), itemID) && strings.HasSuffix(e.Name(), ".json") {
				foundFile = filepath.Join(searchDir, e.Name())
				break
			}
		}

		if foundFile == "" {
			return fmt.Errorf("item not found: %s/%s", itemType, itemID)
		}

		// Read and display the file
		data, err := os.ReadFile(foundFile)
		if err != nil {
			return fmt.Errorf("read item: %w", err)
		}

		if asJSON {
			// Pretty-print JSON
			var prettyData any
			if err := json.Unmarshal(data, &prettyData); err != nil {
				// Not valid JSON, print raw
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			pretty, _ := json.MarshalIndent(prettyData, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
			return nil
		}

		// Parse and display as key-value pairs
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			// Raw display
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "=== %s: %s ===\n", strings.ToUpper(itemType), itemID)
		for k, v := range fields {
			switch val := v.(type) {
			case string:
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, val)
			case float64:
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %v\n", k, val)
			case bool:
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %v\n", k, val)
			default:
				// Skip complex nested objects in non-JSON mode
				jsonVal, _ := json.Marshal(val)
				if len(jsonVal) > 500 {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: [complex data, use --json to view]\n", k)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, string(jsonVal))
				}
			}
		}

		return nil
	},
}

func init() {
	ShowCmd.Flags().Bool("json", false, "Output raw JSON")
}