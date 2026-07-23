package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/sources/hackernews"
	"github.com/moontechs/signalforge/internal/sources/stackexchange"
	"github.com/moontechs/signalforge/internal/storage"
)

func newTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Sources.GitHub.Enabled = false
	return cfg
}

func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	return storage.New(t.TempDir())
}

func TestBuildCollector_HN(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig()
	store := newTestStorage(t)

	collector, err := buildCollector("hackernews", cfg, store)
	if err != nil {
		t.Fatalf("buildCollector(hackernews) failed: %v", err)
	}
	if collector == nil {
		t.Fatal("collector is nil")
	}
	if collector.Name() != "hackernews" {
		t.Errorf("expected name 'hackernews', got %q", collector.Name())
	}

	_, ok := collector.(*hackernews.Collector)
	if !ok {
		t.Errorf("expected *hackernews.Collector, got %T", collector)
	}
}

func TestBuildCollector_StackExchange(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig()
	store := newTestStorage(t)

	collector, err := buildCollector("stackexchange", cfg, store)
	if err != nil {
		t.Fatalf("buildCollector(stackexchange) failed: %v", err)
	}
	if collector == nil || collector.Name() != "stackexchange" {
		t.Fatalf("expected stackexchange collector, got %v", collector)
	}
	if _, ok := collector.(*stackexchange.Collector); !ok {
		t.Fatalf("expected *stackexchange.Collector, got %T", collector)
	}
}

func TestBuildCollector_HN_Disabled(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig()
	cfg.Sources.HackerNews.Enabled = false
	store := newTestStorage(t)

	_, err := buildCollector("hackernews", cfg, store)
	if err == nil {
		t.Fatal("expected error for disabled HN, got nil")
	}
}

func TestBuildCollector_HN_InvalidFeed(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig()
	cfg.Sources.HackerNews.Feeds = []string{"invalidfeed"}
	store := newTestStorage(t)

	_, err := buildCollector("hackernews", cfg, store)
	if err == nil {
		t.Fatal("expected error for invalid feed, got nil")
	}
}

func TestResolveCollectSources_HN(t *testing.T) {
	t.Parallel()

	sources, err := resolveCollectSources("hn")
	if err != nil {
		t.Fatalf("resolveCollectSources(hn) failed: %v", err)
	}
	if len(sources) != 1 || sources[0] != "hackernews" {
		t.Errorf("expected [hackernews], got %v", sources)
	}
}

func TestResolveCollectSources_HNWithGitHub(t *testing.T) {
	t.Parallel()

	sources, err := resolveCollectSources("github,hn")
	if err != nil {
		t.Fatalf("resolveCollectSources(github,hn) failed: %v", err)
	}
	if len(sources) != 2 {
		t.Errorf("expected 2 sources, got %d: %v", len(sources), sources)
	}
}

func TestStatsDelta_HN(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{
		HackerNewsRequests:  10,
		HackerNewsCacheHits: 5,
	}
	after := &domain.ResearchStats{
		RawSignalsCollected: 10,
		RawSignalsSkipped:   2,
		GitHubRequests:      50,
		HackerNewsRequests:  25,
		HackerNewsCacheHits: 12,
	}

	delta := statsDelta(before, after)
	if delta.collected != 10 {
		t.Errorf("expected collected=10, got %d", delta.collected)
	}
	if delta.skipped != 2 {
		t.Errorf("expected skipped=2, got %d", delta.skipped)
	}
	if delta.requests != 50 {
		t.Errorf("expected requests=50, got %d", delta.requests)
	}
	if delta.hnRequests != 15 {
		t.Errorf("expected hnRequests=15, got %d", delta.hnRequests)
	}
	if delta.hnCacheHits != 7 {
		t.Errorf("expected hnCacheHits=7, got %d", delta.hnCacheHits)
	}
}

func TestStatsDelta_StackExchange(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{StackExchangeRequests: 4, StackExchangeCacheHits: 1}
	after := &domain.ResearchStats{StackExchangeRequests: 11, StackExchangeCacheHits: 6}
	delta := statsDelta(before, after)
	if delta.seRequests != 7 || delta.seCacheHits != 5 {
		t.Fatalf("expected Stack Exchange delta 7/5, got %d/%d", delta.seRequests, delta.seCacheHits)
	}
}

func TestReportCollectSummary_StackExchange(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	if err := reportCollectSummary(cmd, "stackexchange", 3, collectStatsDelta{seRequests: 7, seCacheHits: 2}); err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}
	if output := buf.String(); !strings.Contains(output, "Stack Exchange requests: 7 (cache hits: 2)") {
		t.Fatalf("expected Stack Exchange stats in output, got %q", output)
	}
}

func TestStatsDelta_NoHN(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{}
	after := &domain.ResearchStats{
		RawSignalsCollected: 5,
		HackerNewsRequests:  0,
		HackerNewsCacheHits: 0,
	}

	delta := statsDelta(before, after)
	if delta.hnRequests != 0 {
		t.Errorf("expected hnRequests=0, got %d", delta.hnRequests)
	}
	if delta.hnCacheHits != 0 {
		t.Errorf("expected hnCacheHits=0, got %d", delta.hnCacheHits)
	}
}

func TestReportCollectSummary_HN(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected:   10,
		skipped:     2,
		requests:    50,
		hnRequests:  15,
		hnCacheHits: 7,
	}

	err := reportCollectSummary(cmd, "hackernews", 12, delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "HN requests: 15") {
		t.Errorf("expected HN requests: 15 in output, got: %s", output)
	}
	if !strings.Contains(output, "cache hits: 7") {
		t.Errorf("expected cache hits: 7 in output, got: %s", output)
	}
}

func TestReportCollectSummary_NoHN(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 5,
		skipped:   1,
		requests:  20,
	}

	err := reportCollectSummary(cmd, "github", 5, delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "HN requests") {
		t.Errorf("unexpected HN requests in output: %s", output)
	}
}

func TestReportCollectSummary_OnlyHN(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected:   8,
		skipped:     1,
		hnRequests:  12,
		hnCacheHits: 4,
	}

	err := reportCollectSummary(cmd, "hackernews", 8, delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "HN requests: 12") {
		t.Errorf("expected HN requests: 12 in output, got: %s", output)
	}
	if !strings.Contains(output, "cache hits: 4") {
		t.Errorf("expected cache hits: 4 in output, got: %s", output)
	}
	if strings.Contains(output, "GitHub requests") {
		t.Errorf("unexpected GitHub requests in output when delta.requests=0: %s", output)
	}
}

func TestReportCollectSummary_NoRequests(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 3,
		skipped:   0,
	}

	err := reportCollectSummary(cmd, "stackexchange", 3, delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "GitHub requests") {
		t.Errorf("unexpected GitHub requests when delta.requests=0: %s", output)
	}
	if strings.Contains(output, "HN requests") {
		t.Errorf("unexpected HN requests when delta.hnRequests=0: %s", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "New: 3, skipped: 0") {
		t.Errorf("unexpected output format: %s", output)
	}
}

func TestBuildCollector_HN_RequiresNoToken(t *testing.T) {
	t.Parallel()
	// Unlike GitHub, HN collector does not require any environment token.
	// This test verifies that building an HN collector succeeds even when
	// GITHUB_TOKEN is unset.
	cfg := newTestConfig()
	store := newTestStorage(t)

	collector, err := buildCollector("hackernews", cfg, store)
	if err != nil {
		t.Fatalf("buildCollector(hackernews) should not require a token: %v", err)
	}
	if collector == nil {
		t.Fatal("collector is nil")
	}
}

// mockCollector implements domain.SourceCollector for testing resume cursor flow.
type mockCollector struct {
	name      string
	collectFn func(domain.CollectRequest) ([]domain.RawSignal, error)
}

func (m *mockCollector) Name() string { return m.name }

func (m *mockCollector) Collect(_ context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) { //nolint:gocritic // must match SourceCollector interface signature
	if m.collectFn != nil {
		return m.collectFn(req)
	}
	return nil, nil
}

func TestExecuteCollect_ResumeLoadsCursorForMatchingSource(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Set cursors for multiple sources.
	mem.SetCursor("source-a", "cursor-a-value")
	mem.SetCursor("source-b", "cursor-b-value")
	// source-c has no stored cursor.

	capturedA := make(map[string]string)
	capturedB := make(map[string]string)
	capturedC := make(map[string]string)

	collectorA := &mockCollector{
		name: "source-a",
		collectFn: func(req domain.CollectRequest) ([]domain.RawSignal, error) {
			capturedA = req.Cursor
			return nil, nil
		},
	}
	collectorB := &mockCollector{
		name: "source-b",
		collectFn: func(req domain.CollectRequest) ([]domain.RawSignal, error) {
			capturedB = req.Cursor
			return nil, nil
		},
	}
	collectorC := &mockCollector{
		name: "source-c",
		collectFn: func(req domain.CollectRequest) ([]domain.RawSignal, error) {
			capturedC = req.Cursor
			return nil, nil
		},
	}

	// Must use a real beforeStats to avoid panic.
	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{collectorA, collectorB, collectorC},
		selectedSources: []string{"source-a", "source-b", "source-c"},
		before:          &beforeStats,
		resume:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect failed: %v", err)
	}

	// source-a should receive its cursor.
	if len(capturedA) != 1 || capturedA["source-a"] != "cursor-a-value" {
		t.Errorf("source-a expected cursor, got %v", capturedA)
	}

	// source-b should receive its cursor.
	if len(capturedB) != 1 || capturedB["source-b"] != "cursor-b-value" {
		t.Errorf("source-b expected cursor, got %v", capturedB)
	}

	// source-c (no stored cursor) should receive nil cursor.
	if capturedC != nil {
		t.Errorf("source-c expected nil cursor, got %v", capturedC)
	}
}

func TestExecuteCollect_NoResumeNoCursor(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	mem.SetCursor("test-source", "stored-cursor")

	captured := make(map[string]string)
	collector := &mockCollector{
		name: "test-source",
		collectFn: func(req domain.CollectRequest) ([]domain.RawSignal, error) {
			captured = req.Cursor
			return nil, nil
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		resume:          false, // resume is off
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect failed: %v", err)
	}

	if captured != nil {
		t.Errorf("expected nil cursor when resume is disabled, got %v", captured)
	}
}

// testCursorCollector implements domain.SourceCollector plus cursorAware for testing.
type testCursorCollector struct {
	mockCollector
	returnCursor map[string]string
}

func (tcc *testCursorCollector) Cursor() map[string]string {
	return tcc.returnCursor
}

func TestExecuteCollect_ResumePersistsCursor(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Set an initial cursor.
	mem.SetCursor("cursor-source", "initial-cursor")

	collector := &testCursorCollector{
		mockCollector: mockCollector{
			name: "cursor-source",
			collectFn: func(_ domain.CollectRequest) ([]domain.RawSignal, error) {
				return nil, nil
			},
		},
		returnCursor: map[string]string{"cursor-source": "updated-cursor"},
	}

	// Verify the Cursor() method is defined.
	_, implementsCursor := interface{}(collector).(cursorAware)
	if !implementsCursor {
		t.Fatal("collector should implement cursorAware")
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"cursor-source"},
		before:          &beforeStats,
		resume:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect failed: %v", err)
	}

	// Check that the cursor was persisted back to memory.
	cursor, exists := mem.GetCursor("cursor-source")
	if !exists {
		t.Fatal("expected cursor after collection")
	}
	if cursor != "updated-cursor" {
		t.Errorf("expected updated-cursor, got %q", cursor)
	}
}
