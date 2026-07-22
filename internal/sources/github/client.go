package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRetryMax  = 3
	defaultUserAgent = "SignalForge/1.0"
	defaultTimeout   = 30 * time.Second
	restRateLimitMax = 5000
	gqlRateLimitMax  = 5000
	githubAPIBase    = "https://api.github.com"
	githubAPIVersion = "2022-11-28"
)

// etagEntry holds cached response data for conditional requests.
type etagEntry struct {
	Etag         string
	LastModified string
	Body         []byte
}

// githubClient handles HTTP communication with the GitHub API.
// It provides retry, rate-limit tracking, and conditional request support.
type githubClient struct {
	transport    transport
	token        string
	userAgent    string
	retryMax     int
	requestCount int
	requestLimit int

	restRemaining  int
	restReset      time.Time
	gqlRemaining   int
	gqlReset       time.Time

	etags map[string]etagEntry
	etagMutex sync.RWMutex
	statsMutex sync.Mutex
}

// requestOptions describes an HTTP request to the GitHub API.
type requestOptions struct {
	Method    string
	Path      string // path relative to api.github.com, e.g. /search/issues
	Body      []byte
	IsGraphQL bool // selects which rate-limit counter to use
	CacheKey  string // key for ETag cache; empty to skip caching
}

// graphQLRequest is the standard GraphQL request envelope.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphQLResponse is the standard GraphQL response envelope.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// newClient creates a new githubClient using the given transport.
func newClient(transport transport, maxRequests int) *githubClient {
	token := os.Getenv("GITHUB_TOKEN")
	return &githubClient{
		transport:     transport,
		token:         token,
		userAgent:     defaultUserAgent,
		retryMax:      defaultRetryMax,
		requestLimit:  maxRequests,
		restRemaining: restRateLimitMax,
		gqlRemaining:  gqlRateLimitMax,
		etags:         make(map[string]etagEntry),
	}
}

// checkRateLimit returns an error if the applicable rate limit is exhausted.
func (c *githubClient) checkRateLimit(isGraphQL bool) error {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	if isGraphQL {
		if c.gqlRemaining <= 0 && time.Now().Before(c.gqlReset) {
			return &RateLimitError{
				IsPrimary: true,
				Remaining: c.gqlRemaining,
				Limit:     gqlRateLimitMax,
				Reset:     c.gqlReset,
			}
		}
	} else {
		if c.restRemaining <= 0 && time.Now().Before(c.restReset) {
			return &RateLimitError{
				IsPrimary: true,
				Remaining: c.restRemaining,
				Limit:     restRateLimitMax,
				Reset:     c.restReset,
			}
		}
	}
	return nil
}

// updateRateLimits parses rate-limit headers from a response and updates counters.
func (c *githubClient) updateRateLimits(resp *http.Response, isGraphQL bool) {
	remainingStr := resp.Header.Get("X-RateLimit-Remaining")
	resetStr := resp.Header.Get("X-RateLimit-Reset")

	if remainingStr == "" || resetStr == "" {
		return
	}

	remaining, err := strconv.Atoi(remainingStr)
	if err != nil {
		return
	}

	resetUnix, err := strconv.ParseInt(resetStr, 10, 64)
	if err != nil {
		return
	}
	resetTime := time.Unix(resetUnix, 0)

	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	if isGraphQL {
		c.gqlRemaining = remaining
		c.gqlReset = resetTime
	} else {
		c.restRemaining = remaining
		c.restReset = resetTime
	}
}

// incrementRequestCount increases the request counter and returns the new count.
func (c *githubClient) incrementRequestCount() int {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()
	c.requestCount++
	return c.requestCount
}

// requestCountValue returns the current request count (thread-safe).
func (c *githubClient) requestCountValue() int {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()
	return c.requestCount
}

// parseRetryAfter parses the Retry-After header value (seconds or HTTP date).
func parseRetryAfter(val string, now time.Time) time.Duration {
	seconds, err := strconv.Atoi(val)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}

	retryTime, err := time.Parse(http.TimeFormat, val)
	if err == nil {
		return retryTime.Sub(now)
	}

	return 60 * time.Second
}

// doRequest performs an HTTP request with retries, rate-limit checking,
// and conditional request support.
func (c *githubClient) doRequest(ctx context.Context, opts requestOptions) (*http.Response, error) {
	// 1. Check request cap
	c.statsMutex.Lock()
	if c.requestLimit > 0 && c.requestCount >= c.requestLimit {
		c.statsMutex.Unlock()
		return nil, &RequestLimitError{Limit: c.requestLimit}
	}
	c.statsMutex.Unlock()

	// 2. Check rate limit
	if err := c.checkRateLimit(opts.IsGraphQL); err != nil {
		return nil, err
	}

	// 3. Build URL
	reqURL := opts.Path
	if !strings.HasPrefix(reqURL, "http") && !strings.HasPrefix(reqURL, "https://") {
		reqURL = githubAPIBase + opts.Path
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			backoff += time.Duration(rand.Intn(1000)) * time.Millisecond

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if opts.Body != nil {
			bodyReader = bytes.NewReader(opts.Body)
		}

		req, err := http.NewRequestWithContext(ctx, opts.Method, reqURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		// Set standard headers
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		// If GraphQL POST, set content type
		if opts.IsGraphQL {
			req.Header.Set("Content-Type", "application/json")
		}

		// Add conditional request headers
		if opts.CacheKey != "" {
			c.etagMutex.RLock()
			entry, ok := c.etags[opts.CacheKey]
			c.etagMutex.RUnlock()
			if ok {
				if entry.Etag != "" {
					req.Header.Set("If-None-Match", entry.Etag)
				}
				if entry.LastModified != "" {
					req.Header.Set("If-Modified-Since", entry.LastModified)
				}
			}
		}

		resp, err := c.transport.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Handle secondary rate limit (403 + Retry-After)
		if resp.StatusCode == http.StatusForbidden {
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				delay := parseRetryAfter(retryAfter, time.Now())
				resp.Body.Close()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
		}

		// Handle primary rate limit (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			delay := parseRetryAfter(retryAfter, time.Now())
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		// Update rate-limit counters if headers present
		c.updateRateLimits(resp, opts.IsGraphQL)

		// Increment request count for non-304 responses
		if resp.StatusCode != http.StatusNotModified {
			c.incrementRequestCount()
		}

		// Handle 304 Not Modified — return cached body
		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()

			c.etagMutex.RLock()
			entry, ok := c.etags[opts.CacheKey]
			c.etagMutex.RUnlock()

			if ok {
				return &http.Response{
					StatusCode: http.StatusNotModified,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(entry.Body)),
				}, nil
			}

			// No cached body, treat as empty
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		}

		// For 2xx or non-retryable status, read body and cache headers
		if resp.StatusCode < 500 || resp.StatusCode == http.StatusNotModified {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("read response body: %w", err)
			}

			// Store ETag/Last-Modified for conditional requests
			etag := resp.Header.Get("ETag")
			lm := resp.Header.Get("Last-Modified")
			if (etag != "" || lm != "") && opts.CacheKey != "" {
				c.etagMutex.Lock()
				c.etags[opts.CacheKey] = etagEntry{
					Etag:         etag,
					LastModified: lm,
					Body:         body,
				}
				c.etagMutex.Unlock()
			}

			return &http.Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}

		// 5xx server error — retry
		resp.Body.Close()
		lastErr = fmt.Errorf("server error: status %d", resp.StatusCode)
	}

	// Exhausted retries
	if lastErr != nil {
		return nil, &RetryExhaustionError{Wrapped: lastErr, Attempts: c.retryMax + 1}
	}

	return nil, &RetryExhaustionError{
		Wrapped:  fmt.Errorf("request failed after %d attempts", c.retryMax+1),
		Attempts: c.retryMax + 1,
	}
}

// doJSONRequest performs an HTTP request and unmarshals the JSON response.
func (c *githubClient) doJSONRequest(ctx context.Context, opts requestOptions, target any) (*http.Response, error) {
	resp, err := c.doRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotModified {
		// 304 means no change; unmarshal from cached body already in resp.Body
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read 304 body: %w", err)
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, target); err != nil {
				return nil, &MalformedResponseError{
					Wrapped: fmt.Errorf("unmarshal 304 body: %w", err),
					Body:    truncateBody(string(body)),
				}
			}
		}
		return resp, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncateBody(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(body, target); err != nil {
		return nil, &MalformedResponseError{
			Wrapped: fmt.Errorf("unmarshal response: %w", err),
			Body:    truncateBody(string(body)),
		}
	}

	return resp, nil
}

// doGraphQL executes a GraphQL query and returns the full graphQLResponse.
func (c *githubClient) doGraphQL(ctx context.Context, query string, variables map[string]any) (*graphQLResponse, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}

	// Build cache key from query + variables (deterministic)
	cacheKey := cacheKeyForGraphQL(query, variables)

	opts := requestOptions{
		Method:    http.MethodPost,
		Path:      "/graphql",
		Body:      bodyBytes,
		IsGraphQL: true,
		CacheKey:  cacheKey,
	}

	resp, err := c.doRequest(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read graphql response: %w", err)
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, &MalformedResponseError{
			Wrapped: fmt.Errorf("unmarshal graphql response: %w", err),
			Body:    truncateBody(string(body)),
		}
	}

	if len(gqlResp.Errors) > 0 {
		// Return partial data + errors
		return &gqlResp, fmt.Errorf("graphql errors: %v", gqlResp.Errors)
	}

	return &gqlResp, nil
}

// cacheKeyForGraphQL generates a deterministic cache key from a GraphQL query and variables.
func cacheKeyForGraphQL(query string, variables map[string]any) string {
	// Use query + sorted variable values as cache key
	varsStr := ""
	if variables != nil {
		vb, _ := json.Marshal(variables)
		varsStr = string(vb)
	}
	return fmt.Sprintf("gql:%s:%s", query, varsStr)
}

// parseRepo splits "owner/repo" into (owner, repo).
func parseRepo(full string) (string, string, error) {
	parts := strings.SplitN(strings.TrimPrefix(full, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q, expected owner/repo", full)
	}
	return parts[0], parts[1], nil
}

// parseLinkHeader parses a Link header and returns a map of rel -> URL.
func parseLinkHeader(header string) map[string]string {
	result := make(map[string]string)
	if header == "" {
		return result
	}

	re := regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)
	matches := re.FindAllStringSubmatch(header, -1)
	for _, m := range matches {
		if len(m) == 3 {
			result[m[2]] = m[1]
		}
	}
	return result
}

// truncateBody returns up to the first 500 characters of a string.
func truncateBody(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
