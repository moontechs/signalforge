package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

// PipelineCmd represents the signalforge pipeline command.
var PipelineCmd = newPipelineCmd()

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run the full signalforge pipeline",
		Long: `Runs the full signalforge pipeline: collect, classify, cluster, discover, and rank
in sequence. Each stage checks for existing output data and skips if already present
(unless --force is set).

Example:
  signalforge pipeline --sources github,hn --since 30d
  signalforge pipeline --dry-run
  signalforge pipeline --force`,
		RunE: runPipeline,
	}

	cmd.Flags().String("sources", "github", "Comma-separated sources to collect from")
	cmd.Flags().String("since", "30d", "Look back window such as 24h, 7d, or 30d")
	cmd.Flags().Bool("dry-run", false, "Print planned pipeline stages and exit without execution")
	cmd.Flags().Bool("force", false, "Re-run all stages even if output data already exists")

	return cmd
}

type pipelineEnv struct {
	dir     string
	store   *storage.Storage
	mem     *memory.DefaultMemory
	cfg     *config.Config
	sources string
	since   string
	dryRun  bool
	force   bool
}

func runPipeline(cmd *cobra.Command, _ []string) error {
	sources, _ := cmd.Flags().GetString("sources")
	since, _ := cmd.Flags().GetString("since")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

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

	env := &pipelineEnv{
		dir:     dir,
		store:   store,
		mem:     mem,
		cfg:     cfg,
		sources: sources,
		since:   since,
		dryRun:  dryRun,
		force:   force,
	}

	return executePipeline(cmd, env)
}

func executePipeline(cmd *cobra.Command, env *pipelineEnv) error {
	stages := []struct {
		name      string
		run       func(*cobra.Command, *pipelineEnv) (string, error)
		dataCheck func(*pipelineEnv) bool
	}{
		{
			name: "collect",
			run:  runPipelineCollect,
			dataCheck: func(env *pipelineEnv) bool {
				files, err := env.store.ListFiles("raw-signals", ".json")
				return err == nil && len(files) > 0
			},
		},
		{
			name: "classify",
			run:  runPipelineClassify,
			dataCheck: func(env *pipelineEnv) bool {
				files, err := env.store.ListFiles("problem-signals", ".json")
				return err == nil && len(files) > 0
			},
		},
		{
			name: "cluster",
			run:  runPipelineCluster,
			dataCheck: func(env *pipelineEnv) bool {
				files, err := env.store.ListFiles("clusters", ".json")
				return err == nil && len(files) > 0
			},
		},
		{
			name: "discover",
			run:  runPipelineDiscover,
			dataCheck: func(env *pipelineEnv) bool {
				return env.store.Exists(filepath.Join(env.dir, "discover.json"))
			},
		},
		{
			name:      "rank",
			run:       runPipelineRank,
			dataCheck: nil, // Rank always runs because it has no output data check.
		},
	}

	totalStages := len(stages)

	// Dry-run: print planned stages and exit.
	if env.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "=== Pipeline Plan (dry-run) ===\n")
		for i, stage := range stages {
			hasData := stage.dataCheck != nil && stage.dataCheck(env)
			skip := hasData && !env.force
			status := "will run"
			if skip {
				status = "will skip (data exists)"
			} else if hasData && env.force {
				status = "will re-run (--force)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Stage %d/%d: %s [%s]\n", i+1, totalStages, stage.name, status)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\n(dry-run) No API calls were made. No data was persisted.")
		return nil
	}

	// Execute stages in order.
	for i, stage := range stages {
		// Check if data already exists (skip unless --force).
		if stage.dataCheck != nil && stage.dataCheck(env) && !env.force {
			fmt.Fprintf(cmd.OutOrStdout(), "[pipeline] Stage %d/%d: %s... already up-to-date, skipping\n", i+1, totalStages, stage.name)
			continue
		}

		fmt.Fprintf(cmd.OutOrStdout(), "[pipeline] Stage %d/%d: %s...\n", i+1, totalStages, stage.name)

		message, err := stage.run(cmd, env)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "[pipeline] Stage %d/%d: %s failed: %v\n", i+1, totalStages, stage.name, err)
			return fmt.Errorf("pipeline failed at stage %q: %w", stage.name, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "[pipeline] Stage %d/%d: %s completed (%s)\n", i+1, totalStages, stage.name, message)
	}

	return nil
}

// runPipelineCollect sets up and runs the collect stage.
func runPipelineCollect(cmd *cobra.Command, env *pipelineEnv) (string, error) {
	// Read the pipeline-level force flag.
	force, _ := cmd.Flags().GetBool("force")

	collectEnv, err := setupCollectEnv(env.sources, env.since, "", 0, "", force, false, false)
	if err != nil {
		return "", fmt.Errorf("setup collect: %w", err)
	}

	// Count signals before collection.
	beforeFiles, _ := env.store.ListFiles("raw-signals", ".json")
	beforeCount := len(beforeFiles)

	if err := executeCollect(cmd, collectEnv); err != nil {
		return "", err
	}

	// Count signals after collection.
	afterFiles, _ := env.store.ListFiles("raw-signals", ".json")
	signalCount := len(afterFiles) - beforeCount
	if signalCount < 0 {
		signalCount = 0
	}

	return fmt.Sprintf("%d signals", signalCount), nil
}

// runPipelineClassify sets up and runs the classify stage.
func runPipelineClassify(cmd *cobra.Command, env *pipelineEnv) (string, error) {
	// Read the pipeline-level force flag.
	force, _ := cmd.Flags().GetBool("force")

	beforeFiles, _ := env.store.ListFiles("problem-signals", ".json")
	beforeCount := len(beforeFiles)

	// Build a classify env similar to runClassify.
	dir := env.dir
	cfg := env.cfg
	store := env.store
	mem := env.mem

	// Determine prompt directory.
	promptDir := filepath.Join(dir, "..", "prompts")
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		promptDir = filepath.Join(cwd, "prompts")
	}
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		promptDir = filepath.Join(dir, "prompts")
	}

	classifyEnv := &classifyEnv{
		store:     store,
		mem:       mem,
		cfg:       cfg,
		promptDir: promptDir,
		limit:     0,
		batchSize: 0,
		model:     "",
		force:     force,
		dryRun:    false,
		resume:    true,
	}

	if err := executeClassify(cmd, classifyEnv); err != nil {
		return "", err
	}

	afterFiles, _ := env.store.ListFiles("problem-signals", ".json")
	problemCount := len(afterFiles) - beforeCount
	if problemCount < 0 {
		problemCount = 0
	}

	return fmt.Sprintf("%d problem signals", problemCount), nil
}

// runPipelineCluster sets up and runs the cluster stage.
func runPipelineCluster(cmd *cobra.Command, env *pipelineEnv) (string, error) {
	// Read the pipeline-level force flag.
	force, _ := cmd.Flags().GetBool("force")

	// Force means delete existing clusters and re-cluster.
	if force {
		// Remove existing cluster files so re-clustering is not skipped.
		files, _ := env.store.ListFiles("clusters", ".json")
		for _, f := range files {
			os.Remove(f)
		}
	}

	clusterEnv := &clusterEnv{
		store:      env.store,
		mem:        env.mem,
		cfg:        env.cfg,
		threshold:  0.3,
		dryRun:     false,
		limit:      0,
		noSemantic: false,
	}

	if err := executeCluster(cmd, clusterEnv); err != nil {
		return "", err
	}

	afterFiles, _ := env.store.ListFiles("clusters", ".json")
	return fmt.Sprintf("%d clusters", len(afterFiles)), nil
}

// runPipelineDiscover sets up and runs the discover stage.
func runPipelineDiscover(cmd *cobra.Command, env *pipelineEnv) (string, error) {
	discoverEnv := &discoverEnv{
		store:      env.store,
		cfg:        env.cfg,
		limit:      0,
		dryRun:     false,
		noSemantic: false,
	}

	if err := executeDiscover(cmd, discoverEnv); err != nil {
		return "", err
	}

	// Count solutions from discover.json.
	var result DiscoverResult
	discoverPath := filepath.Join(env.store.BaseDir(), "discover.json")
	if env.store.Exists(discoverPath) {
		_ = env.store.LoadJSON(discoverPath, &result)
	}

	return fmt.Sprintf("%d JTBDs, %d solutions", len(result.JTBDs), len(result.Solutions)), nil
}

// runPipelineRank sets up and runs the rank stage.
func runPipelineRank(cmd *cobra.Command, env *pipelineEnv) (string, error) {
	rankEnv := &rankEnv{
		store:         env.store,
		problemScore:  0,
		solutionScore: 0,
		confidence:    0,
		limit:         0,
	}

	if err := executeRank(cmd, rankEnv); err != nil {
		return "", err
	}

	// Count ranked items.
	clusters, _ := loadProblemClusters(env.store)
	solutions, _ := loadSolutions(env.store)

	return fmt.Sprintf("%d clusters, %d solutions", len(clusters), len(solutions)), nil
}
