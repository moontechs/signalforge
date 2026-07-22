package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/sources/github"
	"github.com/moontechs/signalforge/internal/storage"
)

// CollectCmd represents the signalforge collect command.
var CollectCmd = newCollectCmd()

func newCollectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect raw signals from configured sources",
		Long: `Collects raw signals from public sources and stores them in the SignalForge data directory.

For the current MVP CLI flow, GitHub collection is wired end-to-end.

Example:
  signalforge collect --sources github --since 30d`,
		RunE: runCollect,
	}

	cmd.Flags().String("sources", "github", "Comma-separated sources to collect from")
	cmd.Flags().String("since", "30d", "Look back window such as 24h, 7d, or 30d")

	return cmd
}

func runCollect(cmd *cobra.Command, _ []string) error {
	sourceFlag, _ := cmd.Flags().GetString("sources")
	sinceFlag, _ := cmd.Flags().GetString("since")

	sinceWindow, err := parseSinceWindow(sinceFlag)
	if err != nil {
		return err
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

	selectedSources, err := resolveCollectSources(sourceFlag)
	if err != nil {
		return err
	}

	store := storage.New(dir)
	mem := memory.New(store)
	memoryPath := filepath.Join(dir, "memory.json")
	if store.Exists(memoryPath) {
		if err := mem.Load(); err != nil {
			return fmt.Errorf("load memory: %w", err)
		}
	}
	before := mem.GetStats()

	var collectors []domain.SourceCollector
	for _, source := range selectedSources {
		switch source {
		case "github":
			if !cfg.Sources.GitHub.Enabled {
				return fmt.Errorf("github collection is disabled in config")
			}
			if strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) == "" {
				return fmt.Errorf("GITHUB_TOKEN is required for github collection")
			}

			ghCfg := github.CollectorConfig{
				Enabled:            cfg.Sources.GitHub.Enabled,
				SearchIssues:       cfg.Sources.GitHub.SearchIssues,
				SearchDiscussions:  cfg.Sources.GitHub.SearchDiscussions,
				MaxItemsPerRun:     cfg.Sources.GitHub.MaxItemsPerRun,
				MaxCommentsPerItem: cfg.Sources.GitHub.MaxCommentsPerItem,
				Repositories:       cfg.Sources.GitHub.Repositories,
				Languages:          cfg.Sources.GitHub.Languages,
				Labels:             cfg.Sources.GitHub.Labels,
				MaxRequests:        cfg.Limits.MaxGitHubRequests,
			}

			collector, err := github.New(ghCfg)
			if err != nil {
				return fmt.Errorf("create github collector: %w", err)
			}

			// Attach disk cache.
			collector.WithCache(store)
			collectors = append(collectors, collector)

		default:
			return fmt.Errorf("source %q is not supported by the collect command yet", source)
		}
	}

	var totalSignals int
	for _, collector := range collectors {
		since := time.Now().Add(-sinceWindow)
		signals, err := collector.Collect(cmd.Context(), domain.CollectRequest{
			Since:   since,
			Sources: selectedSources,
		})
		totalSignals += len(signals)
		if err != nil {
			after := mem.GetStats()
			reportCollectSummary(cmd, collector.Name(), len(signals), statsDelta(before, after))
			return fmt.Errorf("%s collection completed with errors: %w", collector.Name(), err)
		}

		// Save memory after collection.
		for _, s := range signals {
			mem.AddRawSignal(s.Source, s.SourceID)
		}
	}

	if err := mem.Save(); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	after := mem.GetStats()
	reportCollectSummary(cmd, strings.Join(selectedSources, ","), totalSignals, statsDelta(before, after))
	return nil
}

func resolveCollectSources(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("at least one source must be specified")
	}

	seen := make(map[string]struct{})
	sources := make([]string, 0, 1)
	for _, part := range strings.Split(raw, ",") {
		source, ok := config.NormalizeSourceName(part)
		if !ok {
			return nil, fmt.Errorf("unsupported source %q", strings.TrimSpace(part))
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one source must be specified")
	}

	return sources, nil
}

func parseSinceWindow(raw string) (time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, fmt.Errorf("since window must not be empty")
	}

	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		count, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid since window %q", raw)
		}
		if count <= 0 {
			return 0, fmt.Errorf("since window must be greater than zero")
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}

	window, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid since window %q", raw)
	}
	if window <= 0 {
		return 0, fmt.Errorf("since window must be greater than zero")
	}
	return window, nil
}

func ensureStorageLayout(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for subDir := range config.DefaultDirStructure() {
		if err := os.MkdirAll(filepath.Join(dir, subDir), 0o755); err != nil {
			return err
		}
	}
	return nil
}

type collectStatsDelta struct {
	collected int
	skipped   int
	requests  int
}

func statsDelta(before, after domain.ResearchStats) collectStatsDelta {
	return collectStatsDelta{
		collected: after.RawSignalsCollected - before.RawSignalsCollected,
		skipped:   after.RawSignalsSkipped - before.RawSignalsSkipped,
		requests:  after.GitHubRequests - before.GitHubRequests,
	}
}

func reportCollectSummary(cmd *cobra.Command, source string, totalSignals int, delta collectStatsDelta) {
	fmt.Fprintf(
		cmd.OutOrStdout(),
		"Collected %d signals from %s. New: %d, skipped: %d, GitHub requests: %d\n",
		totalSignals,
		source,
		delta.collected,
		delta.skipped,
		delta.requests,
	)
}
