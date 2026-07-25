package stackexchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/domain"
)

// Collector fetches questions from configured Stack Exchange sites.
type Collector struct {
	config ConfigValues
	client *client
	now    func() time.Time
	stats  Stats
}

// New creates a Stack Exchange collector. A nil client creates the default API client.
func New(cfg *ConfigValues, apiClient *client) *Collector {
	if cfg == nil {
		cfg = &ConfigValues{}
	}
	if apiClient == nil {
		apiClient = newClient(&httpTransport{client: &http.Client{Timeout: 30 * time.Second}}, cfg)
	}
	return &Collector{config: *cfg, client: apiClient, now: time.Now}
}

func (c *Collector) Name() string { return SourceName }

// WithTransport replaces the API transport, primarily for tests.
func (c *Collector) WithTransport(t transport) *Collector { c.client.transport = t; return c }

// WithCache attaches response caching.
func (c *Collector) WithCache(value *cache.Cache) { c.client.WithCache(value) }

// Stats returns this collector's most recent run counters.
func (c *Collector) Stats() Stats { return c.stats }

// processParsedSignals filters parsed signals through run-local deduplication
// and appends new ones to the result slice.
func (c *Collector) processParsedSignals(parsed []domain.RawSignal, seenIDs, seenHashes map[string]struct{}, maxItems int, signals []domain.RawSignal) []domain.RawSignal {
	for i := range parsed {
		sig := &parsed[i]
		if _, ok := seenIDs[sig.ID]; ok {
			continue
		}
		if sig.ContentHash != "" {
			if _, ok := seenHashes[sig.ContentHash]; ok {
				continue
			}
		}
		if maxItems > 0 && len(signals) >= maxItems {
			break
		}
		signals = append(signals, *sig)
		seenIDs[sig.ID] = struct{}{}
		if sig.ContentHash != "" {
			seenHashes[sig.ContentHash] = struct{}{}
		}
	}
	return signals
}

// collectSite collects questions from one site across pages and filters them
// through run-local deduplication.
func (c *Collector) collectSite(ctx context.Context, site string, from, to int64, pageSize, maxPages, maxItems int, since time.Time, seenIDs, seenHashes map[string]struct{}) ([]domain.RawSignal, error) {
	var signals []domain.RawSignal
	for page := 1; page <= maxPages && (maxItems <= 0 || len(signals) < maxItems); page++ {
		if err := ctx.Err(); err != nil {
			return signals, err
		}
		resp, err := c.client.questions(ctx, site, from, to, page, pageSize, APIFieldFilter)
		if err != nil && resp == nil {
			return signals, fmt.Errorf("site %s: %w", site, err)
		}
		eligible := make([]questionDTO, 0, len(resp.Items))
		for i := range resp.Items {
			q := &resp.Items[i]
			if eligibleQuestion(q, QuestionScope{MinimumScore: c.config.MinimumScore, MinimumViews: c.config.MinimumViews, Since: since}) {
				eligible = append(eligible, *q)
			}
		}
		parsed, _ := parseQuestionsWithStats(site, eligible, c.config.MinimumScore, c.config.MinimumViews)
		signals = c.processParsedSignals(parsed, seenIDs, seenHashes, maxItems, signals)
		if err != nil {
			// getQuestions may return a parsed page alongside quota exhaustion;
			// preserve that page, but report the exhaustion to the caller.
			return signals, fmt.Errorf("site %s: %w", site, err)
		}
		if !resp.HasMore || len(resp.Items) == 0 {
			break
		}
	}
	return signals, nil
}

// Collect implements domain.SourceCollector.
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) { //nolint:gocritic // heavy param required by SourceCollector interface
	if !c.config.Enabled {
		return nil, ErrDisabled
	}
	if len(c.config.Sites) == 0 {
		return nil, ErrNoSitesConfigured
	}
	from := req.Since.Unix()
	to := req.Until.Unix()
	if req.Since.IsZero() {
		from = 0
	}
	if req.Until.IsZero() {
		to = c.now().Unix()
	}
	pageSize, maxPages := c.config.PageSize, c.config.MaxPagesPerSite
	if pageSize <= 0 {
		pageSize = 25
	}
	if maxPages <= 0 {
		maxPages = 1
	}
	var signals []domain.RawSignal
	var errs []error
	seenIDs := make(map[string]struct{})
	seenHashes := make(map[string]struct{})
	before := c.client.Stats()
	for _, site := range c.config.Sites {
		if err := ctx.Err(); err != nil {
			c.updateStats()
			return signals, err
		}
		siteSignals, err := c.collectSite(ctx, site, from, to, pageSize, maxPages, c.config.MaxItemsPerSite, req.Since, seenIDs, seenHashes)
		if err != nil {
			errs = append(errs, err)
			// collectSite returns partial results alongside errors (e.g. quota exhaustion).
			signals = append(signals, siteSignals...)
			continue
		}
		signals = append(signals, siteSignals...)
	}
	after := c.client.Stats()
	c.stats = Stats{Requests: after.Requests - before.Requests, CacheHits: after.CacheHits - before.CacheHits}
	return signals, errors.Join(errs...)
}

func (c *Collector) updateStats() { s := c.client.Stats(); c.stats = s }

var _ domain.SourceCollector = (*Collector)(nil)
