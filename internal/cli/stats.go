// Package cli implements the SignalForge CLI commands.
package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

// StatsCmd represents the signalforge stats command.
var StatsCmd = newStatsCmd()

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show research statistics from memory.json",
		Long: `Displays statistics about collected signals, classified problems, clusters,
jobs, solutions, and per-source API usage.

Example:
  signalforge stats
  signalforge stats --json`,
		RunE: runStats,
	}

	cmd.Flags().Bool("json", false, "Output stats as machine-readable JSON")

	return cmd
}

type statsEnv struct {
	store *storage.Storage
	mem   *memory.DefaultMemory
	json  bool
}

func runStats(cmd *cobra.Command, _ []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return fmt.Errorf("determine signalforge dir: %w", err)
	}

	if err := ensureStorageLayout(dir); err != nil {
		return fmt.Errorf("initialize storage layout: %w", err)
	}

	store := storage.New(dir)
	mem := memory.New(store)

	memoryPath := filepath.Join(dir, "memory.json")
	if !store.Exists(memoryPath) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No stats available. Run 'signalforge collect' first.")
		return nil
	}

	if err := mem.Load(); err != nil {
		return fmt.Errorf("load memory: %w", err)
	}

	env := &statsEnv{
		store: store,
		mem:   mem,
		json:  jsonOutput,
	}

	return executeStats(cmd, env)
}

func executeStats(cmd *cobra.Command, env *statsEnv) error {
	stats := env.mem.GetStats()

	if env.json {
		return printStatsJSON(cmd, stats)
	}

	printStatsHuman(cmd, stats)
	return nil
}

func printStatsHuman(cmd *cobra.Command, stats domain.ResearchStats) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(w, "=== Research Statistics ===")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "--- Collection ---")
	_, _ = fmt.Fprintf(w, "  Raw signals collected:     %d\n", stats.RawSignalsCollected)
	_, _ = fmt.Fprintf(w, "  Raw signals skipped:      %d\n", stats.RawSignalsSkipped)
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "--- Classification ---")
	_, _ = fmt.Fprintf(w, "  Problem signals found:    %d\n", stats.ProblemSignalsFound)
	_, _ = fmt.Fprintf(w, "  Noise signals:            %d\n", stats.NoiseSignals)
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "--- Clustering & Discovery ---")
	_, _ = fmt.Fprintf(w, "  Clusters created:         %d\n", stats.ClustersCreated)
	_, _ = fmt.Fprintf(w, "  Jobs created:             %d\n", stats.JobsCreated)
	_, _ = fmt.Fprintf(w, "  Ideas created:            %d\n", stats.IdeasCreated)
	_, _ = fmt.Fprintf(w, "  Duplicate ideas:          %d\n", stats.DuplicateIdeas)
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "--- API Requests ---")
	_, _ = fmt.Fprintf(w, "  GitHub requests:          %d\n", stats.GitHubRequests)
	_, _ = fmt.Fprintf(w, "  Hacker News requests:     %d\n", stats.HackerNewsRequests)
	_, _ = fmt.Fprintf(w, "  Stack Exchange requests:  %d\n", stats.StackExchangeRequests)
	_, _ = fmt.Fprintf(w, "  Reddit requests:          %d\n", stats.RedditRequests)
	_, _ = fmt.Fprintf(w, "  SERP requests:            %d\n", stats.SERPRequests)
	_, _ = fmt.Fprintf(w, "  Unlocker requests:        %d\n", stats.UnlockerRequests)
	_, _ = fmt.Fprintf(w, "  LLM requests:             %d\n", stats.LLMRequests)
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "--- Cache Hits ---")
	_, _ = fmt.Fprintf(w, "  GitHub cache hits:        %d\n", stats.GitHubCacheHits)
	_, _ = fmt.Fprintf(w, "  Hacker News cache hits:   %d\n", stats.HackerNewsCacheHits)
	_, _ = fmt.Fprintf(w, "  Stack Exchange cache hits:%d\n", stats.StackExchangeCacheHits)
	_, _ = fmt.Fprintf(w, "  Reddit cache hits:        %d\n", stats.RedditCacheHits)
	_, _ = fmt.Fprintf(w, "  SERP cache hits:          %d\n", stats.SERPCacheHits)
	_, _ = fmt.Fprintf(w, "  Unlocker cache hits:      %d\n", stats.UnlockerCacheHits)
}

func printStatsJSON(cmd *cobra.Command, stats domain.ResearchStats) error {
	output := map[string]any{
		"stats": stats,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stats: %w", err)
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
