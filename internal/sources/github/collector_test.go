package github

import (
	"net/http"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

// TestCollector_New_NotEnabled verifies that New returns ErrNotEnabled when disabled.
func TestCollector_New_NotEnabled(t *testing.T) {
	t.Parallel()
	_, err := New(CollectorConfig{Enabled: false})
	if err != ErrNotEnabled {
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

	c, err := New(cfg)
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
	c, err := New(CollectorConfig{Enabled: true, MaxRequests: 100})
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
	c, err := New(CollectorConfig{Enabled: true, MaxRequests: 100})
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
	c, err := New(CollectorConfig{
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

// TestCollector_Collect_NilContext verifies that nil context returns an error.
func TestCollector_Collect_NilContext(t *testing.T) {
	t.Parallel()
	c, err := New(CollectorConfig{Enabled: true, MaxRequests: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = c.Collect(nil, domain.CollectRequest{})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

// TestDeriveScope_SearchStrategy verifies that empty repositories produce search strategy.
func TestDeriveScope_SearchStrategy(t *testing.T) {
	t.Parallel()
	scope := deriveScope(
		configValues{MaxItemsPerRun: 100, MaxCommentsPerItem: 10},
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
		configValues{},
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
		configValues{},
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
