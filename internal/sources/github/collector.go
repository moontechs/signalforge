package github

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
)

const (
	defaultSearchPageSize     = 50
	defaultDiscussionPageSize = 25
)

type githubAPI interface {
	SearchIssues(ctx context.Context, params SearchIssuesParams) (SearchIssuesPage, error)
	ListIssueComments(ctx context.Context, params IssueCommentsParams) (IssueCommentsPage, error)
	SearchDiscussions(ctx context.Context, params DiscussionSearchParams) (DiscussionSearchPage, error)
	ListDiscussionComments(ctx context.Context, params DiscussionCommentsParams) (DiscussionCommentsPage, error)
	Stats() ClientStats
}

// Collector coordinates GitHub searches, parsing, deduplication, and persistence.
type Collector struct {
	cfg    config.GitHubConfig
	api    githubAPI
	store  *storage.Storage
	memory *memory.DefaultMemory
	now    func() time.Time
}

// CollectorConfig wires dependencies for a GitHub collector.
type CollectorConfig struct {
	Config  config.GitHubConfig
	API     githubAPI
	Storage *storage.Storage
	Memory  *memory.DefaultMemory
	Now     func() time.Time
}

// NewCollector constructs a GitHub collector.
func NewCollector(cfg CollectorConfig) (*Collector, error) {
	if err := cfg.Config.Validate(); err != nil {
		return nil, err
	}
	if cfg.API == nil {
		return nil, fmt.Errorf("github api client is required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.Memory == nil {
		return nil, fmt.Errorf("memory is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Collector{
		cfg:    cfg.Config,
		api:    cfg.API,
		store:  cfg.Storage,
		memory: cfg.Memory,
		now:    now,
	}, nil
}

// Name returns the collector source name.
func (c *Collector) Name() string {
	return sourceName
}

// Collect runs the GitHub collection flow for the provided request.
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	options := c.resolveCollectOptions(req)
	if options.maxItems <= 0 {
		return nil, nil
	}

	collectedAt := c.now().UTC()
	var (
		signals []domain.RawSignal
		errs    []error
	)

	if c.cfg.SearchIssues && options.remaining() > 0 {
		issueSignals, err := c.collectIssues(ctx, options, collectedAt, req.Force)
		signals = append(signals, issueSignals...)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if c.cfg.SearchDiscussions && options.remaining() > 0 {
		discussionSignals, err := c.collectDiscussions(ctx, options, collectedAt, req.Force)
		signals = append(signals, discussionSignals...)
		if err != nil {
			errs = append(errs, err)
		}
	}

	persisted := req.DryRun
	if len(signals) > 0 && !req.DryRun {
		if err := c.persistSignals(signals); err != nil {
			errs = append(errs, err)
		} else {
			persisted = true
		}
	} else if len(signals) == 0 {
		persisted = true
	}

	stats := c.api.Stats()
	c.memory.AddGitHubRequests(stats.Requests)

	if persisted && !req.DryRun {
		if err := c.memory.Save(); err != nil {
			errs = append(errs, err)
		}
	}

	return signals, errors.Join(errs...)
}

type collectOptions struct {
	since         time.Time
	maxItems      int
	maxComments   int
	repositories  []string
	languages     []string
	labels        []string
	issuePage     int
	discussionCur string
	collected     int
}

func (o *collectOptions) remaining() int {
	return max(0, o.maxItems-o.collected)
}

func (o *collectOptions) advance() {
	o.collected++
}

func (c *Collector) resolveCollectOptions(req domain.CollectRequest) *collectOptions {
	maxItems := c.cfg.MaxItemsPerRun
	if req.MaxItems > 0 && (maxItems == 0 || req.MaxItems < maxItems) {
		maxItems = req.MaxItems
	}

	maxComments := c.cfg.MaxCommentsPerItem
	if req.MaxCommentsPerItem > 0 {
		maxComments = req.MaxCommentsPerItem
	}

	repositories := cloneStrings(c.cfg.Repositories)
	if len(req.Repositories) > 0 {
		repositories = cloneStrings(req.Repositories)
	}

	languages := cloneStrings(c.cfg.Languages)
	if len(req.Languages) > 0 {
		languages = cloneStrings(req.Languages)
	}

	labels := cloneStrings(c.cfg.Labels)
	if len(req.Labels) > 0 {
		labels = cloneStrings(req.Labels)
	}

	since := req.Since.UTC()
	if since.IsZero() && req.SinceWindow > 0 {
		since = c.now().Add(-req.SinceWindow).UTC()
	}

	issuePage := 1
	if rawPage := strings.TrimSpace(req.Cursor["github_issues_page"]); rawPage != "" {
		if parsed, err := strconv.Atoi(rawPage); err == nil && parsed > 0 {
			issuePage = parsed
		}
	}

	return &collectOptions{
		since:         since,
		maxItems:      maxItems,
		maxComments:   max(0, maxComments),
		repositories:  repositories,
		languages:     languages,
		labels:        labels,
		issuePage:     issuePage,
		discussionCur: strings.TrimSpace(req.Cursor["github_discussions_after"]),
	}
}

func (c *Collector) collectIssues(
	ctx context.Context,
	options *collectOptions,
	collectedAt time.Time,
	force bool,
) ([]domain.RawSignal, error) {
	if options.remaining() <= 0 {
		return nil, nil
	}

	query := buildIssueQuery(options.repositories, options.languages, options.labels, options.since)
	page := max(1, options.issuePage)
	perPage := min(defaultSearchPageSize, options.remaining())
	var (
		signals []domain.RawSignal
		errs    []error
	)

	for options.remaining() > 0 {
		pageResult, err := c.api.SearchIssues(ctx, SearchIssuesParams{
			Query:   query,
			Sort:    "updated",
			Order:   "desc",
			Page:    page,
			PerPage: max(1, perPage),
		})
		if err != nil {
			return signals, fmt.Errorf("collect github issues: %w", err)
		}

		for _, item := range pageResult.Response.Items {
			if options.remaining() <= 0 {
				break
			}

			sourceID := buildIssueSourceID(item)
			if !force && c.memory.HasRawSignal(sourceName, sourceID) {
				c.memory.IncrementStat("raw_signals_skipped")
				continue
			}

			comments, err := c.fetchIssueComments(ctx, item, options.maxComments)
			if err != nil {
				errs = append(errs, fmt.Errorf("issue %s comments: %w", sourceID, err))
			}

			signal := ParseIssue(item, comments, ParseOptions{
				CollectedAt: collectedAt,
				MaxComments: options.maxComments,
			})
			if !force && c.memory.HasContentHash(signal.ContentHash) {
				c.memory.IncrementStat("raw_signals_skipped")
				continue
			}

			c.memory.AddRawSignal(signal.Source, signal.SourceID)
			c.memory.AddContentHash(signal.ContentHash, signal.ID)
			signals = append(signals, signal)
			options.advance()
		}

		if !pageResult.HasNext || len(pageResult.Response.Items) == 0 {
			break
		}
		page++
		perPage = min(defaultSearchPageSize, options.remaining())
	}

	return signals, errors.Join(errs...)
}

func (c *Collector) fetchIssueComments(ctx context.Context, item IssueItem, maxComments int) ([]IssueComment, error) {
	if maxComments == 0 || item.Comments == 0 {
		return nil, nil
	}

	owner, repo, ok := splitRepository(item.RepositoryName, item.RepositoryURL)
	if !ok {
		return nil, fmt.Errorf("missing repository for issue %d", item.Number)
	}

	page := 1
	remaining := maxComments
	collected := make([]IssueComment, 0, min(item.Comments, maxComments))
	for remaining > 0 {
		pageResult, err := c.api.ListIssueComments(ctx, IssueCommentsParams{
			Owner:     owner,
			Repo:      repo,
			IssueNum:  item.Number,
			Page:      page,
			PerPage:   min(defaultSearchPageSize, remaining),
			Sort:      "created",
			Direction: "asc",
		})
		if err != nil {
			return collected, err
		}

		collected = append(collected, pageResult.Comments...)
		remaining = maxComments - len(collected)
		if !pageResult.HasNext || len(pageResult.Comments) == 0 {
			break
		}
		page++
	}

	if len(collected) > maxComments {
		collected = collected[:maxComments]
	}
	return collected, nil
}

func (c *Collector) collectDiscussions(
	ctx context.Context,
	options *collectOptions,
	collectedAt time.Time,
	force bool,
) ([]domain.RawSignal, error) {
	if options.remaining() <= 0 {
		return nil, nil
	}

	query := buildDiscussionQuery(options.repositories, options.languages, options.labels, options.since)
	after := options.discussionCur
	first := min(defaultDiscussionPageSize, options.remaining())
	var (
		signals []domain.RawSignal
		errs    []error
	)

	for options.remaining() > 0 {
		pageResult, err := c.api.SearchDiscussions(ctx, DiscussionSearchParams{
			Query: query,
			First: max(1, first),
			After: after,
		})
		if err != nil {
			return signals, fmt.Errorf("collect github discussions: %w", err)
		}

		nodes := pageResult.Response.Data.Search.Nodes
		for _, item := range nodes {
			if options.remaining() <= 0 {
				break
			}

			sourceID := buildDiscussionSourceID(item)
			if !force && c.memory.HasRawSignal(sourceName, sourceID) {
				c.memory.IncrementStat("raw_signals_skipped")
				continue
			}

			comments, err := c.fetchDiscussionComments(ctx, item, options.maxComments)
			if err != nil {
				errs = append(errs, fmt.Errorf("discussion %s comments: %w", sourceID, err))
			}

			signal := ParseDiscussion(item, comments, ParseOptions{
				CollectedAt: collectedAt,
				MaxComments: options.maxComments,
			})
			if !force && c.memory.HasContentHash(signal.ContentHash) {
				c.memory.IncrementStat("raw_signals_skipped")
				continue
			}

			c.memory.AddRawSignal(signal.Source, signal.SourceID)
			c.memory.AddContentHash(signal.ContentHash, signal.ID)
			signals = append(signals, signal)
			options.advance()
		}

		pageInfo := pageResult.Response.Data.Search.PageInfo
		if !pageInfo.HasNextPage || len(nodes) == 0 {
			break
		}
		after = pageInfo.EndCursor
		first = min(defaultDiscussionPageSize, options.remaining())
	}

	return signals, errors.Join(errs...)
}

func (c *Collector) fetchDiscussionComments(
	ctx context.Context,
	item Discussion,
	maxComments int,
) ([]DiscussionComment, error) {
	if maxComments == 0 {
		return nil, nil
	}

	collected := append([]DiscussionComment{}, item.Comments.Nodes...)
	if len(collected) >= maxComments || !item.Comments.PageInfo.HasNextPage {
		if len(collected) > maxComments {
			collected = collected[:maxComments]
		}
		return collected, nil
	}

	after := item.Comments.PageInfo.EndCursor
	hasNext := item.Comments.PageInfo.HasNextPage
	for len(collected) < maxComments && hasNext {
		pageResult, err := c.api.ListDiscussionComments(ctx, DiscussionCommentsParams{
			DiscussionID: item.ID,
			First:        min(defaultDiscussionPageSize, maxComments-len(collected)),
			After:        after,
		})
		if err != nil {
			return collected, err
		}

		page := pageResult.Response.Data.Node.Comments
		collected = append(collected, page.Nodes...)
		hasNext = page.PageInfo.HasNextPage
		if !hasNext || len(page.Nodes) == 0 {
			break
		}
		after = page.PageInfo.EndCursor
	}

	if len(collected) > maxComments {
		collected = collected[:maxComments]
	}
	return collected, nil
}

func (c *Collector) persistSignals(signals []domain.RawSignal) error {
	path := filepath.Join(c.store.BaseDir(), "raw-signals", fmt.Sprintf("github-%s.jsonl", c.now().UTC().Format("2006-01-02")))
	for _, signal := range signals {
		if err := c.store.SaveJSONL(path, signal); err != nil {
			return fmt.Errorf("persist raw signals: %w", err)
		}
	}
	return nil
}

func buildIssueQuery(repositories, languages, labels []string, since time.Time) string {
	parts := []string{"is:issue", "archived:false"}
	parts = append(parts, queryTerms("repo", repositories)...)
	parts = append(parts, queryTerms("language", languages)...)
	parts = append(parts, queryTerms("label", labels)...)
	if !since.IsZero() {
		parts = append(parts, "updated:>="+since.UTC().Format("2006-01-02"))
	}
	return strings.Join(parts, " ")
}

func buildDiscussionQuery(repositories, languages, labels []string, since time.Time) string {
	parts := []string{"is:discussion", "archived:false"}
	parts = append(parts, queryTerms("repo", repositories)...)
	parts = append(parts, queryTerms("language", languages)...)
	parts = append(parts, queryTerms("label", labels)...)
	if !since.IsZero() {
		parts = append(parts, "updated:>="+since.UTC().Format("2006-01-02"))
	}
	return strings.Join(parts, " ")
}

func queryTerms(prefix string, values []string) []string {
	terms := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		terms = append(terms, prefix+":"+value)
	}
	return terms
}

func splitRepository(repositoryName, repositoryURL string) (string, string, bool) {
	repositoryName = normalizeRepositoryName(repositoryName, repositoryURL)
	parts := strings.Split(repositoryName, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cloned = append(cloned, value)
	}
	return cloned
}
