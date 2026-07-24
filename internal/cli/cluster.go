// Package cli implements the SignalForge CLI commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/clustering"
	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/openrouter"
	"github.com/moontechs/signalforge/internal/storage"
)

// ClusterCmd represents the signalforge cluster command.
var ClusterCmd = newClusterCmd()

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster related problem signals",
		Long: `Groups related problem signals into clusters using Jaccard similarity,
entity matching, and canonical action matching. Edge cases use OpenRouter
for semantic boundary checks.

Example:
  signalforge cluster
  signalforge cluster --threshold 0.4
  signalforge cluster --dry-run`,
		RunE: runCluster,
	}

	cmd.Flags().Float64("threshold", 0.3, "Jaccard similarity threshold (0.0-1.0)")
	cmd.Flags().Bool("dry-run", false, "Print clustering plan and exit")
	cmd.Flags().Int("limit", 0, "Maximum number of clusters to create")
	cmd.Flags().Bool("no-semantic", false, "Skip OpenRouter semantic checks for edge cases")

	return cmd
}

type clusterEnv struct {
	store      *storage.Storage
	mem        *memory.DefaultMemory
	cfg        *config.Config
	threshold  float64
	dryRun     bool
	limit      int
	noSemantic bool
}

func runCluster(cmd *cobra.Command, _ []string) error {
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	limit, _ := cmd.Flags().GetInt("limit")
	noSemantic, _ := cmd.Flags().GetBool("no-semantic")

	if threshold < 0 || threshold > 1.0 {
		return errors.New("--threshold must be between 0.0 and 1.0")
	}
	if limit < 0 {
		return errors.New("--limit must be a non-negative integer")
	}

	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return fmt.Errorf("determine signalforge dir: %w", err)
	}

	if err := ensureStorageLayout(dir); err != nil {
		return fmt.Errorf("initialize storage layout: %w", err)
	}

	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store := storage.New(dir)
	mem := memory.New(store)
	memoryPath := filepath.Join(dir, "memory.json")
	if store.Exists(memoryPath) {
		if err := mem.Load(); err != nil {
			return fmt.Errorf("load memory: %w", err)
		}
	}

	env := &clusterEnv{
		store:      store,
		mem:        mem,
		cfg:        cfg,
		threshold:  threshold,
		dryRun:     dryRun,
		limit:      limit,
		noSemantic: noSemantic,
	}

	return executeCluster(cmd, env)
}

func executeCluster(cmd *cobra.Command, env *clusterEnv) error {
	// Load problem signals from the problem-signals directory.
	problemSignals, err := loadProblemSignals(env.store)
	if err != nil {
		return fmt.Errorf("load problem signals: %w", err)
	}

	if len(problemSignals) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No problem signals found. Run 'signalforge classify' first.")
		return nil
	}

	// Filter to only IsProblemSignal == true.
	problemSignals = filterProblemSignals(problemSignals)

	if len(problemSignals) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No problem signals found (all signals are noise). Run 'signalforge classify' first.")
		return nil
	}

	// Build clustering config.
	clusterCfg := clustering.Config{
		JaccardThreshold: env.threshold,
		MaxClusters:      env.limit,
	}

	// Handle dry-run.
	if env.dryRun {
		printClusterDryRun(cmd, problemSignals, clusterCfg, env.noSemantic)
		return nil
	}

	var llmClient domain.LLMClient

	if !env.noSemantic {
		apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		if apiKey != "" {
			orClient, err := openrouter.New(env.cfg.OpenRouter, apiKey)
			if err == nil {
				llmClient = orClient
			}
		}
	}

	// Create clusterer.
	clusterer := clustering.New(clusterCfg, llmClient, env.store)

	// Run clustering.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	clusters, err := clusterer.Cluster(ctx, problemSignals)
	if err != nil {
		return fmt.Errorf("cluster signals: %w", err)
	}

	// Save each cluster.
	savedCount := 0
	for i := range clusters {
		cluster := clusters[i]
		filename := cluster.ID + ".json"
		clusterPath := filepath.Join(env.store.BaseDir(), "clusters", filename)

		if err := env.store.SaveJSON(clusterPath, cluster); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to save cluster %s: %v\n", cluster.ID, err)
			continue
		}

		savedCount++

		// Update memory.
		env.mem.IncrementStat("clusters_created")

		// Build a fingerprint from the cluster's signal IDs for dedup.
		fp := buildClusterFingerprint(cluster.SignalIDs)
		env.mem.AddClusterFingerprint(fp, cluster.ID)
	}

	// Save memory atomically.
	if err := env.mem.Save(); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	// Print summary.
	printClusterSummary(cmd, len(problemSignals), len(clusters), savedCount)

	return nil
}

// loadProblemSignals loads all problem signal files from the problem-signals directory.
func loadProblemSignals(store *storage.Storage) ([]domain.ProblemSignal, error) {
	files, err := store.ListFiles("problem-signals", ".json")
	if err != nil {
		return nil, fmt.Errorf("list problem signal files: %w", err)
	}

	signals := make([]domain.ProblemSignal, 0, len(files))
	for _, path := range files {
		var signal domain.ProblemSignal
		if err := store.LoadJSON(path, &signal); err != nil {
			// Log and continue with other signals.
			continue
		}
		signals = append(signals, signal)
	}

	return signals, nil
}

// filterProblemSignals returns only signals with IsProblemSignal == true.
func filterProblemSignals(signals []domain.ProblemSignal) []domain.ProblemSignal {
	filtered := make([]domain.ProblemSignal, 0, len(signals))
	for _, s := range signals {
		if s.IsProblemSignal {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// buildClusterFingerprint creates a deterministic fingerprint from a set of signal IDs.
func buildClusterFingerprint(signalIDs []string) string {
	return strings.Join(signalIDs, ",")
}

// printClusterDryRun prints the clustering plan for dry-run mode.
func printClusterDryRun(cmd *cobra.Command, signals []domain.ProblemSignal, cfg clustering.Config, noSemantic bool) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintf(w, "=== Clustering Plan (dry-run) ===\n")
	_, _ = fmt.Fprintf(w, "  Problem signals to cluster: %d\n", len(signals))
	_, _ = fmt.Fprintf(w, "  Jaccard threshold: %.2f\n", cfg.JaccardThreshold)
	_, _ = fmt.Fprintf(w, "  Max clusters: %d\n", cfg.MaxClusters)
	_, _ = fmt.Fprintf(w, "  Semantic checks: %s\n", map[bool]string{true: "disabled", false: "enabled"}[noSemantic])

	_, _ = fmt.Fprintln(w, "\n(dry-run) No API calls were made. No data was persisted.")
}

// printClusterSummary prints the clustering results summary.
func printClusterSummary(cmd *cobra.Command, totalSignals, clusterCount, savedCount int) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(w, "=== Clustering Summary ===")
	_, _ = fmt.Fprintf(w, "  Problem signals: %d\n", totalSignals)
	_, _ = fmt.Fprintf(w, "  Clusters created: %d\n", clusterCount)
	_, _ = fmt.Fprintf(w, "  Saved: %d\n", savedCount)
}
