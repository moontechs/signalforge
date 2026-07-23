package hackernews

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// Collector implements domain.SourceCollector for Hacker News.
// It orchestrates feed scanning, item fetching (with a bounded worker pool),
// score filtering, comment flattening, and deduplication in a single run.
type Collector struct {
	config    ConfigValues
	client    *client
	now       func() time.Time
	mu        sync.Mutex
	requests  int
	cacheHits int
}

// New creates a new Hacker News Collector.
// Returns ErrDisabled if cfg.Enabled is false, ErrInvalidFeed if any feed
// name is not in SupportedFeeds.
func New(cfg *ConfigValues) (*Collector, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	for _, feed := range cfg.Feeds {
		valid := false
		for _, supported := range SupportedFeeds {
			if feed == supported {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("%w: %s", ErrInvalidFeed, feed)
		}
	}

	transport := &httpTransport{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	c := &Collector{
		config: *cfg,
		now:    time.Now,
	}
	c.client = newClient(transport, *cfg)
	return c, nil
}

// Name returns the collector name ("hackernews").
func (c *Collector) Name() string {
	return SourceName
}

// WithTransport replaces the HTTP transport (for testing).
func (c *Collector) WithTransport(t transport) *Collector {
	c.client.transport = t
	return c
}

// WithNow overrides the time function (for testing).
func (c *Collector) WithNow(now func() time.Time) *Collector {
	c.now = now
	return c
}

// WithCache attaches an on-disk response cache.
func (c *Collector) WithCache(store *storage.Storage) *Collector {
	c.client.WithCache(store)
	return c
}

// Stats returns the request and cache-hit counts from the last collection.
func (c *Collector) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Requests: c.requests, CacheHits: c.cacheHits}
}

// Collect implements domain.SourceCollector.
//
// The collection pipeline is:
//  1. Derive collection scope from config and request.Since
//  2. Scan each configured feed, deduplicate item IDs across feeds
//  3. Fetch items through a bounded worker pool (5 concurrent workers)
//  4. Filter by eligibleStory (type, score, since window)
//  5. Flatten comments (BFS) for qualifying items
//  6. Sort results by CreatedAt descending
//  7. Apply max-items cap
//  8. Return results with any partial errors joined
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	scope := deriveScope(&c.config, req.Since)

	// Record client stats before collection to compute delta.
	beforeStats := c.client.Stats()

	// Dedup set for item IDs across feeds.
	seen := make(map[int]bool)
	var candidateIDs []int
	var feedErrs []error

	// 1. Scan feeds in order, deduplicate IDs.
	for _, feedName := range scope.feeds {
		ids, err := c.client.feed(ctx, feedName)
		if err != nil {
			feedErrs = append(feedErrs, fmt.Errorf("feed %s: %w", feedName, err))
			continue
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				candidateIDs = append(candidateIDs, id)
			}
		}
	}

	// 2. Process candidates through bounded worker pool (5 workers).
	var (
		mu       sync.Mutex
		signals  []domain.RawSignal
		itemErrs []error
		itemMu   sync.Mutex
		wg       sync.WaitGroup
		sem      = make(chan struct{}, 5)
	)

	for _, id := range candidateIDs {
		select {
		case <-ctx.Done():
			wg.Wait()
			c.storeStatsDelta(beforeStats)
			return signals, ctx.Err()
		default:
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(itemID int) {
			defer wg.Done()
			defer func() { <-sem }()

			item, err := c.client.item(ctx, itemID)
			if err != nil {
				itemMu.Lock()
				itemErrs = append(itemErrs, fmt.Errorf("item %d: %w", itemID, err))
				itemMu.Unlock()
				return
			}

			if !eligibleStory(item, scope.since, scope.minimumScore) {
				return
			}

			var comments []domain.Comment
			if scope.maxComments > 0 {
				comments, err = flattenComments(ctx, item, c.client, scope.maxComments)
				if err != nil {
					itemMu.Lock()
					itemErrs = append(itemErrs, fmt.Errorf("flatten comments for item %d: %w", itemID, err))
					itemMu.Unlock()
					return
				}
			}

			signal := parseStory(item, comments, "story", c.now())

			mu.Lock()
			signals = append(signals, signal)
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	// 3. Sort by CreatedAt descending (newest first).
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].CreatedAt.After(signals[j].CreatedAt)
	})

	// 4. Apply maxItems cap.
	if scope.maxItems > 0 && len(signals) > scope.maxItems {
		signals = signals[:scope.maxItems]
	}

	// 5. Store per-run stats delta.
	c.storeStatsDelta(beforeStats)

	// 6. Return results with partial errors.
	if len(feedErrs) > 0 || len(itemErrs) > 0 {
		allErrs := make([]error, 0, len(feedErrs)+len(itemErrs))
		allErrs = append(allErrs, feedErrs...)
		allErrs = append(allErrs, itemErrs...)
		return signals, errors.Join(allErrs...)
	}

	return signals, nil
}

// storeStatsDelta computes the delta of client stats since beforeStats and
// stores it as the per-run request/cache-hit counts.
func (c *Collector) storeStatsDelta(beforeStats Stats) {
	afterStats := c.client.Stats()
	delta := Stats{
		Requests:  afterStats.Requests - beforeStats.Requests,
		CacheHits: afterStats.CacheHits - beforeStats.CacheHits,
	}
	c.mu.Lock()
	c.requests = delta.Requests
	c.cacheHits = delta.CacheHits
	c.mu.Unlock()
}

// Ensure interface compliance.
var _ domain.SourceCollector = (*Collector)(nil)
