package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/memory"
	"github.com/moontechs/signalforge/internal/sources/github"
	"github.com/moontechs/signalforge/internal/sources/hackernews"
	"github.com/moontechs/signalforge/internal/sources/reddit"
	"github.com/moontechs/signalforge/internal/sources/stackexchange"
	"github.com/moontechs/signalforge/internal/storage"
)

type redditTransportFunc func(*http.Request) (*http.Response, error)

func (f redditTransportFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func redditTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

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

func TestBuildCollector_RedditDisabledAndCredentials(t *testing.T) {
	t.Setenv("REDDIT_CLIENT_ID", "client")
	t.Setenv("REDDIT_CLIENT_SECRET", "secret")
	cfg := newTestConfig()
	store := newTestStorage(t)
	if _, err := buildCollector("reddit", cfg, store); err == nil {
		t.Fatal("expected disabled Reddit error")
	}
	cfg.Sources.Reddit.Enabled = true
	cfg.Sources.Reddit.Subreddits = []string{"saas"}
	cfg.Sources.Reddit.MaxPostsPerRun = 1
	cfg.Sources.Reddit.MaxCommentsPerPost = 0
	cfg.Sources.Reddit.Sort = "TOP"
	cfg.Sources.Reddit.Time = "MONTH"
	collector, err := buildCollector("reddit", cfg, store)
	if err != nil {
		t.Fatalf("buildCollector(reddit) failed: %v", err)
	}
	redditCollector, ok := collector.(*reddit.Collector)
	if !ok {
		t.Fatalf("expected Reddit collector, got %T", collector)
	}

	var listingURL string
	redditCollector.WithTransport(redditTransportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "www.reddit.com":
			return redditTestResponse(http.StatusOK, `{"access_token":"token","expires_in":3600}`), nil
		case "oauth.reddit.com":
			listingURL = req.URL.String()
			return redditTestResponse(http.StatusOK, `{"data":{"children":[],"after":""}}`), nil
		default:
			return nil, errors.New("unexpected Reddit host")
		}
	}))
	if _, err := redditCollector.Collect(context.Background(), domain.CollectRequest{}); err != nil {
		t.Fatalf("collect with built Reddit collector: %v", err)
	}
	if listingURL != "https://oauth.reddit.com/r/saas/top.json?limit=1&t=month" {
		t.Fatalf("listing URL = %q, want configured sort and time", listingURL)
	}
}

func TestBuildCollector_RedditMissingCredentials(t *testing.T) {
	t.Setenv("REDDIT_CLIENT_ID", "")
	t.Setenv("REDDIT_CLIENT_SECRET", "")
	cfg := newTestConfig()
	cfg.Sources.Reddit.Enabled = true
	cfg.Sources.Reddit.Subreddits = []string{"saas"}
	cfg.Sources.Reddit.MaxPostsPerRun = 1
	if _, err := buildCollector("reddit", cfg, newTestStorage(t)); err == nil || !strings.Contains(err.Error(), "REDDIT_CLIENT_ID") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}

func TestRedditDryRunPlan(t *testing.T) {
	cfg := newTestConfig()
	cfg.Sources.Reddit.Enabled = true
	cfg.Sources.Reddit.Subreddits = []string{"saas", "indiehackers"}
	cfg.Sources.Reddit.MaxPostsPerRun = 4
	cfg.Sources.Reddit.MaxCommentsPerPost = 2
	env := &collectEnv{selectedSources: []string{"reddit"}, maxItems: 3, sinceWindow: 24 * time.Hour}
	plans := buildDryRunPlans(env, cfg)
	if len(plans) != 1 || len(plans[0].Targets) != 2 || plans[0].Targets[0] != "subreddit: saas" || plans[0].EstimatedReqs != 6 {
		t.Fatalf("unexpected Reddit dry run plan: %+v", plans)
	}
}

func TestSetupCollectEnv_RedditDryRunDoesNotRequireCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SIGNALFORGE_HOME", dir)
	t.Setenv("REDDIT_CLIENT_ID", "")
	t.Setenv("REDDIT_CLIENT_SECRET", "")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Sources.Reddit.Enabled = true
	cfg.Sources.Reddit.Subreddits = []string{"saas"}
	if err := config.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}

	env, err := setupCollectEnv("reddit", "30d", "", 0, "", false, true, false)
	if err != nil {
		t.Fatalf("dry-run setup checked credentials: %v", err)
	}
	if len(env.collectors) != 0 || !env.dryRun {
		t.Fatalf("unexpected dry-run environment: %+v", env)
	}
	cmd := &cobra.Command{}
	output := new(strings.Builder)
	cmd.SetOut(output)
	if err := executeCollect(cmd, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "subreddit: saas") {
		t.Fatalf("missing Reddit dry-run target: %q", output.String())
	}
}

func TestEstimateRedditRequestsWithoutComments(t *testing.T) {
	cfg := newTestConfig()
	cfg.Sources.Reddit.Subreddits = []string{"go", "golang"}
	cfg.Sources.Reddit.MaxPostsPerRun = 250
	cfg.Sources.Reddit.MaxCommentsPerPost = 0
	if got := estimateRedditRequests(cfg, &collectEnv{}); got != 7 {
		t.Fatalf("estimate = %d, want token plus three listing pages per subreddit", got)
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

func TestStatsDelta_Reddit(t *testing.T) {
	delta := statsDelta(&domain.ResearchStats{RedditRequests: 2, RedditCacheHits: 1}, &domain.ResearchStats{RedditRequests: 8, RedditCacheHits: 4})
	if delta.redditRequests != 6 || delta.redditCacheHits != 3 {
		t.Fatalf("unexpected Reddit delta: %+v", delta)
	}
}

func TestReportCollectSummary_Reddit(t *testing.T) {
	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	delta := collectStatsDelta{redditRequests: 6, redditCacheHits: 3}
	if err := reportCollectSummary(cmd, 0, &delta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Reddit requests: 6 (cache hits: 3)") {
		t.Fatalf("missing Reddit summary: %q", buf.String())
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

	input := []string{"reddit", "hackernews", "stackexchange", "github"}
	result := orderSourcesDeterministically(input)
	expected := []string{"github", "hackernews", "stackexchange", "reddit"}
	if len(result) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Fatalf("expected %v at index %d, got %v", expected, i, result)
		}
	}
}

func TestResolveCollectSources_Reddit(t *testing.T) {
	t.Parallel()
	sources, err := resolveCollectSources("reddit")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0] != "reddit" {
		t.Fatalf("sources = %v", sources)
	}
}

func TestExecuteCollect_PersistsPartialResultsAndContinues(t *testing.T) {
	store := storage.New(t.TempDir())
	mem := memory.New(store)
	first := &mockCollector{
		name: "first",
		collectFn: func(domain.CollectRequest) ([]domain.RawSignal, error) {
			return []domain.RawSignal{{
				ID:          "first:one",
				Source:      "first",
				SourceID:    "one",
				ContentHash: "hash-one",
			}}, errors.New("partial failure")
		},
	}
	secondInvoked := false
	second := &mockCollector{
		name: "second",
		collectFn: func(domain.CollectRequest) ([]domain.RawSignal, error) {
			secondInvoked = true
			return []domain.RawSignal{
				{
					ID:          "second:two",
					Source:      "second",
					SourceID:    "two",
					ContentHash: "hash-one",
				},
				{
					ID:          "second:three",
					Source:      "second",
					SourceID:    "three",
					ContentHash: "hash-two",
				},
			}, nil
		},
	}
	before := mem.GetStats()
	env := &collectEnv{
		store:           store,
		mem:             mem,
		collectors:      []domain.SourceCollector{first, second},
		selectedSources: []string{"first", "second"},
		before:          &before,
		sinceWindow:     24 * time.Hour,
	}
	cmd := &cobra.Command{}
	cmd.SetOut(new(strings.Builder))

	err := executeCollect(cmd, env)
	if err == nil || !strings.Contains(err.Error(), "partial failure") {
		t.Fatalf("error = %v", err)
	}
	if !secondInvoked || mem.HasRawSignal("first", "one") || mem.HasRawSignal("second", "two") || !mem.HasRawSignal("second", "three") {
		t.Fatalf("second invoked=%v memory=%+v", secondInvoked, mem.GetMemory())
	}
	if !mem.HasContentHash("hash-one") || !mem.HasContentHash("hash-two") {
		t.Fatal("persisted partial and successful content hashes should suppress duplicates")
	}
	if !store.Exists(filepath.Join(store.BaseDir(), "memory.json")) {
		t.Fatal("memory.json was not saved after partial failure")
	}

	files, err := store.ListFiles("raw-signals", ".json")
	if err != nil {
		t.Fatalf("list raw signals: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("raw signal files = %d, want 2", len(files))
	}
	savedIDs := make(map[string]bool, len(files))
	for _, path := range files {
		var signal domain.RawSignal
		if err := store.LoadJSON(path, &signal); err != nil {
			t.Fatalf("load raw signal %s: %v", path, err)
		}
		savedIDs[signal.ID] = true
	}
	if !savedIDs["first:one"] || !savedIDs["second:three"] || savedIDs["second:two"] {
		t.Fatalf("saved raw signal IDs = %v", savedIDs)
	}
}

func TestExecuteCollect_DoesNotRecordSignalsThatFailPersistence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "raw-signals"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := storage.New(dir)
	mem := memory.New(store)
	collector := &mockCollector{
		name: "source",
		collectFn: func(domain.CollectRequest) ([]domain.RawSignal, error) {
			return []domain.RawSignal{{
				ID:          "source:one",
				Source:      "source",
				SourceID:    "one",
				ContentHash: "hash-one",
			}}, nil
		},
	}
	before := mem.GetStats()
	env := &collectEnv{
		store:           store,
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"source"},
		before:          &before,
		sinceWindow:     24 * time.Hour,
	}
	cmd := &cobra.Command{}
	cmd.SetOut(new(strings.Builder))

	err := executeCollect(cmd, env)
	if err == nil || !strings.Contains(err.Error(), "persistence") {
		t.Fatalf("executeCollect() error = %v, want persistence error", err)
	}
	if mem.HasRawSignal("source", "one") || mem.HasContentHash("hash-one") {
		t.Fatal("unpersisted signal was recorded in deduplication memory")
	}
}

func TestExecuteCollect_ForcePartialFailurePreservesExistingSignal(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)
	complete := domain.RawSignal{
		ID:          "source:one",
		Source:      "source",
		SourceID:    "one",
		Body:        "complete",
		Comments:    []domain.Comment{{ID: "comment", Body: "complete comment"}},
		ContentHash: "complete-hash",
	}
	path := filepath.Join(store.BaseDir(), "raw-signals", storage.ContentHash(complete.ID)+".json")
	if err := store.SaveJSON(path, &complete); err != nil {
		t.Fatalf("save existing signal: %v", err)
	}
	mem.AddRawSignal(complete.Source, complete.SourceID)
	mem.AddContentHash(complete.ContentHash, complete.ID)

	collector := &mockCollector{
		name: "source",
		collectFn: func(domain.CollectRequest) ([]domain.RawSignal, error) {
			return []domain.RawSignal{{
				ID:          complete.ID,
				Source:      complete.Source,
				SourceID:    complete.SourceID,
				Body:        complete.Body,
				ContentHash: "partial-hash",
			}}, errors.New("comments failed")
		},
	}
	before := mem.GetStats()
	env := &collectEnv{
		store:           store,
		mem:             mem,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"source"},
		before:          &before,
		force:           true,
		sinceWindow:     24 * time.Hour,
	}
	cmd := &cobra.Command{}
	cmd.SetOut(new(strings.Builder))

	err := executeCollect(cmd, env)
	if err == nil || !strings.Contains(err.Error(), "comments failed") {
		t.Fatalf("executeCollect() error = %v, want collection error", err)
	}

	var got domain.RawSignal
	if err := store.LoadJSON(path, &got); err != nil {
		t.Fatalf("load existing signal: %v", err)
	}
	if got.ContentHash != complete.ContentHash || len(got.Comments) != 1 {
		t.Fatalf("existing signal was replaced by partial result: %+v", got)
	}
	if mem.HasContentHash("partial-hash") {
		t.Fatal("unsaved partial content hash was recorded in memory")
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
		store:           store,
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
		store:           store,
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

func TestDeduplicateSignals_RemovesDuplicatesWithinBatch(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	env := &collectEnv{mem: memory.New(store)}
	signals := []domain.RawSignal{
		{ID: "first", Source: "src", SourceID: "one", ContentHash: "hash-one"},
		{ID: "duplicate-source-id", Source: "src", SourceID: "one", ContentHash: "hash-two"},
		{ID: "duplicate-content", Source: "src", SourceID: "two", ContentHash: "hash-one"},
		{ID: "empty-hash-one", Source: "src", SourceID: "three"},
		{ID: "empty-hash-two", Source: "src", SourceID: "four"},
	}

	got := deduplicateSignals(signals, env)
	if len(got) != 3 {
		t.Fatalf("deduplicateSignals() returned %d signals, want 3: %#v", len(got), got)
	}
	if got[0].ID != "first" || got[1].ID != "empty-hash-one" || got[2].ID != "empty-hash-two" {
		t.Fatalf("deduplicateSignals() IDs = [%s %s %s], want [first empty-hash-one empty-hash-two]", got[0].ID, got[1].ID, got[2].ID)
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

func TestResolveCollectSources_RedditWithGitHub(t *testing.T) {
	t.Parallel()

	sources, err := resolveCollectSources("github,reddit")
	if err != nil {
		t.Fatalf("resolveCollectSources(github,reddit) failed: %v", err)
	}
	if len(sources) != 2 {
		t.Errorf("expected 2 sources, got %d: %v", len(sources), sources)
	}
}

func TestStatsDelta_NoReddit(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{}
	after := &domain.ResearchStats{
		RawSignalsCollected: 5,
		RedditRequests:      0,
		RedditCacheHits:     0,
	}

	delta := statsDelta(before, after)
	if delta.redditRequests != 0 {
		t.Errorf("expected redditRequests=0, got %d", delta.redditRequests)
	}
	if delta.redditCacheHits != 0 {
		t.Errorf("expected redditCacheHits=0, got %d", delta.redditCacheHits)
	}
}

func TestReportCollectSummary_RedditNoRequests(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected: 3,
		skipped:   1,
		sources: []sourceCollectionResult{
			{name: "reddit", attempted: 4, collected: 3, skipped: 1},
		},
	}

	err := reportCollectSummary(cmd, 3, &delta)
	if err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Reddit requests") {
		t.Errorf("unexpected Reddit requests when delta.redditRequests=0: %s", output)
	}
	if !strings.Contains(output, "Total new signals: 3") {
		t.Errorf("expected total line, got: %s", output)
	}
}

func TestOrderSourcesDeterministically_RedditLast(t *testing.T) {
	t.Parallel()

	input := []string{"github", "reddit", "hackernews", "stackexchange"}
	result := orderSourcesDeterministically(input)
	expected := []string{"github", "hackernews", "stackexchange", "reddit"}
	if len(result) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Fatalf("expected %v at index %d, got %v", expected, i, result)
		}
	}
}

func TestOrderSourcesDeterministically_RedditOnly(t *testing.T) {
	t.Parallel()

	result := orderSourcesDeterministically([]string{"reddit"})
	if len(result) != 1 || result[0] != "reddit" {
		t.Fatalf("expected [reddit], got %v", result)
	}
}

func TestBuildDryRunPlans_Reddit(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Enabled:            true,
				Subreddits:         []string{"golang", "cli"},
				MaxPostsPerRun:     100,
				MaxCommentsPerPost: 10,
				Sort:               "new",
				Time:               "week",
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"reddit"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     30 * 24 * time.Hour,
	}

	plans := buildDryRunPlans(env, cfg)

	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	if plans[0].Source != "reddit" {
		t.Errorf("expected reddit, got %s", plans[0].Source)
	}

	// Should have 2 targets (2 subreddits).
	if len(plans[0].Targets) != 2 {
		t.Errorf("expected 2 targets, got %d: %v", len(plans[0].Targets), plans[0].Targets)
	}

	// Estimated: 1 OAuth + 2 subreddits + 100 maxPosts = 103.
	if plans[0].EstimatedReqs != 103 {
		t.Errorf("expected 103 estimated requests, got %d", plans[0].EstimatedReqs)
	}
}

func TestBuildDryRunPlans_RedditDisabled(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Enabled:            false,
				Subreddits:         []string{"test"},
				MaxPostsPerRun:     100,
				MaxCommentsPerPost: 0,
				Sort:               "new",
				Time:               "week",
			},
		},
	}

	beforeStats := mem.GetStats()

	env := &collectEnv{
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{},
		selectedSources: []string{"reddit"},
		before:          &beforeStats,
		dryRun:          true,
		sinceWindow:     7 * 24 * time.Hour,
	}

	// Dry-run should still build plans even with disabled config.
	plans := buildDryRunPlans(env, cfg)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].Source != "reddit" {
		t.Errorf("expected reddit, got %s", plans[0].Source)
	}
}

func TestEstimateRedditRequestsWithComments(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Subreddits:         []string{"golang", "python", "rust"},
				MaxPostsPerRun:     200,
				MaxCommentsPerPost: 1,
			},
		},
	}

	env := &collectEnv{}
	reqs := estimateRedditRequests(cfg, env)
	// 1 OAuth + 2 listing pages for each of 3 subreddits + 200 comment requests.
	if reqs != 207 {
		t.Errorf("expected 207 requests, got %d", reqs)
	}
}

func TestEstimateRedditRequests_WithEnvMaxItems(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Subreddits:         []string{"golang"},
				MaxPostsPerRun:     200,
				MaxCommentsPerPost: 1,
			},
		},
	}

	env := &collectEnv{maxItems: 50}
	reqs := estimateRedditRequests(cfg, env)
	// 1 OAuth + 1 subreddit + 50 maxPosts = 52.
	if reqs != 52 {
		t.Errorf("expected 52 requests, got %d", reqs)
	}
}

func TestBuildRedditTargets(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Subreddits: []string{"golang", "rust"},
			},
		},
	}

	targets := buildRedditTargets(cfg)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %v", len(targets), targets)
	}
	if targets[0] != "subreddit: golang" {
		t.Errorf("expected 'subreddit: golang', got %q", targets[0])
	}
	if targets[1] != "subreddit: rust" {
		t.Errorf("expected 'subreddit: rust', got %q", targets[1])
	}
}

func TestBuildRedditTargets_EmptySubreddits(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Reddit: config.RedditConfig{
				Subreddits: []string{},
			},
		},
	}

	targets := buildRedditTargets(cfg)
	if len(targets) != 0 {
		t.Errorf("expected no targets for an invalid empty subreddit list, got %v", targets)
	}
}

func TestRedditCollectorStats_TrackCollectorStatsHandlesReddit(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	collector, err := reddit.New(&reddit.ConfigValues{
		Enabled:            true,
		ClientID:           "test-client-id",
		ClientSecret:       "test-client-secret",
		Subreddits:         []string{"test"},
		MaxPostsPerRun:     10,
		MaxCommentsPerPost: 5,
		Sort:               "new",
		TimeRange:          "week",
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("create reddit collector: %v", err)
	}

	beforeStats := mem.GetStats()
	env := &collectEnv{
		mem:    mem,
		before: &beforeStats,
	}

	// trackCollectorStats should not panic with a Reddit collector.
	trackCollectorStats(env, collector)

	// Stats should be recorded (requests=0, cacheHits=0 since no calls made).
	afterStats := mem.GetStats()
	if afterStats.RedditRequests != 0 {
		t.Errorf("expected 0 reddit requests, got %d", afterStats.RedditRequests)
	}
	if afterStats.RedditCacheHits != 0 {
		t.Errorf("expected 0 reddit cache hits, got %d", afterStats.RedditCacheHits)
	}
	// Ensure other stats were not modified.
	if afterStats.HackerNewsRequests != 0 {
		t.Errorf("expected 0 HN requests unchanged, got %d", afterStats.HackerNewsRequests)
	}
}

func TestStatsDelta_GitHubCacheHits(t *testing.T) {
	t.Parallel()

	before := &domain.ResearchStats{
		GitHubRequests:  5,
		GitHubCacheHits: 2,
	}
	after := &domain.ResearchStats{
		RawSignalsCollected: 20,
		GitHubRequests:      12,
		GitHubCacheHits:     7,
	}

	delta := statsDelta(before, after)
	if delta.requests != 7 {
		t.Errorf("expected requests=7, got %d", delta.requests)
	}
	if delta.githubCacheHits != 5 {
		t.Errorf("expected githubCacheHits=5, got %d", delta.githubCacheHits)
	}
}

func TestReportCollectSummary_GitHubCacheHits(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	delta := collectStatsDelta{
		collected:       10,
		githubCacheHits: 3,
		requests:        8,
		sources: []sourceCollectionResult{
			{name: "github", attempted: 10, collected: 10, skipped: 0},
		},
	}

	if err := reportCollectSummary(cmd, 10, &delta); err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "GitHub requests: 8 (cache hits: 3)") {
		t.Errorf("expected 'GitHub requests: 8 (cache hits: 3)' in output, got: %s", output)
	}
}

func TestGitHubCollectorStats_TrackCollectorStatsHandlesGitHub(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	collectorCfg := github.CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  false,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		MaxRequests:        500,
	}
	collector, err := github.New(&collectorCfg)
	if err != nil {
		t.Fatalf("create github collector: %v", err)
	}

	beforeStats := mem.GetStats()
	env := &collectEnv{
		mem:    mem,
		before: &beforeStats,
	}

	// trackCollectorStats should not panic with a GitHub collector.
	trackCollectorStats(env, collector)

	// Stats should be recorded (requests=0, cacheHits=0 since no calls made).
	afterStats := mem.GetStats()
	if afterStats.GitHubRequests != 0 {
		t.Errorf("expected 0 github requests, got %d", afterStats.GitHubRequests)
	}
	if afterStats.GitHubCacheHits != 0 {
		t.Errorf("expected 0 github cache hits, got %d", afterStats.GitHubCacheHits)
	}
	// Ensure other stats were not modified.
	if afterStats.HackerNewsRequests != 0 {
		t.Errorf("expected 0 HN requests unchanged, got %d", afterStats.HackerNewsRequests)
	}
}

func TestGitHubCollectorStats_SaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Add some GitHub stats manually.
	mem.AddGitHubRequests(3)
	mem.AddGitHubCacheHits(5)

	// Save.
	if err := mem.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into a new memory instance.
	mem2 := memory.New(store)
	if err := mem2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	loadedStats := mem2.GetStats()
	if loadedStats.GitHubRequests != 3 {
		t.Errorf("expected 3 github requests after reload, got %d", loadedStats.GitHubRequests)
	}
	if loadedStats.GitHubCacheHits != 5 {
		t.Errorf("expected 5 github cache hits after reload, got %d", loadedStats.GitHubCacheHits)
	}
}

func TestGitHubCollectorStats_StatsRecordedInSummary(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	// Manually increment GitHub stats via memory.
	mem.AddGitHubRequests(7)
	mem.AddGitHubCacheHits(3)

	afterStats := mem.GetStats()
	beforeStats := domain.ResearchStats{}

	delta := statsDelta(&beforeStats, &afterStats)
	if delta.requests != 7 {
		t.Errorf("expected requests=7, got %d", delta.requests)
	}
	if delta.githubCacheHits != 3 {
		t.Errorf("expected githubCacheHits=3, got %d", delta.githubCacheHits)
	}

	// Verify the summary output includes these stats.
	cmd := &cobra.Command{}
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	delta.sources = []sourceCollectionResult{
		{name: "github", attempted: 7, collected: 7, skipped: 0},
	}

	if err := reportCollectSummary(cmd, 7, &delta); err != nil {
		t.Fatalf("reportCollectSummary failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "GitHub requests: 7 (cache hits: 3)") {
		t.Errorf("expected 'GitHub requests: 7 (cache hits: 3)' in output, got: %s", output)
	}
	if !strings.Contains(output, "github: attempted=7, collected=7, dedup-skipped=0, status=ok") {
		t.Errorf("expected per-source breakdown, got: %s", output)
	}
}

// githubTransportFunc implements the github.transport interface for testing.
type githubTransportFunc func(req *http.Request) (*http.Response, error)

func (f githubTransportFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestExecuteCollect_GitHubMixedIssuesAndDiscussions verifies that executing
// a GitHub collector through the CLI path with both Issues and Discussions
// enabled persists signals of both types.
func TestExecuteCollect_GitHubMixedIssuesAndDiscussions(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := config.DefaultConfig()
	cfg.Sources.GitHub.Enabled = true
	cfg.Sources.GitHub.SearchIssues = true
	cfg.Sources.GitHub.SearchDiscussions = true
	cfg.Sources.GitHub.MaxItemsPerRun = 100
	cfg.Sources.GitHub.MaxCommentsPerItem = 10
	cfg.Sources.GitHub.Repositories = []string{"owner/repo"}
	cfg.Sources.GitHub.Languages = nil
	cfg.Sources.GitHub.Labels = nil

	// Set up a fake transport that serves both REST issues and GraphQL discussions.
	var transport githubTransportFunc = func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()

		// REST issues per-repo endpoint.
		if strings.Contains(url, "/repos/owner/repo/issues") {
			issues := []map[string]any{
				{
					"id": 1001, "number": 1, "title": "Bug report", "body": "Crash on startup",
					"html_url": "https://github.com/owner/repo/issues/1", "state": "open",
					"created_at": "2026-06-01T00:00:00Z", "updated_at": "2026-06-02T00:00:00Z",
					"user":           map[string]any{"login": "user1", "id": 1},
					"comments":       0,
					"reactions":      map[string]int{"+1": 3},
					"repository_url": "https://api.github.com/repos/owner/repo",
				},
			}
			body, _ := json.Marshal(issues)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     http.Header{"X-RateLimit-Remaining": []string{"4999"}, "X-RateLimit-Reset": []string{"0"}},
			}, nil
		}

		// GraphQL endpoint for discussions.
		if strings.Contains(url, "/graphql") {
			discResp := map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"discussions": map[string]any{
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
							"nodes": []map[string]any{
								{
									"id": "D_kwDOTEST", "number": 5,
									"title": "Feature request: dark mode", "body": "Would love dark mode",
									"url":       "https://github.com/owner/repo/discussions/5",
									"createdAt": "2026-06-01T00:00:00Z", "updatedAt": "2026-06-02T00:00:00Z",
									"category":    map[string]any{"name": "Ideas", "slug": "ideas"},
									"labels":      map[string]any{"nodes": []map[string]any{{"name": "enhancement"}}},
									"comments":    map[string]any{"totalCount": 0, "nodes": []any{}},
									"upvoteCount": 10,
								},
							},
						},
					},
				},
			}
			body, _ := json.Marshal(discResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     http.Header{"X-RateLimit-Remaining": []string{"4998"}, "X-RateLimit-Reset": []string{"0"}},
			}, nil
		}

		return nil, fmt.Errorf("unexpected URL: %s", url)
	}

	collector, err := github.New(&github.CollectorConfig{
		Enabled:            true,
		SearchIssues:       true,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 10,
		Repositories:       []string{"owner/repo"},
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	collector.WithTransport(githubTransportFunc(transport))
	collector.WithNow(func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })

	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"github"},
		before:          &beforeStats,
		sinceWindow:     60 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Logf("executeCollect returned error (may include partial success): %v", err)
	}

	// Verify memory was saved.
	if !store.Exists(filepath.Join(store.BaseDir(), "memory.json")) {
		t.Fatal("memory.json was not saved")
	}

	// Verify raw signals contain both issue and discussion types.
	files, err := store.ListFiles("raw-signals", ".json")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}

	var foundIssue, foundDiscussion bool
	for _, f := range files {
		var signal domain.RawSignal
		if err := store.LoadJSON(f, &signal); err != nil {
			t.Fatalf("load signal %s: %v", f, err)
		}
		if signal.SourceType == "github_issue" {
			foundIssue = true
		}
		if signal.SourceType == "github_discussion" {
			foundDiscussion = true
		}
	}

	if !foundIssue {
		t.Error("expected at least one github_issue signal in persisted output")
	}
	if !foundDiscussion {
		t.Error("expected at least one github_discussion signal in persisted output")
	}

	// Verify the summary output includes both types in per-source breakdown.
	output := buf.String()
	if !strings.Contains(output, "github: attempted=") {
		t.Errorf("expected per-source breakdown in summary, got: %s", output)
	}
}

// TestExecuteCollect_GitHubDiscussionsOnly verifies that GraphQL collection
// works independently even when REST issues fail.
func TestExecuteCollect_GitHubDiscussionsOnly(t *testing.T) {
	t.Parallel()

	store := storage.New(t.TempDir())
	mem := memory.New(store)

	cfg := config.DefaultConfig()
	cfg.Sources.GitHub.Enabled = true
	cfg.Sources.GitHub.SearchIssues = false
	cfg.Sources.GitHub.SearchDiscussions = true
	cfg.Sources.GitHub.MaxItemsPerRun = 100
	cfg.Sources.GitHub.MaxCommentsPerItem = 0
	cfg.Sources.GitHub.Repositories = []string{"owner/repo"}
	cfg.Sources.GitHub.Languages = nil
	cfg.Sources.GitHub.Labels = nil

	// Only register GraphQL endpoint; Issues endpoint is NOT registered,
	// which means any REST call will fail with an unexpected URL error.
	var transport githubTransportFunc = func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()

		// Only handle GraphQL.
		if strings.Contains(url, "/graphql") {
			discResp := map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"discussions": map[string]any{
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
							"nodes": []map[string]any{
								{
									"id": "D_kwDODISC1", "number": 1,
									"title": "Discussion-only test", "body": "This discussion works",
									"url":       "https://github.com/owner/repo/discussions/1",
									"createdAt": "2026-06-01T00:00:00Z", "updatedAt": "2026-06-02T00:00:00Z",
									"category": nil, "labels": nil,
									"comments":    map[string]any{"totalCount": 0, "nodes": []any{}},
									"upvoteCount": 3,
								},
							},
						},
					},
				},
			}
			body, _ := json.Marshal(discResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     http.Header{"X-RateLimit-Remaining": []string{"4999"}, "X-RateLimit-Reset": []string{"0"}},
			}, nil
		}

		return nil, fmt.Errorf("unexpected URL: %s", url)
	}

	collector, err := github.New(&github.CollectorConfig{
		Enabled:            true,
		SearchIssues:       false,
		SearchDiscussions:  true,
		MaxItemsPerRun:     100,
		MaxCommentsPerItem: 0,
		Repositories:       []string{"owner/repo"},
		MaxRequests:        500,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	collector.WithTransport(githubTransportFunc(transport))
	collector.WithNow(func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })

	beforeStats := mem.GetStats()

	env := &collectEnv{
		store:           store,
		mem:             mem,
		cfg:             cfg,
		collectors:      []domain.SourceCollector{collector},
		selectedSources: []string{"github"},
		before:          &beforeStats,
		sinceWindow:     60 * 24 * time.Hour,
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	if err := executeCollect(cmd, env); err != nil {
		t.Logf("executeCollect returned error (may include partial success): %v", err)
	}

	// Verify only discussion signals were persisted.
	files, err := store.ListFiles("raw-signals", ".json")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one signal to be persisted")
	}

	for _, f := range files {
		var signal domain.RawSignal
		if err := store.LoadJSON(f, &signal); err != nil {
			t.Fatalf("load signal %s: %v", f, err)
		}
		if signal.SourceType != "github_discussion" {
			t.Errorf("expected github_discussion source type, got %q", signal.SourceType)
		}
	}
}
