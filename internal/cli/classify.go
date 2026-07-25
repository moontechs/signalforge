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

	"github.com/moontechs/signalforge/internal/classify"
	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/openrouter"
	"github.com/moontechs/signalforge/internal/storage"
)

// ClassifyCmd represents the signalforge classify command.
var ClassifyCmd = newClassifyCmd()

func newClassifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "classify",
		Short: "Classify raw signals using LLM",
		Long: `Classifies collected raw signals as problem signals or noise using an LLM
via OpenRouter. Each raw signal is analyzed independently and the results
are stored as problem signal files.

Example:
  signalforge classify
  signalforge classify --limit 50 --model google/gemini-pro:free
  signalforge classify --batch-size 5 --force
  signalforge classify --dry-run`,
		RunE: runClassify,
	}

	cmd.Flags().Int("limit", 0, "Maximum number of signals to classify (0 = no limit)")
	cmd.Flags().Int("batch-size", 0, "Batch size for LLM requests (default from config)")
	cmd.Flags().String("model", "", "Model override (e.g., google/gemini-pro:free)")
	cmd.Flags().Bool("force", false, "Re-classify already classified signals")
	cmd.Flags().Bool("dry-run", false, "Print classification plan and exit without making API calls")
	cmd.Flags().Bool("resume", false, "Skip previously classified signals (default behavior)")

	return cmd
}

type classifyEnv struct {
	store     *storage.Storage
	mem       *memory.DefaultMemory
	cfg       *config.Config
	promptDir string
	limit     int
	batchSize int
	model     string
	force     bool
	dryRun    bool
	resume    bool
}

func runClassify(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	batchSize, _ := cmd.Flags().GetInt("batch-size")
	model, _ := cmd.Flags().GetString("model")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	resume, _ := cmd.Flags().GetBool("resume")

	if limit < 0 {
		return errors.New("--limit must be a non-negative integer")
	}
	if batchSize < 0 {
		return errors.New("--batch-size must be a non-negative integer")
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

	// Determine prompt directory: look for prompts/ next to the config.
	promptDir := filepath.Join(dir, "..", "prompts")
	// If not found, try the project root's prompts/ directory.
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		// Fall back to the current working directory's prompts/.
		cwd, _ := os.Getwd()
		promptDir = filepath.Join(cwd, "prompts")
	}
	// Resolve relative to the config dir if it's a relative path.
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		// Try relative to the signalforge dir.
		promptDir = filepath.Join(dir, "prompts")
	}

	env := &classifyEnv{
		store:     store,
		mem:       mem,
		cfg:       cfg,
		promptDir: promptDir,
		limit:     limit,
		batchSize: batchSize,
		model:     model,
		force:     force,
		dryRun:    dryRun,
		resume:    resume,
	}

	return executeClassify(cmd, env)
}

func executeClassify(cmd *cobra.Command, env *classifyEnv) error {
	// Load raw signals from the raw-signals directory.
	rawSignals, err := loadRawSignals(env)
	if err != nil {
		return fmt.Errorf("load raw signals: %w", err)
	}

	if len(rawSignals) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No raw signals found. Run 'signalforge collect' first.")
		return nil
	}

	// Filter out already classified signals (unless --force).
	signals := filterClassifiedSignals(rawSignals, env)

	// Apply limit.
	if env.limit > 0 && len(signals) > env.limit {
		signals = signals[:env.limit]
	}

	// Determine effective batch size.
	batchSize := env.batchSize
	if batchSize <= 0 {
		batchSize = env.cfg.Pipeline.ClassificationBatchSize
	}

	// Determine model.
	model := env.model
	if model == "" {
		model = env.cfg.OpenRouter.Model
	}
	if model == "" {
		model = "google/gemini-2.0-flash-lite-preview-02-05:free"
	}

	// Determine prompt path.
	promptPath := filepath.Join(env.promptDir, "classify_signal.txt")

	// Handle dry-run.
	if env.dryRun {
		printClassifyDryRun(cmd, env, signals, batchSize, model, promptPath)
		return nil
	}

	// Create OpenRouter client.
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return errors.New("OPENROUTER_API_KEY environment variable is required for classification")
	}

	orClient, err := openrouter.New(&env.cfg.OpenRouter, apiKey)
	if err != nil {
		return fmt.Errorf("create OpenRouter client: %w", err)
	}

	// Create classifier.
	classifierCfg := classify.Config{
		Model:       model,
		BatchSize:   batchSize,
		Temperature: env.cfg.OpenRouter.ClassificationTemp,
		MaxTokens:   env.cfg.OpenRouter.MaxOutputTokens,
		PromptPath:  promptPath,
	}
	classifier := classify.New(orClient, classifierCfg)

	// Classify signals.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	problemSignals, failures := classifier.Classify(ctx, signals)
	counts := saveClassifications(cmd, env, problemSignals)

	// Update LLM request count.
	llmStats := orClient.Stats()
	for i := 0; i < llmStats.Attempts; i++ {
		env.mem.IncrementStat("llm_requests")
	}

	// Save memory atomically.
	if err := env.mem.Save(); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	// Print summary.
	printClassifySummary(cmd, counts.saved, len(failures), counts.signals, counts.noise, len(signals), env.force)

	return nil
}

type classificationCounts struct {
	saved   int
	signals int
	noise   int
}

func saveClassifications(
	cmd *cobra.Command,
	env *classifyEnv,
	problemSignals []domain.ProblemSignal,
) classificationCounts {
	var counts classificationCounts
	for index := range problemSignals {
		signal := &problemSignals[index]
		signalPath := filepath.Join(env.store.BaseDir(), "problem-signals", signal.ID+".json")
		if err := env.store.SaveJSON(signalPath, signal); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to save problem signal %s: %v\n", signal.ID, err)
			continue
		}

		counts.saved++
		if signal.IsProblemSignal {
			counts.signals++
			env.mem.IncrementStat("problem_signals_found")
		} else {
			counts.noise++
			env.mem.IncrementStat("noise_signals")
		}
	}
	return counts
}

// loadRawSignals loads all raw signal files from the raw-signals directory.
func loadRawSignals(env *classifyEnv) ([]domain.RawSignal, error) {
	files, err := env.store.ListFiles("raw-signals", ".json")
	if err != nil {
		return nil, fmt.Errorf("list raw signal files: %w", err)
	}

	signals := make([]domain.RawSignal, 0, len(files))
	for _, path := range files {
		var signal domain.RawSignal
		if err := env.store.LoadJSON(path, &signal); err != nil {
			// Log the error but continue with other signals.
			continue
		}
		signals = append(signals, signal)
	}

	return signals, nil
}

// filterClassifiedSignals removes signals that already have a problem signal
// file, unless --force is set.
func filterClassifiedSignals(signals []domain.RawSignal, env *classifyEnv) []domain.RawSignal {
	if env.force {
		// When force is set, re-classify all signals.
		return signals
	}

	// Build a set of already-classified raw signal IDs.
	problemFiles, err := env.store.ListFiles("problem-signals", ".json")
	if err != nil || len(problemFiles) == 0 {
		return signals
	}

	classifiedIDs := make(map[string]bool)
	for _, path := range problemFiles {
		var ps domain.ProblemSignal
		if err := env.store.LoadJSON(path, &ps); err != nil {
			continue
		}
		if ps.RawSignalID != "" {
			classifiedIDs[ps.RawSignalID] = true
		}
	}

	filtered := make([]domain.RawSignal, 0, len(signals))
	for i := range signals {
		if !classifiedIDs[signals[i].ID] {
			filtered = append(filtered, signals[i])
		}
	}

	return filtered
}

// printClassifyDryRun prints the classification plan for dry-run mode.
func printClassifyDryRun(cmd *cobra.Command, env *classifyEnv, signals []domain.RawSignal, batchSize int, model, promptPath string) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintf(w, "=== Classification Plan (dry-run) ===\n")
	_, _ = fmt.Fprintf(w, "  Signals to classify: %d\n", len(signals))
	_, _ = fmt.Fprintf(w, "  Batch size: %d\n", batchSize)
	_, _ = fmt.Fprintf(w, "  Model: %s\n", model)
	_, _ = fmt.Fprintf(w, "  Prompt template: %s\n", promptPath)
	_, _ = fmt.Fprintf(w, "  Force re-classify: %t\n", env.force)

	batches := (len(signals) + batchSize - 1) / batchSize
	// One request is made per signal in each batch.
	_, _ = fmt.Fprintf(w, "  Estimated LLM requests: %d\n", batches)

	if env.resume {
		_, _ = fmt.Fprintln(w, "  Mode: resume (skip already classified)")
	}

	_, _ = fmt.Fprintln(w, "\n(dry-run) No API calls were made. No data was persisted.")
}

// printClassifySummary prints the classification results summary.
func printClassifySummary(cmd *cobra.Command, saved, failures, signalCount, noiseCount, total int, force bool) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(w, "=== Classification Summary ===")

	if force {
		_, _ = fmt.Fprintln(w, "  Mode: force (re-classified all signals)")
	}

	_, _ = fmt.Fprintf(w, "  Signals processed: %d\n", total)
	_, _ = fmt.Fprintf(w, "  Problem signals found: %d\n", signalCount)
	_, _ = fmt.Fprintf(w, "  Noise signals: %d\n", noiseCount)
	_, _ = fmt.Fprintf(w, "  Saved: %d\n", saved)

	if failures > 0 {
		_, _ = fmt.Fprintf(w, "  Classification failures: %d\n", failures)
	}
}
