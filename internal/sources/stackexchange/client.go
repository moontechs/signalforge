package stackexchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type transport interface {
	Do(*http.Request) (*http.Response, error)
}

type client struct {
	transport transport
	baseURL   string
	apiKey    string
	retryMax  int
	backoff   func(int) time.Duration
	mu        sync.Mutex
	deadline  time.Time
	requests  int
}

func newClient(t transport, cfg ConfigValues) *client {
	if t == nil {
		hc := cfg.HTTPClient
		if hc == nil {
			hc = &http.Client{Timeout: 30 * time.Second}
		}
		t = hc
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = APIBaseURL
	}
	return &client{transport: t, baseURL: base, apiKey: cfg.APIKey, retryMax: 3,
		backoff: func(attempt int) time.Duration {
			return time.Duration(1<<uint(attempt-1))*time.Second + time.Duration(rand.Intn(250))*time.Millisecond
		}}
}

func (c *client) waitBackoff(ctx context.Context) error {
	c.mu.Lock()
	until := c.deadline
	c.mu.Unlock()
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

func (c *client) setBackoff(seconds int) {
	if seconds <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until := time.Now().Add(time.Duration(seconds) * time.Second)
	if until.After(c.deadline) {
		c.deadline = until
	}
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
	endpoint := c.baseURL + "/search/advanced?" + v.Encode()
	var last error
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if err := c.waitBackoff(ctx); err != nil {
			return nil, err
		}
		if attempt > 0 {
			timer := time.NewTimer(c.backoff(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "SignalForge/1.0")
		resp, err := c.transport.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := readResponse(resp)
		retryAfter := resp.Header.Get("Retry-After")
		status := resp.StatusCode
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if status < 200 || status >= 300 {
			last = fmt.Errorf("status %d", status)
			if status == 408 || status == 429 || status >= 500 {
				if n, e := strconv.Atoi(retryAfter); e == nil {
					c.setBackoff(n)
				}
				continue
			}
			return nil, last
		}
		var out searchResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		c.mu.Lock()
		c.requests++
		c.mu.Unlock()
		c.setBackoff(out.Backoff)
		if out.ErrorID != nil {
			return &out, &APIError{ID: *out.ErrorID, Name: out.ErrorName, Message: out.ErrorMessage}
		}
		if out.QuotaRemaining == 0 {
			return &out, ErrQuotaExhausted
		}
		return &out, nil
	}
	return nil, fmt.Errorf("%w: %w", ErrRetriesExhausted, last)
}

func readResponse(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		return nil, fmt.Errorf("empty response body")
	}
	return io.ReadAll(resp.Body)
}
