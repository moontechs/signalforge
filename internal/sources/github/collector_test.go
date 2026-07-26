package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// TestCollector_New_NotEnabled verifies that New returns ErrNotEnabled when disabled.
func TestCollector_New_NotEnabled(t *testing.T) {
	t.Parallel()
	_, err := New(&CollectorConfig{Enabled: false})
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("expected ErrNotEnabled, got %v", err)
	}
}

// TestCollector_New_Defaults verifies that New returns a usable collector with defaults.
func TestCollector_New_Defaults(t *testing.T) {
	t.Parallel()
	cfg := CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     500,
		MaxCommentsPerItem: 20,
		MaxRequests:        500,
	}

	c, err := New(&cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.Name() != "github" {
		t.Fatalf("expected name 'github', got %q", c.Name())
	}

	if c.config.MaxItemsPerRun != 500 {
		t.Fatalf("expected MaxItemsPerRun=500, got %d", c.config.MaxItemsPerRun)
	}

	if c.config.MaxCommentsPerItem != 20 {
		t.Fatalf("expected MaxCommentsPerItem=20, got %d", c.config.MaxCommentsPerItem)
	}

	if !c.config.SearchIssues {
		t.Fatal("expected SearchIssues=true")
	}

	if !c.config.SearchDiscussions {
		t.Fatal("expected SearchDiscussions=true")
	}

	if c.limits.maxRequests != 500 {
		t.Fatalf("expected maxRequests=500, got %d", c.limits.maxRequests)
	}
}

// TestCollector_WithTransport verifies that WithTransport replaces the transport.
func TestCollector_WithTransport(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{Enabled: true, MaxRequests: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ft := newFakeTransport()
	c.WithTransport(ft)

	if c.transport != ft {
		t.Fatal("WithTransport did not replace transport")
	}
}

// TestCollector_WithNow verifies that WithNow overrides the time function.
func TestCollector_WithNow(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{Enabled: true, MaxRequests: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fixed := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	c.WithNow(func() time.Time { return fixed })

	if got := c.now(); !got.Equal(fixed) {
		t.Fatalf("expected %v, got %v", fixed, got)
	}
}

// TestCollector_Collect_Empty verifies that Collect returns empty results.
// when both sources are disabled.
func TestCollector_Collect_Empty(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{
		Enabled:           true,
		SearchIssues:      false,
		SearchDiscussions: false,
		MaxItemsPerRun:    500,
		MaxRequests:       500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	signals, err := c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(signals) != 0 {
		t.Fatalf("expected empty results, got %d signals", len(signals))
	}
}

// TestCollector_Collect_ValidContext verifies that a valid context works.
func TestCollector_Collect_ValidContext(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{
		Enabled:           true,
		SearchIssues:      true,
		SearchDiscussions: false,
		MaxItemsPerRun:    5,
		MaxRequests:       100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no mocked transport, an HTTP call will fail, but the context
	// itself should not cause a nil pointer panic.
	_, err = c.Collect(context.Background(), domain.CollectRequest{})
	if err == nil {
		t.Log("Collect succeeded without mocked transport (expected with real API)")
	}
}

// TestDeriveScope_SearchStrategy verifies that empty repositories produce search strategy.
func TestDeriveScope_SearchStrategy(t *testing.T) {
	t.Parallel()
	scope := deriveScope(
		&configValues{MaxItemsPerRun: 100, MaxCommentsPerItem: 10},
		nil, // empty repos.
		[]string{"bug"},
		[]string{"go"},
		100,
		10,
		"2025-01-01T00:00:00Z",
	)

	if scope.strategy != strategySearch {
		t.Fatalf("expected strategySearch, got %d", scope.strategy)
	}

	if len(scope.repos) != 0 {
		t.Fatalf("expected empty repos, got %v", scope.repos)
	}

	if scope.maxItems != 100 {
		t.Fatalf("expected maxItems=100, got %d", scope.maxItems)
	}

	if scope.maxComments != 10 {
		t.Fatalf("expected maxComments=10, got %d", scope.maxComments)
	}

	if len(scope.labels) != 1 || scope.labels[0] != "bug" {
		t.Fatalf("expected labels=[bug], got %v", scope.labels)
	}

	if scope.since != "2025-01-01T00:00:00Z" {
		t.Fatalf("expected since=2025-01-01T00:00:00Z, got %q", scope.since)
	}
}

// TestDeriveScope_PerRepoStrategy verifies that populated repos produce per-repo strategy.
func TestDeriveScope_PerRepoStrategy(t *testing.T) {
	t.Parallel()
	repos := []string{"owner/repo1", "owner/repo2"}
	scope := deriveScope(
		&configValues{},
		repos,
		nil,
		nil,
		50,
		5,
		"",
	)

	if scope.strategy != strategyPerRepo {
		t.Fatalf("expected strategyPerRepo, got %d", scope.strategy)
	}

	if len(scope.repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(scope.repos))
	}

	if scope.repos[0] != "owner/repo1" || scope.repos[1] != "owner/repo2" {
		t.Fatalf("unexpected repos: %v", scope.repos)
	}

	if scope.maxItems != 50 {
		t.Fatalf("expected maxItems=50, got %d", scope.maxItems)
	}

	if scope.maxComments != 5 {
		t.Fatalf("expected maxComments=5, got %d", scope.maxComments)
	}

	if scope.since != "" {
		t.Fatalf("expected empty since, got %q", scope.since)
	}
}

// TestDeriveScope_EmptyValues verifies scope derivation with minimal inputs.
func TestDeriveScope_EmptyValues(t *testing.T) {
	t.Parallel()
	scope := deriveScope(
		&configValues{},
		nil,
		nil,
		nil,
		0,
		0,
		"",
	)

	if scope.strategy != strategySearch {
		t.Fatalf("expected strategySearch, got %d", scope.strategy)
	}

	if scope.maxItems != 0 {
		t.Fatalf("expected maxItems=0, got %d", scope.maxItems)
	}
}

func TestDeriveScope_RequestMaxItemsOverridesConfig(t *testing.T) {
	scope := deriveScope(&configValues{MaxItemsPerRun: 100}, nil, nil, nil, 3, 0, "")
	if scope.maxItems != 3 {
		t.Fatalf("maxItems = %d, want 3", scope.maxItems)
	}
}

func TestDeriveScope_ZeroRequestMaxItemsUsesConfig(t *testing.T) {
	scope := deriveScope(&configValues{MaxItemsPerRun: 100}, nil, nil, nil, 0, 0, "")
	if scope.maxItems != 100 {
		t.Fatalf("maxItems = %d, want 100", scope.maxItems)
	}
}

// TestErrorTypes verifies the custom error types work as expected.
func TestErrorTypes(t *testing.T) {
	t.Parallel()
	// RateLimitError.
	rle := &RateLimitError{
		IsPrimary: true,
		Remaining: 0,
		Limit:     5000,
		Reset:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if !IsRateLimit(rle) {
		t.Fatal("expected IsRateLimit to return true")
	}
	if !IsPrimaryRateLimit(rle) {
		t.Fatal("expected IsPrimaryRateLimit to return true")
	}
	if IsSecondaryRateLimit(rle) {
		t.Fatal("expected IsSecondaryRateLimit to return false")
	}

	// Secondary rate limit.
	sl := &RateLimitError{IsSecondary: true, RetryAfter: 10 * time.Second}
	if !IsRateLimit(sl) {
		t.Fatal("expected IsRateLimit to return true")
	}
	if !IsSecondaryRateLimit(sl) {
		t.Fatal("expected IsSecondaryRateLimit to return true")
	}
	if IsPrimaryRateLimit(sl) {
		t.Fatal("expected IsPrimaryRateLimit to return false")
	}

	// RetryExhaustionError.
	re := &RetryExhaustionError{Wrapped: http.ErrAbortHandler, Attempts: 3}
	if re.Error() == "" {
		t.Fatal("expected non-empty error string")
	}

	// MalformedResponseError.
	mr := &MalformedResponseError{Wrapped: http.ErrBodyNotAllowed, Body: "<bad>"}
	if mr.Error() == "" {
		t.Fatal("expected non-empty error string")
	}

	// RequestLimitError.
	rl := &RequestLimitError{Limit: 100}
	if rl.Error() == "" {
		t.Fatal("expected non-empty error string")
	}
}

// TestInterfaceCompliance verifies Collector implements domain.SourceCollector.
func TestInterfaceCompliance(t *testing.T) {
	t.Parallel()
	var _ domain.SourceCollector = (*Collector)(nil)
}

// TestCollector_Stats_Empty verifies Stats returns zeros before any collection.
func TestCollector_Stats_Empty(t *testing.T) {
	t.Parallel()
	c, err := New(&CollectorConfig{
		Enabled:           true,
		SearchIssues:      false,
		SearchDiscussions: false,
		MaxRequests:       500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stats := c.Stats()
	if stats.Requests != 0 {
		t.Fatalf("expected 0 requests, got %d", stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}
}

// TestCollector_Stats_AfterCollect verifies Stats returns per-run deltas after Collect.
func TestCollector_Stats_AfterCollect(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	// Register a single search response.
	searchResp := ghSearchResponse{
		TotalCount: 2,
		Items: []ghIssue{
			{ID: 1, Number: 1, Title: "Issue 1", Body: "Body 1",
				HTMLURL: "https://github.com/o/r/issues/1", State: "open",
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
				User:      ghUser{Login: "u1"}, Comments: 0,
				RepoURL: "https://api.github.com/repos/o/r",
			},
			{ID: 2, Number: 2, Title: "Issue 2", Body: "Body 2",
				HTMLURL: "https://github.com/o/r/issues/2", State: "open",
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
				User:      ghUser{Login: "u2"}, Comments: 0,
				RepoURL: "https://api.github.com/repos/o/r",
			},
		},
	}
	searchBody, _ := json.Marshal(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"X-RateLimit-Remaining": "4999"},
		body:       string(searchBody),
	})

	c, err := New(&CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.WithTransport(fake)
	c.WithNow(func() time.Time { return time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC) })

	_, err = c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Stats should reflect the delta (1 search request made).
	stats := c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}
}

// TestCollector_Stats_WithCacheHit verifies Stats tracks cache hits correctly.
func TestCollector_Stats_WithCacheHit(t *testing.T) {
	t.Parallel()
	store := storage.New(t.TempDir())
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{ID: 10, Number: 1, Title: "Cached issue", Body: "Body",
				HTMLURL: "https://github.com/o/r/issues/1", State: "open",
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
				User:      ghUser{Login: "u1"}, Comments: 0,
				RepoURL: "https://api.github.com/repos/o/r",
			},
		},
	}
	searchBody, _ := json.Marshal(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"ETag": `W/"cachetag"`, "X-RateLimit-Remaining": "4999"},
		body:       string(searchBody),
	})

	c, err := New(&CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.WithTransport(fake)
	c.WithCache(store)
	c.WithNow(func() time.Time { return time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC) })

	// First call: 1 outbound request, 0 cache hits.
	_, err = c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	stats := c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.Requests)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", stats.CacheHits)
	}

	// Second call: request is served from disk cache → 0 requests, 1 cache hit.
	fake.resetCallCount()
	// Register 304 for the second call (ETag conditional, will return 304).
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 304,
		headers:    map[string]string{"ETag": `W/"cachetag"`, "X-RateLimit-Remaining": "4998"},
		body:       "",
	})

	_, err = c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	stats = c.Stats()
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}

// TestCollector_Stats_ReuseReset verifies that Stats returns per-run deltas,
// not cumulative values, when a collector instance is reused.
func TestCollector_Stats_ReuseReset(t *testing.T) {
	t.Parallel()
	fake := newFakeTransport()

	searchResp := ghSearchResponse{
		TotalCount: 1,
		Items: []ghIssue{
			{ID: 20, Number: 1, Title: "Issue A", Body: "Body",
				HTMLURL: "https://github.com/o/r/issues/1", State: "open",
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
				User:      ghUser{Login: "u1"}, Comments: 0,
				RepoURL: "https://api.github.com/repos/o/r",
			},
		},
	}
	searchBody, _ := json.Marshal(searchResp)
	searchURL := "https://api.github.com/search/issues?q=is%3Aissue+is%3Aopen&sort=updated&direction=asc&per_page=100&page=1"
	fake.addResponse(searchURL, fakeResponse{
		statusCode: 200,
		headers:    map[string]string{"X-RateLimit-Remaining": "4999"},
		body:       string(searchBody),
	})

	c, err := New(&CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.WithTransport(fake)
	c.WithNow(func() time.Time { return time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC) })

	// Run 1: 1 request.
	_, err = c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("collect 1 failed: %v", err)
	}
	stats := c.Stats()
	if stats.Requests != 1 {
		t.Fatalf("run 1: expected 1 request, got %d", stats.Requests)
	}

	// Run 2: use same collector, should report only run 2's requests.
	_, err = c.Collect(t.Context(), domain.CollectRequest{})
	if err != nil {
		t.Fatalf("collect 2 failed: %v", err)
	}
	stats = c.Stats()
	// The second call should also make 1 request (no cache, HTTP conditional 304).
	if stats.Requests != 1 {
		t.Fatalf("run 2: expected 1 request (per-run delta), got %d", stats.Requests)
	}
}
