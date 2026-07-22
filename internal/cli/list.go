package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/storage"
)

// Valid types for list/show commands.
var validTypes = map[string]string{
	"signals":  "raw-signals",
	"clusters": "clusters",
	"jobs":     "jobs",
	"ideas":    "ideas",
	"runs":     "runs",
}

// ListCmd represents the signalforge list command.
var ListCmd = &cobra.Command{
	Use:   "list <type>",
	Short: "List items from storage",
	Long: `Lists items stored in the SignalForge data directory.

Supported types: signals, clusters, jobs, ideas, runs, all

Use 'signalforge list all' to show everything.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		itemType := args[0]
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		dir, err := config.GetSignalForgeDir()
		if err != nil {
			return fmt.Errorf("determine signalforge dir: %w", err)
		}
		store := storage.New(dir)

		if itemType == "all" {
			return listAll(cmd, store, limit, offset)
		}

		subDir, ok := validTypes[itemType]
		if !ok {
			return fmt.Errorf("unsupported type: %s (supported: signals, clusters, jobs, ideas, runs, all)", itemType)
		}

		return listType(cmd, store, itemType, subDir, limit, offset)
	},
}

func init() {
	ListCmd.Flags().Int("limit", 50, "Maximum number of items to show")
	ListCmd.Flags().Int("offset", 0, "Number of items to skip")
}

func listAll(cmd *cobra.Command, store *storage.Storage, limit, offset int) error {
	anyItems := false
	for typeName, subDir := range validTypes {
		items, err := listItems(store, subDir, limit, offset)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: cannot list %s: %v\n", typeName, err)
			continue
		}
		if len(items) > 0 {
			anyItems = true
			fmt.Fprintf(cmd.OutOrStdout(), "=== %s ===\n", strings.ToUpper(typeName))
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", item)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}
	if !anyItems {
		fmt.Fprintln(cmd.OutOrStdout(), "No items found in storage.")
	}
	return nil
}

func listType(cmd *cobra.Command, store *storage.Storage, typeName, subDir string, limit, offset int) error {
	items, err := listItems(store, subDir, limit, offset)
	if err != nil {
		return fmt.Errorf("list %s: %w", typeName, err)
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No %s found.\n", typeName)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", strings.Title(typeName))
	for _, item := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", item)
	}
	return nil
}

func listItems(store *storage.Storage, subDir string, limit, offset int) ([]string, error) {
	files, err := store.ListFiles(subDir, ".json")
	if err != nil {
		return nil, err
	}

	// Apply offset and limit
	start := offset
	if start > len(files) {
		start = len(files)
	}
	end := start + limit
	if end > len(files) {
		end = len(files)
	}
	files = files[start:end]

	var items []string
	for _, f := range files {
		name := filepath.Base(f)
		// Try to read basic info from the file
		info, err := os.Stat(f)
		if err != nil {
			items = append(items, fmt.Sprintf("%s (unreadable)", name))
			continue
		}
		items = append(items, fmt.Sprintf("%s  (modified: %s, size: %d bytes)",
			strings.TrimSuffix(name, ".json"),
			info.ModTime().Format(time.RFC3339),
			info.Size(),
		))
	}
	return items, nil
}