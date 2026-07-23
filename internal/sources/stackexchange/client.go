package stackexchange

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
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

// transport is the pluggable HTTP round-tripper for testability.
type transport interface {
	Do(*http.Request) (*http.Response, error)
}

// httpTransport wraps http.Client to satisfy transport.
type httpTransport struct {
	client *http.Client
}

func (t *httpTransport) Do(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

// client communicates with the Stack Exchange API.
// It handles retries, request caps, response size limits, API backoff
// directives, quota tracking, and optional on-disk caching.
type client struct {
	transport             transport
	baseURL               string
	timeout               time.Duration
	retryMax, maxRequests int
	maxBodySize           int64
	apiKey                string
	retryBackoff          func(attempt int) time.Duration
	backoff               func(attempt int) time.Duration
	mu                    sync.Mutex
	requests, cacheHits   int
	store                 *storage.Storage
	forcedBackoff         time.Time
}

func (c *client) getQuestions(ctx context.Context, site string, fromUnix, toUnix int64, page, pageSize int, filter string) (*searchResponse, error) {
	v := url.Values{}
	v.Set("site", site)
	v.Set("fromdate", strconv.FormatInt(fromUnix, 10))
	v.Set("todate", strconv.FormatInt(toUnix, 10))
	v.Set("page", strconv.Itoa(page))
	v.Set("pagesize", strconv.Itoa(pageSize))
	v.Set("filter", filter)
	v.Set("order", "desc")
	v.Set("sort", "creation")
	if c.apiKey != "" {
		v.Set("key", c.apiKey)
	}
	r, err := c.get(ctx, "/search/advanced?"+v.Encode(), 0)
	if r == nil {
		return nil, err
	}
	out := &searchResponse{HasMore: r.HasMore, QuotaMax: r.QuotaMax, QuotaRemaining: r.QuotaRemaining, Backoff: r.Backoff, ErrorID: r.ErrorID, ErrorName: r.ErrorName, ErrorMessage: r.ErrorMessage}
	if len(r.Items) > 0 && json.Unmarshal(r.Items, &out.Items) != nil {
		return nil, fmt.Errorf("%w: items", ErrMalformedResponse)
	}
	return out, err
}

// newClient creates a Stack Exchange API client with the given transport and config.
func newClient(t transport, cfg ConfigValues) *client {
	defaultBackoff := func(attempt int) time.Duration {
		return time.Duration(math.Pow(2, float64(attempt)))*time.Second +
			time.Duration(rand.Intn(1000))*time.Millisecond
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = APIBaseURL
	}
	return &client{
		transport:    t,
		baseURL:      strings.TrimRight(baseURL, "/"),
		timeout:      30 * time.Second,
		retryMax:     3,
		maxRequests:  cfg.MaxRequests,
		maxBodySize:  10 * 1024 * 1024, // 10 MB default.
		apiKey:       cfg.APIKey,
		retryBackoff: defaultBackoff,
		backoff:      defaultBackoff,
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
	return filepath.Join(base, "cache", "stackexchange", hex.EncodeToString(sum[:])+".json")
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

// get performs an HTTP GET and returns the parsed API response envelope.
// It handles caching, request caps, API backoff directives, retries with
// exponential backoff, quota exhaustion, and API-level errors.
func (c *client) get(ctx context.Context, path string, ttl time.Duration) (*apiResponse, error) {
	// Check cache first.
	if body, ok := c.cached(path, ttl); ok {
		var out apiResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		return &out, nil
	}

	// Check request cap.
	if c.requestCapReached() {
		return nil, ErrRequestCap
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		// Check forced backoff before making the request.
		if err := c.waitForcedBackoff(ctx); err != nil {
			return nil, err
		}

		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
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
			var wrapper apiResponse
			if err := json.Unmarshal(body, &wrapper); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
			}

			// Check API-level error inside the response body.
			if wrapper.ErrorID != nil && *wrapper.ErrorID != 0 {
				return &wrapper, &APIError{
					ID:      *wrapper.ErrorID,
					Name:    wrapper.ErrorName,
					Message: wrapper.ErrorMessage,
				}
			}

			// Apply server-requested backoff.
			c.setForcedBackoff(wrapper.Backoff)

			// Check quota exhaustion.
			if wrapper.QuotaRemaining <= 0 {
				c.incrementRequests()
				c.save(path, body)
				return &wrapper, ErrQuotaExhausted
			}

			c.incrementRequests()
			c.save(path, body)
			return &wrapper, nil
		}

		lastErr = fmt.Errorf("status %d", resp.StatusCode)

		// Retry on 5xx, 429, 408 (request timeout).
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode >= 500 {
			continue
		}
		// Non-retryable client error.
		return nil, lastErr
	}

	return nil, fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)
}

// waitForcedBackoff blocks until the forced backoff period has elapsed,
// respecting context cancellation.
func (c *client) waitForcedBackoff(ctx context.Context) error {
	c.mu.Lock()
	until := c.forcedBackoff
	c.mu.Unlock()
	if until.IsZero() {
		return nil
	}
	if d := time.Until(until); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrBackoffCancelled, ctx.Err())
		case <-t.C:
		}
	}
	return nil
}

// setForcedBackoff sets the forced backoff deadline. If a longer backoff
// is already in effect, it is preserved.
func (c *client) setForcedBackoff(seconds int) {
	if seconds <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until := time.Now().Add(time.Duration(seconds) * time.Second)
	if c.forcedBackoff.IsZero() || until.After(c.forcedBackoff) {
		c.forcedBackoff = until
	}
}

// questions fetches questions from a site created within the [fromdate, todate]
// window (Unix timestamps). Results are sorted by creation date descending.
func (c *client) questions(ctx context.Context, site string, fromUnix, toUnix int64, page, pageSize int) (*questionsResponse, error) {
	v := url.Values{}
	v.Set("site", site)
	v.Set("sort", "creation")
	v.Set("order", "desc")
	v.Set("filter", "withbody")
	v.Set("page", strconv.Itoa(page))
	v.Set("pagesize", strconv.Itoa(pageSize))
	if fromUnix > 0 {
		v.Set("fromdate", strconv.FormatInt(fromUnix, 10))
	}
	if toUnix > 0 {
		v.Set("todate", strconv.FormatInt(toUnix, 10))
	}
	if c.apiKey != "" {
		v.Set("key", c.apiKey)
	}
	path := "/questions?" + v.Encode()

	env, err := c.get(ctx, path, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	out := &questionsResponse{apiResponse: *env}
	if len(env.Items) > 0 {
		if err := json.Unmarshal(env.Items, &out.Questions); err != nil {
			return nil, fmt.Errorf("%w: decode questions: %w", ErrMalformedResponse, err)
		}
	}
	return out, nil
}

// answers fetches answers for a specific question.
func (c *client) answers(ctx context.Context, site string, questionID int, page, pageSize int) (*answersResponse, error) {
	v := url.Values{}
	v.Set("site", site)
	v.Set("filter", "withbody")
	v.Set("page", strconv.Itoa(page))
	v.Set("pagesize", strconv.Itoa(pageSize))
	if c.apiKey != "" {
		v.Set("key", c.apiKey)
	}
	path := fmt.Sprintf("/questions/%d/answers?%s", questionID, v.Encode())

	env, err := c.get(ctx, path, 30*time.Minute)
	if err != nil {
		return nil, err
	}
	out := &answersResponse{apiResponse: *env}
	if len(env.Items) > 0 {
		if err := json.Unmarshal(env.Items, &out.Answers); err != nil {
			return nil, fmt.Errorf("%w: decode answers: %w", ErrMalformedResponse, err)
		}
	}
	return out, nil
}

// comments fetches comments for a specific question.
func (c *client) comments(ctx context.Context, site string, questionID int, page, pageSize int) (*commentsResponse, error) {
	v := url.Values{}
	v.Set("site", site)
	v.Set("filter", "withbody")
	v.Set("page", strconv.Itoa(page))
	v.Set("pagesize", strconv.Itoa(pageSize))
	if c.apiKey != "" {
		v.Set("key", c.apiKey)
	}
	path := fmt.Sprintf("/questions/%d/comments?%s", questionID, v.Encode())

	env, err := c.get(ctx, path, 30*time.Minute)
	if err != nil {
		return nil, err
	}
	out := &commentsResponse{apiResponse: *env}
	if len(env.Items) > 0 {
		if err := json.Unmarshal(env.Items, &out.Comments); err != nil {
			return nil, fmt.Errorf("%w: decode comments: %w", ErrMalformedResponse, err)
		}
	}
	return out, nil
}
