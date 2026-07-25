package reddit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moontechs/signalforge/internal/cache"
)

type transport interface {
	Do(req *http.Request) (*http.Response, error)
}

type client struct {
	transport                          transport
	clientID, clientSecret             string
	apiURL, tokenURL                   string
	maxRequests, runRequests, requests int
	cacheHits                          int
	retryMax                           int
	backoff                            func(int) time.Duration
	cache                              *cache.Cache
	mu, authMu                         sync.Mutex
	token                              string
	tokenExpires                       time.Time
}

func newClient(t transport, cfg *ConfigValues) *client {
	return &client{transport: t, clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, apiURL: "https://oauth.reddit.com", tokenURL: "https://www.reddit.com/api/v1/access_token", maxRequests: cfg.MaxRequests, retryMax: 3, backoff: func(n int) time.Duration { return time.Duration(math.Pow(2, float64(n))) * time.Second }}
}

func (c *client) WithCache(value *cache.Cache) *client { c.cache = value; return c }
func (c *client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Requests: c.requests, CacheHits: c.cacheHits}
}

func (c *client) beginRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runRequests = 0
}

func (c *client) reserveRequest() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxRequests > 0 && c.runRequests >= c.maxRequests {
		return false
	}
	c.runRequests++
	c.requests++
	return true
}

func (c *client) cached(key string) ([]byte, bool) {
	if c.cache == nil {
		return nil, false
	}
	b, ok := c.cache.Get(key)
	if ok {
		c.mu.Lock()
		c.cacheHits++
		c.mu.Unlock()
	}
	return b, ok
}

func (c *client) save(key string, b []byte, ttl time.Duration) {
	if c.cache != nil {
		_ = c.cache.Set(key, cache.Entry{Body: b, TTL: ttl})
	}
}

func (c *client) authenticate(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.mu.Lock()
	if c.token != "" && time.Now().Add(30*time.Second).Before(c.tokenExpires) {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create reddit authentication request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.clientID+":"+c.clientSecret)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "SignalForge/1.0 (research collector)")
	b, err := c.do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthFailed, err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(b, &tr); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
	}
	if tr.AccessToken == "" || tr.ExpiresIn <= 0 {
		return fmt.Errorf("%w: invalid access token response", ErrMalformedResponse)
	}
	c.mu.Lock()
	c.token = tr.AccessToken
	c.tokenExpires = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	c.mu.Unlock()
	return nil
}

func cloneRequest(req *http.Request, attempt int) (*http.Request, error) {
	cloned := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return cloned, nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("restore request body: %w", err)
		}
		cloned.Body = body
		return cloned, nil
	}
	if attempt == 0 {
		cloned.Body = req.Body
		return cloned, nil
	}
	return nil, errors.New("request body cannot be replayed")
}

func (c *client) do(req *http.Request) ([]byte, error) {
	var last error
	lastStatus := 0
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(c.backoff(attempt)):
			}
		}
		attemptReq, err := cloneRequest(req, attempt)
		if err != nil {
			return nil, err
		}
		if !c.reserveRequest() {
			return nil, ErrRequestCap
		}
		resp, err := c.transport.Do(attemptReq)
		if err != nil {
			last = err
			continue
		}
		lastStatus = resp.StatusCode
		b, e := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024+1))
		resp.Body.Close()
		if e != nil {
			last = e
			continue
		}
		if len(b) > 10*1024*1024 {
			return nil, errors.New("response exceeds 10 MiB")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return b, nil
		}
		last = fmt.Errorf("status %d", resp.StatusCode)
		if resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return nil, last
		}
	}
	retryErr := fmt.Errorf("%w: %w", ErrRetriesExhausted, last)
	if lastStatus == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: %w", ErrRateLimited, retryErr)
	}
	return nil, retryErr
}

func (c *client) get(ctx context.Context, path string, ttl time.Duration, out any) error {
	if b, ok := c.cached(path); ok {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		return nil
	}
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("create reddit API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "SignalForge/1.0 (research collector)")
	b, err := c.do(req)
	if err != nil {
		return err
	}
	c.save(path, b, ttl)
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedResponse, err)
	}
	return nil
}

func (c *client) listing(ctx context.Context, subreddit string, scope *collectionScope, after string) (listingResponse, error) {
	const maxListingPageSize = 100
	limit := min(scope.maxPosts, maxListingPageSize)
	q := url.Values{"limit": {strconv.Itoa(limit)}, "t": {scope.timeRange}}
	if after != "" {
		q.Set("after", after)
	}
	path := "/r/" + url.PathEscape(subreddit) + "/" + url.PathEscape(scope.sort) + ".json?" + q.Encode()
	var out listingResponse
	err := c.get(ctx, path, 5*time.Minute, &out)
	return out, err
}

func (c *client) comments(ctx context.Context, subreddit, postID string, limit int) ([]listingResponse, error) {
	path := "/r/" + url.PathEscape(subreddit) + "/comments/" + url.PathEscape(postID) + ".json?limit=" + strconv.Itoa(limit)
	var out []listingResponse
	err := c.get(ctx, path, 24*time.Hour, &out)
	return out, err
}
