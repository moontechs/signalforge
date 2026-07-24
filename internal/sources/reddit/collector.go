package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
	"github.com/moontechs/signalforge/internal/domain"
)

// Collector implements domain.SourceCollector for Reddit.
//
// It orchestrates subreddit listing scanning, post filtering, comment-tree
// fetching (with a bounded worker pool), comment flattening, and signal
// construction in a single run.
type Collector struct {
	config    ConfigValues
	client    *client
	now       func() time.Time
	mu        sync.Mutex
	requests  int
	cacheHits int
}

// New creates a new Reddit Collector.
// Returns ErrDisabled if cfg.Enabled is false.
// May return ErrMissingCredentials if Reddit is enabled but clientID or
// clientSecret is empty.
func New(cfg *ConfigValues, clientID, clientSecret string) (*Collector, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
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
	c.client = newClient(transport, clientID, clientSecret, cfg.MaxRequests)
	return c, nil
}

// Name returns the collector name ("reddit").
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
func (c *Collector) WithCache(value *cache.Cache) *Collector {
	c.client = c.client.WithCache(value)
	return c
}

// Stats returns the request and cache-hit counts from the last collection.
func (c *Collector) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Requests: c.requests, CacheHits: c.cacheHits}
}

// collectScope is the validated and normalised plan for a single collection run.
type collectScope struct {
	subreddits  []string
	sort        string
	timeFilter  string
	maxPosts    int
	maxComments int
	since       time.Time
	maxRequests int
}

// buildScope validates, normalises, and deduplicates subreddit names and
// derives a concrete collection plan from ConfigValues and CollectRequest.
func buildScope(cfg *ConfigValues, req *domain.CollectRequest) (*collectScope, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}

	// Validate and normalise subreddits.
	subreddits, err := normalizeSubreddits(cfg.Subreddits)
	if err != nil {
		return nil, err
	}
	if len(subreddits) == 0 {
		return nil, fmt.Errorf("%w: at least one subreddit is required", ErrInvalidSubreddit)
	}

	// Validate sort.
	sort := cfg.Sort
	if sort == "" {
		sort = DefaultSort
	}
	validSort := false
	for _, s := range SupportedSortValues {
		if s == sort {
			validSort = true
			break
		}
	}
	if !validSort {
		return nil, fmt.Errorf("%w: %q (supported: %v)", ErrInvalidSort, sort, SupportedSortValues)
	}

	// Validate time filter.
	timeFilter := cfg.Time
	if timeFilter == "" {
		timeFilter = DefaultTime
	}
	validTime := false
	for _, t := range SupportedTimeValues {
		if t == timeFilter {
			validTime = true
			break
		}
	}
	if !validTime {
		return nil, fmt.Errorf("%w: %q (supported: %v)", ErrInvalidTime, timeFilter, SupportedTimeValues)
	}

	maxPosts := cfg.MaxPostsPerRun
	if req.MaxItems > 0 && req.MaxItems < maxPosts {
		maxPosts = req.MaxItems
	}
	if maxPosts <= 0 {
		maxPosts = 200
	}

	maxComments := cfg.MaxCommentsPerPost
	if req.MaxCommentsPerItem > 0 && req.MaxCommentsPerItem < maxComments {
		maxComments = req.MaxCommentsPerItem
	}
	if maxComments < 0 {
		maxComments = 0
	}

	return &collectScope{
		subreddits:  subreddits,
		sort:        sort,
		timeFilter:  timeFilter,
		maxPosts:    maxPosts,
		maxComments: maxComments,
		since:       req.Since,
		maxRequests: cfg.MaxRequests,
	}, nil
}

// normalizeSubreddits validates, normalises, and deduplicates subreddit names.
func normalizeSubreddits(subreddits []string) ([]string, error) {
	if len(subreddits) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var result []string

	for _, sr := range subreddits {
		// Trim whitespace.
		sr = strings.TrimSpace(sr)
		if sr == "" {
			continue
		}
		// Normalise optional r/ prefix.
		sr = strings.TrimPrefix(sr, "r/")
		sr = strings.TrimSpace(sr)
		if sr == "" {
			continue
		}
		// Reject separators and path traversal characters.
		if strings.ContainsAny(sr, "/\\.") || strings.Contains(sr, "..") {
			return nil, fmt.Errorf("%w: %q contains invalid characters", ErrInvalidSubreddit, sr)
		}
		// Reject empty after normalisation.
		if sr == "" {
			continue
		}

		if !seen[sr] {
			seen[sr] = true
			result = append(result, sr)
		}
	}
	return result, nil
}

// Collect implements domain.SourceCollector.
//
// The collection pipeline is:
//  1. Build and validate the collection scope from config and request
//  2. Scan each configured subreddit listing, deduplicate post IDs
//  3. Filter posts by since window
//  4. Fetch comment trees through a bounded worker pool (5 concurrent workers)
//  5. Flatten comments and construct domain.RawSignal values
//  6. Sort results by CreatedAt descending
//  7. Apply max-items cap
//  8. Return results with any partial errors joined
//
//nolint:gocognit,cyclop,funlen,gocritic // orchestration function; CollectRequest passed by value per interface contract
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	scope, err := buildScope(&c.config, &req)
	if err != nil {
		return nil, err
	}

	// Record client stats before collection to compute delta.
	beforeStats := c.client.Stats()

	// Dedup set for post IDs across subreddits.
	seen := make(map[string]struct{})
	var candidatePosts []postData
	var listingErrs []error

	// 1. Scan subreddits in order, deduplicate post IDs.
	for _, sr := range scope.subreddits {
		select {
		case <-ctx.Done():
			c.storeStatsDelta(beforeStats)
			return nil, ctx.Err()
		default:
		}

		limit := scope.maxPosts
		if limit > 100 {
			limit = 100
		}
		listing, err := c.client.listing(ctx, sr, scope.sort, scope.timeFilter, limit)
		if err != nil {
			listingErrs = append(listingErrs, fmt.Errorf("subreddit %s: %w", sr, err))
			continue
		}
		if listing == nil {
			continue
		}
		for i := range listing.Data.Children {
			child := &listing.Data.Children[i]
			if child.Kind != "t3" {
				continue
			}
			if _, ok := seen[child.Data.ID]; ok {
				continue
			}
			if !sinceFilter(&child.Data, scope.since) {
				continue
			}
			seen[child.Data.ID] = struct{}{}
			candidatePosts = append(candidatePosts, child.Data)
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

	for i := range candidatePosts {
		post := &candidatePosts[i]
		select {
		case <-ctx.Done():
			wg.Wait()
			c.storeStatsDelta(beforeStats)
			return signals, ctx.Err()
		default:
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(p *postData) {
			defer wg.Done()
			defer func() { <-sem }()

			var comments []domain.Comment
			if scope.maxComments > 0 {
				// Fetch comment tree raw JSON.
				rawJSON, err := c.client.commentsRawJSON(ctx, p.ID)
				if err != nil {
					itemMu.Lock()
					itemErrs = append(itemErrs, fmt.Errorf("comments for post %s: %w", p.ID, err))
					itemMu.Unlock()
					return
				}
				comments, err = FlattenComments(rawJSON, scope.maxComments)
				if err != nil {
					itemMu.Lock()
					itemErrs = append(itemErrs, fmt.Errorf("flatten comments for post %s: %w", p.ID, err))
					itemMu.Unlock()
					return
				}
			}

			signal := parsePost(*p, comments, scope.sort, c.now())

			mu.Lock()
			signals = append(signals, signal)
			mu.Unlock()
		}(post)
	}
	wg.Wait()

	// 3. Sort by CreatedAt descending (newest first).
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].CreatedAt.After(signals[j].CreatedAt)
	})

	// 4. Apply maxItems cap.
	if scope.maxPosts > 0 && len(signals) > scope.maxPosts {
		signals = signals[:scope.maxPosts]
	}

	// 5. Store per-run stats delta.
	c.storeStatsDelta(beforeStats)

	// 6. Return results with partial errors.
	if len(listingErrs) > 0 || len(itemErrs) > 0 {
		allErrs := make([]error, 0, len(listingErrs)+len(itemErrs))
		allErrs = append(allErrs, listingErrs...)
		allErrs = append(allErrs, itemErrs...)
		return signals, errors.Join(allErrs...)
	}

	return signals, nil
}

// sinceFilter returns true if the post's creation time is within or after
// the given since window. A zero-value since means no filtering.
func sinceFilter(p *postData, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	return !unixTimestamp(p.CreatedUTC).Before(since)
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
