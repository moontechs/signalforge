package hackernews

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

// transport is the pluggable HTTP round-tripper for testability.
type transport interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpTransport wraps http.Client to satisfy transport.
type httpTransport struct {
	client *http.Client
}

func (t *httpTransport) Do(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

// client communicates with the HN Firebase API.
// It handles retries, request caps, response size limits, and optional on-disk caching.
type client struct {
	transport             transport
	baseURL               string
	timeout               time.Duration
	retryMax, maxRequests int
	maxBodySize           int64
	retryBackoff          func(attempt int) time.Duration
	mu                    sync.Mutex
	requests, cacheHits   int
	store                 *storage.Storage
}

// newClient creates a Firebase API client with the given transport and config.
func newClient(t transport, cfg ConfigValues) *client {
	defaultBackoff := func(attempt int) time.Duration {
		return time.Duration(math.Pow(2, float64(attempt)))*time.Second +
			time.Duration(rand.Intn(1000))*time.Millisecond
	}
	baseURL := "https://hacker-news.firebaseio.com/v0"
	return &client{
		transport:    t,
		baseURL:      baseURL,
		timeout:      30 * time.Second,
		retryMax:     3,
		maxRequests:  cfg.MaxRequests,
		maxBodySize:  10 * 1024 * 1024, // 10 MB default.
		retryBackoff: defaultBackoff,
	}
}

// WithCache attaches an on-disk cache.
func (c *client) WithCache(s *storage.Storage) *client {
	c.store = s
	return c
}

// Stats returns the current request and cache-hit counters.
func (c *client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Requests: c.requests, CacheHits: c.cacheHits}
}

// cachePath returns the on-disk cache path for a given cache key.
func (c *client) cachePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	base := ""
	if c.store != nil {
		base = c.store.BaseDir()
	}
	return filepath.Join(base, "cache", "hackernews", hex.EncodeToString(sum[:])+".json")
}

// cached retrieves a cached response. Returns (body, true) on fresh hit.
func (c *client) cached(key string, ttl time.Duration) ([]byte, bool) {
	if c.store == nil {
		return nil, false
	}
	var e cachedResponse
	if c.store.LoadJSON(c.cachePath(key), &e) != nil || time.Since(e.CollectedAt) >= ttl {
		return nil, false
	}
	c.mu.Lock()
	c.cacheHits++
	c.mu.Unlock()
	return e.Body, true
}

// save persists a response body to the on-disk cache. Errors are non-fatal.
func (c *client) save(key string, body []byte) {
	if c.store != nil {
		_ = c.store.SaveJSON(c.cachePath(key), cachedResponse{Body: body, CollectedAt: time.Now()})
	}
}

// requestCapReached returns true if the per-run request cap is exhausted.
func (c *client) requestCapReached() bool {
	if c.maxRequests <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests >= c.maxRequests
}

// incrementRequests increments the request counter.
func (c *client) incrementRequests() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests++
}

// get performs an HTTP GET and unmarshals the JSON response into out.
// It uses the on-disk cache for TTL-based caching.
func (c *client) get(ctx context.Context, path string, ttl time.Duration, out any) error {
	// Check cache first.
	if body, ok := c.cached(path, ttl); ok {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		return nil
	}

	// Check request cap.
	if c.requestCapReached() {
		return ErrRequestCap
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryBackoff(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "SignalForge/1.0")

		resp, err := c.transport.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := c.readBody(resp)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.incrementRequests()
			c.save(path, body)
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
			}
			return nil
		}

		lastErr = fmt.Errorf("status %d", resp.StatusCode)

		// Retry on 5xx, 429, or other server errors.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			continue
		}
		// Non-retryable client error.
		return lastErr
	}

	return fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)
}

// readBody reads the response body, enforcing the max body size limit.
func (c *client) readBody(resp *http.Response) ([]byte, error) {
	limited := io.LimitReader(resp.Body, c.maxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > c.maxBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes", c.maxBodySize)
	}
	return body, nil
}

// feed fetches a feed's item ID list.
func (c *client) feed(ctx context.Context, feed string) (feedResponse, error) {
	var ids feedResponse
	if err := c.get(ctx, "/"+feed+".json", 5*time.Minute, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// item fetches a single item (story or comment) by ID.
func (c *client) item(ctx context.Context, id int) (*itemResponse, error) {
	var item itemResponse
	if err := c.get(ctx, fmt.Sprintf("/item/%d.json", id), 24*time.Hour, &item); err != nil {
		return nil, err
	}
	return &item, nil
}
