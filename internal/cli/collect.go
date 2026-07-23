package cli

import (
	"errors"
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
	"github.com/moontechs/signalforge/internal/sources/hackernews"
	"github.com/moontechs/signalforge/internal/sources/stackexchange"
	"github.com/moontechs/signalforge/internal/storage"
)

// CollectCmd represents the signalforge collect command.
var CollectCmd = newCollectCmd()

func newCollectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect raw signals from configured sources",
		Long: `Collects raw signals from public sources and stores them in the SignalForge data directory.

For the current MVP CLI flow, GitHub, Hacker News, and Stack Exchange collection are wired end-to-end.

Example:
  signalforge collect --sources github --since 30d
  signalforge collect --sources stackexchange --since 30d`,
		RunE: runCollect,
	}

	cmd.Flags().String("sources", "github", "Comma-separated sources to collect from")
	cmd.Flags().String("since", "30d", "Look back window such as 24h, 7d, or 30d")

	return cmd
}

type collectEnv struct {
	store           *storage.Storage
	mem             *memory.DefaultMemory
	selectedSources []string
	collectors      []domain.SourceCollector
	before          *domain.ResearchStats
	sinceWindow     time.Duration
}

func runCollect(cmd *cobra.Command, _ []string) error {
	sourceFlag, _ := cmd.Flags().GetString("sources")
	sinceFlag, _ := cmd.Flags().GetString("since")

	env, err := setupCollectEnv(sourceFlag, sinceFlag)
	if err != nil {
		return err
	}

	return executeCollect(cmd, env)
}

func setupCollectEnv(sourceFlag, sinceFlag string) (*collectEnv, error) {
	sinceWindow, err := parseSinceWindow(sinceFlag)
	if err != nil {
		return nil, err
	}

	dir, err := config.GetSignalForgeDir()
	if err != nil {
		return nil, fmt.Errorf("determine signalforge dir: %w", err)
	}

	if err := ensureStorageLayout(dir); err != nil {
		return nil, fmt.Errorf("initialize storage layout: %w", err)
	}

	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	selectedSources, err := resolveCollectSources(sourceFlag)
	if err != nil {
		return nil, err
	}

	store := storage.New(dir)
	mem := memory.New(store)
	memoryPath := filepath.Join(dir, "memory.json")
	if store.Exists(memoryPath) {
		if err := mem.Load(); err != nil {
			return nil, fmt.Errorf("load memory: %w", err)
		}
	}

	beforeStats := mem.GetStats()

	collectors := make([]domain.SourceCollector, 0, len(selectedSources))
	for _, source := range selectedSources {
		collector, err := buildCollector(source, cfg, store)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, collector)
	}

	return &collectEnv{
		store:           store,
		mem:             mem,
		selectedSources: selectedSources,
		collectors:      collectors,
		before:          &beforeStats,
		sinceWindow:     sinceWindow,
	}, nil
}

func executeCollect(cmd *cobra.Command, env *collectEnv) error {
	var totalSignals int
	for _, collector := range env.collectors {
		since := time.Now().Add(-env.sinceWindow)
		signals, err := collector.Collect(cmd.Context(), domain.CollectRequest{
			Since:   since,
			Sources: env.selectedSources,
		})
		totalSignals += len(signals)

		// Track HN request/cache-hit stats.
		if hnCol, ok := collector.(*hackernews.Collector); ok {
			stats := hnCol.Stats()
			env.mem.AddHNRequests(stats.Requests)
			env.mem.AddHNCacheHits(stats.CacheHits)
		}
		if seCol, ok := collector.(*stackexchange.Collector); ok {
			stats := seCol.Stats()
			env.mem.AddStackExchangeRequests(stats.Requests)
			env.mem.AddStackExchangeCacheHits(stats.CacheHits)
		}

		if err != nil {
			afterStats := env.mem.GetStats()
			if outputErr := reportCollectSummary(cmd, collector.Name(), len(signals), statsDelta(env.before, &afterStats)); outputErr != nil {
				return fmt.Errorf("write collection summary: %w", outputErr)
			}
			return fmt.Errorf("%s collection completed with errors: %w", collector.Name(), err)
		}

		for i := range signals {
			env.mem.AddRawSignal(signals[i].Source, signals[i].SourceID)
		}
	}

	if err := env.mem.Save(); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	afterStats := env.mem.GetStats()
	return reportCollectSummary(cmd, strings.Join(env.selectedSources, ","), totalSignals, statsDelta(env.before, &afterStats))
}

func resolveCollectSources(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("at least one source must be specified")
	}

	seen := make(map[string]struct{})
	sources := make([]string, 0, 1)
	parts := strings.Split(raw, ",")
	for _, part := range parts {
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
		return nil, errors.New("at least one source must be specified")
	}

	return sources, nil
}

func parseSinceWindow(raw string) (time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, errors.New("since window must not be empty")
	}

	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		count, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid since window %q", raw)
		}
		if count <= 0 {
			return 0, errors.New("since window must be greater than zero")
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}

	window, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid since window %q", raw)
	}
	if window <= 0 {
		return 0, errors.New("since window must be greater than zero")
	}
	return window, nil
}

func ensureStorageLayout(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	for subDir := range config.DefaultDirStructure() {
		if err := os.MkdirAll(filepath.Join(dir, subDir), 0o755); err != nil {
			return fmt.Errorf("create storage subdirectory %s: %w", subDir, err)
		}
	}
	return nil
}

func buildCollector(source string, cfg *config.Config, store *storage.Storage) (domain.SourceCollector, error) { //nolint:ireturn // factory function intentionally returns interface
	switch source {
	case "github":
		if !cfg.Sources.GitHub.Enabled {
			return nil, errors.New("github collection is disabled in config")
		}
		if strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) == "" {
			return nil, errors.New("GITHUB_TOKEN is required for github collection")
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

		collector, err := github.New(&ghCfg)
		if err != nil {
			return nil, fmt.Errorf("create github collector: %w", err)
		}

		// Attach disk cache.
		collector.WithCache(store)
		return collector, nil

	case "hackernews":
		if !cfg.Sources.HackerNews.Enabled {
			return nil, errors.New("hackernews collection is disabled in config")
		}

		hnCfg := &hackernews.ConfigValues{
			Enabled:            cfg.Sources.HackerNews.Enabled,
			Feeds:              cfg.Sources.HackerNews.Feeds,
			MaxItemsPerRun:     cfg.Sources.HackerNews.MaxItemsPerRun,
			MaxCommentsPerItem: cfg.Sources.HackerNews.MaxCommentsPerItem,
			MinimumScore:       cfg.Sources.HackerNews.MinimumScore,
			MaxRequests:        cfg.Limits.MaxHNRequests,
		}

		collector, err := hackernews.New(hnCfg)
		if err != nil {
			return nil, fmt.Errorf("create hackernews collector: %w", err)
		}

		collector.WithCache(store)
		return collector, nil

	case "stackexchange":
		if !cfg.Sources.StackExchange.Enabled {
			return nil, errors.New("stackexchange collection is disabled in config")
		}

		seCfg := &stackexchange.ConfigValues{
			Enabled:         cfg.Sources.StackExchange.Enabled,
			APIKey:          strings.TrimSpace(os.Getenv("STACKEXCHANGE_API_KEY")),
			Sites:           cfg.Sources.StackExchange.Sites,
			MaxItemsPerSite: cfg.Sources.StackExchange.MaxItemsPerSite,
			MinimumScore:    cfg.Sources.StackExchange.MinimumScore,
			MinimumViews:    cfg.Sources.StackExchange.MinimumViews,
			PageSize:        cfg.Sources.StackExchange.PageSize,
			MaxPagesPerSite: cfg.Sources.StackExchange.MaxPagesPerSite,
			MaxRequests:     cfg.Limits.MaxStackExchangeReqs,
		}
		collector := stackexchange.New(seCfg, nil)
		collector.WithCache(store)
		return collector, nil

	default:
		return nil, fmt.Errorf("source %q is not supported by the collect command yet", source)
	}
}

type collectStatsDelta struct {
	collected   int
	skipped     int
	requests    int
	hnRequests  int
	hnCacheHits int
	seRequests  int
	seCacheHits int
}

func statsDelta(before, after *domain.ResearchStats) collectStatsDelta {
	return collectStatsDelta{
		collected:   after.RawSignalsCollected - before.RawSignalsCollected,
		skipped:     after.RawSignalsSkipped - before.RawSignalsSkipped,
		requests:    after.GitHubRequests - before.GitHubRequests,
		hnRequests:  after.HackerNewsRequests - before.HackerNewsRequests,
		hnCacheHits: after.HackerNewsCacheHits - before.HackerNewsCacheHits,
		seRequests:  after.StackExchangeRequests - before.StackExchangeRequests,
		seCacheHits: after.StackExchangeCacheHits - before.StackExchangeCacheHits,
	}
}

func reportCollectSummary(cmd *cobra.Command, source string, totalSignals int, delta collectStatsDelta) error {
	msg := fmt.Sprintf("Collected %d signals from %s. New: %d, skipped: %d",
		totalSignals, source, delta.collected, delta.skipped)

	if delta.requests > 0 {
		msg += fmt.Sprintf(", GitHub requests: %d", delta.requests)
	}
	if delta.hnRequests > 0 {
		msg += fmt.Sprintf(", HN requests: %d (cache hits: %d)", delta.hnRequests, delta.hnCacheHits)
	}
	if delta.seRequests > 0 {
		msg += fmt.Sprintf(", Stack Exchange requests: %d (cache hits: %d)", delta.seRequests, delta.seCacheHits)
	}
	msg += "\n"

	_, err := fmt.Fprint(cmd.OutOrStdout(), msg)
	if err != nil {
		return fmt.Errorf("write collection summary: %w", err)
	}
	return nil
}
