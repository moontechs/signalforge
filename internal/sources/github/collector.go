package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
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
	now       func() time.Time
}

// requestLimits holds the per-run request cap.
type requestLimits struct {
	maxRequests int
}

// New creates a new GitHub Collector with the given configuration.
// It returns an error if the collector would have no usable configuration.
func New(cfg domainCollectorConfig) (*Collector, error) {
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}

	c := &Collector{
		config: configValues{
			Enabled:           cfg.Enabled,
			SearchIssues:      cfg.SearchIssues,
			SearchDiscussions: cfg.SearchDiscussions,
			MaxItemsPerRun:    cfg.MaxItemsPerRun,
			MaxCommentsPerItem: cfg.MaxCommentsPerItem,
			Repositories:      cfg.Repositories,
			Languages:         cfg.Languages,
			Labels:            cfg.Labels,
		},
		limits: requestLimits{
			maxRequests: cfg.MaxRequests,
		},
		transport: &httpTransport{
			client: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		now: time.Now,
	}

	return c, nil
}

// domainCollectorConfig is the public configuration type accepted by New.
// It mirrors config.GitHubConfig + config.LimitsConfig.MaxGitHubRequests.
type domainCollectorConfig struct {
	Enabled           bool
	SearchIssues      bool
	SearchDiscussions bool
	MaxItemsPerRun    int
	MaxCommentsPerItem int
	Repositories      []string
	Languages         []string
	Labels            []string
	MaxRequests       int
}

// Name returns the collector name.
func (c *Collector) Name() string {
	return "github"
}

// WithTransport replaces the HTTP transport (for testing).
func (c *Collector) WithTransport(t transport) *Collector {
	c.transport = t
	return c
}

// WithNow overrides the time function (for testing).
func (c *Collector) WithNow(now func() time.Time) *Collector {
	c.now = now
	return c
}

// Collect retrieves issues and discussions from GitHub and returns RawSignals.
// This is a stub for Task 1 — real implementation starts in Task 5.
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context must not be nil")
	}

	scope := deriveScope(
		c.config,
		c.config.Repositories,
		c.config.Labels,
		c.config.Languages,
		c.config.MaxItemsPerRun,
		c.config.MaxCommentsPerItem,
		req.Since,
	)

	_ = scope // will be used in later tasks
	return []domain.RawSignal{}, nil
}

// ensure interface compliance
var _ domain.SourceCollector = (*Collector)(nil)
