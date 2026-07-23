package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// transport is the pluggable HTTP round-tripper for testability.
// Production uses httpTransport; tests use fakeTransport.
type transport interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpTransport wraps http.Client to satisfy the transport interface.
type httpTransport struct {
	client *http.Client
}

func (t *httpTransport) Do(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

// Collector collects GitHub Issues and Discussions as RawSignals.
type Collector struct {
	config    configValues
	limits    requestLimits
	transport transport
	client    *githubClient
	now       func() time.Time
}

// requestLimits holds the per-run request cap.
type requestLimits struct {
	maxRequests int
}

// New creates a new GitHub Collector with the given configuration.
// It returns an error if the collector would have no usable configuration.
func New(cfg CollectorConfig) (*Collector, error) {
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}

	transport := &httpTransport{
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	c := &Collector{
		config: configValues{
			Enabled:            cfg.Enabled,
			SearchIssues:       cfg.SearchIssues,
			SearchDiscussions:  cfg.SearchDiscussions,
			MaxItemsPerRun:     cfg.MaxItemsPerRun,
			MaxCommentsPerItem: cfg.MaxCommentsPerItem,
			Repositories:       cfg.Repositories,
			Languages:          cfg.Languages,
			Labels:             cfg.Labels,
		},
		limits: requestLimits{
			maxRequests: cfg.MaxRequests,
		},
		transport: transport,
		now:       time.Now,
	}

	c.client = newClient(transport, cfg.MaxRequests)
	return c, nil
}

// domainCollectorConfig is the public configuration type accepted by New.
// It mirrors config.GitHubConfig + config.LimitsConfig.MaxGitHubRequests.
type CollectorConfig struct {
	Enabled            bool
	SearchIssues       bool
	SearchDiscussions  bool
	MaxItemsPerRun     int
	MaxCommentsPerItem int
	Repositories       []string
	Languages          []string
	Labels             []string
	MaxRequests        int
}

// Name returns the collector name.
func (c *Collector) Name() string {
	return "github"
}

// WithTransport replaces the HTTP transport (for testing).
// It also recreates the internal client with the new transport.
func (c *Collector) WithTransport(t transport) *Collector {
	c.transport = t
	c.client = newClient(t, c.limits.maxRequests)
	return c
}

// WithNow overrides the time function (for testing).
func (c *Collector) WithNow(now func() time.Time) *Collector {
	c.now = now
	return c
}

// WithCache attaches an on-disk response cache to the collector's internal client.
func (c *Collector) WithCache(store *storage.Storage) *Collector {
	c.client = c.client.WithCache(store)
	return c
}

// Collect retrieves issues and discussions from GitHub and returns RawSignals.
// It orchestrates the full collection pipeline:
//  1. Derive collection scope from config + request
//  2. Fetch issues (REST) if SearchIssues is enabled
//  3. Fetch discussions (GraphQL) if SearchDiscussions is enabled
//  4. Parse both into domain.RawSignal
//  5. Dedup by signal ID within the run
//  6. Enforce max-item limit
//  7. Return combined results with partial errors wrapped
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	// Format since as RFC3339 string for the scope.
	var sinceStr string
	if !req.Since.IsZero() {
		sinceStr = req.Since.Format(time.RFC3339)
	}

	scope := deriveScope(
		c.config,
		c.config.Repositories,
		c.config.Labels,
		c.config.Languages,
		c.config.MaxItemsPerRun,
		c.config.MaxCommentsPerItem,
		sinceStr,
	)

	var signals []domain.RawSignal
	var errs []error
	collectedAt := c.now()

	// 1. Fetch issues (REST).
	if scope.searchIssues {
		issues, err := fetchIssues(ctx, c.client, scope)
		if err != nil {
			errs = append(errs, fmt.Errorf("github issues: %w", err))
		} else {
			parsed := parseIssues(ctx, c.client, issues, scope, collectedAt)
			signals = append(signals, parsed...)
		}
	}

	// 2. Fetch discussions (GraphQL).
	if scope.searchDiscussions {
		discussions, err := fetchDiscussions(ctx, c.client, nil, scope)
		if err != nil {
			errs = append(errs, fmt.Errorf("github discussions: %w", err))
		} else {
			parsed := parseDiscussions(discussions, scope, collectedAt)
			signals = append(signals, parsed...)
		}
	}

	// 3. Dedup within this run (by signal ID).
	seen := make(map[string]bool, len(signals))
	deduped := make([]domain.RawSignal, 0, len(signals))
	for _, s := range signals {
		if !seen[s.ID] {
			seen[s.ID] = true
			deduped = append(deduped, s)
		}
	}
	signals = deduped

	// 4. Enforce max-item limit.
	if scope.maxItems > 0 && len(signals) > scope.maxItems {
		signals = signals[:scope.maxItems]
	}

	// 5. Return combined results with partial errors.
	if len(errs) > 0 {
		return signals, fmt.Errorf("github collector: %w", errors.Join(errs...))
	}

	return signals, nil
}

// parseIssues parses a slice of ghIssue into domain.RawSignal, fetching comments
// for each issue. Issues where owner/repo cannot be determined are skipped.
func parseIssues(ctx context.Context, c *githubClient, issues []ghIssue, scope collectionScope, collectedAt time.Time) []domain.RawSignal {
	signals := make([]domain.RawSignal, 0, len(issues))

	for i := range issues {
		issue := &issues[i]

		// Extract owner/repo from repository_url or fall back to HTML URL.
		owner, repo := extractOwnerRepo(issue.RepoURL)
		if owner == "" || repo == "" {
			owner, repo = extractOwnerRepoFromHTML(issue.HTMLURL)
		}
		if owner == "" || repo == "" {
			continue
		}

		// Fetch comments if maxComments is set.
		var fetchErr error
		var comments []ghIssueComment
		if scope.maxComments > 0 {
			comments, fetchErr = fetchIssueComments(ctx, c, owner, repo, issue.Number, scope.maxComments)
			if fetchErr != nil {
				comments = nil
			}
		}

		signal := parseIssueToSignal(issue, owner, repo, comments, scope.maxComments, collectedAt)
		signals = append(signals, signal)
	}

	return signals
}

// parseDiscussions parses a slice of graphQLDiscussionNode into domain.RawSignal.
// Discussions where owner/repo cannot be determined from the URL are skipped.
func parseDiscussions(discussions []graphQLDiscussionNode, scope collectionScope, collectedAt time.Time) []domain.RawSignal {
	signals := make([]domain.RawSignal, 0, len(discussions))

	for i := range discussions {
		disc := &discussions[i]

		// Extract owner/repo from HTML URL.
		owner, repo := extractOwnerRepoFromHTML(disc.URL)
		if owner == "" || repo == "" {
			continue
		}

		signal := parseDiscussionToSignal(disc, owner, repo, scope.maxComments, collectedAt)
		signals = append(signals, signal)
	}

	return signals
}

// ensure interface compliance.
var _ domain.SourceCollector = (*Collector)(nil)
