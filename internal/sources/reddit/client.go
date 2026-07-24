package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
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

// client communicates with the Reddit OAuth API.
// It handles client-credentials token acquisition, retries, request caps,
// response size limits, safe concurrent token refresh, and optional on-disk
// caching of public API responses. Credentials are provided at construction
// and are never serialized, logged, exposed through stats, or included in
// cache keys.
type client struct {
	transport    transport
	tokenURL     string
	apiBaseURL   string
	timeout      time.Duration
	retryMax     int
	maxRequests  int
	maxBodySize  int64
	retryBackoff func(attempt int) time.Duration

	// OAuth client credentials (never serialized or logged).
	clientID     string
	clientSecret string

	// User-Agent sent on every Reddit API request.
	userAgent string

	mu        sync.Mutex // protects token, requests, cacheHits, cache.
	acquireMu sync.Mutex // serialises OAuth token acquisition.

	token       *oauthTokenResponse
	tokenExpiry time.Time
	requests    int
	cacheHits   int
	cache       *cache.Cache
}

// newClient creates a Reddit OAuth API client with the given transport and
// credentials. maxRequests caps the total number of token and API network
// calls per run; 0 or negative means no cap.
func newClient(t transport, clientID, clientSecret string, maxRequests int) *client {
	defaultBackoff := func(attempt int) time.Duration {
		//nolint:gosec // weak RNG is acceptable for jitter in retry backoff
		return time.Duration(math.Pow(2, float64(attempt)))*time.Second +
			time.Duration(rand.Intn(1000))*time.Millisecond
	}
	return &client{
		transport:    t,
		tokenURL:     "https://www.reddit.com/api/v1/access_token",
		apiBaseURL:   "https://oauth.reddit.com",
		timeout:      30 * time.Second,
		retryMax:     3,
		maxRequests:  maxRequests,
		maxBodySize:  10 * 1024 * 1024, // 10 MB.
		retryBackoff: defaultBackoff,
		clientID:     clientID,
		clientSecret: clientSecret,
		userAgent:    "SignalForge/1.0 (by /u/signalforge_bot)",
	}
}

// WithCache attaches an on-disk response cache.
func (c *client) WithCache(value *cache.Cache) *client {
	c.cache = value
	return c
}

// Stats returns the current request and cache-hit counters.
func (c *client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Requests: c.requests, CacheHits: c.cacheHits}
}

// cached retrieves a cached response. Returns (body, true) on fresh hit.
func (c *client) cached(key string, _ time.Duration) ([]byte, bool) {
	if c.cache == nil {
		return nil, false
	}
	body, ok := c.cache.Get(key)
	if !ok {
		return nil, false
	}
	c.mu.Lock()
	c.cacheHits++
	c.mu.Unlock()
	return body, true
}

// save persists a response body to the on-disk cache. Errors are non-fatal.
func (c *client) save(key string, body []byte, ttl time.Duration) {
	if c.cache != nil {
		_ = c.cache.Set(key, cache.CacheEntry{Body: body, TTL: ttl})
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

// ---------------------------------------------------------------------------
// OAuth token management
// ---------------------------------------------------------------------------

// ensureToken returns a valid access token, acquiring or refreshing one if
// the current token is missing or nearing expiry (60-second buffer).
//
// Token acquisition is serialised via acquireMu so that under concurrent
// access only one goroutine performs the HTTP exchange; others either find
// a valid token on first check or wait for the acquisition to complete.
func (c *client) ensureToken(ctx context.Context) error {
	// Fast path: already have a valid token.
	c.mu.Lock()
	if c.token != nil && time.Now().Add(60*time.Second).Before(c.tokenExpiry) {
		c.mu.Unlock()
		return nil
	}
	needsAcquire := c.clientID != "" && c.clientSecret != ""
	c.mu.Unlock()

	if !needsAcquire {
		return ErrMissingCredentials
	}

	// Serialise acquisition so only one goroutine makes the HTTP call.
	c.acquireMu.Lock()
	defer c.acquireMu.Unlock()

	// Double-check after acquire lock — another goroutine may have
	// already refreshed the token while we were waiting.
	c.mu.Lock()
	if c.token != nil && time.Now().Add(60*time.Second).Before(c.tokenExpiry) {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	return c.acquireTokenLocked(ctx)
}

// acquireTokenLocked performs the OAuth client-credentials token exchange
// and updates client state. acquireMu must be held by the caller.
func (c *client) acquireTokenLocked(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryBackoff(attempt)):
			}
		}

		// Check request cap before making the network call.
		if c.requestCapReached() {
			return ErrRequestCap
		}

		form := url.Values{}
		form.Set("grant_type", "client_credentials")

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			lastErr = fmt.Errorf("create token request: %w", err)
			continue
		}
		req.SetBasicAuth(c.clientID, c.clientSecret)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.transport.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("token transport: %w", err)
			continue
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read token response: %w", readErr)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var tokenResp oauthTokenResponse
			if err := json.Unmarshal(respBody, &tokenResp); err != nil {
				return fmt.Errorf("%w: decode: %w", ErrTokenAuth, err)
			}
			if tokenResp.AccessToken == "" {
				return fmt.Errorf("%w: empty access token", ErrTokenAuth)
			}

			c.mu.Lock()
			c.token = &tokenResp
			c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
			c.requests++
			c.mu.Unlock()

			return nil
		}

		lastErr = fmt.Errorf("%w: status %d: %s", ErrTokenAuth, resp.StatusCode,
			strings.TrimSpace(string(respBody)))

		// Non-retryable: 4xx except 429.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return lastErr
		}
		// 429 and 5xx: retry.
	}

	return fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)
}

// ---------------------------------------------------------------------------
// Authenticated API calls
// ---------------------------------------------------------------------------

// get performs an authenticated GET against the Reddit JSON API, unmarshals
// the JSON response into out, and caches the body on success.
//
// It checks the on-disk cache first (by path+params), then acquires a token
// if needed, then issues the request with retry logic. Transient transport
// errors, 429, 5xx, and 401 (token expired) are retried. Non-retryable 4xx
// and auth failures are returned promptly.
//
//nolint:gocognit,gocyclo,cyclop,funlen // retry loop with cache, auth, and error handling
func (c *client) get(ctx context.Context, path string, params url.Values, ttl time.Duration, out any) error {
	// Build a deterministic cache key from path and non-secret query params.
	cacheKey := path
	if params != nil {
		cacheKey = path + "?" + params.Encode()
	}

	// Check cache first — no auth required for cached responses.
	if body, ok := c.cached(cacheKey, ttl); ok {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		return nil
	}

	// Check request cap before any network call.
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

		// Ensure a valid token before each attempt.
		if err := c.ensureToken(ctx); err != nil {
			return err
		}

		// Build the full authenticated URL.
		fullURL := c.apiBaseURL + path
		if params != nil {
			fullURL += "?" + params.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		// Read token under the lock.
		c.mu.Lock()
		token := ""
		if c.token != nil {
			token = c.token.AccessToken
		}
		c.mu.Unlock()

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.transport.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := c.readBody(resp)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.incrementRequests()
			c.save(cacheKey, body, ttl)
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
			}
			return nil
		}

		lastErr = fmt.Errorf("reddit api: status %d", resp.StatusCode)

		// 401: token expired — invalidate and retry with fresh token.
		if resp.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			c.token = nil
			c.tokenExpiry = time.Time{}
			c.mu.Unlock()
			continue
		}

		// Retry on 5xx and 429 (rate limited).
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			continue
		}

		// Non-retryable client error (4xx except 429).
		return lastErr
	}

	return fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)
}

// readBody reads the response body, enforcing the max body size limit.
func (c *client) readBody(resp *http.Response) ([]byte, error) {
	limited := io.LimitReader(resp.Body, c.maxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > c.maxBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes", c.maxBodySize)
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// Reddit API helpers
// ---------------------------------------------------------------------------

// listing fetches a subreddit listing (posts) using the configured sort and
// optional time filter. limit is the maximum number of posts to return per
// page (Reddit allows 1–100). The t parameter is only valid for sorts that
// support time filtering (top, controversial).
func (c *client) listing(ctx context.Context, subreddit, sort, timeFilter string, limit int) (*listingResponse, error) {
	path := fmt.Sprintf("/r/%s/%s.json", subreddit, sort)
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if timeFilter != "" && sort == "top" {
		params.Set("t", timeFilter)
	}

	var resp listingResponse
	// Subreddit listings change frequently — short TTL.
	if err := c.get(ctx, path, params, 5*time.Minute, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// comments fetches a post's full comment tree. The response is a two-element
// array: index 0 is the post listing, index 1 is the comment listing.
func (c *client) comments(ctx context.Context, postID string) (commentTreeResponse, error) {
	path := fmt.Sprintf("/comments/%s.json", postID)
	var resp commentTreeResponse
	// Comment trees change less frequently — longer TTL.
	if err := c.get(ctx, path, nil, 30*time.Minute, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// commentsRawJSON fetches a post's full comment tree and returns the raw
// JSON bytes. The raw bytes are needed by FlattenComments because the
// standard commentTreeResponse type cannot correctly decode comment-specific
// fields (body, parent_id, replies) due to the use of postData in
// listingChild.
func (c *client) commentsRawJSON(ctx context.Context, postID string) ([]byte, error) {
	path := fmt.Sprintf("/comments/%s.json", postID)
	// Use a wrapper that captures the raw JSON.
	rawHolder := &rawBytesHolder{}
	// Comment trees change less frequently — longer TTL.
	if err := c.get(ctx, path, nil, 30*time.Minute, rawHolder); err != nil {
		return nil, err
	}
	return rawHolder.body, nil
}

// rawBytesHolder is a json.Unmarshaler that captures raw bytes.
type rawBytesHolder struct {
	body []byte
}

// UnmarshalJSON implements json.Unmarshaler.
//
//nolint:unparam // must implement json.Unmarshaler interface
func (r *rawBytesHolder) UnmarshalJSON(b []byte) error {
	r.body = make([]byte, len(b))
	copy(r.body, b)
	return nil
}
