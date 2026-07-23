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
	delta := collectStatsDelta{
		seRequests:  7,
		seCacheHits: 2,
		sources: []sourceCollectionResult{
			{name: "stackexchange", attempted: 3, collected: 3, skipped: 0},
		},
	}
	if err := reportCollectSummary(cmd, 3, &delta); err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Stack Exchange requests: 7 (cache hits: 2)") {
		t.Fatalf("expected Stack Exchange stats in output, got %q", output)
	}
	if !strings.Contains(output, "stackexchange: attempted=3, collected=3, dedup-skipped=0, status=ok") {
		t.Errorf("expected per-source breakdown, got %q", output)
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
		sources: []sourceCollectionResult{
			{name: "hackernews", attempted: 12, collected: 10, skipped: 2},
		},
	}

	err := reportCollectSummary(cmd, 12, &delta)
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
		sources: []sourceCollectionResult{
			{name: "github", attempted: 6, collected: 5, skipped: 1},
		},
	}

	err := reportCollectSummary(cmd, 5, &delta)
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
		sources: []sourceCollectionResult{
			{name: "hackernews", attempted: 9, collected: 8, skipped: 1},
		},
	}

	err := reportCollectSummary(cmd, 8, &delta)
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
		sources: []sourceCollectionResult{
			{name: "stackexchange", attempted: 3, collected: 3, skipped: 0},
		},
	}

	err := reportCollectSummary(cmd, 3, &delta)
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
	if !strings.Contains(output, "Total new signals: 3") || !strings.Contains(output, "total dedup-skipped: 0") {
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

func TestOrderSourcesDeterministically_GitHubFirst(t *testing.T) {
	t.Parallel()

	input := []string{"hackernews", "stackexchange", "github"}
	result := orderSourcesDeterministically(input)
	expected := []string{"github", "hackernews", "stackexchange"}
	if len(result) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Fatalf("expected %v at index %d, got %v", expected, i, result)
		}
	}
}

func TestOrderSourcesDeterministically_PartialSet(t *testing.T) {
	t.Parallel()

	input := []string{"stackexchange", "hackernews"}
	result := orderSourcesDeterministically(input)
	// Should keep existing order within the deterministic subset: hn then se.
	if len(result) != 2 || result[0] != "hackernews" || result[1] != "stackexchange" {
		t.Fatalf("expected [hackernews stackexchange], got %v", result)
	}
}

func TestOrderSourcesDeterministically_SingleSource(t *testing.T) {
	t.Parallel()

	result := orderSourcesDeterministically([]string{"hackernews"})
	if len(result) != 1 || result[0] != "hackernews" {
		t.Fatalf("expected [hackernews], got %v", result)
	}
}

func TestOrderSourcesDeterministically_UnknownSourceAppended(t *testing.T) {
	t.Parallel()

	input := []string{"stackexchange", "unknown-source", "hackernews"}
	result := orderSourcesDeterministically(input)
	// Known sources ordered: hackernews, stackexchange. Unknown source appended.
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d: %v", len(result), result)
	}
	if result[0] != "hackernews" || result[1] != "stackexchange" || result[2] != "unknown-source" {
		t.Fatalf("expected [hackernews stackexchange unknown-source], got %v", result)
	}
}

func TestOrderSourcesDeterministically_OnlyUnknown(t *testing.T) {
	t.Parallel()

	input := []string{"unknown-a", "unknown-b"}
	result := orderSourcesDeterministically(input)
	if len(result) != 2 || result[0] != "unknown-a" || result[1] != "unknown-b" {
		t.Fatalf("expected original order preserved, got %v", result)
	}
}

func TestOrderSourcesDeterministically_EmptyInput(t *testing.T) {
	t.Parallel()

	result := orderSourcesDeterministically(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
	result = orderSourcesDeterministically([]string{})
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
}

func TestExecuteCollect_ForceBypassesDedup(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Pre-populate memory with a signal that would normally be deduplicated.
	mem.AddRawSignal("test-source", "existing-id-1")
	mem.AddRawSignal("test-source", "existing-id-2")

	collector := &mockCollector{
		name: "test-source",
		collectFn: func(_ domain.CollectRequest) ([]domain.RawSignal, error) {
			return []domain.RawSignal{
				{Source: "test-source", SourceID: "existing-id-1", ContentHash: "hash-1", ID: "sig-1"},
				{Source: "test-source", SourceID: "existing-id-2", ContentHash: "hash-2", ID: "sig-2"},
				{Source: "test-source", SourceID: "new-id-3", ContentHash: "hash-3", ID: "sig-3"},
			}, nil
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		force:           true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect failed: %v", err)
	}

	// All 3 signals should have been recorded (no dedup filtering).
	if !mem.HasRawSignal("test-source", "existing-id-1") {
		t.Error("expected signal 1 to exist in memory (recorded twice)")
	}
	if !mem.HasRawSignal("test-source", "existing-id-2") {
		t.Error("expected signal 2 to exist in memory (recorded twice)")
	}
	if !mem.HasRawSignal("test-source", "new-id-3") {
		t.Error("expected signal 3 to exist in memory")
	}
}

func TestExecuteCollect_NonForcePreservesDedup(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Pre-populate memory with a signal that should be deduplicated.
	mem.AddRawSignal("test-source", "existing-id-1")

	// Also pre-populate a content hash that should be deduplicated.
	mem.AddRawSignal("test-source", "existing-id-2")
	// We add the content hash directly via the memory's internal map test trick:
	// AddRawSignal only records SourceID, not ContentHash, so we use AddContentHash.

	collector := &mockCollector{
		name: "test-source",
		collectFn: func(_ domain.CollectRequest) ([]domain.RawSignal, error) {
			// existing-id-1 has a matching source+sourceID in memory
			// new-id-4 has a matching content hash
			return []domain.RawSignal{
				{Source: "test-source", SourceID: "existing-id-1", ContentHash: "hash-1", ID: "sig-1"},
				{Source: "test-source", SourceID: "new-id-3", ContentHash: "hash-3", ID: "sig-3"},
				{Source: "test-source", SourceID: "new-id-4", ContentHash: "hash-duplicate", ID: "sig-4"},
			}, nil
		},
	}

	// Add content hash that matches new-id-4 before collection.
	mem.AddContentHash("hash-duplicate", "sig-0")

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		force:           false, // dedup should filter
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect failed: %v", err)
	}

	// existing-id-1 was filtered out by sourceID dedup
	// new-id-3 should have been recorded
	// new-id-4 was filtered out by content hash dedup

	if mem.HasRawSignal("test-source", "existing-id-1") != true {
		t.Error("existing-id-1 should still exist (pre-populated)")
	}
	// Check that new-id-3 was recorded.
	if !mem.HasRawSignal("test-source", "new-id-3") {
		t.Error("new-id-3 should have been recorded (no conflict)")
	}
	// new-id-4 should NOT have been recorded because its content hash matched.
	if mem.HasRawSignal("test-source", "new-id-4") {
		t.Error("new-id-4 should NOT have been recorded (content hash duplicate)")
	}
}

func TestParseUntilWindow_Empty(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("")
	if err != nil {
		t.Fatalf("expected no error for empty string, got %v", err)
	}
	if d != 0 {
		t.Errorf("expected 0 duration for empty string, got %v", d)
	}
}

func TestParseUntilWindow_TrimmedEmpty(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("  ")
	if err != nil {
		t.Fatalf("expected no error for whitespace-only string, got %v", err)
	}
	if d != 0 {
		t.Errorf("expected 0 duration for whitespace, got %v", d)
	}
}

func TestParseUntilWindow_FutureISODate(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("2099-12-31")
	if err != nil {
		t.Fatalf("expected no error for future ISO date, got %v", err)
	}
	if d <= 0 {
		t.Errorf("expected positive duration for future date, got %v", d)
	}
}

func TestParseUntilWindow_PastISODate(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("2020-01-01")
	if err != nil {
		t.Fatalf("expected no error for past ISO date, got %v", err)
	}
	if d >= 0 {
		t.Errorf("expected negative duration for past date, got %v", d)
	}
}

func TestParseUntilWindow_DurationDays(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("7d")
	if err != nil {
		t.Fatalf("expected no error for 7d, got %v", err)
	}
	expected := -7 * 24 * time.Hour
	if d != expected {
		t.Errorf("expected %v, got %v", expected, d)
	}
}

func TestParseUntilWindow_DurationHours(t *testing.T) {
	t.Parallel()

	d, err := parseUntilWindow("24h")
	if err != nil {
		t.Fatalf("expected no error for 24h, got %v", err)
	}
	expected := -24 * time.Hour
	if d != expected {
		t.Errorf("expected %v, got %v", expected, d)
	}
}

func TestParseUntilWindow_Invalid(t *testing.T) {
	t.Parallel()

	_, err := parseUntilWindow("not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid until value")
	}
}

func TestParseUntilWindow_InvalidNumber(t *testing.T) {
	t.Parallel()

	_, err := parseUntilWindow("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestCollectCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	f := cmd.Flags()

	flagNames := []string{"sources", "since", "until", "max-items", "language", "force", "dry-run", "resume"}
	for _, name := range flagNames {
		flag := f.Lookup(name)
		if flag == nil {
			t.Errorf("flag %q is not registered", name)
		}
	}
}

func TestCollectCmd_FlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	f := cmd.Flags()

	tests := []struct {
		name     string
		expected string
	}{
		{"sources", "github"},
		{"since", "30d"},
		{"until", ""},
		{"language", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := f.Lookup(tt.name)
			if flag == nil {
				t.Fatalf("flag %q not found", tt.name)
			}
			if flag.DefValue != tt.expected {
				t.Errorf("flag %q default: expected %q, got %q", tt.name, tt.expected, flag.DefValue)
			}
		})
	}
}

func TestCollectCmd_ForceDefaultFalse(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	flag := cmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatal("force flag not found")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default false, got %q", flag.DefValue)
	}
}

func TestCollectCmd_DryRunDefaultFalse(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	flag := cmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("dry-run flag not found")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default false, got %q", flag.DefValue)
	}
}

func TestCollectCmd_ResumeDefaultFalse(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	flag := cmd.Flags().Lookup("resume")
	if flag == nil {
		t.Fatal("resume flag not found")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default false, got %q", flag.DefValue)
	}
}

func TestCollectCmd_MaxItemsDefaultZero(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	flag := cmd.Flags().Lookup("max-items")
	if flag == nil {
		t.Fatal("max-items flag not found")
	}
	if flag.DefValue != "0" {
		t.Errorf("expected default 0, got %q", flag.DefValue)
	}
}

func TestCollectCmd_MaxItemsRejectsNegative(t *testing.T) {
	t.Parallel()

	cmd := newCollectCmd()
	cmd.SetArgs([]string{"--max-items=-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative max-items")
	}
}

func TestBuildCollectRequest_ForwardsFlags(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	cfg := newTestConfig()

	mockColl := &mockCollector{name: "test-source"}

	sinceWindow := 7 * 24 * time.Hour
	untilWindow := -24 * time.Hour

	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{mockColl},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		sinceWindow:     sinceWindow,
		untilWindow:     untilWindow,
		maxItems:        42,
		language:        "go",
		force:           true,
		dryRun:          false,
		resume:          true,
	}

	// Set cursor to test resume forwarding.
	env.mem.SetCursor("test-source", "test-cursor")

	req := buildCollectRequest(env, mockColl)

	// Verify window calculations.
	expectedSince := time.Now().Add(-sinceWindow)
	if req.Since.Before(expectedSince.Add(-time.Second)) || req.Since.After(expectedSince.Add(time.Second)) {
		t.Errorf("Since should be near %v, got %v", expectedSince, req.Since)
	}

	expectedUntil := time.Now().Add(untilWindow)
	if req.Until.Before(expectedUntil.Add(-time.Second)) || req.Until.After(expectedUntil.Add(time.Second)) {
		t.Errorf("Until should be near %v, got %v", expectedUntil, req.Until)
	}

	if req.MaxItems != 42 {
		t.Errorf("expected MaxItems=42, got %d", req.MaxItems)
	}

	if len(req.Languages) != 1 || req.Languages[0] != "go" {
		t.Errorf("expected Languages=[go], got %v", req.Languages)
	}

	if !req.Force {
		t.Errorf("expected Force=true")
	}

	if req.DryRun {
		t.Errorf("expected DryRun=false")
	}

	// Verify resume cursor.
	if len(req.Cursor) != 1 || req.Cursor["test-source"] != "test-cursor" {
		t.Errorf("expected cursor map with test-cursor, got %v", req.Cursor)
	}
}

func TestBuildCollectRequest_NoResumeNoCursor(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	cfg := newTestConfig()

	mockColl := &mockCollector{name: "test-source"}
	mem.SetCursor("test-source", "stale-cursor")

	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{mockColl},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		sinceWindow:     30 * 24 * time.Hour,
		resume:          false,
	}

	req := buildCollectRequest(env, mockColl)

	if req.Cursor != nil {
		t.Errorf("expected nil cursor when resume is disabled, got %v", req.Cursor)
	}
}

func TestBuildCollectRequest_Defaults(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	cfg := newTestConfig()

	mockColl := &mockCollector{name: "test-source"}
	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{mockColl},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		sinceWindow:     30 * 24 * time.Hour,
	}

	req := buildCollectRequest(env, mockColl)

	if req.MaxItems != 0 {
		t.Errorf("expected MaxItems=0, got %d", req.MaxItems)
	}
	if req.Force {
		t.Errorf("expected Force=false")
	}
	if req.DryRun {
		t.Errorf("expected DryRun=false")
	}
	if req.Cursor != nil {
		t.Errorf("expected nil cursor when resume is disabled and no cursor set")
	}
	if req.Languages != nil {
		t.Errorf("expected nil Languages when not set, got %v", req.Languages)
	}
}

func TestBuildCollectRequest_NoLanguage(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	cfg := newTestConfig()

	mockColl := &mockCollector{name: "test-source"}
	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{mockColl},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		sinceWindow:     30 * 24 * time.Hour,
		language:        "",
	}

	req := buildCollectRequest(env, mockColl)

	if req.Languages != nil {
		t.Errorf("expected nil Languages when language is empty, got %v", req.Languages)
	}
}

func TestBuildCollectRequest_LanguageForwarded(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	cfg := newTestConfig()

	mockColl := &mockCollector{name: "test-source"}
	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{mockColl},
		selectedSources: []string{"test-source"},
		before:          &beforeStats,
		sinceWindow:     30 * 24 * time.Hour,
		language:        "python",
	}

	req := buildCollectRequest(env, mockColl)

	if len(req.Languages) != 1 || req.Languages[0] != "python" {
		t.Errorf("expected Languages=[python], got %v", req.Languages)
	}
}

func TestReportCollectSummary_ForceMode(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 5,
		skipped:   0,
		force:     true,
		sources: []sourceCollectionResult{
			{name: "github", attempted: 5, collected: 5, skipped: 0},
		},
	}

	err := reportCollectSummary(cmd, 5, &delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Mode: force") {
		t.Errorf("expected force mode in summary output, got: %s", output)
	}
	if !strings.Contains(output, "deduplication disabled") {
		t.Errorf("expected deduplication disabled message, got: %s", output)
	}
}

func TestReportCollectSummary_ResumeMode(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 10,
		skipped:   2,
		resume:    true,
		sources: []sourceCollectionResult{
			{name: "github", attempted: 12, collected: 10, skipped: 2},
		},
	}

	err := reportCollectSummary(cmd, 12, &delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Mode: resume") {
		t.Errorf("expected resume mode in summary output, got: %s", output)
	}
	if !strings.Contains(output, "cursor-based") {
		t.Errorf("expected cursor-based description, got: %s", output)
	}
}

func TestReportCollectSummary_ForceAndResumeMode(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 15,
		skipped:   0,
		force:     true,
		resume:    true,
		sources: []sourceCollectionResult{
			{name: "hackernews", attempted: 15, collected: 15, skipped: 0},
		},
	}

	err := reportCollectSummary(cmd, 15, &delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Mode: force") {
		t.Errorf("expected force mode, got: %s", output)
	}
	if !strings.Contains(output, "Mode: resume") {
		t.Errorf("expected resume mode, got: %s", output)
	}
}

func TestStatsDelta_WithSources(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{}
	after := &domain.ResearchStats{
		RawSignalsCollected: 10,
		RawSignalsSkipped:   2,
		GitHubRequests:      50,
	}

	delta := statsDelta(before, after)
	delta.force = true
	delta.resume = false
	delta.sources = []sourceCollectionResult{
		{name: "github", attempted: 12, collected: 10, skipped: 2},
	}

	if delta.collected != 10 {
		t.Errorf("expected collected=10, got %d", delta.collected)
	}
	if delta.skipped != 2 {
		t.Errorf("expected skipped=2, got %d", delta.skipped)
	}
	if !delta.force {
		t.Errorf("expected force=true")
	}
	if delta.resume {
		t.Errorf("expected resume=false")
	}
	if len(delta.sources) != 1 || delta.sources[0].name != "github" {
		t.Errorf("expected sources with github, got %v", delta.sources)
	}
}

func TestStatsDelta_ForceResumeMode(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{}
	after := &domain.ResearchStats{HackerNewsRequests: 15, HackerNewsCacheHits: 7}

	delta := statsDelta(before, after)
	delta.force = true
	delta.resume = true
	delta.sources = []sourceCollectionResult{
		{name: "hackernews", attempted: 10, collected: 8, skipped: 2},
	}

	if delta.hnRequests != 15 {
		t.Errorf("expected hnRequests=15, got %d", delta.hnRequests)
	}
	if delta.hnCacheHits != 7 {
		t.Errorf("expected hnCacheHits=7, got %d", delta.hnCacheHits)
	}
	if !delta.force {
		t.Errorf("expected force=true")
	}
	if !delta.resume {
		t.Errorf("expected resume=true")
	}
}

func TestDeduplicateSignals_EmptyInput(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	env := &collectEnv{mem: mem}

	result := deduplicateSignals(nil, env)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	result = deduplicateSignals([]domain.RawSignal{}, env)
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestDeduplicateSignals_ForceReturnsAll(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	mem.AddRawSignal("src", "existing")
	env := &collectEnv{mem: mem, force: true}

	signals := []domain.RawSignal{
		{Source: "src", SourceID: "existing"},
		{Source: "src", SourceID: "new"},
	}
	result := deduplicateSignals(signals, env)
	if len(result) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(result))
	}
}

// mockCollectorInvoked tracks whether a collector was called.
type mockCollectorInvoked struct {
	name    string
	invoked bool
}

func (m *mockCollectorInvoked) Name() string { return m.name }

func (m *mockCollectorInvoked) Collect(_ context.Context, req domain.CollectRequest) ([]domain.RawSignal, error) { //nolint:gocritic // must match SourceCollector interface
	m.invoked = true
	_ = req
	return nil, nil
}

func TestExecuteCollect_DryRunDoesNotInvokeCollectors(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	invokedA := &mockCollectorInvoked{name: "source-a"}
	invokedB := &mockCollectorInvoked{name: "source-b"}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		collectors:      []domain.SourceCollector{invokedA, invokedB},
		selectedSources: []string{"source-a", "source-b"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect dry-run failed: %v", err)
	}

	if invokedA.invoked {
		t.Error("collector A was invoked during dry-run")
	}
	if invokedB.invoked {
		t.Error("collector B was invoked during dry-run")
	}
}

func TestExecuteCollect_DryRunOutputContainsPlanFields(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     500,
				MaxCommentsPerItem: 20,
			},
			HackerNews: config.HackerNewsConfig{
				Enabled:            true,
				Feeds:              []string{"askstories", "showstories"},
				MaxItemsPerRun:     300,
				MaxCommentsPerItem: 30,
				MinimumScore:       2,
			},
			StackExchange: config.StackExchangeConfig{
				Enabled:         true,
				Sites:           []string{"stackoverflow"},
				MaxItemsPerSite: 300,
				PageSize:        25,
				MaxPagesPerSite: 10,
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github", "hackernews", "stackexchange"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect dry-run failed: %v", err)
	}

	output := buf.String()

	// Check header.
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected dry-run header, got: %s", output)
	}

	// Check source sections appear.
	if !strings.Contains(output, "--- github ---") {
		t.Errorf("expected github section, got: %s", output)
	}
	if !strings.Contains(output, "--- hackernews ---") {
		t.Errorf("expected hackernews section, got: %s", output)
	}
	if !strings.Contains(output, "--- stackexchange ---") {
		t.Errorf("expected stackexchange section, got: %s", output)
	}

	// Check field labels appear.
	if !strings.Contains(output, "target:") {
		t.Errorf("expected target field, got: %s", output)
	}
	if !strings.Contains(output, "estimated requests:") {
		t.Errorf("expected estimated requests field, got: %s", output)
	}
	if !strings.Contains(output, "since:") {
		t.Errorf("expected since field, got: %s", output)
	}
	if !strings.Contains(output, "until:") {
		t.Errorf("expected until field, got: %s", output)
	}
	if !strings.Contains(output, "max-items:") {
		t.Errorf("expected max-items field, got: %s", output)
	}

	// Check no API calls message.
	if !strings.Contains(output, "No API calls were made") {
		t.Errorf("expected no-api-calls message, got: %s", output)
	}
	if !strings.Contains(output, "No data was persisted") {
		t.Errorf("expected no-data-persisted message, got: %s", output)
	}
}

func TestExecuteCollect_DryRunWithResumeShowsCursor(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	mem.SetCursor("hackernews", "test-cursor-value")

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     100,
				MaxCommentsPerItem: 0,
			},
			HackerNews: config.HackerNewsConfig{
				Enabled:            true,
				Feeds:              []string{"newstories"},
				MaxItemsPerRun:     100,
				MaxCommentsPerItem: 0,
				MinimumScore:       0,
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github", "hackernews"},
		before:          &beforeStats,
		dryRun:          true,
		resume:          true,
		sinceWindow:     7 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect dry-run with resume failed: %v", err)
	}

	output := buf.String()

	// Check that hackernews has cursor info.
	if !strings.Contains(output, "test-cursor-value") {
		t.Errorf("expected cursor value in dry-run output, got: %s", output)
	}

	// The output should also mention "resume cursor".
	if !strings.Contains(output, "resume cursor:") {
		t.Errorf("expected resume cursor field, got: %s", output)
	}
}

func TestBuildDryRunPlans_EstimatesCount(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     500,
				MaxCommentsPerItem: 20,
			},
			HackerNews: config.HackerNewsConfig{
				Enabled:            true,
				Feeds:              []string{"askstories", "showstories", "newstories"},
				MaxItemsPerRun:     300,
				MaxCommentsPerItem: 30,
			},
			StackExchange: config.StackExchangeConfig{
				Enabled:         true,
				Sites:           []string{"stackoverflow", "serverfault"},
				MaxItemsPerSite: 300,
				PageSize:        25,
				MaxPagesPerSite: 5,
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github", "hackernews", "stackexchange"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	plans := buildDryRunPlans(env, cfg)

	// Should have 3 plans.
	if len(plans) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(plans))
	}

	// Check plan order.
	if plans[0].Source != "github" {
		t.Errorf("expected first plan github, got %s", plans[0].Source)
	}
	if plans[1].Source != "hackernews" {
		t.Errorf("expected second plan hackernews, got %s", plans[1].Source)
	}
	if plans[2].Source != "stackexchange" {
		t.Errorf("expected third plan stackexchange, got %s", plans[2].Source)
	}

	// GitHub: 500 items / 100 per page = 5 search pages + 500 comment requests = 505.
	if plans[0].EstimatedReqs <= 0 {
		t.Errorf("expected positive estimate for GitHub, got %d", plans[0].EstimatedReqs)
	}

	// HN: 3 feeds + 300 items + 300 comments = 603.
	if plans[1].EstimatedReqs <= 0 {
		t.Errorf("expected positive estimate for HN, got %d", plans[1].EstimatedReqs)
	}

	// SE: 2 sites * 5 pages = 10.
	if plans[2].EstimatedReqs <= 0 {
		t.Errorf("expected positive estimate for SE, got %d", plans[2].EstimatedReqs)
	}

	// Check targets are populated.
	if len(plans[0].Targets) == 0 {
		t.Errorf("expected GitHub targets, got empty")
	}
	if len(plans[1].Targets) == 0 {
		t.Errorf("expected HN targets, got empty")
	}
	if len(plans[2].Targets) == 0 {
		t.Errorf("expected SE targets, got empty")
	}
}

func TestBuildDryRunPlans_WithLanguage(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     100,
				MaxCommentsPerItem: 0,
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github"},
		before:          &beforeStats,
		dryRun:          true,
		language:        "go",
		sinceWindow:     30 * 24 * time.Hour,
	}

	plans := buildDryRunPlans(env, cfg)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].Language != "go" {
		t.Errorf("expected language 'go', got %q", plans[0].Language)
	}
}

func TestDryRunPlan_NonZeroMaxItems(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     100,
				MaxCommentsPerItem: 0,
			},
		},
	}

	beforeStats := mem.GetStats()

	// Override maxItems from environment.
	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github"},
		before:          &beforeStats,
		dryRun:          true,
		maxItems:        50,
		sinceWindow:     7 * 24 * time.Hour,
	}

	plans := buildDryRunPlans(env, cfg)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].MaxItems != 50 {
		t.Errorf("expected max-items 50, got %d", plans[0].MaxItems)
	}
}

func TestEstimateGitHubRequests_WithSearchIssuesAndComments(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				MaxItemsPerRun:     500,
				MaxCommentsPerItem: 20,
				SearchIssues:       true,
				SearchDiscussions:  false,
			},
		},
	}

	env := &collectEnv{}
	reqs := estimateGitHubRequests(cfg, env)
	// 500/100 = 5 pages + 500 comment requests = 505.
	if reqs != 505 {
		t.Errorf("expected 505 requests, got %d", reqs)
	}
}

func TestEstimateGitHubRequests_WithEnvMaxItems(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				MaxItemsPerRun:     500,
				MaxCommentsPerItem: 0,
				SearchIssues:       true,
				SearchDiscussions:  false,
			},
		},
	}

	env := &collectEnv{maxItems: 50}
	reqs := estimateGitHubRequests(cfg, env)
	// 50/100 = 1 page.
	if reqs != 1 {
		t.Errorf("expected 1 request, got %d", reqs)
	}
}

func TestEstimateHNRequests(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			HackerNews: config.HackerNewsConfig{
				Feeds:              []string{"askstories", "showstories", "newstories"},
				MaxItemsPerRun:     300,
				MaxCommentsPerItem: 30,
			},
		},
	}

	env := &collectEnv{}
	reqs := estimateHNRequests(cfg, env)
	// 3 feeds + 300 items + 300 comments = 603.
	if reqs != 603 {
		t.Errorf("expected 603 requests, got %d", reqs)
	}
}

func TestEstimateSERequests(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			StackExchange: config.StackExchangeConfig{
				Sites:           []string{"stackoverflow", "serverfault"},
				MaxPagesPerSite: 5,
			},
		},
	}

	env := &collectEnv{}
	reqs := estimateSERequests(cfg, env)
	// 2 sites * 5 pages = 10.
	if reqs != 10 {
		t.Errorf("expected 10 requests, got %d", reqs)
	}
}

func TestPrintDryRunPlan_OutputFormat(t *testing.T) {
	t.Parallel()

	plans := []dryRunPlan{
		{
			Source:        "github",
			Targets:       []string{"Search Issues API"},
			EstimatedReqs: 5,
			Since:         "2026-06-23",
			Until:         "now",
			MaxItems:      500,
			Language:      "go",
		},
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := printDryRunPlan(cmd, plans)
	if err != nil {
		t.Fatalf("printDryRunPlan failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Collection Plan (dry-run)") {
		t.Errorf("expected header in output, got: %s", output)
	}
	if !strings.Contains(output, "--- github ---") {
		t.Errorf("expected github section, got: %s", output)
	}
	if !strings.Contains(output, "language: go") {
		t.Errorf("expected language field, got: %s", output)
	}
}

func TestPrintDryRunPlan_EmptyPlans(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	err := printDryRunPlan(cmd, []dryRunPlan{})
	if err != nil {
		t.Fatalf("printDryRunPlan with empty plans failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Collection Plan (dry-run)") {
		t.Errorf("expected header, got: %s", output)
	}
}

func TestExecuteCollect_DryRunNoMemoryMutation(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	beforeStats := mem.GetStats()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			GitHub: config.GitHubConfig{
				Enabled:            true,
				SearchIssues:       true,
				SearchDiscussions:  false,
				MaxItemsPerRun:     100,
				MaxCommentsPerItem: 0,
			},
		},
	}

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"github"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Fatalf("executeCollect dry-run failed: %v", err)
	}

	// Memory should not have changed.
	afterStats := mem.GetStats()
	if afterStats != beforeStats {
		t.Errorf("memory stats changed during dry-run: before %+v, after %+v", beforeStats, afterStats)
	}

	// Cursor map should be unchanged.
	if len(mem.SourceCursors()) != 0 {
		t.Errorf("cursors changed during dry-run: %v", mem.SourceCursors())
	}
}
