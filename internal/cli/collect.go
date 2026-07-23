package cli

import (
	"errors"
	"fmt"
	"io"
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
	cmd.Flags().String("until", "", "ISO date or duration window (e.g., 2024-01-15, 7d, 24h) — if omitted, uses now")
	cmd.Flags().Int("max-items", 0, "Maximum items to collect per source (0 = use source default)")
	cmd.Flags().String("language", "", "Optional language filter (e.g., 'go', 'python')")
	cmd.Flags().Bool("force", false, "Skip deduplication and re-collect already-seen signals")
	cmd.Flags().Bool("dry-run", false, "Print planned collection and exit without making API calls")
	cmd.Flags().Bool("resume", false, "Resume collection from last stored cursor per source")

	return cmd
}

type collectEnv struct {
	store           *storage.Storage
	mem             *memory.DefaultMemory
	cfg             *config.Config
	selectedSources []string
	collectors      []domain.SourceCollector
	before          *domain.ResearchStats
	sinceWindow     time.Duration
	untilWindow     time.Duration
	maxItems        int
	language        string
	force           bool
	dryRun          bool
	resume          bool
}

func runCollect(cmd *cobra.Command, _ []string) error {
	sourceFlag, _ := cmd.Flags().GetString("sources")
	sinceFlag, _ := cmd.Flags().GetString("since")
	untilFlag, _ := cmd.Flags().GetString("until")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	language, _ := cmd.Flags().GetString("language")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	resume, _ := cmd.Flags().GetBool("resume")

	if maxItems < 0 {
		return errors.New("--max-items must be a non-negative integer")
	}

	env, err := setupCollectEnv(sourceFlag, sinceFlag, untilFlag, maxItems, language, force, dryRun, resume)
	if err != nil {
		return err
	}

	return executeCollect(cmd, env)
}

func setupCollectEnv(sourceFlag, sinceFlag, untilFlag string, maxItems int, language string, force, dryRun, resume bool) (*collectEnv, error) {
	sinceWindow, err := parseSinceWindow(sinceFlag)
	if err != nil {
		return nil, err
	}

	untilWindow, err := parseUntilWindow(untilFlag)
	if err != nil {
		return nil, err
	}

	// Validate that until does not precede since (would produce empty range).
	if untilWindow != 0 {
		since := time.Now().Add(-sinceWindow)
		until := time.Now().Add(untilWindow)
		if since.After(until) {
			return nil, fmt.Errorf("until %q must be later than since %q: would produce empty collection range", untilFlag, sinceFlag)
		}
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

	// Order sources deterministically regardless of input order.
	selectedSources = orderSourcesDeterministically(selectedSources)

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
		cfg:             cfg,
		selectedSources: selectedSources,
		collectors:      collectors,
		before:          &beforeStats,
		sinceWindow:     sinceWindow,
		untilWindow:     untilWindow,
		maxItems:        maxItems,
		language:        language,
		force:           force,
		dryRun:          dryRun,
		resume:          resume,
	}, nil
}

// sourceOrder defines the deterministic execution order for collectors.
var sourceOrder = []string{"github", "hackernews", "stackexchange"}

// orderSourcesDeterministically reorders the given source names to the fixed
// order: GitHub, Hacker News, Stack Exchange. Sources not in the known set
// are appended at the end in their original relative order.
func orderSourcesDeterministically(sources []string) []string {
	requested := make(map[string]bool, len(sources))
	for _, s := range sources {
		requested[s] = true
	}
	result := make([]string, 0, len(sources))
	for _, s := range sourceOrder {
		if requested[s] {
			result = append(result, s)
			delete(requested, s)
		}
	}
	// Append any remaining sources not in the preferred order.
	for _, s := range sources {
		if requested[s] {
			result = append(result, s)
		}
	}
	return result
}

// cursorAware is an optional interface collectors can implement to report
// their cursor state after a collection run.
type cursorAware interface {
	Cursor() map[string]string
}

// dryRunPlan describes the planned collection for a single source in dry-run mode.
type dryRunPlan struct {
	Source        string
	Targets       []string
	EstimatedReqs int
	Since         string
	Until         string
	MaxItems      int
	Language      string
	HasCursor     bool
	CursorValue   string
}

// buildDryRunPlans constructs dry-run plans for all selected sources without
// making any HTTP requests. It uses the environment's configuration values
// and known source request shapes to estimate request counts.
func buildDryRunPlans(env *collectEnv, cfg *config.Config) []dryRunPlan {
	plans := make([]dryRunPlan, 0, len(env.selectedSources))

	for _, src := range env.selectedSources {
		plan := dryRunPlan{
			Source:   src,
			MaxItems: env.maxItems,
			Language: env.language,
			Since:    time.Now().Add(-env.sinceWindow).Format("2006-01-02"),
		}

		if env.untilWindow != 0 {
			plan.Until = time.Now().Add(env.untilWindow).Format("2006-01-02")
		} else {
			plan.Until = "now"
		}

		if env.resume {
			if cursor, exists := env.mem.GetCursor(src); exists {
				plan.HasCursor = true
				plan.CursorValue = cursor
			}
		}

		switch src {
		case "github":
			plan.Targets = buildGitHubTargets(cfg)
			plan.EstimatedReqs = estimateGitHubRequests(cfg, env)
		case "hackernews":
			plan.Targets = buildHNFeeds(cfg)
			plan.EstimatedReqs = estimateHNRequests(cfg, env)
		case "stackexchange":
			plan.Targets = buildSETargets(cfg)
			plan.EstimatedReqs = estimateSERequests(cfg, env)
		default:
			plan.Targets = []string{src}
			plan.EstimatedReqs = 1
		}

		plans = append(plans, plan)
	}

	return plans
}

func buildGitHubTargets(cfg *config.Config) []string {
	var targets []string
	if cfg.Sources.GitHub.SearchIssues {
		targets = append(targets, "Search Issues API")
	}
	if cfg.Sources.GitHub.SearchDiscussions {
		targets = append(targets, "GraphQL Discussions API")
	}
	if len(cfg.Sources.GitHub.Repositories) > 0 {
		for _, repo := range cfg.Sources.GitHub.Repositories {
			targets = append(targets, "repo: "+repo)
		}
	} else {
		targets = append(targets, "language filter: "+fmt.Sprintf("%v", cfg.Sources.GitHub.Languages))
	}
	return targets
}

func buildHNFeeds(cfg *config.Config) []string {
	feeds := make([]string, len(cfg.Sources.HackerNews.Feeds))
	for i, f := range cfg.Sources.HackerNews.Feeds {
		feeds[i] = "feed: " + f
	}
	return feeds
}

func buildSETargets(cfg *config.Config) []string {
	sites := make([]string, len(cfg.Sources.StackExchange.Sites))
	for i, s := range cfg.Sources.StackExchange.Sites {
		sites[i] = "site: " + s
	}
	return sites
}

func estimateGitHubRequests(cfg *config.Config, env *collectEnv) int {
	maxItems := cfg.Sources.GitHub.MaxItemsPerRun
	if env.maxItems > 0 {
		maxItems = env.maxItems
	}
	itemsPerPage := 100
	// Search pages for issues + 1 comment request per result (if comments enabled).
	searchPages := (maxItems + itemsPerPage - 1) / itemsPerPage
	var total int
	if cfg.Sources.GitHub.SearchIssues {
		total += searchPages
		if cfg.Sources.GitHub.MaxCommentsPerItem > 0 {
			total += maxItems // one comment fetch per issue.
		}
	}
	if cfg.Sources.GitHub.SearchDiscussions {
		// GraphQL fetches 50 per page.
		total += (maxItems + 49) / 50
	}
	if total < 1 {
		total = 1
	}
	return total
}

func estimateHNRequests(cfg *config.Config, env *collectEnv) int {
	feeds := len(cfg.Sources.HackerNews.Feeds)
	maxItems := cfg.Sources.HackerNews.MaxItemsPerRun
	if env.maxItems > 0 {
		maxItems = env.maxItems
	}
	total := feeds + maxItems
	if cfg.Sources.HackerNews.MaxCommentsPerItem > 0 {
		total += maxItems
	}
	if total < 1 {
		total = 1
	}
	return total
}

func estimateSERequests(cfg *config.Config, _ *collectEnv) int {
	sites := len(cfg.Sources.StackExchange.Sites)
	pagesPerSite := cfg.Sources.StackExchange.MaxPagesPerSite
	total := sites * pagesPerSite
	if total < 1 {
		total = 1
	}
	return total
}

// printDryRunPlan prints the dry-run collection plan to the command output.
func printDryRunPlan(cmd *cobra.Command, plans []dryRunPlan) error {
	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(w, "=== Collection Plan (dry-run) ==="); err != nil {
		return fmt.Errorf("write dry-run header: %w", err)
	}

	hasResume := hasAnyCursor(plans)

	for i := range plans {
		p := &plans[i]
		printPlanHeader(w, p.Source)
		printPlanTargets(w, p.Targets)
		printPlanField(w, "estimated requests", strconv.Itoa(p.EstimatedReqs))
		printPlanField(w, "since", p.Since)
		printPlanField(w, "until", p.Until)
		printPlanField(w, "max-items", strconv.Itoa(p.MaxItems))
		printPlanLanguage(w, p.Language)
		printPlanCursor(w, p, hasResume)
	}

	if _, err := fmt.Fprintln(w, "\n(dry-run) No API calls were made. No data was persisted."); err != nil {
		return fmt.Errorf("write dry-run footer: %w", err)
	}
	return nil
}

func printPlanHeader(w io.Writer, source string) {
	_, _ = fmt.Fprintf(w, "\n--- %s ---\n", source)
}

func printPlanTargets(w io.Writer, targets []string) {
	for _, t := range targets {
		_, _ = fmt.Fprintf(w, "  target: %s\n", t)
	}
}

func printPlanField(w io.Writer, key, value string) {
	_, _ = fmt.Fprintf(w, "  %s: %s\n", key, value)
}

func printPlanLanguage(w io.Writer, lang string) {
	if lang != "" {
		_, _ = fmt.Fprintf(w, "  language: %s\n", lang)
	}
}

func printPlanCursor(w io.Writer, p *dryRunPlan, hasResume bool) {
	if !hasResume {
		return
	}
	cursorVal := "none"
	if p.HasCursor {
		cursorVal = p.CursorValue
	}
	_, _ = fmt.Fprintf(w, "  resume cursor: %s\n", cursorVal)
}

// hasAnyCursor returns true if at least one plan has a cursor set.
func hasAnyCursor(plans []dryRunPlan) bool {
	for i := range plans {
		if plans[i].HasCursor {
			return true
		}
	}
	return false
}

func executeCollect(cmd *cobra.Command, env *collectEnv) error {
	// Dry-run: print plan and return without making any API calls.
	if env.dryRun {
		plans := buildDryRunPlans(env, env.cfg)
		return printDryRunPlan(cmd, plans)
	}

	var totalSignals int
	for _, collector := range env.collectors {
		req := buildCollectRequest(env, collector)
		signals, err := collector.Collect(cmd.Context(), req)
		signals = deduplicateSignals(signals, env)
		totalSignals += len(signals)
		trackCollectorStats(env, collector)

		if err != nil {
			afterStats := env.mem.GetStats()
			if outputErr := reportCollectSummary(cmd, collector.Name(), len(signals), statsDelta(env.before, &afterStats)); outputErr != nil {
				return fmt.Errorf("write collection summary: %w", outputErr)
			}
			return fmt.Errorf("%s collection completed with errors: %w", collector.Name(), err)
		}

		persistCursor(env, collector)
		recordSignals(env, signals)
	}

	if err := env.mem.Save(); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	afterStats := env.mem.GetStats()
	return reportCollectSummary(cmd, strings.Join(env.selectedSources, ","), totalSignals, statsDelta(env.before, &afterStats))
}

// buildCollectRequest constructs a CollectRequest for the given collector from the environment.
func buildCollectRequest(env *collectEnv, collector domain.SourceCollector) domain.CollectRequest {
	since := time.Now().Add(-env.sinceWindow)
	var until time.Time
	if env.untilWindow != 0 {
		until = time.Now().Add(env.untilWindow)
	}
	var languages []string
	if env.language != "" {
		languages = []string{env.language}
	}
	var cursor map[string]string
	if env.resume {
		if c, exists := env.mem.GetCursor(collector.Name()); exists {
			cursor = map[string]string{collector.Name(): c}
		}
	}
	return domain.CollectRequest{
		Since:     since,
		Until:     until,
		MaxItems:  env.maxItems,
		Force:     env.force,
		DryRun:    env.dryRun,
		Sources:   env.selectedSources,
		Languages: languages,
		Cursor:    cursor,
	}
}

// deduplicateSignals filters out signals that already exist in persistent memory.
// When --force is set, all signals pass through without filtering.
func deduplicateSignals(signals []domain.RawSignal, env *collectEnv) []domain.RawSignal {
	if env.force || len(signals) == 0 {
		return signals
	}
	filtered := make([]domain.RawSignal, 0, len(signals))
	for i := range signals {
		if env.mem.HasRawSignal(signals[i].Source, signals[i].SourceID) || env.mem.HasContentHash(signals[i].ContentHash) {
			continue
		}
		filtered = append(filtered, signals[i])
	}
	return filtered
}

// trackCollectorStats records HN or Stack Exchange request/cache-hit stats into memory.
func trackCollectorStats(env *collectEnv, collector domain.SourceCollector) {
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
}

// persistCursor updates memory with any cursor returned by a cursor-aware collector.
func persistCursor(env *collectEnv, collector domain.SourceCollector) {
	if ca, ok := collector.(cursorAware); ok {
		cursors := ca.Cursor()
		for src, cur := range cursors {
			env.mem.SetCursor(src, cur)
		}
	}
}

// recordSignals adds all signals to the persistent memory.
func recordSignals(env *collectEnv, signals []domain.RawSignal) {
	for i := range signals {
		env.mem.AddRawSignal(signals[i].Source, signals[i].SourceID)
	}
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

// parseUntilWindow parses an until flag value into a duration from now.
// Accepts ISO-8601 dates (e.g., "2024-01-15") and duration/window formats
// compatible with parseSinceWindow (e.g., "7d", "24h").
// Returns 0 if raw is empty (no constraint).
func parseUntilWindow(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}

	// Try ISO-8601 date format first (e.g., "2024-01-15").
	if t, err := time.Parse("2006-01-02", value); err == nil {
		until := t.Truncate(24 * time.Hour)
		// Compute the duration from now.
		d := time.Until(until)
		// If until is in the past, duration is negative.
		return d, nil
	}

	// Try duration/window format (e.g., "7d", "24h").
	window, err := parseSinceWindow(value)
	if err != nil {
		return 0, fmt.Errorf("invalid until value %q: must be ISO date (2006-01-02) or duration (7d, 24h)", raw)
	}
	// For until, a positive window means "n time units before now".
	return -window, nil
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
