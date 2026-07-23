package hackernews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

const (
	defaultBaseURL    = "https://hacker-news.firebaseio.com/v0"
	defaultTimeout    = 30 * time.Second
	defaultMaxItems   = 300
	defaultMaxComments = 30
)

// ErrNotEnabled is returned when the collector is not enabled.
var ErrNotEnabled = errors.New("hackernews collector is not enabled")

// transport is the pluggable HTTP round-tripper for testability.
type transport interface {
	RoundTrip(req *http.Request) (*http.Response, error)
}

// httpTransport wraps http.Client to satisfy the transport interface.
type httpTransport struct {
	client *http.Client
}

func (t *httpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

// Collector collects Hacker News stories and comments as RawSignals.
type Collector struct {
	config    collectorConfig
	transport transport
	client    *http.Client
	store     *storage.Storage
	now       func() time.Time
}

type collectorConfig struct {
	Enabled            bool
	Feeds              []string
	MaxItemsPerRun     int
	MaxCommentsPerItem int
	MinimumScore       int
}

// New creates a new Hacker News Collector.
func New(cfg *HackerNewsConfig) (*Collector, error) {
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}

	transport := &httpTransport{
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	c := &Collector{
		config: collectorConfig{
			Enabled:            cfg.Enabled,
			Feeds:              cfg.Feeds,
			MaxItemsPerRun:     cfg.MaxItemsPerRun,
			MaxCommentsPerItem: cfg.MaxCommentsPerItem,
			MinimumScore:       cfg.MinimumScore,
		},
		transport: transport,
		client:    nil,
		store:     nil,
		now:       time.Now,
	}

	c.client = &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}
	return c, nil
}

// Name returns the collector name.
func (c *Collector) Name() string {
	return "hackernews"
}

// WithTransport replaces the HTTP transport (for testing).
func (c *Collector) WithTransport(t transport) *Collector {
	c.transport = t
	c.client = &http.Client{
		Transport: t,
		Timeout:   defaultTimeout,
	}
	return c
}

// WithNow overrides the time function (for testing).
func (c *Collector) WithNow(now func() time.Time) *Collector {
	c.now = now
	return c
}

// WithCache attaches an on-disk response cache to the collector.
func (c *Collector) WithCache(store *storage.Storage) *Collector {
	c.store = store
	return c
}

// Collect retrieves stories from Hacker News feeds and returns RawSignals.
func (c *Collector) Collect(ctx context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	var signals []domain.RawSignal
	var errs []error
	collectedAt := c.now()

	// Fetch stories from each feed
	for _, feed := range c.config.Feeds {
		storyIDs, err := c.fetchFeedIDs(ctx, feed)
		if err != nil {
			errs = append(errs, fmt.Errorf("feed %s: %w", feed, err))
			continue
		}

		// Limit items per feed based on remaining quota
		maxItems := c.config.MaxItemsPerRun
		if len(storyIDs) > maxItems {
			storyIDs = storyIDs[:maxItems]
		}

		for _, id := range storyIDs {
			story, err := c.fetchItem(ctx, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("item %d: %w", id, err))
				continue
			}

			if story.Type != "story" {
				continue
			}

			if story.Score < c.config.MinimumScore {
				continue
			}

			signal := c.parseStoryToSignal(story, collectedAt)
			signals = append(signals, signal)
		}
	}

	// Dedup within this run
	seen := make(map[string]bool, len(signals))
	deduped := make([]domain.RawSignal, 0, len(signals))
	for i := range signals {
		if !seen[signals[i].ID] {
			seen[signals[i].ID] = true
			deduped = append(deduped, signals[i])
		}
	}
	signals = deduped

	// Enforce max-item limit
	if c.config.MaxItemsPerRun > 0 && len(signals) > c.config.MaxItemsPerRun {
		signals = signals[:c.config.MaxItemsPerRun]
	}

	// Return combined results with partial errors
	if len(errs) > 0 {
		return signals, fmt.Errorf("hackernews collector: %w", errors.Join(errs...))
	}

	return signals, nil
}

// fetchFeedIDs fetches story IDs from a feed endpoint.
func (c *Collector) fetchFeedIDs(ctx context.Context, feed string) ([]int, error) {
	cacheKey := fmt.Sprintf("feed_%s.json", feed)
	
	// Check cache first
	if c.store != nil {
		path := c.store.Path(cacheKey)
		var cachedIDs []int
		if err := c.store.LoadJSON(path, &cachedIDs); err == nil {
			return cachedIDs, nil
		}
	}

	url := fmt.Sprintf("%s/%s.json", defaultBaseURL, feed)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ids []int
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, fmt.Errorf("parse ids: %w", err)
	}

	// Cache the result
	if c.store != nil {
		path := c.store.Path(cacheKey)
		_ = c.store.SaveJSON(path, ids)
	}

	return ids, nil
}

// fetchItem fetches a single item by ID.
func (c *Collector) fetchItem(ctx context.Context, id int) (*hnItem, error) {
	cacheKey := fmt.Sprintf("item_%d.json", id)
	
	// Check cache first
	if c.store != nil {
		path := c.store.Path(cacheKey)
		var item hnItem
		if err := c.store.LoadJSON(path, &item); err == nil {
			return &item, nil
		}
	}

	url := fmt.Sprintf("%s/item/%d.json", defaultBaseURL, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var item hnItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("parse item: %w", err)
	}

	// Cache the result
	if c.store != nil {
		path := c.store.Path(cacheKey)
		_ = c.store.SaveJSON(path, &item)
	}

	return &item, nil
}

// fetchComments fetches comment items.
func (c *Collector) fetchComments(ctx context.Context, ids []int, collectedAt time.Time) []domain.Comment {
	comments := make([]domain.Comment, 0, len(ids))
	
	for _, id := range ids {
		comment, err := c.fetchItem(ctx, id)
		if err != nil || comment == nil || comment.Type != "comment" || comment.Deleted || comment.Dead {
			continue
		}

		comments = append(comments, domain.Comment{
			ID:        fmt.Sprintf("hn_%d", id),
			Body:      comment.Text,
			Score:     0,
			CreatedAt: time.Unix(comment.Time, 0),
		})
	}
	
	return comments
}

// hnItem represents a Hacker News item.
type hnItem struct {
	ID          int      `json:"id"`
	Type        string   `json:"type"`
	By          string   `json:"by"`
	Time        int64    `json:"time"`
	Text        string   `json:"text,omitempty"`
	Title       string   `json:"title,omitempty"`
	URL         string   `json:"url,omitempty"`
	Score       int      `json:"score,omitempty"`
	Descendants int      `json:"descendants,omitempty"`
	Kids        []int    `json:"kids,omitempty"`
	Dead        bool     `json:"dead,omitempty"`
	Deleted     bool     `json:"deleted,omitempty"`
}

// parseStoryToSignal converts an HN story to a RawSignal.
func (c *Collector) parseStoryToSignal(item *hnItem, collectedAt time.Time) domain.RawSignal {
	url := item.URL
	if url == "" {
		url = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
	}

	// Fetch comments if enabled
	var comments []domain.Comment
	if c.config.MaxCommentsPerItem > 0 && len(item.Kids) > 0 {
		comments = c.fetchComments(context.Background(), item.Kids[:min(len(item.Kids), c.config.MaxCommentsPerItem)], collectedAt)
	}

	return domain.RawSignal{
		ID:          fmt.Sprintf("hn_%d", item.ID),
		Source:      "hackernews",
		SourceID:    fmt.Sprintf("%d", item.ID),
		SourceType:  "story",
		URL:         url,
		Title:       item.Title,
		Body:        item.Text,
		Comments:    comments,
		Community:   "hackernews",
		Score:       item.Score,
		CommentCount: item.Descendants,
		CreatedAt:   time.Unix(item.Time, 0),
		CollectedAt: collectedAt,
		ContentHash: storage.ContentHash(fmt.Sprintf("%d", item.ID), item.Title, item.Text),
		Metadata: map[string]string{
			"author": fmt.Sprintf("%d", item.ID),
		},
	}
}

// min returns the smaller of x and y.
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// HackerNewsConfig holds Hacker News-specific configuration.
type HackerNewsConfig struct {
	Enabled            bool
	Feeds              []string
	MaxItemsPerRun     int
	MaxCommentsPerItem int
	MinimumScore       int
}

// ensure interface compliance.
var _ domain.SourceCollector = (*Collector)(nil)