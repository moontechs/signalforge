package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/domain"
)

// Collector orchestrates Reddit listing and comment collection.
type Collector struct {
	config    ConfigValues
	client    *client
	now       func() time.Time
	collectMu sync.Mutex
	mu        sync.Mutex
	stats     Stats
}

func New(cfg *ConfigValues) (*Collector, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, ErrDisabled
	}
	for _, s := range cfg.Subreddits {
		s = strings.TrimSpace(s)
		if s == "" || strings.ContainsAny(s, "/?#") {
			return nil, fmt.Errorf("%w: %q", ErrInvalidSubreddit, s)
		}
	}
	t := &http.Client{Timeout: 30 * time.Second}
	return &Collector{config: *cfg, client: newClient(t, cfg), now: time.Now}, nil
}

func (c *Collector) Name() string                            { return SourceName }
func (c *Collector) WithTransport(t transport) *Collector    { c.client.transport = t; return c }
func (c *Collector) WithNow(now func() time.Time) *Collector { c.now = now; return c }
func (c *Collector) WithCache(value *cache.Cache) *Collector { c.client.WithCache(value); return c }
func (c *Collector) Stats() Stats                            { c.mu.Lock(); defer c.mu.Unlock(); return c.stats }

func (c *Collector) effectiveScope(req *domain.CollectRequest) collectionScope {
	scope := deriveScope(&c.config, req.Since)
	if len(req.Subreddits) > 0 {
		scope.subreddits = req.Subreddits
	}
	if req.MaxItems > 0 {
		scope.maxPosts = req.MaxItems
	}
	if req.MaxCommentsPerItem > 0 {
		scope.maxComments = req.MaxCommentsPerItem
	}
	return scope
}

func addEligiblePosts(listing *listingResponse, seen map[string]struct{}, posts *[]postResponse, since time.Time, limit int) {
	for index := range listing.Data.Children {
		child := &listing.Data.Children[index]
		if child.Kind != "t3" || child.Data.ID == "" {
			continue
		}
		if _, ok := seen[child.Data.ID]; ok {
			continue
		}
		seen[child.Data.ID] = struct{}{}
		if !eligiblePost(&child.Data, since) {
			continue
		}
		*posts = append(*posts, child.Data)
		if len(*posts) >= limit {
			return
		}
	}
}

func (c *Collector) collectSubreddit(ctx context.Context, subreddit string, scope *collectionScope, since time.Time, seen map[string]struct{}, posts *[]postResponse) (bool, error) {
	after := ""
	for len(*posts) < scope.maxPosts {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		listing, err := c.client.listing(ctx, strings.TrimSpace(subreddit), scope, after)
		if err != nil {
			return errors.Is(err, ErrRequestCap), fmt.Errorf("listing r/%s: %w", subreddit, err)
		}
		addEligiblePosts(&listing, seen, posts, since, scope.maxPosts)
		if len(*posts) >= scope.maxPosts {
			return true, nil
		}
		if listing.Data.After == "" || listing.Data.After == after || len(listing.Data.Children) == 0 {
			return false, nil
		}
		after = listing.Data.After
	}
	return true, nil
}

func (c *Collector) collectPosts(ctx context.Context, scope *collectionScope, since time.Time) ([]postResponse, []error) {
	seen := make(map[string]struct{})
	posts := make([]postResponse, 0, scope.maxPosts)
	var errs []error
	for _, subreddit := range scope.subreddits {
		stop, err := c.collectSubreddit(ctx, subreddit, scope, since, seen, &posts)
		if err != nil {
			errs = append(errs, err)
		}
		if stop {
			break
		}
	}
	return posts, errs
}

func (c *Collector) collectSignals(ctx context.Context, posts []postResponse, scope *collectionScope, collectedAt time.Time) ([]domain.RawSignal, []error) {
	var mu sync.Mutex
	var errs []error
	addErr := func(err error) { mu.Lock(); errs = append(errs, err); mu.Unlock() }
	results := make([]domain.RawSignal, 0, len(posts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for index := range posts {
		if ctx.Err() != nil {
			break
		}
		post := &posts[index]
		wg.Add(1)
		go func(p *postResponse) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var comments *listingResponse
			if scope.maxComments > 0 {
				cs, err := c.client.comments(ctx, p.Subreddit, p.ID, scope.maxComments)
				if err != nil {
					addErr(fmt.Errorf("comments %s: %w", p.ID, err))
				} else if len(cs) > 1 {
					comments = &cs[1]
				}
			}
			signal := parsePost(p, collectedAt, scope.maxComments, comments)
			mu.Lock()
			results = append(results, signal)
			mu.Unlock()
		}(post)
	}
	wg.Wait()
	sortSignalsNewestFirst(results)
	return results, errs
}

func (c *Collector) storeStats(before Stats) {
	after := c.client.Stats()
	c.mu.Lock()
	c.stats = Stats{Requests: after.Requests - before.Requests, CacheHits: after.CacheHits - before.CacheHits}
	c.mu.Unlock()
}

func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) { //nolint:gocritic // Value signature is required by domain.SourceCollector.
	c.collectMu.Lock()
	defer c.collectMu.Unlock()
	c.client.beginRun()
	before := c.client.Stats()
	scope := c.effectiveScope(&req)
	posts, errs := c.collectPosts(ctx, &scope, req.Since)
	results, commentErrs := c.collectSignals(ctx, posts, &scope, c.now())
	errs = append(errs, commentErrs...)
	c.storeStats(before)
	if err := ctx.Err(); err != nil && !errors.Is(errors.Join(errs...), err) {
		errs = append(errs, err)
	}
	return results, errors.Join(errs...)
}

var _ domain.SourceCollector = (*Collector)(nil)
