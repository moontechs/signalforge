package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/config"
	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/sources/hackernews"
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
