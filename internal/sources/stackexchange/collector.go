package stackexchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/storage"
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
		apiClient = newClient(&httpTransport{client: &http.Client{Timeout: 30 * time.Second}}, *cfg)
	}
	return &Collector{config: *cfg, client: apiClient, now: time.Now}
}

func (c *Collector) Name() string { return SourceName }

// WithTransport replaces the API transport, primarily for tests.
func (c *Collector) WithTransport(t transport) *Collector { c.client.transport = t; return c }

// WithCache attaches response caching and persistent signal memory.
func (c *Collector) WithCache(store *storage.Storage) *Collector { c.client.WithCache(store); return c }

// Stats returns this collector's most recent run counters.
func (c *Collector) Stats() Stats { return c.stats }

// Collect implements domain.SourceCollector.
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
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
	var mem *memory.DefaultMemory
	if c.client.store != nil {
		mem = memory.New(c.client.store)
		_ = mem.Load()
	}
	var signals []domain.RawSignal
	var errs []error
	before := c.client.Stats()
	for _, site := range c.config.Sites {
		if err := ctx.Err(); err != nil {
			c.updateStats()
			return signals, err
		}
		items := 0
		seen := make(map[string]struct{})
		for page := 1; page <= maxPages && items < c.config.MaxItemsPerSite; page++ {
			if err := ctx.Err(); err != nil {
				c.updateStats()
				return signals, err
			}
			resp, err := c.client.getQuestions(ctx, site, from, to, page, pageSize, APIFieldFilter)
			if err != nil && resp == nil {
				errs = append(errs, fmt.Errorf("site %s: %w", site, err))
				break
			}
			parsed, _ := parseQuestionsWithStats(site, resp.Items, c.config.MinimumScore, c.config.MinimumViews)
			for _, signal := range parsed {
				if _, ok := seen[signal.ID]; ok {
					continue
				}
				seen[signal.ID] = struct{}{}
				if mem != nil && (mem.HasRawSignal(SourceName, signal.SourceID) || mem.HasContentHash(signal.ContentHash)) {
					continue
				}
				if c.config.MaxItemsPerSite > 0 && items >= c.config.MaxItemsPerSite {
					break
				}
				signals = append(signals, signal)
				items++
				if mem != nil {
					mem.AddRawSignal(SourceName, signal.SourceID)
					mem.AddContentHash(signal.ContentHash, signal.ID)
				}
			}
			if err != nil {
				// getQuestions may return a parsed page alongside quota exhaustion;
				// preserve that page, but report the exhaustion to the caller.
				errs = append(errs, fmt.Errorf("site %s: %w", site, err))
				break
			}
			if !resp.HasMore || len(resp.Items) == 0 {
				break
			}
		}
	}
	if mem != nil {
		_ = mem.Save()
	}
	after := c.client.Stats()
	c.stats = Stats{Requests: after.Requests - before.Requests, CacheHits: after.CacheHits - before.CacheHits}
	return signals, errors.Join(errs...)
}

func (c *Collector) updateStats() { s := c.client.Stats(); c.stats = s }

var _ domain.SourceCollector = (*Collector)(nil)
