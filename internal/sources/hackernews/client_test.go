package hackernews

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/storage"
)

// ---- Feed / item fetching ----.

func TestClient_feed_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	body := `[40000001, 40000002, 40000003]`
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	ids, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
	if ids[0] != 40000001 || ids[2] != 40000003 {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

func TestClient_item_success(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	item := `{"id":40000001,"type":"story","by":"alice","time":1700000000,"title":"Test","score":100,"descendants":5}`
	fake.addResponse("https://hacker-news.firebaseio.com/v0/item/40000001.json",
		fakeResponse{statusCode: 200, body: item})

	c := testClient(fake)
	got, err := c.item(t.Context(), 40000001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 40000001 || got.Title != "Test" || got.By != "alice" {
		t.Fatalf("unexpected item: %+v", got)
	}
}

// ---- Cache tests ----.

func TestClient_feed_cacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	body := `[1,2,3]`
	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: body})

	c := testClient(fake)
	c.WithCache(store)

	// First call: cache miss, HTTP request.
	ids1, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if fake.callCountFor(url) != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", fake.callCountFor(url))
	}

	// Second call: cache hit, no HTTP request.
	fake.resetCallCount()
	ids2, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if fake.callCountFor(url) != 0 {
		t.Fatalf("expected 0 HTTP calls (cache hit), got %d", fake.callCountFor(url))
	}
	if len(ids1) != len(ids2) || ids1[0] != ids2[0] {
		t.Fatalf("cached result mismatch: %v vs %v", ids1, ids2)
	}
}

func TestClient_item_cacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	item := `{"id":42,"type":"story","by":"bob","time":1700000000,"title":"Cached"}`
	url := "https://hacker-news.firebaseio.com/v0/item/42.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: item})

	c := testClient(fake)
	c.WithCache(store)

	got1, err := c.item(t.Context(), 42)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if got1.Title != "Cached" {
		t.Fatalf("unexpected title: %s", got1.Title)
	}
	if fake.callCountFor(url) != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", fake.callCountFor(url))
	}

	fake.resetCallCount()
	got2, err := c.item(t.Context(), 42)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if fake.callCountFor(url) != 0 {
		t.Fatalf("expected 0 HTTP calls (cache hit), got %d", fake.callCountFor(url))
	}
	if got1.Title != got2.Title {
		t.Fatalf("cached item mismatch")
	}
}

func TestClient_cacheExpiration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	// Compute the cache path for this key.
	key := "/newstories.json"
	sum := sha256.Sum256([]byte(key))
	cacheFile := filepath.Join(dir, "cache/hackernews", hex.EncodeToString(sum[:])+".json")

	// Pre-write an expired cached entry (10 minutes old, TTL is 5 minutes).
	oldResp := cachedResponse{
		Body:        []byte("[99,98,97]"),
		CollectedAt: time.Now().Add(-10 * time.Minute),
	}
	if err := store.SaveJSON(cacheFile, oldResp); err != nil {
		t.Fatalf("pre-write cache: %v", err)
	}

	freshBody := `[1,2,3]`
	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: freshBody})

	c := testClient(fake)
	c.WithCache(store)

	// Cache is expired, should fetch fresh.
	ids, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if fake.callCountFor(url) != 1 {
		t.Fatalf("expected 1 HTTP call (cache expired), got %d", fake.callCountFor(url))
	}
	if len(ids) != 3 || ids[0] != 1 {
		t.Fatalf("expected fresh IDs, got %v", ids)
	}
}

// ---- Retry tests ----.

func TestClient_retryTransientRecovers(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	// First 500, then 200.
	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 500, body: `{"error":"server error"}`},
		fakeResponse{statusCode: 200, body: `[1,2,3]`},
	)

	c := testClient(fake)
	c.retryMax = 2    // 1 initial + 2 retries = 3 total attempts
	c.retryBackoff = func(attempt int) time.Duration {
		return time.Millisecond
	}

	ids, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("expected recovery after retry, got: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
}

func TestClient_retryExhaustion(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 500, body: `{"error":"fail"}`})

	c := testClient(fake)
	c.retryMax = 1    // 1 initial + 1 retry = 2 attempts
	c.retryBackoff = func(attempt int) time.Duration {
		return time.Millisecond
	}

	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatalf("expected ErrRetriesExhausted, got %v", err)
	}
	if fake.callCountFor(url) != 2 {
		t.Fatalf("expected 2 calls (1 initial + 1 retry), got %d", fake.callCountFor(url))
	}
}

// ---- Context cancellation ----.

func TestClient_contextCancellation(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: `[1,2,3]`})

	c := testClient(fake)
	// Use a cancelled context.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.feed(ctx, "newstories")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClient_contextCancellationDuringRetry(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	// Keep returning 500 to trigger retries.
	fake.addResponse(url, fakeResponse{statusCode: 500, body: `{}`})

	c := testClient(fake)
	c.retryMax = 3

	ctx, cancel := context.WithCancel(t.Context())
	// Cancel after a short delay so the first attempt starts.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := c.feed(ctx, "newstories")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context error, got %v", err)
	}
}

// ---- Request cap ----.

func TestClient_requestCapReached(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1,2,3]`})

	c := testClient(fake)
	c.maxRequests = 5
	// Pre-fill to reach cap.
	c.mu.Lock()
	c.requests = 5
	c.mu.Unlock()

	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error for request cap")
	}
	if !errors.Is(err, ErrRequestCap) {
		t.Fatalf("expected ErrRequestCap, got %v", err)
	}
}

func TestClient_requestCapNotReached(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[1,2,3]`})

	c := testClient(fake)
	c.maxRequests = 5
	c.mu.Lock()
	c.requests = 4 // One below cap.
	c.mu.Unlock()

	_, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- Malformed JSON ----.

func TestClient_malformedJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `not json at all`})

	c := testClient(fake)
	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("expected ErrMalformedResponse, got %v", err)
	}
}

// ---- Non-retryable HTTP errors ----.

func TestClient_nonRetryableError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 403, body: `{"error":"forbidden"}`})

	c := testClient(fake)
	c.retryMax = 3

	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	// Should NOT be ErrRetriesExhausted (we don't retry 4xx).
	if errors.Is(err, ErrRetriesExhausted) {
		t.Fatal("4xx should not retry, got ErrRetriesExhausted")
	}
	if fake.callCountFor("https://hacker-news.firebaseio.com/v0/newstories.json") != 1 {
		t.Fatalf("expected only 1 call for 4xx, got %d",
			fake.callCountFor("https://hacker-news.firebaseio.com/v0/newstories.json"))
	}
}

func TestClient_429retryThenRecover(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addSequentialResponses(url,
		fakeResponse{statusCode: 429, body: `{}`},
		fakeResponse{statusCode: 200, body: `[1]`},
	)

	c := testClient(fake)
	c.retryMax = 2
	c.retryBackoff = func(attempt int) time.Duration {
		return time.Millisecond
	}

	ids, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("expected recovery after 429 retry, got: %v", err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

// ---- Request count tracking ----.

func TestClient_requestCountTracking(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: `[1,2]`})

	c := testClient(fake)
	stats := c.Stats()
	if stats.Requests != 0 || stats.CacheHits != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}

	_, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats = c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}
}

func TestClient_requestCountTrackingWithCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: `[1,2]`})

	c := testClient(fake)
	c.WithCache(store)

	// First call: 1 request, 0 cache hits.
	_, _ = c.feed(t.Context(), "newstories")
	stats := c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}

	// Second call: cache hit, no additional request.
	_, _ = c.feed(t.Context(), "newstories")
	stats = c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected still 1 request, got %d", stats.Requests)
	}
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}

// ---- Response size limit ----.

func TestClient_responseSizeLimitExceeded(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	// Generate a body larger than maxBodySize.
	largeBody := make([]byte, 100*1024+1) // 100KB + 1 byte
	for i := range largeBody {
		largeBody[i] = 'x'
	}
	fake.addResponse(url, fakeResponse{statusCode: 200, body: string(largeBody)})

	c := testClient(fake)
	c.maxBodySize = 100 * 1024 // 100KB limit
	c.retryMax = 0              // No retries to speed up test

	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}

func TestClient_defaultBodySizeLimit(t *testing.T) {
	t.Parallel()
	c := testClient(nil)
	if c.maxBodySize != 10*1024*1024 {
		t.Fatalf("expected default 10MB limit, got %d", c.maxBodySize)
	}
}

// ---- Concurrent access safety ----.

func TestClient_concurrentAccess(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	feeds := []string{"newstories", "topstories", "beststories", "askstories", "showstories",
		"newstories", "topstories", "beststories", "askstories", "showstories"}

	for i, feed := range feeds {
		feedURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/%s.json", feed)
		fake.addResponse(feedURL, fakeResponse{statusCode: 200, body: fmt.Sprintf(
			`[%d, %d, %d]`, i+1, i+2, i+3)},
		)
		itemURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", i+1)
		fake.addResponse(itemURL, fakeResponse{statusCode: 200, body: fmt.Sprintf(
			`{"id":%d,"type":"story","by":"user%d","time":1700000000,"title":"Story %d"}`,
			i+1, i+1, i+1,
		)})
	}

	c := testClient(fake)
	c.maxRequests = 100

	var wg sync.WaitGroup
	for i, feed := range feeds {
		wg.Add(1)
		go func(feedName string, itemID int) {
			defer wg.Done()
			_, err := c.feed(t.Context(), feedName)
			if err != nil {
				t.Errorf("feed %s: %v", feedName, err)
			}
			_, err = c.item(t.Context(), itemID)
			if err != nil {
				t.Errorf("item %d: %v", itemID, err)
			}
		}(feed, i+1)
	}
	wg.Wait()

	stats := c.Stats()
	// Each goroutine does 1 feed + 1 item = 2 requests, 10 goroutines = 20.
	if stats.Requests != 20 {
		t.Fatalf("expected 20 requests, got %d", stats.Requests)
	}
}

// ---- No-cache (store = nil) ----.

func TestClient_noStore_neverCaches(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/newstories.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: `[1]`})

	c := testClient(fake)
	// No store attached.

	_, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Without store, second call should also make HTTP request.
	fake.resetCallCount()
	_, err = c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fake.callCountFor(url) != 1 {
		t.Fatalf("expected 1 HTTP call (no cache), got %d", fake.callCountFor(url))
	}
}

// ---- Empty feed ----.

func TestClient_feed_empty(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()
	fake.addResponse("https://hacker-news.firebaseio.com/v0/newstories.json",
		fakeResponse{statusCode: 200, body: `[]`})

	c := testClient(fake)
	ids, err := c.feed(t.Context(), "newstories")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty, got %v", ids)
	}
}

// ---- Transport error ----.

func TestClient_transportError(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Don't register any response → fakeTransport returns 404.
	c := testClient(fake)
	_, err := c.feed(t.Context(), "newstories")
	if err == nil {
		t.Fatal("expected error for unregistered URL")
	}
}

// ---- Stats after cache hit ----.

func TestClient_statsAfterCacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.New(dir)
	fake := newFakeTransport()

	url := "https://hacker-news.firebaseio.com/v0/item/42.json"
	fake.addResponse(url, fakeResponse{statusCode: 200, body: `{"id":42}`})

	c := testClient(fake)
	c.WithCache(store)

	_, _ = c.item(t.Context(), 42)
	_, _ = c.item(t.Context(), 42)

	stats := c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}
