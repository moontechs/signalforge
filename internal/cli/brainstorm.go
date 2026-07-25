package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/discover"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/openrouter"
	"github.com/moontechs/signalforge/internal/storage"
)

// DiscoverResult is the persisted output of the discover command.
type DiscoverResult struct {
	JTBDs      []domain.JobToBeDone        `json:"jtbd"`
	Solutions  []domain.SolutionHypothesis `json:"solutions"`
	Duplicates []discover.Duplicate        `json:"duplicates,omitempty"`
}

// DiscoverCmd generates jobs-to-be-done and product hypotheses from clusters.
var DiscoverCmd = newDiscoverCmd()

func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "discover", Short: "Generate product hypotheses from problem clusters", RunE: runDiscover}
	cmd.Flags().Int("limit", 0, "Maximum number of clusters to process (0 = no limit)")
	cmd.Flags().Bool("dry-run", false, "Print the discovery plan without making API calls")
	cmd.Flags().Bool("no-semantic", false, "Skip LLM calls and print the discovery plan")
	return cmd
}

type discoverEnv struct {
	store      *storage.Storage
	cfg        *config.Config
	limit      int
	dryRun     bool
	noSemantic bool
}

func runDiscover(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noSemantic, _ := cmd.Flags().GetBool("no-semantic")
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
	return executeDiscover(cmd, &discoverEnv{store: storage.New(dir), cfg: cfg, limit: limit, dryRun: dryRun, noSemantic: noSemantic})
}

func executeDiscover(cmd *cobra.Command, env *discoverEnv) error {
	clusters, err := loadProblemClusters(env.store)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}
	if env.limit > 0 && len(clusters) > env.limit {
		clusters = clusters[:env.limit]
	}
	if len(clusters) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No problem clusters found. Run 'signalforge cluster' first.")
		return nil
	}
	if env.dryRun || env.noSemantic {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would discover JTBDs and solutions for %d cluster(s).\n", len(clusters))
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return errors.New("OPENROUTER_API_KEY is required for discover (use --dry-run or --no-semantic to skip LLM calls)")
	}
	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = env.cfg.OpenRouter.Model
	}
	client, err := openrouter.New(&env.cfg.OpenRouter, apiKey)
	if err != nil {
		return fmt.Errorf("create OpenRouter client: %w", err)
	}

	result := DiscoverResult{JTBDs: []domain.JobToBeDone{}, Solutions: []domain.SolutionHypothesis{}}
	for clusterIndex := range clusters {
		cluster := &clusters[clusterIndex]
		jobs, genErr := (discover.Generator{Client: client, Model: model}).Generate(commandContext(cmd), cluster)
		if genErr != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: cluster %s: %v\n", cluster.ID, genErr)
			continue
		}
		for jobIndex := range jobs {
			job := &jobs[jobIndex]
			result.JTBDs = append(result.JTBDs, *job)
			pt, rationale, classErr := (discover.Classifier{Client: client, Model: model}).Classify(commandContext(cmd), job)
			if classErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: JTBD %s: %v\n", job.ID, classErr)
				continue
			}
			if pt == domain.ProductTypeNoProduct {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Skipping %s: %s\n", job.ID, rationale)
				continue
			}
			solutions, solveErr := (discover.Solver{Client: client, Model: model}).Generate(commandContext(cmd), job)
			if solveErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: JTBD %s: %v\n", job.ID, solveErr)
				continue
			}
			result.Solutions = append(result.Solutions, solutions...)
		}
	}
	result.Solutions, result.Duplicates = discover.Deduplicate(result.Solutions)
	if err := env.store.SaveJSON(filepath.Join(env.store.BaseDir(), "discover.json"), result); err != nil {
		return fmt.Errorf("save discover results: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Generated %d JTBD(s) and %d solution(s).\n", len(result.JTBDs), len(result.Solutions))
	return nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func loadProblemClusters(store *storage.Storage) ([]domain.ProblemCluster, error) {
	files, err := store.ListFiles("clusters", ".json")
	if err != nil {
		return nil, fmt.Errorf("list cluster files: %w", err)
	}
	clusters := make([]domain.ProblemCluster, 0, len(files))
	for _, path := range files {
		var c domain.ProblemCluster
		if err := store.LoadJSON(path, &c); err == nil {
			clusters = append(clusters, c)
		}
	}
	return clusters, nil
}
