package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/storage"
	"github.com/spf13/cobra"
)

// checkResult represents the result of a single doctor check.
type checkResult struct {
	Name   string
	Status string // ✅, ❌, ⚠️
	Detail string
}

// DoctorCmd represents the signalforge doctor command.
var DoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check SignalForge configuration and environment",
	Long: `Runs diagnostic checks on the SignalForge installation:
- Directory existence and permissions
- Config file validity
- Required environment variables
- Storage integrity`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		verbose, _ := cmd.Flags().GetBool("verbose")
		results := runChecks(verbose)

		allPassed := true
		for _, r := range results {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s", r.Status, r.Name)
			if r.Detail != "" {
				fmt.Fprintf(cmd.OutOrStdout(), ": %s", r.Detail)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			if r.Status == "❌" {
				allPassed = false
			}
		}

		fmt.Fprintln(cmd.OutOrStdout())
		if allPassed {
			fmt.Fprintln(cmd.OutOrStdout(), "All checks passed!")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Some checks failed. Review the issues above.")
		os.Exit(1)
		return nil
	},
}

func init() {
	DoctorCmd.Flags().BoolP("verbose", "v", false, "Show detailed check output")
}

func runChecks(verbose bool) []checkResult {
	var results []checkResult
	var cfg *config.Config

	// 1. Check signalforge directory
	results = append(results, checkSignalForgeDir())

	// 2. Check config.json
	results = append(results, checkConfig())
	if dir, err := config.GetSignalForgeDir(); err == nil {
		loadedCfg, loadErr := config.LoadConfig(dir)
		if loadErr == nil {
			cfg = loadedCfg
		}
	}

	// 3. Check directory structure
	results = append(results, checkDirectoryStructure())

	// 4. Check environment variables
	results = append(results, checkEnvVars(cfg)...)

	// 5. Check memory.json
	results = append(results, checkMemory())

	if verbose {
		// Add storage path details
		dir, err := config.GetSignalForgeDir()
		if err == nil {
			results = append(results, checkResult{
				Name:   "signalforge data directory",
				Status: "ℹ️",
				Detail: dir,
			})
		}
	}

	return results
}

func checkSignalForgeDir() checkResult {
	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return checkResult{
			Name:   "signalforge data directory",
			Status: "❌",
			Detail: fmt.Sprintf("Cannot determine home directory: %v", err),
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return checkResult{
				Name:   "signalforge data directory",
				Status: "⚠️",
				Detail: fmt.Sprintf("Not initialized yet (run 'signalforge init'): %s", dir),
			}
		}
		return checkResult{
			Name:   "signalforge data directory",
			Status: "❌",
			Detail: fmt.Sprintf("Cannot access: %v", err),
		}
	}
	if !info.IsDir() {
		return checkResult{
			Name:   "signalforge data directory",
			Status: "❌",
			Detail: fmt.Sprintf("Not a directory: %s", dir),
		}
	}
	// Check writability
	testFile := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(testFile, []byte{}, 0o600); err != nil {
		return checkResult{
			Name:   "signalforge data directory",
			Status: "❌",
			Detail: fmt.Sprintf("Not writable: %v", err),
		}
	}
	_ = os.Remove(testFile)
	return checkResult{
		Name:   "signalforge data directory",
		Status: "✅",
		Detail: dir,
	}
}

func checkConfig() checkResult {
	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return checkResult{
			Name:   "config.json",
			Status: "⚠️",
			Detail: "Cannot determine directory",
		}
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return checkResult{
			Name:   "config.json",
			Status: "❌",
			Detail: fmt.Sprintf("Invalid or unreadable: %v", err),
		}
	}
	if cfg == nil {
		return checkResult{
			Name:   "config.json",
			Status: "⚠️",
			Detail: "Using defaults (file not found)",
		}
	}
	return checkResult{
		Name:   "config.json",
		Status: "✅",
		Detail: "Valid configuration",
	}
}

func checkDirectoryStructure() checkResult {
	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return checkResult{
			Name:   "directory structure",
			Status: "⚠️",
			Detail: "Cannot determine directory",
		}
	}
	required := config.DefaultDirStructure()
	missing := []string{}
	for d := range required {
		path := filepath.Join(dir, d)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		return checkResult{
			Name:   "directory structure",
			Status: "⚠️",
			Detail: fmt.Sprintf("Missing directories: %s", strings.Join(missing, ", ")),
		}
	}
	return checkResult{
		Name:   "directory structure",
		Status: "✅",
		Detail: "All directories present",
	}
}

func checkEnvVars(cfg *config.Config) []checkResult {
	var results []checkResult

	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	if cfg.Sources.GitHub.Enabled {
		if os.Getenv("GITHUB_TOKEN") != "" {
			results = append(results, checkResult{
				Name:   "GITHUB_TOKEN",
				Status: "✅",
				Detail: "Set",
			})
		} else {
			results = append(results, checkResult{
				Name:   "GITHUB_TOKEN",
				Status: "❌",
				Detail: "Not set (required for GitHub collection)",
			})
		}
	} else {
		results = append(results, checkResult{
			Name:   "GITHUB_TOKEN",
			Status: "ℹ️",
			Detail: "Not required while GitHub collection is disabled",
		})
	}

	// Optional
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		results = append(results, checkResult{
			Name:   "OPENROUTER_API_KEY",
			Status: "✅",
			Detail: "Set",
		})
	} else {
		results = append(results, checkResult{
			Name:   "OPENROUTER_API_KEY",
			Status: "⚠️",
			Detail: "Not set (required for classification, clustering, and generation)",
		})
	}

	return results
}

func checkMemory() checkResult {
	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return checkResult{
			Name:   "memory.json",
			Status: "⚠️",
			Detail: "Cannot determine directory",
		}
	}
	memoryPath := filepath.Join(dir, "memory.json")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		return checkResult{
			Name:   "memory.json",
			Status: "⚠️",
			Detail: "Not found (will be created on first run)",
		}
	}
	store := storage.New(dir)
	var memData map[string]any
	if err := store.LoadJSON(memoryPath, &memData); err != nil {
		return checkResult{
			Name:   "memory.json",
			Status: "❌",
			Detail: fmt.Sprintf("Corrupt or unreadable: %v", err),
		}
	}
	return checkResult{
		Name:   "memory.json",
		Status: "✅",
		Detail: "Valid",
	}
}
